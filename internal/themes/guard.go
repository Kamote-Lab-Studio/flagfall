package themes

import (
	_ "embed"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The theme guard (guard.js) catches a theme that loads but breaks in the
// browser: if the page is still blank a few seconds after loading, it
// reports the failure and reloads with FallbackCookie, and the server sends
// that browser the default theme until the cookie expires (15 minutes) or
// another theme or version is published.

//go:embed guard.js
var guardJS []byte

// GuardPath is where the guard script is served.
const GuardPath = "/theme-guard.js"

// FallbackCookie names the theme and version ("neon:3") a browser saw fail.
const FallbackCookie = "flagfall_theme_fallback"

// guardTag is the script tag added at the start of <head>, before the
// theme's own scripts, so it sees their load errors.
func guardTag(v View) string {
	attrs := fmt.Sprintf(` data-theme="%s" data-version="%d"`, html.EscapeString(v.Theme.Manifest.ID), v.Version)
	if v.Preview {
		attrs += " data-preview"
	}
	if v.Theme.Manifest.ID == DefaultID {
		attrs += " data-default"
	}
	return `<script src="` + GuardPath + `"` + attrs + `></script>`
}

func serveGuard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Security-Policy", CSP)
	_, _ = w.Write(guardJS)
}

// browserFallback reports whether r carries a fallback cookie for exactly
// this theme and version.
func browserFallback(r *http.Request, v View) bool {
	c, err := r.Cookie(FallbackCookie)
	if err != nil || v.Theme == nil {
		return false
	}
	val, err := url.QueryUnescape(c.Value)
	if err != nil {
		return false
	}
	return val == v.Theme.Manifest.ID+":"+strconv.Itoa(v.Version)
}

// InjectHeadStart inserts markup just after <head>, before anything else.
func InjectHeadStart(page []byte, markup string) []byte {
	s := string(page)
	i := strings.Index(strings.ToLower(s), "<head")
	if i < 0 {
		return []byte(markup + s)
	}
	j := strings.Index(s[i:], ">")
	if j < 0 {
		return []byte(markup + s)
	}
	at := i + j + 1
	return []byte(s[:at] + markup + s[at:])
}

// ---------------------------------------------------------------- reports

// Failure is a summary of the browser reports for one theme version.
type Failure struct {
	ThemeID string    `json:"themeId"`
	Version int       `json:"version"`
	Reports int       `json:"reports"`
	Clients int       `json:"clients"` // distinct browsers (by address), at most 1,000 counted
	Reason  string    `json:"lastReason"`
	Path    string    `json:"lastPath"`
	At      time.Time `json:"lastAt"`
}

// FailureWindow is how long reports are kept.
const FailureWindow = time.Hour

type failureLog struct {
	mu   sync.Mutex
	byID map[string]*failureEntry
}

type failureEntry struct {
	Failure
	clients map[string]bool
}

// ReportFailure records a browser's report that theme id at version didn't
// start. Reports are only kept for the theme players are getting (the live
// one, or the default) at its current version; anything else is ignored,
// so stale or made-up reports can't raise warnings about other themes.
func (m *Manager) ReportFailure(r *http.Request, id string, version int, reason, path, client string) bool {
	live := m.Live(r.Context())
	if live.Theme == nil {
		return false
	}
	ok := (id == live.Theme.Manifest.ID && version == live.Version) || (id == DefaultID && version == 0)
	if !ok {
		return false
	}
	reason, path = clip(reason, 300), clip(path, 200)
	now := time.Now()
	m.failures.mu.Lock()
	defer m.failures.mu.Unlock()
	if m.failures.byID == nil {
		m.failures.byID = map[string]*failureEntry{}
	}
	key := id + ":" + strconv.Itoa(version)
	e := m.failures.byID[key]
	if e == nil || now.Sub(e.At) > FailureWindow {
		e = &failureEntry{Failure: Failure{ThemeID: id, Version: version}, clients: map[string]bool{}}
		m.failures.byID[key] = e
	}
	e.Reports++
	if len(e.clients) < 1000 {
		e.clients[client] = true
	}
	e.Clients, e.Reason, e.Path, e.At = len(e.clients), reason, path, now
	if e.Reports == 1 {
		m.Logger.Warn("a theme didn't start in a player's browser; that browser gets the default theme for 15 minutes",
			"theme", id, "version", version, "reason", reason)
	}
	return true
}

// Failures returns the reports from the last FailureWindow, newest first.
func (m *Manager) Failures() []Failure {
	m.failures.mu.Lock()
	defer m.failures.mu.Unlock()
	out := []Failure{}
	for key, e := range m.failures.byID {
		if time.Since(e.At) > FailureWindow {
			delete(m.failures.byID, key)
			continue
		}
		out = append(out, e.Failure)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

func clip(s string, n int) string {
	s = strings.ToValidUTF8(strings.TrimSpace(s), "")
	if len(s) > n {
		s = strings.ToValidUTF8(s[:n], "")
	}
	return s
}
