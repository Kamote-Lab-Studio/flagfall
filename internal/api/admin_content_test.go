package api_test

import (
	"strings"
	"testing"
	"time"
)

func TestAdminAnnouncements(t *testing.T) {
	e := newEnv(t)
	s := e.staff("sue", "support")
	player := e.client()

	now := s.must(201, "POST", "/admin/api/v1/announcements", map[string]any{"title": "Welcome", "body": "Have **fun**.", "pinned": true})
	if now.body["posted"] != true || !strings.Contains(now.str("bodyHtml"), "<strong>fun</strong>") {
		t.Fatalf("announcement = %s", now.raw)
	}
	later := s.must(201, "POST", "/admin/api/v1/announcements", map[string]any{
		"title": "Hint drop", "body": "Soon.", "postAt": e.clock.Now().Add(time.Hour),
	})
	if later.body["posted"] != false {
		t.Fatalf("scheduled = %s", later.raw)
	}
	if list := player.must(200, "GET", "/api/v1/announcements", nil); len(list.list) != 1 {
		t.Fatalf("players see scheduled announcements: %s", list.raw)
	}
	e.clock.Add(2 * time.Hour)
	if list := player.must(200, "GET", "/api/v1/announcements", nil); len(list.list) != 2 {
		t.Fatalf("scheduled announcement didn't appear: %s", list.raw)
	}
	s.must(200, "PATCH", "/admin/api/v1/announcements/"+now.str("id"), map[string]any{"pinned": false})
	s.must(204, "DELETE", "/admin/api/v1/announcements/"+now.str("id"), nil)
	s.must(400, "POST", "/admin/api/v1/announcements", map[string]any{"body": "no title"})
}

func TestScheduledAnnouncementIsPushedLive(t *testing.T) {
	e := newEnv(t)
	s := e.staff("sue", "support")
	since := e.clock.Now()
	s.must(201, "POST", "/admin/api/v1/announcements", map[string]any{"title": "Later", "body": "x", "postAt": since.Add(time.Minute)})

	events, stop := e.subscribe()
	defer stop()
	e.clock.Add(2 * time.Minute)
	e.api.PublishDueForTest(since)
	select {
	case ev := <-events:
		if !strings.Contains(ev, "event: announcement") || !strings.Contains(ev, "Later") {
			t.Fatalf("event = %q", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no announcement event")
	}
}

func TestAdminPages(t *testing.T) {
	e := newEnv(t)
	admin := e.staff("ada", "admin")
	support := e.staff("sue", "support")
	player := e.client()

	admin.must(201, "PUT", "/admin/api/v1/pages/faq", map[string]any{"title": "FAQ", "body": "## Who?\nYou."})
	if p := player.must(200, "GET", "/api/v1/pages/faq", nil); !strings.Contains(p.str("bodyHtml"), "<h2") {
		t.Fatalf("page = %s", p.raw)
	}
	admin.must(200, "PUT", "/admin/api/v1/pages/faq", map[string]any{"visible": false})
	player.must(404, "GET", "/api/v1/pages/faq", nil)
	support.must(403, "PUT", "/admin/api/v1/pages/faq", map[string]any{"title": "x"})
	admin.must(400, "PUT", "/admin/api/v1/pages/Bad%20Slug", map[string]any{"title": "x"})
	admin.must(400, "PUT", "/admin/api/v1/pages/new", map[string]any{"body": "missing title"})
	admin.must(204, "DELETE", "/admin/api/v1/pages/faq", nil)
}

func TestAdminScoreboardAndAdjustments(t *testing.T) {
	e := newEnv(t)
	admin := e.staff("ada", "admin")
	e.addChallenge("a", 100, "flag{a}")
	x := e.signUp("xena")
	xid := x.createTeam("xs").str("id")
	x.must(200, "POST", "/api/v1/challenges/a/submit", map[string]any{"flag": "flag{wrong}"})
	x.must(200, "POST", "/api/v1/challenges/a/submit", map[string]any{"flag": "flag{a}"})

	admin.must(400, "POST", "/admin/api/v1/scoreboard/adjustments", map[string]any{"teamId": xid, "points": 50, "reason": ""})
	admin.must(201, "POST", "/admin/api/v1/scoreboard/adjustments", map[string]any{"teamId": xid, "points": -30, "reason": "Shared a flag"})
	board := admin.must(200, "GET", "/admin/api/v1/scoreboard", nil)
	entry := board.body["entries"].([]any)[0].(map[string]any)
	if entry["score"] != 70.0 || entry["rank"] != 1.0 {
		t.Fatalf("board = %s", board.raw)
	}
	adj := admin.must(200, "GET", "/admin/api/v1/scoreboard/adjustments", nil)
	if len(adj.list) != 1 || adj.list[0].(map[string]any)["actorName"] != "ada" {
		t.Fatalf("adjustments = %s", adj.raw)
	}
	// Hidden teams stay on the staff board, unranked.
	admin.must(200, "PATCH", "/admin/api/v1/teams/"+xid, map[string]any{"hidden": true})
	board = admin.must(200, "GET", "/admin/api/v1/scoreboard", nil)
	entry = board.body["entries"].([]any)[0].(map[string]any)
	if entry["rank"] != nil || entry["hidden"] != true {
		t.Fatalf("hidden team = %v", entry)
	}
	if ct := admin.must(200, "GET", "/admin/api/v1/scoreboard/export/ctftime", nil); len(ct.body["standings"].([]any)) != 0 {
		t.Fatalf("hidden team in CTFtime export: %s", ct.raw)
	}
	admin.must(204, "DELETE", "/admin/api/v1/scoreboard/adjustments/"+adj.list[0].(map[string]any)["id"].(string), nil)

	// Submissions: newest first, correct flags not shown.
	subs := admin.must(200, "GET", "/admin/api/v1/submissions", nil)
	if len(subs.list) != 2 {
		t.Fatalf("submissions = %s", subs.raw)
	}
	first := subs.list[0].(map[string]any)
	if first["correct"] != true || first["submitted"] != "(correct flag)" || strings.Contains(string(subs.raw), "flag{a}") {
		t.Fatalf("correct flag leaked: %s", subs.raw)
	}
	if wrong := admin.must(200, "GET", "/admin/api/v1/submissions?correct=false", nil); len(wrong.list) != 1 || wrong.list[0].(map[string]any)["submitted"] != "flag{wrong}" {
		t.Fatalf("filtered = %s", wrong.raw)
	}

	dash := admin.must(200, "GET", "/admin/api/v1/dashboard", nil)
	if dash.body["teams"] != 1.0 || dash.body["solves"] != 1.0 || dash.body["submissions"] != 2.0 {
		t.Fatalf("dashboard = %s", dash.raw)
	}
}

func TestDashboardWarnsAboutFlaglessChallenges(t *testing.T) {
	e := newEnv(t)
	author := e.staff("ari", "author")
	c := author.must(201, "POST", "/admin/api/v1/challenges", map[string]any{"slug": "noflag", "name": "No Flag", "category": "Misc", "value": 10})
	author.must(200, "PATCH", "/admin/api/v1/challenges/"+c.str("id"), map[string]any{"state": "visible"})
	dash := author.must(200, "GET", "/admin/api/v1/dashboard", nil)
	if !strings.Contains(string(dash.raw), "No Flag is visible but has no flag") {
		t.Fatalf("warnings = %s", dash.raw)
	}
}
