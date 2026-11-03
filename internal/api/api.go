// Package api implements the player API (docs/dev/api.md).
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/flagfall/flagfall/internal/blobs"
	"github.com/flagfall/flagfall/internal/config"
	"github.com/flagfall/flagfall/internal/live"
	"github.com/flagfall/flagfall/internal/ratelimit"
	"github.com/flagfall/flagfall/internal/scoring"
	"github.com/flagfall/flagfall/internal/store"
	"github.com/flagfall/flagfall/internal/themes"
)

// Mailer sends account emails. By default the API uses the SMTP settings
// saved in the admin panel, and logs emails while there are none.
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

// LogMailer logs emails instead of sending them. For development only.
type LogMailer struct{ Logger *slog.Logger }

func (m LogMailer) Send(_ context.Context, to, subject, body string) error {
	m.Logger.Warn("email not sent (no SMTP server configured)", "to", to, "subject", subject, "body", body)
	return nil
}

// API serves /api/v1 and /files.
type API struct {
	store  *store.Store
	cfg    config.Config
	hub    *live.Hub
	boards *scoring.Cache
	mailer Mailer
	files  *blobs.Store
	theme  func() *themes.Theme
	// httpClient makes outgoing calls (Discord webhooks).
	httpClient *http.Client
	log        *slog.Logger
	now        func() time.Time

	limitSignIn    *ratelimit.Limiter
	limitSignUp    *ratelimit.Limiter
	limitReset     *ratelimit.Limiter
	limitJoin      *ratelimit.Limiter
	limitFlags     *ratelimit.Limiter
	limitGeneral   *ratelimit.Limiter
	sessionTouchAt time.Duration
	setup          setupState
}

// Options configures New.
type Options struct {
	Store  *store.Store
	Config config.Config
	Hub    *live.Hub
	Boards *scoring.Cache
	Mailer Mailer
	// Files stores challenge files; nil means DataDir/files on disk.
	Files *blobs.Store
	// Theme returns the active player theme, for its challenge fields; nil
	// means a theme without any.
	Theme func() *themes.Theme
	// HTTPClient makes outgoing calls; tests swap its transport.
	HTTPClient *http.Client
	Logger     *slog.Logger
	// Now overrides the clock, for tests.
	Now func() time.Time
}

// New returns the player API.
func New(o Options) *API {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Boards == nil {
		o.Boards = scoring.NewCache(o.Store)
	}
	a := &API{
		store: o.Store, cfg: o.Config, hub: o.Hub, boards: o.Boards, mailer: o.Mailer, log: o.Logger, now: o.Now,
		limitSignIn:    ratelimit.New(10, 15*time.Minute),
		limitSignUp:    ratelimit.New(5, time.Hour),
		limitReset:     ratelimit.New(3, time.Hour),
		limitJoin:      ratelimit.New(10, time.Hour),
		limitFlags:     ratelimit.New(10, time.Minute),
		limitGeneral:   ratelimit.New(300, time.Minute),
		sessionTouchAt: 5 * time.Minute,
	}
	if a.httpClient = o.HTTPClient; a.httpClient == nil {
		a.httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	if a.mailer == nil {
		a.mailer = settingsMailer{a}
	}
	a.theme = o.Theme
	if a.files = o.Files; a.files == nil {
		a.files = blobs.Local(blobs.LocalDir(o.Config.DataDir))
	}
	return a
}

// Boards exposes the scoreboard cache, so admin code can invalidate it.
func (a *API) Boards() *scoring.Cache { return a.boards }

// Mount adds the API routes to r.
func (a *API) Mount(r chi.Router) {
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(a.sessionMiddleware, a.csrf, a.generalLimit)

		r.Get("/event", a.h(a.getEvent))
		r.Get("/theme", a.h(a.getTheme))

		r.Post("/auth/sign-up", a.h(a.signUp))
		r.Post("/auth/sign-in", a.h(a.signIn))
		r.Post("/auth/sign-out", a.h(a.signOut))
		r.Post("/auth/password-reset", a.h(a.requestPasswordReset))
		r.Post("/auth/password-reset/confirm", a.h(a.confirmPasswordReset))

		r.Get("/me", a.h(a.getMe))
		r.Patch("/me", a.h(a.player(a.updateMe)))
		r.Post("/me/password", a.h(a.player(a.changePassword)))

		r.Get("/teams", a.h(a.listTeams))
		r.Post("/teams", a.h(a.player(a.createTeam)))
		r.Post("/teams/join", a.h(a.player(a.joinTeam)))
		r.Post("/teams/leave", a.h(a.player(a.leaveTeam)))
		r.Patch("/teams/mine", a.h(a.player(a.updateMyTeam)))
		r.Post("/teams/mine/invite-code", a.h(a.player(a.regenerateInvite)))
		r.Delete("/teams/mine/members/{userID}", a.h(a.player(a.removeMember)))
		r.Post("/teams/mine/captain", a.h(a.player(a.transferCaptain)))
		r.Get("/teams/{id}", a.h(a.getTeam))

		r.Get("/challenges", a.h(a.listChallenges))
		r.Get("/challenges/{slug}", a.h(a.getChallenge))
		r.Post("/challenges/{slug}/submit", a.h(a.player(a.submitFlag)))
		r.Post("/challenges/{slug}/hints/{hintID}/unlock", a.h(a.player(a.unlockHint)))

		r.Get("/scoreboard", a.h(a.getScoreboard))
		r.Get("/scoreboard/series", a.h(a.getSeries))

		r.Get("/announcements", a.h(a.listAnnouncements))
		r.Get("/pages", a.h(a.listPages))
		r.Get("/pages/{slug}", a.h(a.getPage))

		if a.hub != nil {
			r.Get("/live", a.hub.ServeHTTP)
		}
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, errNotFound)
		})
	})

	r.With(a.sessionMiddleware).Get("/files/{sha256}/{name}", a.h(a.downloadFile))
}

