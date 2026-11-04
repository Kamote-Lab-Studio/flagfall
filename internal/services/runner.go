// Package services runs the shared challenge containers declared by
// `service:` in challenge.yml (docs/dev/challenge-services.md).
//
// The organizer starts and stops each service in the admin panel; the
// runner records that as the desired state and, every few seconds, makes
// Docker match it: it builds or pulls the image, creates an isolated network
// and a container with CPU, memory and process limits, publishes the port,
// and checks that the service answers. What it sees is stored in
// challenge_services for the dashboard.
package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Kamote-Lab-Studio/flagfall/internal/blobs"
	"github.com/Kamote-Lab-Studio/flagfall/internal/challenges"
	"github.com/Kamote-Lab-Studio/flagfall/internal/store"
)

// States a service can be in.
const (
	StateStopped   = "stopped"
	StateStarting  = "starting"  // building, pulling, or waiting for its first health check
	StateRunning   = "running"   // up and answering its health check
	StateUnhealthy = "unhealthy" // up but not answering
	StateFailed    = "failed"    // couldn't be started; Message says why
)

// Desired states the organizer can choose.
const (
	DesiredRunning = "running"
	DesiredStopped = "stopped"
)

// Labels on everything the runner creates. Containers with LabelManaged
// that no challenge wants any more are removed.
const (
	LabelManaged   = "dev.flagfall.managed"
	LabelChallenge = "dev.flagfall.challenge"
	LabelSpec      = "dev.flagfall.spec"
)

// startGrace is how long a new container may fail its health check before
// it counts as unhealthy.
const startGrace = 30 * time.Second

// Options configure a Runner.
type Options struct {
	Store *store.Store
	Files *blobs.Store
	// Docker is nil when no Docker host is configured; the runner then only
	// records what organizers ask for.
	Docker *Docker
	// Bind is the host address published ports listen on (default 0.0.0.0).
	Bind string
	// ProbeHost is where the runner reaches published ports for health
	// checks (default 127.0.0.1; host.docker.internal when Flagfall itself
	// runs in a container).
	ProbeHost string
	Logger    *slog.Logger
	Now       func() time.Time
}

// Runner keeps challenge containers in their desired state.
type Runner struct {
	o       Options
	mu      sync.Mutex // one reconcile at a time
	kick    chan struct{}
	restart sync.Map // slug -> true: replace the container on the next pass
}

