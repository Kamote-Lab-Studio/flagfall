package challenges_test

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/flagfall/flagfall/internal/blobs"
	"github.com/flagfall/flagfall/internal/challenges"
	"github.com/flagfall/flagfall/internal/store"
	"github.com/flagfall/flagfall/internal/store/storetest"
)

const serviceYML = `slug: web
name: Web
category: Web
description: d.md
value: 100
flags: [{type: exact, value: "flag{web}"}]
connection: "http://{{event_domain}}:31000"
service:
%s
`

func serviceFolder(block string) fstest.MapFS {
	return fstest.MapFS{
		"web/challenge.yml": {Data: []byte(strings.Replace(serviceYML, "%s", block, 1))},
		"web/d.md":          {Data: []byte("A web challenge.")},
		"web/Dockerfile":    {Data: []byte("FROM busybox\nCOPY run.sh /run.sh\nCMD [\"/run.sh\"]\n")},
		"web/run.sh":        {Data: []byte("#!/bin/sh\nexec httpd -f -p 8080\n"), Mode: 0o755},
	}
}

func TestServiceChecks(t *testing.T) {
	cases := map[string]string{
		"  port: 8080":                                      "image is required",
		"  image: busybox\n  port: 0":                       "port must be between",
		"  image: busybox\n  port: 80\n  memory: 2GB":       "memory must look like 256Mi",
		"  image: busybox\n  port: 80\n  memory: 9Gi":       "memory must be between",
		"  image: busybox\n  port: 80\n  cpu: 16":           "cpu must be more than 0",
		"  image: busybox\n  port: 80\n  healthcheck: http": "healthcheck must be tcp or an HTTP path",
		"  image: ./missing/Dockerfile\n  port: 80":         "isn't in the challenge folder",
		"  image: ../Dockerfile\n  port: 80":                "must be inside the challenge folder",
		"  image: Bad Image!\n  port: 80":                   "isn't a valid image name",
		"  image: busybox\n  port: 80\n  env: {1X: y}":      "isn't a valid variable name",
		"  image: busybox\n  port: 80\n  ports: 1":          "field ports not found",
	}
	for block, want := range cases {
		_, err := challenges.Load(serviceFolder(block), ".")
		if err == nil {
			t.Errorf("%q: loaded, want %q", block, want)
			continue
		}
		if got := err.Error(); !strings.Contains(got, want) {
			t.Errorf("%q: %s, want %q", block, got, want)
		}
	}
}

func TestServiceImportExportRoundTrip(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	files := blobs.Local(t.TempDir())

	folder := serviceFolder("  image: ./Dockerfile\n  port: 8080\n  publish: 31000\n  memory: 1Gi\n  healthcheck: /\n  env: {MODE: prod}")
	specs, err := challenges.Load(folder, ".")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := challenges.Apply(ctx, db, secret, files, specs); err != nil {
		t.Fatal(err)
	}
	c := new(store.Challenge)
	if err := db.DB.NewSelect().Model(c).Where("slug = 'web'").Scan(ctx); err != nil {
		t.Fatal(err)
	}
	spec, err := challenges.ParseService(c.Service)
	if err != nil || spec == nil {
		t.Fatalf("stored service = %v, %v", c.Service, err)
	}
	if !spec.Build() || spec.Dockerfile != "Dockerfile" || spec.Port != 8080 || spec.Publish != 31000 ||
		spec.CPU != challenges.DefaultServiceCPU || spec.MemoryMB != 1024 || spec.Healthcheck != "/" || spec.Env["MODE"] != "prod" {
		t.Fatalf("spec = %+v", spec)
	}

	// The build context is stored, leaves out challenge.yml (it has the
	// flags), and keeps the executable bit.
	rc, err := files.Open(ctx, spec.Context)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	modes := map[string]int64{}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		modes[h.Name] = h.Mode
	}
	names := make([]string, 0, len(modes))
	for n := range modes {
		names = append(names, n)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"Dockerfile", "d.md", "run.sh"}) || modes["run.sh"] != 0o755 || modes["Dockerfile"] != 0o644 {
		t.Fatalf("build context = %v", modes)
	}
	if bytes.Contains(data, []byte("flag{web}")) {
		t.Fatal("the build context contains the flag")
	}

	// Export writes the service and its build files back. The folder isn't
	// byte-for-byte the same (description.md is added, and Windows has no
	// executable bit), so only the settings are compared.
	out := filepath.Join(t.TempDir(), "export")
	if _, err := challenges.Export(ctx, db, files, out); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(out, "web", "run.sh")); err != nil || !strings.Contains(string(b), "httpd") {
		t.Fatalf("exported run.sh = %q, %v", b, err)
	}
	yml, _ := os.ReadFile(filepath.Join(out, "web", "challenge.yml"))
	for _, want := range []string{"image: ./Dockerfile", "memory: 1Gi", "publish: 31000"} {
		if !strings.Contains(string(yml), want) {
			t.Errorf("exported challenge.yml lacks %q:\n%s", want, yml)
		}
	}
	// The exported flag is redacted; put the real one back to compare.
	fixed := strings.Replace(string(yml), challenges.RedactedFlag, "flag{web}", 1)
	if err := os.WriteFile(filepath.Join(out, "web", "challenge.yml"), []byte(fixed), 0o644); err != nil {
		t.Fatal(err)
	}
	back, err := challenges.LoadFolder(os.DirFS(filepath.Join(out, "web")))
	if err != nil {
		t.Fatal(err)
	}
	db2 := storetest.New(t)
	if _, err := challenges.Apply(ctx, db2, secret, files, back); err != nil {
		t.Fatal(err)
	}
	c2 := new(store.Challenge)
	_ = db2.DB.NewSelect().Model(c2).Where("slug = 'web'").Scan(ctx)
	spec2, _ := challenges.ParseService(c2.Service)
	if spec2 == nil || !spec2.Build() || spec2.Dockerfile != spec.Dockerfile {
		t.Fatalf("re-imported spec = %+v", spec2)
	}
	spec2.Context, spec.Context = "", ""
	if spec2.Hash() != spec.Hash() {
		t.Fatalf("re-imported spec = %+v, want %+v", spec2, spec)
	}

	// A changed service is named in the plan.
	folder["web/run.sh"] = &fstest.MapFile{Data: []byte("#!/bin/sh\nexec httpd -f -p 9090\n"), Mode: fs.FileMode(0o755)}
	specs, _ = challenges.Load(folder, ".")
	plan, _ := challenges.MakePlan(ctx, db, secret, specs)
	if len(plan.Changed) != 1 || !slices.Equal(plan.Changed[0].What, []string{"service"}) {
		t.Fatalf("changed = %+v", plan.Changed)
	}
}

func TestServiceWithImageNeedsNoBuild(t *testing.T) {
	specs, err := challenges.Load(serviceFolder("  image: nginx:1.27-alpine\n  port: 80"), ".")
	if err != nil {
		t.Fatal(err)
	}
	db := storetest.New(t)
	ctx := context.Background()
	if _, err := challenges.Apply(ctx, db, secret, blobs.Local(t.TempDir()), specs); err != nil {
		t.Fatal(err)
	}
	c := new(store.Challenge)
	_ = db.DB.NewSelect().Model(c).Where("slug = 'web'").Scan(ctx)
	spec, _ := challenges.ParseService(c.Service)
	if spec == nil || spec.Build() || spec.Image != "nginx:1.27-alpine" || spec.MemoryMB != challenges.DefaultServiceMemoryMB || spec.Healthcheck != "tcp" {
		t.Fatalf("spec = %+v", spec)
	}
}
