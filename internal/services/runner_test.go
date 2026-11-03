package services

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flagfall/flagfall/internal/blobs"
	"github.com/flagfall/flagfall/internal/challenges"
	"github.com/flagfall/flagfall/internal/store"
	"github.com/flagfall/flagfall/internal/store/storetest"
)

// fakeDocker is an in-memory Docker Engine API with just what the runner uses.
type fakeDocker struct {
	mu         sync.Mutex
	t          *testing.T
	images     map[string]bool
	builds     []fakeBuild
	pulls      []string
	networks   map[string]map[string]any
	containers map[string]*fakeContainer
	nextID     int
	// hostPort is what every container's port is published on: a real
	// listener in the test, so health checks can pass.
	hostPort  int
	buildFail string
}

type fakeBuild struct {
	tag, dockerfile string
	files           []string
}

type fakeContainer struct {
	id, name     string
	body         map[string]any
	running      bool
	status       string
	exitCode     int
	restartCount int
	startedAt    time.Time
}

func newFakeDocker(t *testing.T) (*fakeDocker, *Docker) {
	f := &fakeDocker{t: t, images: map[string]bool{}, networks: map[string]map[string]any{}, containers: map[string]*fakeContainer{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	d, err := NewDocker("tcp://" + strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	return f, d
}

func (f *fakeDocker) labels(c *fakeContainer) map[string]string {
	out := map[string]string{}
	for k, v := range c.body["Labels"].(map[string]any) {
		out[k] = v.(string)
	}
	return out
}

func (f *fakeDocker) byID(id string) *fakeContainer {
	for _, c := range f.containers {
		if c.id == id || c.name == id {
			return c
		}
	}
	return nil
}

func (f *fakeDocker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := strings.TrimPrefix(r.URL.Path, "/"+apiVersion)
	q := r.URL.Query()
	notFound := func() { w.WriteHeader(404); fmt.Fprint(w, `{"message":"no such thing"}`) }
	switch {
	case p == "/_ping":
		fmt.Fprint(w, "OK")
	case r.Method == "GET" && strings.HasPrefix(p, "/images/") && strings.HasSuffix(p, "/json"):
		if !f.images[strings.TrimSuffix(strings.TrimPrefix(p, "/images/"), "/json")] {
			notFound()
			return
		}
		fmt.Fprint(w, `{}`)
	case p == "/images/create":
		ref := q.Get("fromImage") + ":" + q.Get("tag")
		f.pulls = append(f.pulls, ref)
		f.images[ref] = true
		fmt.Fprint(w, `{"status":"Downloaded"}`+"\n")
	case p == "/build":
		if f.buildFail != "" {
			fmt.Fprintf(w, `{"stream":"Step 1/2"}`+"\n"+`{"errorDetail":{"message":%q},"error":%q}`+"\n", f.buildFail, f.buildFail)
			return
		}
		b := fakeBuild{tag: q.Get("t"), dockerfile: q.Get("dockerfile")}
		tr := tar.NewReader(r.Body)
		for {
			h, err := tr.Next()
			if err != nil {
				break
			}
			b.files = append(b.files, h.Name)
		}
		f.builds = append(f.builds, b)
		f.images[b.tag] = true
		fmt.Fprint(w, `{"stream":"Successfully built"}`+"\n")
	case r.Method == "GET" && p == "/containers/json":
		var filter map[string][]string
		_ = json.Unmarshal([]byte(q.Get("filters")), &filter)
		want := strings.SplitN(filter["label"][0], "=", 2)
		out := []map[string]any{}
		for _, c := range f.containers {
			if l := f.labels(c); l[want[0]] == want[1] {
				out = append(out, map[string]any{"Id": c.id, "Names": []string{"/" + c.name}, "Labels": l, "State": c.status})
			}
		}
		_ = json.NewEncoder(w).Encode(out)
	case p == "/containers/create":
		name := q.Get("name")
		if f.containers[name] != nil {
			w.WriteHeader(409)
			fmt.Fprint(w, `{"message":"name in use"}`)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !f.images[body["Image"].(string)] {
			notFound()
			return
		}
		f.nextID++
		c := &fakeContainer{id: "c" + strconv.Itoa(f.nextID), name: name, body: body, status: "created"}
		f.containers[name] = c
		fmt.Fprintf(w, `{"Id":%q}`, c.id)
	case strings.HasPrefix(p, "/containers/"):
		parts := strings.Split(strings.TrimPrefix(p, "/containers/"), "/")
		c := f.byID(parts[0])
		if c == nil {
			notFound()
			return
		}
		switch {
		case r.Method == "DELETE":
			delete(f.containers, c.name)
		case len(parts) == 2 && parts[1] == "start":
			c.running, c.status, c.startedAt = true, "running", time.Now()
		case len(parts) == 2 && parts[1] == "stop":
			c.running, c.status = false, "exited"
		case len(parts) == 2 && parts[1] == "json":
			port := ""
			for k := range c.body["ExposedPorts"].(map[string]any) {
				port = k
			}
			ports := map[string]any{}
			if c.running {
				ports[port] = []map[string]string{{"HostIp": "0.0.0.0", "HostPort": strconv.Itoa(f.hostPort)}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Id": c.id,
				"State": map[string]any{"Status": c.status, "Running": c.running, "ExitCode": c.exitCode,
					"StartedAt": c.startedAt.UTC().Format(time.RFC3339Nano)},
				"RestartCount":    c.restartCount,
				"NetworkSettings": map[string]any{"Ports": ports},
			})
		default:
			notFound()
		}
	case r.Method == "GET" && strings.HasPrefix(p, "/networks/"):
		if f.networks[strings.TrimPrefix(p, "/networks/")] == nil {
			notFound()
			return
		}
		fmt.Fprint(w, `{}`)
	case p == "/networks/create":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.networks[body["Name"].(string)] = body
		fmt.Fprint(w, `{"Id":"n1"}`)
	case r.Method == "DELETE" && strings.HasPrefix(p, "/networks/"):
		name := strings.TrimPrefix(p, "/networks/")
		if f.networks[name] == nil {
			notFound()
			return
		}
		delete(f.networks, name)
	default:
		f.t.Errorf("fake docker: unexpected %s %s", r.Method, r.URL)
		w.WriteHeader(500)
	}
}

// listen opens a TCP listener that stands in for a published service.
func listen(t *testing.T) (net.Listener, int) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	return l, l.Addr().(*net.TCPAddr).Port
}

const webYML = `slug: web
name: Web
category: Web
description: Hi.
value: 100
flags: [{type: exact, value: "flag{web}"}]
service:
  image: ./Dockerfile
  port: 8080
  memory: 128Mi
  env: {MODE: prod}
`

const echoYML = `slug: echo
name: Echo
category: Misc
description: Hi.
value: 50
flags: [{type: exact, value: "flag{echo}"}]
service:
  image: busybox:1.36
  port: 7
  cpu: 0.25
`

type harness struct {
	ctx   context.Context
	db    *store.Store
	files *blobs.Store
	fake  *fakeDocker
	r     *Runner
	now   time.Time
}

func newHarness(t *testing.T) *harness {
	h := &harness{ctx: context.Background(), db: storetest.New(t), files: blobs.Local(t.TempDir()), now: time.Now()}
	var d *Docker
	h.fake, d = newFakeDocker(t)
	h.r = New(Options{Store: h.db, Files: h.files, Docker: d, Now: func() time.Time { return h.now }})
	h.importYML(t, webYML, echoYML)
	return h
}

func (h *harness) importYML(t *testing.T, ymls ...string) {
	t.Helper()
	fsys := fstest.MapFS{}
	for _, y := range ymls {
		slug := strings.TrimPrefix(strings.SplitN(y, "\n", 2)[0], "slug: ")
		fsys[slug+"/challenge.yml"] = &fstest.MapFile{Data: []byte(y)}
		fsys[slug+"/Dockerfile"] = &fstest.MapFile{Data: []byte("FROM busybox\n")}
	}
	specs, err := challenges.Load(fsys, ".")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := challenges.Apply(h.ctx, h.db, []byte("0123456789abcdef0123456789abcdef"), h.files, specs); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) reconcile(t *testing.T) map[string]Status {
	t.Helper()
	if err := h.r.Reconcile(h.ctx); err != nil {
		t.Fatal(err)
	}
	list, err := h.r.List(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Status{}
	for _, s := range list {
		out[s.Slug] = s
	}
	return out
}

func TestRunnerStartsServicesWithLimits(t *testing.T) {
	h := newHarness(t)
	l, port := listen(t)
	defer l.Close()
	h.fake.hostPort = port

	// Nothing runs until an organizer starts it.
	st := h.reconcile(t)
	if len(st) != 2 || st["web"].State != StateStopped || len(h.fake.containers) != 0 {
		t.Fatalf("before start: %+v, containers %v", st, h.fake.containers)
	}

	if err := h.r.SetDesired(h.ctx, "web", DesiredRunning); err != nil {
		t.Fatal(err)
	}
	st = h.reconcile(t)
	web := st["web"]
	if web.State != StateRunning || web.HostPort == nil || *web.HostPort != port || web.Message != "" {
		t.Fatalf("web = %+v", web)
	}
	if st["echo"].State != StateStopped {
		t.Fatalf("echo = %+v", st["echo"])
	}

	// Built from the challenge folder, without challenge.yml.
	if len(h.fake.builds) != 1 || h.fake.builds[0].dockerfile != "Dockerfile" || !strings.HasPrefix(h.fake.builds[0].tag, "flagfall-chal/web:") ||
		strings.Join(h.fake.builds[0].files, ",") != "Dockerfile" {
		t.Fatalf("builds = %+v", h.fake.builds)
	}
	// Its own network, with containers unable to talk to each other.
	nw := h.fake.networks["flagfall-chal-web"]
	if nw == nil || nw["Options"].(map[string]any)["com.docker.network.bridge.enable_icc"] != "false" {
		t.Fatalf("network = %v", nw)
	}
	c := h.fake.containers["flagfall-chal-web"]
	hc := c.body["HostConfig"].(map[string]any)
	if hc["NetworkMode"] != "flagfall-chal-web" || hc["NanoCpus"].(float64) != 0.5e9 || hc["Memory"].(float64) != 128<<20 ||
		hc["MemorySwap"] != hc["Memory"] || hc["PidsLimit"].(float64) != 256 || fmt.Sprint(hc["SecurityOpt"]) != "[no-new-privileges]" {
		t.Fatalf("host config = %v", hc)
	}
	if b := fmt.Sprint(hc["PortBindings"]); b != "map[8080/tcp:[map[HostIp:0.0.0.0 HostPort:]]]" {
		t.Fatalf("port bindings = %s", b)
	}
	if fmt.Sprint(c.body["Env"]) != "[MODE=prod]" {
		t.Fatalf("env = %v", c.body["Env"])
	}

	// A second pass changes nothing.
	id := c.id
	h.reconcile(t)
	if len(h.fake.builds) != 1 || h.fake.containers["flagfall-chal-web"].id != id {
		t.Fatal("a second pass rebuilt or replaced the container")
	}

	// A service that stops answering is unhealthy once its grace period is over.
	l.Close()
	if st = h.reconcile(t); st["web"].State != StateStarting {
		t.Fatalf("just started and not answering: %+v", st["web"])
	}
	h.now = h.now.Add(time.Minute)
	if st = h.reconcile(t); st["web"].State != StateUnhealthy || !strings.Contains(st["web"].Message, "Health check failed") {
		t.Fatalf("not answering: %+v", st["web"])
	}
}

func TestRunnerReplacesStopsAndCleansUp(t *testing.T) {
	h := newHarness(t)
	l, port := listen(t)
	defer l.Close()
	h.fake.hostPort = port

	// An image service is pulled, not built.
	if err := h.r.SetDesired(h.ctx, "echo", DesiredRunning); err != nil {
		t.Fatal(err)
	}
	if st := h.reconcile(t); st["echo"].State != StateRunning || len(h.fake.pulls) != 1 || h.fake.pulls[0] != "busybox:1.36" {
		t.Fatalf("echo = %+v, pulls %v", st["echo"], h.fake.pulls)
	}
	first := h.fake.containers["flagfall-chal-echo"].id

	// Restart replaces the container.
	if err := h.r.Restart(h.ctx, "echo"); err != nil {
		t.Fatal(err)
	}
	h.reconcile(t)
	second := h.fake.containers["flagfall-chal-echo"].id
	if second == first {
		t.Fatal("restart kept the old container")
	}

	// A changed spec replaces it too.
	h.importYML(t, webYML, strings.Replace(echoYML, "cpu: 0.25", "cpu: 1", 1))
	h.reconcile(t)
	c := h.fake.containers["flagfall-chal-echo"]
	if c.id == second || c.body["HostConfig"].(map[string]any)["NanoCpus"].(float64) != 1e9 {
		t.Fatalf("after a spec change: %s %v", c.id, c.body["HostConfig"])
	}

	// A container that keeps crashing is reported, not restarted forever.
	c.running, c.status, c.exitCode, c.restartCount = false, "exited", 3, maxRestarts
	if st := h.reconcile(t); st["echo"].State != StateFailed || !strings.Contains(st["echo"].Message, "exited with code 3") {
		t.Fatalf("crashed: %+v", st["echo"])
	}

	// Stop removes the container and its network.
	if err := h.r.SetDesired(h.ctx, "echo", DesiredStopped); err != nil {
		t.Fatal(err)
	}
	if st := h.reconcile(t); st["echo"].State != StateStopped || len(h.fake.containers) != 0 || len(h.fake.networks) != 0 {
		t.Fatalf("stopped: %+v, containers %v, networks %v", st["echo"], h.fake.containers, h.fake.networks)
	}

	// A container whose challenge is gone is removed.
	if err := h.r.SetDesired(h.ctx, "web", DesiredRunning); err != nil {
		t.Fatal(err)
	}
	h.reconcile(t)
	if _, err := h.db.DB.NewDelete().Model((*store.Challenge)(nil)).Where("slug = 'web'").Exec(h.ctx); err != nil {
		t.Fatal(err)
	}
	if st := h.reconcile(t); len(st) != 1 || len(h.fake.containers) != 0 {
		t.Fatalf("after delete: %+v, containers %v", st, h.fake.containers)
	}
}

func TestRunnerReportsBuildFailures(t *testing.T) {
	h := newHarness(t)
	h.fake.buildFail = "COPY failed: no such file"
	if err := h.r.SetDesired(h.ctx, "web", DesiredRunning); err != nil {
		t.Fatal(err)
	}
	st := h.reconcile(t)
	if st["web"].State != StateFailed || !strings.Contains(st["web"].Message, "COPY failed") {
		t.Fatalf("web = %+v", st["web"])
	}
}

func TestRunnerWithoutDocker(t *testing.T) {
	h := newHarness(t)
	r := New(Options{Store: h.db, Files: h.files})
	if r.Enabled() {
		t.Fatal("enabled without Docker")
	}
	if err := r.Reconcile(h.ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.SetDesired(h.ctx, "web", "sideways"); err == nil {
		t.Fatal("accepted a bad desired state")
	}
	h.importYML(t, strings.Replace(webYML, "service:\n  image: ./Dockerfile\n  port: 8080\n  memory: 128Mi\n  env: {MODE: prod}\n", "", 1))
	if err := r.SetDesired(h.ctx, "web", DesiredRunning); err != ErrNoService {
		t.Fatalf("no service: %v", err)
	}
}

func TestDockerHosts(t *testing.T) {
	for host, ok := range map[string]bool{
		"unix:///var/run/docker.sock": true,
		"tcp://127.0.0.1:2375":        true,
		"npipe:////./pipe/docker":     false,
		"tcp://":                      false,
	} {
		if _, err := NewDocker(host); (err == nil) != ok {
			t.Errorf("NewDocker(%q) = %v", host, err)
		}
	}
	if err := stream(strings.NewReader(`{"status":"ok"}` + "\n" + `{"error":"denied"}`)); err == nil || err.Error() != "denied" {
		t.Errorf("stream error = %v", err)
	}
}
