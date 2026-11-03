package store

import (
	"time"

	"github.com/uptrace/bun"
)

// Models mirror migrations/00001_initial.sql. Nullable columns are pointers.

type Event struct {
	bun.BaseModel `bun:"table:event"`

	ID               int64      `bun:"id,pk"`
	Name             string     `bun:"name"`
	StartsAt         *time.Time `bun:"starts_at"`
	EndsAt           *time.Time `bun:"ends_at"`
	FreezeAt         *time.Time `bun:"freeze_at"`
	TimeZone         string     `bun:"time_zone"`
	MaxTeamSize      int        `bun:"max_team_size"`
	ScoringMode      string     `bun:"scoring_mode"`
	RegistrationOpen bool       `bun:"registration_open"`
	TeamsLocked      bool       `bun:"teams_locked"`
	PublicChallenges bool       `bun:"public_challenges"`
	FlagRateLimit    int        `bun:"flag_rate_limit"`
	Settings         string     `bun:"settings"`
	UpdatedAt        time.Time  `bun:"updated_at"`
}

type Bracket struct {
	bun.BaseModel `bun:"table:brackets"`

	ID                   int64  `bun:"id,pk,autoincrement"`
	Name                 string `bun:"name"`
	Description          string `bun:"description"`
	RequiresVerification bool   `bun:"requires_verification"`
	Position             int    `bun:"position"`
}

type Team struct {
	bun.BaseModel `bun:"table:teams"`

	ID            int64      `bun:"id,pk,autoincrement"`
	Name          string     `bun:"name"`
	NameKey       string     `bun:"name_key"`
	Country       *string    `bun:"country"`
	BracketID     int64      `bun:"bracket_id"`
	InviteCode    string     `bun:"invite_code"`
	Recruiting    bool       `bun:"recruiting"`
	CaptainID     *int64     `bun:"captain_id"`
	Hidden        bool       `bun:"hidden"`
	PrizeEligible bool       `bun:"prize_eligible"`
	BannedAt      *time.Time `bun:"banned_at"`
	CreatedAt     time.Time  `bun:"created_at"`
}

type User struct {
	bun.BaseModel `bun:"table:users"`

	ID           int64      `bun:"id,pk,autoincrement"`
	Name         string     `bun:"name"`
	NameKey      string     `bun:"name_key"`
	Email        string     `bun:"email"`
	EmailKey     string     `bun:"email_key"`
	PasswordHash string     `bun:"password_hash"`
	Role         string     `bun:"role"`
	TeamID       *int64     `bun:"team_id"`
	Country      *string    `bun:"country"`
	TOTPSecret   *string    `bun:"totp_secret"`
	VerifiedAt   *time.Time `bun:"verified_at"`
	BannedAt     *time.Time `bun:"banned_at"`
	CreatedAt    time.Time  `bun:"created_at"`
}

// IsStaff reports whether the user can use the admin panel.
func (u *User) IsStaff() bool { return u.Role != "player" }

type Session struct {
	bun.BaseModel `bun:"table:sessions"`

	IDHash     string    `bun:"id_hash,pk"`
	UserID     int64     `bun:"user_id"`
	CreatedAt  time.Time `bun:"created_at"`
	LastSeenAt time.Time `bun:"last_seen_at"`
	ExpiresAt  time.Time `bun:"expires_at"`
	IP         *string   `bun:"ip"`
	UserAgent  *string   `bun:"user_agent"`
	// PreviewTheme is the theme an admin is previewing in this session.
	PreviewTheme *string `bun:"preview_theme"`
}

type PasswordReset struct {
	bun.BaseModel `bun:"table:password_resets"`

	TokenHash string     `bun:"token_hash,pk"`
	UserID    int64      `bun:"user_id"`
	ExpiresAt time.Time  `bun:"expires_at"`
	UsedAt    *time.Time `bun:"used_at"`
}

