package api_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flagfall/flagfall/internal/store"
)

func (e *env) userID(name string) string {
	e.t.Helper()
	u := new(store.User)
	if err := e.store.DB.NewSelect().Model(u).Where("name_key = ?", name).Scan(context.Background()); err != nil {
		e.t.Fatal(err)
	}
	return strings.Trim(jsonID(u.ID), `"`)
}

func TestAdminUserPermissions(t *testing.T) {
	e := newEnv(t)
	support := e.staff("sue", "support")
	admin := e.staff("ada", "admin")
	owner := e.client()
	owner.must(200, "POST", "/api/v1/auth/sign-in", map[string]any{"email": "root@example.org", "password": "a long enough password"})
	e.signUp("pat")

	// Support can verify and ban players, but not touch admins or roles.
	support.must(200, "PATCH", "/admin/api/v1/users/"+e.userID("pat"), map[string]any{"verified": true})
	support.must(403, "PATCH", "/admin/api/v1/users/"+e.userID("ada"), map[string]any{"banned": true})
	if r := admin.must(400, "PATCH", "/admin/api/v1/users/"+e.userID("pat"), map[string]any{"role": "author"}); r.str("error", "fields", "role") == "" {
		t.Fatalf("admin changed a role: %s", r.raw)
	}
	// Owners can.
	if u := owner.must(200, "PATCH", "/admin/api/v1/users/"+e.userID("pat"), map[string]any{"role": "author"}); u.str("role") != "author" {
		t.Fatalf("role = %s", u.raw)
	}
	// The last owner can't step down or be banned.
	owner.must(409, "PATCH", "/admin/api/v1/users/"+e.userID("root"), map[string]any{"role": "admin"})
	owner.must(200, "PATCH", "/admin/api/v1/users/"+e.userID("ada"), map[string]any{"role": "owner"})
	owner.must(200, "PATCH", "/admin/api/v1/users/"+e.userID("root"), map[string]any{"role": "admin"})

	list := support.must(200, "GET", "/admin/api/v1/users?search=PA", nil)
	if list.body["total"] != 1.0 {
		t.Fatalf("search = %s", list.raw)
	}
	if staff := support.must(200, "GET", "/admin/api/v1/staff", nil); len(staff.list) != 4 {
		t.Fatalf("staff = %s", staff.raw)
	}
}

func TestAdminBanSignsOut(t *testing.T) {
	e := newEnv(t)
	support := e.staff("sue", "support")
	pat := e.signUp("pat")
	pat.must(200, "GET", "/api/v1/me", nil)
	support.must(200, "PATCH", "/admin/api/v1/users/"+e.userID("pat"), map[string]any{"banned": true})
	if me := pat.must(200, "GET", "/api/v1/me", nil); string(me.raw) != "null\n" {
		t.Fatalf("banned user still signed in: %s", me.raw)
	}
	pat.must(403, "POST", "/api/v1/auth/sign-in", map[string]any{"email": "pat@example.org", "password": "a long enough password"})
}

func TestAdminPasswordResetLink(t *testing.T) {
	e := newEnv(t)
	support := e.staff("sue", "support")
	e.signUp("pat")
	r := support.must(200, "POST", "/admin/api/v1/users/"+e.userID("pat")+"/password-reset", nil)
	// The test mailer is a real mailer, so it's emailed rather than shown.
	if r.body["emailed"] != true || !strings.Contains(e.mail.last, "/reset-password?token=") {
		t.Fatalf("reset = %s, mail = %q", r.raw, e.mail.last)
	}
}

func TestAdminTeams(t *testing.T) {
	e := newEnv(t)
	support := e.staff("sue", "support")
	alice := e.signUp("alice")
	team := alice.createTeam("alices")
	tid := team.str("id")
	e.signUp("bob")

	// Move bob in (ignores the size limit), make him captain, then remove alice.
	support.must(200, "POST", "/admin/api/v1/teams/"+tid+"/members", map[string]any{"userId": e.userID("bob")})
	d := support.must(200, "PATCH", "/admin/api/v1/teams/"+tid, map[string]any{"captainId": e.userID("bob"), "hidden": true, "name": "renamed"})
	if d.body["team"].(map[string]any)["captainId"] != e.userID("bob") || d.body["team"].(map[string]any)["hidden"] != true {
		t.Fatalf("team = %s", d.raw)
	}
	d = support.must(200, "DELETE", "/admin/api/v1/teams/"+tid+"/members/"+e.userID("alice"), nil)
	if len(d.body["members"].([]any)) != 1 {
		t.Fatalf("members = %s", d.raw)
	}
	// Captain must be a member.
	support.must(400, "PATCH", "/admin/api/v1/teams/"+tid, map[string]any{"captainId": e.userID("alice")})
	// New invite code.
	before := d.body["team"].(map[string]any)["inviteCode"]
	after := support.must(200, "POST", "/admin/api/v1/teams/"+tid+"/invite-code", nil).body["team"].(map[string]any)["inviteCode"]
	if before == after {
		t.Fatal("invite code unchanged")
	}
	// Support can't merge.
	support.must(403, "POST", "/admin/api/v1/teams/"+tid+"/merge", map[string]any{"fromTeamId": tid})
}

func TestAdminMergeKeepsEarlierSolve(t *testing.T) {
	e := newEnv(t)
	admin := e.staff("ada", "admin")
	e.addChallenge("a", 100, "flag{a}")
	e.addChallenge("b", 200, "flag{b}")

	x := e.signUp("xena")
	xid := x.createTeam("xs").str("id")
	y := e.signUp("yuri")
	yid := y.createTeam("ys").str("id")

	y.must(200, "POST", "/api/v1/challenges/a/submit", map[string]any{"flag": "flag{a}"}) // y first on a
	e.clock.Add(time.Minute)
	x.must(200, "POST", "/api/v1/challenges/a/submit", map[string]any{"flag": "flag{a}"})
	y.must(200, "POST", "/api/v1/challenges/b/submit", map[string]any{"flag": "flag{b}"})

	d := admin.must(200, "POST", "/admin/api/v1/teams/"+xid+"/merge", map[string]any{"fromTeamId": yid})
	solves := d.body["solves"].([]any)
	if len(solves) != 2 || len(d.body["members"].([]any)) != 2 {
		t.Fatalf("merged = %s", d.raw)
	}
	first := solves[0].(map[string]any)
	if first["slug"] != "a" || !strings.HasPrefix(first["solvedAt"].(string), e.start.Add(time.Hour).Format("2006-01-02T15:04")) {
		t.Fatalf("kept the later solve: %v", first)
	}
	admin.must(404, "GET", "/admin/api/v1/teams/"+yid, nil)
	if d.body["team"].(map[string]any)["score"] != 300.0 {
		t.Fatalf("score = %s", d.raw)
	}
}
