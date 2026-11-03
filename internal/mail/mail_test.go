package mail_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/flagfall/flagfall/internal/mail"
	"github.com/flagfall/flagfall/internal/mail/mailtest"
)

func TestSendThroughServer(t *testing.T) {
	srv := mailtest.Start(t)
	s := srv.Settings()
	s.Username, s.Password = "ctf", "hunter2-long" // PLAIN auth is allowed to 127.0.0.1
	err := mail.Send(context.Background(), s, "Kaito <kaito@example.org>", "Réinitialiser", "Line one\nLine two é")
	if err != nil {
		t.Fatal(err)
	}
	m := srv.Last(t)
	if m.From != "ctf@example.org" || m.To != "kaito@example.org" || m.Auth != "\x00ctf\x00hunter2-long" {
		t.Fatalf("envelope = %+v", m)
	}
	for _, want := range []string{"Subject: =?utf-8?q?R=C3=A9initialiser?=", "Content-Type: text/plain; charset=utf-8", "Line one\r\nLine two =C3=A9"} {
		if !strings.Contains(m.Data, want) {
			t.Errorf("message lacks %q:\n%s", want, m.Data)
		}
	}
}

func TestStartTLSRequired(t *testing.T) {
	srv := mailtest.Start(t)
	s := srv.Settings()
	s.Security = mail.StartTLS
	err := mail.Send(context.Background(), s, "a@example.org", "x", "y")
	if err == nil || !strings.Contains(err.Error(), "doesn't offer STARTTLS") {
		t.Fatalf("err = %v", err)
	}
}

func TestHeaderInjection(t *testing.T) {
	if _, err := mail.Message("a@example.org", "b@example.org", "hi\r\nBcc: evil@example.org", "x", time.Now()); err == nil {
		t.Fatal("subject with a line break was accepted")
	}
}

func TestValidate(t *testing.T) {
	errs := mail.Server{Host: "", Port: 0, From: "nope", Security: "ssl"}.Validate()
	for _, k := range []string{"smtpHost", "smtpPort", "smtpFrom", "smtpSecurity"} {
		if errs[k] == "" {
			t.Errorf("no error for %s", k)
		}
	}
}

func TestSealOpen(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	sealed, err := mail.Seal(key, "s3cret")
	if err != nil || strings.Contains(sealed, "s3cret") {
		t.Fatalf("sealed = %q, %v", sealed, err)
	}
	if plain, err := mail.Open(key, sealed); err != nil || plain != "s3cret" {
		t.Fatalf("open = %q, %v", plain, err)
	}
	if _, err := mail.Open(bytes.Repeat([]byte{8}, 32), sealed); !errors.Is(err, mail.ErrUnreadable) {
		t.Fatalf("other key: err = %v", err)
	}
}
