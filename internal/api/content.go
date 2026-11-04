package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Kamote-Lab-Studio/flagfall/internal/markdown"
	"github.com/Kamote-Lab-Studio/flagfall/internal/store"
	"github.com/Kamote-Lab-Studio/flagfall/internal/themes"
)

// ---------------------------------------------------------------- event

func (a *API) getEvent(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	ev, err := a.store.LoadEvent(ctx)
	if err != nil {
		return err
	}
	var brackets []store.Bracket
	if err := a.store.DB.NewSelect().Model(&brackets).Order("position", "id").Scan(ctx); err != nil {
		return err
	}
	now := a.now()
	var categories []string
	if _, ok, _ := a.challengeAccess(ctx, r); ok {
		var chals []store.Challenge
		if err := a.store.DB.NewSelect().Model(&chals).Column("category", "state", "visible_from").Order("position", "id").Scan(ctx); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, c := range chals {
			if c.VisibleAt(now) && !seen[c.Category] {
				seen[c.Category] = true
				categories = append(categories, c.Category)
			}
		}
	}
	writeJSON(w, http.StatusOK, eventToView(ev, brackets, categories, now))
	return nil
}

// ---------------------------------------------------------------- scoreboard

type scoreboardEntryView struct {
	Rank        int           `json:"rank"`
	Team        scoreTeamView `json:"team"`
	Score       int           `json:"score"`
	SolveCount  int           `json:"solveCount"`
	LastSolveAt *time.Time    `json:"lastSolveAt"`
}

type scoreTeamView struct {
	ID        idStr   `json:"id"`
	Name      string  `json:"name"`
	Country   *string `json:"country"`
	BracketID idStr   `json:"bracketId"`
}

func bracketParam(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.URL.Query().Get("bracketId"), 10, 64)
	return id
}

func (a *API) getScoreboard(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	snap, err := a.boards.Get(ctx)
	if err != nil {
		return err
	}
	user := currentUser(r)
	staff := user != nil && user.IsStaff()
	bracket := bracketParam(r)

	entries := []scoreboardEntryView{}
	for _, e := range snap.Board(staff).Bracket(bracket) {
		var country *string
		if e.Team.Country != "" {
			c := e.Team.Country
			country = &c
		}
		entries = append(entries, scoreboardEntryView{
			Rank:        e.Rank,
			Team:        scoreTeamView{ID: idStr(e.Team.ID), Name: e.Team.Name, Country: country, BracketID: idStr(e.Team.BracketID)},
			Score:       e.Score,
			SolveCount:  e.SolveCount,
			LastSolveAt: timePtr(e.LastSolveAt),
		})
	}
	var bracketID *idStr
	if bracket != 0 {
		b := idStr(bracket)
		bracketID = &b
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bracketId": bracketID,
		"frozen":    snap.Frozen && !staff,
		"updatedAt": snap.UpdatedAt,
		"entries":   entries,
	})
	return nil
}

type seriesView struct {
	TeamID   idStr       `json:"teamId"`
	TeamName string      `json:"teamName"`
	Points   []pointView `json:"points"`
}

type pointView struct {
	At    time.Time `json:"at"`
	Score int       `json:"score"`
}

