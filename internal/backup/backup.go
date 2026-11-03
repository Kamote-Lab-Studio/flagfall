// Package backup writes and restores a whole Flagfall install as one .zip:
//
//	manifest.json          format, schema version, row counts
//	tables/<name>.jsonl    one JSON object per row
//	files/<sha256>         uploaded challenge files
//
// Rows are written through the typed store models, so a backup taken on
// SQLite restores into PostgreSQL and the other way round. Sessions,
// password-reset links and container status are left out: they are
// short-lived, and restoring them could sign people in as someone else.
package backup

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/flagfall/flagfall/internal/blobs"
	"github.com/flagfall/flagfall/internal/store"
)

// Format is the backup layout version. Bump it when the layout changes.
const Format = 1

// Manifest describes a backup.
type Manifest struct {
	Format    int            `json:"format"`
	App       string         `json:"app"`
	Version   string         `json:"version"`
	Schema    int64          `json:"schema"`
	Dialect   string         `json:"dialect"`
	CreatedAt time.Time      `json:"createdAt"`
	Rows      map[string]int `json:"rows"`
	Files     int            `json:"files"`
	// MissingFiles lists storage keys the database names but the disk didn't have.
	MissingFiles []string `json:"missingFiles,omitempty"`
}

type table struct {
	name  string
	hasID bool // an integer identity column named id (PostgreSQL sequences need a reset)
	dump  func(ctx context.Context, db bun.IDB, w io.Writer) (int, error)
	load  func(ctx context.Context, db bun.IDB, r io.Reader) (int, error)
}

func tbl[T any](name string, hasID bool) table {
	return table{
		name: name, hasID: hasID,
		dump: func(ctx context.Context, db bun.IDB, w io.Writer) (int, error) {
			var rows []T
			if err := db.NewSelect().Model(&rows).Scan(ctx); err != nil {
				return 0, fmt.Errorf("read %s: %w", name, err)
			}
			enc := json.NewEncoder(w)
			for i := range rows {
				if err := enc.Encode(&rows[i]); err != nil {
					return 0, err
				}
			}
			return len(rows), nil
		},
		load: func(ctx context.Context, db bun.IDB, r io.Reader) (int, error) {
			dec := json.NewDecoder(r)
			n := 0
			batch := make([]T, 0, batchSize)
			flush := func() error {
				if len(batch) == 0 {
					return nil
				}
				if _, err := db.NewInsert().Model(&batch).Exec(ctx); err != nil {
					return fmt.Errorf("restore %s: %w", name, err)
				}
				batch = batch[:0]
				return nil
			}
			for {
				var row T
				if err := dec.Decode(&row); errors.Is(err, io.EOF) {
					break
				} else if err != nil {
					return n, fmt.Errorf("%s row %d: %w", name, n+1, err)
				}
				batch = append(batch, row)
				n++
				if len(batch) == batchSize {
					if err := flush(); err != nil {
						return n, err
					}
				}
			}
			return n, flush()
		},
	}
}

// batchSize keeps each INSERT well under both engines' parameter limits.
const batchSize = 200

// tables in foreign-key order: parents first.
var tables = []table{
	tbl[store.Event]("event", false),
	tbl[store.Bracket]("brackets", true),
	tbl[store.Team]("teams", true),
	tbl[store.User]("users", true),
	tbl[store.Challenge]("challenges", true),
	tbl[store.Flag]("flags", true),
	tbl[store.File]("files", true),
	tbl[store.Hint]("hints", true),
	tbl[store.HintUnlock]("hint_unlocks", false),
	tbl[store.Submission]("submissions", true),
	tbl[store.Solve]("solves", false),
	tbl[store.ScoreAdjustment]("score_adjustments", true),
	tbl[store.Announcement]("announcements", true),
	tbl[store.Page]("pages", false),
	tbl[store.Theme]("themes", false),
	tbl[store.ThemeSettings]("theme_settings", true),
	tbl[store.AuditEntry]("audit_log", true),
}

// notBackedUp are cleared on restore but never written to a backup.
var notBackedUp = []string{"sessions", "password_resets", "challenge_services"}

// SchemaVersion is the newest applied migration.
func SchemaVersion(ctx context.Context, db bun.IDB) (int64, error) {
	var v int64
	err := db.NewRaw("SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied").Scan(ctx, &v)
	return v, err
}

// Write writes a backup of db and its challenge files to w.
//
// It reads everything inside one transaction so the backup is consistent;
// write to a local file (not straight to a slow client) to keep it short.
func Write(ctx context.Context, s *store.Store, files *blobs.Store, appVersion string, now time.Time, w io.Writer) (*Manifest, error) {
	m := &Manifest{
		Format: Format, App: "flagfall", Version: appVersion, Dialect: string(s.Dialect),
		CreatedAt: now.UTC(), Rows: map[string]int{},
	}
	zw := zip.NewWriter(w)
	var keys []string
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var err error
		if m.Schema, err = SchemaVersion(ctx, tx); err != nil {
			return fmt.Errorf("read schema version: %w", err)
		}
		for _, t := range tables {
			fw, err := zw.Create("tables/" + t.name + ".jsonl")
			if err != nil {
				return err
			}
			if m.Rows[t.name], err = t.dump(ctx, tx, fw); err != nil {
				return err
			}
		}
		return tx.NewSelect().Model((*store.File)(nil)).Distinct().Column("storage_key").Scan(ctx, &keys)
	})
	if err != nil {
		return nil, err
	}

	for _, key := range keys {
		f, err := files.Open(ctx, key)
		if errors.Is(err, blobs.ErrNotFound) {
			m.MissingFiles = append(m.MissingFiles, key)
			continue
		} else if err != nil {
			return nil, err
		}
		// Uploads are usually compressed already; store them as they are.
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: "files/" + key, Method: zip.Store, Modified: now})
		if err == nil {
			_, err = io.Copy(fw, f)
		}
		f.Close()
		if err != nil {
			return nil, err
		}
		m.Files++
	}

	mw, err := zw.Create("manifest.json")
	if err != nil {
		return nil, err
	}
	enc := json.NewEncoder(mw)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return m, zw.Close()
}

