package api_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flagfall/flagfall/internal/api"
	"github.com/flagfall/flagfall/internal/themes"
)

const testPages = `"pages":{"board":"/","challenge":"/c/:slug","scoreboard":"/s","teams":"/t","team":"/t/:id","page":"/p/:slug",` +
	`"signIn":"/in","signUp":"/up","account":"/a","announcements":"/n","notStarted":"/soon","over":"/over"}`

func testTheme(t *testing.T, id, name string) *themes.Theme {
	t.Helper()
	manifest := `{"manifestVersion":1,"id":"` + id + `","name":"` + name + `","version":"1.0.0","entry":"dist/index.html",` + testPages + `,
	 "settings":[
	  {"key":"accent","type":"color","label":"Accent","default":"#FFB000","cssVar":"--amber","contrastWith":"background"},
	  {"key":"background","type":"color","label":"Background","default":"#0D1321","cssVar":"--bg"},
	  {"key":"layout","type":"select","label":"Layout","default":"grid","options":[{"value":"grid","label":"Grid"},{"value":"list","label":"List"}]},
	  {"key":"logo","type":"image","label":"Logo","accept":["image/png","image/svg+xml"],"maxBytes":1024}]}`
	th, err := themes.Load(fstest.MapFS{
		"theme.json":      {Data: []byte(manifest)},
		"dist/index.html": {Data: []byte("<html><head></head></html>")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return th
}

func themeEnv(t *testing.T) (*env, *themes.Manager) {
	t.Helper()
	var m *themes.Manager
	e := makeEnv(t, true, func(o *api.Options) {
		m = themes.NewManager(themes.NewRegistry(testTheme(t, "amber-phosphor", "Amber"), testTheme(t, "neon", "Neon")), o.Store, nil)
		o.Themes = m
	})
	m.PreviewTheme = e.api.PreviewTheme
	for _, th := range m.Registry.List() {
		if err := th.Register(t.Context(), e.store, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	return e, m
}

func findTheme(r resp, id string) map[string]any {
	for _, x := range r.body["themes"].([]any) {
		if m := x.(map[string]any); m["id"] == id {
			return m
		}
	}
	return nil
}

func TestThemeDraftPreviewPublish(t *testing.T) {
	e, _ := themeEnv(t)
	admin := e.staff("ada", "admin")
	player := e.signUp("pat")

	list := admin.must(200, "GET", "/admin/api/v1/themes", nil)
	if list.str("liveThemeId") != "amber-phosphor" || len(list.body["themes"].([]any)) != 2 {
		t.Fatalf("themes = %s", list.raw)
	}
	if amber := findTheme(list, "amber-phosphor"); amber["live"] != true || amber["draft"] != nil {
		t.Fatalf("amber = %v", amber)
	}
	e.staff("sue", "support").must(403, "GET", "/admin/api/v1/themes", nil)

	// Drafts are checked: types, unknown options, contrast, custom CSS.
	bad := admin.must(400, "PUT", "/admin/api/v1/themes/neon/draft", map[string]any{
		"settings":  map[string]any{"accent": "#101830", "layout": "cards"},
		"customCss": "</style>",
	})
	for _, f := range []string{"settings.accent", "settings.layout", "customCss"} {
		if bad.str("error", "fields", f) == "" {
			t.Errorf("%s not flagged: %s", f, bad.raw)
		}
	}
	admin.must(404, "PUT", "/admin/api/v1/themes/nope/draft", map[string]any{"settings": map[string]any{}})

	d := admin.must(200, "PUT", "/admin/api/v1/themes/neon/draft", map[string]any{
		"settings": map[string]any{"accent": "#00FF88", "layout": "list"}, "customCss": ".x{}"})
	if d.num("version") != 1 {
		t.Fatalf("draft = %s", d.raw)
	}
	// Saving again replaces the same draft (PUT sends every value).
	if d := admin.must(200, "PUT", "/admin/api/v1/themes/neon/draft", map[string]any{
		"settings": map[string]any{"accent": "#00FF99", "layout": "list"}}); d.num("version") != 1 {
		t.Fatalf("second save = %s", d.raw)
	}

	// Preview: only this admin sees the draft.
	admin.must(200, "POST", "/admin/api/v1/themes/neon/preview", nil)
	if th := admin.must(200, "GET", "/api/v1/theme", nil); th.str("id") != "neon" || th.body["preview"] != true ||
		th.str("settings", "accent") != "#00FF99" {
		t.Fatalf("admin preview = %s", th.raw)
	}
	if th := player.must(200, "GET", "/api/v1/theme", nil); th.str("id") != "amber-phosphor" || th.body["preview"] != false {
		t.Fatalf("player = %s", th.raw)
	}
	if findTheme(admin.must(200, "GET", "/admin/api/v1/themes", nil), "neon")["previewing"] != true {
		t.Fatal("previewing not reported")
	}
	// A player can't turn preview on, even by setting the column.
	player.must(403, "POST", "/admin/api/v1/themes/neon/preview", nil)

	// Publish: the draft goes live, players are told, the preview ends.
	events, stop := e.subscribe()
	defer stop()
	pub := admin.must(200, "POST", "/admin/api/v1/themes/neon/publish", map[string]any{})
	if pub.num("published", "version") != 1 {
		t.Fatalf("publish = %s", pub.raw)
	}
	select {
	case ev := <-events:
		if !strings.Contains(ev, "event: theme-published") || !strings.Contains(ev, `"id":"neon"`) {
			t.Fatalf("event = %q", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no theme-published event")
	}
	for _, c := range []*client{player, admin} {
		if th := c.must(200, "GET", "/api/v1/theme", nil); th.str("id") != "neon" || th.body["preview"] != false ||
			th.str("settings", "layout") != "list" {
			t.Fatalf("after publish = %s", th.raw)
		}
	}
	list = admin.must(200, "GET", "/admin/api/v1/themes", nil)
	if neon := findTheme(list, "neon"); neon["live"] != true || neon["draft"] != nil || neon["published"] == nil {
		t.Fatalf("neon = %v", neon)
	}

	// Switching back to a theme with no saved settings uses its defaults.
	admin.must(200, "POST", "/admin/api/v1/themes/amber-phosphor/publish", nil)
	if th := player.must(200, "GET", "/api/v1/theme", nil); th.str("id") != "amber-phosphor" || len(th.body["settings"].(map[string]any)) != 0 {
		t.Fatalf("back to amber = %s", th.raw)
	}

	// Rollback publishes a copy of an old version as a new one.
	r := admin.must(200, "POST", "/admin/api/v1/themes/neon/versions/1/restore", nil)
	if r.num("published", "version") != 2 || r.num("restoredFrom") != 1 {
		t.Fatalf("restore = %s", r.raw)
	}
	vs := admin.must(200, "GET", "/admin/api/v1/themes/neon/versions", nil)
	versions := vs.body["versions"].([]any)
	if len(versions) != 2 || versions[0].(map[string]any)["status"] != "live" || versions[1].(map[string]any)["status"] != "archived" ||
		versions[0].(map[string]any)["publishedBy"] != "ada" {
		t.Fatalf("versions = %s", vs.raw)
	}
	admin.must(404, "POST", "/admin/api/v1/themes/neon/versions/9/restore", nil)

	// Publishing and restoring are in the audit log.
	var actions []string
	for _, a := range e.audit() {
		actions = append(actions, a.Action)
	}
	joined := strings.Join(actions, "\n")
	for _, want := range []string{"PUT /themes/{id}/draft", "POST /themes/{id}/publish", "POST /themes/{id}/versions/{version}/restore"} {
		if !strings.Contains(joined, want) {
			t.Errorf("audit is missing %q:\n%s", want, joined)
		}
	}
}

func TestThemeScheduledSwitch(t *testing.T) {
	e, _ := themeEnv(t)
	admin := e.staff("ada", "admin")
	player := e.signUp("pat")

	at := e.clock.Now().Add(time.Hour)
	s := admin.must(200, "POST", "/admin/api/v1/themes/neon/publish", map[string]any{"publishAt": at})
	if s.str("scheduled", "publishAt") == "" {
		t.Fatalf("schedule = %s", s.raw)
	}
	if findTheme(admin.must(200, "GET", "/admin/api/v1/themes", nil), "neon")["scheduled"] == nil {
		t.Fatal("scheduled switch not listed")
	}

	// Not yet.
	e.api.PublishScheduledThemes(t.Context())
	if th := player.must(200, "GET", "/api/v1/theme", nil); th.str("id") != "amber-phosphor" {
		t.Fatalf("too early: %s", th.raw)
	}
	e.clock.Add(2 * time.Hour)
	e.api.PublishScheduledThemes(t.Context())
	if th := player.must(200, "GET", "/api/v1/theme", nil); th.str("id") != "neon" {
		t.Fatalf("after the time: %s", th.raw)
	}

	// Cancelling turns a scheduled switch back into a draft.
	admin.must(200, "POST", "/admin/api/v1/themes/amber-phosphor/publish", map[string]any{"publishAt": e.clock.Now().Add(time.Hour)})
	admin.must(204, "DELETE", "/admin/api/v1/themes/amber-phosphor/schedule", nil)
	amber := findTheme(admin.must(200, "GET", "/admin/api/v1/themes", nil), "amber-phosphor")
	if amber["scheduled"] != nil || amber["draft"] == nil {
		t.Fatalf("after cancel: %v", amber)
	}
	admin.must(404, "DELETE", "/admin/api/v1/themes/amber-phosphor/schedule", nil)
}

func TestThemeImages(t *testing.T) {
	e, _ := themeEnv(t)
	admin := e.staff("ada", "admin")
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	svg := []byte(`<?xml version="1.0"?><!-- logo --><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)

	// Only image settings take uploads, and only in the formats they accept.
	admin.upload(404, "/admin/api/v1/themes/neon/images/accent", "x.png", png)
	admin.upload(404, "/admin/api/v1/themes/nope/images/logo", "x.png", png)
	if r := admin.upload(400, "/admin/api/v1/themes/neon/images/logo", "x.png", []byte("GIF89a....")); r.str("error", "fields", "file") == "" {
		t.Fatalf("gif: %s", r.raw)
	}
	if r := admin.upload(400, "/admin/api/v1/themes/neon/images/logo", "x.png", append(png, make([]byte, 2000)...)); !strings.Contains(r.str("error", "fields", "file"), "1 KB") {
		t.Fatalf("too large: %s", r.raw)
	}
	e.signUp("pat").upload(403, "/admin/api/v1/themes/neon/images/logo", "x.png", png)

	up := admin.upload(201, "/admin/api/v1/themes/neon/images/logo", "logo.png", png)
	url := up.str("url")
	if !strings.HasPrefix(url, "/theme-assets/") || up.str("contentType") != "image/png" {
		t.Fatalf("upload = %s", up.raw)
	}
	svgURL := admin.upload(201, "/admin/api/v1/themes/neon/images/logo", "logo.svg", svg).str("url")

	// Served publicly, cached forever, and an SVG can't run script.
	res := e.client().do("GET", url, nil)
	if res.status != 200 || res.header.Get("Content-Type") != "image/png" || !strings.Contains(res.header.Get("Cache-Control"), "immutable") {
		t.Fatalf("serve png: %d %v", res.status, res.header)
	}
	res = e.client().do("GET", svgURL, nil)
	if res.status != 200 || res.header.Get("Content-Type") != "image/svg+xml" || !strings.Contains(res.header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("serve svg: %d %v", res.status, res.header)
	}

	// A challenge file's hash isn't served here.
	c := e.addChallenge("secret", 100, "flag{x}")
	secret := []byte("hidden challenge file")
	admin.upload(201, sprintf("/admin/api/v1/challenges/%d/files", c.ID), "secret.bin", secret)
	digest := sha256.Sum256(secret)
	sum := hex.EncodeToString(digest[:])
	if r := e.client().do("GET", "/theme-assets/"+sum, nil); r.status != 404 {
		t.Fatalf("challenge blob served: %d", r.status)
	}
	if r := e.client().do("GET", "/theme-assets/not-a-hash", nil); r.status != 404 {
		t.Fatalf("bad hash: %d", r.status)
	}

	// The draft takes uploaded images only.
	bad := admin.must(400, "PUT", "/admin/api/v1/themes/neon/draft", map[string]any{"settings": map[string]any{"logo": "/theme-assets/" + sum}})
	if bad.str("error", "fields", "settings.logo") == "" {
		t.Fatalf("unuploaded image accepted: %s", bad.raw)
	}
	admin.must(400, "PUT", "/admin/api/v1/themes/neon/draft", map[string]any{"settings": map[string]any{"logo": "https://evil.example/x.png"}})
	admin.must(200, "PUT", "/admin/api/v1/themes/neon/draft", map[string]any{"settings": map[string]any{"logo": url}})
	admin.must(200, "PUT", "/admin/api/v1/themes/neon/draft", map[string]any{"settings": map[string]any{"logo": ""}})
}
