package challenges_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flagfall/flagfall/internal/blobs"
	"github.com/flagfall/flagfall/internal/challenges"
	"github.com/flagfall/flagfall/internal/seed"
	"github.com/flagfall/flagfall/internal/store"
	"github.com/flagfall/flagfall/internal/store/storetest"
)

func problems(t *testing.T, err error) string {
	t.Helper()
	var ps challenges.Problems
	if !errors.As(err, &ps) {
		t.Fatalf("err = %v, want Problems", err)
	}
	return ps.Error()
}

func TestLoaderCatchesMissingTextAndPlaceholders(t *testing.T) {
	fsys := fstest.MapFS{
		"a/challenge.yml": {Data: []byte("slug: a\nname: A\ncategory: Web\ndescription: README.md\nvalue: 10\nflags: [{type: exact, value: x}]\n" +
			"hints: [{cost: 1, content: hint.md}, {cost: 1, content: 'Inline text mentioning notes.md'}]\nconnection: 'nc {{event_domain}} {{port}}'\n")},
		"b/challenge.yml": {Data: []byte("slug: b\nname: B\ncategory: Web\ndescription: x\nvalue: 10\nflags: [{type: exact, value: x}]\nconnection: 'https://{{ event_domain }}/b'\n")},
		"c/challenge.yml": {Data: []byte("slug: c\nname: C\ncategory: Web\ndescription: x\nvalue: 10\nflags: [{type: exact, value: '<redacted: set before import>'}]\n")},
	}
	text := problems(t, func() error { _, err := challenges.Load(fsys, "."); return err }())
	for _, want := range []string{
		`description: "README.md" isn't in the challenge folder`,
		`hints[0]: "hint.md" isn't in the challenge folder`,
		"unknown placeholder {{port}}",
		"unknown placeholder {{ event_domain }}",
		"replace the redacted placeholder",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "hints[1]") || strings.Contains(text, "placeholder {{event_domain}}") {
		t.Errorf("false positive:\n%s", text)
	}

	// A single challenge folder loads on its own.
	one := fstest.MapFS{"challenge.yml": {Data: []byte("slug: solo\nname: S\ncategory: Web\ndescription: d.md\nvalue: 1\nflags: [{type: pattern, value: 'x.*'}]\n")}, "d.md": {Data: []byte("hi")}}
	specs, err := challenges.LoadFolder(one)
	if err != nil || len(specs) != 1 || specs[0].Slug != "solo" {
		t.Fatalf("LoadFolder = %v, %v", specs, err)
	}
}

const packYML = `slug: %s
name: Pattern %s
category: Misc
difficulty: Medium
author: ada
description: description.md
scoring: {initial: 500, minimum: 100, decay: 20}
flags: [{type: pattern, value: 'flag\{[a-z]+\}'}]
hints: [{cost: 50, content: hints/1.md}]
files: [dist/data.bin]
connection: 'nc {{event_domain}} 31337'
tags: [crypto, intro]
theme_fields: {location: {city: Oslo}}
visible_from: 2026-01-02T03:04:05Z
`

func pack(slugs ...string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for _, s := range slugs {
		fsys[s+"/challenge.yml"] = &fstest.MapFile{Data: []byte(strings.ReplaceAll(packYML, "%s", s))}
		fsys[s+"/description.md"] = &fstest.MapFile{Data: []byte("About " + s)}
		fsys[s+"/hints/1.md"] = &fstest.MapFile{Data: []byte("Look closer.")}
		fsys[s+"/dist/data.bin"] = &fstest.MapFile{Data: []byte{0, 1, 2, byte(len(s))}}
	}
	return fsys
}

