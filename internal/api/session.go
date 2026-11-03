package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/flagfall/flagfall/internal/auth"
	"github.com/flagfall/flagfall/internal/store"
)

const (
	sessionCookie   = "flagfall_session"
	sessionLifetime = 30 * 24 * time.Hour
)

type ctxKey int

const (
	userKey ctxKey = iota
	sessionKey
)

func currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey).(*store.User)
	return u
}

func currentSessionHash(r *http.Request) string {
	s, _ := r.Context().Value(sessionKey).(string)
	return s
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// sessionMiddleware loads the signed-in user, if any, into the context.
func (a *API) sessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		ctx := r.Context()
		idHash := hashToken(c.Value)
		sess := new(store.Session)
		if err := a.store.DB.NewSelect().Model(sess).Where("id_hash = ?", idHash).Scan(ctx); err != nil {
			a.clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}
		now := a.now()
		if now.After(sess.ExpiresAt) {
			a.store.DB.NewDelete().Model(sess).WherePK().Exec(ctx)
			a.clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}
		user := new(store.User)
		if err := a.store.DB.NewSelect().Model(user).Where("id = ?", sess.UserID).Scan(ctx); err != nil || user.BannedAt != nil {
			next.ServeHTTP(w, r)
			return
		}
		if now.Sub(sess.LastSeenAt) > a.sessionTouchAt {
			a.store.DB.NewUpdate().Model(sess).Set("last_seen_at = ?", now.UTC()).WherePK().Exec(ctx)
		}
		ctx = context.WithValue(ctx, userKey, user)
		ctx = context.WithValue(ctx, sessionKey, idHash)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// startSession creates a session for user and sets the cookie.
func (a *API) startSession(ctx context.Context, w http.ResponseWriter, r *http.Request, user *store.User) error {
	token := auth.RandomToken(32)
	now := a.now().UTC()
	ua := r.UserAgent()
	if len(ua) > 512 {
		ua = ua[:512]
	}
	sess := &store.Session{
		IDHash: hashToken(token), UserID: user.ID,
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(sessionLifetime),
		IP: strp(clientIP(r)), UserAgent: strp(ua),
	}
	if _, err := a.store.DB.NewInsert().Model(sess).Exec(ctx); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/",
		Expires: sess.ExpiresAt, HttpOnly: true, Secure: a.cfg.Secure(), SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (a *API) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: a.cfg.Secure(), SameSite: http.SameSiteLaxMode,
	})
}

// csrf rejects state-changing requests that aren't same-origin JSON.
// Browsers can't send cross-site JSON without a CORS preflight, which this
// server never approves, and the Origin check covers older browsers.
func (a *API) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != a.cfg.Origin() {
			writeError(w, errForbidden("Cross-site request blocked."))
			return
		}
		if r.ContentLength != 0 {
			ct := r.Header.Get("Content-Type")
			if ct != "application/json" && !hasPrefix(ct, "application/json;") {
				writeError(w, &apiError{Status: 415, Code: "validation", Message: "Send JSON (Content-Type: application/json)."})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

// generalLimit applies the catch-all rate limit per session or IP.
func (a *API) generalLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/live" {
			next.ServeHTTP(w, r)
			return
		}
		key := "ip:" + clientIP(r)
		if s := currentSessionHash(r); s != "" {
			key = "s:" + s
		}
		if ok, wait := a.limitGeneral.Allow(key); !ok {
			writeError(w, errRateLimited(wait))
			return
		}
		next.ServeHTTP(w, r)
	})
}
