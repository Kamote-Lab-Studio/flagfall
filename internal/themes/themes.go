// Package themes loads theme packages and serves the player site: the live
// theme's index.html for every page path (so its client-side router can take
// over) and each installed theme's static files under /theme/{id}/.
//
// Which theme and settings a request gets is decided by the Manager: the
// live row in theme_settings for players, or a draft for an admin who has
// preview turned on.
package themes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/flagfall/flagfall/internal/store"
)

// CSP is sent with every theme response. See docs/dev/theme-manifest.md.
const CSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
	"font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

// DefaultID is the bundled theme players get when nothing else is published,
// or when the published theme can't be served.
const DefaultID = "amber-phosphor"

// Manifest is theme.json. See docs/dev/schemas/theme.schema.json.
type Manifest struct {
	ManifestVersion int               `json:"manifestVersion"`
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Description     string            `json:"description,omitempty"`
	Version         string            `json:"version"`
	Author          string            `json:"author,omitempty"`
	License         string            `json:"license,omitempty"`
	Homepage        string            `json:"homepage,omitempty"`
	Flagfall        string            `json:"flagfall,omitempty"`
	Entry           string            `json:"entry"`
	Pages           map[string]string `json:"pages"`
	Screenshots     []Screenshot      `json:"screenshots,omitempty"`
	Settings        []Setting         `json:"settings"`
	// ChallengeFields are extra per-challenge values the theme shows.
	ChallengeFields []ChallengeField `json:"challengeFields"`
	// AdminStyle is an optional stylesheet, inside the entry's folder, that
	// restyles the admin panel to match the theme while it is active.
	AdminStyle string `json:"adminStyle,omitempty"`
}

// Screenshot is a picture of the theme shown in the admin theme browser.
type Screenshot struct {
	Path string `json:"path"`
	Alt  string `json:"alt"`
}

// Theme is a loaded theme package.
type Theme struct {
	Manifest Manifest
	// Source is "bundled" (compiled into the binary) or "uploaded".
	Source   string
	raw      []byte // theme.json as read
	files    fs.FS  // rooted at the theme folder (theme.json, dist/...)
	index    []byte
	assetDir string // folder holding the entry, e.g. "dist"
	adminCSS string // AdminStyle relative to assetDir, when the file exists
	static   http.Handler
}

// Load reads and validates a theme package. A missing entry file is not an
// error: the theme is returned with Built() false so the server can explain
// what to do.
func Load(fsys fs.FS) (*Theme, error) {
	raw, err := fs.ReadFile(fsys, "theme.json")
	if err != nil {
		return nil, fmt.Errorf("theme.json: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("theme.json: %w", err)
	}
	if m.Entry == "" || !fs.ValidPath(m.Entry) || path.Ext(m.Entry) != ".html" || path.Dir(m.Entry) == "." {
		return nil, errors.New("theme.json: entry must be an .html file inside a folder, such as dist/index.html")
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("theme.json: %w", err)
	}
	if err := checkFields(m.ChallengeFields); err != nil {
		return nil, fmt.Errorf("theme.json: %w", err)
	}
	t := &Theme{Manifest: m, Source: "bundled", raw: raw, files: fsys, assetDir: path.Dir(m.Entry)}
	if m.AdminStyle != "" {
		rel, ok := strings.CutPrefix(m.AdminStyle, t.assetDir+"/")
		if !fs.ValidPath(m.AdminStyle) || !ok || path.Ext(rel) != ".css" {
			return nil, errors.New("theme.json: adminStyle must be a .css file inside the entry's folder")
		}
		if _, err := fs.Stat(fsys, m.AdminStyle); err == nil {
			t.adminCSS = rel
		}
	}
	if b, err := fs.ReadFile(fsys, m.Entry); err == nil {
		t.index = b
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if sub, err := fs.Sub(fsys, t.assetDir); err == nil {
		t.static = http.StripPrefix(t.Prefix(), http.FileServer(http.FS(sub)))
	}
	return t, nil
}

// Built reports whether the theme's front end has been compiled.
func (t *Theme) Built() bool { return t.index != nil }

// Prefix is the URL path the theme's files are served under.
func (t *Theme) Prefix() string { return "/theme/" + t.Manifest.ID + "/" }

// Screenshot opens screenshot i of the theme.
func (t *Theme) Screenshot(i int) (fs.File, error) {
	if i < 0 || i >= len(t.Manifest.Screenshots) {
		return nil, fs.ErrNotExist
	}
	p := t.Manifest.Screenshots[i].Path
	if !fs.ValidPath(p) {
		return nil, fs.ErrNotExist
	}
	return t.files.Open(p)
}

// Register records the theme in the themes table (insert or update), so
// settings can be saved against it.
func (t *Theme) Register(ctx context.Context, db *store.Store, now time.Time) error {
	row := &store.Theme{
		ID: t.Manifest.ID, Name: t.Manifest.Name, Version: t.Manifest.Version,
		Source: t.Source, Manifest: string(t.raw), InstalledAt: now.UTC(),
	}
	_, err := db.DB.NewInsert().Model(row).
		On("CONFLICT (id) DO UPDATE").
		Set("name = EXCLUDED.name, version = EXCLUDED.version, source = EXCLUDED.source, manifest = EXCLUDED.manifest").
		Exec(ctx)
	return err
}

// ---------------------------------------------------------------- registry

// Registry holds the installed themes. It is safe for concurrent use.
type Registry struct {
	mu   sync.RWMutex
	byID map[string]*Theme
}

// NewRegistry returns a registry holding themes.
func NewRegistry(themes ...*Theme) *Registry {
	r := &Registry{byID: map[string]*Theme{}}
	for _, t := range themes {
		r.Add(t)
	}
	return r
}

// Add installs t, replacing any theme with the same id.
func (r *Registry) Add(t *Theme) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[t.Manifest.ID] = t
}

// Remove uninstalls the theme with id.
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byID, id)
}

