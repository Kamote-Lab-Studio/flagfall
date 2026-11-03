package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/flagfall/flagfall/internal/store"
	"github.com/flagfall/flagfall/internal/store/storetest"
)

func TestMigrateAndRoundTrip(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	// Migrating twice is a no-op.
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	b := &store.Bracket{Name: "Open"}
	if _, err := s.DB.NewInsert().Model(b).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if b.ID == 0 {
		t.Fatal("bracket id not set after insert")
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	team := &store.Team{Name: "kernel_panic", NameKey: "kernel_panic", BracketID: b.ID, InviteCode: "abc", PrizeEligible: true, CreatedAt: now}
	if _, err := s.DB.NewInsert().Model(team).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	var got store.Team
	if err := s.DB.NewSelect().Model(&got).Where("id = ?", team.ID).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("created_at = %v, want %v", got.CreatedAt, now)
	}
	if !got.PrizeEligible || got.Hidden {
		t.Errorf("booleans did not round-trip: %+v", got)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	s := storetest.New(t)
	team := &store.Team{Name: "x", NameKey: "x", BracketID: 999, InviteCode: "zzz", CreatedAt: time.Now()}
	if _, err := s.DB.NewInsert().Model(team).Exec(context.Background()); err == nil {
		t.Fatal("insert with a missing bracket should fail")
	}
}

func TestUniqueSolvePerTeam(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	now := time.Now()

	b := &store.Bracket{Name: "Open"}
	s.DB.NewInsert().Model(b).Exec(ctx)
	team := &store.Team{Name: "t", NameKey: "t", BracketID: b.ID, InviteCode: "c", CreatedAt: now}
	s.DB.NewInsert().Model(team).Exec(ctx)
	ch := &store.Challenge{Slug: "s", Name: "S", Category: "misc", DescriptionMD: "d", Value: 50, State: "visible", Requires: "[]", ThemeFields: "{}", Tags: "[]", CreatedAt: now, UpdatedAt: now}
	if _, err := s.DB.NewInsert().Model(ch).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	sub := &store.Submission{TeamID: team.ID, ChallengeID: ch.ID, Correct: true, Submitted: "flag{x}", SubmittedAt: now}
	if _, err := s.DB.NewInsert().Model(sub).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	solve := &store.Solve{TeamID: team.ID, ChallengeID: ch.ID, SubmissionID: sub.ID, SolvedAt: now}
	if _, err := s.DB.NewInsert().Model(solve).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.NewInsert().Model(solve).Exec(ctx); err == nil {
		t.Fatal("second solve for the same team and challenge should fail")
	}
}
