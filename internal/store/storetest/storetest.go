// Package storetest gives tests a fresh, migrated database.
//
// By default that is in-memory SQLite. Set FLAGFALL_TEST_DATABASE_URL to a
// postgres:// URL to run the same tests on PostgreSQL; each test then gets its
// own schema, dropped when the test ends.
package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/flagfall/flagfall/internal/store"
)

// New returns a migrated store that is closed when the test ends.
func New(t testing.TB) *store.Store {
	t.Helper()
	ctx := context.Background()

	url := os.Getenv("FLAGFALL_TEST_DATABASE_URL")
	if url == "" || strings.HasPrefix(url, "sqlite") {
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

	// PostgreSQL: one schema per test.
	admin, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	schema := "t_" + hex.EncodeToString(b)
	if _, err := admin.DB.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.DB.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})

	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	s, err := store.Open(ctx, url+sep+"search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}
