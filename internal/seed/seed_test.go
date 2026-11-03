package seed_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/flagfall/flagfall/internal/auth"
	"github.com/flagfall/flagfall/internal/blobs"
	"github.com/flagfall/flagfall/internal/scoring"
	"github.com/flagfall/flagfall/internal/seed"
	"github.com/flagfall/flagfall/internal/store"
	"github.com/flagfall/flagfall/internal/store/storetest"
)

func TestSampleMatchesTheDesigns(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	secret := []byte(strings.Repeat("s", 32))

	sum, err := seed.Sample(ctx, s, secret, blobs.Local(t.TempDir()), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if sum != (seed.Summary{Challenges: 12, Teams: 312, Players: 828, Solves: 709}) {
		t.Fatalf("summary = %+v", sum)
	}

	snap, err := scoring.NewCache(s).Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Ranks and points printed on design/option-a/teams.html.
	want := []struct {
		name  string
		score int
	}{{"kernel_panic", 2750}, {"0xdeadbeef", 2350}, {"bitflip", 1900}, {"null_pointer", 800}, {"r00tbeer", 750}}
	for i, w := range want {
		e := snap.Public.Entries[i]
		if e.Team.Name != w.name || e.Score != w.score {
			t.Errorf("rank %d = %s %d, want %s %d", i+1, e.Team.Name, e.Score, w.name, w.score)
		}
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
