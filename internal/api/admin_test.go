package api_test

import (
	"context"
	"strings"
	"testing"

	"github.com/flagfall/flagfall/internal/store"
)

// staff signs up a user and gives them a role. If there's no owner yet, it
// also creates one ("root"), so setup counts as done.
func (e *env) staff(name, role string) *client {
	e.t.Helper()
	ctx := context.Background()
	if role != "owner" {
		if n, _ := e.store.DB.NewSelect().Model((*store.User)(nil)).Where("role = 'owner'").Count(ctx); n == 0 {
			e.staff("root", "owner")
		}
	}
	c := e.signUp(name)
	if _, err := e.store.DB.NewUpdate().Model((*store.User)(nil)).Set("role = ?", role).
		Where("name_key = ?", name).Exec(ctx); err != nil {
		e.t.Fatal(err)
	}
	return c
}

func (e *env) audit() []store.AuditEntry {
	e.t.Helper()
	var rows []store.AuditEntry
	if err := e.store.DB.NewSelect().Model(&rows).Order("id").Scan(context.Background()); err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func TestSetupWizard(t *testing.T) {
	e := newEnv(t)
	code, required, err := e.api.PrepareSetup(context.Background())
	if err != nil || !required || len(code) != 12 {
		t.Fatalf("PrepareSetup = %q, %v, %v", code, required, err)
	}

	anon := e.client()
	if r := anon.must(200, "GET", "/admin/api/v1/setup", nil); r.body["required"] != true {
		t.Fatalf("setup = %s", r.raw)
	}
	// Everything else waits for setup.
	if r := anon.do("GET", "/admin/api/v1/me", nil); r.status != 409 || r.str("error", "code") != "setup_required" {
		t.Fatalf("before setup: %d %s", r.status, r.raw)
	}

	body := map[string]any{"setupCode": "WRONGCODE000", "name": "olive", "email": "olive@example.org",
		"password": "a long enough password", "eventName": "Olive CTF", "loadSample": false}
	if r := anon.must(400, "POST", "/admin/api/v1/setup", body); r.str("error", "fields", "setupCode") == "" {
		t.Fatalf("wrong code accepted: %s", r.raw)
	}
	body["setupCode"] = strings.ToLower(code) // case doesn't matter
	anon.must(201, "POST", "/admin/api/v1/setup", body)

	// The owner is signed in and the event is named.
	if me := anon.must(200, "GET", "/admin/api/v1/me", nil); me.str("role") != "owner" {
		t.Fatalf("me = %s", me.raw)
	}
	if ev := anon.must(200, "GET", "/admin/api/v1/event", nil); ev.str("name") != "Olive CTF" {
		t.Fatalf("event = %s", ev.raw)
	}
	// Setup can't run twice, and the old code is gone.
	body["email"] = "other@example.org"
	body["name"] = "other"
	e.client().must(409, "POST", "/admin/api/v1/setup", body)
	if _, required, _ := e.api.PrepareSetup(context.Background()); required {
		t.Fatal("setup still required")
	}

	// The audit log has the setup, without the password or code.
	rows := e.audit()
	if len(rows) != 1 || rows[0].Action != "POST /setup" || strings.Contains(*rows[0].After, "long enough") || strings.Contains(*rows[0].After, code) {
		t.Fatalf("audit = %+v", rows)
	}
}

func TestSetupWithSample(t *testing.T) {
	e := newEnv(t)
	code, _, _ := e.api.PrepareSetup(context.Background())
	c := e.client()
	r := c.must(201, "POST", "/admin/api/v1/setup", map[string]any{"setupCode": code, "name": "olive", "email": "olive@example.org",
		"password": "a long enough password", "eventName": "Olive CTF", "loadSample": true})
	if r.body["sampleLoaded"] != true {
		t.Fatalf("setup = %s", r.raw)
	}
	ev := c.must(200, "GET", "/api/v1/event", nil)
	if ev.str("name") != "Olive CTF" || ev.body["demoAccount"] != nil {
		t.Fatalf("a wizard install must keep its own name and not advertise the demo login: %s", ev.raw)
	}
	if b := c.must(200, "GET", "/api/v1/scoreboard", nil); len(b.body["entries"].([]any)) == 0 || b.body["entries"].([]any)[0].(map[string]any)["team"].(map[string]any)["name"] != "kernel_panic" {
		t.Fatal("sample teams missing")
	}
}

func TestAdminRoles(t *testing.T) {
	e := newEnv(t)
	e.staff("owen", "owner")
	player := e.signUp("pat")
	support := e.staff("sue", "support")
	admin := e.staff("ada", "admin")

	e.client().must(401, "GET", "/admin/api/v1/me", nil)
	player.must(403, "GET", "/admin/api/v1/me", nil)
	if me := support.must(200, "GET", "/admin/api/v1/me", nil); me.str("role") != "support" {
		t.Fatalf("me = %s", me.raw)
	}
	support.must(200, "GET", "/admin/api/v1/event", nil)
	support.must(403, "PATCH", "/admin/api/v1/event", map[string]any{"name": "nope"})
	support.must(403, "GET", "/admin/api/v1/audit", nil)
	admin.must(200, "PATCH", "/admin/api/v1/event", map[string]any{"name": "Renamed"})
	// Failed requests aren't audited; the one successful write is.
	if rows := e.audit(); len(rows) != 1 || rows[0].Action != "PATCH /event" {
		t.Fatalf("audit = %+v", rows)
	}
}

func TestAdminPatchEvent(t *testing.T) {
	e := newEnv(t)
	admin := e.staff("ada", "admin")
	ev := admin.must(200, "PATCH", "/admin/api/v1/event", map[string]any{
		"name": "Spring CTF", "timeZone": "Asia/Manila", "maxTeamSize": 5, "scoringMode": "dynamic",
		"registrationOpen": false, "flagRateLimit": 20, "freezeAt": e.start.Add(40 * 3600e9),
	})
	if ev.str("timeZone") != "Asia/Manila" || ev.body["maxTeamSize"] != 5.0 || ev.body["registrationOpen"] != false {
		t.Fatalf("event = %s", ev.raw)
	}
	// Players see the change.
	if p := e.client().must(200, "GET", "/api/v1/event", nil); p.str("name") != "Spring CTF" || p.str("scoringMode") != "dynamic" {
		t.Fatalf("player event = %s", p.raw)
	}
	// Unfreeze with null.
	if ev := admin.must(200, "PATCH", "/admin/api/v1/event", map[string]any{"freezeAt": nil}); ev.body["freezeAt"] != nil {
		t.Fatalf("freeze not cleared: %s", ev.raw)
	}

	bad := admin.must(400, "PATCH", "/admin/api/v1/event", map[string]any{
		"timeZone": "Mars/Olympus", "maxTeamSize": 0, "scoringMode": "weird", "endsAt": e.start.Add(-3600e9),
	})
	for _, f := range []string{"timeZone", "maxTeamSize", "scoringMode", "endsAt"} {
		if bad.str("error", "fields", f) == "" {
			t.Errorf("no error for %s: %s", f, bad.raw)
		}
	}
	if r := admin.must(400, "PATCH", "/admin/api/v1/event", map[string]any{"bogus": 1}); !strings.Contains(r.str("error", "message"), "bogus") {
		t.Fatalf("unknown field: %s", r.raw)
	}
}

func TestAdminBrackets(t *testing.T) {
	e := newEnv(t)
	admin := e.staff("ada", "admin")
	b := admin.must(201, "POST", "/admin/api/v1/brackets", map[string]any{"name": "Students", "description": "Enrolled players"})
	id := b.str("id")
	if b := admin.must(200, "PATCH", "/admin/api/v1/brackets/"+id, map[string]any{"description": "Enrolled students"}); b.str("description") != "Enrolled students" {
		t.Fatalf("bracket = %s", b.raw)
	}
	list := admin.must(200, "GET", "/admin/api/v1/brackets", nil)
	if len(list.list) != 2 {
		t.Fatalf("brackets = %s", list.raw)
	}

	// A bracket with teams can't be deleted.
	p := e.signUp("pat")
	p.must(201, "POST", "/api/v1/teams", map[string]any{"name": "pats", "bracketId": id})
	admin.must(409, "DELETE", "/admin/api/v1/brackets/"+id, nil)
}

func TestAuditLogRedactsSecrets(t *testing.T) {
	e := newEnv(t)
	admin := e.staff("ada", "admin")
	admin.must(200, "PATCH", "/admin/api/v1/event", map[string]any{"name": "Secretive"})
	log := admin.must(200, "GET", "/admin/api/v1/audit", nil)
	if len(log.list) != 1 {
		t.Fatalf("audit = %s", log.raw)
	}
	entry := log.list[0].(map[string]any)
	if entry["actorName"] != "ada" || entry["action"] != "PATCH /event" || !strings.Contains(entry["details"].(string), "Secretive") {
		t.Fatalf("entry = %v", entry)
	}
}
