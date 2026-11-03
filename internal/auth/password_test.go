package auth

import (
	"strings"
	"testing"
)

func TestHashAndCheck(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("unexpected hash format: %s", h)
	}
	if ok, err := CheckPassword(h, "correct horse battery"); !ok || err != nil {
		t.Errorf("right password rejected: %v %v", ok, err)
	}
	if ok, _ := CheckPassword(h, "wrong horse battery"); ok {
		t.Error("wrong password accepted")
	}
	h2, _ := HashPassword("correct horse battery")
	if h == h2 {
		t.Error("two hashes of the same password are identical; salt missing")
	}
}

func TestCheckRejectsGarbage(t *testing.T) {
	for _, h := range []string{"", "plain", "$argon2i$v=19$m=1,t=1,p=1$aa$bb", "$argon2id$v=19$m=x$aa$bb"} {
		if ok, err := CheckPassword(h, "x"); ok || err == nil {
			t.Errorf("CheckPassword(%q) = %v, %v", h, ok, err)
		}
	}
	if CheckPasswordOrDummy("", "anything") {
		t.Error("empty hash accepted")
	}
}

func TestValidatePassword(t *testing.T) {
	cases := map[string]bool{"short": false, "1234567890": false, "Password123": false, "plenty long passphrase": true}
	for pw, ok := range cases {
		if got := ValidatePassword(pw) == ""; got != ok {
			t.Errorf("ValidatePassword(%q) ok = %v, want %v", pw, got, ok)
		}
	}
}
