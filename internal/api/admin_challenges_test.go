package api_test

import (
	"archive/zip"
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

// upload posts a multipart form with one "file" field.
func (c *client) upload(status int, path, filename string, data []byte) resp {
	c.e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", filename)
	fw.Write(data)
	mw.Close()
	req, _ := http.NewRequest("POST", c.e.server.URL+path, &buf)
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
		c.e.t.Fatalf("upload %s: status %d, want %d; body %s", path, out.status, status, raw)
	}
	return out
}

func TestAdminChallengeLifecycle(t *testing.T) {
	e := newEnv(t)
	author := e.staff("ari", "author")
	player := e.signUp("pat")
	player.createTeam("pats")

	// New challenges start hidden.
	c := author.must(201, "POST", "/admin/api/v1/challenges", map[string]any{
		"slug": "baby-xor", "name": "Baby XOR", "category": "Crypto", "difficulty": "Easy",
		"value": 200, "description": "XOR **everything**.",
	})
	id := c.str("id")
	if c.str("state") != "hidden" || c.str("difficulty") != "easy" {
		t.Fatalf("created = %s", c.raw)
	}
	player.must(404, "GET", "/api/v1/challenges/baby-xor", nil)

	// Validation: duplicate slug, bad values, half-set dynamic scoring.
	bad := author.must(400, "POST", "/admin/api/v1/challenges", map[string]any{
		"slug": "baby-xor", "name": "", "category": "Crypto", "value": -1, "minValue": 50,
	})
	for _, f := range []string{"slug", "name", "value", "decay"} {
		if bad.str("error", "fields", f) == "" {
			t.Errorf("no error for %s: %s", f, bad.raw)
		}
	}

	// Flags: exact values come back hashed (absent), patterns are shown.
	author.must(201, "POST", "/admin/api/v1/challenges/"+id+"/flags", map[string]any{"kind": "exact", "value": "flag{x0r_is_its_own_inverse}"})
	d := author.must(201, "POST", "/admin/api/v1/challenges/"+id+"/flags", map[string]any{"kind": "pattern", "value": `flag\{x0r_[a-z]+\}`})
	if strings.Contains(string(d.raw), "its_own_inverse") || !strings.Contains(string(d.raw), `x0r_[a-z]+`) {
		t.Fatalf("flags leaked or missing: %s", d.raw)
	}
	author.must(400, "POST", "/admin/api/v1/challenges/"+id+"/flags", map[string]any{"kind": "pattern", "value": "("})
	for _, row := range e.audit() {
		if row.After != nil && strings.Contains(*row.After, "its_own_inverse") {
			t.Fatalf("flag value in audit log: %s", *row.After)
		}
	}

	// Publish it; the player solves it.
	author.must(200, "PATCH", "/admin/api/v1/challenges/"+id, map[string]any{"state": "visible"})
	r := player.must(200, "POST", "/api/v1/challenges/baby-xor/submit", map[string]any{"flag": "flag{x0r_is_its_own_inverse}"})
	if r.str("status") != "correct" {
		t.Fatalf("submit = %s", r.raw)
	}
	if got := author.must(200, "GET", "/admin/api/v1/challenges/"+id, nil); got.body["solves"] != 1.0 || got.body["attempts"] != 1.0 {
		t.Fatalf("stats = %s", got.raw)
	}

	// Solved challenges can't be deleted, only retired.
	author.must(409, "DELETE", "/admin/api/v1/challenges/"+id, nil)
	author.must(200, "PATCH", "/admin/api/v1/challenges/"+id, map[string]any{"state": "retired"})

	// Authors can't touch event settings.
	author.must(403, "PATCH", "/admin/api/v1/event", map[string]any{"name": "x"})

	// An unsolved challenge can be deleted.
	c2 := author.must(201, "POST", "/admin/api/v1/challenges", map[string]any{"slug": "tmp", "name": "Tmp", "category": "Misc", "value": 1})
	author.must(204, "DELETE", "/admin/api/v1/challenges/"+c2.str("id"), nil)
}