// Get returns the theme with id, or nil.
func (r *Registry) Get(id string) *Theme {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byID[id]
}

// List returns every installed theme: the default first, then by name.
func (r *Registry) List() []*Theme {
	r.mu.RLock()
	out := make([]*Theme, 0, len(r.byID))
	for _, t := range r.byID {
		out = append(out, t)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Manifest, out[j].Manifest
		if (a.ID == DefaultID) != (b.ID == DefaultID) {
			return a.ID == DefaultID
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return out
}

// ---------------------------------------------------------------- manager

// View is the theme and settings one request should see.
type View struct {
	Theme *Theme
	// Version of the settings row (0 when nothing is published).
	Version int
	// Values are the stored setting values; defaults are not merged in.
	Values    map[string]any
	CustomCSS string
	// Preview is true when an admin is looking at a draft.
	Preview bool
	// Fallback is true when the published theme couldn't be served and
	// the default theme is shown instead.
	Fallback bool
}

// Manager decides which theme and settings each request gets.
type Manager struct {
	Registry *Registry
	Store    *store.Store
	Logger   *slog.Logger
	// PreviewTheme returns the theme an admin is previewing in this
	// request's session, or "". Nil turns preview off.
	PreviewTheme func(*http.Request) string

	warnMu   sync.Mutex
	warnedAt map[string]time.Time
}

// NewManager returns a manager for the themes in reg.
func NewManager(reg *Registry, db *store.Store, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{Registry: reg, Store: db, Logger: logger}
}

// Live is what players see: the published theme and settings, or the
// default theme if nothing is published or the published theme is missing
// or unbuilt.
func (m *Manager) Live(ctx context.Context) View {
	row := new(store.ThemeSettings)
	if m.Store != nil {
		err := m.Store.DB.NewSelect().Model(row).Where("status = ?", "live").
			OrderExpr("published_at DESC, id DESC").Limit(1).Scan(ctx)
		if err == nil {
			t := m.Registry.Get(row.ThemeID)
			if t != nil && t.Built() {
				return View{Theme: t, Version: row.Version, Values: decodeValues(m.Logger, row), CustomCSS: row.CustomCSS}
			}
			if row.ThemeID != DefaultID {
				m.warnOnce(row.ThemeID, "the published theme can't be served; players get the default theme instead",
					"theme", row.ThemeID, "installed", t != nil)
				return View{Theme: m.defaultTheme(), Values: map[string]any{}, Fallback: true}
			}
		}
	}
	return View{Theme: m.defaultTheme(), Values: map[string]any{}}
}

// LiveTheme is the published theme (see Live).
func (m *Manager) LiveTheme() *Theme { return m.Live(context.Background()).Theme }

// Current is what this request should see: the admin's preview, if one is
// on for this session, or the live theme.
func (m *Manager) Current(r *http.Request) View {
	if m.PreviewTheme != nil {
		if id := m.PreviewTheme(r); id != "" {
			if v, ok := m.Draft(r.Context(), id); ok {
				return v
			}
		}
	}
	return m.Live(r.Context())
}

// Draft is what an admin previewing theme id sees: its draft, else its
// latest saved settings, else its defaults. ok is false if the theme isn't
// installed and built.
func (m *Manager) Draft(ctx context.Context, id string) (View, bool) {
	t := m.Registry.Get(id)
	if t == nil || !t.Built() {
		return View{}, false
	}
	v := View{Theme: t, Values: map[string]any{}, Preview: true}
	if m.Store == nil {
		return v, true
	}
	row := new(store.ThemeSettings)
	// The draft if there is one, otherwise the live, scheduled or most
	// recent version, so preview starts from what the organizer last saw.
	err := m.Store.DB.NewSelect().Model(row).Where("theme_id = ?", id).
		OrderExpr("CASE status WHEN 'draft' THEN 0 WHEN 'live' THEN 1 WHEN 'scheduled' THEN 2 ELSE 3 END, version DESC").
		Limit(1).Scan(ctx)
	if err == nil {
		v.Version, v.Values, v.CustomCSS = row.Version, decodeValues(m.Logger, row), row.CustomCSS
	}
	return v, true
}

func (m *Manager) defaultTheme() *Theme {
	if t := m.Registry.Get(DefaultID); t != nil {
		return t
	}
	if list := m.Registry.List(); len(list) > 0 {
		return list[0]
	}
	return nil
}

// warnOnce logs the same theme problem at most every 10 minutes.
func (m *Manager) warnOnce(key, msg string, args ...any) {
	m.warnMu.Lock()
	defer m.warnMu.Unlock()
	if m.warnedAt == nil {
		m.warnedAt = map[string]time.Time{}
	}
	if time.Since(m.warnedAt[key]) < 10*time.Minute {
		return
	}
	m.warnedAt[key] = time.Now()
	m.Logger.Warn(msg, args...)
}

func decodeValues(logger *slog.Logger, row *store.ThemeSettings) map[string]any {
	values := map[string]any{}
	if err := json.Unmarshal([]byte(row.Data), &values); err != nil {
		logger.Warn("theme settings are not valid JSON", "theme", row.ThemeID, "version", row.Version, "err", err)
	}
	return values
}

// ---------------------------------------------------------------- HTTP

// Handler serves the player site and theme files.
type Handler struct {
	m *Manager
}

// New returns a handler for the themes m manages. Settings are read from
// the store on each page load so published changes show up at once.
func New(m *Manager) *Handler { return &Handler{m: m} }

// Mount registers the theme routes. Mount it after the API so the catch-all
// only sees page paths.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/theme/{id}/*", h.serveAsset)
	r.Get("/*", h.servePage)
}

var reserved = []string{"/api/", "/files/", "/admin", "/theme/", "/healthz"}

func (h *Handler) servePage(w http.ResponseWriter, r *http.Request) {
	for _, p := range reserved {
		if strings.HasPrefix(r.URL.Path, p) {
			http.NotFound(w, r)
			return
		}
	}
	w.Header().Set("Content-Security-Policy", CSP)
	v := h.m.Current(r)
	if v.Theme == nil || !v.Theme.Built() {
		name, id := "player", DefaultID
		if v.Theme != nil {
			name, id = v.Theme.Manifest.Name, v.Theme.Manifest.ID
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, notBuilt, html.EscapeString(name), html.EscapeString(id))
		return
	}
	page := v.Theme.index
	if style := CSSVars(v.Theme.Manifest.Settings, v.Values); style != "" {
		page = injectHead(page, style)
	}
	if css := SafeCustomCSS(v.CustomCSS); css != "" {
		page = InjectHead(page, `<style id="flagfall-custom-css">`+css+"</style>")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if v.Preview {
		w.Header().Set("Cache-Control", "no-store")
	}
	_, _ = w.Write(page)
}

func (h *Handler) serveAsset(w http.ResponseWriter, r *http.Request) {
	t := h.m.Registry.Get(chi.URLParam(r, "id"))
	if t == nil || t.static == nil || !t.Built() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Security-Policy", CSP)
	rest := strings.TrimPrefix(r.URL.Path, t.Prefix())
	if rest == "" || strings.HasSuffix(rest, "/") {
		http.NotFound(w, r) // no directory listings
		return
	}
	if strings.HasPrefix(rest, "assets/") {
		// Vite puts a content hash in these names.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	t.static.ServeHTTP(w, r)
}

// AdminHead returns the tags that restyle the admin panel to match the live
// theme: a link to its admin stylesheet, then the organizer's live settings
// as CSS variables so customized colors carry over. It returns "" when the
// theme ships no admin stylesheet, and the admin keeps its own look.
func (h *Handler) AdminHead(ctx context.Context) string {
	v := h.m.Live(ctx)
	if v.Theme == nil || v.Theme.adminCSS == "" || !v.Theme.Built() {
		return ""
	}
	out := `<link rel="stylesheet" id="flagfall-theme-admin" href="` + html.EscapeString(v.Theme.Prefix()+v.Theme.adminCSS) + `">`
	if style := CSSVars(v.Theme.Manifest.Settings, v.Values); style != "" {
		out += `<style id="flagfall-settings">` + style + "</style>"
	}
	return out
}

// ---------------------------------------------------------------- CSS

var (
	cssVarName = regexp.MustCompile(`^--[a-zA-Z0-9-]+$`)
	hexColor   = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	// Plain values: words, numbers, units, commas, quotes for font names.
	safeValue = regexp.MustCompile(`^[a-zA-Z0-9 #%.,'"_-]{1,200}$`)
)

// CSSVars renders the settings that declare a cssVar. Only values set by the
// organizer are written (defaults already live in the theme's CSS). Anything
// that doesn't look like a plain CSS value is dropped, so a stored value can
// never close the style block or add rules.
func CSSVars(settings []Setting, values map[string]any) string {
	var decls []string
	for _, s := range settings {
		if s.CSSVar == "" || !cssVarName.MatchString(s.CSSVar) {
			continue
		}
		var v string
		switch x := values[s.Key].(type) {
		case string:
			v = x
		case float64:
			v = fmt.Sprint(x)
		}
		if v == "" {
			continue
		}
		if s.Type == "color" && !hexColor.MatchString(v) {
			continue
		}
		if !safeCSSValue(v) {
			continue
		}
		decls = append(decls, s.CSSVar+": "+v+";")
	}
	if len(decls) == 0 {
		return ""
	}
	sort.Strings(decls)
	return ":root { " + strings.Join(decls, " ") + " }"
}

// MaxCustomCSS caps the custom CSS box.
const MaxCustomCSS = 40_000

// CheckCustomCSS explains why custom CSS can't be saved, or returns "".
// It must stay inside one <style> element, so "<" is not allowed, and it
// may only load files from this server (the CSP enforces that too).
func CheckCustomCSS(css string) string {
	switch {
	case len(css) > MaxCustomCSS:
		return fmt.Sprintf("Custom CSS must be at most %d characters.", MaxCustomCSS)
	case strings.Contains(css, "<"):
		return `Custom CSS can't contain "<".`
	case strings.Contains(strings.ToLower(css), "@import"):
		return "Custom CSS can't use @import."
	}
	return ""
}

// SafeCustomCSS returns css if it passes CheckCustomCSS, else "".
func SafeCustomCSS(css string) string {
	if strings.TrimSpace(css) == "" || CheckCustomCSS(css) != "" {
		return ""
	}
	return css
}

func injectHead(page []byte, css string) []byte {
	return InjectHead(page, `<style id="flagfall-settings">`+css+"</style>")
}

// InjectHead inserts markup just before </head>.
func InjectHead(page []byte, markup string) []byte {
	tag := []byte(markup)
	i := bytes.Index(page, []byte("</head>"))
	if i < 0 {
		return append(tag, page...)
	}
	out := make([]byte, 0, len(page)+len(tag))
	out = append(out, page[:i]...)
	out = append(out, tag...)
	return append(out, page[i:]...)
}

const notBuilt = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Flagfall</title>
<style>body{font:16px/1.5 system-ui,sans-serif;max-width:40rem;margin:4rem auto;padding:0 1rem;background:#0D1321;color:#E6E9EF}code{background:#1A2233;padding:2px 6px;border-radius:4px}</style>
</head><body>
<h1>The player theme isn't built</h1>
<p>This server was compiled without the %s theme's front end. Build it, then rebuild the server:</p>
<pre><code>make web-build
make build</code></pre>
<p>Or run <code>pnpm build</code> in <code>themes/%s</code>. The API at <code>/api/v1</code> works in the meantime.</p>
</body></html>
`
