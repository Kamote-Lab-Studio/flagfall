package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/flagfall/flagfall/internal/mail"
	"github.com/flagfall/flagfall/internal/store"
)

// discordHosts are the only hosts a webhook may point at, so the setting
// can't be used to make the server call internal addresses.
var discordHosts = map[string]bool{
	"discord.com": true, "discordapp.com": true, "ptb.discord.com": true, "canary.discord.com": true,
}

// validDiscordWebhook checks for https://discord.com/api/webhooks/<id>/<token>.
func validDiscordWebhook(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !discordHosts[u.Hostname()] || u.Port() != "" || u.User != nil {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	return len(parts) == 4 && parts[0] == "api" && parts[1] == "webhooks" && parts[2] != "" && parts[3] != ""
}

// discordWebhook returns the saved webhook URL, or "" if there is none or
// it can't be decrypted.
func (a *API) discordWebhook(ctx context.Context) string {
	ev, err := a.store.LoadEvent(ctx)
	if err != nil {
		return ""
	}
	sealed := ev.ParsedSettings().DiscordWebhook
	if sealed == "" {
		return ""
	}
	u, err := mail.Open(a.cfg.Secret(), sealed)
	if err != nil {
		a.log.Warn("discord webhook can't be decrypted; set it again in Settings", "err", err)
		return ""
	}
	return u
}

// discordMessage is the announcement as Discord markdown, within Discord's
// 2000-character limit. Mentions are switched off so an announcement can't
// ping @everyone.
func (a *API) discordMessage(an *store.Announcement) []byte {
	link := "\n\n<" + a.cfg.PublicURL + "/announcements>"
	text := "📣 **" + an.Title + "**\n" + an.BodyMD
	if room := 2000 - utf8.RuneCountInString(link); utf8.RuneCountInString(text) > room {
		r := []rune(text)
		text = string(r[:room-1]) + "…"
	}
	b, _ := json.Marshal(map[string]any{
		"content":          text + link,
		"allowed_mentions": map[string]any{"parse": []string{}},
	})
	return b
}

// postDiscord sends one message to the webhook.
func (a *API) postDiscord(ctx context.Context, webhook string, payload []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Flagfall (https://github.com/flagfall/flagfall)")
	res, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Discord didn't answer: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return fmt.Errorf("Discord answered %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// publishAnnouncement sends an announcement to players and, in the
// background, to Discord.
func (a *API) publishAnnouncement(ctx context.Context, an *store.Announcement) {
	a.publish("announcement", AnnouncementView(an))
	webhook := a.discordWebhook(ctx)
	if webhook == "" {
		return
	}
	payload := a.discordMessage(an)
	go func() {
		if err := a.postDiscord(context.Background(), webhook, payload); err != nil {
			a.log.Warn("announcement not posted to Discord", "title", an.Title, "err", err)
		}
	}()
}
