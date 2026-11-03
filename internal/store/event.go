package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

// Event states, as in docs/dev/api.md.
const (
	StateNotStarted = "not_started"
	StateRunning    = "running"
	StateFrozen     = "frozen"
	StateOver       = "over"
)

// State returns the event state at time now. An event with no start time is
// treated as running, so a fresh install is usable before dates are set.
func (e *Event) State(now time.Time) string {
	switch {
	case e.StartsAt != nil && now.Before(*e.StartsAt):
		return StateNotStarted
	case e.EndsAt != nil && !now.Before(*e.EndsAt):
		return StateOver
	case e.FreezeAt != nil && !now.Before(*e.FreezeAt):
		return StateFrozen
	}
	return StateRunning
}

// AcceptsSubmissions reports whether flags and hints can be used at time now.
func (e *Event) AcceptsSubmissions(now time.Time) bool {
	s := e.State(now)
	return s == StateRunning || s == StateFrozen
}

// LoadEvent returns the event settings row.
func (s *Store) LoadEvent(ctx context.Context) (*Event, error) {
	ev := new(Event)
	err := s.DB.NewSelect().Model(ev).Where("id = 1").Scan(ctx)
	return ev, err
}

// EnsureEvent creates the event row and a default bracket on first start.
func (s *Store) EnsureEvent(ctx context.Context) error {
	_, err := s.LoadEvent(ctx)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := time.Now().UTC()
	ev := &Event{
		ID: 1, Name: "Flagfall CTF", TimeZone: "UTC", MaxTeamSize: 4,
		ScoringMode: "static", RegistrationOpen: true, FlagRateLimit: 10,
		Settings: "{}", UpdatedAt: now,
	}
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(ev).Exec(ctx); err != nil {
			return err
		}
		_, err := tx.NewInsert().Model(&Bracket{Name: "Open", Description: "Everyone"}).Exec(ctx)
		return err
	})
}

// EventSettings is the JSON kept in event.settings for options that don't
// need their own column.
type EventSettings struct {
	// DemoAccount is shown on the sign-in page. Only `flagfall seed sample`
	// sets it; real events never have one.
	DemoAccount *DemoAccount `json:"demoAccount,omitempty"`
}

// DemoAccount is a public, throwaway login for demo installs.
type DemoAccount struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// ParsedSettings decodes Settings. Bad or empty JSON gives zero settings.
func (e *Event) ParsedSettings() EventSettings {
	var s EventSettings
	_ = json.Unmarshal([]byte(e.Settings), &s)
	return s
}
