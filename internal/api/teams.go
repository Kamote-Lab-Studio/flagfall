package api

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"github.com/flagfall/flagfall/internal/store"
)

var errTeamFull = &apiError{Status: 409, Code: "team_full", Message: "This team is full."}

// newInviteCode returns a 10-character code without look-alike characters.
func newInviteCode() string {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

func (a *API) loadTeam(ctx context.Context, id int64) (*store.Team, error) {
	t := new(store.Team)
	err := a.store.DB.NewSelect().Model(t).Where("id = ?", id).Scan(ctx)
	return t, err
}

func (a *API) memberCounts(ctx context.Context, ids []int64) (map[int64]int, error) {
	out := map[int64]int{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		TeamID int64 `bun:"team_id"`
		N      int   `bun:"n"`
	}
	err := a.store.DB.NewSelect().Table("users").ColumnExpr("team_id, COUNT(*) AS n").
		Where("team_id IN (?)", bun.In(ids)).Group("team_id").Scan(ctx, &rows)
	for _, r := range rows {
		out[r.TeamID] = r.N
	}
	return out, err
}

func (a *API) listTeams(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	qs := r.URL.Query()
	page, _ := strconv.Atoi(qs.Get("page"))
	if page < 1 {
		page = 1
	}
	size, _ := strconv.Atoi(qs.Get("pageSize"))
	if size < 1 || size > 200 {
		size = 50
	}

	q := a.store.DB.NewSelect().Model((*store.Team)(nil)).Where("hidden = ?", false).Where("banned_at IS NULL")
	if b, err := strconv.ParseInt(qs.Get("bracketId"), 10, 64); err == nil {
		q = q.Where("bracket_id = ?", b)
	}
	if s := strings.TrimSpace(qs.Get("search")); s != "" {
		q = q.Where(`name_key LIKE ? ESCAPE '\'`, "%"+strings.ToLower(escapeLike(s))+"%")
	}
	if qs.Get("recruiting") == "true" {
		q = q.Where("recruiting = ?", true)
	}

	var teams []store.Team
	total, err := q.Order("name_key").Limit(int64(size)).Offset(int64((page-1)*size)).ScanAndCount(ctx, &teams)
	if err != nil {
		return err
	}
	ids := make([]int64, len(teams))
	for i, t := range teams {
		ids[i] = t.ID
	}
	counts, err := a.memberCounts(ctx, ids)
	if err != nil {
		return err
	}
	snap, err := a.boards.Get(ctx)
	if err != nil {
		return err
	}
	board := snap.Board(false)
	items := make([]teamSummaryView, 0, len(teams))
	for i := range teams {
		items = append(items, summaryFor(&teams[i], counts[teams[i].ID], board))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
	return nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s)
}

func (a *API) getTeam(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		return errNotFound
	}
	t, err := a.loadTeam(ctx, id)
	if err != nil {
		return err
	}
	user := currentUser(r)
	own := user != nil && user.TeamID != nil && *user.TeamID == t.ID
	staff := user != nil && user.IsStaff()
	if (t.Hidden || t.BannedAt != nil) && !own && !staff {
		return errNotFound
	}
	snap, err := a.boards.Get(ctx)
	if err != nil {
		return err
	}
	board := snap.Board(staff)
	if own {
		v, err := a.myTeam(ctx, t, user, board)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, v)
		return nil
	}
	v, err := a.teamDetail(ctx, t, board)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, v)
	return nil
}

// respondMyTeam reloads the caller and writes their team.
func (a *API) respondMyTeam(w http.ResponseWriter, r *http.Request, status int) error {
	ctx := r.Context()
	user := new(store.User)
	if err := a.store.DB.NewSelect().Model(user).Where("id = ?", currentUser(r).ID).Scan(ctx); err != nil {
		return err
	}
	if user.TeamID == nil {
		return errNotFound
	}
	t, err := a.loadTeam(ctx, *user.TeamID)
	if err != nil {
		return err
	}
	snap, err := a.boards.Get(ctx)
	if err != nil {
		return err
	}
	v, err := a.myTeam(ctx, t, user, snap.Board(user.IsStaff()))
	if err != nil {
		return err
	}
	writeJSON(w, status, v)
	return nil
}

