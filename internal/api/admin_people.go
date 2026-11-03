package api

import (
	"context"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"github.com/flagfall/flagfall/internal/auth"
	"github.com/flagfall/flagfall/internal/store"
)

func (a *API) mountAdminPeople(r chi.Router) {
	r.Get("/users", a.h(a.staff("support", a.adminListUsers)))
	r.Get("/users/{id}", a.h(a.staff("support", a.adminGetUser)))
	r.Patch("/users/{id}", a.h(a.staff("support", a.adminPatchUser)))
	r.Post("/users/{id}/password-reset", a.h(a.staff("support", a.adminResetPassword)))
	r.Get("/staff", a.h(a.staff("support", a.adminListStaff)))

	r.Get("/teams", a.h(a.staff("support", a.adminListTeams)))
	r.Get("/teams/{id}", a.h(a.staff("support", a.adminGetTeam)))
	r.Patch("/teams/{id}", a.h(a.staff("support", a.adminPatchTeam)))
	r.Post("/teams/{id}/invite-code", a.h(a.staff("support", a.adminNewInviteCode)))
	r.Post("/teams/{id}/members", a.h(a.staff("support", a.adminAddMember)))
	r.Delete("/teams/{id}/members/{userID}", a.h(a.staff("support", a.adminRemoveMember)))
	r.Post("/teams/{id}/merge", a.h(a.staff("admin", a.adminMergeTeams)))
}

// paging reads ?page= (from 1) and returns offset and limit.
func paging(r *http.Request, size int) (page, offset, limit int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	return page, (page - 1) * size, size
}

// ---------------------------------------------------------------- users

type adminUserView struct {
	ID        idStr      `json:"id"`
	Name      string     `json:"name"`
	Email     string     `json:"email"`
	Role      string     `json:"role"`
	Country   *string    `json:"country"`
	TeamID    *idStr     `json:"teamId"`
	TeamName  *string    `json:"teamName"`
	Verified  bool       `json:"verified"`
	Banned    bool       `json:"banned"`
	CreatedAt time.Time  `json:"createdAt"`
	LastSeen  *time.Time `json:"lastSeenAt"`
}

type userRow struct {
	store.User `bun:",extend"`
	TeamName   *string    `bun:"team_name"`
	LastSeen   *time.Time `bun:"last_seen"`
}

func (u *userRow) view() adminUserView {
	v := adminUserView{
		ID: idStr(u.ID), Name: u.Name, Email: u.Email, Role: u.Role, Country: u.Country, TeamName: u.TeamName,
		Verified: u.VerifiedAt != nil, Banned: u.BannedAt != nil, CreatedAt: u.CreatedAt.UTC(), LastSeen: utcPtr(u.LastSeen),
	}
	if u.TeamID != nil {
		t := idStr(*u.TeamID)
		v.TeamID = &t
	}
	return v
}

func (a *API) userQuery() *bun.SelectQuery {
	return a.store.DB.NewSelect().TableExpr("users AS u").ColumnExpr("u.*").
		ColumnExpr("t.name AS team_name").
		ColumnExpr("(SELECT MAX(s.last_seen_at) FROM sessions AS s WHERE s.user_id = u.id) AS last_seen").
		Join("LEFT JOIN teams AS t ON t.id = u.team_id")
}

func (a *API) adminListUsers(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	page, offset, limit := paging(r, 50)
	q := a.userQuery()
	if s := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search"))); s != "" {
		like := "%" + escapeLike(s) + "%"
		q = q.Where("(u.name_key LIKE ? ESCAPE '\\' OR u.email_key LIKE ? ESCAPE '\\')", like, like)
	}
	if role := r.URL.Query().Get("role"); role != "" {
		q = q.Where("u.role = ?", role)
	}
	var rows []userRow
	total, err := q.OrderExpr("u.id").Offset(int64(offset)).Limit(int64(limit)).ScanAndCount(ctx, &rows)
	if err != nil {
		return err
	}
	items := []adminUserView{}
	for i := range rows {
		items = append(items, rows[i].view())
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "pageSize": limit})
	return nil
}

