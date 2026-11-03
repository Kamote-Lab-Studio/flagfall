// Package auth hashes passwords and manages sessions.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (OWASP 2024 guidance: m=19 MiB, t=2, p=1).
const (
	argonMemory  = 19 * 1024
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	saltLen      = 16
)

// HashPassword returns an Argon2id hash in PHC string format.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

var errBadHash = errors.New("unrecognized password hash")

// CheckPassword reports whether password matches the PHC-format hash.
func CheckPassword(hash, password string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errBadHash
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, errBadHash
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, errBadHash
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return false, errBadHash
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash is checked against when an account doesn't exist, so sign-in
// takes the same time either way and can't be used to find accounts.
var dummyHash, _ = HashPassword("flagfall-timing-equalizer")

// CheckPasswordOrDummy runs a full hash check even when hash is empty.
func CheckPasswordOrDummy(hash, password string) bool {
	if hash == "" {
		CheckPassword(dummyHash, password)
		return false
	}
	ok, _ := CheckPassword(hash, password)
	return ok
}

// ValidatePassword enforces the password policy in docs/dev/api.md.
func ValidatePassword(password string) string {
	switch {
	case len(password) < 10:
		return "Use at least 10 characters."
	case len(password) > 256:
		return "Use at most 256 characters."
	case commonPasswords[strings.ToLower(password)]:
		return "That password is too common. Choose another."
	}
	return ""
}

// commonPasswords is a short list of the most common 10+ character
// passwords. A fuller list ships as an embedded file later in Phase 1.
var commonPasswords = map[string]bool{
	"1234567890": true, "12345678910": true, "123456789a": true, "qwertyuiop": true,
	"password12": true, "password123": true, "password1234": true, "iloveyou12": true,
	"1q2w3e4r5t": true, "qwerty1234": true, "0987654321": true, "1111111111": true,
	"abcdefghij": true, "letmein123": true, "welcome123": true, "administrator": true,
	"passw0rd123": true, "changeme123": true, "flagfall123": true, "capturetheflag": true,
}

// RandomToken returns n random bytes, URL-safe base64 encoded.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
