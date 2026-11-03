package api

// Admin API: /admin/api/v1 (docs/dev/admin-api.md).

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/uptrace/bun"

	"github.com/flagfall/flagfall/internal/seed"
	"github.com/flagfall/flagfall/internal/store"
)

// Staff roles, lowest first.
var roleRank = map[string]int{"player": 0, "support": 1, "author": 2, "admin": 3, "owner": 4}

// setupState tracks first-run setup. Once an owner exists it never goes back.
type setupState struct {
	mu       sync.Mutex
	code     string
	hasOwner atomic.Bool
}

// PrepareSetup checks whether an owner exists. If not, it returns a fresh
// one-time setup code for the caller to print; the wizard requires it.
func (a *API) PrepareSetup(ctx context.Context) (code string, required bool, err error) {
	n, err := a.store.DB.NewSelect().Model((*store.User)(nil)).Where("role = ?", "owner").Count(ctx)
	if err != nil {
		return "", false, err
	}
	if n > 0 {
		a.setup.hasOwner.Store(true)
		return "", false, nil
	}
	a.setup.mu.Lock()
	defer a.setup.mu.Unlock()
	if a.setup.code == "" {
		a.setup.code = newSetupCode()
	}
	return a.setup.code, true, nil
}

// newSetupCode returns 12 characters from an alphabet without look-alikes
// (no 0/O, 1/I/L), since people copy it from a terminal: about 60 bits.
func newSetupCode() string {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	out := make([]byte, 0, 12)
	buf := make([]byte, 1)
	for len(out) < 12 {
		if _, err := rand.Read(buf); err != nil {
			panic(err)
		}
		// Reject bytes past the last full multiple of the alphabet size, so
		// every character is equally likely.
		if int(buf[0]) < 256-256%len(alphabet) {
			out = append(out, alphabet[int(buf[0])%len(alphabet)])
		}
	}
	return string(out)
}

// MountAdmin adds the admin API routes to r.
func (a *API) MountAdmin(r chi.Router) {
	r.Route("/admin/api/v1", func(r chi.Router) {
		r.Use(a.sessionMiddleware, a.csrf, a.generalLimit)

		r.Get("/setup", a.h(a.getSetup))
		r.Post("/setup", a.h(a.postSetup))

		r.Group(func(r chi.Router) {
			r.Use(a.requireSetupDone, a.audit)

			r.Get("/me", a.h(a.staff("support", a.adminMe)))

			r.Get("/event", a.h(a.staff("support", a.adminGetEvent)))
			r.Patch("/event", a.h(a.staff("admin", a.adminPatchEvent)))
			r.Get("/brackets", a.h(a.staff("support", a.adminListBrackets)))
			r.Post("/brackets", a.h(a.staff("admin", a.adminCreateBracket)))
			r.Patch("/brackets/{id}", a.h(a.staff("admin", a.adminUpdateBracket)))
			r.Delete("/brackets/{id}", a.h(a.staff("admin", a.adminDeleteBracket)))

			r.Get("/audit", a.h(a.staff("admin", a.adminListAudit)))

			a.mountAdminChallenges(r)
			a.mountAdminPeople(r)
			a.mountAdminContent(r)
			a.mountAdminScoreboard(r)
			a.mountAdminData(r)
		})

		r.NotFound(func(w http.ResponseWriter, r *http.Request) { writeError(w, errNotFound) })
	})
}

// staff requires a signed-in user with at least the given role.
func (a *API) staff(minRole string, fn handler) handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		u := currentUser(r)
		if u == nil {
			return errUnauthenticated
		}
		if roleRank[u.Role] < roleRank[minRole] {
			return errForbidden("Your role can't do this.")
		}
		return fn(w, r)
	}
}

func isRole(r *http.Request, role string) bool {
	u := currentUser(r)
	return u != nil && roleRank[u.Role] >= roleRank[role]
}

var errSetupRequired = &apiError{Status: 409, Code: "setup_required", Message: "Finish first-run setup at /admin first."}

