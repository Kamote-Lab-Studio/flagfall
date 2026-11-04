package api_test

import (
	"strings"
	"testing"
)

// A database that stops answering mustn't look like a signed-out user or a
// wrong password: under load that signed players out for good, because the
// session cookie was cleared on any error.
func TestDatabaseErrorsAreNotAuthErrors(t *testing.T) {
	e := newEnv(t)
	player := e.signUp("hiccup")
	player.must(200, "GET", "/api/v1/me", nil)

	e.store.DB.Close()

	r := player.do("GET", "/api/v1/me", nil)
	if r.status != 500 {
		t.Fatalf("GET /me with the database down: status %d, want 500; body %s", r.status, r.raw)
	}
	for _, c := range r.header.Values("Set-Cookie") {
		if strings.HasPrefix(c, "flagfall_session=") {
			t.Fatalf("the session cookie was touched: %s", c)
		}
	}

	r = e.client().do("POST", "/api/v1/auth/sign-in", map[string]any{"email": "hiccup@example.org", "password": "a long enough password"})
	if r.status != 500 {
		t.Fatalf("sign-in with the database down: status %d, want 500; body %s", r.status, r.raw)
	}
}