func (a *API) checkTeamsUnlocked(ctx context.Context) (*store.Event, error) {
	ev, err := a.store.LoadEvent(ctx)
	if err != nil {
		return nil, err
	}
	if ev.TeamsLocked {
		return nil, errForbidden("Teams are locked for this event. Ask the organizers for changes.")
	}
	return ev, nil
}

func (a *API) createTeam(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	user := currentUser(r)
	var in struct {
		Name      string `json:"name"`
		Country   string `json:"country"`
		BracketID string `json:"bracketId"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if user.TeamID != nil {
		return errConflict("You're already on a team. Leave it first.")
	}
	if _, err := a.checkTeamsUnlocked(ctx); err != nil {
		return err
	}

	in.Name = strings.TrimSpace(in.Name)
	fields := map[string]string{}
	if msg := validName(in.Name); msg != "" {
		fields["name"] = msg
	} else if n, _ := a.store.DB.NewSelect().Model((*store.Team)(nil)).Where("name_key = ?", nameKey(in.Name)).Count(ctx); n > 0 {
		fields["name"] = "That team name is taken."
	}
	country, ok := normalizeCountry(in.Country)
	if !ok {
		fields["country"] = "Use a two-letter country code."
	}
	bracket := new(store.Bracket)
	bracketID, _ := strconv.ParseInt(in.BracketID, 10, 64)
	if err := a.store.DB.NewSelect().Model(bracket).Where("id = ?", bracketID).Scan(ctx); err != nil {
		fields["bracketId"] = "Choose a bracket."
	} else if bracket.RequiresVerification && user.VerifiedAt == nil {
		fields["bracketId"] = "This bracket needs a verified account."
	}
	if len(fields) > 0 {
		return errValidation(fields)
	}

	err := a.store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		t := &store.Team{
			Name: in.Name, NameKey: nameKey(in.Name), Country: strp(country), BracketID: bracket.ID,
			InviteCode: newInviteCode(), CaptainID: &user.ID, PrizeEligible: true, CreatedAt: a.now().UTC(),
		}
		if _, err := tx.NewInsert().Model(t).Exec(ctx); err != nil {
			return errValidation(map[string]string{"name": "That team name is taken."})
		}
		res, err := tx.NewUpdate().Model((*store.User)(nil)).Set("team_id = ?", t.ID).
			Where("id = ? AND team_id IS NULL", user.ID).Exec(ctx)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return errConflict("You're already on a team. Leave it first.")
		}
		return nil
	})
	if err != nil {
		return err
	}
	a.boards.Invalidate()
	return a.respondMyTeam(w, r, http.StatusCreated)
}

func (a *API) joinTeam(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	user := currentUser(r)
	var in struct {
		InviteCode string `json:"inviteCode"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if user.TeamID != nil {
		return errConflict("You're already on a team. Leave it first.")
	}
	if ok, wait := a.limitJoin.Allow("u:" + strconv.FormatInt(user.ID, 10)); !ok {
		return errRateLimited(wait)
	}
	ev, err := a.checkTeamsUnlocked(ctx)
	if err != nil {
		return err
	}
	badCode := errValidation(map[string]string{"inviteCode": "That invite code doesn't match a team."})
	t := new(store.Team)
	code := strings.ToUpper(strings.TrimSpace(in.InviteCode))
	if err := a.store.DB.NewSelect().Model(t).Where("invite_code = ?", code).Scan(ctx); err != nil || t.BannedAt != nil {
		return badCode
	}
	bracket := new(store.Bracket)
	if err := a.store.DB.NewSelect().Model(bracket).Where("id = ?", t.BracketID).Scan(ctx); err != nil {
		return err
	}
	if bracket.RequiresVerification && user.VerifiedAt == nil {
		return errForbidden("This team's bracket needs a verified account.")
	}

	// SQLite transactions start IMMEDIATE (one writer); on PostgreSQL the
	// team row is locked so two joins can't both take the last place.
	err = a.store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		lock := tx.NewSelect().Model((*store.Team)(nil)).Column("id").Where("id = ?", t.ID)
		if a.store.Dialect == store.Postgres {
			lock = lock.For("UPDATE")
		}
		var locked int64
		if err := lock.Scan(ctx, &locked); err != nil {
			return err
		}
		n, err := tx.NewSelect().Model((*store.User)(nil)).Where("team_id = ?", t.ID).Count(ctx)
		if err != nil {
			return err
		}
		if n >= int64(ev.MaxTeamSize) {
			return errTeamFull
		}
		res, err := tx.NewUpdate().Model((*store.User)(nil)).Set("team_id = ?", t.ID).
			Where("id = ? AND team_id IS NULL", user.ID).Exec(ctx)
		if err != nil {
			return err
		}
		if rows, _ := res.RowsAffected(); rows != 1 {
			return errConflict("You're already on a team. Leave it first.")
		}
		if t.CaptainID == nil { // team was empty
			_, err = tx.NewUpdate().Model((*store.Team)(nil)).Set("captain_id = ?", user.ID).Where("id = ?", t.ID).Exec(ctx)
		}
		return err
	})
	if err != nil {
		return err
	}
	a.boards.Invalidate()
	return a.respondMyTeam(w, r, http.StatusOK)
}

