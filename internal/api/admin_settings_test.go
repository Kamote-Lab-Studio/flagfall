package api_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/Kamote-Lab-Studio/flagfall/internal/mail/mailtest"
	"github.com/Kamote-Lab-Studio/flagfall/internal/store"
)

func TestSettingsAndEmail(t *testing.T) {
	e := makeEnv(t, false)
	owner := e.owner()
	admin := e.staff("ada2", "admin")
	e.signUp("pat")

	admin.must(403, "GET", "/admin/api/v1/settings", nil)
	s := owner.must(200, "GET", "/admin/api/v1/settings", nil)
	if s.str("smtpHost") != "" || s.str("storage", "kind") != "local" {
		t.Fatalf("settings = %s", s.raw)
	}
	// Without a mail server: reset links are shown, the dashboard warns,
	// and the test email explains why it can't send.
	if r := owner.must(200, "POST", "/admin/api/v1/users/"+e.userID("pat")+"/password-reset", nil); r.body["emailed"] != false || r.str("link") == "" {
		t.Fatalf("reset = %s", r.raw)
	}
	if d := owner.must(200, "GET", "/admin/api/v1/dashboard", nil); !strings.Contains(string(d.raw), "No mail server") {
		t.Fatalf("dashboard = %s", d.raw)
	}
	owner.must(400, "POST", "/admin/api/v1/settings/test-email", nil)

	// Bad settings are refused field by field.
	bad := owner.must(400, "PATCH", "/admin/api/v1/settings", map[string]any{"smtpHost": "mail.example.org", "smtpFrom": "nope", "smtpSecurity": "ssl"})
	if bad.str("error", "fields", "smtpFrom") == "" || bad.str("error", "fields", "smtpSecurity") == "" {
		t.Fatalf("bad = %s", bad.raw)
	}

	srv := mailtest.Start(t)
	cfg := srv.Settings()
	saved := owner.must(200, "PATCH", "/admin/api/v1/settings", map[string]any{
		"smtpHost": cfg.Host, "smtpPort": cfg.Port, "smtpFrom": cfg.From, "smtpSecurity": "none",
		"smtpUser": "ctf", "smtpPassword": "hunter2-long",
	})
	if saved.body["smtpPasswordSet"] != true || strings.Contains(string(saved.raw), "hunter2") {
		t.Fatalf("saved = %s", saved.raw)
	}
	// Stored sealed, never in the clear; the audit log doesn't have it either.
	ev, _ := e.store.LoadEvent(context.Background())
	if strings.Contains(ev.Settings, "hunter2") || !strings.Contains(ev.Settings, "v1:") {
		t.Fatalf("stored settings = %s", ev.Settings)
	}
	var audit []store.AuditEntry
	_ = e.store.DB.NewSelect().Model(&audit).Scan(context.Background())
	for _, a := range audit {
		if a.After != nil && strings.Contains(*a.After, "hunter2") {
			t.Fatalf("password in audit log: %s", *a.After)
		}
	}

	if r := owner.must(200, "POST", "/admin/api/v1/settings/test-email", nil); r.str("sentTo") != "root@example.org" {
		t.Fatalf("test email = %s", r.raw)
	}
	if m := srv.Last(t); m.To != "root@example.org" || m.Auth != "\x00ctf\x00hunter2-long" {
		t.Fatalf("delivered = %+v", m)
	}
	// Password resets now go by email, through the saved settings.
	if r := owner.must(200, "POST", "/admin/api/v1/users/"+e.userID("pat")+"/password-reset", nil); r.body["emailed"] != true || r.str("link") != "" {
		t.Fatalf("reset = %s", r.raw)
	}
	if m := srv.Last(t); m.To != "pat@example.org" || !strings.Contains(m.Data, "reset-password?token=3D") {
		t.Fatalf("reset email = %+v", m)
	}

	// Changing other fields keeps the password; an empty host turns email off.
	owner.must(200, "PATCH", "/admin/api/v1/settings", map[string]any{"smtpPort": cfg.Port})
	owner.must(200, "POST", "/admin/api/v1/settings/test-email", nil)
	off := owner.must(200, "PATCH", "/admin/api/v1/settings", map[string]any{"smtpHost": ""})
	if off.str("smtpHost") != "" {
		t.Fatalf("off = %s", off.raw)
	}
	owner.must(400, "POST", "/admin/api/v1/settings/test-email", nil)
}

func TestTestEmailReportsServerErrors(t *testing.T) {
	e := makeEnv(t, false)
	owner := e.owner()
	// Nothing listens on port 1.
	owner.must(200, "PATCH", "/admin/api/v1/settings", map[string]any{
		"smtpHost": "127.0.0.1", "smtpPort": 1, "smtpFrom": "ctf@example.org", "smtpSecurity": "none",
	})
	r := owner.must(502, "POST", "/admin/api/v1/settings/test-email", nil)
	if r.str("error", "code") != "mail_failed" || !strings.Contains(r.str("error", "message"), "127.0.0.1:"+strconv.Itoa(1)) {
		t.Fatalf("error = %s", r.raw)
	}
}
