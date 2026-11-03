// Package config loads the few settings Flagfall needs before the admin panel
// can start. Everything else is configured in the admin panel and stored in
// the database.
//
// Precedence: environment variables, then flagfall.toml, then defaults.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the bootstrap configuration.
type Config struct {
	// Addr is the address the HTTP server listens on.
	Addr string `toml:"addr"`
	// PublicURL is the URL players use to reach the site.
	PublicURL string `toml:"public_url"`
	// DataDir holds the SQLite database, uploaded files, themes and the secret key.
	DataDir string `toml:"data_dir"`
	// DatabaseURL is a sqlite:// or postgres:// URL.
	DatabaseURL string `toml:"database_url"`
	// SecretKey signs sessions and hashes flags. Hex, at least 32 bytes.
	SecretKey string `toml:"secret_key"`
	// TrustProxy reads the client IP from X-Forwarded-For. Only set this
	// behind a reverse proxy (Caddy, nginx, a load balancer).
	TrustProxy bool `toml:"trust_proxy"`
	// S3 stores challenge files in a bucket instead of DataDir/files.
	S3 S3Config `toml:"s3"`

	// secret is the decoded SecretKey, set by EnsureSecret.
	secret []byte
}

// LookupFunc matches os.LookupEnv, so tests can pass a fake environment.
type LookupFunc func(key string) (string, bool)

// Load reads the config file (FLAGFALL_CONFIG, or ./flagfall.toml if it
// exists), applies environment variables on top, then defaults, and validates.
// It does not touch the secret key; call EnsureSecret for that.
func Load(lookup LookupFunc) (Config, error) {
	var cfg Config

	path, explicit := lookup("FLAGFALL_CONFIG")
	if !explicit || path == "" {
		path = "flagfall.toml"
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if !errors.Is(err, fs.ErrNotExist) || explicit {
			return Config{}, fmt.Errorf("read config file %s: %w", path, err)
		}
	}

	env := func(key string, dst *string) {
		if v, ok := lookup(key); ok && v != "" {
			*dst = v
		}
	}
	env("FLAGFALL_ADDR", &cfg.Addr)
	env("FLAGFALL_PUBLIC_URL", &cfg.PublicURL)
	env("FLAGFALL_DATA_DIR", &cfg.DataDir)
	env("FLAGFALL_DATABASE_URL", &cfg.DatabaseURL)
	env("FLAGFALL_SECRET_KEY", &cfg.SecretKey)
	flag := func(key string, dst *bool) {
		if v, ok := lookup(key); ok && v != "" {
			*dst = v == "true" || v == "1"
		}
	}
	flag("FLAGFALL_TRUST_PROXY", &cfg.TrustProxy)
	env("FLAGFALL_S3_BUCKET", &cfg.S3.Bucket)
	env("FLAGFALL_S3_ENDPOINT", &cfg.S3.Endpoint)
	env("FLAGFALL_S3_REGION", &cfg.S3.Region)
	env("FLAGFALL_S3_ACCESS_KEY", &cfg.S3.AccessKey)
	env("FLAGFALL_S3_SECRET_KEY", &cfg.S3.SecretKey)
	env("FLAGFALL_S3_PREFIX", &cfg.S3.Prefix)
	flag("FLAGFALL_S3_PATH_STYLE", &cfg.S3.PathStyle)
	flag("FLAGFALL_S3_REDIRECT", &cfg.S3.Redirect)

	def := func(dst *string, v string) {
		if *dst == "" {
			*dst = v
		}
	}
	def(&cfg.Addr, ":8080")
	def(&cfg.PublicURL, "http://localhost:8080")
	def(&cfg.DataDir, "data")
	def(&cfg.DatabaseURL, "sqlite://"+filepath.ToSlash(filepath.Join(cfg.DataDir, "flagfall.db")))
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")

	return cfg, cfg.validate()
}

