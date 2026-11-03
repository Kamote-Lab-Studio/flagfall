package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"github.com/flagfall/flagfall/internal/backup"
	"github.com/flagfall/flagfall/internal/store"
	"github.com/flagfall/flagfall/internal/version"
)

// MaxBackupSize caps restore uploads.
const MaxBackupSize = 8 << 30

func (a *API) mountAdminData(r chi.Router) {
	r.Get("/export", a.h(a.staff("admin", a.adminExport)))
	r.Get("/backup", a.h(a.staff("owner", a.adminBackup)))
	r.Post("/restore", a.h(a.staff("owner", a.adminRestore)))
	r.Post("/reset", a.h(a.staff("owner", a.adminReset)))
}

// ---------------------------------------------------------------- backup

func (a *API) adminBackup(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	// Build it on disk first: the read transaction stays short even if the
	// download is slow.
	tmp, err := os.CreateTemp("", "flagfall-backup-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	now := a.now()
	m, err := backup.Write(ctx, a.store, FilesDir(a.cfg.DataDir), version.Version, now, tmp)
	if err != nil {
		return err
	}
	size, err := tmp.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	// Backups hold password hashes; downloads are worth a line in the log
	// even though they change nothing.
	a.writeAudit(ctx, r, currentUser(r).ID, "GET /backup", nil, map[string]any{"rows": m.Rows, "files": m.Files})
	if len(m.MissingFiles) > 0 {
		a.log.Warn("backup: some challenge files are missing from storage", "keys", m.MissingFiles)
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+backup.FileName(now)+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Cache-Control", "no-store")
	_, err = io.Copy(w, tmp)
	if err != nil {
		a.log.Warn("backup download cut off", "err", err)
	}
	return nil
}

// adminRestore replaces the whole install with an uploaded backup. The form
// needs the file and confirm (the current event's name).
func (a *API) adminRestore(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, MaxBackupSize)
	mr, err := r.MultipartReader()
	if err != nil {
		return errBadRequest("Upload the backup as multipart/form-data in a field named \"file\".")
	}
	tmp, err := os.CreateTemp("", "flagfall-restore-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	var size int64 = -1
	confirm := ""
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errBadRequest("The upload was cut off or is too large.")
		}
		switch part.FormName() {
		case "confirm":
			b, _ := io.ReadAll(io.LimitReader(part, 1024))
			confirm = string(b)
		case "file":
			if size, err = io.Copy(tmp, part); err != nil {
				return errBadRequest("The upload was cut off or is too large.")
			}
		}
	}
	ev, err := a.store.LoadEvent(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(confirm) != ev.Name {
		return errValidation(map[string]string{"confirm": fmt.Sprintf("Type the current event's name (%s) to confirm.", ev.Name)})
	}
	if size < 0 {
		return errValidation(map[string]string{"file": "Choose a backup file."})
	}
	zr, err := zip.NewReader(tmp, size)
	if err != nil {
		return errValidation(map[string]string{"file": "That isn't a Flagfall backup (.zip)."})
	}
	actor := currentUser(r)
	m, err := backup.Restore(ctx, a.store, FilesDir(a.cfg.DataDir), zr)
	if errors.Is(err, backup.ErrInvalid) {
		return errValidation(map[string]string{"file": strings.TrimPrefix(err.Error(), backup.ErrInvalid.Error()+": ")})
	}
	if err != nil {
		return err
	}
	a.boards.Invalidate()
	if ev, err := a.store.LoadEvent(ctx); err == nil {
		a.publish("event-state", map[string]string{"state": ev.State(a.now())})
	}
	// The log itself was just replaced. Record the restore in the restored
	// log, under the same person if the backup has their account.
	var actorID *int64
	var same store.User
	if a.store.DB.NewSelect().Model(&same).Where("email_key = ?", strings.ToLower(actor.Email)).Scan(ctx) == nil {
		actorID = &same.ID
	}
	details, _ := json.Marshal(map[string]any{"backupCreatedAt": m.CreatedAt, "rows": m.Rows, "files": m.Files, "by": actor.Email})
	ds := string(details)
	if _, err := a.store.DB.NewInsert().Model(&store.AuditEntry{
		ActorID: actorID, Action: "POST /restore", After: &ds, IP: strp(clientIP(r)), At: a.now().UTC(),
	}).Exec(context.WithoutCancel(ctx)); err != nil {
		a.log.Error("audit log write failed", "action", "POST /restore", "err", err)
	}
	a.log.Warn("restored a backup; everyone was signed out", "by", actor.Email, "backup_created_at", m.CreatedAt)
	writeJSON(w, http.StatusOK, map[string]any{"restored": true, "createdAt": m.CreatedAt, "rows": m.Rows, "files": m.Files})
	return nil
}

// ---------------------------------------------------------------- reset

// adminReset clears players, teams and their activity, keeping challenges,
// pages, announcements, brackets, settings, staff and the audit log.
func (a *API) adminReset(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var in struct {
		Confirm string `json:"confirm"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	ev, err := a.store.LoadEvent(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(in.Confirm) != ev.Name {
		return errValidation(map[string]string{"confirm": fmt.Sprintf("Type the event's name (%s) to confirm.", ev.Name)})
	}
	out := map[string]int64{}
	err = a.store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		del := func(key, table, where string) error {
			res, err := tx.NewDelete().TableExpr(table).Where(where).Exec(ctx)
			if err != nil {
				return fmt.Errorf("reset %s: %w", table, err)
			}
			if key != "" {
				out[key], _ = res.RowsAffected()
			}
			return nil
		}
		steps := []struct{ key, table, where string }{
			{"", "hint_unlocks", "1 = 1"},
			{"solves", "solves", "1 = 1"},
			{"submissions", "submissions", "1 = 1"},
			{"adjustments", "score_adjustments", "1 = 1"},
			{"", "flags", "team_id IS NOT NULL"},
		}
		for _, s := range steps {
			if err := del(s.key, s.table, s.where); err != nil {
				return err
			}
		}
		if _, err := tx.NewUpdate().Model((*store.User)(nil)).Set("team_id = NULL").Where("team_id IS NOT NULL").Exec(ctx); err != nil {
			return err
		}
		if err := del("players", "users", "role = 'player'"); err != nil {
			return err
		}
		return del("teams", "teams", "1 = 1")
	})
	if err != nil {
		return err
	}
	a.boards.Invalidate()
	a.publish("event-state", map[string]string{"state": ev.State(a.now())})
	writeJSON(w, http.StatusOK, out)
	return nil
}

// ---------------------------------------------------------------- export

var exportColumns = map[string][]string{
	"scoreboard":  {"rank", "team", "country", "bracket", "score", "solves", "firstBloods", "lastSolveAt", "hidden", "banned"},
	"teams":       {"id", "name", "country", "bracket", "members", "captain", "prizeEligible", "hidden", "banned", "createdAt"},
	"users":       {"id", "name", "email", "role", "team", "country", "verified", "banned", "createdAt"},
	"solves":      {"team", "user", "challenge", "category", "solvedAt"},
	"submissions": {"id", "team", "user", "challenge", "correct", "submitted", "ip", "submittedAt"},
}

func (a *API) adminExport(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	dataset := r.URL.Query().Get("dataset")
	format := r.URL.Query().Get("format")
	cols, ok := exportColumns[dataset]
	if !ok {
		return errValidation(map[string]string{"dataset": "Use scoreboard, teams, users, solves or submissions."})
	}
	if format != "json" && format != "csv" {
		return errValidation(map[string]string{"format": "Use json or csv."})
	}
	rows, err := a.exportRows(ctx, dataset)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("flagfall-%s-%s.%s", dataset, a.now().UTC().Format("20060102-150405"), format)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	if format == "json" {
		out := make([]map[string]any, len(rows))
		for i, row := range rows {
			obj := make(map[string]any, len(cols))
			for j, c := range cols {
				obj[c] = row[j]
			}
			out[i] = obj
		}
		writeJSON(w, http.StatusOK, out)
		return nil
	}
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	_ = cw.Write(cols)
	for _, row := range rows {
		rec := make([]string, len(row))
		for i, v := range row {
			rec[i] = csvCell(v)
		}
		_ = cw.Write(rec)
	}
	cw.Flush()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return nil
}

// csvCell formats a value for CSV. Text that a spreadsheet would run as a
// formula gets a leading apostrophe (CSV injection).
func csvCell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		if t != "" && strings.ContainsRune("=+-@\t\r", rune(t[0])) {
			return "'" + t
		}
		return t
	case *string:
		if t == nil {
			return ""
		}
		return csvCell(*t)
	case time.Time:
		return t.UTC().Format(time.RFC3339)
	case *time.Time:
		if t == nil {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	case *int:
		if t == nil {
			return ""
		}
		return strconv.Itoa(*t)
	default:
		return fmt.Sprint(t)
	}
}

func (a *API) bracketNames(ctx context.Context) (map[int64]string, error) {
	var bs []store.Bracket
	if err := a.store.DB.NewSelect().Model(&bs).Scan(ctx); err != nil {
		return nil, err
	}
	m := map[int64]string{}
	for _, b := range bs {
		m[b.ID] = b.Name
	}
	return m, nil
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func (a *API) exportRows(ctx context.Context, dataset string) ([][]any, error) {
	db := a.store.DB
	rows := [][]any{}
	switch dataset {
	case "scoreboard":
		snap, err := a.boards.Get(ctx)
		if err != nil {
			return nil, err
		}
		brackets, err := a.bracketNames(ctx)
		if err != nil {
			return nil, err
		}
		for _, e := range staffEntries(snap) {
			rows = append(rows, []any{e.Rank, e.TeamName, e.Country, brackets[int64(e.BracketID)], e.Score, e.SolveCount, e.FirstBloods, utc(e.LastSolveAt), e.Hidden, e.Banned})
		}
	case "teams":
		brackets, err := a.bracketNames(ctx)
		if err != nil {
			return nil, err
		}
		var ts []struct {
			store.Team `bun:",extend"`
			Members    int     `bun:"members"`
			Captain    *string `bun:"captain"`
		}
		if err := db.NewSelect().TableExpr("teams AS t").ColumnExpr("t.*").
			ColumnExpr("(SELECT COUNT(*) FROM users AS m WHERE m.team_id = t.id) AS members").
			ColumnExpr("(SELECT c.name FROM users AS c WHERE c.id = t.captain_id) AS captain").
			OrderExpr("t.id").Scan(ctx, &ts); err != nil {
			return nil, err
		}
		for _, t := range ts {
			rows = append(rows, []any{t.ID, t.Name, t.Country, brackets[t.BracketID], t.Members, t.Captain, t.PrizeEligible, t.Hidden, t.BannedAt != nil, t.CreatedAt.UTC()})
		}
	case "users":
		var us []struct {
			store.User `bun:",extend"`
			TeamName   *string `bun:"team_name"`
		}
		if err := db.NewSelect().TableExpr("users AS u").ColumnExpr("u.*").ColumnExpr("t.name AS team_name").
			Join("LEFT JOIN teams AS t ON t.id = u.team_id").OrderExpr("u.id").Scan(ctx, &us); err != nil {
			return nil, err
		}
		for _, u := range us {
			rows = append(rows, []any{u.ID, u.Name, u.Email, u.Role, u.TeamName, u.Country, u.VerifiedAt != nil, u.BannedAt != nil, u.CreatedAt.UTC()})
		}
	case "solves":
		var ss []struct {
			Team      string    `bun:"team"`
			User      *string   `bun:"username"`
			Challenge string    `bun:"challenge"`
			Category  string    `bun:"category"`
			SolvedAt  time.Time `bun:"solved_at"`
		}
		if err := db.NewSelect().TableExpr("solves AS s").
			ColumnExpr("t.name AS team, u.name AS username, c.name AS challenge, c.category, s.solved_at").
			Join("JOIN teams AS t ON t.id = s.team_id").Join("JOIN challenges AS c ON c.id = s.challenge_id").
			Join("LEFT JOIN users AS u ON u.id = s.user_id").OrderExpr("s.solved_at").Scan(ctx, &ss); err != nil {
			return nil, err
		}
		for _, s := range ss {
			rows = append(rows, []any{s.Team, s.User, s.Challenge, s.Category, s.SolvedAt.UTC()})
		}
	case "submissions":
		var ss []struct {
			ID          int64     `bun:"id"`
			Team        string    `bun:"team"`
			User        *string   `bun:"username"`
			Challenge   string    `bun:"challenge"`
			Correct     bool      `bun:"correct"`
			Submitted   string    `bun:"submitted"`
			IP          *string   `bun:"ip"`
			SubmittedAt time.Time `bun:"submitted_at"`
		}
		if err := db.NewSelect().TableExpr("submissions AS s").
			ColumnExpr("s.id, t.name AS team, u.name AS username, c.name AS challenge, s.correct, s.submitted, s.ip, s.submitted_at").
			Join("JOIN teams AS t ON t.id = s.team_id").Join("JOIN challenges AS c ON c.id = s.challenge_id").
			Join("LEFT JOIN users AS u ON u.id = s.user_id").OrderExpr("s.id").Scan(ctx, &ss); err != nil {
			return nil, err
		}
		for _, s := range ss {
			if s.Correct {
				s.Submitted = "(correct flag)" // same rule as the submissions screen
			}
			rows = append(rows, []any{s.ID, s.Team, s.User, s.Challenge, s.Correct, s.Submitted, s.IP, s.SubmittedAt.UTC()})
		}
	}
	return rows, nil
}
