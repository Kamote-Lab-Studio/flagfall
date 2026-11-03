package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"github.com/flagfall/flagfall/internal/store"
	"github.com/flagfall/flagfall/internal/themes"
)

// The Appearance section: installed themes, their settings drafts, preview,
// publish (now or scheduled), version history and rollback. See
// docs/dev/admin-api.md (Themes).

func (a *API) mountAdminThemes(r chi.Router) {
	if a.themes == nil {
		return
	}
	r.Get("/themes", a.h(a.staff("admin", a.adminListThemes)))
	r.Put("/themes/{id}/draft", a.h(a.staff("admin", a.adminPutThemeDraft)))
	r.Delete("/themes/{id}/draft", a.h(a.staff("admin", a.adminDeleteThemeDraft)))
	r.Post("/themes/{id}/preview", a.h(a.staff("admin", a.adminStartPreview)))
	r.Delete("/themes/{id}/preview", a.h(a.staff("admin", a.adminStopPreview)))
	r.Post("/themes/{id}/publish", a.h(a.staff("admin", a.adminPublishTheme)))
	r.Delete("/themes/{id}/schedule", a.h(a.staff("admin", a.adminCancelSchedule)))
	r.Get("/themes/{id}/versions", a.h(a.staff("admin", a.adminThemeVersions)))
	r.Post("/themes/{id}/versions/{version}/restore", a.h(a.staff("admin", a.adminRestoreThemeVersion)))
	r.Get("/themes/{id}/screenshots/{n}", a.h(a.staff("admin", a.adminThemeScreenshot)))
	r.Post("/themes/{id}/images/{key}", a.h(a.staff("admin", a.adminUploadThemeImage)))
}

type themeSettingsView struct {
	Version     int            `json:"version"`
	Values      map[string]any `json:"values"`
	CustomCSS   string         `json:"customCss"`
	PublishAt   *time.Time     `json:"publishAt,omitempty"`
	PublishedAt *time.Time     `json:"publishedAt,omitempty"`
}

type screenshotView struct {
	URL string `json:"url"`
	Alt string `json:"alt"`
}

type adminThemeView struct {
	ID              string                  `json:"id"`
	Name            string                  `json:"name"`
	Description     string                  `json:"description"`
	Version         string                  `json:"version"`
	Author          string                  `json:"author"`
	License         string                  `json:"license"`
	Homepage        string                  `json:"homepage"`
	Source          string                  `json:"source"`
	Built           bool                    `json:"built"`
	Live            bool                    `json:"live"`
	Previewing      bool                    `json:"previewing"`
	AdminStyle      bool                    `json:"adminStyle"`
	Settings        []themes.Setting        `json:"settings"`
	ChallengeFields []themes.ChallengeField `json:"challengeFields"`
	Screenshots     []screenshotView        `json:"screenshots"`
	Published       *themeSettingsView      `json:"published"`
	Draft           *themeSettingsView      `json:"draft"`
	Scheduled       *themeSettingsView      `json:"scheduled"`
}

func themeSettingsOf(row *store.ThemeSettings) *themeSettingsView {
	values := map[string]any{}
	_ = json.Unmarshal([]byte(row.Data), &values)
	return &themeSettingsView{Version: row.Version, Values: values, CustomCSS: row.CustomCSS,
		PublishAt: utcPtr(row.PublishAt), PublishedAt: utcPtr(row.PublishedAt)}
}

