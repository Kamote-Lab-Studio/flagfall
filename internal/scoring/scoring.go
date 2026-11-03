// Package scoring computes the scoreboard from solves.
//
// Scores are never stored. Each computation starts from the raw facts
// (challenges, teams, solves, hint unlocks, adjustments), so editing a
// challenge's value changes every team's score at once. Compute is a pure
// function; Cache (cache.go) loads the facts from the database and keeps the
// result in memory until something changes.
package scoring

import (
	"math"
	"sort"
	"time"
)

// Challenge holds what scoring needs to know about a challenge.
type Challenge struct {
	ID       int64
	Slug     string
	Name     string
	Category string
	Value    int  // static value, or initial value when Dynamic
	Dynamic  bool // uses MinValue and Decay
	MinValue int
	Decay    int
}

// Team holds what scoring needs to know about a team.
type Team struct {
	ID        int64
	Name      string
	Country   string
	BracketID int64
	// Hidden and banned teams keep their solves but are left off the board,
	// don't lower dynamic values, and can't take first blood.
	Hidden bool
	Banned bool
}

func (t Team) eligible() bool { return !t.Hidden && !t.Banned }

// Solve is a team's first correct submission for a challenge.
type Solve struct {
	TeamID      int64
	ChallengeID int64
	UserID      int64 // 0 if the user was deleted
	At          time.Time
}

// Unlock is a hint purchase.
type Unlock struct {
	TeamID int64
	Cost   int
	At     time.Time
}

// Adjustment is an admin point change.
type Adjustment struct {
	TeamID int64
	Points int
	At     time.Time
}

// Input is everything Compute needs.
type Input struct {
	Challenges  []Challenge
	Teams       []Team
	Solves      []Solve
	Unlocks     []Unlock
	Adjustments []Adjustment
	// Dynamic turns on dynamic scoring for challenges marked Dynamic. When
	// false (event scoring mode "static"), every challenge uses Value.
	Dynamic bool
}

// Board is a computed scoreboard.
type Board struct {
	// Cutoff is the time the board was computed up to (the freeze time for a
	// frozen public board); zero means everything.
	Cutoff time.Time
	// Entries are the ranked teams: eligible teams with at least one solve or
	// a non-zero score, best first.
	Entries    []*TeamScore
	Teams      map[int64]*TeamScore
	Users      map[int64]*UserScore
	Challenges map[int64]*ChallengeStat
}

// TeamScore is one team's result.
type TeamScore struct {
	Team        Team
	Rank        int // 0 when unranked (hidden, banned, or nothing scored)
	Score       int
	SolveCount  int
	FirstBloods int
	LastSolveAt time.Time // zero if none
	// reachedAt is when the team reached its current score; ties go to the earlier time.
	reachedAt  time.Time
	ByCategory map[string]int
	Solves     []SolveResult
}

// SolveResult is a solve with its current worth.
type SolveResult struct {
	Solve
	Points     int
	FirstBlood bool
}

// UserScore is one player's contribution.
type UserScore struct {
	SolveCount  int
	Score       int
	LastSolveAt time.Time
}

// ChallengeStat is per-challenge data shown on the board.
type ChallengeStat struct {
	SolveCount int // solves by eligible teams
	Value      int // current value
	FirstBlood *Solve
}

// Value returns a challenge's worth after `solves` eligible solves.
//
// Dynamic: value = initial - (initial - minimum) * (s / decay)^2, with
// s = solves - 1 so the first solver doesn't lower it, rounded up, and
// never below minimum.
func Value(c Challenge, solves int, dynamicMode bool) int {
	if !dynamicMode || !c.Dynamic || c.Decay <= 0 {
		return c.Value
	}
	s := solves - 1
	if s < 0 {
		s = 0
	}
	v := float64(c.Value) - float64(c.Value-c.MinValue)*math.Pow(float64(s)/float64(c.Decay), 2)
	out := int(math.Ceil(v))
	if out < c.MinValue {
		return c.MinValue
	}
	return out
}

