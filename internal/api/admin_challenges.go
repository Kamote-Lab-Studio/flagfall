package api

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"github.com/flagfall/flagfall/internal/challenges"
	"github.com/flagfall/flagfall/internal/flags"
	"github.com/flagfall/flagfall/internal/markdown"
	"github.com/flagfall/flagfall/internal/scoring"
	"github.com/flagfall/flagfall/internal/store"
)

func (a *API) mountAdminChallenges(r chi.Router) {
	r.Get("/challenges", a.h(a.staff("support", a.adminListChallenges)))
	r.Post("/challenges", a.h(a.staff("author", a.adminCreateChallenge)))
	r.Post("/challenges/import", a.h(a.staff("author", a.adminImportChallenges)))
	r.Get("/challenges/{id}", a.h(a.staff("support", a.adminGetChallenge)))
	r.Patch("/challenges/{id}", a.h(a.staff("author", a.adminPatchChallenge)))
	r.Delete("/challenges/{id}", a.h(a.staff("author", a.adminDeleteChallenge)))
	r.Post("/challenges/{id}/flags", a.h(a.staff("author", a.adminAddFlag)))
	r.Delete("/challenges/{id}/flags/{flagID}", a.h(a.staff("author", a.adminDeleteFlag)))
	r.Put("/challenges/{id}/hints", a.h(a.staff("author", a.adminPutHints)))
	r.Post("/challenges/{id}/files", a.h(a.staff("author", a.adminUploadFile)))
	r.Delete("/challenges/{id}/files/{fileID}", a.h(a.staff("author", a.adminDeleteFile)))
	r.Post("/preview", a.h(a.staff("support", a.adminPreview)))
}

// ---------------------------------------------------------------- views

type adminChallengeSummary struct {
	ID          idStr      `json:"id"`
	Slug        string     `json:"slug"`
	Name        string     `json:"name"`
	Category    string     `json:"category"`
	Difficulty  *string    `json:"difficulty"`
	State       string     `json:"state"`
	VisibleFrom *time.Time `json:"visibleFrom"`
	Value       int        `json:"value"`        // as configured (initial value when dynamic)
	ValueNow    int        `json:"currentValue"` // after dynamic decay
	Solves      int        `json:"solves"`
	Attempts    int        `json:"attempts"`
	Position    int        `json:"position"`
}

type adminFlagView struct {
	ID      idStr   `json:"id"`
	Kind    string  `json:"kind"`
	Pattern *string `json:"pattern"` // exact flags are stored hashed and never returned
}

type adminHintView struct {
	ID      idStr  `json:"id"`
	Cost    int    `json:"cost"`
	Content string `json:"content"`
	Unlocks int    `json:"unlocks"`
}

