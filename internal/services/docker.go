package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Docker is a small client for the parts of the Docker Engine API the
// runner needs. Flagfall talks to the API directly rather than through the
// Docker SDK, which would add a large dependency tree for a dozen calls.
type Docker struct {
	http *http.Client
	base string // e.g. http://docker/v1.41
}

// apiVersion is the oldest API the runner needs (Docker Engine 20.10).
const apiVersion = "v1.41"

// NewDocker connects to host: unix:///var/run/docker.sock or
// tcp://127.0.0.1:2375. It doesn't dial until the first call.
func NewDocker(host string) (*Docker, error) {
	u, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("docker host %q: %w", host, err)
	}
	t := &http.Transport{MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second}
	base := ""
	switch u.Scheme {
	case "unix":
		sock := u.Path
		t.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		}
		base = "http://docker"
	case "tcp", "http":
		if u.Host == "" {
			return nil, fmt.Errorf("docker host %q has no address", host)
		}
		base = "http://" + u.Host
	default:
		return nil, fmt.Errorf("docker host %q: use unix:///path/to/docker.sock or tcp://host:port", host)
	}
	return &Docker{http: &http.Client{Transport: t}, base: base + "/" + apiVersion}, nil
}

// APIError is an error response from the Docker API.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("docker: %s (HTTP %d)", e.Message, e.Status) }

// IsNotFound reports whether err is a Docker 404.
func IsNotFound(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

func (d *Docker) request(ctx context.Context, method, path string, q url.Values, body io.Reader, contentType string) (*http.Response, error) {
	u := d.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := d.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker: %w", err)
	}
	if res.StatusCode >= 400 {
		defer res.Body.Close()
		var e struct {
			Message string `json:"message"`
		}
		b, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
		if json.Unmarshal(b, &e) != nil || e.Message == "" {
			e.Message = strings.TrimSpace(string(b))
		}
		return nil, &APIError{Status: res.StatusCode, Message: e.Message}
	}
	return res, nil
}

// call sends in as JSON (if not nil) and decodes the reply into out (if not nil).
func (d *Docker) call(ctx context.Context, method, path string, q url.Values, in, out any) error {
	var body io.Reader
	ct := ""
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body, ct = bytes.NewReader(b), "application/json"
	}
	res, err := d.request(ctx, method, path, q, body, ct)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// stream reads a progress stream (pull, build) and returns its first error.
func stream(r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var m struct {
			Error       string `json:"error"`
			ErrorDetail struct {
				Message string `json:"message"`
			} `json:"errorDetail"`
		}
		if json.Unmarshal(sc.Bytes(), &m) == nil && (m.Error != "" || m.ErrorDetail.Message != "") {
			if m.ErrorDetail.Message != "" {
				return errors.New(strings.TrimSpace(m.ErrorDetail.Message))
			}
			return errors.New(strings.TrimSpace(m.Error))
		}
	}
	return sc.Err()
}

// Ping checks that the Docker API answers.
func (d *Docker) Ping(ctx context.Context) error {
	return d.call(ctx, http.MethodGet, "/_ping", nil, nil, nil)
}

// ImageExists reports whether ref is present locally.
func (d *Docker) ImageExists(ctx context.Context, ref string) (bool, error) {
	err := d.call(ctx, http.MethodGet, "/images/"+ref+"/json", nil, nil, nil)
	if IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// Pull downloads ref.
func (d *Docker) Pull(ctx context.Context, ref string) error {
	name, tag := ref, ""
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") && !strings.Contains(ref, "@") {
		name, tag = ref[:i], ref[i+1:]
	}
	if tag == "" && !strings.Contains(ref, "@") {
		tag = "latest"
	}
	q := url.Values{"fromImage": {name}}
	if tag != "" {
		q.Set("tag", tag)
	}
	res, err := d.request(ctx, http.MethodPost, "/images/create", q, nil, "")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return stream(res.Body)
}

// Build builds an image from a tar build context and tags it.
func (d *Docker) Build(ctx context.Context, buildContext io.Reader, dockerfile, tag string, labels map[string]string) error {
	lb, _ := json.Marshal(labels)
	q := url.Values{"t": {tag}, "dockerfile": {dockerfile}, "rm": {"1"}, "forcerm": {"1"}, "labels": {string(lb)}}
	res, err := d.request(ctx, http.MethodPost, "/build", q, buildContext, "application/x-tar")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return stream(res.Body)
}

