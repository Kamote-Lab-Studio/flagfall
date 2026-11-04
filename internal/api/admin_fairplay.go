package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/uptrace/bun"

	"github.com/Kamote-Lab-Studio/flagfall/internal/flags"
)

// Fair-play checks behind the Submissions screen's side panel
// (design/option-a/admin/submissions.html).
const (
	// The same wrong flag from this many teams...
	sharedFlagTeams = 3
	// ...within this long is worth a look: it can mean a shared answer.
	sharedFlagWindow = 10 * time.Minute
	// How far back the shared-flag check looks.
	sharedFlagLookback = 24 * time.Hour
	// A team at this share of the per-minute flag limit is "close to it".
	rateLimitShare = 0.8
	// How far back the rate check looks.
	rateLookback = 15 * time.Minute
	// Solves of one challenge by different teams this close together...
	simulSolveWindow = 30 * time.Second
	// ...among its first this many solves are worth a look (later on, popular
	// challenges are solved every few seconds anyway).
	simulSolveEarly = 10
)

type fairPlayAlert struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"` // "shared-flag", "rate-limit", "team-flag" or "simul-solve"
	ChallengeID   idStr     `json:"challengeId"`
	ChallengeName string    `json:"challengeName"`
	Teams         []string  `json:"teams"`
	TeamIDs       []idStr   `json:"teamIds"`
	Count         int       `json:"count"`       // teams (shared-flag) or attempts in a minute (rate-limit)
	Limit         int       `json:"limit"`       // rate-limit only
	SpanSeconds   int       `json:"spanSeconds"` // shared-flag and simul-solve: first to last
	SameIP        bool      `json:"sameIp"`      // simul-solve: two of the solves came from one IP
	At            time.Time `json:"at"`          // the latest attempt involved
}

type fairPlayRow struct {
	TeamID        int64     `bun:"team_id"`
	TeamName      string    `bun:"team_name"`
	ChallengeID   int64     `bun:"challenge_id"`
	ChallengeName string    `bun:"challenge_name"`
	Submitted     string    `bun:"submitted"`
	SubmittedAt   time.Time `bun:"submitted_at"`
}

// GET /submissions/summary: counts by result (with the same team, user and
// challenge filters as the list) and the fair-play alerts.
func (a *API) adminSubmissionsSummary(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	q := r.URL.Query()
	now := a.now().UTC()

	filter := func(sel *bun.SelectQuery) *bun.SelectQuery {
		for param, col := range map[string]string{"teamId": "team_id", "userId": "user_id", "challengeId": "challenge_id"} {
			if id, err := strconv.ParseInt(q.Get(param), 10, 64); err == nil {
				sel = sel.Where(col+" = ?", id)
			}
		}
		return sel
	}
	var counts struct {
		Total   int `bun:"total"`
		Correct int `bun:"correct"`
	}
	if err := filter(a.store.DB.NewSelect().TableExpr("submissions").
		ColumnExpr("COUNT(*) AS total, COALESCE(SUM(CASE WHEN correct THEN 1 ELSE 0 END), 0) AS correct")).
		Scan(ctx, &counts); err != nil {
		return err
	}

	rows := func(since time.Time, correct *bool) ([]fairPlayRow, error) {
		var out []fairPlayRow
		sel := a.store.DB.NewSelect().TableExpr("submissions AS s").
			ColumnExpr("s.team_id, t.name AS team_name, s.challenge_id, c.name AS challenge_name, s.submitted, s.submitted_at").
			Join("JOIN teams AS t ON t.id = s.team_id").Join("JOIN challenges AS c ON c.id = s.challenge_id").
			Where("s.submitted_at > ?", since).OrderExpr("s.submitted_at")
		if correct != nil {
			sel = sel.Where("s.correct = ?", *correct)
		}
		return out, sel.Scan(ctx, &out)
	}

	alerts := []fairPlayAlert{}
	wrong := false
	shared, err := rows(now.Add(-sharedFlagLookback), &wrong)
	if err != nil {
		return err
	}
	alerts = append(alerts, sharedFlagAlerts(shared)...)

	owners, err := a.teamFlagOwners(ctx)
	if err != nil {
		return err
	}
	alerts = append(alerts, teamFlagAlerts(shared, owners, a.cfg.Secret())...)

	var solves []solveRow
	if err := a.store.DB.NewSelect().TableExpr("solves AS s").
		ColumnExpr("s.team_id, t.name AS team_name, s.challenge_id, c.name AS challenge_name, s.solved_at, sub.ip").
		Join("JOIN teams AS t ON t.id = s.team_id").Join("JOIN challenges AS c ON c.id = s.challenge_id").
		Join("LEFT JOIN submissions AS sub ON sub.id = s.submission_id").
		OrderExpr("s.challenge_id, s.solved_at").Scan(ctx, &solves); err != nil {
		return err
	}
	alerts = append(alerts, simulSolveAlerts(solves, now.Add(-sharedFlagLookback))...)

	ev, err := a.store.LoadEvent(ctx)
	if err != nil {
		return err
	}
	if ev.FlagRateLimit > 0 {
		recent, err := rows(now.Add(-rateLookback), nil)
		if err != nil {
			return err
		}
		alerts = append(alerts, rateAlerts(recent, ev.FlagRateLimit)...)
	}
	sort.SliceStable(alerts, func(i, j int) bool { return alerts[i].At.After(alerts[j].At) })

	writeJSON(w, http.StatusOK, map[string]any{
		"total":     counts.Total,
		"correct":   counts.Correct,
		"incorrect": counts.Total - counts.Correct,
		"alerts":    alerts,
	})
	return nil
}

