package api

import (
	"context"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/flagfall/flagfall/internal/markdown"
	"github.com/flagfall/flagfall/internal/store"
)

func (a *API) mountAdminContent(r chi.Router) {
	r.Get("/announcements", a.h(a.staff("support", a.adminListAnnouncements)))
	r.Post("/announcements", a.h(a.staff("support", a.adminCreateAnnouncement)))
	r.Patch("/announcements/{id}", a.h(a.staff("support", a.adminPatchAnnouncement)))
	r.Delete("/announcements/{id}", a.h(a.staff("support", a.adminDeleteAnnouncement)))

	r.Get("/pages", a.h(a.staff("support", a.adminListPages)))
	r.Get("/pages/{slug}", a.h(a.staff("support", a.adminGetPage)))
	r.Put("/pages/{slug}", a.h(a.staff("admin", a.adminPutPage)))
	r.Delete("/pages/{slug}", a.h(a.staff("admin", a.adminDeletePage)))
}

// ---------------------------------------------------------------- announcements

type adminAnnouncementView struct {
	ID       idStr     `json:"id"`
	Title    string    `json:"title"`
	Body     string    `json:"body"`
	Pinned   bool      `json:"pinned"`
	PostAt   time.Time `json:"postAt"`
	Posted   bool      `json:"posted"`
	AuthorID *idStr    `json:"authorId"`
	BodyHTML string    `json:"bodyHtml"`
}

func (a *API) adminAnnouncement(an *store.Announcement) adminAnnouncementView {
	v := adminAnnouncementView{
		ID: idStr(an.ID), Title: an.Title, Body: an.BodyMD, Pinned: an.Pinned, PostAt: an.PostedAt.UTC(),
		Posted: !an.PostedAt.After(a.now()), BodyHTML: markdown.Render(an.BodyMD),
	}
	if an.AuthorID != nil {
		id := idStr(*an.AuthorID)
		v.AuthorID = &id
	}
	return v
}

func (a *API) adminListAnnouncements(w http.ResponseWriter, r *http.Request) error {
	var list []store.Announcement
	if err := a.store.DB.NewSelect().Model(&list).OrderExpr("posted_at DESC").Limit(500).Scan(r.Context()); err != nil {
		return err
	}
	out := []adminAnnouncementView{}
	for i := range list {
		out = append(out, a.adminAnnouncement(&list[i]))
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) applyAnnouncementPatch(p *patch, an *store.Announcement) {
	if v, ok := p.str("title", 1, 120); ok {
		an.Title = v
	}
	if v, ok := p.str("body", 0, 20_000); ok {
		an.BodyMD = v
	}
	p.get("pinned", &an.Pinned)
	if t, ok := p.optTime("postAt"); ok {
		if t == nil {
			now := a.now().UTC()
			t = &now
		}
		an.PostedAt = *t
	}
}

func (a *API) adminCreateAnnouncement(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	p, err := decodePatch(r, "title", "body", "pinned", "postAt")
	if err != nil {
		return err
	}
	now := a.now().UTC()
	an := &store.Announcement{PostedAt: now, AuthorID: &currentUser(r).ID}
	if !p.has("title") {
		p.fail("title", "Required.")
	}
	a.applyAnnouncementPatch(p, an)
	if err := p.err(); err != nil {
		return err
	}
	if _, err := a.store.DB.NewInsert().Model(an).Exec(ctx); err != nil {
		return err
	}
	if !an.PostedAt.After(now) {
		a.publishAnnouncement(ctx, an)
	}
	writeJSON(w, http.StatusCreated, a.adminAnnouncement(an))
	return nil
}

func (a *API) adminPatchAnnouncement(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	p, err := decodePatch(r, "title", "body", "pinned", "postAt")
	if err != nil {
		return err
	}
	an := new(store.Announcement)
	if err := a.store.DB.NewSelect().Model(an).Where("id = ?", id).Scan(ctx); err != nil {
		return err
	}
	wasPosted := !an.PostedAt.After(a.now())
	a.applyAnnouncementPatch(p, an)
	if err := p.err(); err != nil {
		return err
	}
	if _, err := a.store.DB.NewUpdate().Model(an).WherePK().Exec(ctx); err != nil {
		return err
	}
	// Edits to a posted announcement aren't pushed again (no repeat toast);
	// players see them on their next load. Moving the time to now posts it.
	if !wasPosted && !an.PostedAt.After(a.now()) {
		a.publishAnnouncement(ctx, an)
	}
	writeJSON(w, http.StatusOK, a.adminAnnouncement(an))
	return nil
}

func (a *API) adminDeleteAnnouncement(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	res, err := a.store.DB.NewDelete().Model((*store.Announcement)(nil)).Where("id = ?", id).Exec(r.Context())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	return noContent(w)
}

// RunScheduler pushes scheduled announcements live when their time comes.
// It returns when ctx is done.
func (a *API) RunScheduler(ctx context.Context, every time.Duration) {
	last := a.now().UTC()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			last = a.publishDue(ctx, last)
		}
	}
}