// New returns a runner.
func New(o Options) *Runner {
	if o.Bind == "" {
		o.Bind = "0.0.0.0"
	}
	if o.ProbeHost == "" {
		o.ProbeHost = "127.0.0.1"
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Runner{o: o, kick: make(chan struct{}, 1)}
}

// Enabled reports whether a Docker host is configured.
func (r *Runner) Enabled() bool { return r != nil && r.o.Docker != nil }

// ErrNoService means the challenge has no `service:` block.
var ErrNoService = errors.New("this challenge has no service")

// SetDesired records whether the organizer wants the service running, and
// wakes the runner.
func (r *Runner) SetDesired(ctx context.Context, slug, desired string) error {
	if desired != DesiredRunning && desired != DesiredStopped {
		return fmt.Errorf("desired state must be running or stopped")
	}
	c := new(store.Challenge)
	if err := r.o.Store.DB.NewSelect().Model(c).Where("slug = ?", slug).Scan(ctx); err != nil {
		return err
	}
	if c.Service == nil {
		return ErrNoService
	}
	row := &store.ChallengeService{ChallengeID: c.ID, Runtime: "docker", Ref: ContainerName(slug), Desired: desired, Status: StateStopped}
	if desired == DesiredRunning {
		row.Status = StateStarting
	}
	if _, err := r.o.Store.DB.NewInsert().Model(row).
		On("CONFLICT (challenge_id) DO UPDATE").Set("desired = EXCLUDED.desired").Set("status = EXCLUDED.status").Set("message = ''").Exec(ctx); err != nil {
		return err
	}
	r.Kick()
	return nil
}

// Restart replaces the service's container on the next pass.
func (r *Runner) Restart(ctx context.Context, slug string) error {
	if err := r.SetDesired(ctx, slug, DesiredRunning); err != nil {
		return err
	}
	r.restart.Store(slug, true)
	r.Kick()
	return nil
}

// Kick asks for a reconcile soon.
func (r *Runner) Kick() {
	if r == nil {
		return
	}
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

// Run reconciles every interval and whenever Kick is called, until ctx ends.
func (r *Runner) Run(ctx context.Context, every time.Duration) {
	if !r.Enabled() {
		return
	}
	if err := r.o.Docker.Ping(ctx); err != nil {
		r.o.Logger.Warn("challenge services: can't reach Docker; services won't start until it's back", "err", err)
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := r.Reconcile(ctx); err != nil && ctx.Err() == nil {
			r.o.Logger.Error("challenge services", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-r.kick:
		}
	}
}

// ContainerName is the container (and network) name for a challenge.
func ContainerName(slug string) string { return "flagfall-chal-" + slug }

// imageTag is the tag of an image built from a challenge folder.
func imageTag(slug string, spec *challenges.ServiceSpec) string {
	return "flagfall-chal/" + slug + ":" + spec.Context[:12]
}

// Reconcile makes Docker match the desired state once.
func (r *Runner) Reconcile(ctx context.Context) error {
	if !r.Enabled() {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	db := r.o.Store.DB

	var cs []store.Challenge
	if err := db.NewSelect().Model(&cs).Where("service IS NOT NULL").Scan(ctx); err != nil {
		return err
	}
	var rows []store.ChallengeService
	if err := db.NewSelect().Model(&rows).Scan(ctx); err != nil {
		return err
	}
	desired := map[int64]*store.ChallengeService{}
	for i := range rows {
		desired[rows[i].ChallengeID] = &rows[i]
	}
	list, err := r.o.Docker.ContainerList(ctx, LabelManaged+"=1")
	if err != nil {
		return err
	}
	containers := map[string]Container{}
	for _, c := range list {
		containers[c.Labels[LabelChallenge]] = c
	}

	wanted := map[string]bool{}
	for i := range cs {
		c := &cs[i]
		spec, err := challenges.ParseService(c.Service)
		if err != nil || spec == nil {
			continue
		}
		wanted[c.Slug] = true
		row := desired[c.ID]
		if row == nil {
			continue // never started: nothing to do
		}
		existing, has := containers[c.Slug]
		var cur *Container
		if has {
			cur = &existing
		}
		run := row.Desired == DesiredRunning && c.State != "retired"
		var st status
		if run {
			st = r.ensureRunning(ctx, c.Slug, spec, cur)
		} else {
			st = status{state: StateStopped}
			if cur != nil {
				if err := r.remove(ctx, c.Slug, cur.ID); err != nil {
					st = status{state: StateFailed, message: "Couldn't stop: " + err.Error()}
				}
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		now := r.o.Now().UTC()
		row.Status, row.Message, row.HostPort, row.CheckedAt = st.state, st.message, st.hostPort, &now
		if _, err := db.NewUpdate().Model(row).Column("status", "message", "host_port", "checked_at").WherePK().Exec(ctx); err != nil {
			return err
		}
	}

	// Containers whose challenge was deleted or lost its service.
	for slug, c := range containers {
		if !wanted[slug] {
			r.o.Logger.Info("challenge services: removing a container no challenge uses", "container", ContainerName(slug))
			if err := r.remove(ctx, slug, c.ID); err != nil {
				r.o.Logger.Warn("challenge services: remove", "container", ContainerName(slug), "err", err)
			}
		}
	}
	return nil
}

type status struct {
	state, message string
	hostPort       *int
}

func failed(format string, args ...any) status {
	return status{state: StateFailed, message: fmt.Sprintf(format, args...)}
}

// ensureRunning creates or replaces the container as needed and checks it.
func (r *Runner) ensureRunning(ctx context.Context, slug string, spec *challenges.ServiceSpec, cur *Container) status {
	d := r.o.Docker
	hash := spec.Hash()
	_, restart := r.restart.LoadAndDelete(slug)
	if cur != nil && (restart || cur.Labels[LabelSpec] != hash) {
		if err := d.ContainerRemove(ctx, cur.ID); err != nil {
			return failed("Couldn't replace the old container: %v", err)
		}
		cur = nil
	}

	id := ""
	if cur == nil {
		image, err := r.ensureImage(ctx, slug, spec)
		if err != nil {
			return failed("%v", err)
		}
		network := ContainerName(slug)
		labels := map[string]string{LabelManaged: "1", LabelChallenge: slug}
		if err := d.NetworkEnsure(ctx, network, labels); err != nil {
			return failed("Couldn't create the network: %v", err)
		}
		labels[LabelSpec] = hash
		id, err = d.ContainerCreate(ctx, ContainerName(slug), createBody(spec, image, network, r.o.Bind, labels))
		if err != nil {
			return failed("Couldn't create the container: %v", err)
		}
		r.o.Logger.Info("challenge services: created", "container", ContainerName(slug), "image", image)
	} else {
		id = cur.ID
	}

	info, err := d.ContainerInspect(ctx, id)
	if err != nil {
		return failed("Couldn't read the container's state: %v", err)
	}
	if !info.State.Running && !info.State.Restarting {
		// New, or stopped after crashing more often than the restart policy allows.
		if info.State.Status == "exited" || info.State.Status == "dead" {
			why := fmt.Sprintf("It exited with code %d", info.State.ExitCode)
			if info.State.OOMKilled {
				why = "It ran out of memory"
			}
			if info.RestartCount >= maxRestarts {
				return failed("%s and was restarted %d times; check its logs (docker logs %s), then Restart.", why, info.RestartCount, ContainerName(slug))
			}
		}
		if err := d.ContainerStart(ctx, id); err != nil {
			return failed("Couldn't start the container: %v", err)
		}
		if info, err = d.ContainerInspect(ctx, id); err != nil {
			return failed("Couldn't read the container's state: %v", err)
		}
	}

	st := status{}
	for _, b := range info.NetworkSettings.Ports[portKey(spec.Port)] {
		if p, err := strconv.Atoi(b.HostPort); err == nil && p > 0 {
			st.hostPort = &p
			break
		}
	}
	switch {
	case info.State.Restarting:
		st.state, st.message = StateUnhealthy, fmt.Sprintf("It keeps crashing (exit code %d, %d restarts).", info.State.ExitCode, info.RestartCount)
	case st.hostPort == nil:
		st.state, st.message = StateUnhealthy, "The port isn't published."
	default:
		if err := r.probe(ctx, *st.hostPort, spec.Healthcheck); err != nil {
			st.state, st.message = StateUnhealthy, "Health check failed: "+err.Error()
			if r.o.Now().Sub(info.State.StartedAt) < startGrace {
				st.state, st.message = StateStarting, "Waiting for the service to answer."
			}
		} else {
			st.state = StateRunning
		}
	}
	return st
}

// maxRestarts is the on-failure restart limit given to Docker.
const maxRestarts = 5

// ensureImage pulls or builds the service's image if it isn't there yet.
func (r *Runner) ensureImage(ctx context.Context, slug string, spec *challenges.ServiceSpec) (string, error) {
	d := r.o.Docker
	ref := spec.Image
	if spec.Build() {
		ref = imageTag(slug, spec)
	}
	ok, err := d.ImageExists(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("couldn't check for the image: %w", err)
	}
	if ok {
		return ref, nil
	}
	if !spec.Build() {
		r.o.Logger.Info("challenge services: pulling", "image", ref)
		if err := d.Pull(ctx, ref); err != nil {
			return "", fmt.Errorf("couldn't pull %s: %w", ref, err)
		}
		return ref, nil
	}
	r.o.Logger.Info("challenge services: building", "image", ref)
	in, err := r.o.Files.Open(ctx, spec.Context)
	if err != nil {
		return "", fmt.Errorf("the build files are missing from storage (import the challenge again): %w", err)
	}
	defer in.Close()
	if err := d.Build(ctx, in, spec.Dockerfile, ref, map[string]string{LabelManaged: "1", LabelChallenge: slug}); err != nil {
		return "", fmt.Errorf("the image didn't build: %w", err)
	}
	return ref, nil
}

func (r *Runner) remove(ctx context.Context, slug, id string) error {
	if err := r.o.Docker.ContainerRemove(ctx, id); err != nil {
		return err
	}
	return r.o.Docker.NetworkRemove(ctx, ContainerName(slug))
}

func portKey(port int) string { return strconv.Itoa(port) + "/tcp" }

// createBody is the Docker create request for a service container.
func createBody(spec *challenges.ServiceSpec, image, network, bind string, labels map[string]string) map[string]any {
	env := make([]string, 0, len(spec.Env))
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}
	hostPort := ""
	if spec.Publish > 0 {
		hostPort = strconv.Itoa(spec.Publish)
	}
	mem := int64(spec.MemoryMB) << 20
	return map[string]any{
		"Image":        image,
		"Env":          env,
		"Labels":       labels,
		"ExposedPorts": map[string]any{portKey(spec.Port): map[string]any{}},
		"HostConfig": map[string]any{
			"NetworkMode":   network,
			"PortBindings":  map[string]any{portKey(spec.Port): []map[string]string{{"HostIp": bind, "HostPort": hostPort}}},
			"NanoCpus":      int64(spec.CPU * 1e9),
			"Memory":        mem,
			"MemorySwap":    mem, // no swap on top of the limit
			"PidsLimit":     256,
			"SecurityOpt":   []string{"no-new-privileges"},
			"CapDrop":       []string{"NET_RAW", "MKNOD", "AUDIT_WRITE", "SETFCAP", "SYS_CHROOT"},
			"RestartPolicy": map[string]any{"Name": "on-failure", "MaximumRetryCount": maxRestarts},
			"LogConfig":     map[string]any{"Type": "json-file", "Config": map[string]string{"max-size": "10m", "max-file": "2"}},
		},
	}
}

// probe checks that the service answers on its published port.
func (r *Runner) probe(ctx context.Context, port int, check string) error {
	addr := net.JoinHostPort(r.o.ProbeHost, strconv.Itoa(port))
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if check == "tcp" || check == "" {
		var d net.Dialer
		c, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return err
		}
		return c.Close()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+check, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode >= 500 {
		return fmt.Errorf("GET %s answered %d", check, res.StatusCode)
	}
	return nil
}

// Status is one service as the admin panel shows it.
type Status struct {
	Slug      string     `json:"slug"`
	Name      string     `json:"name"`
	Image     string     `json:"image"`
	Port      int        `json:"port"`
	Desired   string     `json:"desired"`
	State     string     `json:"state"`
	Message   string     `json:"message"`
	HostPort  *int       `json:"hostPort"`
	CPU       float64    `json:"cpu"`
	MemoryMB  int        `json:"memoryMB"`
	CheckedAt *time.Time `json:"checkedAt"`
}

// List returns every challenge with a service and its last known state.
func (r *Runner) List(ctx context.Context) ([]Status, error) {
	var cs []store.Challenge
	if err := r.o.Store.DB.NewSelect().Model(&cs).Where("service IS NOT NULL").Order("position", "id").Scan(ctx); err != nil {
		return nil, err
	}
	var rows []store.ChallengeService
	if err := r.o.Store.DB.NewSelect().Model(&rows).Scan(ctx); err != nil {
		return nil, err
	}
	byID := map[int64]store.ChallengeService{}
	for _, row := range rows {
		byID[row.ChallengeID] = row
	}
	out := []Status{}
	for _, c := range cs {
		spec, err := challenges.ParseService(c.Service)
		if err != nil || spec == nil {
			continue
		}
		s := Status{Slug: c.Slug, Name: c.Name, Image: spec.Image, Port: spec.Port, CPU: spec.CPU, MemoryMB: spec.MemoryMB,
			Desired: DesiredStopped, State: StateStopped}
		if spec.Build() {
			s.Image = "built from ./" + spec.Dockerfile
		}
		if row, ok := byID[c.ID]; ok {
			s.Desired, s.State, s.Message, s.HostPort, s.CheckedAt = row.Desired, row.Status, row.Message, row.HostPort, row.CheckedAt
		}
		out = append(out, s)
	}
	return out, nil
}
