// Package flags stores and checks challenge flags.
//
// Exact and case-insensitive flags are stored as HMAC-SHA256 hashes keyed
// with the server secret, so a leaked database backup doesn't reveal answers.
// Pattern flags are stored as RE2 expressions anchored to the whole submission.
package flags

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Kinds of flag.
const (
	Exact           = "exact"
	CaseInsensitive = "case-insensitive"
	Pattern         = "pattern"
	PerTeam         = "per-team"
)

// MaxLength is the longest submission accepted, in bytes.
const MaxLength = 512

// Flag is what the checker needs from a stored flag.
type Flag struct {
	Kind      string
	ValueHash string
	Pattern   string
	TeamID    *int64
}

// Normalize trims whitespace players often paste by accident.
func Normalize(s string) string { return strings.TrimSpace(s) }

// Hash returns the stored form of an exact, case-insensitive or per-team flag.
func Hash(secret []byte, kind, value string) string {
	value = Normalize(value)
	if kind == CaseInsensitive {
		value = strings.ToLower(value)
	}
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("flagfall-flag-v1\x00"))
	m.Write([]byte(value))
	return hex.EncodeToString(m.Sum(nil))
}

// TeamValue derives a team's per-team flag for a challenge: the format with
// its %s replaced by 24 hex characters of an HMAC keyed with the server
// secret. The same inputs always give the same flag, so it can be shown again
// without being stored; another server secret gives other flags.
func TeamValue(secret []byte, challengeID, teamID int64, format string) string {
	m := hmac.New(sha256.New, secret)
	fmt.Fprintf(m, "flagfall-team-flag-v1\x00%d\x00%d", challengeID, teamID)
	return strings.Replace(format, "%s", hex.EncodeToString(m.Sum(nil))[:24], 1)
}

// CompilePattern checks and compiles a pattern flag.
func CompilePattern(p string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(`^(?:` + p + `)$`)
	if err != nil {
		return nil, fmt.Errorf("invalid flag pattern: %w", err)
	}
	return re, nil
}

var patternCache sync.Map // string -> *regexp.Regexp

// Matches reports whether submission is accepted by f for the given team.
func Matches(secret []byte, f Flag, submission string, teamID int64) bool {
	submission = Normalize(submission)
	if submission == "" || len(submission) > MaxLength {
		return false
	}
	switch f.Kind {
	case Exact, CaseInsensitive:
		return hmac.Equal([]byte(Hash(secret, f.Kind, submission)), []byte(f.ValueHash))
	case PerTeam:
		if f.TeamID == nil || *f.TeamID != teamID {
			return false
		}
		return hmac.Equal([]byte(Hash(secret, Exact, submission)), []byte(f.ValueHash))
	case Pattern:
		v, ok := patternCache.Load(f.Pattern)
		if !ok {
			re, err := CompilePattern(f.Pattern)
			if err != nil {
				return false
			}
			v, _ = patternCache.LoadOrStore(f.Pattern, re)
		}
		return v.(*regexp.Regexp).MatchString(submission)
	}
	return false
}

// Any reports whether any of the flags accepts the submission.
func Any(secret []byte, fs []Flag, submission string, teamID int64) bool {
	for _, f := range fs {
		if Matches(secret, f, submission, teamID) {
			return true
		}
	}
	return false
}