func (a *API) adminListThemes(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	live := a.themes.Live(ctx)
	previewing := a.sessionPreview(ctx, currentSessionHash(r))

	var rows []store.ThemeSettings
	if err := a.store.DB.NewSelect().Model(&rows).Where("status IN (?)", bun.In([]string{"live", "draft", "scheduled"})).
		Scan(ctx); err != nil {
		return err
	}
	byTheme := map[string]map[string]*store.ThemeSettings{}
	for i := range rows {
		row := &rows[i]
		if byTheme[row.ThemeID] == nil {
			byTheme[row.ThemeID] = map[string]*store.ThemeSettings{}
		}
		byTheme[row.ThemeID][row.Status] = row
	}

	out := []adminThemeView{}
	for _, t := range a.themes.Registry.List() {
		m := t.Manifest
		v := adminThemeView{
			ID: m.ID, Name: m.Name, Description: m.Description, Version: m.Version, Author: m.Author,
			License: m.License, Homepage: m.Homepage, Source: t.Source, Built: t.Built(),
			Live: live.Theme == t && !live.Fallback, Previewing: previewing == m.ID, AdminStyle: m.AdminStyle != "",
			Settings: m.Settings, ChallengeFields: m.ChallengeFields, Screenshots: []screenshotView{},
		}
		if v.Settings == nil {
			v.Settings = []themes.Setting{}
		}
		if v.ChallengeFields == nil {
			v.ChallengeFields = []themes.ChallengeField{}
		}
		for i, s := range m.Screenshots {
			v.Screenshots = append(v.Screenshots, screenshotView{
				URL: "/admin/api/v1/themes/" + m.ID + "/screenshots/" + strconv.Itoa(i), Alt: s.Alt})
		}
		if st := byTheme[m.ID]; st != nil {
			if row := st["live"]; row != nil {
				v.Published = themeSettingsOf(row)
			}
			if row := st["draft"]; row != nil {
				v.Draft = themeSettingsOf(row)
			}
			if row := st["scheduled"]; row != nil {
				v.Scheduled = themeSettingsOf(row)
			}
		}
		out = append(out, v)
	}
	liveID := ""
	if live.Theme != nil {
		liveID = live.Theme.Manifest.ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"themes": out, "liveThemeId": liveID, "fallback": live.Fallback,
		"browserFailures": a.themes.Failures(), "warnings": orEmptyStrings(a.themeFailureWarnings())})
	return nil
}

// installedTheme returns the theme named in the URL.
func (a *API) installedTheme(r *http.Request) (*themes.Theme, error) {
	t := a.themes.Registry.Get(chi.URLParam(r, "id"))
	if t == nil {
		return nil, errNotFound
	}
	return t, nil
}

