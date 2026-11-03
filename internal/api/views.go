package api

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/flagfall/flagfall/internal/scoring"
	"github.com/flagfall/flagfall/internal/store"
)

// JSON shapes match web/sdk/src/types.ts. IDs are strings.

type idStr int64

func (i idStr) MarshalJSON() ([]byte, error) { return json.Marshal(strconv.FormatInt(int64(i), 10)) }

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func nullStr(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}

type bracketView struct {
	ID          idStr  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type eventView struct {
	Name             string        `json:"name"`
	State            string        `json:"state"`
	StartsAt         *time.Time    `json:"startsAt"`
	EndsAt           *time.Time    `json:"endsAt"`
	FreezeAt         *time.Time    `json:"freezeAt"`
	ServerTime       time.Time     `json:"serverTime"`
	TimeZone         string        `json:"timeZone"`
	MaxTeamSize      int           `json:"maxTeamSize"`
	ScoringMode      string        `json:"scoringMode"`
	RegistrationOpen bool          `json:"registrationOpen"`
	Brackets         []bracketView `json:"brackets"`
	Categories       []string      `json:"categories"`
}

type userSummaryView struct {
	ID      idStr   `json:"id"`
	Name    string  `json:"name"`
	Country *string `json:"country"`
}

type meView struct {
	userSummaryView
	Email    string      `json:"email"`
	Verified bool        `json:"verified"`
	Team     *myTeamView `json:"team"`
}

type teamSummaryView struct {
	ID          idStr   `json:"id"`
	Name        string  `json:"name"`
	Country     *string `json:"country"`
	BracketID   idStr   `json:"bracketId"`
	MemberCount int     `json:"memberCount"`
	Recruiting  bool    `json:"recruiting"`
	Rank        *int    `json:"rank"`
	Score       int     `json:"score"`
}

type teamMemberView struct {
	userSummaryView
	Role        string     `json:"role"`
	SolveCount  int        `json:"solveCount"`
	Score       int        `json:"score"`
	LastSolveAt *time.Time `json:"lastSolveAt"`
}

type teamSolveView struct {
	ChallengeSlug string    `json:"challengeSlug"`
	ChallengeName string    `json:"challengeName"`
	Category      string    `json:"category"`
	UserName      string    `json:"userName"`
	Points        int       `json:"points"`
	SolvedAt      time.Time `json:"solvedAt"`
	FirstBlood    bool      `json:"firstBlood"`
}

type categoryScore struct {
	Category string `json:"category"`
	Score    int    `json:"score"`
}

type teamView struct {
	teamSummaryView
	CreatedAt       time.Time        `json:"createdAt"`
	Members         []teamMemberView `json:"members"`
	Solves          []teamSolveView  `json:"solves"`
	FirstBloodCount int              `json:"firstBloodCount"`
	ScoreByCategory []categoryScore  `json:"scoreByCategory"`
}

type myTeamView struct {
	teamView
	InviteCode      string  `json:"inviteCode"`
	IsCaptain       bool    `json:"isCaptain"`
	UnlockedHintIDs []idStr `json:"unlockedHintIds"`
}

// ---------------------------------------------------------------- builders

func eventToView(ev *store.Event, brackets []store.Bracket, categories []string, now time.Time) eventView {
	v := eventView{
		Name: ev.Name, State: ev.State(now),
		StartsAt: utcPtr(ev.StartsAt), EndsAt: utcPtr(ev.EndsAt), FreezeAt: utcPtr(ev.FreezeAt),
		ServerTime: now.UTC(), TimeZone: ev.TimeZone, MaxTeamSize: ev.MaxTeamSize,
		ScoringMode: ev.ScoringMode, RegistrationOpen: ev.RegistrationOpen,
		Brackets: []bracketView{}, Categories: categories,
	}
	if v.Categories == nil {
		v.Categories = []string{}
	}
	for _, b := range brackets {
		v.Brackets = append(v.Brackets, bracketView{ID: idStr(b.ID), Name: b.Name, Description: b.Description})
	}
	return v
}

func summaryFor(t *store.Team, members int, board *scoring.Board) teamSummaryView {
	v := teamSummaryView{
		ID: idStr(t.ID), Name: t.Name, Country: nullStr(t.Country), BracketID: idStr(t.BracketID),
		MemberCount: members, Recruiting: t.Recruiting,
	}
	if ts := board.Teams[t.ID]; ts != nil {
		v.Score = ts.Score
		if ts.Rank > 0 {
			r := ts.Rank
			v.Rank = &r
		}
	}
	return v
}

// teamDetail builds the full team profile.
func (a *API) teamDetail(ctx context.Context, t *store.Team, board *scoring.Board) (teamView, error) {
	var members []store.User
	if err := a.store.DB.NewSelect().Model(&members).Where("team_id = ?", t.ID).Order("id").Scan(ctx); err != nil {
		return teamView{}, err
	}
	var chals []store.Challenge
	if err := a.store.DB.NewSelect().Model(&chals).Column("id", "slug", "name", "category").Scan(ctx); err != nil {
		return teamView{}, err
	}
	chByID := map[int64]store.Challenge{}
	for _, c := range chals {
		chByID[c.ID] = c
	}
	userName := map[int64]string{}
	for _, m := range members {
		userName[m.ID] = m.Name
	}

	v := teamView{
		teamSummaryView: summaryFor(t, len(members), board),
		CreatedAt:       t.CreatedAt.UTC(),
		Members:         []teamMemberView{},
		Solves:          []teamSolveView{},
		ScoreByCategory: []categoryScore{},
	}
	for _, m := range members {
		mv := teamMemberView{
			userSummaryView: userSummaryView{ID: idStr(m.ID), Name: m.Name, Country: nullStr(m.Country)},
			Role:            "player",
		}
		if t.CaptainID != nil && *t.CaptainID == m.ID {
			mv.Role = "captain"
		}
		if us := board.Users[m.ID]; us != nil {
			mv.SolveCount, mv.Score, mv.LastSolveAt = us.SolveCount, us.Score, timePtr(us.LastSolveAt)
		}
		v.Members = append(v.Members, mv)
	}
	if ts := board.Teams[t.ID]; ts != nil {
		v.FirstBloodCount = ts.FirstBloods
		for i := len(ts.Solves) - 1; i >= 0; i-- { // newest first
			s := ts.Solves[i]
			c := chByID[s.ChallengeID]
			name := userName[s.UserID]
			if name == "" {
				name = "former member"
			}
			v.Solves = append(v.Solves, teamSolveView{
				ChallengeSlug: c.Slug, ChallengeName: c.Name, Category: c.Category,
				UserName: name, Points: s.Points, SolvedAt: s.At.UTC(), FirstBlood: s.FirstBlood,
			})
		}
		for cat, score := range ts.ByCategory {
			v.ScoreByCategory = append(v.ScoreByCategory, categoryScore{Category: cat, Score: score})
		}
		sort.Slice(v.ScoreByCategory, func(i, j int) bool { return v.ScoreByCategory[i].Category < v.ScoreByCategory[j].Category })
	}
	return v, nil
}

func (a *API) myTeam(ctx context.Context, t *store.Team, user *store.User, board *scoring.Board) (*myTeamView, error) {
	tv, err := a.teamDetail(ctx, t, board)
	if err != nil {
		return nil, err
	}
	var unlocks []store.HintUnlock
	if err := a.store.DB.NewSelect().Model(&unlocks).Where("team_id = ?", t.ID).Scan(ctx); err != nil {
		return nil, err
	}
	v := &myTeamView{
		teamView: tv, InviteCode: t.InviteCode,
		IsCaptain:       t.CaptainID != nil && *t.CaptainID == user.ID,
		UnlockedHintIDs: []idStr{},
	}
	for _, u := range unlocks {
		v.UnlockedHintIDs = append(v.UnlockedHintIDs, idStr(u.HintID))
	}
	return v, nil
}

func (a *API) meView(ctx context.Context, user *store.User) (*meView, error) {
	v := &meView{
		userSummaryView: userSummaryView{ID: idStr(user.ID), Name: user.Name, Country: nullStr(user.Country)},
		Email:           user.Email,
		Verified:        user.VerifiedAt != nil,
	}
	if user.TeamID == nil {
		return v, nil
	}
	t := new(store.Team)
	if err := a.store.DB.NewSelect().Model(t).Where("id = ?", *user.TeamID).Scan(ctx); err != nil {
		return nil, err
	}
	snap, err := a.boards.Get(ctx)
	if err != nil {
		return nil, err
	}
	mt, err := a.myTeam(ctx, t, user, snap.Board(user.IsStaff()))
	if err != nil {
		return nil, err
	}
	v.Team = mt
	return v, nil
}

func lowerOr(s *string, def string) string {
	if s == nil || *s == "" {
		return def
	}
	return strings.ToLower(*s)
}
