package api_test

import (
	"strconv"
	"strings"
	"testing"
)

func TestPerTeamFlags(t *testing.T) {
	e := newEnv(t)
	author := e.staff("ari", "author")
	c := e.addChallenge("box", 100, "flag{shared}")
	base := "/admin/api/v1/challenges/" + strconv.FormatInt(c.ID, 10)
	x := e.signUp("xena")
	xid := x.createTeam("xs").str("id")
	y := e.signUp("yuri")
	y.createTeam("ys")

	author.must(400, "POST", base+"/team-flags", map[string]any{"prefix": "=cmd", "suffix": "}"})
	view := author.must(200, "POST", base+"/team-flags", map[string]any{"prefix": "box{", "suffix": "}"})
	tf, _ := view.body["teamFlags"].(map[string]any)
	if tf == nil || tf["teams"] != 2.0 || tf["prefix"] != "box{" || len(view.body["flags"].([]any)) != 1 {
		t.Fatalf("challenge = %s", view.raw)
	}
	// Another format needs the old flags gone first; the same one is a no-op.
	author.must(409, "POST", base+"/team-flags", map[string]any{"prefix": "x{", "suffix": "}"})
	author.must(200, "POST", base+"/team-flags", map[string]any{"prefix": "box{", "suffix": "}"})

	list := author.must(200, "GET", base+"/team-flags", nil)
	flagOf := map[string]string{}
	for _, row := range list.list {
		m := row.(map[string]any)
		flagOf[m["teamName"].(string)] = m["flag"].(string)
	}
	if len(flagOf) != 2 || !strings.HasPrefix(flagOf["xs"], "box{") || flagOf["xs"] == flagOf["ys"] {
		t.Fatalf("team flags = %s", list.raw)
	}
	if csv := author.must(200, "GET", base+"/team-flags?format=csv", nil); !strings.Contains(string(csv.raw), "xs,"+flagOf["xs"]) {
		t.Fatalf("csv = %s", csv.raw)
	}

	// A team's own flag solves it; another team's doesn't, and is flagged.
	if r := y.must(200, "POST", "/api/v1/challenges/box/submit", map[string]any{"flag": flagOf["xs"]}); r.str("status") != "incorrect" {
		t.Fatalf("other team's flag = %s", r.raw)
	}
	if r := x.must(200, "POST", "/api/v1/challenges/box/submit", map[string]any{"flag": flagOf["xs"]}); r.str("status") != "correct" {
		t.Fatalf("own flag = %s", r.raw)
	}
	sum := author.must(200, "GET", "/admin/api/v1/submissions/summary", nil)
	var found bool
	for _, al := range sum.body["alerts"].([]any) {
		m := al.(map[string]any)
		if m["kind"] == "team-flag" && m["teams"].([]any)[0] == "ys" && m["teamIds"].([]any)[1] == xid {
			found = true
		}
	}
	if !found {
		t.Fatalf("no team-flag alert: %s", sum.raw)
	}

	// A team that joins later gets its flag when it first submits.
	z := e.signUp("zed")
	z.createTeam("zs")
	z.must(200, "POST", "/api/v1/challenges/box/submit", map[string]any{"flag": "nope"})
	if v := author.must(200, "GET", base, nil); v.body["teamFlags"].(map[string]any)["teams"] != 3.0 {
		t.Fatalf("late team = %s", v.raw)
	}

	x.must(403, "GET", base+"/team-flags", nil) // players never see them
	if v := author.must(200, "DELETE", base+"/team-flags", nil); v.body["teamFlags"] != nil {
		t.Fatalf("after delete = %s", v.raw)
	}
	author.must(404, "GET", base+"/team-flags", nil)
}