func (c Config) validate() error {
	u, err := url.Parse(c.PublicURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("public URL must be an http or https URL, got %q", c.PublicURL)
	}
	db, err := url.Parse(c.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database URL is not valid: %w", err)
	}
	switch db.Scheme {
	case "sqlite", "postgres", "postgresql":
	default:
		return fmt.Errorf("database URL must start with sqlite:// or postgres://, got %q", db.Scheme+"://")
	}
	if c.SecretKey != "" {
		if _, err := decodeSecret(c.SecretKey); err != nil {
			return err
		}
	}
	if c.S3.Bucket != "" {
		if _, _, err := c.S3.EndpointHost(); err != nil {
			return err
		}
		if (c.S3.AccessKey == "") != (c.S3.SecretKey == "") {
			return errors.New("S3 storage: set both the access key and the secret key, or neither (to use AWS_* variables or an instance role)")
		}
	} else if c.S3.Endpoint != "" || c.S3.AccessKey != "" {
		return errors.New("S3 storage: set the bucket (FLAGFALL_S3_BUCKET) too, or remove the other S3 settings")
	}
	return nil
}

// S3Config is an S3-compatible bucket for challenge files.
type S3Config struct {
	Bucket string `toml:"bucket"`
	// Endpoint is host[:port] or a URL; default s3.amazonaws.com. Use
	// http://host:port for a local MinIO without TLS.
	Endpoint  string `toml:"endpoint"`
	Region    string `toml:"region"`
	AccessKey string `toml:"access_key"`
	SecretKey string `toml:"secret_key"`
	// Prefix puts the files in a folder of the bucket, e.g. "flagfall/".
	Prefix string `toml:"prefix"`
	// PathStyle uses endpoint/bucket/key URLs instead of bucket.endpoint/key
	// (MinIO and some others need it).
	PathStyle bool `toml:"path_style"`
	// Redirect sends players to the bucket with a signed URL that works
	// for a few minutes, instead of passing files through the server.
	// Players' browsers must be able to reach the endpoint.
	Redirect bool `toml:"redirect"`
}

// EndpointHost returns the endpoint's host[:port] and whether it uses TLS.
func (c S3Config) EndpointHost() (host string, secure bool, err error) {
	e := c.Endpoint
	if e == "" {
		return "s3.amazonaws.com", true, nil
	}
	if !strings.Contains(e, "://") {
		e = "https://" + e
	}
	u, err := url.Parse(e)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || strings.Trim(u.Path, "/") != "" || u.User != nil {
		return "", false, fmt.Errorf("S3 endpoint must be a host name or an http(s) URL without a path, got %q", c.Endpoint)
	}
	return u.Host, u.Scheme == "https", nil
}

// KeyPrefix is Prefix with one trailing slash, or "".
func (c S3Config) KeyPrefix() string {
	p := strings.Trim(c.Prefix, "/")
	if p == "" {
		return ""
	}
	return p + "/"
}

// Secure reports whether the site is served over HTTPS.
func (c Config) Secure() bool { return strings.HasPrefix(c.PublicURL, "https://") }

// Origin is the scheme and host players' browsers send in the Origin header.
func (c Config) Origin() string {
	u, _ := url.Parse(c.PublicURL)
	return u.Scheme + "://" + u.Host
}

// Secret returns the decoded secret key. EnsureSecret must have been called.
func (c Config) Secret() []byte { return c.secret }

const secretFile = "secret.key"

// EnsureSecret makes sure a secret key is available. If none is configured,
// it reads DataDir/secret.key, creating it with a random key on first start.
func (c *Config) EnsureSecret() error {
	if c.SecretKey == "" {
		path := filepath.Join(c.DataDir, secretFile)
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			c.SecretKey = strings.TrimSpace(string(b))
		case errors.Is(err, fs.ErrNotExist):
			key := make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return err
			}
			c.SecretKey = hex.EncodeToString(key)
			if err := os.MkdirAll(c.DataDir, 0o750); err != nil {
				return fmt.Errorf("create data directory: %w", err)
			}
			if err := os.WriteFile(path, []byte(c.SecretKey+"\n"), 0o600); err != nil {
				return fmt.Errorf("save secret key: %w", err)
			}
		default:
			return fmt.Errorf("read secret key: %w", err)
		}
	}
	secret, err := decodeSecret(c.SecretKey)
	if err != nil {
		return err
	}
	c.secret = secret
	return nil
}

func decodeSecret(s string) ([]byte, error) {
	b, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) < 32 {
		return nil, errors.New("secret key must be at least 64 hex characters (32 bytes); generate one with: openssl rand -hex 32")
	}
	return b, nil
}