func (a *API) requireSetupDone(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.setup.hasOwner.Load() {
			if _, required, err := a.PrepareSetup(r.Context()); err != nil || required {
				writeError(w, errSetupRequired)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------- audit

// Keys whose values never reach the audit log.
var redactKeys = map[string]bool{"password": true, "current": true, "next": true, "flag": true, "smtppassword": true, "secretkey": true, "setupcode": true}

// audit writes one audit_log row for every successful write.
func (a *API) audit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Previews only render Markdown; they change nothing. A restore
		// replaces the audit log itself, so its handler writes its own entry.
		if r.Method == http.MethodGet || r.Method == http.MethodHead ||
			strings.HasSuffix(r.URL.Path, "/preview") || strings.HasSuffix(r.URL.Path, "/restore") {
			next.ServeHTTP(w, r)
			return
		}
		const maxRecorded = 64 << 10
		var body []byte
		tooLarge := false
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			body, _ = io.ReadAll(io.LimitReader(r.Body, maxRecorded+1))
			// Hand the handler the whole body: what was read, then the rest.
			r.Body = struct {
				io.Reader
				io.Closer
			}{io.MultiReader(bytes.NewReader(body), r.Body), r.Body}
			tooLarge = len(body) > maxRecorded
		}
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		if ww.Status() < 200 || ww.Status() >= 300 {
			return
		}

		rctx := chi.RouteContext(r.Context())
		pattern := strings.TrimPrefix(rctx.RoutePattern(), "/admin/api/v1")
		var target *string
		if params := routeParams(rctx); params != "" {
			target = &params
		}
		entry := &store.AuditEntry{
			Action: r.Method + " " + pattern, Target: target,
			After: redactedJSON(body, strings.Contains(pattern, "/flags")),
			IP:    strp(clientIP(r)), At: a.now().UTC(),
		}
		if tooLarge {
			s := "(body too large to record)"
			entry.After = &s
		}
		if u := currentUser(r); u != nil {
			entry.ActorID = &u.ID
		}
		// Use a fresh context: the request's may be cancelled once the
		// response is written.
		if _, err := a.store.DB.NewInsert().Model(entry).Exec(context.WithoutCancel(r.Context())); err != nil {
			a.log.Error("audit log write failed", "action", entry.Action, "err", err)
		}
	})
}

func routeParams(rctx *chi.Context) string {
	var parts []string
	for i, k := range rctx.URLParams.Keys {
		if k == "*" {
			continue
		}
		parts = append(parts, k+"="+rctx.URLParams.Values[i])
	}
	return strings.Join(parts, " ")
}

// redactedJSON returns body with secret fields replaced. Flag routes also
// hide "value", since that's the flag itself.
func redactedJSON(body []byte, flagRoute bool) *string {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		s := "(unparsed body)"
		return &s
	}
	var walk func(any) any
	walk = func(x any) any {
		switch t := x.(type) {
		case map[string]any:
			for k, val := range t {
				lk := strings.ToLower(k)
				if redactKeys[lk] || (flagRoute && lk == "value") {
					t[k] = "[redacted]"
				} else {
					t[k] = walk(val)
				}
			}
		case []any:
			for i := range t {
				t[i] = walk(t[i])
			}
		}
		return x
	}
	out, _ := json.Marshal(walk(v))
	s := string(out)
	return &s
}

// ---------------------------------------------------------------- setup

func (a *API) getSetup(w http.ResponseWriter, r *http.Request) error {
	_, required, err := a.PrepareSetup(r.Context())
	if err != nil {
		return err
	}
	// demoAccount is null except on demo installs (flagfall seed sample),
	// where the admin sign-in page offers the demo owner login.
	var demo *store.DemoAccount
	if !required {
		if ev, err := a.store.LoadEvent(r.Context()); err == nil {
			demo = ev.ParsedSettings().DemoAdmin
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"required": required, "demoAccount": demo})
	return nil
}

