package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"github.com/flagfall/flagfall/internal/blobs"
	"github.com/flagfall/flagfall/internal/flags"
	"github.com/flagfall/flagfall/internal/markdown"
	"github.com/flagfall/flagfall/internal/scoring"
	"github.com/flagfall/flagfall/internal/store"
)

type challengeSummaryView struct {
	Slug        string         `json:"slug"`
	Name        string         `json:"name"`
	Category    string         `json:"category"`
	Difficulty  string         `json:"difficulty"`
	Value       int            `json:"value"`
	SolveCount  int            `json:"solveCount"`
	Solved      bool           `json:"solved"`
	ThemeFields map[string]any `json:"themeFields"`
}

type hintView struct {
	ID      idStr   `json:"id"`
	Cost    int     `json:"cost"`
	Content *string `json:"content"`
}

type fileView struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	URL    string `json:"url"`
}

type firstBloodView struct {
	TeamID   idStr     `json:"teamId"`
	TeamName string    `json:"teamName"`
	SolvedAt time.Time `json:"solvedAt"`
}

type challengeView struct {
	challengeSummaryView
	DescriptionHTML string          `json:"descriptionHtml"`
	Author          string          `json:"author"`
	Connection      *string         `json:"connection"`
	Files           []fileView      `json:"files"`
	Hints           []hintView      `json:"hints"`
	FirstBlood      *firstBloodView `json:"firstBlood"`
}

// challengeAccess decides whether the caller may see challenges at all.
// Staff always may; players may once the event has started (and signed in,
// unless the organizer made the board public).
func (a *API) challengeAccess(ctx context.Context, r *http.Request) (*store.Event, bool, error) {
	ev, err := a.store.LoadEvent(ctx)
	if err != nil {
		return nil, false, err
	}
	user := currentUser(r)
	if user != nil && user.IsStaff() {
		return ev, true, nil
	}
	if ev.State(a.now()) == store.StateNotStarted {
		return ev, false, nil
	}
	if user == nil && !ev.PublicChallenges {
		return ev, false, errUnauthenticated
	}
	return ev, true, nil
}

func (a *API) visibleChallenge(ctx context.Context, r *http.Request, slug string) (*store.Event, *store.Challenge, error) {
	ev, ok, err := a.challengeAccess(ctx, r)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, errNotFound
	}
	c := new(store.Challenge)
	if err := a.store.DB.NewSelect().Model(c).Where("slug = ?", slug).Scan(ctx); err != nil {
		return nil, nil, errNotFound
	}
	user := currentUser(r)
	if !c.VisibleAt(a.now()) && (user == nil || !user.IsStaff()) {
		return nil, nil, errNotFound
	}
	return ev, c, nil
}

func (a *API) teamSolvedSet(ctx context.Context, user *store.User) (map[int64]bool, error) {
	out := map[int64]bool{}
	if user == nil || user.TeamID == nil {
		return out, nil
	}
	var solves []store.Solve
	err := a.store.DB.NewSelect().Model(&solves).Column("challenge_id").Where("team_id = ?", *user.TeamID).Scan(ctx)
	for _, s := range solves {
		out[s.ChallengeID] = true
	}
	return out, err
}

func summarize(c *store.Challenge, board *scoring.Board, solved map[int64]bool) challengeSummaryView {
	v := challengeSummaryView{
		Slug: c.Slug, Name: c.Name, Category: c.Category,
		Difficulty: lowerOr(c.Difficulty, "medium"), Value: c.Value,
		Solved: solved[c.ID], ThemeFields: map[string]any{},
	}
	if st := board.Challenges[c.ID]; st != nil {
		v.Value, v.SolveCount = st.Value, st.SolveCount
	}
	_ = json.Unmarshal([]byte(c.ThemeFields), &v.ThemeFields)
	return v
}

