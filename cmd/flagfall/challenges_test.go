package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Kamote-Lab-Studio/flagfall/internal/config"
	"github.com/Kamote-Lab-Studio/flagfall/internal/store"
)

func writeChallenge(t *testing.T, root, slug, flag string, value int) {
	t.Helper()
	dir := filepath.Join(root, slug)
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	yml := "slug: " + slug + "\nname: " + slug + "\ncategory: Web\ndescription: description.md\nvalue: " + strconv.Itoa(value) +
		"\nflags: [{type: exact, value: '" + flag + "'}]\nfiles: [dist/notes.txt]\n"
	for name, body := range map[string]string{"challenge.yml": yml, "description.md": "About " + slug, "dist/notes.txt": "notes"} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func runCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code := run(args, &out, &out)
	return code, out.String()
}

func TestChallengesCommands(t *testing.T) {
	data := t.TempDir()
	t.Setenv("FLAGFALL_DATA_DIR", data)
	t.Setenv("FLAGFALL_DATABASE_URL", "")
	interactive = func() bool { return false }

	pack := t.TempDir()
	writeChallenge(t, pack, "alpha", "flag{a}", 100)
	writeChallenge(t, pack, "beta", "flag{b}", 200)

	if code, out := runCLI(t, "challenges", "check", pack); code != 0 || !strings.Contains(out, "2 challenge(s) OK") {
		t.Fatalf("check: %d %s", code, out)
	}
	if code, out := runCLI(t, "challenges", "check", filepath.Join(pack, "alpha")); code != 0 || !strings.Contains(out, "1 challenge(s) OK: alpha") {
		t.Fatalf("check one: %d %s", code, out)
	}
	if code, out := runCLI(t, "challenges", "import", pack); code != 1 || !strings.Contains(out, "pass --yes") {
		t.Fatalf("import without --yes: %d %s", code, out)
	}
	if code, out := runCLI(t, "challenges", "import", "--yes", pack); code != 0 || !strings.Contains(out, "2 new, 0 updated") {
		t.Fatalf("import: %d %s", code, out)
	}
	if code, out := runCLI(t, "challenges", "import", pack); code != 0 || !strings.Contains(out, "Nothing to do") {
		t.Fatalf("re-import: %d %s", code, out)
	}

	// Interactive: "n" cancels, "y" goes ahead.
	writeChallenge(t, pack, "alpha", "flag{a2}", 150)
	interactive = func() bool { return true }
	stdin = strings.NewReader("n\n")
	if code, out := runCLI(t, "challenges", "import", pack); code != 1 || !strings.Contains(out, "alpha (value, flags)") || !strings.Contains(out, "cancelled") {
		t.Fatalf("cancelled import: %d %s", code, out)
	}
	stdin = strings.NewReader("y\n")
	if code, out := runCLI(t, "challenges", "import", pack); code != 0 || !strings.Contains(out, "0 new, 1 updated") {
		t.Fatalf("confirmed import: %d %s", code, out)
	}
	interactive = func() bool { return false }

	// Export redacts exact flags; importing that back is refused.
	out := filepath.Join(t.TempDir(), "out")
	if code, o := runCLI(t, "challenges", "export", out); code != 0 || !strings.Contains(o, "Exported 2 challenge(s) and 2 file(s)") || !strings.Contains(o, "alpha, beta") {
		t.Fatalf("export: %d %s", code, o)
	}
	if code, o := runCLI(t, "challenges", "check", out); code != 1 || !strings.Contains(o, "redacted placeholder") {
		t.Fatalf("check export: %d %s", code, o)
	}

	// --prune removes what the folder no longer has, but not mid-event.
	if err := os.RemoveAll(filepath.Join(pack, "beta")); err != nil {
		t.Fatal(err)
	}
	setStart(t, time.Now().Add(-time.Hour))
	if code, o := runCLI(t, "challenges", "import", "--yes", "--prune", pack); code != 1 || !strings.Contains(o, "event is running") {
		t.Fatalf("prune while running: %d %s", code, o)
	}
	setStart(t, time.Now().Add(time.Hour))
	if code, o := runCLI(t, "challenges", "import", "--yes", "--prune", pack); code != 0 || !strings.Contains(o, "1 deleted, 0 retired") {
		t.Fatalf("prune: %d %s", code, o)
	}

	if code, o := runCLI(t, "challenges", "frobnicate", pack); code != 2 || !strings.Contains(o, "usage:") {
		t.Fatalf("bad subcommand: %d %s", code, o)
	}
}

func setStart(t *testing.T, at time.Time) {
	t.Helper()
	ctx := context.Background()
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.DB.NewUpdate().Model((*store.Event)(nil)).Set("starts_at = ?", at.UTC()).Where("id = 1").Exec(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCheckNotesUnusedThemeFields(t *testing.T) {
	code, out := runCLI(t, "challenges", "check", filepath.Join("..", "..", "challenges", "sample"))
	if code != 0 || !strings.Contains(out, "12 challenge(s) OK") ||
		!strings.Contains(out, "note: The Amber Phosphor theme doesn't use theme_fields location (12 challenge(s))") {
		t.Fatalf("check sample: %d %s", code, out)
	}
	if strings.Count(out, "location") != 1 {
		t.Fatalf("the note should appear once:\n%s", out)
	}
}
