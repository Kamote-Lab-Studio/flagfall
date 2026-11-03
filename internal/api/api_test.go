package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/flagfall/flagfall/internal/api"
	"github.com/flagfall/flagfall/internal/config"
	"github.com/flagfall/flagfall/internal/flags"
	"github.com/flagfall/flagfall/internal/live"
	"github.com/flagfall/flagfall/internal/server"
	"github.com/flagfall/flagfall/internal/store"
	"github.com/flagfall/flagfall/internal/store/storetest"
)

// ---------------------------------------------------------------- harness

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }
func (c *clock) Set(t time.Time)     { c.mu.Lock(); c.now = t; c.mu.Unlock() }

type mailbox struct {
	mu   sync.Mutex
	last string
}

func (m *mailbox) Send(_ context.Context, to, subject, body string) error {
	m.mu.Lock()
	m.last = body
	m.mu.Unlock()
	return nil
}

type env struct {
	t      *testing.T
	store  *store.Store
	cfg    config.Config
	clock  *clock
	mail   *mailbox
	server *httptest.Server
	start  time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	s := storetest.New(t)
	ctx := context.Background()
	if err := s.EnsureEvent(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		PublicURL: "http://ff.test",
		DataDir:   t.TempDir(),
		SecretKey: strings.Repeat("ab", 32),
	}
	if err := cfg.EnsureSecret(); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)
	clk := &clock{now: start.Add(time.Hour)}
	end := start.Add(48 * time.Hour)
	if _, err := s.DB.NewUpdate().Model((*store.Event)(nil)).
		Set("starts_at = ?", start).Set("ends_at = ?", end).Where("id = 1").Exec(ctx); err != nil {
		t.Fatal(err)
	}

	mail := &mailbox{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := api.New(api.Options{Store: s, Config: cfg, Hub: live.NewHub(), Mailer: mail, Logger: logger, Now: clk.Now})
	h := server.New(server.Options{Logger: logger, Mount: []func(chi.Router){a.Mount}})
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return &env{t: t, store: s, cfg: cfg, clock: clk, mail: mail, server: ts, start: start}
}

type client struct {
	e    *env
	http *http.Client
}

func (e *env) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, http: &http.Client{Jar: jar}}
}

type resp struct {
	status int
	header http.Header
	raw    []byte
	body   map[string]any
	list   []any
}

func (r resp) str(path ...string) string {
	var cur any = r.body
	for _, p := range path {
		m, _ := cur.(map[string]any)
		cur = m[p]
	}
	s, _ := cur.(string)
	return s
}

func (r resp) num(path ...string) float64 {
	var cur any = r.body
	for _, p := range path {
		m, _ := cur.(map[string]any)
		cur = m[p]
	}
	f, _ := cur.(float64)
	return f
}

func (c *client) do(method, path string, body any) resp {
	c.e.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.e.server.URL+path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("Origin", c.e.cfg.Origin())
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{status: res.StatusCode, header: res.Header, raw: raw}
	_ = json.Unmarshal(raw, &out.body)
	_ = json.Unmarshal(raw, &out.list)
	return out
}

func (c *client) must(status int, method, path string, body any) resp {
	c.e.t.Helper()
	r := c.do(method, path, body)
	if r.status != status {
		c.e.t.Fatalf("%s %s: status %d, want %d; body %s", method, path, r.status, status, r.raw)
	}
	return r
}

func (e *env) signUp(name string) *client {
	e.t.Helper()
	c := e.client()
	c.must(201, "POST", "/api/v1/auth/sign-up", map[string]any{
		"name": name, "email": name + "@example.org", "password": "a long enough password",
	})
	return c
}

func (e *env) bracketID() string {
	var b store.Bracket
	if err := e.store.DB.NewSelect().Model(&b).Limit(1).Scan(context.Background()); err != nil {
		e.t.Fatal(err)
	}
	return jsonID(b.ID)
}

func jsonID(id int64) string { b, _ := json.Marshal(id); return string(b) }