func (a *API) listChallenges(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	_, ok, err := a.challengeAccess(ctx, r)
	if err != nil {
		return err
	}
	out := []challengeSummaryView{}
	if !ok {
		writeJSON(w, http.StatusOK, out)
		return nil
	}
	user := currentUser(r)
	var chals []store.Challenge
	if err := a.store.DB.NewSelect().Model(&chals).Order("position", "id").Scan(ctx); err != nil {
		return err
	}
	snap, err := a.boards.Get(ctx)
	if err != nil {
		return err
	}
	solved, err := a.teamSolvedSet(ctx, user)
	if err != nil {
		return err
	}
	staff := user != nil && user.IsStaff()
	board := snap.Board(staff)
	for i := range chals {
		if chals[i].VisibleAt(a.now()) || staff {
			out = append(out, summarize(&chals[i], board, solved))
		}
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) eventDomain() string {
	u, _ := url.Parse(a.cfg.PublicURL)
	return u.Hostname()
}

func (a *API) getChallenge(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	_, c, err := a.visibleChallenge(ctx, r, chi.URLParam(r, "slug"))
	if err != nil {
		return err
	}
	user := currentUser(r)
	snap, err := a.boards.Get(ctx)
	if err != nil {
		return err
	}
	board := snap.Board(user != nil && user.IsStaff())
	solved, err := a.teamSolvedSet(ctx, user)
	if err != nil {
		return err
	}

	var hints []store.Hint
	if err := a.store.DB.NewSelect().Model(&hints).Where("challenge_id = ?", c.ID).Order("position", "id").Scan(ctx); err != nil {
		return err
	}
	unlocked := map[int64]bool{}
	if user != nil && user.TeamID != nil {
		var us []store.HintUnlock
		if err := a.store.DB.NewSelect().Model(&us).Where("team_id = ?", *user.TeamID).Scan(ctx); err != nil {
			return err
		}
		for _, u := range us {
			unlocked[u.HintID] = true
		}
	}
	var files []store.File
	if err := a.store.DB.NewSelect().Model(&files).Where("challenge_id = ?", c.ID).Order("name").Scan(ctx); err != nil {
		return err
	}

	v := challengeView{
		challengeSummaryView: summarize(c, board, solved),
		DescriptionHTML:      markdown.Render(c.DescriptionMD),
		Author:               c.Author,
		Files:                []fileView{},
		Hints:                []hintView{},
	}
	if c.Connection != nil && *c.Connection != "" {
		conn := strings.ReplaceAll(*c.Connection, "{{event_domain}}", a.eventDomain())
		v.Connection = &conn
	}
	for _, f := range files {
		v.Files = append(v.Files, fileView{
			Name: f.Name, Size: f.Size, SHA256: f.SHA256,
			URL: "/files/" + f.SHA256 + "/" + url.PathEscape(f.Name),
		})
	}
	for _, h := range hints {
		hv := hintView{ID: idStr(h.ID), Cost: h.Cost}
		if h.Cost == 0 || unlocked[h.ID] {
			html := markdown.Render(h.ContentMD)
			hv.Content = &html
		}
		v.Hints = append(v.Hints, hv)
	}
	if st := board.Challenges[c.ID]; st != nil && st.FirstBlood != nil {
		if ts := board.Teams[st.FirstBlood.TeamID]; ts != nil {
			v.FirstBlood = &firstBloodView{TeamID: idStr(ts.Team.ID), TeamName: ts.Team.Name, SolvedAt: st.FirstBlood.At.UTC()}
		}
	}
	writeJSON(w, http.StatusOK, v)
	return nil
}

// playingTeam returns the caller's team if it may play right now.
func (a *API) playingTeam(ctx context.Context, user *store.User) (*store.Team, error) {
	if user.TeamID == nil {
		return nil, errForbidden("Join or create a team first.")
	}
	t, err := a.loadTeam(ctx, *user.TeamID)
	if err != nil {
		return nil, err
	}
	if t.BannedAt != nil {
		return nil, errForbidden("Your team is suspended. Contact the organizers.")
	}
	return t, nil
}

func (a *API) submitFlag(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	user := currentUser(r)
	var in struct {
		Flag string `json:"flag"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	ev, c, err := a.visibleChallenge(ctx, r, chi.URLParam(r, "slug"))
	if err != nil {
		return err
	}
	team, err := a.playingTeam(ctx, user)
	if err != nil {
		return err
	}
	now := a.now().UTC()
	if !ev.AcceptsSubmissions(now) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "event_not_running", "state": ev.State(now)})
		return nil
	}
	if len(in.Flag) > flags.MaxLength {
		return errValidation(map[string]string{"flag": "That's longer than any flag."})
	}

	already, err := a.store.DB.NewSelect().Model((*store.Solve)(nil)).Where("team_id = ? AND challenge_id = ?", team.ID, c.ID).Exists(ctx)
	if err != nil {
		return err
	}
	if already {
		writeJSON(w, http.StatusOK, map[string]any{"status": "already_solved"})
		return nil
	}
	if ok, wait := a.limitFlags.AllowN("t:"+strconv.FormatInt(team.ID, 10), ev.FlagRateLimit); !ok {
		writeJSON(w, http.StatusOK, map[string]any{"status": "rate_limited", "retryAfterSeconds": int(wait.Seconds() + 0.999)})
		return nil
	}

	var stored []store.Flag
	if err := a.store.DB.NewSelect().Model(&stored).Where("challenge_id = ?", c.ID).Scan(ctx); err != nil {
		return err
	}
	check := make([]flags.Flag, len(stored))
	for i, f := range stored {
		check[i] = flags.Flag{Kind: f.Kind, ValueHash: deref(f.ValueHash), Pattern: deref(f.Pattern), TeamID: f.TeamID}
	}
	correct := flags.Any(a.cfg.Secret(), check, in.Flag, team.ID)

	sub := &store.Submission{
		TeamID: team.ID, UserID: &user.ID, ChallengeID: c.ID, Correct: correct,
		Submitted: flags.Normalize(in.Flag), IP: strp(clientIP(r)), SubmittedAt: now,
	}
	solvedNow := false
	err = a.store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(sub).Exec(ctx); err != nil {
			return err
		}
		if !correct {
			return nil
		}
		res, err := tx.NewInsert().Model(&store.Solve{
			TeamID: team.ID, ChallengeID: c.ID, UserID: &user.ID, SubmissionID: sub.ID, SolvedAt: now,
		}).On("CONFLICT DO NOTHING").Exec(ctx)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		solvedNow = n == 1
		return nil
	})
	if err != nil {
		return err
	}
	if !correct {
		writeJSON(w, http.StatusOK, map[string]any{"status": "incorrect"})
		return nil
	}
	if !solvedNow { // a teammate solved it a moment ago
		writeJSON(w, http.StatusOK, map[string]any{"status": "already_solved"})
		return nil
	}

	a.boards.Invalidate()
	snap, err := a.boards.Get(ctx)
	if err != nil {
		return err
	}
	st := snap.Full.Challenges[c.ID]
	firstBlood := st != nil && st.FirstBlood != nil && st.FirstBlood.TeamID == team.ID
	points := c.Value
	if st != nil {
		points = st.Value
	}

	// Live events are public, so nothing is sent for hidden teams or while frozen.
	if !snap.Frozen && !team.Hidden {
		a.publish("solve", map[string]any{
			"teamId": idStr(team.ID), "teamName": team.Name, "challengeSlug": c.Slug,
			"points": points, "firstBlood": firstBlood, "at": now,
		})
		a.publishScoreboard(snap)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "correct", "points": points, "firstBlood": firstBlood})
	return nil
}

func (a *API) unlockHint(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	user := currentUser(r)
	ev, c, err := a.visibleChallenge(ctx, r, chi.URLParam(r, "slug"))
	if err != nil {
		return err
	}
	team, err := a.playingTeam(ctx, user)
	if err != nil {
		return err
	}
	hintID, err := strconv.ParseInt(chi.URLParam(r, "hintID"), 10, 64)
	if err != nil {
		return errNotFound
	}
	h := new(store.Hint)
	if err := a.store.DB.NewSelect().Model(h).Where("id = ? AND challenge_id = ?", hintID, c.ID).Scan(ctx); err != nil {
		return errNotFound
	}
	respond := func() error {
		writeJSON(w, http.StatusOK, map[string]any{"id": idStr(h.ID), "content": markdown.Render(h.ContentMD)})
		return nil
	}
	if h.Cost == 0 {
		return respond()
	}
	exists, err := a.store.DB.NewSelect().Model((*store.HintUnlock)(nil)).Where("team_id = ? AND hint_id = ?", team.ID, h.ID).Exists(ctx)
	if err != nil {
		return err
	}
	if exists {
		return respond()
	}
	now := a.now().UTC()
	if !ev.AcceptsSubmissions(now) {
		return &apiError{Status: 403, Code: "event_not_running", Message: "Hints can only be unlocked while the event is running."}
	}
	if _, err := a.store.DB.NewInsert().Model(&store.HintUnlock{
		TeamID: team.ID, HintID: h.ID, UserID: &user.ID, Cost: h.Cost, UnlockedAt: now,
	}).On("CONFLICT DO NOTHING").Exec(ctx); err != nil {
		return err
	}
	a.boards.Invalidate()
	if snap, err := a.boards.Get(ctx); err == nil && !snap.Frozen && !team.Hidden {
		a.publishScoreboard(snap)
	}
	return respond()
}

func (a *API) publish(kind string, data any) {
	if a.hub != nil {
		a.hub.Publish(kind, data)
	}
}

func (a *API) publishScoreboard(snap *scoring.Snapshot) {
	if a.hub != nil {
		a.hub.PublishScoreboard(map[string]any{"bracketId": nil, "updatedAt": snap.UpdatedAt})
	}
}

func (a *API) downloadFile(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	sum := chi.URLParam(r, "sha256")
	name, err := url.PathUnescape(chi.URLParam(r, "name"))
	if err != nil || len(sum) != 64 || strings.Trim(sum, "0123456789abcdef") != "" {
		return errNotFound
	}
	f := new(store.File)
	if err := a.store.DB.NewSelect().Model(f).Where("sha256 = ? AND name = ?", sum, name).Limit(1).Scan(ctx); err != nil {
		return errNotFound
	}
	c := new(store.Challenge)
	if err := a.store.DB.NewSelect().Model(c).Column("slug").Where("id = ?", f.ChallengeID).Scan(ctx); err != nil {
		return errNotFound
	}
	if _, _, err := a.visibleChallenge(ctx, r, c.Slug); err != nil {
		return err
	}

	if a.files.Redirect != nil {
		u, err := a.files.Redirect(ctx, f.StorageKey, f.Name)
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store") // the link expires
		http.Redirect(w, r, u, http.StatusFound)
		return nil
	}
	fh, err := a.files.Open(ctx, f.StorageKey)
	if errors.Is(err, blobs.ErrNotFound) {
		a.log.Error("challenge file missing from storage", "file", f.Name, "key", f.StorageKey)
		return errNotFound
	} else if err != nil {
		return err
	}
	defer fh.Close()

	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(f.Name))
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, "", time.Time{}, fh)
	return nil
}
