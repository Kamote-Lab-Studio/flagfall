package api_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const hookURL = "https://discord.com/api/webhooks/123456/secret-token"

func TestDiscordWebhook(t *testing.T) {
	e := newEnv(t)
	owner := e.owner()
	sue := e.staff("sue", "support")

	// Only Discord webhook URLs are accepted.
	for _, bad := range []string{
		"http://discord.com/api/webhooks/1/x", "https://discord.com.evil.example/api/webhooks/1/x",
		"https://169.254.169.254/api/webhooks/1/x", "https://discord.com:8443/api/webhooks/1/x",
		"https://discord.com/api/channels/1", "https://user@discord.com/api/webhooks/1/x",
	} {
		r := owner.must(400, "PATCH", "/admin/api/v1/settings", map[string]any{"discordWebhook": bad})
		if r.str("error", "fields", "discordWebhook") == "" {
			t.Errorf("%s: %s", bad, r.raw)
		}
	}
	owner.must(400, "POST", "/admin/api/v1/settings/test-discord", nil)
	s := owner.must(200, "PATCH", "/admin/api/v1/settings", map[string]any{"discordWebhook": hookURL})
	if s.body["discordWebhookSet"] != true || strings.Contains(string(s.raw), "secret-token") {
		t.Fatalf("settings = %s", s.raw)
	}
	ev, _ := e.store.LoadEvent(context.Background())
	if strings.Contains(ev.Settings, "secret-token") {
		t.Fatalf("webhook stored in the clear: %s", ev.Settings)
	}
	if a := string(owner.must(200, "GET", "/admin/api/v1/audit", nil).raw); strings.Contains(a, "secret-token") {
		t.Fatalf("webhook in the audit log: %s", a)
	}

	owner.must(200, "POST", "/admin/api/v1/settings/test-discord", nil)
	if c := e.hooks.wait(t); c.URL != hookURL || !strings.Contains(c.Body, "connected") {
		t.Fatalf("test call = %+v", c)
	}

	// Posting now goes to Discord, with mentions off.
	sue.must(201, "POST", "/admin/api/v1/announcements", map[string]any{"title": "Hint drop", "body": "@everyone look at **web**."})
	c := e.hooks.wait(t)
	var msg struct {
		Content         string `json:"content"`
		AllowedMentions struct {
			Parse []string `json:"parse"`
		} `json:"allowed_mentions"`
	}
	if err := json.Unmarshal([]byte(c.Body), &msg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.Content, "**Hint drop**") || !strings.Contains(msg.Content, "<http://ff.test/announcements>") || msg.AllowedMentions.Parse == nil || len(msg.AllowedMentions.Parse) != 0 {
		t.Fatalf("message = %s", c.Body)
	}

	// Scheduled ones go when the scheduler sends them, not before.
	since := e.clock.Now()
	sue.must(201, "POST", "/admin/api/v1/announcements", map[string]any{"title": "Later", "body": "x", "postAt": since.Add(time.Minute)})
	select {
	case <-e.hooks.got:
		t.Fatal("scheduled announcement went to Discord early")
	case <-time.After(200 * time.Millisecond):
	}
	e.clock.Add(2 * time.Minute)
	e.api.PublishDueForTest(since)
	if c := e.hooks.wait(t); !strings.Contains(c.Body, "Later") {
		t.Fatalf("scheduled call = %+v", c)
	}

	// Long announcements are cut to Discord's 2000-character limit.
	sue.must(201, "POST", "/admin/api/v1/announcements", map[string]any{"title": "Long", "body": strings.Repeat("é", 5000)})
	if err := json.Unmarshal([]byte(e.hooks.wait(t).Body), &msg); err != nil || len([]rune(msg.Content)) > 2000 {
		t.Fatalf("content is %d characters", len([]rune(msg.Content)))
	}

	// Clearing it stops the posts.
	owner.must(200, "PATCH", "/admin/api/v1/settings", map[string]any{"discordWebhook": ""})
	sue.must(201, "POST", "/admin/api/v1/announcements", map[string]any{"title": "Quiet", "body": "x"})
	select {
	case <-e.hooks.got:
		t.Fatal("posted after the webhook was removed")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestDiscordErrorsAreReported(t *testing.T) {
	e := newEnv(t)
	owner := e.owner()
	owner.must(200, "PATCH", "/admin/api/v1/settings", map[string]any{"discordWebhook": hookURL})
	e.hooks.status = 404
	r := owner.must(502, "POST", "/admin/api/v1/settings/test-discord", nil)
	if r.str("error", "code") != "discord_failed" || !strings.Contains(r.str("error", "message"), "Unknown Webhook") {
		t.Fatalf("error = %s", r.raw)
	}
}