func (a *API) leaveTeam(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	user := currentUser(r)
	if user.TeamID == nil {
		return errConflict("You're not on a team.")
	}
	if _, err := a.checkTeamsUnlocked(ctx); err != nil {
		return err
	}
	t, err := a.loadTeam(ctx, *user.TeamID)
	if err != nil {
		return err
	}
	err = a.store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		others, err := tx.NewSelect().Model((*store.User)(nil)).Where("team_id = ? AND id <> ?", t.ID, user.ID).Count(ctx)
		if err != nil {
			return err
		}
		isCaptain := t.CaptainID != nil && *t.CaptainID == user.ID
		if isCaptain && others > 0 {
			return errConflict("Make another member captain before you leave.")
		}
		if _, err := tx.NewUpdate().Model((*store.User)(nil)).Set("team_id = NULL").Where("id = ?", user.ID).Exec(ctx); err != nil {
			return err
		}
		if others > 0 {
			return nil
		}
		// Last member out. Keep the team if it has scored, so results stay explainable.
		solves, err := tx.NewSelect().Model((*store.Solve)(nil)).Where("team_id = ?", t.ID).Count(ctx)
		if err != nil {
			return err
		}
		if solves > 0 {
			_, err = tx.NewUpdate().Model((*store.Team)(nil)).Set("captain_id = NULL").Where("id = ?", t.ID).Exec(ctx)
			return err
		}
		for _, m := range []any{(*store.HintUnlock)(nil), (*store.Submission)(nil), (*store.ScoreAdjustment)(nil), (*store.Flag)(nil)} {
			if _, err := tx.NewDelete().Model(m).Where("team_id = ?", t.ID).Exec(ctx); err != nil {
				return err
			}
		}
		_, err = tx.NewDelete().Model((*store.Team)(nil)).Where("id = ?", t.ID).Exec(ctx)
		return err
	})
	if err != nil {
		return err
	}
	a.boards.Invalidate()
	return noContent(w)
}

// captainTeam returns the caller's team if they are its captain.
func (a *API) captainTeam(r *http.Request) (*store.Team, error) {
	user := currentUser(r)
	if user.TeamID == nil {
		return nil, errConflict("You're not on a team.")
	}
	t, err := a.loadTeam(r.Context(), *user.TeamID)
	if err != nil {
		return nil, err
	}
	if t.CaptainID == nil || *t.CaptainID != user.ID {
		return nil, errForbidden("Only the team captain can do that.")
	}
	return t, nil
}

