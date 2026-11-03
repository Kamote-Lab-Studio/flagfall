package backup_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/flagfall/flagfall/internal/backup"
	"github.com/flagfall/flagfall/internal/blobs"
	"github.com/flagfall/flagfall/internal/scoring"
	"github.com/flagfall/flagfall/internal/seed"
	"github.com/flagfall/flagfall/internal/store"
	"github.com/flagfall/flagfall/internal/store/storetest"
)

var ctx = context.Background()

func sqliteStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(ctx, "sqlite://:memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}

// source is a store (SQLite, or PostgreSQL when FLAGFALL_TEST_DATABASE_URL
// is set) with the sample event, demo logins, a session and one file.
func source(t *testing.T) (*store.Store, string, string) {
	t.Helper()
	s := storetest.New(t)
	files := t.TempDir()
	if _, err := seed.Sample(ctx, s, []byte(strings.Repeat("k", 32)), blobs.Local(files), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := seed.DemoLogins(ctx, s, time.Now()); err != nil {
		t.Fatal(err)
	}
	data := []byte("handout bytes")
	sum := sha256.Sum256(data)
	key := hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(files, key), data, 0o600); err != nil {
		t.Fatal(err)
	}
	var c store.Challenge
	if err := s.DB.NewSelect().Model(&c).Where("slug = 'cookie-jar'").Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.NewInsert().Model(&store.File{ChallengeID: c.ID, Name: "handout.txt", StorageKey: key, Size: int64(len(data)), SHA256: key}).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	return s, files, key
}

func write(t *testing.T, s *store.Store, files string) (*zip.Reader, *backup.Manifest) {
	t.Helper()
	var buf bytes.Buffer
	m, err := backup.Write(ctx, s, blobs.Local(files), "test", time.Now(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr, m
}

func count(t *testing.T, s *store.Store, table string) int {
	t.Helper()
	n, err := s.DB.NewSelect().TableExpr(table).Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

func top(t *testing.T, s *store.Store) string {
	t.Helper()
	snap, err := scoring.NewCache(s).Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	e := snap.Full.Entries[0]
	return fmt.Sprintf("%s %d", e.Team.Name, e.Score)
}

func TestRoundTripIntoAnotherDatabase(t *testing.T) {
	src, srcFiles, key := source(t)
	zr, m := write(t, src, srcFiles)
	if m.Rows["teams"] != 312 || m.Rows["users"] != 829 || m.Files != 1 || len(m.MissingFiles) != 0 {
		t.Fatalf("manifest = %+v", m)
	}

	// Restore into a different, non-empty install.
	dst := sqliteStore(t)
	dstFiles := t.TempDir()
	if _, err := seed.Sample(ctx, dst, []byte(strings.Repeat("x", 32)), blobs.Local(dstFiles), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Restore(ctx, dst, blobs.Local(dstFiles), zr); err != nil {
		t.Fatal(err)
	}
	for table, n := range m.Rows {
		if got := count(t, dst, table); got != n {
			t.Errorf("%s: %d rows, want %d", table, got, n)
		}
	}
	if top(t, dst) != top(t, src) {
		t.Errorf("leader = %q, want %q", top(t, dst), top(t, src))
	}
	if b, err := os.ReadFile(filepath.Join(dstFiles, key)); err != nil || string(b) != "handout bytes" {
		t.Errorf("file = %q, %v", b, err)
	}
	// New rows get fresh ids after the restored ones.
	if _, err := dst.DB.NewInsert().Model(&store.Bracket{Name: "After restore"}).Exec(ctx); err != nil {
		t.Errorf("insert after restore: %v", err)
	}
}

func TestRestoreIntoPostgresResetsIdentity(t *testing.T) {
	// Runs against PostgreSQL when FLAGFALL_TEST_DATABASE_URL is set; on
	// SQLite it still checks a restore into the same kind of store.
	src := sqliteStore(t)
	files := t.TempDir()
	if _, err := seed.Sample(ctx, src, []byte(strings.Repeat("k", 32)), blobs.Local(files), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := seed.DemoLogins(ctx, src, time.Now()); err != nil {
		t.Fatal(err)
	}
	zr, _ := write(t, src, files)
	dst := storetest.New(t)
	if _, err := backup.Restore(ctx, dst, blobs.Local(t.TempDir()), zr); err != nil {
		t.Fatal(err)
	}
	if _, err := dst.DB.NewInsert().Model(&store.Bracket{Name: "After restore"}).Exec(ctx); err != nil {
		t.Fatalf("insert after restore: %v", err)
	}
}

func TestRestoreClearsSessions(t *testing.T) {
	src, files, _ := source(t)
	var u store.User
	if err := src.DB.NewSelect().Model(&u).Where("email_key = ?", seed.DemoOwnerEmail).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := src.DB.NewInsert().Model(&store.Session{IDHash: "h", UserID: u.ID, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	zr, _ := write(t, src, files)
	if _, err := backup.Restore(ctx, src, blobs.Local(files), zr); err != nil {
		t.Fatal(err)
	}
	if n := count(t, src, "sessions"); n != 0 {
		t.Errorf("%d sessions survived the restore", n)
	}
}

// rewrite copies a backup, letting edit change or drop entries.
func rewrite(t *testing.T, zr *zip.Reader, edit func(name string, data []byte) []byte) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		if data = edit(f.Name, data); data == nil {
			continue
		}
		w, _ := zw.Create(f.Name)
		w.Write(data)
	}
	zw.Close()
	out, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBadBackupsChangeNothing(t *testing.T) {
	src, files, key := source(t)
	zr, _ := write(t, src, files)
	before := count(t, src, "teams")

	cases := map[string]func(name string, data []byte) []byte{
		"no owner": func(name string, data []byte) []byte {
			if name == "tables/users.jsonl" {
				return bytes.ReplaceAll(data, []byte(`"Role":"owner"`), []byte(`"Role":"player"`))
			}
			return data
		},
		"other schema": func(name string, data []byte) []byte {
			if name == "manifest.json" {
				return regexp.MustCompile(`"schema": \d+`).ReplaceAll(data, []byte(`"schema": 99`))
			}
			return data
		},
		"missing table": func(name string, data []byte) []byte {
			if name == "tables/solves.jsonl" {
				return nil
			}
			return data
		},
		"damaged file": func(name string, data []byte) []byte {
			if name == "files/"+key {
				return []byte("tampered")
			}
			return data
		},
		"broken row": func(name string, data []byte) []byte {
			if name == "tables/teams.jsonl" {
				return append(data, []byte("{not json\n")...)
			}
			return data
		},
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			dstFiles := t.TempDir()
			_, err := backup.Restore(ctx, src, blobs.Local(dstFiles), rewrite(t, zr, edit))
			if !errors.Is(err, backup.ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
			if n := count(t, src, "teams"); n != before {
				t.Fatalf("teams = %d after a failed restore, want %d", n, before)
			}
		})
	}
}