// addChallenge inserts a visible challenge with one exact flag.
func (e *env) addChallenge(slug string, value int, flag string) *store.Challenge {
	e.t.Helper()
	ctx := context.Background()
	now := e.clock.Now()
	c := &store.Challenge{
		Slug: slug, Name: strings.ToUpper(slug), Category: "Web", DescriptionMD: "Find **the** flag.",
		Value: value, State: "visible", Requires: "[]", ThemeFields: "{}", Tags: "[]", CreatedAt: now, UpdatedAt: now,
	}
	if _, err := e.store.DB.NewInsert().Model(c).Exec(ctx); err != nil {
		e.t.Fatal(err)
	}
	h := flags.Hash(e.cfg.Secret(), flags.Exact, flag)
	if _, err := e.store.DB.NewInsert().Model(&store.Flag{ChallengeID: c.ID, Kind: flags.Exact, ValueHash: &h}).Exec(ctx); err != nil {
		e.t.Fatal(err)
	}
	return c
}

func (e *env) setEvent(column string, value any) {
	e.t.Helper()
	if _, err := e.store.DB.NewUpdate().Model((*store.Event)(nil)).Set(column+" = ?", value).Where("id = 1").Exec(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

func (c *client) createTeam(name string) resp {
	c.e.t.Helper()
	return c.must(201, "POST", "/api/v1/teams", map[string]any{"name": name, "bracketId": c.e.bracketID()})
}

// ---------------------------------------------------------------- tests

func TestFullPlayerFlow(t *testing.T) {
	e := newEnv(t)
	e.addChallenge("cookie-jar", 100, "flag{cookies}")
	e.addChallenge("stack-smasher", 300, "flag{smash}")

	alice := e.signUp("alice")
	me := alice.must(200, "GET", "/api/v1/me", nil)
	if me.str("name") != "alice" || me.body["team"] != nil {
		t.Fatalf("me = %s", me.raw)
	}

	team := alice.createTeam("kernel_panic")
	if team.str("name") != "kernel_panic" || team.body["isCaptain"] != true {
		t.Fatalf("team = %s", team.raw)
	}
	code := team.str("inviteCode")

	bob := e.signUp("bob")
	joined := bob.must(200, "POST", "/api/v1/teams/join", map[string]any{"inviteCode": strings.ToLower(code)})
	if joined.num("memberCount") != 2 {
		t.Fatalf("after join: %s", joined.raw)
	}

	list := bob.must(200, "GET", "/api/v1/challenges", nil)
	if len(list.list) != 2 {
		t.Fatalf("challenges = %s", list.raw)
	}

	r := bob.must(200, "POST", "/api/v1/challenges/cookie-jar/submit", map[string]any{"flag": "flag{nope}"})
	if r.str("status") != "incorrect" {
		t.Fatalf("wrong flag: %s", r.raw)
	}
	r = bob.must(200, "POST", "/api/v1/challenges/cookie-jar/submit", map[string]any{"flag": "  flag{cookies}\n"})
	if r.str("status") != "correct" || r.num("points") != 100 || r.body["firstBlood"] != true {
		t.Fatalf("right flag: %s", r.raw)
	}
	r = alice.must(200, "POST", "/api/v1/challenges/cookie-jar/submit", map[string]any{"flag": "flag{cookies}"})
	if r.str("status") != "already_solved" {
		t.Fatalf("teammate resubmit: %s", r.raw)
	}

	detail := alice.must(200, "GET", "/api/v1/challenges/cookie-jar", nil)
	if detail.body["solved"] != true || detail.num("solveCount") != 1 || detail.str("firstBlood", "teamName") != "kernel_panic" {
		t.Fatalf("detail = %s", detail.raw)
	}
	if !strings.Contains(detail.str("descriptionHtml"), "<strong>the</strong>") {
		t.Errorf("description not rendered: %s", detail.str("descriptionHtml"))
	}

	board := e.client().must(200, "GET", "/api/v1/scoreboard", nil)
	entries := board.body["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("scoreboard = %s", board.raw)
	}
	top := entries[0].(map[string]any)
	if top["score"] != 100.0 || top["rank"] != 1.0 || top["team"].(map[string]any)["name"] != "kernel_panic" {
		t.Fatalf("top entry = %v", top)
	}

	profile := e.client().must(200, "GET", "/api/v1/teams/"+team.str("id"), nil)
	if profile.num("score") != 100 || profile.num("firstBloodCount") != 1 || profile.body["inviteCode"] != nil {
		t.Fatalf("public profile leaked or wrong: %s", profile.raw)
	}

	series := e.client().must(200, "GET", "/api/v1/scoreboard/series", nil)
	if len(series.list) != 1 {
		t.Fatalf("series = %s", series.raw)
	}

	// Everyone leaves; the team stays because it has a solve.
	bob.must(204, "POST", "/api/v1/teams/leave", nil)
	alice.must(204, "POST", "/api/v1/teams/leave", nil)
	kept := e.client().must(200, "GET", "/api/v1/teams/"+team.str("id"), nil)
	if kept.num("score") != 100 || kept.num("memberCount") != 0 {
		t.Fatalf("team with solves should be kept: %s", kept.raw)
	}
}

func TestCSRFProtection(t *testing.T) {
	e := newEnv(t)
	body := strings.NewReader(`{"name":"x","email":"x@example.org","password":"a long enough password"}`)

	req, _ := http.NewRequest("POST", e.server.URL+"/api/v1/auth/sign-up", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	res, _ := http.DefaultClient.Do(req)
	if res.StatusCode != 403 {
		t.Errorf("cross-origin POST: status %d, want 403", res.StatusCode)
	}

	req, _ = http.NewRequest("POST", e.server.URL+"/api/v1/auth/sign-up", strings.NewReader("name=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", e.cfg.Origin())
	res, _ = http.DefaultClient.Do(req)
	if res.StatusCode != 415 {
		t.Errorf("form POST: status %d, want 415", res.StatusCode)
	}
}

func TestAuthErrors(t *testing.T) {
	e := newEnv(t)
	e.signUp("carol")
	c := e.client()

	r := c.must(400, "POST", "/api/v1/auth/sign-up", map[string]any{"name": "Carol", "email": "bad", "password": "short"})
	for _, f := range []string{"name", "email", "password"} {
		if r.str("error", "fields", f) == "" {
			t.Errorf("no field error for %s: %s", f, r.raw)
		}
	}
	c.must(401, "POST", "/api/v1/auth/sign-in", map[string]any{"email": "carol@example.org", "password": "wrong password!!"})
	c.must(401, "POST", "/api/v1/auth/sign-in", map[string]any{"email": "nobody@example.org", "password": "wrong password!!"})
	me := c.must(200, "GET", "/api/v1/me", nil)
	if string(bytes.TrimSpace(me.raw)) != "null" {
		t.Errorf("signed-out /me = %s, want null", me.raw)
	}
	c.must(200, "POST", "/api/v1/auth/sign-in", map[string]any{"email": "CAROL@example.org", "password": "a long enough password"})
	r = c.must(400, "POST", "/api/v1/teams", map[string]any{"name": "x"})
	if r.str("error", "fields", "bracketId") == "" {
		t.Errorf("missing bracket not reported: %s", r.raw)
	}
}

func TestSignOutAndProtectedRoutes(t *testing.T) {
	e := newEnv(t)
	c := e.signUp("dave")
	c.must(204, "POST", "/api/v1/auth/sign-out", nil)
	c.must(401, "POST", "/api/v1/teams", map[string]any{"name": "x", "bracketId": e.bracketID()})
	if r := c.must(401, "GET", "/api/v1/challenges", nil); r.str("error", "code") != "unauthenticated" {
		t.Errorf("error code = %s", r.raw)
	}
}

func TestRegistrationClosed(t *testing.T) {
	e := newEnv(t)
	e.setEvent("registration_open", false)
	r := e.client().must(403, "POST", "/api/v1/auth/sign-up", map[string]any{"name": "eve", "email": "eve@example.org", "password": "a long enough password"})
	if r.str("error", "code") != "registration_closed" {
		t.Errorf("code = %s", r.raw)
	}
}

func TestFlagRateLimit(t *testing.T) {
	e := newEnv(t)
	e.addChallenge("c", 100, "flag{x}")
	e.setEvent("flag_rate_limit", 2)
	p := e.signUp("frank")
	p.createTeam("t1")
	for i := 0; i < 2; i++ {
		if r := p.must(200, "POST", "/api/v1/challenges/c/submit", map[string]any{"flag": "no"}); r.str("status") != "incorrect" {
			t.Fatalf("attempt %d: %s", i, r.raw)
		}
	}
	r := p.must(200, "POST", "/api/v1/challenges/c/submit", map[string]any{"flag": "flag{x}"})
	if r.str("status") != "rate_limited" || r.num("retryAfterSeconds") < 1 {
		t.Fatalf("third attempt: %s", r.raw)
	}
}

func TestEventNotStartedAndOver(t *testing.T) {
	e := newEnv(t)
	e.addChallenge("c", 100, "flag{x}")
	p := e.signUp("gina")
	p.createTeam("t1")

	e.clock.Set(e.start.Add(-time.Minute))
	if ev := e.client().must(200, "GET", "/api/v1/event", nil); ev.str("state") != "not_started" {
		t.Fatalf("state = %s", ev.raw)
	}
	if r := p.must(200, "GET", "/api/v1/challenges", nil); len(r.list) != 0 {
		t.Fatalf("challenges visible before start: %s", r.raw)
	}
	p.must(404, "GET", "/api/v1/challenges/c", nil)

	e.clock.Set(e.start.Add(49 * time.Hour))
	r := p.must(200, "POST", "/api/v1/challenges/c/submit", map[string]any{"flag": "flag{x}"})
	if r.str("status") != "event_not_running" || r.str("state") != "over" {
		t.Fatalf("after end: %s", r.raw)
	}
}

func TestHiddenChallengeIs404(t *testing.T) {
	e := newEnv(t)
	c := e.addChallenge("secret", 100, "flag{x}")
	e.store.DB.NewUpdate().Model(c).Set("state = 'hidden'").WherePK().Exec(context.Background())
	p := e.signUp("hank")
	p.must(404, "GET", "/api/v1/challenges/secret", nil)
	if r := p.must(200, "GET", "/api/v1/challenges", nil); len(r.list) != 0 {
		t.Fatalf("hidden challenge listed: %s", r.raw)
	}
}

func TestScoreboardFreeze(t *testing.T) {
	e := newEnv(t)
	e.addChallenge("a", 100, "flag{a}")
	e.addChallenge("b", 200, "flag{b}")
	p := e.signUp("ivy")
	p.createTeam("t1")
	p.must(200, "POST", "/api/v1/challenges/a/submit", map[string]any{"flag": "flag{a}"})

	freeze := e.clock.Now().Add(time.Minute)
	e.setEvent("freeze_at", freeze)
	e.clock.Add(2 * time.Minute)
	r := p.must(200, "POST", "/api/v1/challenges/b/submit", map[string]any{"flag": "flag{b}"})
	if r.str("status") != "correct" {
		t.Fatalf("submit while frozen: %s", r.raw)
	}

	board := e.client().must(200, "GET", "/api/v1/scoreboard", nil)
	if board.body["frozen"] != true {
		t.Fatalf("not frozen: %s", board.raw)
	}
	if score := board.body["entries"].([]any)[0].(map[string]any)["score"]; score != 100.0 {
		t.Fatalf("frozen board shows %v, want 100 (solve after freeze leaked)", score)
	}
	// The team still sees its own solve marked on the board.
	list := p.must(200, "GET", "/api/v1/challenges", nil)
	for _, item := range list.list {
		m := item.(map[string]any)
		if m["slug"] == "b" && m["solved"] != true {
			t.Error("own post-freeze solve not marked")
		}
	}
}

func TestHints(t *testing.T) {
	e := newEnv(t)
	c := e.addChallenge("h", 100, "flag{h}")
	ctx := context.Background()
	hint := &store.Hint{ChallengeID: c.ID, Cost: 25, ContentMD: "Look at the *cookies*."}
	e.store.DB.NewInsert().Model(hint).Exec(ctx)
	free := &store.Hint{ChallengeID: c.ID, Cost: 0, ContentMD: "Free hint.", Position: 1}
	e.store.DB.NewInsert().Model(free).Exec(ctx)

	p := e.signUp("jay")
	p.createTeam("t1")
	p.must(200, "POST", "/api/v1/challenges/h/submit", map[string]any{"flag": "flag{h}"})

	d := p.must(200, "GET", "/api/v1/challenges/h", nil)
	hints := d.body["hints"].([]any)
	if hints[0].(map[string]any)["content"] != nil || hints[1].(map[string]any)["content"] == nil {
		t.Fatalf("hint content visibility wrong: %s", d.raw)
	}

	path := "/api/v1/challenges/h/hints/" + jsonID(hint.ID) + "/unlock"
	u := p.must(200, "POST", path, nil)
	if !strings.Contains(u.str("content"), "<em>cookies</em>") {
		t.Fatalf("unlock = %s", u.raw)
	}
	p.must(200, "POST", path, nil) // idempotent

	board := e.client().must(200, "GET", "/api/v1/scoreboard", nil)
	if s := board.body["entries"].([]any)[0].(map[string]any)["score"]; s != 75.0 {
		t.Fatalf("score after hint = %v, want 75 (charged once)", s)
	}
}

func TestTeamFullAndCaptainRules(t *testing.T) {
	e := newEnv(t)
	e.setEvent("max_team_size", 2)
	a := e.signUp("kim")
	code := a.createTeam("t1").str("inviteCode")
	b := e.signUp("lee")
	b.must(200, "POST", "/api/v1/teams/join", map[string]any{"inviteCode": code})
	c := e.signUp("max")
	if r := c.must(409, "POST", "/api/v1/teams/join", map[string]any{"inviteCode": code}); r.str("error", "code") != "team_full" {
		t.Fatalf("join full team: %s", r.raw)
	}
	c.must(400, "POST", "/api/v1/teams/join", map[string]any{"inviteCode": "WRONGCODE1"})

	b.must(403, "POST", "/api/v1/teams/mine/invite-code", nil) // not captain
	a.must(409, "POST", "/api/v1/teams/leave", nil)            // captain with members
	me := b.must(200, "GET", "/api/v1/me", nil)
	bobID := me.str("id")
	a.must(200, "POST", "/api/v1/teams/mine/captain", map[string]any{"userId": bobID})
	a.must(204, "POST", "/api/v1/teams/leave", nil)

	newCode := b.must(200, "POST", "/api/v1/teams/mine/invite-code", nil).str("inviteCode")
	if newCode == code {
		t.Error("invite code did not change")
	}
	c.must(400, "POST", "/api/v1/teams/join", map[string]any{"inviteCode": code})
	c.must(200, "POST", "/api/v1/teams/join", map[string]any{"inviteCode": newCode})
}

func TestTeamsLocked(t *testing.T) {
	e := newEnv(t)
	e.setEvent("teams_locked", true)
	p := e.signUp("ned")
	p.must(403, "POST", "/api/v1/teams", map[string]any{"name": "t", "bracketId": e.bracketID()})
}

func TestPasswordReset(t *testing.T) {
	e := newEnv(t)
	p := e.signUp("olga")
	c := e.client()
	c.must(204, "POST", "/api/v1/auth/password-reset", map[string]any{"email": "nobody@example.org"})
	c.must(204, "POST", "/api/v1/auth/password-reset", map[string]any{"email": "olga@example.org"})

	i := strings.Index(e.mail.last, "token=")
	if i < 0 {
		t.Fatalf("no reset link in email: %q", e.mail.last)
	}
	token := strings.Fields(e.mail.last[i+len("token="):])[0]

	c.must(204, "POST", "/api/v1/auth/password-reset/confirm", map[string]any{"token": token, "password": "a brand new passphrase"})
	c.must(400, "POST", "/api/v1/auth/password-reset/confirm", map[string]any{"token": token, "password": "a brand new passphrase"})

	// The old session was ended; the new password works.
	if me := p.must(200, "GET", "/api/v1/me", nil); string(bytes.TrimSpace(me.raw)) != "null" {
		t.Error("old session survived a password reset")
	}
	c.must(200, "POST", "/api/v1/auth/sign-in", map[string]any{"email": "olga@example.org", "password": "a brand new passphrase"})
}

func TestFileDownload(t *testing.T) {
	e := newEnv(t)
	c := e.addChallenge("files", 100, "flag{f}")
	sum := strings.Repeat("a1", 32)
	dir := api.FilesDir(e.cfg.DataDir)
	os.MkdirAll(dir, 0o750)
	os.WriteFile(filepath.Join(dir, sum), []byte("PK-fake-zip"), 0o600)
	e.store.DB.NewInsert().Model(&store.File{ChallengeID: c.ID, Name: "source code.zip", StorageKey: sum, Size: 11, SHA256: sum}).Exec(context.Background())

	p := e.signUp("pat")
	d := p.must(200, "GET", "/api/v1/challenges/files", nil)
	url := d.body["files"].([]any)[0].(map[string]any)["url"].(string)
	r := p.must(200, "GET", url, nil)
	if string(r.raw) != "PK-fake-zip" {
		t.Fatalf("file body = %q", r.raw)
	}
	if !strings.HasPrefix(r.header.Get("Content-Disposition"), "attachment") || !strings.Contains(r.header.Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("unsafe download headers: %v", r.header)
	}
	e.client().must(401, "GET", url, nil)
	p.must(404, "GET", "/files/"+strings.Repeat("0", 64)+"/x.zip", nil)
}

func TestUnknownAPIRouteIsJSON404(t *testing.T) {
	e := newEnv(t)
	r := e.client().must(404, "GET", "/api/v1/nope", nil)
	if r.str("error", "code") != "not_found" {
		t.Errorf("body = %s", r.raw)
	}
}
