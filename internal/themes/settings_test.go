package themes_test

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/go-chi/chi/v5"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Kamote-Lab-Studio/flagfall/internal/store"
	"github.com/Kamote-Lab-Studio/flagfall/internal/store/storetest"
	"github.com/Kamote-Lab-Studio/flagfall/internal/themes"
)

func TestManifestValidation(t *testing.T) {
	bad := map[string]string{
		"id":             `"id":"Demo!"`,
		"version":        `"version":"one"`,
		"missing page":   `"pages":{"board":"/"}`,
		"dup key":        `"settings":[{"key":"a","type":"text","label":"A","default":""},{"key":"a","type":"text","label":"B","default":""}]`,
		"no label":       `"settings":[{"key":"a","type":"text","default":""}]`,
		"bad color":      `"settings":[{"key":"a","type":"color","label":"A","default":"red"}]`,
		"bad type":       `"settings":[{"key":"a","type":"html","label":"A","default":""}]`,
		"select default": `"settings":[{"key":"a","type":"select","label":"A","default":"z","options":[{"value":"x","label":"X"},{"value":"y","label":"Y"}]}]`,
		"contrast":       `"settings":[{"key":"a","type":"color","label":"A","default":"#000000","contrastWith":"nope"}]`,
		"image cssVar":   `"settings":[{"key":"a","type":"image","label":"A","cssVar":"--a"}]`,
		"font name":      `"settings":[{"key":"a","type":"font","label":"A","default":"x;}","options":["x;}"]}]`,
		"entry":          `"entry":"index.html"`,
	}
	base := map[string]any{}
	if err := json.Unmarshal([]byte(manifest), &base); err != nil {
		t.Fatal(err)
	}
	for name, override := range bad {
		m := map[string]any{}
		for k, v := range base {
			m[k] = v
		}
		var o map[string]any
		if err := json.Unmarshal([]byte("{"+override+"}"), &o); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for k, v := range o {
			m[k] = v
		}
		raw, _ := json.Marshal(m)
		if _, err := themes.Load(fstest.MapFS{"theme.json": {Data: raw}}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := themes.Load(fstest.MapFS{"theme.json": {Data: []byte(manifest)}}); err != nil {
		t.Fatalf("valid manifest: %v", err)
	}
}

func TestContrastRatio(t *testing.T) {
	r, ok := themes.ContrastRatio("#000000", "#FFFFFF")
	if !ok || math.Abs(r-21) > 0.01 {
		t.Fatalf("black/white = %v", r)
	}
	if r, _ := themes.ContrastRatio("#777777", "#FFFFFF"); r > 4.5 || r < 4.4 {
		t.Fatalf("#777/white = %v, want just under 4.5", r)
	}
	if _, ok := themes.ContrastRatio("red", "#FFFFFF"); ok {
		t.Fatal("named colors aren't accepted")
	}
}

func TestCheckValues(t *testing.T) {
	theme, err := themes.Load(fstest.MapFS{"theme.json": {Data: []byte(manifest)}})
	if err != nil {
		t.Fatal(err)
	}
	s := theme.Manifest.Settings

	clean, bad := themes.CheckValues(s, map[string]any{"accent": "#FFD27A", "title": "Hi", "unknown": 1}, nil)
	if len(bad) != 0 || clean["accent"] != "#FFD27A" || clean["title"] != "Hi" || clean["unknown"] != nil {
		t.Fatalf("clean=%v bad=%v", clean, bad)
	}

	// Accent too close to the default background.
	_, bad = themes.CheckValues(s, map[string]any{"accent": "#101830"}, nil)
	if !strings.Contains(bad["accent"], "Contrast with Background") {
		t.Fatalf("contrast: %v", bad)
	}
	// ...or to a new background saved in the same request.
	_, bad = themes.CheckValues(s, map[string]any{"background": "#FFB000"}, nil)
	if bad["accent"] == "" {
		t.Fatalf("contrast against new background: %v", bad)
	}

	_, bad = themes.CheckValues(s, map[string]any{"accent": "red", "title": 3, "font": "a;b"}, nil)
	if bad["accent"] == "" || bad["title"] == "" || bad["font"] == "" {
		t.Fatalf("types: %v", bad)
	}
}

func TestCustomCSS(t *testing.T) {
	for css, ok := range map[string]bool{
		".x { color: red }":         true,
		"</style><script>":          false,
		"@IMPORT url(x.css);":       false,
		strings.Repeat("a", 40_001): false,
	} {
		if got := themes.CheckCustomCSS(css) == ""; got != ok {
			t.Errorf("%.30q: ok=%v, want %v", css, got, ok)
		}
	}
}

func demoTheme(t *testing.T, id string) *themes.Theme {
	t.Helper()
	m := strings.Replace(manifest, `"id":"demo"`, `"id":"`+id+`"`, 1)
	files := builtFS()
	files["theme.json"] = &fstest.MapFile{Data: []byte(m)}
	files["dist/index.html"] = &fstest.MapFile{Data: []byte("<html><head><title>" + id + "</title></head></html>")}
	theme, err := themes.Load(files)
	if err != nil {
		t.Fatal(err)
	}
	return theme
}

func TestManagerPicksLivePreviewAndFallback(t *testing.T) {
	db := storetest.New(t)
	def, other := demoTheme(t, "amber-phosphor"), demoTheme(t, "neon")
	for _, th := range []*themes.Theme{def, other} {
		if err := th.Register(t.Context(), db, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	reg := themes.NewRegistry(def, other)
	m := themes.NewManager(reg, db, nil)
	previewing := ""
	m.PreviewTheme = func(*http.Request) string { return previewing }
	req := httptest.NewRequest("GET", "/", nil)

	// Nothing published: the default theme.
	if v := m.Current(req); v.Theme != def || v.Preview || v.Version != 0 {
		t.Fatalf("nothing published: %+v", v)
	}

	now := time.Now().UTC()
	insert := func(row store.ThemeSettings) {
		t.Helper()
		if _, err := db.DB.NewInsert().Model(&row).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	insert(store.ThemeSettings{ThemeID: "neon", Version: 1, Status: "live", Data: `{"title":"live"}`, PublishedAt: &now})
	insert(store.ThemeSettings{ThemeID: "neon", Version: 2, Status: "draft", Data: `{"title":"draft"}`})

	if v := m.Current(req); v.Theme != other || v.Values["title"] != "live" || v.Preview {
		t.Fatalf("live: %+v", v)
	}

	// An admin previewing sees the draft; others still see live.
	previewing = "neon"
	if v := m.Current(req); v.Theme != other || v.Values["title"] != "draft" || !v.Preview || v.Version != 2 {
		t.Fatalf("preview: %+v", v)
	}
	// Previewing a theme with no saved settings starts from its defaults.
	previewing = "amber-phosphor"
	if v := m.Current(req); v.Theme != def || len(v.Values) != 0 || !v.Preview {
		t.Fatalf("preview default: %+v", v)
	}
	// Previewing something not installed falls back to live.
	previewing = "gone"
	if v := m.Current(req); v.Theme != other || v.Preview {
		t.Fatalf("preview missing: %+v", v)
	}
	previewing = ""

	// The published theme is uninstalled: players get the default.
	reg.Remove("neon")
	if v := m.Current(req); v.Theme != def || !v.Fallback {
		t.Fatalf("fallback: %+v", v)
	}
}

func TestPreviewPageIsNotCached(t *testing.T) {
	theme := demoTheme(t, "amber-phosphor")
	m := themes.NewManager(themes.NewRegistry(theme), nil, nil)
	m.PreviewTheme = func(*http.Request) string { return "amber-phosphor" }
	srv := newServerWith(t, themes.New(m))
	res, _ := get(t, srv.URL+"/scoreboard")
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", res.Header.Get("Cache-Control"))
	}
}

func TestCustomCSSIsInjected(t *testing.T) {
	db := storetest.New(t)
	theme := demoTheme(t, "amber-phosphor")
	if err := theme.Register(t.Context(), db, time.Now()); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, row := range []store.ThemeSettings{
		{ThemeID: "amber-phosphor", Version: 1, Status: "live", CustomCSS: ".x{color:red}", PublishedAt: &now},
	} {
		if _, err := db.DB.NewInsert().Model(&row).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	srv := newServerWith(t, handler(db, theme))
	_, body := get(t, srv.URL+"/")
	if !strings.Contains(body, `<style id="flagfall-custom-css">.x{color:red}</style>`) {
		t.Fatalf("custom CSS missing: %q", body)
	}
}

func newServerWith(t *testing.T, h *themes.Handler) *httptest.Server {
	t.Helper()
	r := chi.NewRouter()
	h.Mount(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}