type adminFileView struct {
	ID     idStr  `json:"id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	URL    string `json:"url"`
}

type adminChallengeView struct {
	adminChallengeSummary
	Author      string          `json:"author"`
	Description string          `json:"description"`
	Connection  *string         `json:"connection"`
	MinValue    *int            `json:"minValue"`
	Decay       *int            `json:"decay"`
	Tags        []string        `json:"tags"`
	ThemeFields map[string]any  `json:"themeFields"`
	Flags       []adminFlagView `json:"flags"`
	Hints       []adminHintView `json:"hints"`
	Files       []adminFileView `json:"files"`
}

func (a *API) challengeStats(ctx context.Context) (map[int64]int, error) {
	var rows []struct {
		ChallengeID int64 `bun:"challenge_id"`
		N           int   `bun:"n"`
	}
	err := a.store.DB.NewSelect().Model((*store.Submission)(nil)).Column("challenge_id").ColumnExpr("COUNT(*) AS n").
		Group("challenge_id").Scan(ctx, &rows)
	out := map[int64]int{}
	for _, r := range rows {
		out[r.ChallengeID] = r.N
	}
	return out, err
}

func (a *API) summarizeAdmin(c *store.Challenge, board *scoring.Board, attempts map[int64]int) adminChallengeSummary {
	v := adminChallengeSummary{
		ID: idStr(c.ID), Slug: c.Slug, Name: c.Name, Category: c.Category, Difficulty: c.Difficulty,
		State: c.State, VisibleFrom: utcPtr(c.VisibleFrom), Value: c.Value, ValueNow: c.Value,
		Attempts: attempts[c.ID], Position: c.Position,
	}
	if st := board.Challenges[c.ID]; st != nil {
		v.ValueNow, v.Solves = st.Value, st.SolveCount
	}
	return v
}

func (a *API) adminListChallenges(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var cs []store.Challenge
	if err := a.store.DB.NewSelect().Model(&cs).Order("position", "id").Scan(ctx); err != nil {
		return err
	}
	snap, err := a.staffSnap(ctx)
	if err != nil {
		return err
	}
	attempts, err := a.challengeStats(ctx)
	if err != nil {
		return err
	}
	out := []adminChallengeSummary{}
	for i := range cs {
		out = append(out, a.summarizeAdmin(&cs[i], snap, attempts))
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) loadChallengeByID(ctx context.Context, r *http.Request) (*store.Challenge, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	c := new(store.Challenge)
	if err := a.store.DB.NewSelect().Model(c).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (a *API) adminChallengeDetail(ctx context.Context, c *store.Challenge) (*adminChallengeView, error) {
	snap, err := a.staffSnap(ctx)
	if err != nil {
		return nil, err
	}
	attempts, err := a.challengeStats(ctx)
	if err != nil {
		return nil, err
	}
	v := &adminChallengeView{
		adminChallengeSummary: a.summarizeAdmin(c, snap, attempts),
		Author:                c.Author, Description: c.DescriptionMD, Connection: c.Connection,
		MinValue: c.MinValue, Decay: c.Decay, Tags: []string{}, ThemeFields: map[string]any{},
		Flags: []adminFlagView{}, Hints: []adminHintView{}, Files: []adminFileView{},
	}
	_ = json.Unmarshal([]byte(c.Tags), &v.Tags)
	_ = json.Unmarshal([]byte(c.ThemeFields), &v.ThemeFields)

	var fl []store.Flag
	if err := a.store.DB.NewSelect().Model(&fl).Where("challenge_id = ?", c.ID).Order("id").Scan(ctx); err != nil {
		return nil, err
	}
	for _, f := range fl {
		v.Flags = append(v.Flags, adminFlagView{idStr(f.ID), f.Kind, f.Pattern})
	}
	var hs []struct {
		store.Hint `bun:",extend"`
		Unlocks    int `bun:"unlocks"`
	}
	if err := a.store.DB.NewSelect().TableExpr("hints AS h").ColumnExpr("h.*").
		ColumnExpr("(SELECT COUNT(*) FROM hint_unlocks AS u WHERE u.hint_id = h.id) AS unlocks").
		Where("h.challenge_id = ?", c.ID).OrderExpr("h.position, h.id").Scan(ctx, &hs); err != nil {
		return nil, err
	}
	for _, h := range hs {
		v.Hints = append(v.Hints, adminHintView{idStr(h.ID), h.Cost, h.ContentMD, h.Unlocks})
	}
	var files []store.File
	if err := a.store.DB.NewSelect().Model(&files).Where("challenge_id = ?", c.ID).Order("name").Scan(ctx); err != nil {
		return nil, err
	}
	for _, f := range files {
		v.Files = append(v.Files, adminFileView{idStr(f.ID), f.Name, f.Size, f.SHA256, "/files/" + f.SHA256 + "/" + url.PathEscape(f.Name)})
	}
	return v, nil
}

func (a *API) adminGetChallenge(w http.ResponseWriter, r *http.Request) error {
	c, err := a.loadChallengeByID(r.Context(), r)
	if err != nil {
		return err
	}
	v, err := a.adminChallengeDetail(r.Context(), c)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, v)
	return nil
}

// ---------------------------------------------------------------- create and edit

var challengeFields = []string{"slug", "name", "category", "difficulty", "author", "description", "connection",
	"value", "minValue", "decay", "state", "visibleFrom", "tags", "themeFields", "position"}

func (a *API) applyChallengePatch(ctx context.Context, p *patch, c *store.Challenge) {
	if v, ok := p.str("slug", 1, 64); ok {
		if !challenges.ValidSlug(v) {
			p.fail("slug", "Use lowercase letters, digits and dashes, starting with a letter or digit.")
		} else if v != c.Slug {
			if n, _ := a.store.DB.NewSelect().Model((*store.Challenge)(nil)).Where("slug = ?", v).Count(ctx); n > 0 {
				p.fail("slug", "Another challenge uses this slug.")
			} else {
				c.Slug = v
			}
		}
	}
	if v, ok := p.str("name", 1, 80); ok {
		c.Name = v
	}
	if v, ok := p.str("category", 1, 40); ok {
		c.Category = v
	}
	if p.isNull("difficulty") {
		c.Difficulty = nil
	} else if v, ok := p.str("difficulty", 1, 10); ok {
		v = strings.ToLower(v)
		switch v {
		case "easy", "medium", "hard", "insane":
			c.Difficulty = &v
		default:
			p.fail("difficulty", "Use easy, medium, hard or insane.")
		}
	}
	if v, ok := p.str("author", 0, 80); ok {
		c.Author = v
	}
	if v, ok := p.str("description", 0, 100_000); ok {
		c.DescriptionMD = v
	}
	if p.isNull("connection") {
		c.Connection = nil
	} else if v, ok := p.str("connection", 0, 300); ok {
		c.Connection = strp(v)
	}
	if v, ok := p.intRange("value", 0, 100_000); ok {
		c.Value = v
	}
	if p.isNull("minValue") {
		c.MinValue = nil
	} else if v, ok := p.intRange("minValue", 0, 100_000); ok {
		c.MinValue = &v
	}
	if p.isNull("decay") {
		c.Decay = nil
	} else if v, ok := p.intRange("decay", 1, 100_000); ok {
		c.Decay = &v
	}
	if v, ok := p.str("state", 1, 10); ok {
		switch v {
		case "visible", "hidden", "retired":
			c.State = v
		default:
			p.fail("state", "Use visible, hidden or retired.")
		}
	}
	if t, ok := p.optTime("visibleFrom"); ok {
		c.VisibleFrom = t
	}
	var tags []string
	if p.get("tags", &tags) {
		if len(tags) > 20 {
			p.fail("tags", "Use at most 20 tags.")
		} else {
			b, _ := json.Marshal(tags)
			c.Tags = string(b)
		}
	}
	var tf map[string]any
	if p.get("themeFields", &tf) {
		if tf == nil {
			tf = map[string]any{}
		}
		b, _ := json.Marshal(tf)
		c.ThemeFields = string(b)
	}
	if v, ok := p.intRange("position", 0, 100_000); ok {
		c.Position = v
	}
	if c.MinValue != nil && *c.MinValue > c.Value {
		p.fail("minValue", "The minimum can't be above the value.")
	}
	if (c.MinValue == nil) != (c.Decay == nil) {
		p.fail("decay", "Dynamic scoring needs both a minimum and a decay.")
	}
}

// challengeChanged clears caches and tells players.
func (a *API) challengeChanged(ctx context.Context, slug string) {
	a.boards.Invalidate()
	a.publish("challenge-updated", map[string]string{"slug": slug})
	if snap, err := a.boards.Get(ctx); err == nil && !snap.Frozen {
		a.publishScoreboard(snap)
	}
}

func (a *API) respondChallenge(w http.ResponseWriter, ctx context.Context, c *store.Challenge, status int) error {
	v, err := a.adminChallengeDetail(ctx, c)
	if err != nil {
		return err
	}
	writeJSON(w, status, v)
	return nil
}

func (a *API) adminCreateChallenge(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	p, err := decodePatch(r, challengeFields...)
	if err != nil {
		return err
	}
	now := a.now().UTC()
	c := &store.Challenge{State: "hidden", Requires: "[]", ThemeFields: "{}", Tags: "[]", CreatedAt: now, UpdatedAt: now}
	for _, k := range []string{"slug", "name", "category", "value"} {
		if !p.has(k) {
			p.fail(k, "Required.")
		}
	}
	a.applyChallengePatch(ctx, p, c)
	if err := p.err(); err != nil {
		return err
	}
	if !p.has("position") {
		var maxPos int
		_ = a.store.DB.NewSelect().Model((*store.Challenge)(nil)).ColumnExpr("COALESCE(MAX(position), -1) + 1").Scan(ctx, &maxPos)
		c.Position = maxPos
	}
	if _, err := a.store.DB.NewInsert().Model(c).Exec(ctx); err != nil {
		return errValidation(map[string]string{"slug": "Another challenge uses this slug."})
	}
	a.challengeChanged(ctx, c.Slug)
	return a.respondChallenge(w, ctx, c, http.StatusCreated)
}

func (a *API) adminPatchChallenge(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	c, err := a.loadChallengeByID(ctx, r)
	if err != nil {
		return err
	}
	p, err := decodePatch(r, challengeFields...)
	if err != nil {
		return err
	}
	oldSlug := c.Slug
	a.applyChallengePatch(ctx, p, c)
	if err := p.err(); err != nil {
		return err
	}
	c.UpdatedAt = a.now().UTC()
	if _, err := a.store.DB.NewUpdate().Model(c).WherePK().ExcludeColumn("id", "created_at").Exec(ctx); err != nil {
		return err
	}
	a.challengeChanged(ctx, c.Slug)
	if oldSlug != c.Slug {
		a.challengeChanged(ctx, oldSlug)
	}
	return a.respondChallenge(w, ctx, c, http.StatusOK)
}

func (a *API) adminDeleteChallenge(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	c, err := a.loadChallengeByID(ctx, r)
	if err != nil {
		return err
	}
	if n, err := a.store.DB.NewSelect().Model((*store.Solve)(nil)).Where("challenge_id = ?", c.ID).Count(ctx); err != nil {
		return err
	} else if n > 0 {
		return errConflict("Teams have solved this challenge. Retire it instead, so their points stay.")
	}
	if _, err := a.store.DB.NewDelete().Model(c).WherePK().Exec(ctx); err != nil {
		return err
	}
	a.challengeChanged(ctx, c.Slug)
	return noContent(w)
}

// ---------------------------------------------------------------- flags and hints

func (a *API) adminAddFlag(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	c, err := a.loadChallengeByID(ctx, r)
	if err != nil {
		return err
	}
	var in struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	f := &store.Flag{ChallengeID: c.ID, Kind: in.Kind}
	switch in.Kind {
	case flags.Exact, flags.CaseInsensitive:
		v := flags.Normalize(in.Value)
		if v == "" || len(v) > flags.MaxLength {
			return errValidation(map[string]string{"value": "Enter the flag (up to 512 characters)."})
		}
		h := flags.Hash(a.cfg.Secret(), in.Kind, v)
		f.ValueHash = &h
	case flags.Pattern:
		if _, err := flags.CompilePattern(in.Value); err != nil || in.Value == "" {
			return errValidation(map[string]string{"value": "That isn't a valid pattern (RE2 syntax)."})
		}
		f.Pattern = strp(in.Value)
	default:
		return errValidation(map[string]string{"kind": "Use exact, case-insensitive or pattern."})
	}
	if _, err := a.store.DB.NewInsert().Model(f).Exec(ctx); err != nil {
		return err
	}
	return a.respondChallenge(w, ctx, c, http.StatusCreated)
}

func (a *API) adminDeleteFlag(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	c, err := a.loadChallengeByID(ctx, r)
	if err != nil {
		return err
	}
	fid, err := pathID(r, "flagID")
	if err != nil {
		return err
	}
	res, err := a.store.DB.NewDelete().Model((*store.Flag)(nil)).Where("id = ? AND challenge_id = ?", fid, c.ID).Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	return a.respondChallenge(w, ctx, c, http.StatusOK)
}

func (a *API) adminPutHints(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	c, err := a.loadChallengeByID(ctx, r)
	if err != nil {
		return err
	}
	var in []struct {
		ID      *idStr `json:"id"`
		Cost    int    `json:"cost"`
		Content string `json:"content"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if len(in) > 20 {
		return errBadRequest("Use at most 20 hints.")
	}
	fields := map[string]string{}
	for i, h := range in {
		if h.Cost < 0 || h.Cost > 100_000 {
			fields[jsonIndex(i, "cost")] = "Use 0 to 100000."
		}
		if strings.TrimSpace(h.Content) == "" || len(h.Content) > 20_000 {
			fields[jsonIndex(i, "content")] = "Write the hint (up to 20000 characters)."
		}
	}
	if len(fields) > 0 {
		return errValidation(fields)
	}
	err = a.store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var current []store.Hint
		if err := tx.NewSelect().Model(&current).Where("challenge_id = ?", c.ID).Scan(ctx); err != nil {
			return err
		}
		keep := map[int64]bool{}
		for i, h := range in {
			if h.ID != nil {
				id := int64(*h.ID)
				owned := false
				for _, cur := range current {
					owned = owned || cur.ID == id
				}
				if !owned {
					return errValidation(map[string]string{jsonIndex(i, "id"): "Not a hint of this challenge."})
				}
				keep[id] = true
				if _, err := tx.NewUpdate().Model((*store.Hint)(nil)).Set("cost = ?, content_md = ?, position = ?", h.Cost, h.Content, i).
					Where("id = ?", id).Exec(ctx); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.NewInsert().Model(&store.Hint{ChallengeID: c.ID, Cost: h.Cost, ContentMD: h.Content, Position: i}).Exec(ctx); err != nil {
				return err
			}
		}
		for _, cur := range current {
			if !keep[cur.ID] {
				if _, err := tx.NewDelete().Model((*store.Hint)(nil)).Where("id = ?", cur.ID).Exec(ctx); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	a.challengeChanged(ctx, c.Slug) // hint costs change scores
	return a.respondChallenge(w, ctx, c, http.StatusOK)
}

func jsonIndex(i int, field string) string {
	return "[" + strconv.Itoa(i) + "]." + field
}

// ---------------------------------------------------------------- files

func (a *API) adminUploadFile(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	c, err := a.loadChallengeByID(ctx, r)
	if err != nil {
		return err
	}
	r.Body = http.MaxBytesReader(w, r.Body, challenges.MaxFileSize+(1<<20))
	mr, err := r.MultipartReader()
	if err != nil {
		return errBadRequest("Upload the file as multipart/form-data in a field named \"file\".")
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return errBadRequest("No \"file\" field in the upload.")
		}
		if err != nil {
			return errBadRequest("The upload was cut off or is too large.")
		}
		if part.FormName() != "file" {
			continue
		}
		name := path.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
		if name == "" || name == "." || name == "/" || len(name) > 200 || strings.ContainsAny(name, "\x00") {
			return errValidation(map[string]string{"file": "The file needs a name (up to 200 characters)."})
		}
		sum, size, err := challenges.StoreBlob(FilesDir(a.cfg.DataDir), part, challenges.MaxFileSize)
		if errors.Is(err, challenges.ErrTooLarge) {
			return errValidation(map[string]string{"file": "Files can be at most 100 MB."})
		}
		if err != nil {
			return err
		}
		// Same name replaces the old file.
		if _, err := a.store.DB.NewDelete().Model((*store.File)(nil)).Where("challenge_id = ? AND name = ?", c.ID, name).Exec(ctx); err != nil {
			return err
		}
		if _, err := a.store.DB.NewInsert().Model(&store.File{ChallengeID: c.ID, Name: name, StorageKey: sum, Size: size, SHA256: sum}).Exec(ctx); err != nil {
			return err
		}
		a.challengeChanged(ctx, c.Slug)
		return a.respondChallenge(w, ctx, c, http.StatusCreated)
	}
}

func (a *API) adminDeleteFile(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	c, err := a.loadChallengeByID(ctx, r)
	if err != nil {
		return err
	}
	fid, err := pathID(r, "fileID")
	if err != nil {
		return err
	}
	// The blob stays on disk: other challenges may share it (content-addressed).
	res, err := a.store.DB.NewDelete().Model((*store.File)(nil)).Where("id = ? AND challenge_id = ?", fid, c.ID).Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	a.challengeChanged(ctx, c.Slug)
	return a.respondChallenge(w, ctx, c, http.StatusOK)
}

// ---------------------------------------------------------------- import

// MaxImportSize caps a challenge pack upload.
const MaxImportSize = 512 << 20

func (a *API) adminImportChallenges(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, MaxImportSize)
	mr, err := r.MultipartReader()
	if err != nil {
		return errBadRequest("Upload the .zip as multipart/form-data in a field named \"file\".")
	}
	// Spool to a temp file: zip needs random access, and packs can be large.
	tmp, err := os.CreateTemp("", "flagfall-import-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	var size int64 = -1
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errBadRequest("The upload was cut off or is too large.")
		}
		if part.FormName() == "file" {
			if size, err = io.Copy(tmp, part); err != nil {
				return errBadRequest("The upload was cut off or is too large.")
			}
			break
		}
	}
	if size < 0 {
		return errBadRequest("No \"file\" field in the upload.")
	}
	zr, err := zip.NewReader(tmp, size)
	if err != nil {
		return errValidation(map[string]string{"file": "That isn't a .zip file."})
	}
	specs, err := challenges.LoadArchive(zr)
	var problems challenges.Problems
	if errors.As(err, &problems) {
		list := make([]map[string]string, len(problems))
		for i, p := range problems {
			list[i] = map[string]string{"path": p.Path, "message": p.Message}
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{
			"code": "validation", "message": "The pack has problems; nothing was imported.", "problems": list,
		}})
		return nil
	}
	if err != nil {
		return errValidation(map[string]string{"file": err.Error()})
	}
	res, err := challenges.Apply(ctx, a.store, a.cfg.Secret(), FilesDir(a.cfg.DataDir), specs)
	if err != nil {
		return err
	}
	for _, slug := range append(append([]string{}, res.Created...), res.Updated...) {
		a.publish("challenge-updated", map[string]string{"slug": slug})
	}
	a.boards.Invalidate()
	if res.Created == nil {
		res.Created = []string{}
	}
	if res.Updated == nil {
		res.Updated = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": res.Created, "updated": res.Updated})
	return nil
}

// ---------------------------------------------------------------- preview

func (a *API) adminPreview(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Markdown string `json:"markdown"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"html": markdown.Render(in.Markdown)})
	return nil
}

// staffSnap is the unfrozen board, with hidden teams, as staff see it.
func (a *API) staffSnap(ctx context.Context) (*scoring.Board, error) {
	snap, err := a.boards.Get(ctx)
	if err != nil {
		return nil, err
	}
	return snap.Board(true), nil
}
