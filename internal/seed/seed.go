// Package seed loads the sample event: the 12 sample challenges and the
// teams, players and solves shown in the Option A designs.
//
// data/teams.json was extracted from design/option-a/teams.html and
// team/*.html. Solve times are stored relative to "now", so the sample always
// looks like an event in progress with about 14 hours left.
package seed

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/flagfall/flagfall/challenges/sample"
	"github.com/flagfall/flagfall/internal/auth"
	"github.com/flagfall/flagfall/internal/challenges"
	"github.com/flagfall/flagfall/internal/store"
)

//go:embed data/teams.json
var teamsJSON []byte

//go:embed data/rules.md
var rulesMD string

// Demo logins, created by DemoLogins for `flagfall seed sample` only.
// DemoEmail is the captain of null_pointer, the team shown as "Your team"
// in the designs; DemoOwnerEmail is an owner for trying the admin panel.
// Both use DemoPassword.
const (
	DemoEmail      = "kaito@sample.invalid"
	DemoOwnerName  = "organizer"
	DemoOwnerEmail = "organizer@sample.invalid"
	DemoPassword   = "flagfall-demo-password"
)

// ErrNotEmpty means the database already has an event in it.
var ErrNotEmpty = errors.New("the database already has challenges or teams; the sample event is only loaded into an empty install")

type fixture struct {
	Teams []struct {
		Name       string `json:"name"`
		Country    string `json:"country"`
		Bracket    string `json:"bracket"`
		Formed     string `json:"formed"`
		Recruiting bool   `json:"recruiting"`
		Players    []struct {
			Name    string `json:"name"`
			Captain bool   `json:"captain"`
		} `json:"players"`
		Solves []struct {
			Challenge  string `json:"challenge"`
			By         string `json:"by"`
			MinutesAgo int    `json:"minutesAgo"`
		} `json:"solves"`
	} `json:"teams"`
}

// Summary reports what was loaded.
type Summary struct {
	Challenges, Teams, Players, Solves int
}

