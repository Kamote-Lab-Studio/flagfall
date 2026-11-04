package blobs_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"

	"github.com/Kamote-Lab-Studio/flagfall/internal/blobs"
	"github.com/Kamote-Lab-Studio/flagfall/internal/config"
)

// fakeS3 runs an in-memory S3 server with one bucket, "ctf".
func fakeS3(t *testing.T) (*httptest.Server, *s3mem.Backend) {
	t.Helper()
	be := s3mem.New()
	if err := be.CreateBucket("ctf"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(gofakes3.New(be).Server())
	t.Cleanup(srv.Close)
	return srv, be
}

func s3Config(srv *httptest.Server) config.S3Config {
	return config.S3Config{
		Bucket: "ctf", Endpoint: srv.URL, Region: "us-east-1", AccessKey: "test", SecretKey: "test-secret",
		Prefix: "/event-2026/", PathStyle: true,
	}
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func stores(t *testing.T) map[string]*blobs.Store {
	srv, _ := fakeS3(t)
	s3, err := blobs.S3(s3Config(srv), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]*blobs.Store{"local": blobs.Local(t.TempDir()), "s3": s3}
}

func TestStores(t *testing.T) {
	ctx := context.Background()
	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			if err := s.Check(ctx); err != nil {
				t.Fatalf("Check: %v", err)
			}
			data := []byte("challenge bytes")
			key, size, err := s.PutHashed(ctx, bytes.NewReader(data), 100)
			if err != nil || key != sum(data) || size != int64(len(data)) {
				t.Fatalf("PutHashed = %s, %d, %v", key, size, err)
			}
			// Storing again is a no-op.
			if k2, err := s.PutBytes(ctx, data); err != nil || k2 != key {
				t.Fatalf("PutBytes = %s, %v", k2, err)
			}
			if ok, err := s.Exists(ctx, key); !ok || err != nil {
				t.Fatalf("Exists = %v, %v", ok, err)
			}
			rc, err := s.Open(ctx, key)
			if err != nil {
				t.Fatal(err)
			}
			// Seekable, for range requests.
			if _, err := rc.Seek(10, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			rest, _ := io.ReadAll(rc)
			rc.Close()
			if string(rest) != "bytes" {
				t.Fatalf("after seek: %q", rest)
			}

			missing := sum([]byte("nope"))
			if _, err := s.Open(ctx, missing); !errors.Is(err, blobs.ErrNotFound) {
				t.Fatalf("Open(missing) = %v", err)
			}
			if ok, err := s.Exists(ctx, missing); ok || err != nil {
				t.Fatalf("Exists(missing) = %v, %v", ok, err)
			}
			if _, err := s.Open(ctx, "../secret.key"); !errors.Is(err, blobs.ErrNotFound) {
				t.Fatalf("Open(bad key) = %v", err)
			}
			if err := s.Put(ctx, "../x", strings.NewReader("x"), 1); err == nil {
				t.Fatal("Put accepted a bad key")
			}
			if _, _, err := s.PutHashed(ctx, strings.NewReader(strings.Repeat("x", 101)), 100); !errors.Is(err, blobs.ErrTooLarge) {
				t.Fatalf("PutHashed(too large) = %v", err)
			}
		})
	}
}

func TestS3LayoutAndRedirect(t *testing.T) {
	ctx := context.Background()
	srv, be := fakeS3(t)
	cfg := s3Config(srv)
	cfg.Redirect = true
	spool := t.TempDir()
	s, err := blobs.S3(cfg, spool, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Kind != "s3" || s.Location != "http://"+strings.TrimPrefix(srv.URL, "http://")+"/ctf/event-2026/" {
		t.Fatalf("kind %q location %q", s.Kind, s.Location)
	}
	key, err := s.PutBytes(ctx, []byte("pcap"))
	if err != nil {
		t.Fatal(err)
	}
	// Under the prefix, and nothing left behind in the spool folder.
	if _, err := be.HeadObject("ctf", "event-2026/"+key); err != nil {
		t.Fatalf("object not under the prefix: %v", err)
	}
	if _, _, err := s.PutHashed(ctx, strings.NewReader("more"), 10); err != nil {
		t.Fatal(err)
	}
	if left, _ := os.ReadDir(spool); len(left) != 0 {
		t.Fatalf("spool not cleaned up: %v", left)
	}

	u, err := s.Redirect(ctx, key, "capture file.pcap")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u, "X-Amz-Signature=") || !strings.Contains(u, "response-content-disposition=") || !strings.Contains(u, "X-Amz-Expires=300") {
		t.Fatalf("signed URL = %s", u)
	}
	res, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || string(body) != "pcap" {
		t.Fatalf("GET signed URL: %d %q", res.StatusCode, body)
	}
}

func TestS3MissingBucket(t *testing.T) {
	srv, _ := fakeS3(t)
	cfg := s3Config(srv)
	cfg.Bucket = "no-such-bucket"
	s, err := blobs.S3(cfg, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "no-such-bucket") {
		t.Fatalf("Check = %v", err)
	}
}

func TestFromConfig(t *testing.T) {
	dir := t.TempDir()
	s, err := blobs.FromConfig(config.Config{DataDir: dir})
	if err != nil || s.Kind != "local" || !strings.HasSuffix(s.Location, "files") || s.Redirect != nil {
		t.Fatalf("local = %+v, %v", s, err)
	}
	srv, _ := fakeS3(t)
	s, err = blobs.FromConfig(config.Config{DataDir: dir, S3: s3Config(srv)})
	if err != nil || s.Kind != "s3" {
		t.Fatalf("s3 = %+v, %v", s, err)
	}
}
