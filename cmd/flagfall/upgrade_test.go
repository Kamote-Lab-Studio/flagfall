package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/flagfall/flagfall/internal/blobs"
	"github.com/flagfall/flagfall/internal/store"
)

// oldInstall is a SQLite install at schema 4, with an event that starts at
// startsAt (nil: not scheduled) and one challenge flag.
func oldInstall(t *testing.T, startsAt *time.Time) (*store.Store, *blobs.Store, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(ctx, "sqlite://"+filepath.ToSlash(filepath.Join(dir, "flagfall.db")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.MigrateTo(ctx, 4); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureEvent(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.NewRaw("UPDATE event SET starts_at = ?", startsAt).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"INSERT INTO challenges (slug, name, category, description_md, value, state, created_at, updated_at) VALUES ('c', 'C', 'Web', '', 10, 'visible', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
		"INSERT INTO flags (challenge_id, kind, value_hash) VALUES (1, 'exact', 'h')",
	} {
		if _, err := db.DB.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	return db, blobs.Local(filepath.Join(dir, "files")), dir
}

func schema(t *testing.T, db *store.Store) int64 {
	t.Helper()
	cur, _, err := db.SchemaVersions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return cur
}

func backups(t *testing.T, dir string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dir, "backups", "*.zip"))
	return m
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestUpgradeRefusesDuringTheEvent(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	started := now.Add(-time.Hour)
	db, files, dir := oldInstall(t, &started)

	err := upgrade(ctx, db, files, dir, false, now, quiet)
	if !errors.Is(err, errEventRunning) {
		t.Fatalf("err = %v", err)
	}
	if schema(t, db) != 4 || len(backups(t, dir)) != 0 {
		t.Fatalf("schema %d, backups %v: a refused upgrade changed something", schema(t, db), backups(t, dir))
	}

	// With the owner's go-ahead it backs up, then migrates.
	if err := upgrade(ctx, db, files, dir, true, now, quiet); err != nil {
		t.Fatal(err)
	}
	if _, target, _ := db.SchemaVersions(ctx); schema(t, db) != target {
		t.Fatalf("schema = %d, want %d", schema(t, db), target)
	}
	b := backups(t, dir)
	if len(b) != 1 {
		t.Fatalf("backups = %v", b)
	}
	// The backup is of the old schema, flags included.
	zr, err := zip.OpenReader(b[0])
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	f, err := zr.Open("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Schema int64          `json:"schema"`
		Rows   map[string]int `json:"rows"`
	}
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if m.Schema != 4 || m.Rows["flags"] != 1 || m.Rows["challenges"] != 1 {
		t.Fatalf("manifest = %+v", m)
	}
}

func TestUpgradeOutsideTheEvent(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	for name, starts := range map[string]*time.Time{"not scheduled": nil, "later": ptr(now.Add(24 * time.Hour))} {
		t.Run(name, func(t *testing.T) {
			db, files, dir := oldInstall(t, starts)
			if err := upgrade(ctx, db, files, dir, false, now, quiet); err != nil {
				t.Fatal(err)
			}
			if len(backups(t, dir)) != 1 || schema(t, db) == 4 {
				t.Fatalf("schema %d, backups %v", schema(t, db), backups(t, dir))
			}
			// Up to date: nothing more happens.
			if err := upgrade(ctx, db, files, dir, false, now, quiet); err != nil || len(backups(t, dir)) != 1 {
				t.Fatalf("second run: %v, %v", err, backups(t, dir))
			}
		})
	}
}

func TestUpgradeOfNewInstallSkipsTheBackup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(ctx, "sqlite://"+filepath.ToSlash(filepath.Join(dir, "flagfall.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := upgrade(ctx, db, blobs.Local(filepath.Join(dir, "files")), dir, false, time.Now(), quiet); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "backups")); !os.IsNotExist(err) {
		t.Fatalf("a new install was backed up: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }
