package api

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/flagfall/flagfall/internal/scoring"
	"github.com/flagfall/flagfall/internal/store"
)

func (a *API) mountAdminScoreboard(r chi.Router) {
	r.Get("/scoreboard", a.h(a.staff("support", a.adminScoreboard)))
	r.Get("/scoreboard/adjustments", a.h(a.staff("support", a.adminListAdjustments)))
	r.Post("/scoreboard/adjustments", a.h(a.staff("admin", a.adminAddAdjustment)))
	r.Delete("/scoreboard/adjustments/{id}", a.h(a.staff("admin", a.adminDeleteAdjustment)))
	r.Get("/scoreboard/export/ctftime", a.h(a.staff("support", a.adminCTFtime)))
	r.Get("/submissions", a.h(a.staff("support", a.adminSubmissions)))
	r.Get("/dashboard", a.h(a.staff("support", a.adminDashboard)))
}

// ---------------------------------------------------------------- scoreboard

type adminBoardEntry struct {
	Rank        *int       `json:"rank"`
	TeamID      idStr      `json:"teamId"`
	TeamName    string     `json:"teamName"`
	Country     *string    `json:"country"`
	BracketID   idStr      `json:"bracketId"`
	Hidden      bool       `json:"hidden"`
	Banned      bool       `json:"banned"`
	Score       int        `json:"score"`
	SolveCount  int        `json:"solveCount"`
	FirstBloods int        `json:"firstBloods"`
	LastSolveAt *time.Time `json:"lastSolveAt"`
}

func (a *API) adminScoreboard(w http.ResponseWriter, r *http.Request) error {
	snap, err := a.boards.Get(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"frozen": snap.Frozen, "freezeAt": utcPtr(snap.FreezeAt), "updatedAt": snap.UpdatedAt.UTC(), "entries": staffEntries(snap),
	})
	return nil
}

// staffEntries is the unfrozen board: ranked teams, then hidden and banned
// teams that scored, without a rank.
func staffEntries(snap *scoring.Snapshot) []adminBoardEntry {
	board := snap.Full
	entries := []adminBoardEntry{}
	add := func(ts *scoring.TeamScore, rank int) {
		e := adminBoardEntry{
			TeamID: idStr(ts.Team.ID), TeamName: ts.Team.Name, BracketID: idStr(ts.Team.BracketID),
			Hidden: ts.Team.Hidden, Banned: ts.Team.Banned, Score: ts.Score, SolveCount: ts.SolveCount,
			FirstBloods: ts.FirstBloods, LastSolveAt: timePtr(ts.LastSolveAt),
		}
		if ts.Team.Country != "" {
			c := ts.Team.Country
			e.Country = &c
		}
		if rank > 0 {
			e.Rank = &rank
		}
		entries = append(entries, e)
	}
	for i, ts := range board.Entries {
		add(ts, i+1)
	}
	// Hidden and banned teams that scored, after the ranked ones.
	var off []*scoring.TeamScore
	for _, ts := range board.Teams {
		if (ts.Team.Hidden || ts.Team.Banned) && (ts.Score != 0 || ts.SolveCount > 0) {
			off = append(off, ts)
		}
	}
	sort.Slice(off, func(i, j int) bool {
		if off[i].Score != off[j].Score {
			return off[i].Score > off[j].Score
		}
		return off[i].Team.ID < off[j].Team.ID
	})
	for _, ts := range off {
		add(ts, 0)
	}
	return entries
}

func (a *API) adminCTFtime(w http.ResponseWriter, r *http.Request) error {
	snap, err := a.boards.Get(r.Context())
	if err != nil {
		return err
	}
	type standing struct {
		Pos   int    `json:"pos"`
		Team  string `json:"team"`
		Score int    `json:"score"`
	}
	out := []standing{}
	for i, ts := range snap.Full.Entries {
		out = append(out, standing{i + 1, ts.Team.Name, ts.Score})
	}
	w.Header().Set("Content-Disposition", `attachment; filename="ctftime-scoreboard.json"`)
	writeJSON(w, http.StatusOK, map[string]any{"standings": out})
	return nil
}

// ---------------------------------------------------------------- adjustments

