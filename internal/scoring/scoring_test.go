package scoring

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

func fixture() Input {
	return Input{
		Challenges: []Challenge{
			{ID: 1, Slug: "web", Category: "Web", Value: 100},
			{ID: 2, Slug: "pwn", Category: "Pwn", Value: 300},
			{ID: 3, Slug: "dyn", Category: "Crypto", Value: 500, Dynamic: true, MinValue: 100, Decay: 2},
		},
		Teams: []Team{
			{ID: 10, Name: "alpha", BracketID: 1},
			{ID: 20, Name: "bravo", BracketID: 2},
			{ID: 30, Name: "charlie", BracketID: 1},
			{ID: 40, Name: "hidden", BracketID: 1, Hidden: true},
			{ID: 50, Name: "idle", BracketID: 1},
		},
	}
}

func TestValueStaticAndDynamic(t *testing.T) {
	c := Challenge{Value: 500, Dynamic: true, MinValue: 100, Decay: 10}
	cases := []struct {
		solves int
		want   int
	}{{0, 500}, {1, 500}, {2, 496}, {6, 400}, {11, 100}, {50, 100}}
	for _, tc := range cases {
		if got := Value(c, tc.solves, true); got != tc.want {
			t.Errorf("Value(%d solves) = %d, want %d", tc.solves, got, tc.want)
		}
	}
	if got := Value(c, 50, false); got != 500 {
		t.Errorf("static mode should ignore decay, got %d", got)
	}
	if got := Value(Challenge{Value: 250}, 99, true); got != 250 {
		t.Errorf("static challenge in dynamic mode = %d", got)
	}
}

func TestRankingAndTieBreak(t *testing.T) {
	in := fixture()
	in.Solves = []Solve{
		{TeamID: 20, ChallengeID: 1, UserID: 2, At: at(10)}, // bravo reaches 100 first
		{TeamID: 10, ChallengeID: 1, UserID: 1, At: at(20)},
		{TeamID: 30, ChallengeID: 2, UserID: 3, At: at(30)},
	}
	b := Compute(in, time.Time{})

	got := []string{}
	for _, e := range b.Entries {
		got = append(got, e.Team.Name)
	}
	want := []string{"charlie", "bravo", "alpha"}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entries = %v, want %v (tie goes to whoever reached the score first)", got, want)
		}
	}
	if b.Teams[50].Rank != 0 {
		t.Error("team with no solves should be unranked")
	}
	if b.Challenges[1].FirstBlood.TeamID != 20 {
		t.Error("first blood on challenge 1 should be bravo")
	}
	if b.Teams[20].FirstBloods != 1 || b.Teams[10].FirstBloods != 0 {
		t.Error("first blood counts wrong")
	}
	if b.Users[3].Score != 300 {
		t.Errorf("user 3 score = %d", b.Users[3].Score)
	}
}

func TestHiddenTeamsDontAffectOthers(t *testing.T) {
	in := fixture()
	in.Dynamic = true
	in.Solves = []Solve{
		{TeamID: 40, ChallengeID: 3, At: at(1)}, // hidden team solves first
		{TeamID: 10, ChallengeID: 3, At: at(2)},
	}
	b := Compute(in, time.Time{})
	if b.Challenges[3].FirstBlood.TeamID != 10 {
		t.Error("hidden team took first blood")
	}
	if b.Challenges[3].SolveCount != 1 || b.Challenges[3].Value != 500 {
		t.Errorf("hidden team lowered the dynamic value: %+v", b.Challenges[3])
	}
	for _, e := range b.Entries {
		if e.Team.ID == 40 {
			t.Error("hidden team on the board")
		}
	}
	if b.Teams[40].Score != 500 {
		t.Error("hidden team should still have its own score for the admin view")
	}
}

func TestDynamicValueAppliesToEveryone(t *testing.T) {
	in := fixture()
	in.Dynamic = true
	in.Solves = []Solve{
		{TeamID: 10, ChallengeID: 3, At: at(1)},
		{TeamID: 20, ChallengeID: 3, At: at(2)},
		{TeamID: 30, ChallengeID: 3, At: at(3)},
	}
	b := Compute(in, time.Time{})
	// 3 solves, decay 2: 500 - 400 * (2/2)^2 = 100
	for _, id := range []int64{10, 20, 30} {
		if b.Teams[id].Score != 100 {
			t.Errorf("team %d score = %d, want 100 (early solvers drop too)", id, b.Teams[id].Score)
		}
	}
}

func TestHintsAdjustmentsAndFreeze(t *testing.T) {
	in := fixture()
	in.Solves = []Solve{
		{TeamID: 10, ChallengeID: 2, At: at(10)},
		{TeamID: 20, ChallengeID: 2, At: at(20)},
		{TeamID: 20, ChallengeID: 1, At: at(90)}, // after freeze
	}
	in.Unlocks = []Unlock{{TeamID: 10, Cost: 25, At: at(5)}}
	in.Adjustments = []Adjustment{{TeamID: 20, Points: -50, At: at(30)}}

	full := Compute(in, time.Time{})
	if full.Teams[10].Score != 275 || full.Teams[20].Score != 350 {
		t.Fatalf("full scores: alpha=%d bravo=%d", full.Teams[10].Score, full.Teams[20].Score)
	}

	frozen := Compute(in, at(60))
	if frozen.Teams[20].Score != 250 || frozen.Teams[20].SolveCount != 1 {
		t.Errorf("frozen bravo = %d points, %d solves; solve after freeze leaked", frozen.Teams[20].Score, frozen.Teams[20].SolveCount)
	}
	if frozen.Entries[0].Team.ID != 10 {
		t.Error("alpha should lead the frozen board")
	}
}

func TestBracketAndSeries(t *testing.T) {
	in := fixture()
	in.Solves = []Solve{
		{TeamID: 10, ChallengeID: 1, At: at(10)},
		{TeamID: 20, ChallengeID: 2, At: at(20)},
		{TeamID: 10, ChallengeID: 2, At: at(30)},
	}
	in.Unlocks = []Unlock{{TeamID: 10, Cost: 25, At: at(15)}}
	b := Compute(in, time.Time{})

	br := b.Bracket(2)
	if len(br) != 1 || br[0].Team.ID != 20 || br[0].Rank != 1 || br[0].TeamScore.Rank != 2 {
		t.Errorf("bracket 2 view wrong: %+v", br)
	}

	s := b.Series(in, []int64{10}, t0)[10]
	scores := []int{}
	for _, p := range s {
		scores = append(scores, p.Score)
	}
	want := []int{0, 100, 75, 375}
	if len(scores) != len(want) {
		t.Fatalf("series = %v, want %v", scores, want)
	}
	for i := range want {
		if scores[i] != want[i] {
			t.Fatalf("series = %v, want %v", scores, want)
		}
	}
}
