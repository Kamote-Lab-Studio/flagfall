package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) LookupFunc {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

// inTempDir runs the test in an empty directory, so no flagfall.toml is found.
func inTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	return dir
}

func TestDefaults(t *testing.T) {
	inTempDir(t)
	cfg, err := Load(env(nil))
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
	if cfg.Secure() {
		t.Error("http URL reported as secure")
	}
}

func TestEnvOverridesFile(t *testing.T) {
	dir := inTempDir(t)
	os.WriteFile(filepath.Join(dir, "flagfall.toml"), []byte(`
addr = "127.0.0.1:7000"
public_url = "https://from-file.example.org/"
database_url = "postgres://u:p@db/flagfall"
`), 0o600)

	cfg, err := Load(env(map[string]string{"FLAGFALL_ADDR": "127.0.0.1:9000"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:9000" {
		t.Errorf("env should win: Addr = %q", cfg.Addr)
	}
	if cfg.PublicURL != "https://from-file.example.org" {
		t.Errorf("file value and trailing slash: PublicURL = %q", cfg.PublicURL)
	}
	if cfg.Origin() != "https://from-file.example.org" || !cfg.Secure() {
		t.Errorf("Origin = %q, Secure = %v", cfg.Origin(), cfg.Secure())
	}
	if cfg.DatabaseURL != "postgres://u:p@db/flagfall" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
}

func TestExplicitMissingConfigFileFails(t *testing.T) {
	inTempDir(t)
	if _, err := Load(env(map[string]string{"FLAGFALL_CONFIG": "nope.toml"})); err == nil {
		t.Fatal("expected an error for a missing explicit config file")
	}
}

func TestInvalid(t *testing.T) {
	inTempDir(t)
	cases := map[string]map[string]string{
		"bad public url":   {"FLAGFALL_PUBLIC_URL": "ctf.example.org"},
		"ftp public url":   {"FLAGFALL_PUBLIC_URL": "ftp://ctf.example.org"},
		"unknown db type":  {"FLAGFALL_DATABASE_URL": "mysql://db/flagfall"},
		"short secret key": {"FLAGFALL_SECRET_KEY": "abcd"},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(env(m)); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestEnsureSecretGeneratesOnceAndReuses(t *testing.T) {
	dir := inTempDir(t)
	cfg, err := Load(env(map[string]string{"FLAGFALL_DATA_DIR": filepath.Join(dir, "d")}))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.EnsureSecret(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Secret()) != 32 {
		t.Fatalf("secret length = %d", len(cfg.Secret()))
	}

	again, _ := Load(env(map[string]string{"FLAGFALL_DATA_DIR": filepath.Join(dir, "d")}))
	if err := again.EnsureSecret(); err != nil {
		t.Fatal(err)
	}
	if string(again.Secret()) != string(cfg.Secret()) {
		t.Error("second start generated a different key")
	}

	b, _ := os.ReadFile(filepath.Join(dir, "d", "secret.key"))
	if strings.TrimSpace(string(b)) != cfg.SecretKey {
		t.Error("key file does not match")
	}
}

func TestS3Settings(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"FLAGFALL_S3_BUCKET": "ctf-files", "FLAGFALL_S3_ENDPOINT": "http://minio:9000", "FLAGFALL_S3_ACCESS_KEY": "a",
		"FLAGFALL_S3_SECRET_KEY": "b", "FLAGFALL_S3_PREFIX": "/2026/", "FLAGFALL_S3_PATH_STYLE": "true", "FLAGFALL_S3_REDIRECT": "1",
	}))
	if err != nil {
		t.Fatal(err)
	}
	host, secure, _ := cfg.S3.EndpointHost()
	if host != "minio:9000" || secure || cfg.S3.KeyPrefix() != "2026/" || !cfg.S3.PathStyle || !cfg.S3.Redirect {
		t.Fatalf("s3 = %+v host %q secure %v", cfg.S3, host, secure)
	}
	if host, secure, _ := (S3Config{Bucket: "b"}).EndpointHost(); host != "s3.amazonaws.com" || !secure {
		t.Errorf("default endpoint = %q %v", host, secure)
	}
	if host, secure, _ := (S3Config{Endpoint: "abc.r2.cloudflarestorage.com"}).EndpointHost(); host != "abc.r2.cloudflarestorage.com" || !secure {
		t.Errorf("bare host = %q %v", host, secure)
	}

	for name, e := range map[string]map[string]string{
		"endpoint with a path": {"FLAGFALL_S3_BUCKET": "b", "FLAGFALL_S3_ENDPOINT": "https://s3.example.org/bucket"},
		"half the keys":        {"FLAGFALL_S3_BUCKET": "b", "FLAGFALL_S3_ACCESS_KEY": "a"},
		"no bucket":            {"FLAGFALL_S3_ENDPOINT": "https://s3.example.org"},
	} {
		if _, err := Load(env(e)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