// Container is one entry from ContainerList.
type Container struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Labels map[string]string `json:"Labels"`
	State  string            `json:"State"`
}

// ContainerList lists all containers (running or not) with the label.
func (d *Docker) ContainerList(ctx context.Context, label string) ([]Container, error) {
	f, _ := json.Marshal(map[string][]string{"label": {label}})
	var out []Container
	err := d.call(ctx, http.MethodGet, "/containers/json", url.Values{"all": {"1"}, "filters": {string(f)}}, nil, &out)
	return out, err
}

// ContainerCreate creates a container from a create body (see runner.go).
func (d *Docker) ContainerCreate(ctx context.Context, name string, body any) (string, error) {
	var out struct {
		ID string `json:"Id"`
	}
	err := d.call(ctx, http.MethodPost, "/containers/create", url.Values{"name": {name}}, body, &out)
	return out.ID, err
}

// ContainerStart starts a container; starting a running one is fine.
func (d *Docker) ContainerStart(ctx context.Context, id string) error {
	err := d.call(ctx, http.MethodPost, "/containers/"+id+"/start", nil, nil, nil)
	var e *APIError
	if errors.As(err, &e) && e.Status == http.StatusNotModified {
		return nil
	}
	return err
}

// ContainerRemove stops (3 s grace) and removes a container. Challenge
// containers keep no state, and many ignore SIGTERM as PID 1, so a long
// grace would only hold up every other service.
func (d *Docker) ContainerRemove(ctx context.Context, id string) error {
	err := d.call(ctx, http.MethodPost, "/containers/"+id+"/stop", url.Values{"t": {"3"}}, nil, nil)
	if err != nil && !IsNotFound(err) {
		var e *APIError
		if !errors.As(err, &e) || e.Status != http.StatusNotModified {
			return err
		}
	}
	err = d.call(ctx, http.MethodDelete, "/containers/"+id, url.Values{"force": {"1"}, "v": {"1"}}, nil, nil)
	if IsNotFound(err) {
		return nil
	}
	return err
}

// Inspect is the part of a container inspect the runner reads.
type Inspect struct {
	ID    string `json:"Id"`
	State struct {
		Status     string    `json:"Status"` // created, running, restarting, exited, dead
		Running    bool      `json:"Running"`
		Restarting bool      `json:"Restarting"`
		OOMKilled  bool      `json:"OOMKilled"`
		ExitCode   int       `json:"ExitCode"`
		Error      string    `json:"Error"`
		StartedAt  time.Time `json:"StartedAt"`
	} `json:"State"`
	RestartCount    int `json:"RestartCount"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
	} `json:"NetworkSettings"`
}

// ContainerInspect returns a container's state and published ports.
func (d *Docker) ContainerInspect(ctx context.Context, id string) (*Inspect, error) {
	out := new(Inspect)
	err := d.call(ctx, http.MethodGet, "/containers/"+id+"/json", nil, nil, out)
	return out, err
}

// NetworkEnsure creates a bridge network if there isn't one with that name.
// Containers on it can't talk to each other (icc off), and it's separate
// from Flagfall's own network, so a challenge can't reach the database.
func (d *Docker) NetworkEnsure(ctx context.Context, name string, labels map[string]string) error {
	err := d.call(ctx, http.MethodGet, "/networks/"+name, nil, nil, nil)
	if err == nil || !IsNotFound(err) {
		return err
	}
	body := map[string]any{
		"Name": name, "Driver": "bridge", "CheckDuplicate": true, "Labels": labels,
		"Options": map[string]string{"com.docker.network.bridge.enable_icc": "false"},
	}
	return d.call(ctx, http.MethodPost, "/networks/create", nil, body, nil)
}

// NetworkRemove removes a network; a missing one is fine.
func (d *Docker) NetworkRemove(ctx context.Context, name string) error {
	err := d.call(ctx, http.MethodDelete, "/networks/"+name, nil, nil, nil)
	if IsNotFound(err) {
		return nil
	}
	return err
}