// sharedFlagAlerts finds one wrong flag sent for one challenge by
// sharedFlagTeams or more teams within sharedFlagWindow. rows are oldest first.
func sharedFlagAlerts(rows []fairPlayRow) []fairPlayAlert {
	type key struct {
		challenge int64
		flag      string
	}
	groups := map[key][]fairPlayRow{}
	var order []key
	for _, r := range rows {
		k := key{r.ChallengeID, r.Submitted}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	var out []fairPlayAlert
	for _, k := range order {
		g := groups[k]
		if len(g) < sharedFlagTeams {
			continue
		}
		// The window ending at each attempt; keep the one with the most teams.
		best, bestStart, bestEnd := 0, 0, 0
		start := 0
		for end := range g {
			for g[end].SubmittedAt.Sub(g[start].SubmittedAt) > sharedFlagWindow {
				start++
			}
			if n := distinctTeams(g[start : end+1]); n > best {
				best, bestStart, bestEnd = n, start, end
			}
		}
		if best < sharedFlagTeams {
			continue
		}
		win := g[bestStart : bestEnd+1]
		names, ids := teamsIn(win)
		sum := sha256.Sum256([]byte(k.flag))
		out = append(out, fairPlayAlert{
			ID:   "shared-" + strconv.FormatInt(k.challenge, 10) + "-" + hex.EncodeToString(sum[:6]),
			Kind: "shared-flag", ChallengeID: idStr(k.challenge), ChallengeName: win[0].ChallengeName,
			Teams: names, TeamIDs: ids, Count: best,
			SpanSeconds: int(win[len(win)-1].SubmittedAt.Sub(win[0].SubmittedAt).Seconds()),
			At:          win[len(win)-1].SubmittedAt.UTC(),
		})
	}
	return out
}

// rateAlerts finds teams that sent at least rateLimitShare of the per-minute
// limit within one minute. rows are oldest first.
func rateAlerts(rows []fairPlayRow, limit int) []fairPlayAlert {
	threshold := int(float64(limit)*rateLimitShare + 0.5)
	if threshold < 2 {
		threshold = 2
	}
	byTeam := map[int64][]fairPlayRow{}
	var order []int64
	for _, r := range rows {
		if _, ok := byTeam[r.TeamID]; !ok {
			order = append(order, r.TeamID)
		}
		byTeam[r.TeamID] = append(byTeam[r.TeamID], r)
	}
	var out []fairPlayAlert
	for _, team := range order {
		g := byTeam[team]
		best, bestStart, bestEnd := 0, 0, 0
		start := 0
		for end := range g {
			for g[end].SubmittedAt.Sub(g[start].SubmittedAt) >= time.Minute {
				start++
			}
			if n := end - start + 1; n > best {
				best, bestStart, bestEnd = n, start, end
			}
		}
		if best < threshold {
			continue
		}
		// Name the challenge most of that minute's attempts went to.
		per := map[int64]int{}
		top := g[bestStart]
		for _, r := range g[bestStart : bestEnd+1] {
			per[r.ChallengeID]++
			if per[r.ChallengeID] > per[top.ChallengeID] {
				top = r
			}
		}
		out = append(out, fairPlayAlert{
			ID:   "rate-" + strconv.FormatInt(team, 10) + "-" + strconv.FormatInt(g[bestEnd].SubmittedAt.Unix()/60, 10),
			Kind: "rate-limit", ChallengeID: idStr(top.ChallengeID), ChallengeName: top.ChallengeName,
			Teams: []string{g[0].TeamName}, TeamIDs: []idStr{idStr(team)}, Count: best, Limit: limit,
			At: g[bestEnd].SubmittedAt.UTC(),
		})
	}
	return out
}

func distinctTeams(rows []fairPlayRow) int {
	seen := map[int64]bool{}
	for _, r := range rows {
		seen[r.TeamID] = true
	}
	return len(seen)
}

func teamsIn(rows []fairPlayRow) ([]string, []idStr) {
	seen := map[int64]bool{}
	var names []string
	var ids []idStr
	for _, r := range rows {
		if !seen[r.TeamID] {
			seen[r.TeamID] = true
			names = append(names, r.TeamName)
			ids = append(ids, idStr(r.TeamID))
		}
	}
	return names, ids
}

// teamFlagOwner is whose per-team flag a hash is.
type teamFlagOwner struct {
	TeamID   int64
	TeamName string
}

type teamFlagKey struct {
	challenge int64
	hash      string
}

func (a *API) teamFlagOwners(ctx context.Context) (map[teamFlagKey]teamFlagOwner, error) {
	var rows []struct {
		ChallengeID int64  `bun:"challenge_id"`
		TeamID      int64  `bun:"team_id"`
		ValueHash   string `bun:"value_hash"`
		TeamName    string `bun:"team_name"`
	}
	if err := a.store.DB.NewSelect().TableExpr("flags AS f").
		ColumnExpr("f.challenge_id, f.team_id, f.value_hash, t.name AS team_name").
		Join("JOIN teams AS t ON t.id = f.team_id").
		Where("f.kind = ?", flags.PerTeam).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	out := make(map[teamFlagKey]teamFlagOwner, len(rows))
	for _, r := range rows {
		out[teamFlagKey{r.ChallengeID, r.ValueHash}] = teamFlagOwner{r.TeamID, r.TeamName}
	}
	return out, nil
}

// teamFlagAlerts finds wrong attempts that were another team's per-team flag:
// that flag can only have come from that team. rows are oldest first.
func teamFlagAlerts(rows []fairPlayRow, owners map[teamFlagKey]teamFlagOwner, secret []byte) []fairPlayAlert {
	if len(owners) == 0 {
		return nil
	}
	type key struct{ challenge, from, owner int64 }
	byKey := map[key]*fairPlayAlert{}
	var order []key
	for _, r := range rows {
		owner, ok := owners[teamFlagKey{r.ChallengeID, flags.Hash(secret, flags.Exact, r.Submitted)}]
		if !ok || owner.TeamID == r.TeamID {
			continue
		}
		k := key{r.ChallengeID, r.TeamID, owner.TeamID}
		al := byKey[k]
		if al == nil {
			al = &fairPlayAlert{
				ID:   "team-" + strconv.FormatInt(r.ChallengeID, 10) + "-" + strconv.FormatInt(r.TeamID, 10) + "-" + strconv.FormatInt(owner.TeamID, 10),
				Kind: "team-flag", ChallengeID: idStr(r.ChallengeID), ChallengeName: r.ChallengeName,
				Teams: []string{r.TeamName, owner.TeamName}, TeamIDs: []idStr{idStr(r.TeamID), idStr(owner.TeamID)},
			}
			byKey[k] = al
			order = append(order, k)
		}
		al.Count++
		al.At = r.SubmittedAt.UTC()
	}
	out := make([]fairPlayAlert, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out
}

type solveRow struct {
	TeamID        int64     `bun:"team_id"`
	TeamName      string    `bun:"team_name"`
	ChallengeID   int64     `bun:"challenge_id"`
	ChallengeName string    `bun:"challenge_name"`
	SolvedAt      time.Time `bun:"solved_at"`
	IP            *string   `bun:"ip"`
}

// simulSolveAlerts finds a challenge's early solves (its first
// simulSolveEarly) by different teams within simulSolveWindow of each other,
// ending after since. rows are ordered by challenge, then time.
func simulSolveAlerts(rows []solveRow, since time.Time) []fairPlayAlert {
	var out []fairPlayAlert
	for i := 0; i < len(rows); {
		j := i
		for j < len(rows) && rows[j].ChallengeID == rows[i].ChallengeID {
			j++
		}
		early := rows[i:j]
		if len(early) > simulSolveEarly {
			early = early[:simulSolveEarly]
		}
		for k := 0; k < len(early); {
			end := k + 1
			for end < len(early) && early[end].SolvedAt.Sub(early[k].SolvedAt) <= simulSolveWindow {
				end++
			}
			if g := early[k:end]; len(g) >= 2 && g[len(g)-1].SolvedAt.After(since) {
				al := fairPlayAlert{
					ID:   "simul-" + strconv.FormatInt(g[0].ChallengeID, 10) + "-" + strconv.FormatInt(g[0].SolvedAt.Unix(), 10),
					Kind: "simul-solve", ChallengeID: idStr(g[0].ChallengeID), ChallengeName: g[0].ChallengeName,
					Count:       len(g),
					SpanSeconds: int(g[len(g)-1].SolvedAt.Sub(g[0].SolvedAt).Seconds()),
					At:          g[len(g)-1].SolvedAt.UTC(),
				}
				ips := map[string]bool{}
				for _, s := range g {
					al.Teams = append(al.Teams, s.TeamName)
					al.TeamIDs = append(al.TeamIDs, idStr(s.TeamID))
					if s.IP != nil && *s.IP != "" {
						if ips[*s.IP] {
							al.SameIP = true
						}
						ips[*s.IP] = true
					}
				}
				out = append(out, al)
			}
			k = end
		}
		i = j
	}
	return out
}
