package api

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // time zone names work on every OS

	"github.com/go-chi/chi/v5"

	"github.com/Kamote-Lab-Studio/flagfall/internal/store"
)

// ---------------------------------------------------------------- PATCH bodies

// patch is a decoded PATCH body. It tells "absent" apart from "null" and
// collects per-field validation errors.
type patch struct {
	raw    map[string]json.RawMessage
	fields map[string]string
}

func decodePatch(r *http.Request, allowed ...string) (*patch, error) {
	p := &patch{raw: map[string]json.RawMessage{}, fields: map[string]string{}}
	b, err := io.ReadAll(io.LimitReader(r.Body, 256<<10))
	if err != nil || json.Unmarshal(b, &p.raw) != nil {
		return nil, errBadRequest("The request body isn't a JSON object.")
	}
	ok := map[string]bool{}
	for _, k := range allowed {
		ok[k] = true
	}
	var unknown []string
	for k := range p.raw {
		if !ok[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, errBadRequest("Unknown fields: " + strings.Join(unknown, ", ") + ".")
	}
	return p, nil
}

func (p *patch) has(k string) bool { _, ok := p.raw[k]; return ok }
func (p *patch) isNull(k string) bool {
	v, ok := p.raw[k]
	return ok && string(v) == "null"
}

// get decodes field k into dst if present. It returns false when absent or
// invalid (and records the error).
func (p *patch) get(k string, dst any) bool {
	v, ok := p.raw[k]
	if !ok {
		return false
	}
	if err := json.Unmarshal(v, dst); err != nil {
		p.fields[k] = "Wrong type."
		return false
	}
	return true
}

func (p *patch) fail(k, msg string) { p.fields[k] = msg }

func (p *patch) err() error {
	if len(p.fields) > 0 {
		return errValidation(p.fields)
	}
	return nil
}

// str reads a trimmed string with a length limit.
func (p *patch) str(k string, min, max int) (string, bool) {
	var s string
	if !p.get(k, &s) {
		return "", false
	}
	s = strings.TrimSpace(s)
	if len(s) < min || len(s) > max {
		if min > 0 {
			p.fail(k, "Use "+strconv.Itoa(min)+" to "+strconv.Itoa(max)+" characters.")
		} else {
			p.fail(k, "Use at most "+strconv.Itoa(max)+" characters.")
		}
		return "", false
	}
	return s, true
}

func (p *patch) intRange(k string, min, max int) (int, bool) {
	var n int
	if !p.get(k, &n) {
		return 0, false
	}
	if n < min || n > max {
		p.fail(k, "Use a number from "+strconv.Itoa(min)+" to "+strconv.Itoa(max)+".")
		return 0, false
	}
	return n, true
}

// optTime reads a nullable timestamp. set is true when the field is present.
func (p *patch) optTime(k string) (t *time.Time, set bool) {
	if !p.has(k) {
		return nil, false
	}
	if p.isNull(k) {
		return nil, true
	}
	var v time.Time
	if !p.get(k, &v) {
		return nil, false
	}
	v = v.UTC()
	return &v, true
}

func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		return 0, errNotFound
	}
	return id, nil
}

// ---------------------------------------------------------------- event