func (a *API) adminListAdjustments(w http.ResponseWriter, r *http.Request) error {
	var rows []struct {
		store.ScoreAdjustment `bun:",extend"`
		TeamName              string  `bun:"team_name"`
		ActorName             *string `bun:"actor_name"`
	}
	if err := a.store.DB.NewSelect().TableExpr("score_adjustments AS s").ColumnExpr("s.*").
		ColumnExpr("t.name AS team_name").ColumnExpr("u.name AS actor_name").
		Join("JOIN teams AS t ON t.id = s.team_id").Join("LEFT JOIN users AS u ON u.id = s.actor_id").
		OrderExpr("s.id DESC").Scan(r.Context(), &rows); err != nil {
		return err
	}
	type item struct {
		ID        idStr     `json:"id"`
		TeamID    idStr     `json:"teamId"`
		TeamName  string    `json:"teamName"`
		Points    int       `json:"points"`
		Reason    string    `json:"reason"`
		ActorName *string   `json:"actorName"`
		CreatedAt time.Time `json:"createdAt"`
	}
	out := []item{}
	for _, a := range rows {
		out = append(out, item{idStr(a.ID), idStr(a.TeamID), a.TeamName, a.Points, a.Reason, a.ActorName, a.CreatedAt.UTC()})
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) adminAddAdjustment(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var in struct {
		TeamID idStr  `json:"teamId"`
		Points int    `json:"points"`
		Reason string `json:"reason"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	fields := map[string]string{}
	if n, _ := a.store.DB.NewSelect().Model((*store.Team)(nil)).Where("id = ?", int64(in.TeamID)).Count(ctx); n == 0 {
		fields["teamId"] = "No such team."
	}
	if in.Points == 0 || in.Points < -100_000 || in.Points > 100_000 {
		fields["points"] = "Use a non-zero number from -100000 to 100000."
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if len(in.Reason) < 3 || len(in.Reason) > 200 {
		fields["reason"] = "Say why (3 to 200 characters). Players may ask."
	}
	if len(fields) > 0 {
		return errValidation(fields)
	}
	adj := &store.ScoreAdjustment{TeamID: int64(in.TeamID), Points: in.Points, Reason: in.Reason, ActorID: currentUser(r).ID, CreatedAt: a.now().UTC()}
	if _, err := a.store.DB.NewInsert().Model(adj).Exec(ctx); err != nil {
		return err
	}
	a.scoresChanged(ctx)
	writeJSON(w, http.StatusCreated, map[string]any{"id": idStr(adj.ID)})
	return nil
}

func (a *API) adminDeleteAdjustment(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	res, err := a.store.DB.NewDelete().Model((*store.ScoreAdjustment)(nil)).Where("id = ?", id).Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	a.scoresChanged(ctx)
	return noContent(w)
}

// ---------------------------------------------------------------- submissions

func (a *API) adminSubmissions(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	q := r.URL.Query()
	limit := 100
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 && n <= 500 {
		limit = n
	}
	sel := a.store.DB.NewSelect().TableExpr("submissions AS s").
		ColumnExpr("s.id, s.team_id, s.user_id, s.challenge_id, s.correct, s.submitted, s.ip, s.submitted_at").
		ColumnExpr("t.name AS team_name, u.name AS user_name, c.slug AS challenge_slug, c.name AS challenge_name").
		Join("JOIN teams AS t ON t.id = s.team_id").Join("LEFT JOIN users AS u ON u.id = s.user_id").
		Join("JOIN challenges AS c ON c.id = s.challenge_id").
		OrderExpr("s.id DESC").Limit(int64(limit))
	for param, col := range map[string]string{"teamId": "s.team_id", "userId": "s.user_id", "challengeId": "s.challenge_id"} {
		if id, err := strconv.ParseInt(q.Get(param), 10, 64); err == nil {
			sel = sel.Where(col+" = ?", id)
		}
	}
	if b, err := strconv.ParseInt(q.Get("before"), 10, 64); err == nil {
		sel = sel.Where("s.id < ?", b)
	}
	if c := q.Get("correct"); c == "true" || c == "false" {
		sel = sel.Where("s.correct = ?", c == "true")
	}
	var rows []struct {
		ID            int64     `bun:"id"`
		TeamID        int64     `bun:"team_id"`
		UserID        *int64    `bun:"user_id"`
		ChallengeID   int64     `bun:"challenge_id"`
		Correct       bool      `bun:"correct"`
		Submitted     string    `bun:"submitted"`
		IP            *string   `bun:"ip"`
		SubmittedAt   time.Time `bun:"submitted_at"`
		TeamName      string    `bun:"team_name"`
		UserName      *string   `bun:"user_name"`
		ChallengeSlug string    `bun:"challenge_slug"`
		ChallengeName string    `bun:"challenge_name"`
	}
	if err := sel.Scan(ctx, &rows); err != nil {
		return err
	}
	type item struct {
		ID            idStr     `json:"id"`
		TeamID        idStr     `json:"teamId"`
		TeamName      string    `json:"teamName"`
		UserID        *idStr    `json:"userId"`
		UserName      *string   `json:"userName"`
		ChallengeID   idStr     `json:"challengeId"`
		ChallengeSlug string    `json:"challengeSlug"`
		ChallengeName string    `json:"challengeName"`
		Correct       bool      `json:"correct"`
		Submitted     string    `json:"submitted"`
		IP            *string   `json:"ip"`
		SubmittedAt   time.Time `json:"submittedAt"`
	}
	out := []item{}
	for _, s := range rows {
		it := item{ID: idStr(s.ID), TeamID: idStr(s.TeamID), TeamName: s.TeamName, UserName: s.UserName,
			ChallengeID: idStr(s.ChallengeID), ChallengeSlug: s.ChallengeSlug, ChallengeName: s.ChallengeName,
			Correct: s.Correct, Submitted: s.Submitted, IP: s.IP, SubmittedAt: s.SubmittedAt.UTC()}
		if s.Correct {
			// A correct submission is the flag; don't spread it to every staff screen.
			it.Submitted = "(correct flag)"
		}
		if s.UserID != nil {
			u := idStr(*s.UserID)
			it.UserID = &u
		}
		out = append(out, it)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// ---------------------------------------------------------------- dashboard

func (a *API) adminDashboard(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	now := a.now().UTC()
	db := a.store.DB
	count := func(model any, where string, args ...any) int {
		q := db.NewSelect().Model(model)
		if where != "" {
			q = q.Where(where, args...)
		}
		n, err := q.Count(ctx)
		if err != nil {
			a.log.Error("dashboard count", "err", err)
		}
		return int(n)
	}
	var online int
	if err := db.NewSelect().Model((*store.Session)(nil)).ColumnExpr("COUNT(DISTINCT user_id)").
		Where("last_seen_at > ?", now.Add(-5*time.Minute)).Scan(ctx, &online); err != nil {
		return err
	}
	recent := count((*store.Submission)(nil), "submitted_at > ?", now.Add(-5*time.Minute))

	board, err := a.staffSnap(ctx)
	if err != nil {
		return err
	}
	attempts, err := a.challengeStats(ctx)
	if err != nil {
		return err
	}
	var cs []store.Challenge
	if err := db.NewSelect().Model(&cs).Order("position", "id").Scan(ctx); err != nil {
		return err
	}
	type chal struct {
		ID         idStr      `json:"id"`
		Slug       string     `json:"slug"`
		Name       string     `json:"name"`
		Category   string     `json:"category"`
		State      string     `json:"state"`
		Solves     int        `json:"solves"`
		Attempts   int        `json:"attempts"`
		FirstBlood *string    `json:"firstBloodTeam"`
		FirstAt    *time.Time `json:"firstBloodAt"`
	}
	teamNames := map[int64]string{}
	for id, ts := range board.Teams {
		teamNames[id] = ts.Team.Name
	}
	challenges := []chal{}
	var warnings []string
	var flagCounts []struct {
		ChallengeID int64 `bun:"challenge_id"`
	}
	_ = db.NewSelect().Model((*store.Flag)(nil)).Column("challenge_id").Group("challenge_id").Scan(ctx, &flagCounts)
	hasFlag := map[int64]bool{}
	for _, f := range flagCounts {
		hasFlag[f.ChallengeID] = true
	}
	for _, c := range cs {
		v := chal{ID: idStr(c.ID), Slug: c.Slug, Name: c.Name, Category: c.Category, State: c.State, Attempts: attempts[c.ID]}
		if st := board.Challenges[c.ID]; st != nil {
			v.Solves = st.SolveCount
			if st.FirstBlood != nil {
				name := teamNames[st.FirstBlood.TeamID]
				v.FirstBlood, v.FirstAt = &name, timePtr(st.FirstBlood.At)
			}
		}
		challenges = append(challenges, v)
		if c.State == "visible" && !hasFlag[c.ID] {
			warnings = append(warnings, c.Name+" is visible but has no flag, so nobody can solve it.")
		}
		if c.State == "visible" {
			if w := a.missingFieldsWarning(&c); w != "" {
				warnings = append(warnings, w)
			}
		}
	}
	ev, err := a.store.LoadEvent(ctx)
	if err != nil {
		return err
	}
	if ev.StartsAt == nil || ev.EndsAt == nil {
		warnings = append(warnings, "The event has no start or end time set.")
	}
	if !a.canEmail(ctx) {
		warnings = append(warnings, "No mail server is set up (Settings), so password reset emails go to the server log.")
	}
	if warnings == nil {
		warnings = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"state":                ev.State(now),
		"playersOnline":        online,
		"teams":                count((*store.Team)(nil), ""),
		"players":              count((*store.User)(nil), "role = 'player'"),
		"submissions":          count((*store.Submission)(nil), ""),
		"submissionsPerMinute": float64(recent) / 5,
		"solves":               count((*store.Solve)(nil), ""),
		"challenges":           challenges,
		"warnings":             warnings,
	})
	return nil
}