func TestAdminHintsKeepUnlocks(t *testing.T) {
	e := newEnv(t)
	author := e.staff("ari", "author")
	ch := e.addChallenge("rev-me", 300, "flag{rev}")
	id := jsonID(ch.ID)
	id = strings.Trim(id, `"`)

	d := author.must(200, "PUT", "/admin/api/v1/challenges/"+id+"/hints", []map[string]any{
		{"cost": 0, "content": "Look at main."}, {"cost": 50, "content": "XOR key is 0x42."},
	})
	hints := d.body["hints"].([]any)
	paid := hints[1].(map[string]any)["id"].(string)

	player := e.signUp("pat")
	player.createTeam("pats")
	player.must(200, "POST", "/api/v1/challenges/rev-me/hints/"+paid+"/unlock", nil)

	// Reword the first hint and reorder; the unlocked one keeps its ID and unlock.
	d = author.must(200, "PUT", "/admin/api/v1/challenges/"+id+"/hints", []map[string]any{
		{"id": paid, "cost": 50, "content": "XOR key is 0x42 (one byte)."},
		{"cost": 0, "content": "Start at main."},
	})
	hints = d.body["hints"].([]any)
	first := hints[0].(map[string]any)
	if first["id"] != paid || first["unlocks"] != 1.0 || len(hints) != 2 {
		t.Fatalf("hints = %s", d.raw)
	}
	// IDs from other challenges are rejected.
	other := e.addChallenge("other", 100, "flag{o}")
	author.must(400, "PUT", "/admin/api/v1/challenges/"+strings.Trim(jsonID(other.ID), `"`)+"/hints", []map[string]any{{"id": paid, "cost": 1, "content": "x"}})
}

func TestAdminFileUpload(t *testing.T) {
	e := newEnv(t)
	author := e.staff("ari", "author")
	ch := e.addChallenge("forensics-1", 100, "flag{f}")
	id := strings.Trim(jsonID(ch.ID), `"`)

	d := author.upload(201, "/admin/api/v1/challenges/"+id+"/files", "../../evil/capture.pcap", []byte("pcap bytes"))
	files := d.body["files"].([]any)
	f := files[0].(map[string]any)
	if f["name"] != "capture.pcap" || f["size"] != 10.0 {
		t.Fatalf("file = %v", f)
	}
	player := e.signUp("pat")
	player.createTeam("pats")
	got := player.do("GET", f["url"].(string), nil)
	if got.status != 200 || string(got.raw) != "pcap bytes" {
		t.Fatalf("download = %d %q", got.status, got.raw)
	}
	// Same name replaces.
	d = author.upload(201, "/admin/api/v1/challenges/"+id+"/files", "capture.pcap", []byte("newer"))
	if files := d.body["files"].([]any); len(files) != 1 {
		t.Fatalf("files = %s", d.raw)
	}
	fid := d.body["files"].([]any)[0].(map[string]any)["id"].(string)
	author.must(200, "DELETE", "/admin/api/v1/challenges/"+id+"/files/"+fid, nil)
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, _ := zw.Create(name)
		io.WriteString(w, body)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAdminImport(t *testing.T) {
	e := newEnv(t)
	author := e.staff("ari", "author")
	yml := "format: 1\nslug: %s\nname: %s\ncategory: Misc\ndifficulty: Easy\nvalue: 100\ndescription: Hi.\nflags:\n  - type: exact\n    value: \"flag{%s}\"\n"
	pack := zipOf(t, map[string]string{
		"pack/one/challenge.yml": sprintf(yml, "one", "One", "one"),
		"pack/two/challenge.yml": sprintf(yml, "two", "Two", "two"),
	})
	r := author.upload(200, "/admin/api/v1/challenges/import", "pack.zip", pack)
	if len(r.body["created"].([]any)) != 2 {
		t.Fatalf("import = %s", r.raw)
	}
	// Re-import updates.
	r = author.upload(200, "/admin/api/v1/challenges/import", "pack.zip", pack)
	if len(r.body["updated"].([]any)) != 2 {
		t.Fatalf("re-import = %s", r.raw)
	}
	// Problems are listed and nothing is imported.
	broken := zipOf(t, map[string]string{"three/challenge.yml": "format: 1\nslug: Bad Slug\n"})
	r = author.upload(400, "/admin/api/v1/challenges/import", "broken.zip", broken)
	if probs, _ := r.body["error"].(map[string]any)["problems"].([]any); len(probs) == 0 {
		t.Fatalf("problems = %s", r.raw)
	}
	author.upload(400, "/admin/api/v1/challenges/import", "notzip.zip", []byte("hello"))
}

func TestAdminPreviewSanitizes(t *testing.T) {
	e := newEnv(t)
	s := e.staff("sue", "support")
	r := s.must(200, "POST", "/admin/api/v1/preview", map[string]any{"markdown": "**hi** <script>alert(1)</script>"})
	if !strings.Contains(r.str("html"), "<strong>hi</strong>") || strings.Contains(r.str("html"), "<script") {
		t.Fatalf("preview = %s", r.raw)
	}
}