// Compute builds the board from facts up to cutoff (zero = no cutoff).
func Compute(in Input, cutoff time.Time) *Board {
	include := func(t time.Time) bool { return cutoff.IsZero() || !t.After(cutoff) }

	b := &Board{
		Cutoff:     cutoff,
		Teams:      make(map[int64]*TeamScore, len(in.Teams)),
		Users:      map[int64]*UserScore{},
		Challenges: make(map[int64]*ChallengeStat, len(in.Challenges)),
	}
	challenges := make(map[int64]Challenge, len(in.Challenges))
	for _, c := range in.Challenges {
		challenges[c.ID] = c
		b.Challenges[c.ID] = &ChallengeStat{}
	}
	for _, t := range in.Teams {
		b.Teams[t.ID] = &TeamScore{Team: t, ByCategory: map[string]int{}}
	}

	// Solves in time order, so first blood is the first eligible one.
	solves := make([]Solve, 0, len(in.Solves))
	for _, s := range in.Solves {
		if include(s.At) && b.Teams[s.TeamID] != nil && b.Challenges[s.ChallengeID] != nil {
			solves = append(solves, s)
		}
	}
	sort.SliceStable(solves, func(i, j int) bool { return solves[i].At.Before(solves[j].At) })

	for i := range solves {
		s := solves[i]
		if !b.Teams[s.TeamID].Team.eligible() {
			continue
		}
		st := b.Challenges[s.ChallengeID]
		st.SolveCount++
		if st.FirstBlood == nil {
			st.FirstBlood = &solves[i]
		}
	}
	for id, st := range b.Challenges {
		st.Value = Value(challenges[id], st.SolveCount, in.Dynamic)
	}

	for i := range solves {
		s := solves[i]
		ts := b.Teams[s.TeamID]
		c := challenges[s.ChallengeID]
		st := b.Challenges[s.ChallengeID]
		fb := st.FirstBlood != nil && st.FirstBlood.TeamID == s.TeamID
		ts.Score += st.Value
		ts.SolveCount++
		ts.ByCategory[c.Category] += st.Value
		ts.LastSolveAt = s.At
		ts.reachedAt = s.At
		ts.Solves = append(ts.Solves, SolveResult{Solve: s, Points: st.Value, FirstBlood: fb})
		if fb {
			ts.FirstBloods++
		}
		if s.UserID != 0 {
			us := b.Users[s.UserID]
			if us == nil {
				us = &UserScore{}
				b.Users[s.UserID] = us
			}
			us.SolveCount++
			us.Score += st.Value
			us.LastSolveAt = s.At
		}
	}

	for _, u := range in.Unlocks {
		if ts := b.Teams[u.TeamID]; ts != nil && include(u.At) {
			ts.Score -= u.Cost
		}
	}
	for _, a := range in.Adjustments {
		if ts := b.Teams[a.TeamID]; ts != nil && include(a.At) {
			ts.Score += a.Points
			if a.At.After(ts.reachedAt) {
				ts.reachedAt = a.At
			}
		}
	}

	for _, ts := range b.Teams {
		if ts.Team.eligible() && (ts.SolveCount > 0 || ts.Score != 0) {
			b.Entries = append(b.Entries, ts)
		}
	}
	sort.Slice(b.Entries, func(i, j int) bool {
		x, y := b.Entries[i], b.Entries[j]
		if x.Score != y.Score {
			return x.Score > y.Score
		}
		if !x.reachedAt.Equal(y.reachedAt) {
			return x.reachedAt.Before(y.reachedAt)
		}
		return x.Team.ID < y.Team.ID
	})
	for i, ts := range b.Entries {
		ts.Rank = i + 1
	}
	return b
}

// Bracket returns the entries for one bracket, re-ranked from 1.
// The returned ranks are positions in the bracket; TeamScore.Rank stays global.
func (b *Board) Bracket(bracketID int64) []RankedEntry {
	var out []RankedEntry
	for _, ts := range b.Entries {
		if bracketID == 0 || ts.Team.BracketID == bracketID {
			out = append(out, RankedEntry{Rank: len(out) + 1, TeamScore: ts})
		}
	}
	return out
}

// RankedEntry is a TeamScore with a rank within a filtered view.
type RankedEntry struct {
	Rank int
	*TeamScore
}

// Point is one step on a score-over-time line.
type Point struct {
	At    time.Time
	Score int
}

// Series returns score-over-time lines for the given teams, using each
// solve's current value. It starts each line at start with 0 points.
func (b *Board) Series(in Input, teamIDs []int64, start time.Time) map[int64][]Point {
	include := func(t time.Time) bool { return b.Cutoff.IsZero() || !t.After(b.Cutoff) }

	type change struct {
		at    time.Time
		delta int
	}
	want := map[int64][]change{}
	for _, id := range teamIDs {
		want[id] = nil
	}
	for _, s := range in.Solves {
		if _, ok := want[s.TeamID]; ok && include(s.At) {
			if st := b.Challenges[s.ChallengeID]; st != nil {
				want[s.TeamID] = append(want[s.TeamID], change{s.At, st.Value})
			}
		}
	}
	for _, u := range in.Unlocks {
		if _, ok := want[u.TeamID]; ok && include(u.At) {
			want[u.TeamID] = append(want[u.TeamID], change{u.At, -u.Cost})
		}
	}
	for _, a := range in.Adjustments {
		if _, ok := want[a.TeamID]; ok && include(a.At) {
			want[a.TeamID] = append(want[a.TeamID], change{a.At, a.Points})
		}
	}

	out := make(map[int64][]Point, len(want))
	for id, changes := range want {
		sort.SliceStable(changes, func(i, j int) bool { return changes[i].at.Before(changes[j].at) })
		pts := []Point{{At: start, Score: 0}}
		score := 0
		for _, c := range changes {
			score += c.delta
			pts = append(pts, Point{At: c.at, Score: score})
		}
		out[id] = pts
	}
	return out
}
