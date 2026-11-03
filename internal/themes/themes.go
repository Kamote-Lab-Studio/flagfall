// Package themes serves the published player theme: its static files under
// /theme/{id}/ and its index.html for every other page path, so the theme's
// client-side router can take over.
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
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/flagfall/flagfall/internal/store"
)

// CSP is sent with every theme response. See docs/dev/theme-manifest.md.
const CSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
	"font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

// Manifest is the part of theme.json the server needs.
type Manifest struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Version  string    `json:"version"`
	Entry    string    `json:"entry"`
	Settings []Setting `json:"settings"`
	// ChallengeFields are extra per-challenge values the theme shows.
	ChallengeFields []ChallengeField `json:"challengeFields"`
	// AdminStyle is an optional stylesheet, inside the entry's folder, that
	// restyles the admin panel to match the theme while it is active.
	AdminStyle string `json:"adminStyle"`
}

// Setting is one organizer-editable value declared by a theme.
type Setting struct {
	Key     string `json:"key"`
	Type    string `json:"type"`
	Default any    `json:"default"`
	CSSVar  string `json:"cssVar"`
}

// Theme is a loaded theme package.
type Theme struct {
	Manifest Manifest
	files    fs.FS // rooted at the theme folder (theme.json, dist/...)
	index    []byte
	assetDir string // folder holding the entry, e.g. "dist"
	adminCSS string // AdminStyle relative to assetDir, when the file exists
}

// Load reads a theme package. A missing entry file is not an error: the
// theme is returned with Built() false so the server can explain what to do.
func Load(fsys fs.FS) (*Theme, error) {
	raw, err := fs.ReadFile(fsys, "theme.json")
	if err != nil {
		return nil, fmt.Errorf("theme.json: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("theme.json: %w", err)
	}
	if m.ID == "" || m.Entry == "" || !fs.ValidPath(m.Entry) {
		return nil, errors.New("theme.json: id and a relative entry path are required")
	}
	if err := checkFields(m.ChallengeFields); err != nil {
		return nil, fmt.Errorf("theme.json: %w", err)
	}
	t := &Theme{Manifest: m, files: fsys, assetDir: path.Dir(m.Entry)}
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
	return t, nil
}

// Built reports whether the theme's front end has been compiled.
func (t *Theme) Built() bool { return t.index != nil }

// Register records a bundled theme in the themes table (insert or update),
// so settings can be saved against it.
func (t *Theme) Register(ctx context.Context, db *store.Store, now time.Time) error {
	raw, err := fs.ReadFile(t.files, "theme.json")
	if err != nil {
		return err
	}
	row := &store.Theme{
		ID: t.Manifest.ID, Name: t.Manifest.Name, Version: t.Manifest.Version,
		Source: "bundled", Manifest: string(raw), InstalledAt: now.UTC(),
	}
	_, err = db.DB.NewInsert().Model(row).
		On("CONFLICT (id) DO UPDATE").
		Set("name = EXCLUDED.name, version = EXCLUDED.version, manifest = EXCLUDED.manifest").
		Exec(ctx)
	return err
}

// Handler serves one theme.
type Handler struct {
	theme  *Theme
	store  *store.Store
	logger *slog.Logger
	static http.Handler
}

// New returns a handler for theme. Settings are read from the store on each
// page load so published changes show up without a restart.
func New(theme *Theme, db *store.Store, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{theme: theme, store: db, logger: logger}
	if sub, err := fs.Sub(theme.files, theme.assetDir); err == nil {
		h.static = http.StripPrefix(h.Prefix(), http.FileServer(http.FS(sub)))
	}
	return h
}

// Prefix is the URL path the theme's files are served under.
func (h *Handler) Prefix() string { return "/theme/" + h.theme.Manifest.ID + "/" }

// Mount registers the theme routes. Mount it after the API so the catch-all
// only sees page paths.
func (h *Handler) Mount(r chi.Router) {
	r.Get(h.Prefix()+"*", h.serveAsset)
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
	if !h.theme.Built() {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, notBuilt, html.EscapeString(h.theme.Manifest.Name), html.EscapeString(h.theme.Manifest.ID))
		return
	}
	page := h.theme.index
	if style := h.settingsStyle(r.Context()); style != "" {
		page = injectHead(page, style)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(page)
}

func (h *Handler) serveAsset(w http.ResponseWriter, r *http.Request) {
	if h.static == nil || !h.theme.Built() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Security-Policy", CSP)
	rest := strings.TrimPrefix(r.URL.Path, h.Prefix())
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
	h.static.ServeHTTP(w, r)
}

// AdminHead returns the tags that restyle the admin panel to match this
// theme: a link to its admin stylesheet, then the organizer's live settings
// as CSS variables so customized colors carry over. It returns "" when the
// theme ships no admin stylesheet, and the admin keeps its own look.
func (h *Handler) AdminHead(ctx context.Context) string {
	if h.theme.adminCSS == "" || !h.theme.Built() {
		return ""
	}
	out := `<link rel="stylesheet" id="flagfall-theme-admin" href="` + html.EscapeString(h.Prefix()+h.theme.adminCSS) + `">`
	if style := h.settingsStyle(ctx); style != "" {
		out += `<style id="flagfall-settings">` + style + "</style>"
	}
	return out
}

// settingsStyle turns the live settings into a :root block of CSS variables.
func (h *Handler) settingsStyle(ctx context.Context) string {
	values := map[string]any{}
	if h.store != nil {
		live := new(store.ThemeSettings)
		err := h.store.DB.NewSelect().Model(live).
			Where("status = ?", "live").Where("theme_id = ?", h.theme.Manifest.ID).
			Limit(1).Scan(ctx)
		if err == nil {
			if err := json.Unmarshal([]byte(live.Data), &values); err != nil {
				h.logger.Warn("theme settings are not valid JSON", "theme", h.theme.Manifest.ID, "err", err)
			}
		}
	}
	return CSSVars(h.theme.Manifest.Settings, values)
}

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
		v, ok := values[s.Key].(string)
		if !ok || v == "" {
			continue
		}
		if s.Type == "color" && !hexColor.MatchString(v) {
			continue
		}
		if !safeValue.MatchString(v) || strings.Count(v, `"`)%2 != 0 || strings.Count(v, `'`)%2 != 0 {
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
