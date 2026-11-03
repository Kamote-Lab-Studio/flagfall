package adminui_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"

	"github.com/flagfall/flagfall/internal/adminui"
)

func router(files fstest.MapFS) http.Handler {
	r := chi.NewRouter()
	r.Get("/admin/api/v1/me", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("api")) })
	adminui.New(files).Mount(r)
	return r
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestServesAppAndAssets(t *testing.T) {
	h := router(fstest.MapFS{
		"dist/index.html":      {Data: []byte("<html>panel</html>")},
		"dist/assets/app-1.js": {Data: []byte("js")},
		"dist/favicon.svg":     {Data: []byte("<svg/>")},
	})
	for _, p := range []string{"/admin", "/admin/", "/admin/teams/12", "/admin/challenges"} {
		rec := get(h, p)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "panel") {
			t.Errorf("%s = %d %q", p, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Errorf("%s: no CSP", p)
		}
	}
	if rec := get(h, "/admin/assets/app-1.js"); rec.Body.String() != "js" || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("asset = %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
	}
	if rec := get(h, "/admin/assets/missing.js"); rec.Code != 404 {
		t.Errorf("missing asset = %d", rec.Code)
	}
	// The API keeps its routes; unknown API paths get a JSON 404, not the app.
	if rec := get(h, "/admin/api/v1/me"); rec.Body.String() != "api" {
		t.Errorf("api shadowed: %q", rec.Body.String())
	}
	if rec := get(h, "/admin/api/v1/nope"); rec.Code != 404 || !strings.Contains(rec.Body.String(), "not_found") {
		t.Errorf("unknown api = %d %q", rec.Code, rec.Body.String())
	}
}

func TestNotBuilt(t *testing.T) {
	rec := get(router(fstest.MapFS{"dist/.keep": {}}), "/admin")
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "isn't built") {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}
