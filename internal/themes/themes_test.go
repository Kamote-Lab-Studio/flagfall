package themes_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/flagfall/flagfall/internal/store"
	"github.com/flagfall/flagfall/internal/store/storetest"
	"github.com/flagfall/flagfall/internal/themes"
	amberphosphor "github.com/flagfall/flagfall/themes/amber-phosphor"
)

const manifest = `{"id":"demo","name":"Demo","version":"1.0.0","entry":"dist/index.html","settings":[
 {"key":"accent","type":"color","default":"#FFB000","cssVar":"--amber"},
 {"key":"font","type":"text","default":"x","cssVar":"--font"},
 {"key":"title","type":"text","default":"x"}]}`

func newServer(t *testing.T, fsys fstest.MapFS, db *store.Store) *httptest.Server {
	t.Helper()
	theme, err := themes.Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Get("/api/v1/event", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "api") })
	themes.New(theme, db, nil).Mount(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func builtFS() fstest.MapFS {
	return fstest.MapFS{
		"theme.json":           {Data: []byte(manifest)},
		"dist/index.html":      {Data: []byte("<!doctype html><html><head><title>x</title></head><body></body></html>")},
		"dist/assets/app-1.js": {Data: []byte("console.log(1)")},
		"dist/favicon.svg":     {Data: []byte("<svg/>")},
	}
}

func TestPagesFallBackToIndex(t *testing.T) {
	srv := newServer(t, builtFS(), storetest.New(t))
	for _, p := range []string{"/", "/scoreboard", "/teams/42", "/challenges/baby-rev"} {
		res, body := get(t, srv.URL+p)
		if res.StatusCode != 200 || !strings.Contains(body, "<title>x</title>") {
			t.Fatalf("%s: %d %q", p, res.StatusCode, body)
		}
		if res.Header.Get("Content-Security-Policy") != themes.CSP {
			t.Fatalf("%s: missing CSP", p)
		}
		if strings.Contains(body, "flagfall-settings") {
			t.Fatalf("%s: no settings are published, so no style block expected", p)
		}
	}
	// The API keeps its own routes; unknown API paths are 404s, not the SPA.
	if _, body := get(t, srv.URL+"/api/v1/event"); body != "api" {
		t.Fatalf("api route shadowed: %q", body)
	}
	for _, p := range []string{"/api/v1/nope", "/files/abc/x", "/admin"} {
		if res, _ := get(t, srv.URL+p); res.StatusCode != 404 {
			t.Fatalf("%s: got %d, want 404", p, res.StatusCode)
		}
	}
}

func TestAssets(t *testing.T) {
	srv := newServer(t, builtFS(), nil)
	res, body := get(t, srv.URL+"/theme/demo/assets/app-1.js")
	if res.StatusCode != 200 || body != "console.log(1)" || !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d %q %q", res.StatusCode, body, res.Header.Get("Cache-Control"))
	}
	if res, _ := get(t, srv.URL+"/theme/demo/favicon.svg"); res.StatusCode != 200 || res.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("favicon: %d %q", res.StatusCode, res.Header.Get("Cache-Control"))
	}
	for _, p := range []string{"/theme/demo/missing.js", "/theme/demo/assets/", "/theme/demo/../theme.json"} {
		if res, _ := get(t, srv.URL+p); res.StatusCode != 404 {
			t.Fatalf("%s: got %d, want 404", p, res.StatusCode)
		}
	}
}

func TestPublishedSettingsAreInjected(t *testing.T) {
	db := storetest.New(t)
	theme, err := themes.Load(builtFS())
	if err != nil {
		t.Fatal(err)
	}
	// Registering twice must work (it runs on every start).
	for range 2 {
		if err := theme.Register(t.Context(), db, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.DB.NewInsert().Model(&store.ThemeSettings{
		ThemeID: "demo", Version: 1, Status: "live",
		Data: `{"accent":"#00FF88","font":"</style><script>alert(1)</script>","title":"hello"}`,
	}).Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	srv := newServer(t, builtFS(), db)
	_, body := get(t, srv.URL+"/")
	want := `<style id="flagfall-settings">:root { --amber: #00FF88; }</style></head>`
	if !strings.Contains(body, want) {
		t.Fatalf("want %q in %q", want, body)
	}
	if strings.Contains(body, "alert") {
		t.Fatal("unsafe value was injected")
	}
}

func TestCSSVarsRejectsUnsafeValues(t *testing.T) {
	settings := []themes.Setting{
		{Key: "c", Type: "color", CSSVar: "--c"},
		{Key: "f", Type: "text", CSSVar: "--f"},
		{Key: "bad", Type: "text", CSSVar: "--x; } body {"},
	}
	cases := []struct {
		values map[string]any
		want   string
	}{
		{map[string]any{"c": "#abc"}, ":root { --c: #abc; }"},
		{map[string]any{"c": "red"}, ""},
		{map[string]any{"c": "#abc;}"}, ""},
		{map[string]any{"f": `"IBM Plex Sans", sans-serif`}, `:root { --f: "IBM Plex Sans", sans-serif; }`},
		{map[string]any{"f": `"unclosed`}, ""},
		{map[string]any{"f": "x; color: red"}, ""},
		{map[string]any{"f": "url(http://evil)"}, ""},
		{map[string]any{"bad": "1"}, ""},
		{map[string]any{"f": 12}, ""},
	}
	for _, c := range cases {
		if got := themes.CSSVars(settings, c.values); got != c.want {
			t.Errorf("%v: got %q, want %q", c.values, got, c.want)
		}
	}
}

func TestUnbuiltTheme(t *testing.T) {
	srv := newServer(t, fstest.MapFS{"theme.json": {Data: []byte(manifest)}}, nil)
	res, body := get(t, srv.URL+"/")
	if res.StatusCode != http.StatusServiceUnavailable || !strings.Contains(body, "make web-build") {
		t.Fatalf("got %d %q", res.StatusCode, body)
	}
	if res, _ := get(t, srv.URL+"/theme/demo/assets/app-1.js"); res.StatusCode != 404 {
		t.Fatalf("asset on unbuilt theme: %d", res.StatusCode)
	}
}

func TestBundledThemeManifestLoads(t *testing.T) {
	theme, err := themes.Load(amberphosphor.FS)
	if err != nil {
		t.Fatal(err)
	}
	if theme.Manifest.ID != "amber-phosphor" || len(theme.Manifest.Settings) == 0 {
		t.Fatalf("unexpected manifest: %+v", theme.Manifest)
	}
}
