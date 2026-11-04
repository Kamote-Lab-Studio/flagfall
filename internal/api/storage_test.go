package api_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"

	"github.com/Kamote-Lab-Studio/flagfall/internal/api"
	"github.com/Kamote-Lab-Studio/flagfall/internal/blobs"
	"github.com/Kamote-Lab-Studio/flagfall/internal/config"
)

// s3Env is an env whose challenge files live in an in-memory S3 bucket.
func s3Env(t *testing.T, redirect bool) (*env, *s3mem.Backend) {
	t.Helper()
	be := s3mem.New()
	if err := be.CreateBucket("ctf"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(gofakes3.New(be).Server())
	t.Cleanup(srv.Close)
	files, err := blobs.S3(config.S3Config{
		Bucket: "ctf", Endpoint: srv.URL, Region: "us-east-1", AccessKey: "k", SecretKey: "s",
		PathStyle: true, Redirect: redirect,
	}, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return makeEnv(t, true, func(o *api.Options) { o.Files = files }), be
}

func TestS3UploadDownloadAndBackup(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		name := "proxied"
		if redirect {
			name = "redirect"
		}
		t.Run(name, func(t *testing.T) {
			e, be := s3Env(t, redirect)
			owner := e.owner()
			author := e.staff("ann", "author")
			ch := e.addChallenge("bucket", 100, "flag{s3}")
			id := strings.Trim(jsonID(ch.ID), `"`)
			d := author.upload(201, "/admin/api/v1/challenges/"+id+"/files", "dump.pcap", []byte("pcap from s3"))
			sum := d.body["files"].([]any)[0].(map[string]any)["sha256"].(string)
			if _, err := be.HeadObject("ctf", sum); err != nil {
				t.Fatalf("upload isn't in the bucket: %v", err)
			}

			// Players get the bytes either way: through the server, or by
			// following the redirect to a signed bucket URL.
			p := e.signUp("pat")
			p.createTeam("pats")
			view := p.must(200, "GET", "/api/v1/challenges/bucket", nil)
			url := view.body["files"].([]any)[0].(map[string]any)["url"].(string)
			r := p.must(200, "GET", url, nil)
			if string(r.raw) != "pcap from s3" {
				t.Fatalf("download = %q", r.raw)
			}
			if fromServer := r.header.Get("Content-Security-Policy") != ""; fromServer == redirect {
				t.Fatalf("redirect=%v but the file came from the server: %v", redirect, fromServer)
			}
			e.client().must(401, "GET", url, nil)

			s := owner.must(200, "GET", "/admin/api/v1/settings", nil)
			if s.str("storage", "kind") != "s3" || !strings.HasSuffix(s.str("storage", "path"), "/ctf/") || s.body["storage"].(map[string]any)["redirect"] != redirect {
				t.Fatalf("settings storage = %s", s.raw)
			}

			// Backups read files from the bucket.
			b := owner.must(200, "GET", "/admin/api/v1/backup", nil)
			if !strings.Contains(string(b.raw), "pcap from s3") {
				t.Fatal("backup doesn't contain the file from the bucket")
			}
			// Restoring puts the file back in the bucket.
			if _, err := be.DeleteObject("ctf", sum); err != nil {
				t.Fatal(err)
			}
			owner.restore(200, "Flagfall CTF", b.raw)
			if _, err := be.HeadObject("ctf", sum); err != nil {
				t.Fatalf("restore didn't put the file back: %v", err)
			}
		})
	}
}