// Sample loads the sample event into an empty database.
func Sample(ctx context.Context, db *store.Store, secret []byte, filesDir string, now time.Time) (Summary, error) {
	var sum Summary
	now = now.UTC().Truncate(time.Second)

	for _, m := range []any{(*store.Challenge)(nil), (*store.Team)(nil)} {
		n, err := db.DB.NewSelect().Model(m).Count(ctx)
		if err != nil {
			return sum, err
		}
		if n > 0 {
			return sum, ErrNotEmpty
		}
	}
	if err := db.EnsureEvent(ctx); err != nil {
		return sum, err
	}

	var fx fixture
	if err := json.Unmarshal(teamsJSON, &fx); err != nil {
		return sum, fmt.Errorf("sample teams: %w", err)
	}

	specs, err := challenges.Load(sample.FS, ".")
	if err != nil {
		return sum, fmt.Errorf("sample challenges: %w", err)
	}
	if _, err := challenges.Apply(ctx, db, secret, filesDir, specs); err != nil {
		return sum, err
	}
	sum.Challenges = len(specs)

	// Sample players can't sign in: they share a hash of a random password
	// nobody knows. Hashing 800 distinct passwords would take ~30 seconds.
	lockedHash, err := auth.HashPassword(auth.RandomToken(32))
	if err != nil {
		return sum, err
	}
	// 33h38m into a 48-hour event, matching the "14:22:07 left" in the designs.
	start := now.Add(-(33*time.Hour + 38*time.Minute))
	end := start.Add(48 * time.Hour)
	freeze := end.Add(-2 * time.Hour)

	err = db.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewUpdate().Model((*store.Event)(nil)).
			Set("name = ?", "Flagfall CTF").
			Set("starts_at = ?", start).Set("ends_at = ?", end).Set("freeze_at = ?", freeze).
			Set("max_team_size = 4").Set("registration_open = ?", true).Set("updated_at = ?", now).
			Where("id = 1").Exec(ctx); err != nil {
			return err
		}

		brackets := map[string]int64{}
		var existing []store.Bracket
		if err := tx.NewSelect().Model(&existing).Scan(ctx); err != nil {
			return err
		}
		for _, b := range existing {
			brackets[b.Name] = b.ID
		}
		for i, b := range []store.Bracket{
			{Name: "Open", Description: "Anyone can play"},
			{Name: "Students", Description: "Every player is enrolled in a school or university", Position: 1},
		} {
			if id, ok := brackets[b.Name]; ok {
				if _, err := tx.NewUpdate().Model((*store.Bracket)(nil)).Set("description = ?", b.Description).Set("position = ?", i).Where("id = ?", id).Exec(ctx); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.NewInsert().Model(&b).Exec(ctx); err != nil {
				return err
			}
			brackets[b.Name] = b.ID
		}

		var chals []store.Challenge
		if err := tx.NewSelect().Model(&chals).Column("id", "slug").Scan(ctx); err != nil {
			return err
		}
		chalID := map[string]int64{}
		for _, c := range chals {
			chalID[c.Slug] = c.ID
		}
		for i, slug := range boardOrder {
			if _, err := tx.NewUpdate().Model((*store.Challenge)(nil)).Set("position = ?", i).Where("slug = ?", slug).Exec(ctx); err != nil {
				return err
			}
		}

		for _, ft := range fx.Teams {
			created, err := time.Parse("2 Jan 2006", ft.Formed)
			if err != nil {
				created = start.Add(-14 * 24 * time.Hour)
			}
			country := ft.Country
			team := &store.Team{
				Name: ft.Name, NameKey: strings.ToLower(ft.Name), Country: &country,
				BracketID: brackets[ft.Bracket], InviteCode: inviteCode(), Recruiting: ft.Recruiting,
				PrizeEligible: true, CreatedAt: created.UTC(),
			}
			if _, err := tx.NewInsert().Model(team).Exec(ctx); err != nil {
				return fmt.Errorf("team %s: %w", ft.Name, err)
			}
			sum.Teams++

			userID := map[string]int64{}
			for _, fp := range ft.Players {
				email := strings.ToLower(fp.Name) + "@sample.invalid"
				u := &store.User{
					Name: fp.Name, NameKey: strings.ToLower(fp.Name), Email: email, EmailKey: email,
					PasswordHash: lockedHash, Role: "player", TeamID: &team.ID, Country: &country,
					VerifiedAt: &created, CreatedAt: created.UTC(),
				}
				if _, err := tx.NewInsert().Model(u).Exec(ctx); err != nil {
					return fmt.Errorf("player %s: %w", fp.Name, err)
				}
				userID[fp.Name] = u.ID
				sum.Players++
				if fp.Captain {
					if _, err := tx.NewUpdate().Model((*store.Team)(nil)).Set("captain_id = ?", u.ID).Where("id = ?", team.ID).Exec(ctx); err != nil {
						return err
					}
				}
			}

			for _, fs := range ft.Solves {
				cid, ok := chalID[fs.Challenge]
				if !ok {
					return fmt.Errorf("team %s solved unknown challenge %q", ft.Name, fs.Challenge)
				}
				uid := userID[fs.By]
				at := now.Add(-time.Duration(fs.MinutesAgo) * time.Minute)
				sub := &store.Submission{
					TeamID: team.ID, UserID: &uid, ChallengeID: cid, Correct: true,
					Submitted: "(sample)", SubmittedAt: at,
				}
				if _, err := tx.NewInsert().Model(sub).Exec(ctx); err != nil {
					return err
				}
				if _, err := tx.NewInsert().Model(&store.Solve{
					TeamID: team.ID, ChallengeID: cid, UserID: &uid, SubmissionID: sub.ID, SolvedAt: at,
				}).Exec(ctx); err != nil {
					return err
				}
				sum.Solves++
			}
		}

		if _, err := tx.NewInsert().Model(&store.Page{
			Slug: "rules", Title: "Rules", BodyMD: rulesMD, Visible: true, UpdatedAt: now,
		}).On("CONFLICT (slug) DO NOTHING").Exec(ctx); err != nil {
			return err
		}
		for _, an := range []store.Announcement{
			{Title: "Welcome to Flagfall CTF", BodyMD: "The event is live. Read the [rules](/p/rules) before you start, and have fun.", Pinned: true, PostedAt: start},
			{Title: "Lattice Whisper: clarified", BodyMD: "The signatures in `sigs.txt` use the curve **secp256k1**. The challenge text now says so.", PostedAt: now.Add(-3 * time.Hour)},
		} {
			if _, err := tx.NewInsert().Model(&an).Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
	return sum, err
}

// ErrHasOwner means DemoLogins was asked to add a demo owner to an install
// that already has one, i.e. a real event. Public owner logins must never
// end up there.
var ErrHasOwner = errors.New("the database already has an owner; demo logins are only added to fresh sample installs")

// DemoLogins makes the sample event usable as a public demo: it gives the
// sample player DemoEmail a known password, adds an owner account
// (DemoOwnerEmail), and advertises both logins on the sign-in pages.
// Run it after Sample, and only for demo installs: anyone who reads the
// sign-in page can sign in as owner.
func DemoLogins(ctx context.Context, db *store.Store, now time.Time) error {
	if n, err := db.DB.NewSelect().Model((*store.User)(nil)).Where("role = 'owner'").Count(ctx); err != nil {
		return err
	} else if n > 0 {
		return ErrHasOwner
	}
	hash, err := auth.HashPassword(DemoPassword)
	if err != nil {
		return err
	}
	login := func(email string) *store.DemoAccount { return &store.DemoAccount{Email: email, Password: DemoPassword} }
	settings, err := json.Marshal(store.EventSettings{DemoAccount: login(DemoEmail), DemoAdmin: login(DemoOwnerEmail)})
	if err != nil {
		return err
	}
	now = now.UTC().Truncate(time.Second)
	return db.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewUpdate().Model((*store.User)(nil)).Set("password_hash = ?", hash).
			Where("email_key = ?", DemoEmail).Exec(ctx)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("demo player %s not found; load the sample event first", DemoEmail)
		}
		owner := &store.User{
			Name: DemoOwnerName, NameKey: strings.ToLower(DemoOwnerName), Email: DemoOwnerEmail, EmailKey: DemoOwnerEmail,
			PasswordHash: hash, Role: "owner", VerifiedAt: &now, CreatedAt: now,
		}
		if _, err := tx.NewInsert().Model(owner).Exec(ctx); err != nil {
			return fmt.Errorf("demo owner: %w", err)
		}
		_, err = tx.NewUpdate().Model((*store.Event)(nil)).Set("settings = ?", string(settings)).Where("id = 1").Exec(ctx)
		return err
	})
}

// boardOrder is the challenge order on the designed board.
var boardOrder = []string{
	"cookie-jar", "template-of-doom", "stack-smasher", "heap-feng-shui",
	"baby-rsa", "lattice-whisper", "crackme-v3", "obfuscated-oracle",
	"packet-archaeology", "memory-lane", "where-is-the-server", "sanity-check",
}

// inviteCode returns a random 10-character invite code.
func inviteCode() string {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	b := []byte(auth.RandomToken(10))
	for i := range b[:10] {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b[:10])
}
