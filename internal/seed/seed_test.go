package seed_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Kamote-Lab-Studio/flagfall/internal/auth"
	"github.com/Kamote-Lab-Studio/flagfall/internal/blobs"
	"github.com/Kamote-Lab-Studio/flagfall/internal/scoring"
	"github.com/Kamote-Lab-Studio/flagfall/internal/seed"
	"github.com/Kamote-Lab-Studio/flagfall/internal/store"
	"github.com/Kamote-Lab-Studio/flagfall/internal/store/storetest"
)

func TestSampleMatchesTheDesigns(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	secret := []byte(strings.Repeat("s", 32))

	sum, err := seed.Sample(ctx, s, secret, blobs.Local(t.TempDir()), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Ten teams from design/option-a/teams.html, including the demo player's
	// null_pointer, all starting at 0 points.
	if sum != (seed.Summary{Challenges: 12, Teams: 10, Players: 32, Solves: 0}) {
		t.Fatalf("summary = %+v", sum)
	}

	snap, err := scoring.NewCache(s).Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The scoreboard lists teams with a non-zero score, so it starts empty.
	if len(snap.Public.Entries) != 0 {
		t.Errorf("scoreboard starts with %d teams, want none", len(snap.Public.Entries))
	}

	ev, _ := s.LoadEvent(ctx)
	if ev.State(time.Now()) != store.StateRunning {
		t.Errorf("sample event state = %s", ev.State(time.Now()))
	}
	// The setup wizard loads the sample too, so on its own it adds no
	// public logins.
	if st := ev.ParsedSettings(); st.DemoAccount != nil || st.DemoAdmin != nil {
		t.Errorf("Sample alone advertised demo logins: %+v", st)
	}
	if signsIn(t, s, seed.DemoEmail) {
		t.Error("Sample alone gave the demo player a known password")
	}

	if _, err := seed.Sample(ctx, s, secret, blobs.Local(t.TempDir()), time.Now()); !errors.Is(err, seed.ErrNotEmpty) {
		t.Errorf("second seed: err = %v, want ErrNotEmpty", err)
	}
}

func TestDemoLogins(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	if _, err := seed.Sample(ctx, s, []byte(strings.Repeat("s", 32)), blobs.Local(t.TempDir()), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := seed.DemoLogins(ctx, s, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{seed.DemoEmail, seed.DemoOwnerEmail} {
		if !signsIn(t, s, email) {
			t.Errorf("%s can't sign in with the demo password", email)
		}
	}
	var owner store.User
	if err := s.DB.NewSelect().Model(&owner).Where("email_key = ?", seed.DemoOwnerEmail).Scan(ctx); err != nil || owner.Role != "owner" || owner.TeamID != nil {
		t.Errorf("demo owner = %+v, %v", owner, err)
	}
	ev, _ := s.LoadEvent(ctx)
	st := ev.ParsedSettings()
	if st.DemoAccount == nil || st.DemoAccount.Email != seed.DemoEmail || st.DemoAdmin == nil || st.DemoAdmin.Email != seed.DemoOwnerEmail {
		t.Errorf("settings = %+v", st)
	}
	// Never on an install that already has an owner.
	if err := seed.DemoLogins(ctx, s, time.Now()); !errors.Is(err, seed.ErrHasOwner) {
		t.Errorf("second DemoLogins: err = %v, want ErrHasOwner", err)
	}
}

func signsIn(t *testing.T, s *store.Store, email string) bool {
	t.Helper()
	var u store.User
	if err := s.DB.NewSelect().Model(&u).Where("email_key = ?", email).Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	ok, _ := auth.CheckPassword(u.PasswordHash, seed.DemoPassword)
	return ok
}
