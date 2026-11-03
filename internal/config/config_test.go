package config

import "testing"

func env(m map[string]string) LookupFunc {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestDefaults(t *testing.T) {
	cfg, err := FromEnv(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", cfg.Addr)
	}
	if cfg.PublicURL != "http://localhost:8080" {
		t.Errorf("PublicURL = %q", cfg.PublicURL)
	}
	if cfg.DatabaseURL != "sqlite://data/flagfall.db" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
}

func TestOverrides(t *testing.T) {
	cfg, err := FromEnv(env(map[string]string{
		"FLAGFALL_ADDR":         "127.0.0.1:9000",
		"FLAGFALL_PUBLIC_URL":   "https://ctf.example.org",
		"FLAGFALL_DATABASE_URL": "postgres://u:p@db/flagfall",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:9000" || cfg.PublicURL != "https://ctf.example.org" || cfg.DatabaseURL != "postgres://u:p@db/flagfall" {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

func TestInvalid(t *testing.T) {
	cases := map[string]map[string]string{
		"bad public url":  {"FLAGFALL_PUBLIC_URL": "ctf.example.org"},
		"ftp public url":  {"FLAGFALL_PUBLIC_URL": "ftp://ctf.example.org"},
		"unknown db type": {"FLAGFALL_DATABASE_URL": "mysql://db/flagfall"},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := FromEnv(env(m)); err == nil {
				t.Error("expected an error")
			}
		})
	}
}
