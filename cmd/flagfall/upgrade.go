package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/flagfall/flagfall/internal/backup"
	"github.com/flagfall/flagfall/internal/blobs"
	"github.com/flagfall/flagfall/internal/store"
	"github.com/flagfall/flagfall/internal/version"
)

// errEventRunning stops an upgrade that would change the database mid-event.
var errEventRunning = errors.New("event running")

// upgrade brings the database schema up to date. When this build changes an
// existing database it first refuses if the event is running (unless
// duringEvent), then writes a backup to dataDir/backups, then migrates.
// A new, empty database is just migrated.
func upgrade(ctx context.Context, db *store.Store, files *blobs.Store, dataDir string, duringEvent bool, now time.Time, log *slog.Logger) error {
	current, target, err := db.SchemaVersions(ctx)
	if err != nil {
		return err
	}
	if current == 0 || current >= target {
		return db.Migrate(ctx)
	}

	running, err := eventRunning(ctx, db, now)
	if err != nil {
		return err
	}
	if running && !duringEvent {
		return fmt.Errorf("%w: this version of Flagfall changes the database (schema %d to %d), and the event is running. "+
			"Upgrade after it ends, or go back to the version you had. To upgrade now anyway, run `flagfall migrate --during-event`, "+
			"or start once with FLAGFALL_MIGRATE_DURING_EVENT=true; a backup is written first either way", errEventRunning, current, target)
	}

	path, err := backupBeforeUpgrade(ctx, db, files, dataDir, current, target, now)
	if err != nil {
		return fmt.Errorf("back up the database before upgrading it (nothing was changed): %w", err)
	}
	log.Info("backed up the database before upgrading it", "file", path, "from_schema", current, "to_schema", target)
	if err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("%w (a backup from before the upgrade is in %s; restore it with the version you had)", err, path)
	}
	log.Info("database upgraded", "schema", target)
	return nil
}

// eventRunning reports whether the event has started and not ended. An event
// with no start time isn't scheduled yet, so it doesn't count. It reads only
// the two columns every schema has.
func eventRunning(ctx context.Context, db *store.Store, now time.Time) (bool, error) {
	var ev struct {
		StartsAt *time.Time `bun:"starts_at"`
		EndsAt   *time.Time `bun:"ends_at"`
	}
	err := db.DB.NewRaw("SELECT starts_at, ends_at FROM event LIMIT 1").Scan(ctx, &ev)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if ev.StartsAt == nil || now.Before(*ev.StartsAt) {
		return false, nil
	}
	return ev.EndsAt == nil || now.Before(*ev.EndsAt), nil
}

func backupBeforeUpgrade(ctx context.Context, db *store.Store, files *blobs.Store, dataDir string, from, to int64, now time.Time) (string, error) {
	dir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("before-upgrade-schema-%d-to-%d-%s.zip", from, to, now.UTC().Format("20060102-150405")))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := backup.Write(ctx, db, files, version.String(), now, f); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}
