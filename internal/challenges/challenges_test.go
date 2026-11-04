package challenges_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Kamote-Lab-Studio/flagfall/challenges/sample"
	"github.com/Kamote-Lab-Studio/flagfall/internal/blobs"
	"github.com/Kamote-Lab-Studio/flagfall/internal/challenges"
	"github.com/Kamote-Lab-Studio/flagfall/internal/flags"
	"github.com/Kamote-Lab-Studio/flagfall/internal/store"
	"github.com/Kamote-Lab-Studio/flagfall/internal/store/storetest"
)

var secret = []byte(strings.Repeat("k", 32))

func TestSamplePackLoads(t *testing.T) {
	specs, err := challenges.Load(sample.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 12 {
		t.Fatalf("loaded %d challenges, want 12", len(specs))
	}
}

func TestValidationReportsEveryProblem(t *testing.T) {
	fsys := fstest.MapFS{
		"a/challenge.yml": {Data: []byte("slug: Bad Slug\nname: A\ncategory: Web\ndescription: x\nvalue: 10\nscoring: {initial: 5, minimum: 1, decay: 1}\nflags: []\n")},
		"b/challenge.yml": {Data: []byte("slug: b\nname: B\ncategory: Web\ndescription: x\nvalue: 10\nflags: [{type: pattern, value: 'flag{('}]\nfiles: [../../etc/passwd]\n")},
		"c/challenge.yml": {Data: []byte("slug: c\nname: C\ncategory: Web\ndescription: x\nvalue: 10\nflags: [{type: exact, value: 'flag{c}'}]\n")},
		"e/challenge.yml": {Data: []byte("slug: c\nname: E\ncategory: Web\ndescription: x\nvalue: 10\nflags: [{type: exact, value: 'flag{e}'}]\n")},
		"d/challenge.yml": {Data: []byte("slug: d\nname: D\ncategory: Web\ndescription: x\nvalue: 10\nflags: [{type: exact, value: 'x'}]\nunknown_field: 1\n")},
	}
	_, err := challenges.Load(fsys, ".")
	var ps challenges.Problems
	if !errors.As(err, &ps) {
		t.Fatalf("err = %v", err)
	}
	text := ps.Error()
	for _, want := range []string{"slug \"Bad Slug\"", "either value or scoring", "at least one flag", "invalid flag pattern", "inside the challenge folder", "also used by", "unknown_field"} {
		if !strings.Contains(text, want) {
			t.Errorf("problems missing %q:\n%s", want, text)
		}
	}
}

func TestApplyCreatesThenUpdates(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	dir := t.TempDir()

	fsys := fstest.MapFS{
		"x/challenge.yml": {Data: []byte("slug: x\nname: X\ncategory: Web\ndescription: desc.md\nvalue: 100\nflags: [{type: case-insensitive, value: 'FLAG{X}'}]\nhints: [{cost: 25, content: 'Try harder.'}]\nfiles: [dist/a.txt]\ntheme_fields: {location: {city: Oslo}}\n")},
		"x/desc.md":       {Data: []byte("Read **this**.")},
		"x/dist/a.txt":    {Data: []byte("hello")},
	}
	specs, err := challenges.Load(fsys, ".")
	if err != nil {
		t.Fatal(err)
	}
	res, err := challenges.Apply(ctx, s, secret, blobs.Local(dir), specs)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created) != 1 {
		t.Fatalf("result = %+v", res)
	}

	var c store.Challenge
	s.DB.NewSelect().Model(&c).Where("slug = 'x'").Scan(ctx)
	if c.DescriptionMD != "Read **this**." || !strings.Contains(c.ThemeFields, "Oslo") {
		t.Errorf("challenge = %+v", c)
	}
	var fl store.Flag
	s.DB.NewSelect().Model(&fl).Where("challenge_id = ?", c.ID).Scan(ctx)
	if !flags.Matches(secret, flags.Flag{Kind: fl.Kind, ValueHash: *fl.ValueHash}, "flag{x}", 1) {
		t.Error("stored flag doesn't match")
	}
	var hint store.Hint
	s.DB.NewSelect().Model(&hint).Where("challenge_id = ?", c.ID).Scan(ctx)
	if b, _ := os.ReadFile(filepath.Join(dir, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824")); string(b) != "hello" {
		t.Error("file not stored by sha256")
	}

	// Re-import with a new value: updated in place, hint id unchanged.
	fsys["x/challenge.yml"].Data = []byte(strings.Replace(string(fsys["x/challenge.yml"].Data), "value: 100", "value: 150", 1))
	specs, _ = challenges.Load(fsys, ".")
	res, err = challenges.Apply(ctx, s, secret, blobs.Local(dir), specs)
	if err != nil || len(res.Updated) != 1 {
		t.Fatalf("second apply: %+v, %v", res, err)
	}
	var c2 store.Challenge
	s.DB.NewSelect().Model(&c2).Where("slug = 'x'").Scan(ctx)
	var hint2 store.Hint
	s.DB.NewSelect().Model(&hint2).Where("challenge_id = ?", c.ID).Scan(ctx)
	if c2.ID != c.ID || c2.Value != 150 || hint2.ID != hint.ID {
		t.Errorf("update: id %d->%d value %d hint %d->%d", c.ID, c2.ID, c2.Value, hint.ID, hint2.ID)
	}
	n, _ := s.DB.NewSelect().Model((*store.Flag)(nil)).Count(ctx)
	if n != 1 {
		t.Errorf("flags after re-import = %d, want 1", n)
	}
}
