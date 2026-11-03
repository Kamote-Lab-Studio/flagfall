// Package server wires up Flagfall's HTTP routes.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/flagfall/flagfall/internal/version"
)

// Options configures the server.
type Options struct {
	Logger *slog.Logger
	// TrustProxy uses X-Forwarded-For / X-Real-IP for the client address.
	// Only enable behind a reverse proxy that sets them; otherwise players
	// could fake their IP to dodge rate limits.
	TrustProxy bool
	// Mount adds feature routes (player API, files, theme, admin).
	Mount []func(chi.Router)
}

// New returns the root HTTP handler.
//
// Route layout (see docs/dev/api.md):
//
//	/healthz      liveness check
//	/api/v1/...   player API (Phase 1)
//	/admin/...    admin panel and admin API (Phase 2)
//	/files/...    challenge downloads (Phase 1)
//	/*            the published theme (Phase 1)
func New(opts Options) http.Handler {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	if opts.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(requestLogger(opts.Logger))
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)

	r.Get("/healthz", healthz)
	for _, mount := range opts.Mount {
		mount(r)
	}

	return r
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": version.Version,
	})
}

// securityHeaders sets headers every response gets. The strict per-theme
// Content Security Policy is added with the theme manager in Phase 1.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration", time.Since(start),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
