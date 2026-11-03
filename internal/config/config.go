// Package config loads the few settings Flagfall needs before the admin panel
// can start. Everything else is configured in the admin panel and stored in
// the database.
//
// Phase 0 reads environment variables only. flagfall.toml support and secret
// key generation arrive in Phase 1 (see PLAN.md).
package config

import (
	"fmt"
	"net/url"
	"path/filepath"
)

// Config is the bootstrap configuration.
type Config struct {
	// Addr is the address the HTTP server listens on.
	Addr string
	// PublicURL is the URL players use to reach the site.
	PublicURL string
	// DataDir holds the SQLite database, uploaded files and themes.
	DataDir string
	// DatabaseURL is a sqlite:// or postgres:// URL.
	DatabaseURL string
}

// LookupFunc matches os.LookupEnv, so tests can pass a fake environment.
type LookupFunc func(key string) (string, bool)

// FromEnv builds a Config from environment variables, applying defaults.
func FromEnv(lookup LookupFunc) (Config, error) {
	get := func(key, def string) string {
		if v, ok := lookup(key); ok && v != "" {
			return v
		}
		return def
	}

	cfg := Config{
		Addr:      get("FLAGFALL_ADDR", ":8080"),
		PublicURL: get("FLAGFALL_PUBLIC_URL", "http://localhost:8080"),
		DataDir:   get("FLAGFALL_DATA_DIR", "data"),
	}
	cfg.DatabaseURL = get("FLAGFALL_DATABASE_URL",
		"sqlite://"+filepath.ToSlash(filepath.Join(cfg.DataDir, "flagfall.db")))

	u, err := url.Parse(cfg.PublicURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Config{}, fmt.Errorf("FLAGFALL_PUBLIC_URL must be an http or https URL, got %q", cfg.PublicURL)
	}

	db, err := url.Parse(cfg.DatabaseURL)
	if err != nil {
		return Config{}, fmt.Errorf("FLAGFALL_DATABASE_URL is not a valid URL: %w", err)
	}
	switch db.Scheme {
	case "sqlite", "postgres", "postgresql":
	default:
		return Config{}, fmt.Errorf("FLAGFALL_DATABASE_URL must start with sqlite:// or postgres://, got %q", db.Scheme+"://")
	}

	return cfg, nil
}