func (a *API) getSeries(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	snap, err := a.boards.Get(ctx)
	if err != nil {
		return err
	}
	user := currentUser(r)
	board := snap.Board(user != nil && user.IsStaff())

	top, _ := strconv.Atoi(r.URL.Query().Get("top"))
	if top < 1 {
		top = 10
	}
	if top > 25 {
		top = 25
	}
	all := board.Bracket(bracketParam(r))
	entries := all
	if len(entries) > top {
		entries = entries[:top:top]
		// ?teamId= adds one more ranked team, e.g. the viewer's own.
		if extra, err := strconv.ParseInt(r.URL.Query().Get("teamId"), 10, 64); err == nil {
			for _, e := range all[top:] {
				if e.Team.ID == extra {
					entries = append(entries, e)
					break
				}
			}
		}
	}
	ids := make([]int64, len(entries))
	for i, e := range entries {
		ids[i] = e.Team.ID
	}

	start := snap.UpdatedAt
	if snap.StartsAt != nil {
		start = *snap.StartsAt
	} else {
		for _, s := range snap.Input.Solves {
			if s.At.Before(start) {
				start = s.At
			}
		}
	}
	lines := board.Series(snap.Input, ids, start.UTC())

	out := make([]seriesView, 0, len(entries))
	for _, e := range entries {
		sv := seriesView{TeamID: idStr(e.Team.ID), TeamName: e.Team.Name, Points: []pointView{}}
		for _, p := range lines[e.Team.ID] {
			sv.Points = append(sv.Points, pointView{At: p.At.UTC(), Score: p.Score})
		}
		out = append(out, sv)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// ---------------------------------------------------------------- announcements and pages

type announcementView struct {
	ID       idStr     `json:"id"`
	Title    string    `json:"title"`
	BodyHTML string    `json:"bodyHtml"`
	Pinned   bool      `json:"pinned"`
	PostedAt time.Time `json:"postedAt"`
}

// AnnouncementView renders an announcement for players and the live stream.
func AnnouncementView(an *store.Announcement) any {
	return announcementView{
		ID: idStr(an.ID), Title: an.Title, BodyHTML: markdown.Render(an.BodyMD),
		Pinned: an.Pinned, PostedAt: an.PostedAt.UTC(),
	}
}

func (a *API) listAnnouncements(w http.ResponseWriter, r *http.Request) error {
	var list []store.Announcement
	if err := a.store.DB.NewSelect().Model(&list).Where("posted_at <= ?", a.now().UTC()).
		OrderExpr("pinned DESC, posted_at DESC").Limit(200).Scan(r.Context()); err != nil {
		return err
	}
	out := make([]any, 0, len(list))
	for i := range list {
		out = append(out, AnnouncementView(&list[i]))
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) listPages(w http.ResponseWriter, r *http.Request) error {
	var pages []store.Page
	if err := a.store.DB.NewSelect().Model(&pages).Column("slug", "title").Where("visible = ?", true).Order("position", "slug").Scan(r.Context()); err != nil {
		return err
	}
	out := make([]map[string]string, 0, len(pages))
	for _, p := range pages {
		out = append(out, map[string]string{"slug": p.Slug, "title": p.Title})
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) getPage(w http.ResponseWriter, r *http.Request) error {
	p := new(store.Page)
	if err := a.store.DB.NewSelect().Model(p).Where("slug = ? AND visible = ?", chi.URLParam(r, "slug"), true).Scan(r.Context()); err != nil {
		return errNotFound
	}
	writeJSON(w, http.StatusOK, map[string]string{"slug": p.Slug, "title": p.Title, "bodyHtml": markdown.Render(p.BodyMD)})
	return nil
}

// ---------------------------------------------------------------- theme

// DefaultTheme is used until an admin publishes another.
const DefaultTheme = themes.DefaultID

func (a *API) getTheme(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	out := map[string]any{
		"id": DefaultTheme, "version": 0, "settings": map[string]any{},
		"preview": false, "locale": "en", "messages": map[string]string{},
	}
	if a.themes != nil {
		// The same choice the page itself was served with: an admin's
		// preview, the live theme, or the default if that can't be served.
		v := a.themes.Current(r)
		if v.Theme != nil {
			out["id"] = v.Theme.Manifest.ID
		}
		out["version"], out["settings"], out["preview"] = v.Version, v.Values, v.Preview
		writeJSON(w, http.StatusOK, out)
		return nil
	}
	live := new(store.ThemeSettings)
	if err := a.store.DB.NewSelect().Model(live).Where("status = ?", "live").Limit(1).Scan(ctx); err == nil {
		settings := map[string]any{}
		_ = json.Unmarshal([]byte(live.Data), &settings)
		out["id"], out["version"], out["settings"] = live.ThemeID, live.Version, settings
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}
