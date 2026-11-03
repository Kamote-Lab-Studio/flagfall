package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/flagfall/flagfall/internal/store"
	"github.com/flagfall/flagfall/internal/store/storetest"
)

func TestEventState(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	freeze := start.Add(46 * time.Hour)
	end := start.Add(48 * time.Hour)
	ev := &store.Event{StartsAt: &start, FreezeAt: &freeze, EndsAt: &end}

	cases := map[time.Time]string{
		start.Add(-time.Second): store.StateNotStarted,
		start:                   store.StateRunning,
		freeze:                  store.StateFrozen,
		end:                     store.StateOver,
	}
	for now, want := range cases {
		if got := ev.State(now); got != want {
			t.Errorf("State(%v) = %s, want %s", now, got, want)
		}
	}
	if (&store.Event{}).State(start) != store.StateRunning {
		t.Error("event without dates should be running")
	}
}

func TestEnsureEventIsIdempotent(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := s.EnsureEvent(ctx); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.DB.NewSelect().Model((*store.Bracket)(nil)).Count(ctx)
	if err != nil || n != 1 {
		t.Fatalf("brackets = %d, err = %v", n, err)
	}
	ev, err := s.LoadEvent(ctx)
	if err != nil || ev.Name != "Flagfall CTF" || !ev.RegistrationOpen {
		t.Fatalf("event = %+v, err = %v", ev, err)
	}
}