func TestPlanAndExportRoundTrip(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	files := blobs.Local(t.TempDir())

	specs, err := challenges.Load(pack("one", "two"), ".")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := challenges.MakePlan(ctx, db, secret, specs)
	if err != nil || len(plan.New) != 2 || len(plan.Changed)+len(plan.Unchanged)+len(plan.Missing) != 0 {
		t.Fatalf("first plan = %+v, %v", plan, err)
	}
	if _, err := challenges.Apply(ctx, db, secret, files, specs); err != nil {
		t.Fatal(err)
	}
	if plan, _ = challenges.MakePlan(ctx, db, secret, specs); len(plan.Unchanged) != 2 {
		t.Fatalf("re-plan = %+v", plan)
	}

	// Export, then load the export: nothing is lost, so nothing changes.
	out := filepath.Join(t.TempDir(), "export")
	res, err := challenges.Export(ctx, db, files, out)
	if err != nil || res.Challenges != 2 || res.Files != 2 || len(res.Redacted) != 0 {
		t.Fatalf("export = %+v, %v", res, err)
	}
	back, err := challenges.LoadFolder(os.DirFS(out))
	if err != nil {
		t.Fatal(err)
	}
	if plan, _ = challenges.MakePlan(ctx, db, secret, back); len(plan.Unchanged) != 2 {
		t.Fatalf("plan of the export = %+v\n%s", plan, plan.Summary(false))
	}
	if _, err := challenges.Export(ctx, db, files, out); err == nil || !strings.Contains(err.Error(), "isn't empty") {
		t.Fatalf("export into a non-empty folder: %v", err)
	}

	// Changes are named; challenges left out are reported.
	edited := pack("one")
	edited["one/challenge.yml"].Data = []byte(strings.Replace(string(edited["one/challenge.yml"].Data), "initial: 500", "initial: 400", 1))
	edited["one/description.md"].Data = []byte("New text")
	specs, _ = challenges.Load(edited, ".")
	plan, _ = challenges.MakePlan(ctx, db, secret, specs)
	if len(plan.Changed) != 1 || !slices.Equal(plan.Changed[0].What, []string{"description", "value"}) || !plan.Changed[0].Scored() {
		t.Fatalf("changed = %+v", plan.Changed)
	}
	if len(plan.Missing) != 1 || plan.Missing[0].Slug != "two" {
		t.Fatalf("missing = %+v", plan.Missing)
	}
}

func TestExportOfSampleNeedsOnlyFlags(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	files := blobs.Local(t.TempDir())
	if _, err := seed.Sample(ctx, db, secret, files, time.Now()); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	res, err := challenges.Export(ctx, db, files, out)
	if err != nil || res.Challenges != 12 || len(res.Redacted) == 0 {
		t.Fatalf("export = %+v, %v", res, err)
	}
	_, err = challenges.LoadFolder(os.DirFS(out))
	for _, line := range strings.Split(problems(t, err), "\n") {
		if !strings.Contains(line, "redacted placeholder") {
			t.Errorf("unexpected problem: %s", line)
		}
	}
}

func TestPruneRetiresSolvedChallenges(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	files := blobs.Local(t.TempDir())
	if _, err := seed.Sample(ctx, db, secret, files, time.Now()); err != nil {
		t.Fatal(err)
	}
	// The sample starts with no solves; give one team a solve so prune has
	// something to retire.
	var team store.Team
	var chal store.Challenge
	if err := db.DB.NewSelect().Model(&team).Limit(1).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.NewSelect().Model(&chal).Where("slug = ?", "baby-rsa").Scan(ctx); err != nil {
		t.Fatal(err)
	}
	sub := &store.Submission{TeamID: team.ID, ChallengeID: chal.ID, Correct: true, Submitted: "(test)", SubmittedAt: time.Now()}
	if _, err := db.DB.NewInsert().Model(sub).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.NewInsert().Model(&store.Solve{TeamID: team.ID, ChallengeID: chal.ID, SubmissionID: sub.ID, SolvedAt: time.Now()}).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	specs, err := challenges.Load(pack("fresh"), ".")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := challenges.Apply(ctx, db, secret, files, specs); err != nil {
		t.Fatal(err)
	}
	// Add an unsolved one that the folder doesn't have.
	extra, _ := challenges.Load(pack("unsolved"), ".")
	if _, err := challenges.Apply(ctx, db, secret, files, extra); err != nil {
		t.Fatal(err)
	}

	plan, err := challenges.MakePlan(ctx, db, secret, specs)
	if err != nil || len(plan.Missing) != 13 {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	deleted, retired, err := challenges.Prune(ctx, db, plan.Missing)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(deleted, "unsolved") || len(retired) == 0 || len(deleted)+len(retired) != 13 {
		t.Fatalf("deleted %v, retired %v", deleted, retired)
	}
	var left []store.Challenge
	db.DB.NewSelect().Model(&left).Scan(ctx)
	for _, c := range left {
		if c.Slug != "fresh" && c.State != "retired" {
			t.Errorf("%s is %s after prune", c.Slug, c.State)
		}
	}
	solves, _ := db.DB.NewSelect().Model((*store.Solve)(nil)).Count(ctx)
	if solves == 0 {
		t.Error("prune removed solves")
	}
}
