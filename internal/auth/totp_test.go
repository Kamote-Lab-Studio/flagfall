package auth

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

func TestTOTPMatchesRFC6238(t *testing.T) {
	// RFC 6238 appendix B, SHA-1 key "12345678901234567890" (last 6 digits).
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	for unix, want := range map[int64]string{59: "287082", 1111111109: "081804", 1111111111: "050471", 1234567890: "005924", 2000000000: "279037"} {
		got, err := TOTPCode(secret, TOTPStep(time.Unix(unix, 0)))
		if err != nil || got != want {
			t.Errorf("t=%d: %s, %v; want %s", unix, got, err, want)
		}
	}
}

func TestCheckTOTP(t *testing.T) {
	secret := NewTOTPSecret()
	now := time.Unix(1_800_000_000, 0)
	code, _ := TOTPCode(secret, TOTPStep(now))
	step, ok := CheckTOTP(secret, code[:3]+" "+code[3:], now, 0)
	if !ok || step != TOTPStep(now) {
		t.Fatal("current code refused")
	}
	if _, ok := CheckTOTP(secret, code, now, step); ok {
		t.Fatal("a used code worked twice")
	}
	if _, ok := CheckTOTP(secret, code, now.Add(30*time.Second), 0); !ok {
		t.Fatal("previous step refused")
	}
	if _, ok := CheckTOTP(secret, code, now.Add(2*time.Minute), 0); ok {
		t.Fatal("an old code worked")
	}
	if _, ok := CheckTOTP(secret, "12345", now, 0); ok {
		t.Fatal("short code worked")
	}
}

func TestSealTOTPSecret(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	sealed, err := SealTOTPSecret(key, "JBSWY3DPEHPK3PXP")
	if err != nil || strings.Contains(sealed, "JBSWY3DP") {
		t.Fatalf("sealed = %q, %v", sealed, err)
	}
	if got, err := OpenTOTPSecret(key, sealed); err != nil || got != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("opened = %q, %v", got, err)
	}
	if _, err := OpenTOTPSecret([]byte(strings.Repeat("x", 32)), sealed); err == nil {
		t.Fatal("another key opened it")
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes := NewRecoveryCodes(10)
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 9 || c[4] != '-' || seen[c] {
			t.Fatalf("codes = %v", codes)
		}
		seen[c] = true
	}
	key := []byte("k")
	if HashRecoveryCode(key, codes[0]) != HashRecoveryCode(key, " "+strings.ToUpper(strings.ReplaceAll(codes[0], "-", ""))+" ") {
		t.Fatal("recovery code isn't forgiving about case and dashes")
	}
}