func (a *API) adminListStaff(w http.ResponseWriter, r *http.Request) error {
	var rows []userRow
	if err := a.userQuery().Where("u.role <> 'player'").OrderExpr("u.id").Scan(r.Context(), &rows); err != nil {
		return err
	}
	items := []adminUserView{}
	for i := range rows {
		items = append(items, rows[i].view())
	}
	writeJSON(w, http.StatusOK, items)
	return nil
}

func (a *API) loadUserRow(ctx context.Context, id int64) (*userRow, error) {
	u := new(userRow)
	if err := a.userQuery().Where("u.id = ?", id).Scan(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

func (a *API) adminGetUser(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	u, err := a.loadUserRow(r.Context(), id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, u.view())
	return nil
}

// canManage: owners manage anyone; others only people ranked below them.
func canManage(actor, target *store.User) bool {
	return actor.Role == "owner" || roleRank[actor.Role] > roleRank[target.Role]
}

func (a *API) adminPatchUser(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	actor := currentUser(r)
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	p, err := decodePatch(r, "name", "email", "country", "verified", "banned", "role")
	if err != nil {
		return err
	}
	u := new(store.User)
	if err := a.store.DB.NewSelect().Model(u).Where("id = ?", id).Scan(ctx); err != nil {
		return err
	}
	self := u.ID == actor.ID
	wasActiveOwner := u.Role == "owner" && u.BannedAt == nil
	if !self && !canManage(actor, u) {
		return errForbidden("You can only change accounts with a lower role than yours.")
	}

	var cols []string
	if v, ok := p.str("name", 1, 40); ok {
		if msg := validName(v); msg != "" {
			p.fail("name", msg)
		} else if n, _ := a.store.DB.NewSelect().Model((*store.User)(nil)).Where("name_key = ? AND id <> ?", nameKey(v), u.ID).Count(ctx); n > 0 {
			p.fail("name", "That name is taken.")
		} else {
			u.Name, u.NameKey = v, nameKey(v)
			cols = append(cols, "name", "name_key")
		}
	}
	if v, ok := p.str("email", 3, 254); ok {
		if addr, err := mail.ParseAddress(v); err != nil || addr.Address != v {
			p.fail("email", "Enter a valid email address.")
		} else if n, _ := a.store.DB.NewSelect().Model((*store.User)(nil)).Where("email_key = ? AND id <> ?", strings.ToLower(v), u.ID).Count(ctx); n > 0 {
			p.fail("email", "Another account uses this email.")
		} else {
			u.Email, u.EmailKey = v, strings.ToLower(v)
			cols = append(cols, "email", "email_key")
		}
	}
	if p.isNull("country") {
		u.Country = nil
		cols = append(cols, "country")
	} else if v, ok := p.str("country", 0, 2); ok {
		if c, ok := normalizeCountry(v); !ok {
			p.fail("country", "Use a two-letter country code.")
		} else {
			u.Country = strp(c)
			cols = append(cols, "country")
		}
	}
	now := a.now().UTC()
	var verified bool
	if p.get("verified", &verified) {
		if verified && u.VerifiedAt == nil {
			u.VerifiedAt = &now
		} else if !verified {
			u.VerifiedAt = nil
		}
		cols = append(cols, "verified_at")
	}
	var banned, signOut bool
	if p.get("banned", &banned) {
		if self {
			p.fail("banned", "You can't ban yourself.")
		} else if banned && u.BannedAt == nil {
			u.BannedAt, signOut = &now, true
		} else if !banned {
			u.BannedAt = nil
		}
		cols = append(cols, "banned_at")
	}
	if v, ok := p.str("role", 1, 10); ok && v != u.Role {
		switch {
		case actor.Role != "owner":
			p.fail("role", "Only owners can change roles.")
		case roleRank[v] == 0 && v != "player":
			p.fail("role", "Use player, support, author, admin or owner.")
		default:
			u.Role = v
			cols = append(cols, "role")
			signOut = true // sessions start fresh with the new role
		}
	}
	if err := p.err(); err != nil {
		return err
	}
	// Never leave the event without an active owner.
	if wasActiveOwner && (u.Role != "owner" || u.BannedAt != nil) {
		n, err := a.store.DB.NewSelect().Model((*store.User)(nil)).
			Where("role = 'owner' AND banned_at IS NULL AND id <> ?", u.ID).Count(ctx)
		if err != nil {
			return err
		}
		if n == 0 {
			return errConflict("This is the last owner. Make someone else an owner first.")
		}
	}
	if len(cols) > 0 {
		if _, err := a.store.DB.NewUpdate().Model(u).Column(cols...).WherePK().Exec(ctx); err != nil {
			return err
		}
	}
	if signOut && !self {
		if _, err := a.store.DB.NewDelete().Model((*store.Session)(nil)).Where("user_id = ?", u.ID).Exec(ctx); err != nil {
			return err
		}
	}
	if p.has("banned") {
		a.boards.Invalidate()
	}
	row, err := a.loadUserRow(ctx, u.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, row.view())
	return nil
}

func (a *API) adminResetPassword(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	u := new(store.User)
	if err := a.store.DB.NewSelect().Model(u).Where("id = ?", id).Scan(ctx); err != nil {
		return err
	}
	if u.ID != currentUser(r).ID && !canManage(currentUser(r), u) {
		return errForbidden("You can only reset accounts with a lower role than yours.")
	}
	token := auth.RandomToken(32)
	pr := &store.PasswordReset{TokenHash: hashToken(token), UserID: u.ID, ExpiresAt: a.now().UTC().Add(resetLifetime)}
	if _, err := a.store.DB.NewInsert().Model(pr).Exec(ctx); err != nil {
		return err
	}
	link := a.cfg.PublicURL + "/reset-password?token=" + token
	out := map[string]any{"emailed": false}
	if !a.canEmail(ctx) {
		// No SMTP server: hand the link to the staff member to pass on.
		out["link"] = link
	} else {
		body := "An organizer started a password reset for your account.\n\nTo choose a new password, open this link within one hour:\n" + link
		if err := a.mailer.Send(ctx, u.Email, "Reset your password", body); err != nil {
			return err
		}
		out["emailed"] = true
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// ---------------------------------------------------------------- teams

type adminTeamView struct {
	ID            idStr     `json:"id"`
	Name          string    `json:"name"`
	Country       *string   `json:"country"`
	BracketID     idStr     `json:"bracketId"`
	InviteCode    string    `json:"inviteCode"`
	CaptainID     *idStr    `json:"captainId"`
	Hidden        bool      `json:"hidden"`
	Banned        bool      `json:"banned"`
	PrizeEligible bool      `json:"prizeEligible"`
	Recruiting    bool      `json:"recruiting"`
	Members       int       `json:"memberCount"`
	Score         int       `json:"score"`
	Rank          *int      `json:"rank"`
	CreatedAt     time.Time `json:"createdAt"`
}

func (a *API) adminTeamView(t *store.Team, members int, board boardLike) adminTeamView {
	v := adminTeamView{
		ID: idStr(t.ID), Name: t.Name, Country: t.Country, BracketID: idStr(t.BracketID), InviteCode: t.InviteCode,
		Hidden: t.Hidden, Banned: t.BannedAt != nil, PrizeEligible: t.PrizeEligible, Recruiting: t.Recruiting,
		Members: members, CreatedAt: t.CreatedAt.UTC(),
	}
	if t.CaptainID != nil {
		c := idStr(*t.CaptainID)
		v.CaptainID = &c
	}
	if score, rank, ok := board.teamScore(t.ID); ok {
		v.Score = score
		if rank > 0 {
			v.Rank = &rank
		}
	}
	return v
}

func (a *API) adminListTeams(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	page, offset, limit := paging(r, 50)
	q := a.store.DB.NewSelect().Model((*store.Team)(nil))
	if s := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search"))); s != "" {
		q = q.Where("name_key LIKE ? ESCAPE '\\'", "%"+escapeLike(s)+"%")
	}
	var teams []store.Team
	total, err := q.Order("id").Offset(int64(offset)).Limit(int64(limit)).ScanAndCount(ctx, &teams)
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
	board, err := a.staffBoard(ctx)
	if err != nil {
		return err
	}
	items := []adminTeamView{}
	for i := range teams {
		items = append(items, a.adminTeamView(&teams[i], counts[teams[i].ID], board))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "pageSize": limit})
	return nil
}

func (a *API) respondAdminTeam(w http.ResponseWriter, ctx context.Context, id int64) error {
	t, err := a.loadTeam(ctx, id)
	if err != nil {
		return err
	}
	board, err := a.staffBoard(ctx)
	if err != nil {
		return err
	}
	var members []userRow
	if err := a.userQuery().Where("u.team_id = ?", id).OrderExpr("u.id").Scan(ctx, &members); err != nil {
		return err
	}
	mv := []adminUserView{}
	for i := range members {
		mv = append(mv, members[i].view())
	}
	var solves []struct {
		ChallengeID int64     `bun:"challenge_id"`
		Slug        string    `bun:"slug"`
		Name        string    `bun:"name"`
		SolvedAt    time.Time `bun:"solved_at"`
	}
	if err := a.store.DB.NewSelect().TableExpr("solves AS s").ColumnExpr("s.challenge_id, c.slug, c.name, s.solved_at").
		Join("JOIN challenges AS c ON c.id = s.challenge_id").Where("s.team_id = ?", id).
		OrderExpr("s.solved_at").Scan(ctx, &solves); err != nil {
		return err
	}
	type solveView struct {
		ChallengeID idStr     `json:"challengeId"`
		Slug        string    `json:"slug"`
		Name        string    `json:"name"`
		SolvedAt    time.Time `json:"solvedAt"`
	}
	sv := []solveView{}
	for _, s := range solves {
		sv = append(sv, solveView{idStr(s.ChallengeID), s.Slug, s.Name, s.SolvedAt.UTC()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"team": a.adminTeamView(t, len(members), board), "members": mv, "solves": sv})
	return nil
}

func (a *API) adminGetTeam(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	return a.respondAdminTeam(w, r.Context(), id)
}

func (a *API) adminPatchTeam(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	p, err := decodePatch(r, "name", "country", "bracketId", "hidden", "banned", "prizeEligible", "captainId")
	if err != nil {
		return err
	}
	t, err := a.loadTeam(ctx, id)
	if err != nil {
		return err
	}
	var cols []string
	if v, ok := p.str("name", 1, 40); ok {
		if msg := validName(v); msg != "" {
			p.fail("name", msg)
		} else if n, _ := a.store.DB.NewSelect().Model((*store.Team)(nil)).Where("name_key = ? AND id <> ?", nameKey(v), t.ID).Count(ctx); n > 0 {
			p.fail("name", "That name is taken.")
		} else {
			t.Name, t.NameKey = v, nameKey(v)
			cols = append(cols, "name", "name_key")
		}
	}
	if p.isNull("country") {
		t.Country = nil
		cols = append(cols, "country")
	} else if v, ok := p.str("country", 0, 2); ok {
		if c, ok := normalizeCountry(v); !ok {
			p.fail("country", "Use a two-letter country code.")
		} else {
			t.Country = strp(c)
			cols = append(cols, "country")
		}
	}
	var bid idStr
	if p.get("bracketId", &bid) {
		if n, _ := a.store.DB.NewSelect().Model((*store.Bracket)(nil)).Where("id = ?", int64(bid)).Count(ctx); n == 0 {
			p.fail("bracketId", "No such bracket.")
		} else {
			t.BracketID = int64(bid)
			cols = append(cols, "bracket_id")
		}
	}
	if p.get("hidden", &t.Hidden) {
		cols = append(cols, "hidden")
	}
	if p.get("prizeEligible", &t.PrizeEligible) {
		cols = append(cols, "prize_eligible")
	}
	var banned bool
	if p.get("banned", &banned) {
		now := a.now().UTC()
		if banned && t.BannedAt == nil {
			t.BannedAt = &now
		} else if !banned {
			t.BannedAt = nil
		}
		cols = append(cols, "banned_at")
	}
	var cap idStr
	if p.get("captainId", &cap) {
		if n, _ := a.store.DB.NewSelect().Model((*store.User)(nil)).Where("id = ? AND team_id = ?", int64(cap), t.ID).Count(ctx); n == 0 {
			p.fail("captainId", "The captain must be on the team.")
		} else {
			c := int64(cap)
			t.CaptainID = &c
			cols = append(cols, "captain_id")
		}
	}
	if err := p.err(); err != nil {
		return err
	}
	if len(cols) > 0 {
		if _, err := a.store.DB.NewUpdate().Model(t).Column(cols...).WherePK().Exec(ctx); err != nil {
			return err
		}
		a.scoresChanged(ctx)
	}
	return a.respondAdminTeam(w, ctx, t.ID)
}

// scoresChanged clears the board and pushes an update to players.
func (a *API) scoresChanged(ctx context.Context) {
	a.boards.Invalidate()
	if snap, err := a.boards.Get(ctx); err == nil && !snap.Frozen {
		a.publishScoreboard(snap)
	}
}

func (a *API) adminNewInviteCode(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	res, err := a.store.DB.NewUpdate().Model((*store.Team)(nil)).Set("invite_code = ?", newInviteCode()).Where("id = ?", id).Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	return a.respondAdminTeam(w, ctx, id)
}

func (a *API) adminAddMember(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		UserID idStr `json:"userId"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	t, err := a.loadTeam(ctx, id)
	if err != nil {
		return err
	}
	err = a.store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		u := new(store.User)
		if err := tx.NewSelect().Model(u).Where("id = ?", int64(in.UserID)).Scan(ctx); err != nil {
			return errValidation(map[string]string{"userId": "No such user."})
		}
		if u.TeamID != nil && *u.TeamID == t.ID {
			return nil
		}
		if u.TeamID != nil {
			if err := detachMember(ctx, tx, *u.TeamID, u.ID); err != nil {
				return err
			}
		}
		if _, err := tx.NewUpdate().Model(u).Set("team_id = ?", t.ID).WherePK().Exec(ctx); err != nil {
			return err
		}
		if t.CaptainID == nil {
			_, err := tx.NewUpdate().Model(t).Set("captain_id = ?", u.ID).WherePK().Exec(ctx)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	a.scoresChanged(ctx)
	return a.respondAdminTeam(w, ctx, t.ID)
}

// detachMember removes a user from a team, handing the captaincy to the
// longest-standing remaining member if needed.
func detachMember(ctx context.Context, tx bun.Tx, teamID, userID int64) error {
	if _, err := tx.NewUpdate().Model((*store.User)(nil)).Set("team_id = NULL").Where("id = ?", userID).Exec(ctx); err != nil {
		return err
	}
	t := new(store.Team)
	if err := tx.NewSelect().Model(t).Where("id = ?", teamID).Scan(ctx); err != nil {
		return err
	}
	if t.CaptainID == nil || *t.CaptainID != userID {
		return nil
	}
	var next []int64
	if err := tx.NewSelect().Model((*store.User)(nil)).Column("id").Where("team_id = ?", teamID).Order("id").Limit(1).Scan(ctx, &next); err != nil {
		return err
	}
	var cap any
	if len(next) > 0 {
		cap = next[0]
	}
	_, err := tx.NewUpdate().Model(t).Set("captain_id = ?", cap).WherePK().Exec(ctx)
	return err
}

func (a *API) adminRemoveMember(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	uid, err := pathID(r, "userID")
	if err != nil {
		return err
	}
	err = a.store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if n, err := tx.NewSelect().Model((*store.User)(nil)).Where("id = ? AND team_id = ?", uid, id).Count(ctx); err != nil {
			return err
		} else if n == 0 {
			return errNotFound
		}
		return detachMember(ctx, tx, id, uid)
	})
	if err != nil {
		return err
	}
	a.scoresChanged(ctx)
	return a.respondAdminTeam(w, ctx, id)
}

// adminMergeTeams moves everything from one team into another. For a
// challenge both solved, the earlier solve is kept.
func (a *API) adminMergeTeams(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	into, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		FromTeamID idStr `json:"fromTeamId"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	from := int64(in.FromTeamID)
	if from == into {
		return errValidation(map[string]string{"fromTeamId": "Pick a different team."})
	}
	if _, err := a.loadTeam(ctx, into); err != nil {
		return err
	}
	if _, err := a.loadTeam(ctx, from); err != nil {
		return errValidation(map[string]string{"fromTeamId": "No such team."})
	}
	err = a.store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var theirs []store.Solve
		if err := tx.NewSelect().Model(&theirs).Where("team_id = ?", from).Scan(ctx); err != nil {
			return err
		}
		for _, s := range theirs {
			mine := new(store.Solve)
			err := tx.NewSelect().Model(mine).Where("team_id = ? AND challenge_id = ?", into, s.ChallengeID).Scan(ctx)
			if err == nil && !s.SolvedAt.Before(mine.SolvedAt) {
				continue // ours is earlier; theirs is dropped with the team
			}
			if err == nil {
				if _, err := tx.NewDelete().Model(mine).WherePK().Exec(ctx); err != nil {
					return err
				}
			}
			if _, err := tx.NewUpdate().Model((*store.Solve)(nil)).Set("team_id = ?", into).
				Where("team_id = ? AND challenge_id = ?", from, s.ChallengeID).Exec(ctx); err != nil {
				return err
			}
		}
		// Hint unlocks: keep ours where both unlocked the same hint.
		if _, err := tx.NewUpdate().Model((*store.HintUnlock)(nil)).Set("team_id = ?", into).
			Where("team_id = ? AND hint_id NOT IN (SELECT hint_id FROM hint_unlocks WHERE team_id = ?)", from, into).Exec(ctx); err != nil {
			return err
		}
		for _, m := range []any{(*store.Submission)(nil), (*store.ScoreAdjustment)(nil), (*store.User)(nil)} {
			if _, err := tx.NewUpdate().Model(m).Set("team_id = ?", into).Where("team_id = ?", from).Exec(ctx); err != nil {
				return err
			}
		}
		for _, m := range []any{(*store.Solve)(nil), (*store.HintUnlock)(nil)} {
			if _, err := tx.NewDelete().Model(m).Where("team_id = ?", from).Exec(ctx); err != nil {
				return err
			}
		}
		_, err := tx.NewDelete().Model((*store.Team)(nil)).Where("id = ?", from).Exec(ctx)
		return err
	})
	if err != nil {
		return err
	}
	a.scoresChanged(ctx)
	return a.respondAdminTeam(w, ctx, into)
}

// ---------------------------------------------------------------- board helper

type boardLike struct{ teams map[int64][2]int }

func (b boardLike) teamScore(id int64) (score, rank int, ok bool) {
	v, ok := b.teams[id]
	return v[0], v[1], ok
}

func (a *API) staffBoard(ctx context.Context) (boardLike, error) {
	board, err := a.staffSnap(ctx)
	if err != nil {
		return boardLike{}, err
	}
	out := boardLike{teams: map[int64][2]int{}}
	for id, ts := range board.Teams {
		out.teams[id] = [2]int{ts.Score, ts.Rank}
	}
	return out, nil
}
