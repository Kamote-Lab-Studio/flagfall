package scoring

import (
	"context"
	"sync"
	"time"

	"github.com/flagfall/flagfall/internal/store"
)

// Snapshot is a consistent set of boards computed from one load.
type Snapshot struct {
	Input Input
	// Full includes everything; staff see it, and it decides flag points.
	Full *Board
	// Public is what players see: cut off at the freeze time when one is set.
	Public *Board
	// Frozen is true when the public board is currently hiding newer solves.
	Frozen    bool
	FreezeAt  *time.Time
	StartsAt  *time.Time
	UpdatedAt time.Time
}

// Cache holds the latest snapshot and rebuilds it after Invalidate.
type Cache struct {
	store *store.Store
	now   func() time.Time

	mu    sync.Mutex
	snap  *Snapshot
	dirty bool
}

// NewCache returns a cache over the given store.
func NewCache(s *store.Store) *Cache {
	return &Cache{store: s, now: time.Now, dirty: true}
}

// Invalidate marks the cached boards stale. Call after any change to
// solves, challenges, teams, hint unlocks, adjustments or event settings.
func (c *Cache) Invalidate() {
	c.mu.Lock()
	c.dirty = true
	c.mu.Unlock()
}

// Get returns the current snapshot, rebuilding it if needed.
func (c *Cache) Get(ctx context.Context) (*Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty && c.snap != nil {
		// The frozen flag depends on the clock, so refresh it on every read.
		if c.snap.FreezeAt != nil {
			c.snap.Frozen = !c.now().Before(*c.snap.FreezeAt)
		}
		return c.snap, nil
	}
	snap, err := c.load(ctx)
	if err != nil {
		return nil, err
	}
	c.snap, c.dirty = snap, false
	return snap, nil
}

func (c *Cache) load(ctx context.Context) (*Snapshot, error) {
	db := c.store.DB
	ev, err := c.store.LoadEvent(ctx)
	if err != nil {
		return nil, err
	}

	var chals []store.Challenge
	if err := db.NewSelect().Model(&chals).Column("id", "slug", "name", "category", "value", "min_value", "decay").Scan(ctx); err != nil {
		return nil, err
	}
	var teams []store.Team
	if err := db.NewSelect().Model(&teams).Column("id", "name", "country", "bracket_id", "hidden", "banned_at").Scan(ctx); err != nil {
		return nil, err
	}
	var solves []store.Solve
	if err := db.NewSelect().Model(&solves).Scan(ctx); err != nil {
		return nil, err
	}
	var unlocks []store.HintUnlock
	if err := db.NewSelect().Model(&unlocks).Scan(ctx); err != nil {
		return nil, err
	}
	var adjs []store.ScoreAdjustment
	if err := db.NewSelect().Model(&adjs).Scan(ctx); err != nil {
		return nil, err
	}

	in := Input{Dynamic: ev.ScoringMode == "dynamic"}
	for _, ch := range chals {
		sc := Challenge{ID: ch.ID, Slug: ch.Slug, Name: ch.Name, Category: ch.Category, Value: ch.Value}
		if ch.MinValue != nil && ch.Decay != nil {
			sc.Dynamic, sc.MinValue, sc.Decay = true, *ch.MinValue, *ch.Decay
		}
		in.Challenges = append(in.Challenges, sc)
	}
	for _, t := range teams {
		st := Team{ID: t.ID, Name: t.Name, BracketID: t.BracketID, Hidden: t.Hidden, Banned: t.BannedAt != nil}
		if t.Country != nil {
			st.Country = *t.Country
		}
		in.Teams = append(in.Teams, st)
	}
	for _, s := range solves {
		ss := Solve{TeamID: s.TeamID, ChallengeID: s.ChallengeID, At: s.SolvedAt}
		if s.UserID != nil {
			ss.UserID = *s.UserID
		}
		in.Solves = append(in.Solves, ss)
	}
	for _, u := range unlocks {
		in.Unlocks = append(in.Unlocks, Unlock{TeamID: u.TeamID, Cost: u.Cost, At: u.UnlockedAt})
	}
	for _, a := range adjs {
		in.Adjustments = append(in.Adjustments, Adjustment{TeamID: a.TeamID, Points: a.Points, At: a.CreatedAt})
	}

	snap := &Snapshot{Input: in, FreezeAt: ev.FreezeAt, StartsAt: ev.StartsAt, UpdatedAt: c.now().UTC()}
	snap.Full = Compute(in, time.Time{})
	if ev.FreezeAt != nil {
		// Everything before the freeze time, which (before the freeze) is everything.
		snap.Public = Compute(in, *ev.FreezeAt)
		snap.Frozen = !c.now().Before(*ev.FreezeAt)
	} else {
		snap.Public = snap.Full
	}
	return snap, nil
}

// Board returns the board a viewer should see.
func (s *Snapshot) Board(staff bool) *Board {
	if staff {
		return s.Full
	}
	return s.Public
}