func (a *API) postSetup(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var in struct {
		SetupCode  string `json:"setupCode"`
		Name       string `json:"name"`
		Email      string `json:"email"`
		Password   string `json:"password"`
		EventName  string `json:"eventName"`
		LoadSample bool   `json:"loadSample"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if ok, wait := a.limitSignIn.Allow("setup:" + clientIP(r)); !ok {
		return errRateLimited(wait)
	}
	code, required, err := a.PrepareSetup(ctx)
	if err != nil {
		return err
	}
	if !required {
		return errConflict("Setup is already done. Sign in instead.")
	}
	given := strings.ToUpper(strings.TrimSpace(in.SetupCode))
	if subtle.ConstantTimeCompare([]byte(given), []byte(code)) != 1 {
		return errValidation(map[string]string{"setupCode": "That isn't the setup code. It's printed in the server log at startup."})
	}
	eventName := strings.TrimSpace(in.EventName)
	if eventName == "" || len(eventName) > 80 {
		return errValidation(map[string]string{"eventName": "Give the event a name (up to 80 characters)."})
	}

	var owner *store.User
	err = a.store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if n, err := tx.NewSelect().Model((*store.User)(nil)).Where("role = ?", "owner").Count(ctx); err != nil {
			return err
		} else if n > 0 {
			return errConflict("Setup is already done. Sign in instead.")
		}
		u, err := a.newAccount(ctx, tx, in.Name, in.Email, in.Password, "", "owner")
		if err != nil {
			return err
		}
		now := a.now().UTC()
		if _, err := tx.NewUpdate().Model(u).Set("verified_at = ?", now).WherePK().Exec(ctx); err != nil {
			return err
		}
		owner = u
		return nil
	})
	if err != nil {
		return err
	}
	a.setup.hasOwner.Store(true)
	a.setup.mu.Lock()
	a.setup.code = ""
	a.setup.mu.Unlock()

	out := map[string]any{"sampleLoaded": false}
	if in.LoadSample {
		if _, err := seed.Sample(ctx, a.store, a.cfg.Secret(), FilesDir(a.cfg.DataDir), a.now()); err != nil {
			out["warning"] = "Your account was created, but the sample event couldn't be loaded: " + err.Error()
		} else {
			out["sampleLoaded"] = true
			a.boards.Invalidate()
		}
	}
	// Set the name last, since the sample overwrites it. Installs set up
	// through the wizard never get the demo logins (seed.DemoLogins).
	if _, err := a.store.DB.NewUpdate().Model((*store.Event)(nil)).
		Set("name = ?", eventName).Set("settings = ?", "{}").Set("updated_at = ?", a.now().UTC()).
		Where("id = 1").Exec(ctx); err != nil {
		return err
	}
	if err := a.startSession(ctx, w, r, owner); err != nil {
		return err
	}
	a.writeAudit(ctx, r, owner.ID, "POST /setup", nil, map[string]any{"name": owner.Name, "email": owner.Email, "eventName": eventName, "loadSample": in.LoadSample})
	writeJSON(w, http.StatusCreated, out)
	return nil
}

// writeAudit records an action outside the audit middleware.
func (a *API) writeAudit(ctx context.Context, r *http.Request, actorID int64, action string, target *string, after any) {
	b, _ := json.Marshal(after)
	s := string(b)
	entry := &store.AuditEntry{ActorID: &actorID, Action: action, Target: target, After: &s, IP: strp(clientIP(r)), At: a.now().UTC()}
	if _, err := a.store.DB.NewInsert().Model(entry).Exec(context.WithoutCancel(ctx)); err != nil {
		a.log.Error("audit log write failed", "action", action, "err", err)
	}
}

func (a *API) adminMe(w http.ResponseWriter, r *http.Request) error {
	u := currentUser(r)
	writeJSON(w, http.StatusOK, map[string]any{"id": idStr(u.ID), "name": u.Name, "email": u.Email, "role": u.Role})
	return nil
}