type Challenge struct {
	bun.BaseModel `bun:"table:challenges"`

	ID            int64      `bun:"id,pk,autoincrement"`
	Slug          string     `bun:"slug"`
	Name          string     `bun:"name"`
	Category      string     `bun:"category"`
	Difficulty    *string    `bun:"difficulty"`
	Author        string     `bun:"author"`
	DescriptionMD string     `bun:"description_md"`
	Connection    *string    `bun:"connection"`
	Value         int        `bun:"value"`
	MinValue      *int       `bun:"min_value"`
	Decay         *int       `bun:"decay"`
	State         string     `bun:"state"`
	VisibleFrom   *time.Time `bun:"visible_from"`
	Requires      string     `bun:"requires"`
	ThemeFields   string     `bun:"theme_fields"`
	Tags          string     `bun:"tags"`
	Position      int        `bun:"position"`
	// Service is the checked `service:` spec as JSON (challenges.ServiceSpec),
	// or nil when the challenge has no container.
	Service   *string   `bun:"service"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

// ChallengeService is the state of a challenge's container: what the
// organizer asked for and what the runner last saw.
type ChallengeService struct {
	bun.BaseModel `bun:"table:challenge_services"`

	ChallengeID int64      `bun:"challenge_id,pk"`
	Runtime     string     `bun:"runtime"` // docker (kubernetes in Phase 3)
	Ref         string     `bun:"ref"`     // container name
	Desired     string     `bun:"desired"` // running or stopped
	Status      string     `bun:"status"`  // see services.State*
	Message     string     `bun:"message"`
	HostPort    *int       `bun:"host_port"`
	CheckedAt   *time.Time `bun:"checked_at"`
}

// VisibleAt reports whether players may see the challenge at time t.
func (c *Challenge) VisibleAt(t time.Time) bool {
	return c.State == "visible" && (c.VisibleFrom == nil || !t.Before(*c.VisibleFrom))
}

type Flag struct {
	bun.BaseModel `bun:"table:flags"`

	ID          int64   `bun:"id,pk,autoincrement"`
	ChallengeID int64   `bun:"challenge_id"`
	Kind        string  `bun:"kind"`
	ValueHash   *string `bun:"value_hash"`
	Pattern     *string `bun:"pattern"`
	TeamID      *int64  `bun:"team_id"`
}

type File struct {
	bun.BaseModel `bun:"table:files"`

	ID          int64  `bun:"id,pk,autoincrement"`
	ChallengeID int64  `bun:"challenge_id"`
	Name        string `bun:"name"`
	StorageKey  string `bun:"storage_key"`
	Size        int64  `bun:"size"`
	SHA256      string `bun:"sha256"`
}

type Hint struct {
	bun.BaseModel `bun:"table:hints"`

	ID          int64  `bun:"id,pk,autoincrement"`
	ChallengeID int64  `bun:"challenge_id"`
	Cost        int    `bun:"cost"`
	ContentMD   string `bun:"content_md"`
	Position    int    `bun:"position"`
}

type HintUnlock struct {
	bun.BaseModel `bun:"table:hint_unlocks"`

	TeamID     int64     `bun:"team_id,pk"`
	HintID     int64     `bun:"hint_id,pk"`
	UserID     *int64    `bun:"user_id"`
	Cost       int       `bun:"cost"`
	UnlockedAt time.Time `bun:"unlocked_at"`
}

type Submission struct {
	bun.BaseModel `bun:"table:submissions"`

	ID          int64     `bun:"id,pk,autoincrement"`
	TeamID      int64     `bun:"team_id"`
	UserID      *int64    `bun:"user_id"`
	ChallengeID int64     `bun:"challenge_id"`
	Correct     bool      `bun:"correct"`
	Submitted   string    `bun:"submitted"`
	IP          *string   `bun:"ip"`
	SubmittedAt time.Time `bun:"submitted_at"`
}

type Solve struct {
	bun.BaseModel `bun:"table:solves"`

	TeamID       int64     `bun:"team_id,pk"`
	ChallengeID  int64     `bun:"challenge_id,pk"`
	UserID       *int64    `bun:"user_id"`
	SubmissionID int64     `bun:"submission_id"`
	SolvedAt     time.Time `bun:"solved_at"`
}

type ScoreAdjustment struct {
	bun.BaseModel `bun:"table:score_adjustments"`

	ID        int64     `bun:"id,pk,autoincrement"`
	TeamID    int64     `bun:"team_id"`
	Points    int       `bun:"points"`
	Reason    string    `bun:"reason"`
	ActorID   int64     `bun:"actor_id"`
	CreatedAt time.Time `bun:"created_at"`
}

type Announcement struct {
	bun.BaseModel `bun:"table:announcements"`

	ID       int64     `bun:"id,pk,autoincrement"`
	Title    string    `bun:"title"`
	BodyMD   string    `bun:"body_md"`
	Pinned   bool      `bun:"pinned"`
	PostedAt time.Time `bun:"posted_at"`
	AuthorID *int64    `bun:"author_id"`
}

type Page struct {
	bun.BaseModel `bun:"table:pages"`

	Slug      string    `bun:"slug,pk"`
	Title     string    `bun:"title"`
	BodyMD    string    `bun:"body_md"`
	Visible   bool      `bun:"visible"`
	Position  int       `bun:"position"`
	UpdatedAt time.Time `bun:"updated_at"`
}

type Theme struct {
	bun.BaseModel `bun:"table:themes"`

	ID          string    `bun:"id,pk"`
	Name        string    `bun:"name"`
	Version     string    `bun:"version"`
	Source      string    `bun:"source"`
	Manifest    string    `bun:"manifest"`
	InstalledAt time.Time `bun:"installed_at"`
}

type ThemeSettings struct {
	bun.BaseModel `bun:"table:theme_settings"`

	ID          int64      `bun:"id,pk,autoincrement"`
	ThemeID     string     `bun:"theme_id"`
	Version     int        `bun:"version"`
	Status      string     `bun:"status"`
	Data        string     `bun:"data"`
	CustomCSS   string     `bun:"custom_css"`
	PublishAt   *time.Time `bun:"publish_at"`
	PublishedBy *int64     `bun:"published_by"`
	PublishedAt *time.Time `bun:"published_at"`
}

type AuditEntry struct {
	bun.BaseModel `bun:"table:audit_log"`

	ID      int64     `bun:"id,pk,autoincrement"`
	ActorID *int64    `bun:"actor_id"`
	Action  string    `bun:"action"`
	Target  *string   `bun:"target"`
	Before  *string   `bun:"before"`
	After   *string   `bun:"after"`
	IP      *string   `bun:"ip"`
	At      time.Time `bun:"at"`
}

// ThemeImage is an image uploaded for a theme setting. Its bytes are the
// blob with the same SHA-256.
type ThemeImage struct {
	bun.BaseModel `bun:"table:theme_images"`

	SHA256      string    `bun:"sha256,pk"`
	ContentType string    `bun:"content_type"`
	Size        int64     `bun:"size"`
	UploadedBy  *int64    `bun:"uploaded_by"`
	CreatedAt   time.Time `bun:"created_at"`
}
