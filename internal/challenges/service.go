package challenges

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ServiceYAML is the `service:` block as written in challenge.yml.
type ServiceYAML struct {
	// Image is an image reference (nginx:1.27) or a path to a Dockerfile in
	// the challenge folder (./Dockerfile).
	Image       string            `yaml:"image"`
	Port        int               `yaml:"port"`
	Publish     int               `yaml:"publish,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	CPU         float64           `yaml:"cpu,omitempty"`
	Memory      string            `yaml:"memory,omitempty"`
	Healthcheck string            `yaml:"healthcheck,omitempty"`
}

// ServiceSpec is the checked service, stored as JSON in challenges.service.
// Exactly one of Image and Context is set.
type ServiceSpec struct {
	Image string `json:"image,omitempty"`
	// Context is the SHA-256 of the build context (a tar of the challenge
	// folder) in file storage, and Dockerfile its path inside the tar.
	Context    string            `json:"context,omitempty"`
	Dockerfile string            `json:"dockerfile,omitempty"`
	Port       int               `json:"port"`
	Publish    int               `json:"publish,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	CPU        float64           `json:"cpu"`
	MemoryMB   int               `json:"memoryMB"`
	// Healthcheck is "tcp" or an HTTP path such as /health.
	Healthcheck string `json:"healthcheck"`
}

// Service limits and defaults (docs/dev/challenge-format.md).
const (
	DefaultServiceCPU      = 0.5
	DefaultServiceMemoryMB = 256
	MaxServiceCPU          = 4
	MaxServiceMemoryMB     = 8192
	// MaxBuildContext is the largest challenge folder sent to a Docker build.
	MaxBuildContext = 50 << 20
)

var (
	memoryRE  = regexp.MustCompile(`^([0-9]+)(Mi|Gi)$`)
	envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	// imageRE is a loose image reference check: name[:tag][@digest].
	imageRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._/:@-]*$`)
)

// ParseService parses a stored service spec; it returns nil for none.
func ParseService(raw *string) (*ServiceSpec, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	s := new(ServiceSpec)
	if err := json.Unmarshal([]byte(*raw), s); err != nil {
		return nil, fmt.Errorf("stored service spec: %w", err)
	}
	return s, nil
}

// Hash identifies the spec, so a changed spec replaces the container.
func (s *ServiceSpec) Hash() string {
	b, _ := json.Marshal(s)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Build reports whether the image is built from the challenge folder.
func (s *ServiceSpec) Build() bool { return s.Context != "" }

// isDockerfilePath reports whether image names a file rather than an image.
func isDockerfilePath(image string) bool {
	return strings.HasPrefix(image, "./") || path.Base(image) == "Dockerfile" || strings.HasSuffix(image, ".Dockerfile")
}

// checkService validates the service block and, for a Dockerfile, packs the
// challenge folder as the build context. bad reports problems.
func checkService(fsys fs.FS, dir string, y *ServiceYAML, bad func(string, ...any)) (*ServiceSpec, []byte) {
	s := &ServiceSpec{Port: y.Port, Publish: y.Publish, Env: y.Env, CPU: y.CPU, Healthcheck: y.Healthcheck}
	ok := true
	fail := func(format string, args ...any) { bad("service: "+format, args...); ok = false }

	if y.Port < 1 || y.Port > 65535 {
		fail("port must be between 1 and 65535")
	}
	if y.Publish < 0 || y.Publish > 65535 {
		fail("publish must be between 1 and 65535 (or left out for any free port)")
	}
	if s.CPU == 0 {
		s.CPU = DefaultServiceCPU
	}
	if s.CPU < 0 || s.CPU > MaxServiceCPU {
		fail("cpu must be more than 0 and at most %d", MaxServiceCPU)
	}
	s.MemoryMB = DefaultServiceMemoryMB
	if y.Memory != "" {
		m := memoryRE.FindStringSubmatch(y.Memory)
		if m == nil {
			fail("memory must look like 256Mi or 1Gi")
		} else {
			n, _ := strconv.Atoi(m[1])
			if m[2] == "Gi" {
				n *= 1024
			}
			s.MemoryMB = n
			if n < 16 || n > MaxServiceMemoryMB {
				fail("memory must be between 16Mi and %dGi", MaxServiceMemoryMB/1024)
			}
		}
	}
	switch {
	case s.Healthcheck == "":
		s.Healthcheck = "tcp"
	case s.Healthcheck == "tcp", strings.HasPrefix(s.Healthcheck, "/"):
	default:
		fail("healthcheck must be tcp or an HTTP path starting with /")
	}
	for k := range s.Env {
		if !envNameRE.MatchString(k) {
			fail("env: %q isn't a valid variable name", k)
		}
	}
	if len(s.Env) == 0 {
		s.Env = nil
	}

	var context []byte
	switch image := strings.TrimSpace(y.Image); {
	case image == "":
		fail("image is required: an image name or ./Dockerfile")
	case isDockerfilePath(image):
		clean := path.Clean(image)
		if path.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			fail("image: %q must be inside the challenge folder", image)
			break
		}
		if _, err := fs.Stat(fsys, path.Join(dir, clean)); err != nil {
			fail("image: %q isn't in the challenge folder", image)
			break
		}
		var err error
		context, err = BuildContext(fsys, dir)
		if err != nil {
			fail("%v", err)
			break
		}
		sum := sha256.Sum256(context)
		s.Context, s.Dockerfile = hex.EncodeToString(sum[:]), clean
	case !imageRE.MatchString(image):
		fail("image: %q isn't a valid image name", image)
	default:
		s.Image = image
	}
	if !ok {
		return nil, nil
	}
	return s, context
}

// BuildContext packs the challenge folder as a tar for `docker build`. The
// tar is the same for the same files (sorted, no times or owners), so
// re-importing an unchanged folder doesn't rebuild the image.
//
// challenge.yml is left out: it holds the flags, and a `COPY . .` would
// otherwise put them in the image.
func BuildContext(fsys fs.FS, dir string) ([]byte, error) {
	var names []string
	exec := map[string]bool{}
	err := fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("the challenge folder has a symlink (%s); Docker builds can't use it", p)
		}
		if d.IsDir() || p == path.Join(dir, "challenge.yml") {
			return nil
		}
		names = append(names, p)
		if info, err := d.Info(); err == nil && info.Mode().Perm()&0o111 != 0 {
			exec[p] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(names)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, p := range names {
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, err
		}
		if buf.Len()+len(data) > MaxBuildContext {
			return nil, fmt.Errorf("the challenge folder is larger than %d MB, too big to build", MaxBuildContext>>20)
		}
		rel := p
		if dir != "." {
			rel = strings.TrimPrefix(p, dir+"/")
		}
		mode := int64(0o644)
		if exec[p] {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Name: rel, Mode: mode, Size: int64(len(data)), Typeflag: tar.TypeReg, Format: tar.FormatPAX}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// toYAML turns a stored spec back into the challenge.yml block, for export.
func (s *ServiceSpec) toYAML() *ServiceYAML {
	y := &ServiceYAML{Image: s.Image, Port: s.Port, Publish: s.Publish, Env: s.Env, CPU: s.CPU, Healthcheck: s.Healthcheck}
	if s.Build() {
		y.Image = "./" + s.Dockerfile
	}
	if s.MemoryMB%1024 == 0 {
		y.Memory = strconv.Itoa(s.MemoryMB/1024) + "Gi"
	} else {
		y.Memory = strconv.Itoa(s.MemoryMB) + "Mi"
	}
	return y
}