func (a *API) updateMyTeam(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	t, err := a.captainTeam(r)
	if err != nil {
		return err
	}
	var in struct {
		Name       *string `json:"name"`
		Country    *string `json:"country"`
		Recruiting *bool   `json:"recruiting"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	q := a.store.DB.NewUpdate().Model(t).WherePK()
	cols := 0
	fields := map[string]string{}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if msg := validName(name); msg != "" {
			fields["name"] = msg
		} else if n, _ := a.store.DB.NewSelect().Model((*store.Team)(nil)).Where("name_key = ? AND id <> ?", nameKey(name), t.ID).Count(ctx); n > 0 {
			fields["name"] = "That team name is taken."
		} else {
			t.Name, t.NameKey = name, nameKey(name)
			q, cols = q.Column("name", "name_key"), cols+1
		}
	}
	if in.Country != nil {
		c, ok := normalizeCountry(*in.Country)
		if !ok {
			fields["country"] = "Use a two-letter country code."
		} else {
			t.Country = strp(c)
			q, cols = q.Column("country"), cols+1
		}
	}
	if in.Recruiting != nil {
		t.Recruiting = *in.Recruiting
		q, cols = q.Column("recruiting"), cols+1
	}
	if len(fields) > 0 {
		return errValidation(fields)
	}
	if cols > 0 {
		if _, err := q.Exec(ctx); err != nil {
			return err
		}
		a.boards.Invalidate()
	}
	return a.respondMyTeam(w, r, http.StatusOK)
}

func (a *API) regenerateInvite(w http.ResponseWriter, r *http.Request) error {
	t, err := a.captainTeam(r)
	if err != nil {
		return err
	}
	if _, err := a.store.DB.NewUpdate().Model((*store.Team)(nil)).Set("invite_code = ?", newInviteCode()).Where("id = ?", t.ID).Exec(r.Context()); err != nil {
		return err
	}
	return a.respondMyTeam(w, r, http.StatusOK)
}

func (a *API) memberOf(ctx context.Context, teamID int64, userIDParam string) (*store.User, error) {
	id, err := strconv.ParseInt(userIDParam, 10, 64)
	if err != nil {
		return nil, errNotFound
	}
	m := new(store.User)
	if err := a.store.DB.NewSelect().Model(m).Where("id = ? AND team_id = ?", id, teamID).Scan(ctx); err != nil {
		return nil, errNotFound
	}
	return m, nil
}

func (a *API) removeMember(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	t, err := a.captainTeam(r)
	if err != nil {
		return err
	}
	if _, err := a.checkTeamsUnlocked(ctx); err != nil {
		return err
	}
	m, err := a.memberOf(ctx, t.ID, chi.URLParam(r, "userID"))
	if err != nil {
		return err
	}
	if m.ID == currentUser(r).ID {
		return errConflict("You can't remove yourself. Leave the team instead.")
	}
	ev, err := a.store.LoadEvent(ctx)
	if err != nil {
		return err
	}
	if ev.AcceptsSubmissions(a.now()) {
		n, err := a.store.DB.NewSelect().Model((*store.Solve)(nil)).Where("team_id = ? AND user_id = ?", t.ID, m.ID).Count(ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			return errConflict("This member has solves, so they can't be removed during the event. Ask the organizers.")
		}
	}
	if _, err := a.store.DB.NewUpdate().Model((*store.User)(nil)).Set("team_id = NULL").Where("id = ?", m.ID).Exec(ctx); err != nil {
		return err
	}
	a.boards.Invalidate()
	return a.respondMyTeam(w, r, http.StatusOK)
}

func (a *API) transferCaptain(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	t, err := a.captainTeam(r)
	if err != nil {
		return err
	}
	var in struct {
		UserID string `json:"userId"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	m, err := a.memberOf(ctx, t.ID, in.UserID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return errValidation(map[string]string{"userId": "Choose a member of your team."})
		}
		return err
	}
	if _, err := a.store.DB.NewUpdate().Model((*store.Team)(nil)).Set("captain_id = ?", m.ID).Where("id = ?", t.ID).Exec(ctx); err != nil {
		return err
	}
	return a.respondMyTeam(w, r, http.StatusOK)
}
