package api

import (
	"context"
	"encoding/csv"
	"net/http"
	"strings"
	"unicode"

	"github.com/uptrace/bun"

	"github.com/Kamote-Lab-Studio/flagfall/internal/flags"
	"github.com/Kamote-Lab-Studio/flagfall/internal/store"
)

// Per-team flags: every team gets its own flag for a challenge, derived from
// the server secret (flags.TeamValue), so a flag that turns up from another
// team shows it was shared. Teams that join later get theirs on first use.

type teamFlagsView struct {
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
	Teams  int    `json:"teams"` // teams with a flag
}

type teamFlagRow struct {
	TeamID   idStr  `json:"teamId"`
	TeamName string `json:"teamName"`
	Flag     string `json:"flag"`
}

// teamFlagFormat returns the format the challenge's per-team flags use, or ""
// when it has none.
func (a *API) teamFlagFormat(ctx context.Context, db bun.IDB, challengeID int64) (string, error) {
	var formats []string
	err := db.NewSelect().Model((*store.Flag)(nil)).Column("format").
		Where("challenge_id = ? AND kind = ? AND format IS NOT NULL", challengeID, flags.PerTeam).
		Limit(1).Scan(ctx, &formats)
	if err != nil || len(formats) == 0 {
		return "", err
	}
	return formats[0], nil
}

// ensureTeamFlags gives every team (or just onlyTeam, when not zero) that has
// no per-team flag for the challenge one in format. It returns how many it added.
func (a *API) ensureTeamFlags(ctx context.Context, db bun.IDB, challengeID int64, format string, onlyTeam int64) (int, error) {
	q := db.NewSelect().Model((*store.Team)(nil)).Column("id").
		Where("NOT EXISTS (SELECT 1 FROM flags AS f WHERE f.challenge_id = ? AND f.team_id = team.id)", challengeID)
	if onlyTeam != 0 {
		q = q.Where("team.id = ?", onlyTeam)
	}
	var ids []int64
	if err := q.Scan(ctx, &ids); err != nil {
		return 0, err
	}
	for _, id := range ids {
		h := flags.Hash(a.cfg.Secret(), flags.Exact, flags.TeamValue(a.cfg.Secret(), challengeID, id, format))
		team := id
		f := &store.Flag{ChallengeID: challengeID, Kind: flags.PerTeam, ValueHash: &h, TeamID: &team, Format: strp(format)}
		if _, err := db.NewInsert().Model(f).Exec(ctx); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

func splitFormat(format string) (prefix, suffix string) {
	prefix, suffix, _ = strings.Cut(format, "%s")
	return prefix, suffix
}

// POST /challenges/{id}/team-flags {prefix, suffix}: give every team a flag.
// Teams that already have one keep it; to change the format, delete them first.
func (a *API) adminGenerateTeamFlags(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	c, err := a.loadChallengeByID(ctx, r)
	if err != nil {
		return err
	}
	var in struct {
		Prefix string `json:"prefix"`
		Suffix string `json:"suffix"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	fields := map[string]string{}
	for name, v := range map[string]string{"prefix": in.Prefix, "suffix": in.Suffix} {
		if len(v) > 64 || strings.ContainsRune(v, '%') || strings.IndexFunc(v, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			fields[name] = "Use up to 64 characters, with no spaces or %."
		}
	}
	// A flag that a spreadsheet would read as a formula can't go in the CSV as is.
	if in.Prefix != "" && strings.ContainsRune("=+-@", rune(in.Prefix[0])) {
		fields["prefix"] = "Don't start with = + - or @."
	}
	if len(fields) > 0 {
		return errValidation(fields)
	}
	format := in.Prefix + "%s" + in.Suffix
	if have, err := a.teamFlagFormat(ctx, a.store.DB, c.ID); err != nil {
		return err
	} else if have != "" && have != format {
		return errConflict("This challenge already has per-team flags in another format. Delete them first to change it.")
	}
	if _, err := a.ensureTeamFlags(ctx, a.store.DB, c.ID, format, 0); err != nil {
		return err
	}
	a.challengeChanged(ctx, c.Slug)
	return a.respondChallenge(w, ctx, c, http.StatusOK)
}

// DELETE /challenges/{id}/team-flags: remove every per-team flag.
func (a *API) adminDeleteTeamFlags(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	c, err := a.loadChallengeByID(ctx, r)
	if err != nil {
		return err
	}
	if _, err := a.store.DB.NewDelete().Model((*store.Flag)(nil)).
		Where("challenge_id = ? AND kind = ?", c.ID, flags.PerTeam).Exec(ctx); err != nil {
		return err
	}
	a.challengeChanged(ctx, c.Slug)
	return a.respondChallenge(w, ctx, c, http.StatusOK)
}

// GET /challenges/{id}/team-flags[?format=csv]: every team's flag, for
// handing out. Teams that joined since get theirs first.
func (a *API) adminListTeamFlags(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	c, err := a.loadChallengeByID(ctx, r)
	if err != nil {
		return err
	}
	format, err := a.teamFlagFormat(ctx, a.store.DB, c.ID)
	if err != nil {
		return err
	}
	if format == "" {
		return errNotFound
	}
	if _, err := a.ensureTeamFlags(ctx, a.store.DB, c.ID, format, 0); err != nil {
		return err
	}
	var teams []store.Team
	if err := a.store.DB.NewSelect().Model(&teams).
		Where("EXISTS (SELECT 1 FROM flags AS f WHERE f.challenge_id = ? AND f.team_id = team.id)", c.ID).
		Order("name").Scan(ctx); err != nil {
		return err
	}
	rows := make([]teamFlagRow, 0, len(teams))
	for _, t := range teams {
		rows = append(rows, teamFlagRow{idStr(t.ID), t.Name, flags.TeamValue(a.cfg.Secret(), c.ID, t.ID, format)})
	}
	if r.URL.Query().Get("format") != "csv" {
		writeJSON(w, http.StatusOK, rows)
		return nil
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+c.Slug+`-team-flags.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"team_id", "team", "flag"})
	for _, row := range rows {
		_ = cw.Write([]string{csvCell(int64(row.TeamID)), csvCell(row.TeamName), row.Flag})
	}
	cw.Flush()
	return cw.Error()
}

// teamFlagState reports the per-team flag format among a challenge's flags
// ("" when there are none) and whether team already has one.
func teamFlagState(fs []store.Flag, team int64) (format string, has bool) {
	for _, f := range fs {
		if f.Kind != flags.PerTeam {
			continue
		}
		if f.Format != nil && format == "" {
			format = *f.Format
		}
		if f.TeamID != nil && *f.TeamID == team {
			has = true
		}
	}
	return format, has
}
