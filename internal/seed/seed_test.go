package seed_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/flagfall/flagfall/internal/auth"
	"github.com/flagfall/flagfall/internal/scoring"
	"github.com/flagfall/flagfall/internal/seed"
	"github.com/flagfall/flagfall/internal/store"
	"github.com/flagfall/flagfall/internal/store/storetest"
)

func TestSampleMatchesTheDesigns(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	secret := []byte(strings.Repeat("s", 32))

	sum, err := seed.Sample(ctx, s, secret, t.TempDir(), time.Now())
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

	var demo store.User
	if err := s.DB.NewSelect().Model(&demo).Where("email_key = ?", seed.DemoEmail).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, _ := auth.CheckPassword(demo.PasswordHash, seed.DemoPassword); !ok {
		t.Error("demo password doesn't work")
	}

	if _, err := seed.Sample(ctx, s, secret, t.TempDir(), time.Now()); !errors.Is(err, seed.ErrNotEmpty) {
		t.Errorf("second seed: err = %v, want ErrNotEmpty", err)
	}
}
