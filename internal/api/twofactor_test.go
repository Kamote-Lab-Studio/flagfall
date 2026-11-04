package api_test

import (
	"testing"
	"time"

	"github.com/Kamote-Lab-Studio/flagfall/internal/auth"
)

func TestTwoFactorSignIn(t *testing.T) {
	e := newEnv(t)
	ada := e.staff("ada", "admin") // also makes an owner, "root"
	const pw = "a long enough password"
	code := func(secret string) string {
		t.Helper()
		c, err := auth.TOTPCode(secret, auth.TOTPStep(e.clock.Now()))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	if s := ada.must(200, "GET", "/api/v1/me/two-factor", nil); s.body["enabled"] != false {
		t.Fatalf("status = %s", s.raw)
	}
	ada.must(400, "POST", "/api/v1/me/two-factor/enable", map[string]any{"code": "123456"}) // no setup yet
	setup := ada.must(200, "POST", "/api/v1/me/two-factor/setup", nil)
	secret := setup.str("secret")
	if len(secret) < 32 || setup.str("qrSvg")[:4] != "<svg" {
		t.Fatalf("setup = %.200s", setup.raw)
	}
	ada.must(400, "POST", "/api/v1/me/two-factor/enable", map[string]any{"code": "000000"})
	on := ada.must(200, "POST", "/api/v1/me/two-factor/enable", map[string]any{"code": code(secret)})
	recovery := on.body["recoveryCodes"].([]any)
	if len(recovery) != 10 {
		t.Fatalf("enable = %s", on.raw)
	}
	ada.must(409, "POST", "/api/v1/me/two-factor/setup", nil)

	// The password alone now gets a ticket, not a session.
	c := e.client()
	first := c.must(200, "POST", "/api/v1/auth/sign-in", map[string]any{"email": "ada@example.org", "password": pw})
	ticket := first.str("ticket")
	if first.body["twoFactorRequired"] != true || ticket == "" {
		t.Fatalf("sign-in = %s", first.raw)
	}
	if me := c.must(200, "GET", "/api/v1/me", nil); string(me.raw) != "null\n" {
		t.Fatalf("signed in without a code: %s", me.raw)
	}
	// The code that turned it on was used up; the next one works.
	c.must(400, "POST", "/api/v1/auth/sign-in/code", map[string]any{"ticket": ticket, "code": code(secret)})
	e.clock.Add(30 * time.Second)
	if r := c.do("POST", "/api/v1/auth/sign-in/code", map[string]any{"ticket": ticket + "x", "code": code(secret)}); r.status != 401 {
		t.Fatalf("tampered ticket: %d", r.status)
	}
	if me := c.must(200, "POST", "/api/v1/auth/sign-in/code", map[string]any{"ticket": ticket, "code": code(secret)}); me.str("email") != "ada@example.org" {
		t.Fatalf("code sign-in = %s", me.raw)
	}

	// A recovery code works once.
	r1 := e.client()
	tk := r1.must(200, "POST", "/api/v1/auth/sign-in", map[string]any{"email": "ada@example.org", "password": pw}).str("ticket")
	r1.must(200, "POST", "/api/v1/auth/sign-in/code", map[string]any{"ticket": tk, "code": recovery[0]})
	r2 := e.client()
	tk = r2.must(200, "POST", "/api/v1/auth/sign-in", map[string]any{"email": "ada@example.org", "password": pw}).str("ticket")
	r2.must(400, "POST", "/api/v1/auth/sign-in/code", map[string]any{"ticket": tk, "code": recovery[0]})
	if s := ada.must(200, "GET", "/api/v1/me/two-factor", nil); s.num("recoveryCodesLeft") != 9 {
		t.Fatalf("status = %s", s.raw)
	}

	// Tickets expire.
	tk = r2.must(200, "POST", "/api/v1/auth/sign-in", map[string]any{"email": "ada@example.org", "password": pw}).str("ticket")
	e.clock.Add(6 * time.Minute)
	if r := r2.do("POST", "/api/v1/auth/sign-in/code", map[string]any{"ticket": tk, "code": code(secret)}); r.status != 401 {
		t.Fatalf("expired ticket: %d %s", r.status, r.raw)
	}

	// New recovery codes need the password.
	ada.must(400, "POST", "/api/v1/me/two-factor/recovery-codes", map[string]any{"password": "wrong"})
	if fresh := ada.must(200, "POST", "/api/v1/me/two-factor/recovery-codes", map[string]any{"password": pw}); len(fresh.body["recoveryCodes"].([]any)) != 10 {
		t.Fatalf("recovery codes = %s", fresh.raw)
	}

	// The admin list shows it; staff can't reset their own or a higher role's.
	root := e.client()
	root.must(200, "POST", "/api/v1/auth/sign-in", map[string]any{"email": "root@example.org", "password": pw})
	users := root.must(200, "GET", "/admin/api/v1/users", nil)
	for _, it := range users.body["items"].([]any) {
		if u := it.(map[string]any); (u["name"] == "ada") != (u["twoFactor"] == true) {
			t.Fatalf("users = %s", users.raw)
		}
	}
	adaID := e.userID("ada")
	ada.must(403, "DELETE", "/admin/api/v1/users/"+adaID+"/two-factor", nil)
	root.must(204, "DELETE", "/admin/api/v1/users/"+adaID+"/two-factor", nil)
	if me := e.client().must(200, "POST", "/api/v1/auth/sign-in", map[string]any{"email": "ada@example.org", "password": pw}); me.str("email") != "ada@example.org" {
		t.Fatalf("after reset = %s", me.raw)
	}

	// Turning it off needs the password.
	ada.must(200, "POST", "/api/v1/me/two-factor/setup", nil)
	s2 := ada.must(200, "POST", "/api/v1/me/two-factor/setup", nil).str("secret")
	ada.must(200, "POST", "/api/v1/me/two-factor/enable", map[string]any{"code": code(s2)})
	ada.must(400, "POST", "/api/v1/me/two-factor/disable", map[string]any{"password": "nope"})
	ada.must(204, "POST", "/api/v1/me/two-factor/disable", map[string]any{"password": pw})
	if s := ada.must(200, "GET", "/api/v1/me/two-factor", nil); s.body["enabled"] != false {
		t.Fatalf("after disable = %s", s.raw)
	}
}
