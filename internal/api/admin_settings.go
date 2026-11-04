package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Kamote-Lab-Studio/flagfall/internal/mail"
	"github.com/Kamote-Lab-Studio/flagfall/internal/store"
)

func (a *API) mountAdminSettings(r chi.Router) {
	r.Get("/settings", a.h(a.staff("owner", a.adminGetSettings)))
	r.Patch("/settings", a.h(a.staff("owner", a.adminPatchSettings)))
	r.Post("/settings/test-email", a.h(a.staff("owner", a.adminTestEmail)))
	r.Post("/settings/test-discord", a.h(a.staff("owner", a.adminTestDiscord)))
}

// settingsMailer sends with the SMTP settings saved in the database, read
// on every send so changes apply at once. Without settings it logs.
type settingsMailer struct{ a *API }

func (m settingsMailer) server(ctx context.Context) (*mail.Server, error) {
	ev, err := m.a.store.LoadEvent(ctx)
	if err != nil {
		return nil, err
	}
	ms := ev.ParsedSettings().Mail
	if ms == nil || ms.Host == "" {
		return nil, nil
	}
	s := &mail.Server{Host: ms.Host, Port: ms.Port, Username: ms.Username, From: ms.From, Security: mail.Security(ms.Security)}
	if ms.SealedPassword != "" {
		if s.Password, err = mail.Open(m.a.cfg.Secret(), ms.SealedPassword); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (m settingsMailer) Send(ctx context.Context, to, subject, body string) error {
	s, err := m.server(ctx)
	if err != nil {
		return err
	}
	if s == nil {
		return LogMailer{Logger: m.a.log}.Send(ctx, to, subject, body)
	}
	return mail.Send(ctx, *s, to, subject, body)
}

func (m settingsMailer) Configured(ctx context.Context) bool {
	s, err := m.server(ctx)
	return err == nil && s != nil
}

// canEmail reports whether account emails actually go out (rather than to
// the log), so screens can offer a link instead.
func (a *API) canEmail(ctx context.Context) bool {
	switch m := a.mailer.(type) {
	case LogMailer:
		return false
	case interface{ Configured(context.Context) bool }:
		return m.Configured(ctx)
	default:
		return true
	}
}

type settingsView struct {
	SMTPHost        string  `json:"smtpHost"`
	SMTPPort        int     `json:"smtpPort"`
	SMTPUser        string  `json:"smtpUser"`
	SMTPPasswordSet bool    `json:"smtpPasswordSet"`
	SMTPFrom        string  `json:"smtpFrom"`
	SMTPSecurity    string  `json:"smtpSecurity"`
	SMTPProblem     *string `json:"smtpProblem"`
	DiscordSet      bool    `json:"discordWebhookSet"`
	Storage         struct {
		Kind     string `json:"kind"`
		Path     string `json:"path"`
		Redirect bool   `json:"redirect"`
	} `json:"storage"`
}

func (a *API) settingsView(ev *store.Event) settingsView {
	v := settingsView{SMTPPort: 587, SMTPSecurity: string(mail.StartTLS)}
	if ms := ev.ParsedSettings().Mail; ms != nil {
		v.SMTPHost, v.SMTPPort, v.SMTPUser, v.SMTPFrom, v.SMTPSecurity = ms.Host, ms.Port, ms.Username, ms.From, ms.Security
		v.SMTPPasswordSet = ms.SealedPassword != ""
		if v.SMTPPasswordSet {
			if _, err := mail.Open(a.cfg.Secret(), ms.SealedPassword); err != nil {
				p := err.Error()
				v.SMTPProblem = &p
			}
		}
	}
	v.DiscordSet = ev.ParsedSettings().DiscordWebhook != ""
	v.Storage.Kind, v.Storage.Path, v.Storage.Redirect = a.files.Kind, a.files.Location, a.files.Redirect != nil
	return v
}

func (a *API) adminGetSettings(w http.ResponseWriter, r *http.Request) error {
	ev, err := a.store.LoadEvent(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, a.settingsView(ev))
	return nil
}

// adminPatchSettings updates the SMTP settings. An empty smtpHost turns
// email off. smtpPassword: absent keeps it, "" or null clears it.
func (a *API) adminPatchSettings(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	p, err := decodePatch(r, "smtpHost", "smtpPort", "smtpUser", "smtpPassword", "smtpFrom", "smtpSecurity", "discordWebhook")
	if err != nil {
		return err
	}
	ev, err := a.store.LoadEvent(ctx)
	if err != nil {
		return err
	}
	settings := ev.ParsedSettings()
	ms := store.MailSettings{Port: 587, Security: string(mail.StartTLS)}
	if settings.Mail != nil {
		ms = *settings.Mail
	}
	if v, ok := p.str("smtpHost", 0, 253); ok {
		ms.Host = v
	}
	if v, ok := p.intRange("smtpPort", 1, 65535); ok {
		ms.Port = v
	}
	if v, ok := p.str("smtpUser", 0, 254); ok {
		ms.Username = v
	}
	if v, ok := p.str("smtpFrom", 0, 254); ok {
		ms.From = v
	}
	if v, ok := p.str("smtpSecurity", 1, 10); ok {
		ms.Security = v
	}
	if p.isNull("smtpPassword") {
		ms.SealedPassword = ""
	} else if v, ok := p.str("smtpPassword", 0, 512); ok {
		ms.SealedPassword = ""
		if v != "" {
			if ms.SealedPassword, err = mail.Seal(a.cfg.Secret(), v); err != nil {
				return err
			}
		}
	}
	if p.isNull("discordWebhook") {
		settings.DiscordWebhook = ""
	} else if v, ok := p.str("discordWebhook", 0, 500); ok {
		settings.DiscordWebhook = ""
		if v != "" {
			if !validDiscordWebhook(v) {
				p.fail("discordWebhook", "Paste a Discord webhook URL (https://discord.com/api/webhooks/…). In Discord: channel settings → Integrations → Webhooks.")
			} else if settings.DiscordWebhook, err = mail.Seal(a.cfg.Secret(), v); err != nil {
				return err
			}
		}
	}
	if err := p.err(); err != nil {
		return err
	}
	if ms.Host == "" {
		settings.Mail = nil // email off
	} else {
		s := mail.Server{Host: ms.Host, Port: ms.Port, From: ms.From, Security: mail.Security(ms.Security)}
		if errs := s.Validate(); len(errs) > 0 {
			return errValidation(errs)
		}
		if ms.Username != "" && ms.SealedPassword == "" {
			return errValidation(map[string]string{"smtpPassword": "Enter the password for this user name."})
		}
		settings.Mail = &ms
	}
	b, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	if _, err := a.store.DB.NewUpdate().Model((*store.Event)(nil)).Set("settings = ?", string(b)).
		Set("updated_at = ?", a.now().UTC()).Where("id = 1").Exec(ctx); err != nil {
		return err
	}
	ev.Settings = string(b)
	writeJSON(w, http.StatusOK, a.settingsView(ev))
	return nil
}

// adminTestEmail sends a test message to the signed-in owner.
func (a *API) adminTestEmail(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if !a.canEmail(ctx) {
		return errValidation(map[string]string{"smtpHost": "Save the mail server settings first."})
	}
	u := currentUser(r)
	body := "This is a test email from Flagfall.\n\nIf you can read this, password reset emails will reach your players.\n\n" + a.cfg.PublicURL
	if err := a.mailer.Send(ctx, u.Email, "Flagfall test email", body); err != nil {
		var msg string
		if errors.Is(err, context.DeadlineExceeded) {
			msg = "The mail server didn't answer within 20 seconds."
		} else {
			msg = err.Error()
		}
		return &apiError{Status: http.StatusBadGateway, Code: "mail_failed", Message: "The test email wasn't sent: " + msg}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sentTo": u.Email})
	return nil
}

// adminTestDiscord posts a test message to the saved webhook.
func (a *API) adminTestDiscord(w http.ResponseWriter, r *http.Request) error {
	webhook := a.discordWebhook(r.Context())
	if webhook == "" {
		return errValidation(map[string]string{"discordWebhook": "Save a Discord webhook first."})
	}
	payload, _ := json.Marshal(map[string]any{
		"content":          "✅ Flagfall is connected. Announcements will appear in this channel.",
		"allowed_mentions": map[string]any{"parse": []string{}},
	})
	if err := a.postDiscord(r.Context(), webhook, payload); err != nil {
		return &apiError{Status: http.StatusBadGateway, Code: "discord_failed", Message: "The test message wasn't posted: " + err.Error()}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"posted": true})
	return nil
}
