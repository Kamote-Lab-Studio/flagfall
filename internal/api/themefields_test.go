package api_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/flagfall/flagfall/internal/api"
	"github.com/flagfall/flagfall/internal/themes"
)

// mapEnv is an env whose active theme wants a location per challenge.
func mapEnv(t *testing.T) *env {
	t.Helper()
	theme, err := themes.Load(fstest.MapFS{"theme.json": {Data: []byte(`{"id":"world-map","name":"World Map","version":"1.0.0",
		"entry":"dist/index.html","challengeFields":[
		{"key":"location","type":"location","label":"Map location","help":"Where it sits on the map.","required":true},
		{"key":"region","type":"select","label":"Region","options":[{"value":"eu","label":"Europe"},{"value":"asia","label":"Asia"}]}]}`)}})
	if err != nil {
		t.Fatal(err)
	}
	return makeEnv(t, true, func(o *api.Options) { o.Theme = func() *themes.Theme { return theme } })
}

func TestThemeFields(t *testing.T) {
	e := mapEnv(t)
	author := e.staff("ann", "author")
	support := e.staff("sam", "support")

	f := support.must(200, "GET", "/admin/api/v1/challenge-fields", nil)
	if f.str("theme", "name") != "World Map" || len(f.body["fields"].([]any)) != 2 {
		t.Fatalf("fields = %s", f.raw)
	}

	ch := e.addChallenge("cairo", 100, "flag{c}")
	id := strings.Trim(jsonID(ch.ID), `"`)
	path := "/admin/api/v1/challenges/" + id

	// Missing the required location: saved, but warned about on the
	// challenge and the dashboard.
	d := author.must(200, "GET", path, nil)
	if w := d.body["themeFieldWarnings"].([]any); len(w) != 1 || !strings.Contains(w[0].(string), "Map location") {
		t.Fatalf("warnings = %s", d.raw)
	}
	if dash := author.must(200, "GET", "/admin/api/v1/dashboard", nil); !strings.Contains(string(dash.raw), "CAIRO is missing Map location, which the World Map theme needs") {
		t.Fatalf("dashboard = %s", dash.raw)
	}

	// Wrong types are refused, per field.
	r := author.must(400, "PATCH", path, map[string]any{"themeFields": map[string]any{"location": map[string]any{"lat": 200, "lon": 0}, "region": "mars"}})
	if r.str("error", "fields", "themeFields.location") == "" || r.str("error", "fields", "themeFields.region") == "" {
		t.Fatalf("errors = %s", r.raw)
	}

	// Valid values are saved; keys for other themes are kept, not warned about.
	d = author.must(200, "PATCH", path, map[string]any{"themeFields": map[string]any{
		"location": map[string]any{"city": "Cairo", "lat": 30.04, "lon": 31.24}, "region": "eu", "tower_floor": 3,
	}})
	if len(d.body["themeFieldWarnings"].([]any)) != 0 || d.body["otherThemeFields"].([]any)[0] != "tower_floor" {
		t.Fatalf("after save = %s", d.raw)
	}
	if dash := author.must(200, "GET", "/admin/api/v1/dashboard", nil); strings.Contains(string(dash.raw), "missing Map location") {
		t.Fatalf("dashboard still warns: %s", dash.raw)
	}

	// Players get the values to draw.
	p := e.signUp("pat")
	p.createTeam("pats")
	if v := p.must(200, "GET", "/api/v1/challenges/cairo", nil); v.str("themeFields", "location", "city") != "Cairo" {
		t.Fatalf("player view = %s", v.raw)
	}
}

func TestThemeFieldsOnZipImport(t *testing.T) {
	e := mapEnv(t)
	author := e.staff("ann", "author")
	yml := func(slug, extra string) string {
		return "slug: " + slug + "\nname: " + slug + "\ncategory: Web\ndescription: x\nvalue: 10\nflags: [{type: exact, value: x}]\n" + extra
	}

	bad := zipOf(t, map[string]string{"a/challenge.yml": yml("a", "theme_fields: {location: {lat: 52, lon: 999}}\n")})
	r := author.upload(400, "/admin/api/v1/challenges/import", "pack.zip", bad)
	if !strings.Contains(string(r.raw), "theme_fields.location") {
		t.Fatalf("bad import = %s", r.raw)
	}

	good := zipOf(t, map[string]string{
		"a/challenge.yml": yml("a", "theme_fields: {location: {city: Oslo, lat: 59, lon: 10}, legacy: 1}\n"),
		"b/challenge.yml": yml("b", ""),
	})
	r = author.upload(200, "/admin/api/v1/challenges/import", "pack.zip", good)
	text := string(r.raw)
	if !strings.Contains(text, `"b/challenge.yml"`) || !strings.Contains(text, "Map location") || !strings.Contains(text, "legacy (1 challenge(s))") {
		t.Fatalf("import warnings = %s", text)
	}
}