// publishDue sends announcements whose time fell in (since, now].
func (a *API) publishDue(ctx context.Context, since time.Time) time.Time {
	now := a.now().UTC()
	var due []store.Announcement
	if err := a.store.DB.NewSelect().Model(&due).Where("posted_at > ? AND posted_at <= ?", since, now).
		Order("posted_at").Scan(ctx); err != nil {
		a.log.Error("scheduler: load announcements", "err", err)
		return since
	}
	for i := range due {
		a.publishAnnouncement(ctx, &due[i])
	}
	return now
}

// ---------------------------------------------------------------- pages

var pageSlugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type adminPageView struct {
	Slug      string    `json:"slug"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Visible   bool      `json:"visible"`
	Position  int       `json:"position"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func pageView(p *store.Page) adminPageView {
	return adminPageView{p.Slug, p.Title, p.BodyMD, p.Visible, p.Position, p.UpdatedAt.UTC()}
}

func (a *API) adminListPages(w http.ResponseWriter, r *http.Request) error {
	var list []store.Page
	if err := a.store.DB.NewSelect().Model(&list).Order("position", "slug").Scan(r.Context()); err != nil {
		return err
	}
	out := []adminPageView{}
	for i := range list {
		out = append(out, pageView(&list[i]))
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) adminGetPage(w http.ResponseWriter, r *http.Request) error {
	p := new(store.Page)
	if err := a.store.DB.NewSelect().Model(p).Where("slug = ?", chi.URLParam(r, "slug")).Scan(r.Context()); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, pageView(p))
	return nil
}

// adminPutPage creates or replaces a page.
func (a *API) adminPutPage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	slug := chi.URLParam(r, "slug")
	if !pageSlugRE.MatchString(slug) {
		return errValidation(map[string]string{"slug": "Use lowercase letters, digits and dashes."})
	}
	p, err := decodePatch(r, "title", "body", "visible", "position")
	if err != nil {
		return err
	}
	pg := &store.Page{Slug: slug, Visible: true}
	exists := a.store.DB.NewSelect().Model(pg).Where("slug = ?", slug).Scan(ctx) == nil
	if !exists && !p.has("title") {
		p.fail("title", "Required.")
	}
	if v, ok := p.str("title", 1, 80); ok {
		pg.Title = v
	}
	if v, ok := p.str("body", 0, 200_000); ok {
		pg.BodyMD = v
	}
	p.get("visible", &pg.Visible)
	if v, ok := p.intRange("position", 0, 10_000); ok {
		pg.Position = v
	}
	if err := p.err(); err != nil {
		return err
	}
	pg.UpdatedAt = a.now().UTC()
	status := http.StatusOK
	if exists {
		_, err = a.store.DB.NewUpdate().Model(pg).WherePK().Exec(ctx)
	} else {
		_, err = a.store.DB.NewInsert().Model(pg).Exec(ctx)
		status = http.StatusCreated
	}
	if err != nil {
		return err
	}
	writeJSON(w, status, pageView(pg))
	return nil
}

func (a *API) adminDeletePage(w http.ResponseWriter, r *http.Request) error {
	res, err := a.store.DB.NewDelete().Model((*store.Page)(nil)).Where("slug = ?", chi.URLParam(r, "slug")).Exec(r.Context())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	return noContent(w)
}