// ErrInvalid wraps problems with the backup file itself.
var ErrInvalid = errors.New("not a usable Flagfall backup")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// ReadManifest opens a backup and checks it can be restored into db.
func ReadManifest(ctx context.Context, s *store.Store, zr *zip.Reader) (*Manifest, error) {
	f, err := zr.Open("manifest.json")
	if err != nil {
		return nil, invalid("manifest.json is missing")
	}
	defer f.Close()
	m := new(Manifest)
	if err := json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(m); err != nil || m.App != "flagfall" {
		return nil, invalid("manifest.json can't be read")
	}
	if m.Format != Format {
		return nil, invalid("backup format %d; this version reads format %d", m.Format, Format)
	}
	schema, err := SchemaVersion(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	if m.Schema != schema {
		return nil, invalid("it was made with database schema %d, and this server uses %d; restore it with Flagfall %s", m.Schema, schema, m.Version)
	}
	return m, nil
}

// Restore replaces everything in db with the backup, and adds its files to
// files. It runs in one transaction: on any error nothing changes.
// All sessions are removed, so everyone signs in again.
func Restore(ctx context.Context, s *store.Store, files *blobs.Store, zr *zip.Reader) (*Manifest, error) {
	m, err := ReadManifest(ctx, s, zr)
	if err != nil {
		return nil, err
	}
	entries := map[string]*zip.File{}
	for _, f := range zr.File {
		entries[f.Name] = f
	}
	for _, t := range tables {
		if entries["tables/"+t.name+".jsonl"] == nil {
			return nil, invalid("tables/%s.jsonl is missing", t.name)
		}
	}

	// Files first: they're content-addressed, so adding them is harmless
	// even if the database part fails.
	if err := restoreFiles(ctx, zr, files); err != nil {
		return nil, err
	}

	err = s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		for _, name := range notBackedUp {
			if _, err := tx.NewDelete().TableExpr(name).Where("1 = 1").Exec(ctx); err != nil {
				return fmt.Errorf("clear %s: %w", name, err)
			}
		}
		for i := len(tables) - 1; i >= 0; i-- {
			if _, err := tx.NewDelete().TableExpr(tables[i].name).Where("1 = 1").Exec(ctx); err != nil {
				return fmt.Errorf("clear %s: %w", tables[i].name, err)
			}
		}
		for _, t := range tables {
			rc, err := entries["tables/"+t.name+".jsonl"].Open()
			if err != nil {
				return err
			}
			_, err = t.load(ctx, tx, rc)
			rc.Close()
			if err != nil {
				return invalid("%v", err)
			}
		}
		if s.Dialect == store.Postgres {
			for _, t := range tables {
				if !t.hasID {
					continue
				}
				// Inserted rows kept their ids; move the identity past them.
				q := fmt.Sprintf("SELECT setval(pg_get_serial_sequence('%[1]s', 'id'), COALESCE((SELECT MAX(id) FROM %[1]s), 0) + 1, false)", t.name)
				if _, err := tx.ExecContext(ctx, q); err != nil {
					return fmt.Errorf("reset %s ids: %w", t.name, err)
				}
			}
		}
		owners, err := tx.NewSelect().Model((*store.User)(nil)).Where("role = 'owner'").Count(ctx)
		if err != nil {
			return err
		}
		if owners == 0 {
			return invalid("it has no owner account, so nobody could manage the event afterwards")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

func restoreFiles(ctx context.Context, zr *zip.Reader, files *blobs.Store) error {
	for _, f := range zr.File {
		dir, key := path.Split(f.Name)
		if dir != "files/" {
			continue
		}
		if !blobs.ValidKey(key) {
			return invalid("unexpected file %q", f.Name)
		}
		if ok, err := files.Exists(ctx, key); err != nil {
			return err
		} else if ok {
			continue // same name, same content
		}
		if err := restoreFile(ctx, f, files, key); err != nil {
			return err
		}
	}
	return nil
}

func restoreFile(ctx context.Context, f *zip.File, files *blobs.Store, key string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	// Stored under the hash of what was actually read, so a damaged file
	// can't take the place of the real one.
	got, _, err := files.PutHashed(ctx, rc, int64(f.UncompressedSize64))
	if errors.Is(err, blobs.ErrTooLarge) || errors.Is(err, zip.ErrChecksum) || (err == nil && got != key) {
		return invalid("file %s is damaged (its checksum doesn't match)", key)
	}
	return err
}

// FileName is the suggested download name for a backup taken at t.
func FileName(t time.Time) string {
	return "flagfall-backup-" + strings.ReplaceAll(t.UTC().Format("20060102-150405"), ":", "") + ".zip"
}