// ---------------------------------------------------------------- errors

// apiError is an error with an HTTP status and a stable code.
type apiError struct {
	Status  int               `json:"-"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
	retry   time.Duration
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

var (
	errNotFound        = &apiError{Status: 404, Code: "not_found", Message: "Not found."}
	errUnauthenticated = &apiError{Status: 401, Code: "unauthenticated", Message: "Sign in first."}
)

func errForbidden(msg string) error { return &apiError{Status: 403, Code: "forbidden", Message: msg} }
func errConflict(msg string) error  { return &apiError{Status: 409, Code: "conflict", Message: msg} }
func errValidation(fields map[string]string) error {
	return &apiError{Status: 400, Code: "validation", Message: "Check the highlighted fields.", Fields: fields}
}
func errBadRequest(msg string) error {
	return &apiError{Status: 400, Code: "validation", Message: msg}
}
func errRateLimited(wait time.Duration) error {
	return &apiError{Status: 429, Code: "rate_limited", Message: "Too many attempts. Try again shortly.", retry: wait}
}

func writeError(w http.ResponseWriter, err error) {
	var ae *apiError
	if !errors.As(err, &ae) {
		ae = &apiError{Status: 500, Code: "internal", Message: "Something went wrong on our side."}
	}
	if ae.retry > 0 {
		w.Header().Set("Retry-After", fmt.Sprint(int(ae.retry.Seconds()+0.999)))
	}
	writeJSON(w, ae.Status, map[string]any{"error": ae})
}

// ---------------------------------------------------------------- handlers

type handler func(w http.ResponseWriter, r *http.Request) error

// h adapts a handler that returns an error.
func (a *API) h(fn handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := fn(w, r)
		if err == nil {
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			err = errNotFound
		}
		var ae *apiError
		if !errors.As(err, &ae) {
			a.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		}
		writeError(w, err)
	}
}

// player requires a signed-in user.
func (a *API) player(fn handler) handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		if currentUser(r) == nil {
			return errUnauthenticated
		}
		return fn(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func noContent(w http.ResponseWriter) error {
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// decode reads a JSON body of at most 64 KiB into v, rejecting unknown fields.
func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errBadRequest("The request body isn't valid JSON for this endpoint.")
	}
	return nil
}

// clientIP returns the caller's IP (RealIP middleware has already applied
// X-Forwarded-For when configured).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func strp(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func normalizeCountry(c string) (string, bool) {
	c = strings.ToUpper(strings.TrimSpace(c))
	if c == "" {
		return "", true
	}
	if len(c) != 2 || c[0] < 'A' || c[0] > 'Z' || c[1] < 'A' || c[1] > 'Z' {
		return "", false
	}
	return c, true
}
