// Package adminui serves the built admin panel (web/admin) at /admin.
package adminui

import (
	"context"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Kamote-Lab-Studio/flagfall/internal/themes"
)

// Handler serves the admin single-page app.
type Handler struct {
	index  []byte
	static http.Handler
	head   func(context.Context) string
}

// New takes the embedded build (a filesystem with dist/ at its root).
func New(files fs.FS) *Handler {
	h := &Handler{}
	sub, err := fs.Sub(files, "dist")
	if err != nil {
		return h
	}
	if b, err := fs.ReadFile(sub, "index.html"); err == nil {
		h.index = b
	}
	h.static = http.StripPrefix("/admin/", http.FileServer(http.FS(sub)))
	return h
}

// WithHead sets a function whose markup is added to the end of the admin
// page's <head> on every load. The server uses it to apply the active
// theme's admin stylesheet.
func (h *Handler) WithHead(fn func(context.Context) string) *Handler {
	h.head = fn
	return h
}

// Built reports whether the admin panel was compiled in.
func (h *Handler) Built() bool { return h.index != nil }

// Mount registers /admin and /admin/*. The admin API's routes are more
// specific, so chi still sends /admin/api/v1/... there.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/admin", h.serve)
	r.Get("/admin/*", h.serve)
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", themes.CSP)
	w.Header().Set("X-Robots-Tag", "noindex")
	rest := strings.TrimPrefix(r.URL.Path, "/admin")
	rest = strings.TrimPrefix(rest, "/")
	if strings.HasPrefix(rest, "api/") {
		// An admin API path that no route matched.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"No such admin API endpoint."}}` + "\n"))
		return
	}
	if !h.Built() {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(notBuilt))
		return
	}
	if strings.HasPrefix(rest, "assets/") || rest == "favicon.svg" {
		if strings.HasPrefix(rest, "assets/") {
			// Vite puts a content hash in these names.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		h.static.ServeHTTP(w, r)
		return
	}
	// Every other path is a screen of the app.
	page := h.index
	if h.head != nil {
		if extra := h.head(r.Context()); extra != "" {
			page = themes.InjectHead(page, extra)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(page)
}

const notBuilt = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Flagfall admin</title>
<style>body{font:16px/1.5 system-ui,sans-serif;max-width:40rem;margin:4rem auto;padding:0 1rem;background:#0f1115;color:#e6e8ec}code{background:#1e222b;padding:2px 6px;border-radius:4px}</style>
</head><body>
<h1>The admin panel isn't built</h1>
<p>This server was compiled without the admin panel's front end. Build it, then rebuild the server:</p>
<pre><code>make web-build
make build</code></pre>
<p>Or run <code>pnpm build</code> in <code>web/admin</code>. The admin API at <code>/admin/api/v1</code> works in the meantime.</p>
</body></html>
`
