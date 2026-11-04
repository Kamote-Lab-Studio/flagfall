package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 TOTP uses HMAC-SHA1; authenticator apps expect it.
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP (RFC 6238): 6 digits, 30-second steps, HMAC-SHA1, as every
// authenticator app expects.
const (
	TOTPDigits = 6
	TOTPPeriod = 30 * time.Second
	// totpSkew accepts the step before and after, for clocks a little off.
	totpSkew = 1
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a random 160-bit secret, base32 without padding.
func NewTOTPSecret() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b32.EncodeToString(b)
}

// TOTPStep is the time step t falls in.
func TOTPStep(t time.Time) int64 { return t.Unix() / int64(TOTPPeriod/time.Second) }

// TOTPCode is the code for a step.
func TOTPCode(secret string, step int64) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", fmt.Errorf("bad TOTP secret: %w", err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", TOTPDigits, n%1_000_000), nil
}

// CheckTOTP reports whether code is valid at now, and for which step.
// Steps at or before lastStep are refused, so a code works only once.
func CheckTOTP(secret, code string, now time.Time, lastStep int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != TOTPDigits {
		return 0, false
	}
	cur := TOTPStep(now)
	for d := int64(-totpSkew); d <= totpSkew; d++ {
		step := cur + d
		if step <= lastStep {
			continue
		}
		want, err := TOTPCode(secret, step)
		if err == nil && hmac.Equal([]byte(want), []byte(code)) {
			return step, true
		}
	}
	return 0, false
}

// TOTPURL is the otpauth:// link authenticator apps read (usually as a QR code).
func TOTPURL(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{"secret": {secret}, "issuer": {issuer}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// SealTOTPSecret encrypts a TOTP secret for storage (AES-256-GCM, with a key
// derived from the server secret), so a database copy alone can't make codes.
func SealTOTPSecret(serverSecret []byte, totpSecret string) (string, error) {
	gcm, err := totpCipher(serverSecret)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return "v1:" + base64.RawStdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(totpSecret), nil)), nil
}

// OpenTOTPSecret reverses SealTOTPSecret.
func OpenTOTPSecret(serverSecret []byte, sealed string) (string, error) {
	raw, ok := strings.CutPrefix(sealed, "v1:")
	if !ok {
		return "", errors.New("unknown TOTP secret format")
	}
	b, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil {
		return "", err
	}
	gcm, err := totpCipher(serverSecret)
	if err != nil {
		return "", err
	}
	if len(b) < gcm.NonceSize() {
		return "", errors.New("TOTP secret too short")
	}
	plain, err := gcm.Open(nil, b[:gcm.NonceSize()], b[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("TOTP secret can't be read with this server's secret key")
	}
	return string(plain), nil
}

func totpCipher(serverSecret []byte) (cipher.AEAD, error) {
	m := hmac.New(sha256.New, serverSecret)
	m.Write([]byte("flagfall-totp-key-v1"))
	block, err := aes.NewCipher(m.Sum(nil))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// NewRecoveryCodes returns n one-time codes like "k7m2-9qxp".
func NewRecoveryCodes(n int) []string {
	const alphabet = "23456789abcdefghjkmnpqrstuvwxyz" // no 0/o, 1/l/i
	out := make([]string, n)
	b := make([]byte, 8)
	for i := range out {
		if _, err := rand.Read(b); err != nil {
			panic(err)
		}
		var sb strings.Builder
		for j, x := range b {
			if j == 4 {
				sb.WriteByte('-')
			}
			sb.WriteByte(alphabet[int(x)%len(alphabet)])
		}
		out[i] = sb.String()
	}
	return out
}

// HashRecoveryCode is the stored form of a recovery code (HMAC with the
// server secret; case, spaces and dashes don't matter).
func HashRecoveryCode(serverSecret []byte, code string) string {
	c := strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
	m := hmac.New(sha256.New, serverSecret)
	m.Write([]byte("flagfall-recovery-v1\x00" + c))
	return base64.RawStdEncoding.EncodeToString(m.Sum(nil))
}