func (a *API) adminPutThemeDraft(w http.ResponseWriter, r *http.Request) error {
	t, err := a.installedTheme(r)
	if err != nil {
		return err
	}
	var in struct {
		Settings  map[string]any `json:"settings"`
		CustomCSS string         `json:"customCss"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	imageOK, err := a.themeImageCheck(r.Context())
	if err != nil {
		return err
	}
	clean, bad := themes.CheckValues(t.Manifest.Settings, in.Settings, imageOK)
	fields := map[string]string{}
	for k, msg := range bad {
		fields["settings."+k] = msg
	}
	if msg := themes.CheckCustomCSS(in.CustomCSS); msg != "" {
		fields["customCss"] = msg
	}
	if len(fields) > 0 {
		return errValidation(fields)
	}
	data, _ := json.Marshal(clean)

	ctx := r.Context()
	var row store.ThemeSettings
	err = a.store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		err := tx.NewSelect().Model(&row).Where("theme_id = ? AND status = 'draft'", t.Manifest.ID).Limit(1).Scan(ctx)
		switch {
		case err == nil:
			row.Data, row.CustomCSS = string(data), in.CustomCSS
			_, err = tx.NewUpdate().Model(&row).Column("data", "custom_css").WherePK().Exec(ctx)
			return err
		case errors.Is(err, sql.ErrNoRows):
			next, err := nextThemeVersion(ctx, tx, t.Manifest.ID)
			if err != nil {
				return err
			}
			row = store.ThemeSettings{ThemeID: t.Manifest.ID, Version: next, Status: "draft",
				Data: string(data), CustomCSS: in.CustomCSS}
			_, err = tx.NewInsert().Model(&row).Exec(ctx)
			return err
		default:
			return err
		}
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, themeSettingsOf(&row))
	return nil
}

func (a *API) adminDeleteThemeDraft(w http.ResponseWriter, r *http.Request) error {
	t, err := a.installedTheme(r)
	if err != nil {
		return err
	}
	if _, err := a.store.DB.NewDelete().Model((*store.ThemeSettings)(nil)).
		Where("theme_id = ? AND status = 'draft'", t.Manifest.ID).Exec(r.Context()); err != nil {
		return err
	}
	return noContent(w)
}

func nextThemeVersion(ctx context.Context, db bun.IDB, themeID string) (int, error) {
	var max sql.NullInt64
	if err := db.NewSelect().Model((*store.ThemeSettings)(nil)).ColumnExpr("MAX(version)").
		Where("theme_id = ?", themeID).Scan(ctx, &max); err != nil {
		return 0, err
	}
	return int(max.Int64) + 1, nil
}

// ---------------------------------------------------------------- preview

func (a *API) adminStartPreview(w http.ResponseWriter, r *http.Request) error {
	t, err := a.installedTheme(r)
	if err != nil {
		return err
	}
	if !t.Built() {
		return errConflict("This theme isn't built, so it can't be previewed.")
	}
	if err := a.setSessionPreview(r, &t.Manifest.ID); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"previewing": t.Manifest.ID, "url": "/"})
	return nil
}

func (a *API) adminStopPreview(w http.ResponseWriter, r *http.Request) error {
	if err := a.setSessionPreview(r, nil); err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) setSessionPreview(r *http.Request, themeID *string) error {
	_, err := a.store.DB.NewUpdate().Model((*store.Session)(nil)).Set("preview_theme = ?", themeID).
		Where("id_hash = ?", currentSessionHash(r)).Exec(r.Context())
	return err
}

func (a *API) sessionPreview(ctx context.Context, idHash string) string {
	if idHash == "" {
		return ""
	}
	var id sql.NullString
	if err := a.store.DB.NewSelect().Model((*store.Session)(nil)).Column("preview_theme").
		Where("id_hash = ?", idHash).Scan(ctx, &id); err != nil {
		return ""
	}
	return id.String
}

// PreviewTheme returns the theme the admin signed in on r is previewing, or
// "". The player site calls it on every page load, outside the API's
// session middleware, so it reads the cookie itself. Only sessions of staff
// who may still use Appearance count.
func (a *API) PreviewTheme(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return ""
	}
	var row struct {
		PreviewTheme sql.NullString `bun:"preview_theme"`
		Role         string         `bun:"role"`
		BannedAt     *time.Time     `bun:"banned_at"`
		ExpiresAt    time.Time      `bun:"expires_at"`
	}
	err = a.store.DB.NewSelect().TableExpr("sessions AS s").Join("JOIN users AS u ON u.id = s.user_id").
		ColumnExpr("s.preview_theme, s.expires_at, u.role, u.banned_at").
		Where("s.id_hash = ?", hashToken(c.Value)).Where("s.preview_theme IS NOT NULL").
		Scan(r.Context(), &row)
	if err != nil || !row.PreviewTheme.Valid || row.BannedAt != nil || a.now().After(row.ExpiresAt) ||
		roleRank[row.Role] < roleRank["admin"] {
		return ""
	}
	return row.PreviewTheme.String
}

// ---------------------------------------------------------------- publish

func (a *API) adminPublishTheme(w http.ResponseWriter, r *http.Request) error {
	t, err := a.installedTheme(r)
	if err != nil {
		return err
	}
	if !t.Built() {
		return errConflict("This theme isn't built, so it can't be published.")
	}
	var in struct {
		PublishAt *time.Time `json:"publishAt"`
	}
	if r.ContentLength != 0 {
		if err := decode(r, &in); err != nil {
			return err
		}
	}
	ctx := r.Context()
	now := a.now().UTC()
	user := currentUser(r)

	if in.PublishAt != nil && in.PublishAt.After(now) {
		at := in.PublishAt.UTC()
		var row store.ThemeSettings
		err := a.store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			// One scheduled switch at a time: a new one replaces the old.
			if _, err := tx.NewUpdate().Model((*store.ThemeSettings)(nil)).Set("status = 'archived'").
				Where("status = 'scheduled'").Exec(ctx); err != nil {
				return err
			}
			r, err := draftOrCopy(ctx, tx, t.Manifest.ID)
			if err != nil {
				return err
			}
			r.Status, r.PublishAt, r.PublishedBy = "scheduled", &at, &user.ID
			_, err = tx.NewUpdate().Model(r).Column("status", "publish_at", "published_by").WherePK().Exec(ctx)
			row = *r
			return err
		})
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"scheduled": themeSettingsOf(&row)})
		return nil
	}

	var row *store.ThemeSettings
	err = a.store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		r, err := draftOrCopy(ctx, tx, t.Manifest.ID)
		if err != nil {
			return err
		}
		row = r
		return makeLive(ctx, tx, row, &user.ID, now)
	})
	if err != nil {
		return err
	}
	// The organizer is done previewing this theme: show them the live site.
	if a.sessionPreview(ctx, currentSessionHash(r)) == t.Manifest.ID {
		_ = a.setSessionPreview(r, nil)
	}
	a.themePublished(row)
	writeJSON(w, http.StatusOK, map[string]any{"published": themeSettingsOf(row)})
	return nil
}

// draftOrCopy returns the theme's draft, or a new draft copied from its
// latest saved settings (or empty, using the theme's defaults).
func draftOrCopy(ctx context.Context, tx bun.Tx, themeID string) (*store.ThemeSettings, error) {
	row := new(store.ThemeSettings)
	err := tx.NewSelect().Model(row).Where("theme_id = ? AND status = 'draft'", themeID).Limit(1).Scan(ctx)
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	base := new(store.ThemeSettings)
	err = tx.NewSelect().Model(base).Where("theme_id = ?", themeID).
		OrderExpr("CASE status WHEN 'live' THEN 0 ELSE 1 END, version DESC").Limit(1).Scan(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	next, err := nextThemeVersion(ctx, tx, themeID)
	if err != nil {
		return nil, err
	}
	row = &store.ThemeSettings{ThemeID: themeID, Version: next, Status: "draft", Data: "{}"}
	if base.ID != 0 {
		row.Data, row.CustomCSS = base.Data, base.CustomCSS
	}
	_, err = tx.NewInsert().Model(row).Exec(ctx)
	return row, err
}

// makeLive archives the live settings (of any theme) and makes row live.
func makeLive(ctx context.Context, tx bun.Tx, row *store.ThemeSettings, by *int64, now time.Time) error {
	if _, err := tx.NewUpdate().Model((*store.ThemeSettings)(nil)).Set("status = 'archived'").
		Where("status = 'live'").Exec(ctx); err != nil {
		return err
	}
	row.Status, row.PublishedAt = "live", &now
	if by != nil {
		row.PublishedBy = by
	}
	_, err := tx.NewUpdate().Model(row).Column("status", "published_at", "published_by").WherePK().Exec(ctx)
	return err
}

// themePublished tells open player tabs to reload into the new look.
func (a *API) themePublished(row *store.ThemeSettings) {
	a.log.Info("theme published", "theme", row.ThemeID, "version", row.Version)
	if a.hub != nil {
		a.hub.Publish("theme-published", map[string]any{"id": row.ThemeID, "version": row.Version})
	}
}

func (a *API) adminCancelSchedule(w http.ResponseWriter, r *http.Request) error {
	t, err := a.installedTheme(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	err = a.store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		row := new(store.ThemeSettings)
		err := tx.NewSelect().Model(row).Where("theme_id = ? AND status = 'scheduled'", t.Manifest.ID).Limit(1).Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound
		} else if err != nil {
			return err
		}
		// Back to a draft, unless the organizer has started a new one.
		n, err := tx.NewSelect().Model((*store.ThemeSettings)(nil)).
			Where("theme_id = ? AND status = 'draft'", t.Manifest.ID).Count(ctx)
		if err != nil {
			return err
		}
		status := "draft"
		if n > 0 {
			status = "archived"
		}
		if _, err := tx.NewUpdate().Model(row).Set("status = ?", status).Set("publish_at = NULL").
			Set("published_by = NULL").WherePK().Exec(ctx); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return noContent(w)
}

// publishScheduledThemes makes a due scheduled switch live.
func (a *API) publishScheduledThemes(ctx context.Context) {
	now := a.now().UTC()
	row := new(store.ThemeSettings)
	err := a.store.DB.NewSelect().Model(row).Where("status = 'scheduled' AND publish_at <= ?", now).
		OrderExpr("publish_at DESC").Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return
	} else if err != nil {
		a.log.Error("scheduler: load scheduled theme", "err", err)
		return
	}
	if a.themes != nil {
		if t := a.themes.Registry.Get(row.ThemeID); t == nil || !t.Built() {
			a.log.Error("scheduler: the scheduled theme isn't installed or built; leaving the current theme live", "theme", row.ThemeID)
			return
		}
	}
	err = a.store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return makeLive(ctx, tx, row, nil, now)
	})
	if err != nil {
		a.log.Error("scheduler: publish theme", "theme", row.ThemeID, "err", err)
		return
	}
	a.themePublished(row)
}

// ---------------------------------------------------------------- history

type themeVersionView struct {
	Version     int            `json:"version"`
	Status      string         `json:"status"`
	Values      map[string]any `json:"values"`
	CustomCSS   string         `json:"customCss"`
	PublishAt   *time.Time     `json:"publishAt"`
	PublishedAt *time.Time     `json:"publishedAt"`
	PublishedBy *string        `json:"publishedBy"`
}

func (a *API) adminThemeVersions(w http.ResponseWriter, r *http.Request) error {
	t, err := a.installedTheme(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	var rows []store.ThemeSettings
	if err := a.store.DB.NewSelect().Model(&rows).Where("theme_id = ?", t.Manifest.ID).
		Order("version DESC").Limit(200).Scan(ctx); err != nil {
		return err
	}
	ids := []int64{}
	for _, row := range rows {
		if row.PublishedBy != nil {
			ids = append(ids, *row.PublishedBy)
		}
	}
	names := map[int64]string{}
	if len(ids) > 0 {
		var users []store.User
		if err := a.store.DB.NewSelect().Model(&users).Column("id", "name").Where("id IN (?)", bun.In(ids)).Scan(ctx); err != nil {
			return err
		}
		for _, u := range users {
			names[u.ID] = u.Name
		}
	}
	out := []themeVersionView{}
	for i := range rows {
		row := &rows[i]
		v := themeSettingsOf(row)
		tv := themeVersionView{Version: row.Version, Status: row.Status, Values: v.Values, CustomCSS: row.CustomCSS,
			PublishAt: v.PublishAt, PublishedAt: v.PublishedAt}
		if row.PublishedBy != nil {
			if n, ok := names[*row.PublishedBy]; ok {
				tv.PublishedBy = &n
			}
		}
		out = append(out, tv)
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": out})
	return nil
}

// adminRestoreThemeVersion rolls back: it publishes a copy of an earlier
// version as a new version, so history only ever grows.
func (a *API) adminRestoreThemeVersion(w http.ResponseWriter, r *http.Request) error {
	t, err := a.installedTheme(r)
	if err != nil {
		return err
	}
	if !t.Built() {
		return errConflict("This theme isn't built, so it can't be published.")
	}
	version, err := strconv.Atoi(chi.URLParam(r, "version"))
	if err != nil {
		return errNotFound
	}
	ctx := r.Context()
	user := currentUser(r)
	imageOK, err := a.themeImageCheck(ctx)
	if err != nil {
		return err
	}
	var row *store.ThemeSettings
	err = a.store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		old := new(store.ThemeSettings)
		err := tx.NewSelect().Model(old).Where("theme_id = ? AND version = ?", t.Manifest.ID, version).Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound
		} else if err != nil {
			return err
		}
		// Settings saved for an older theme version may no longer fit.
		values := map[string]any{}
		_ = json.Unmarshal([]byte(old.Data), &values)
		clean, _ := themes.CheckValues(t.Manifest.Settings, values, imageOK)
		data, _ := json.Marshal(clean)
		next, err := nextThemeVersion(ctx, tx, t.Manifest.ID)
		if err != nil {
			return err
		}
		row = &store.ThemeSettings{ThemeID: t.Manifest.ID, Version: next, Status: "draft",
			Data: string(data), CustomCSS: themes.SafeCustomCSS(old.CustomCSS)}
		if _, err := tx.NewInsert().Model(row).Exec(ctx); err != nil {
			return err
		}
		return makeLive(ctx, tx, row, &user.ID, a.now().UTC())
	})
	if err != nil {
		return err
	}
	a.themePublished(row)
	writeJSON(w, http.StatusOK, map[string]any{"published": themeSettingsOf(row), "restoredFrom": version})
	return nil
}

// ---------------------------------------------------------------- screenshots

func (a *API) adminThemeScreenshot(w http.ResponseWriter, r *http.Request) error {
	t, err := a.installedTheme(r)
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(chi.URLParam(r, "n"))
	if err != nil {
		return errNotFound
	}
	f, err := t.Screenshot(n)
	if err != nil {
		return errNotFound
	}
	defer f.Close()
	ctype := mime.TypeByExtension(path.Ext(t.Manifest.Screenshots[n].Path))
	switch ctype {
	case "image/png", "image/jpeg", "image/webp":
	default:
		return errNotFound
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, err = io.Copy(w, f)
	return err
}
