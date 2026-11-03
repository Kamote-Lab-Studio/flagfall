package api_test

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

// restore posts a backup with a confirm field.
func (c *client) restore(status int, confirm string, data []byte) resp {
	c.e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("confirm", confirm)
	fw, _ := mw.CreateFormFile("file", "backup.zip")
	fw.Write(data)
	mw.Close()
	req, _ := http.NewRequest("POST", c.e.server.URL+"/admin/api/v1/restore", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", c.e.cfg.Origin())
	res, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{status: res.StatusCode, header: res.Header, raw: raw}
	_ = jsonUnmarshal(raw, &out.body)
	if out.status != status {
		c.e.t.Fatalf("restore: status %d, want %d; body %s", out.status, status, raw)
	}
	return out
}

func (e *env) owner() *client {
	e.t.Helper()
	e.staff("ada", "admin") // makes sure the "root" owner exists
	c := e.client()
	c.must(200, "POST", "/api/v1/auth/sign-in", map[string]any{"email": "root@example.org", "password": "a long enough password"})
	return c
}

func TestExport(t *testing.T) {
	e := newEnv(t)
	admin := e.staff("ada", "admin")
	support := e.staff("sue", "support")
	e.addChallenge("a", 100, "flag{a}")
	x := e.signUp("xena")
	x.createTeam("=HYPERLINK(1)")
	x.must(200, "POST", "/api/v1/challenges/a/submit", map[string]any{"flag": "flag{a}"})

	support.must(403, "GET", "/admin/api/v1/export?dataset=users&format=json", nil)
	admin.must(400, "GET", "/admin/api/v1/export?dataset=nope&format=json", nil)
	admin.must(400, "GET", "/admin/api/v1/export?dataset=users&format=xml", nil)

	r := admin.must(200, "GET", "/admin/api/v1/export?dataset=scoreboard&format=csv", nil)
	recs, err := csv.NewReader(bytes.NewReader(r.raw)).ReadAll()
	if err != nil || len(recs) != 2 || recs[0][0] != "rank" || recs[1][0] != "1" || recs[1][4] != "100" {
		t.Fatalf("scoreboard csv = %q, %v", r.raw, err)
	}
	// Spreadsheet formulas are defused.
	if recs[1][1] != "'=HYPERLINK(1)" {
		t.Errorf("team cell = %q", recs[1][1])
	}
	if !strings.Contains(r.header.Get("Content-Disposition"), "flagfall-scoreboard-") {
		t.Errorf("disposition = %q", r.header.Get("Content-Disposition"))
	}

	users := admin.must(200, "GET", "/admin/api/v1/export?dataset=users&format=json", nil)
	if strings.Contains(string(users.raw), "argon2") || !strings.Contains(string(users.raw), "xena@example.org") {
		t.Fatalf("users = %s", users.raw)
	}
	subs := admin.must(200, "GET", "/admin/api/v1/export?dataset=submissions&format=json", nil)
	if strings.Contains(string(subs.raw), "flag{a}") {
		t.Fatalf("correct flag in export: %s", subs.raw)
	}
	for _, ds := range []string{"teams", "solves"} {
		admin.must(200, "GET", "/admin/api/v1/export?dataset="+ds+"&format=csv", nil)
	}
}

func TestBackupAndRestore(t *testing.T) {
	e := newEnv(t)
	owner := e.owner()
	admin := e.staff("ada2", "admin")
	e.addChallenge("a", 100, "flag{a}")
	x := e.signUp("xena")
	x.createTeam("xs")
	name := owner.must(200, "GET", "/admin/api/v1/event", nil).str("name")

	admin.must(403, "GET", "/admin/api/v1/backup", nil)
	b := owner.must(200, "GET", "/admin/api/v1/backup", nil)
	if b.header.Get("Content-Type") != "application/zip" {
		t.Fatalf("content type = %q", b.header.Get("Content-Type"))
	}
	zr, err := zip.NewReader(bytes.NewReader(b.raw), int64(len(b.raw)))
	if err != nil || zr.File[len(zr.File)-1].Name != "manifest.json" {
		t.Fatalf("backup isn't a zip with a manifest: %v", err)
	}

	if audit := string(owner.must(200, "GET", "/admin/api/v1/audit", nil).raw); !strings.Contains(audit, "GET /backup") {
		t.Errorf("backup download not audited: %s", audit)
	}

	// Things change after the backup...
	e.signUp("yuri").createTeam("ys")
	owner.must(200, "PATCH", "/admin/api/v1/event", map[string]any{"name": "Renamed"})

	// ...a restore needs the current name, then brings the old state back.
	owner.restore(400, name, b.raw)
	owner.restore(400, "Renamed", []byte("not a zip"))
	r := owner.restore(200, "Renamed", b.raw)
	if r.body["restored"] != true {
		t.Fatalf("restore = %s", r.raw)
	}
	// Everyone was signed out, the restorer included.
	owner.must(401, "GET", "/admin/api/v1/me", nil)
	owner = e.client()
	owner.must(200, "POST", "/api/v1/auth/sign-in", map[string]any{"email": "root@example.org", "password": "a long enough password"})
	if n := owner.must(200, "GET", "/admin/api/v1/event", nil).str("name"); n != name {
		t.Errorf("event name = %q, want %q", n, name)
	}
	if teams := owner.must(200, "GET", "/admin/api/v1/teams", nil); teams.body["total"] != 1.0 {
		t.Errorf("teams = %s", teams.raw)
	}
	// The restore is recorded in the restored audit log.
	if audit := string(owner.must(200, "GET", "/admin/api/v1/audit", nil).raw); !strings.Contains(audit, "POST /restore") {
		t.Errorf("audit = %s", audit)
	}
}

func TestReset(t *testing.T) {
	e := newEnv(t)
	owner := e.owner()
	e.addChallenge("a", 100, "flag{a}")
	x := e.signUp("xena")
	x.createTeam("xs")
	x.must(200, "POST", "/api/v1/challenges/a/submit", map[string]any{"flag": "flag{a}"})
	name := owner.must(200, "GET", "/admin/api/v1/event", nil).str("name")

	e.staff("sue", "support").must(403, "POST", "/admin/api/v1/reset", map[string]any{"confirm": name})
	owner.must(400, "POST", "/admin/api/v1/reset", map[string]any{"confirm": "wrong"})
	r := owner.must(200, "POST", "/admin/api/v1/reset", map[string]any{"confirm": name})
	if r.body["teams"] != 1.0 || r.body["players"] != 1.0 || r.body["solves"] != 1.0 {
		t.Fatalf("reset = %s", r.raw)
	}
	dash := owner.must(200, "GET", "/admin/api/v1/dashboard", nil)
	if dash.body["teams"] != 0.0 || dash.body["solves"] != 0.0 || len(dash.body["challenges"].([]any)) != 1 {
		t.Fatalf("after reset = %s", dash.raw)
	}
	// Staff keep their accounts.
	if staff := owner.must(200, "GET", "/admin/api/v1/staff", nil); len(staff.list) != 3 {
		t.Fatalf("staff = %s", staff.raw)
	}
	x.must(200, "GET", "/api/v1/me", nil) // signed out: the session went with the account
}