type adminEventView struct {
	Name             string     `json:"name"`
	State            string     `json:"state"`
	StartsAt         *time.Time `json:"startsAt"`
	EndsAt           *time.Time `json:"endsAt"`
	FreezeAt         *time.Time `json:"freezeAt"`
	TimeZone         string     `json:"timeZone"`
	MaxTeamSize      int        `json:"maxTeamSize"`
	ScoringMode      string     `json:"scoringMode"`
	RegistrationOpen bool       `json:"registrationOpen"`
	TeamsLocked      bool       `json:"teamsLocked"`
	PublicChallenges bool       `json:"publicChallenges"`
	FlagRateLimit    int        `json:"flagRateLimit"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

func (a *API) adminEventView(ev *store.Event) adminEventView {
	return adminEventView{
		Name: ev.Name, State: ev.State(a.now()), StartsAt: utcPtr(ev.StartsAt), EndsAt: utcPtr(ev.EndsAt),
		FreezeAt: utcPtr(ev.FreezeAt), TimeZone: ev.TimeZone, MaxTeamSize: ev.MaxTeamSize, ScoringMode: ev.ScoringMode,
		RegistrationOpen: ev.RegistrationOpen, TeamsLocked: ev.TeamsLocked, PublicChallenges: ev.PublicChallenges,
		FlagRateLimit: ev.FlagRateLimit, UpdatedAt: ev.UpdatedAt.UTC(),
	}
}

func (a *API) adminGetEvent(w http.ResponseWriter, r *http.Request) error {
	ev, err := a.store.LoadEvent(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, a.adminEventView(ev))
	return nil
}

func (a *API) adminPatchEvent(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	p, err := decodePatch(r, "name", "startsAt", "endsAt", "freezeAt", "timeZone", "maxTeamSize", "scoringMode",
		"registrationOpen", "teamsLocked", "publicChallenges", "flagRateLimit")
	if err != nil {
		return err
	}
	ev, err := a.store.LoadEvent(ctx)
	if err != nil {
		return err
	}

	q := a.store.DB.NewUpdate().Model(ev).WherePK()
	var cols []string
	set := func(col string) { cols = append(cols, col) }

	if v, ok := p.str("name", 1, 80); ok {
		ev.Name = v
		set("name")
	}
	if t, ok := p.optTime("startsAt"); ok {
		ev.StartsAt = t
		set("starts_at")
	}
	if t, ok := p.optTime("endsAt"); ok {
		ev.EndsAt = t
		set("ends_at")
	}
	if t, ok := p.optTime("freezeAt"); ok {
		ev.FreezeAt = t
		set("freeze_at")
	}
	if v, ok := p.str("timeZone", 1, 64); ok {
		if _, err := time.LoadLocation(v); err != nil {
			p.fail("timeZone", "Use an IANA time zone name, like Asia/Manila or UTC.")
		} else {
			ev.TimeZone = v
			set("time_zone")
		}
	}
	if v, ok := p.intRange("maxTeamSize", 1, 100); ok {
		ev.MaxTeamSize = v
		set("max_team_size")
	}
	if v, ok := p.str("scoringMode", 1, 16); ok {
		if v != "static" && v != "dynamic" {
			p.fail("scoringMode", `Use "static" or "dynamic".`)
		} else {
			ev.ScoringMode = v
			set("scoring_mode")
		}
	}
	for _, b := range []struct {
		key, col string
		dst      *bool
	}{
		{"registrationOpen", "registration_open", &ev.RegistrationOpen},
		{"teamsLocked", "teams_locked", &ev.TeamsLocked},
		{"publicChallenges", "public_challenges", &ev.PublicChallenges},
	} {
		if p.get(b.key, b.dst) {
			set(b.col)
		}
	}
	if v, ok := p.intRange("flagRateLimit", 1, 1000); ok {
		ev.FlagRateLimit = v
		set("flag_rate_limit")
	}

	if ev.StartsAt != nil && ev.EndsAt != nil && !ev.StartsAt.Before(*ev.EndsAt) {
		p.fail("endsAt", "The end must be after the start.")
	}
	if ev.FreezeAt != nil {
		if ev.StartsAt != nil && !ev.FreezeAt.After(*ev.StartsAt) {
			p.fail("freezeAt", "The freeze must be after the start.")
		}
		if ev.EndsAt != nil && ev.FreezeAt.After(*ev.EndsAt) {
			p.fail("freezeAt", "The freeze can't be after the end.")
		}
	}
	if err := p.err(); err != nil {
		return err
	}
	if len(cols) > 0 {
		ev.UpdatedAt = a.now().UTC()
		set("updated_at")
		if _, err := q.Column(cols...).Exec(ctx); err != nil {
			return err
		}
		a.boards.Invalidate()
		// Sent even when the state is unchanged: dates or the freeze may have
		// moved, and clients refetch the event on this message.
		a.publish("event-state", map[string]string{"state": ev.State(a.now())})
		if snap, err := a.boards.Get(ctx); err == nil {
			a.publishScoreboard(snap)
		}
	}
	writeJSON(w, http.StatusOK, a.adminEventView(ev))
	return nil
}

// ---------------------------------------------------------------- brackets

type adminBracketView struct {
	ID                   idStr  `json:"id"`
	Name                 string `json:"name"`
	Description          string `json:"description"`
	RequiresVerification bool   `json:"requiresVerification"`
	Position             int    `json:"position"`
	Teams                int    `json:"teams"`
}

func (a *API) adminListBrackets(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var bs []store.Bracket
	if err := a.store.DB.NewSelect().Model(&bs).Order("position", "id").Scan(ctx); err != nil {
		return err
	}
	var counts []struct {
		BracketID int64 `bun:"bracket_id"`
		N         int   `bun:"n"`
	}
	if err := a.store.DB.NewSelect().Model((*store.Team)(nil)).Column("bracket_id").ColumnExpr("COUNT(*) AS n").
		Group("bracket_id").Scan(ctx, &counts); err != nil {
		return err
	}
	byID := map[int64]int{}
	for _, c := range counts {
		byID[c.BracketID] = c.N
	}
	out := []adminBracketView{}
	for _, b := range bs {
		out = append(out, adminBracketView{idStr(b.ID), b.Name, b.Description, b.RequiresVerification, b.Position, byID[b.ID]})
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) applyBracketPatch(p *patch, b *store.Bracket) {
	if v, ok := p.str("name", 1, 40); ok {
		b.Name = v
	}
	if v, ok := p.str("description", 0, 200); ok {
		b.Description = v
	}
	p.get("requiresVerification", &b.RequiresVerification)
	if v, ok := p.intRange("position", 0, 10000); ok {
		b.Position = v
	}
}

func (a *API) adminCreateBracket(w http.ResponseWriter, r *http.Request) error {
	p, err := decodePatch(r, "name", "description", "requiresVerification", "position")
	if err != nil {
		return err
	}
	b := &store.Bracket{}
	a.applyBracketPatch(p, b)
	if !p.has("name") {
		p.fail("name", "Required.")
	}
	if err := p.err(); err != nil {
		return err
	}
	if _, err := a.store.DB.NewInsert().Model(b).Exec(r.Context()); err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, adminBracketView{idStr(b.ID), b.Name, b.Description, b.RequiresVerification, b.Position, 0})
	return nil
}

func (a *API) adminUpdateBracket(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	p, err := decodePatch(r, "name", "description", "requiresVerification", "position")
	if err != nil {
		return err
	}
	b := new(store.Bracket)
	if err := a.store.DB.NewSelect().Model(b).Where("id = ?", id).Scan(r.Context()); err != nil {
		return err
	}
	a.applyBracketPatch(p, b)
	if err := p.err(); err != nil {
		return err
	}
	if _, err := a.store.DB.NewUpdate().Model(b).WherePK().Exec(r.Context()); err != nil {
		return err
	}
	a.boards.Invalidate()
	writeJSON(w, http.StatusOK, adminBracketView{idStr(b.ID), b.Name, b.Description, b.RequiresVerification, b.Position, 0})
	return nil
}

func (a *API) adminDeleteBracket(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if n, err := a.store.DB.NewSelect().Model((*store.Team)(nil)).Where("bracket_id = ?", id).Count(ctx); err != nil {
		return err
	} else if n > 0 {
		return errConflict("Teams are in this bracket. Move them first.")
	}
	if n, err := a.store.DB.NewSelect().Model((*store.Bracket)(nil)).Count(ctx); err != nil {
		return err
	} else if n <= 1 {
		return errConflict("An event needs at least one bracket.")
	}
	res, err := a.store.DB.NewDelete().Model((*store.Bracket)(nil)).Where("id = ?", id).Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	return noContent(w)
}

// ---------------------------------------------------------------- audit log

func (a *API) adminListAudit(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	limit := 100
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 500 {
		limit = n
	}
	var rows []struct {
		store.AuditEntry `bun:",extend"`
		ActorName        *string `bun:"actor_name"`
	}
	q := a.store.DB.NewSelect().TableExpr("audit_log AS a").ColumnExpr("a.*").ColumnExpr("u.name AS actor_name").
		Join("LEFT JOIN users AS u ON u.id = a.actor_id").OrderExpr("a.id DESC").Limit(int64(limit))
	if b, err := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64); err == nil {
		q = q.Where("a.id < ?", b)
	}
	if actor, err := strconv.ParseInt(r.URL.Query().Get("actorId"), 10, 64); err == nil {
		q = q.Where("a.actor_id = ?", actor)
	}
	if err := q.Scan(ctx, &rows); err != nil {
		return err
	}
	type item struct {
		ID        idStr     `json:"id"`
		ActorID   *idStr    `json:"actorId"`
		ActorName *string   `json:"actorName"`
		Action    string    `json:"action"`
		Target    *string   `json:"target"`
		Details   *string   `json:"details"`
		IP        *string   `json:"ip"`
		At        time.Time `json:"at"`
	}
	out := []item{}
	for _, e := range rows {
		it := item{ID: idStr(e.ID), ActorName: e.ActorName, Action: e.Action, Target: e.Target, Details: e.After, IP: e.IP, At: e.At.UTC()}
		if e.ActorID != nil {
			v := idStr(*e.ActorID)
			it.ActorID = &v
		}
		out = append(out, it)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}
