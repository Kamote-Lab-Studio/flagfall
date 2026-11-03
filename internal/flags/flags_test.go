package flags

import (
	"strings"
	"testing"
)

var secret = []byte("0123456789abcdef0123456789abcdef")

func TestExact(t *testing.T) {
	f := Flag{Kind: Exact, ValueHash: Hash(secret, Exact, "flag{Hello}")}
	cases := map[string]bool{
		"flag{Hello}":     true,
		"  flag{Hello}\n": true,
		"flag{hello}":     false,
		"flag{Hello} x":   false,
		"":                false,
	}
	for in, want := range cases {
		if got := Matches(secret, f, in, 1); got != want {
			t.Errorf("Matches(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestHashDependsOnSecret(t *testing.T) {
	if Hash(secret, Exact, "flag{x}") == Hash([]byte("another-secret-another-secret-xx"), Exact, "flag{x}") {
		t.Error("hash does not depend on the secret")
	}
}

func TestCaseInsensitive(t *testing.T) {
	f := Flag{Kind: CaseInsensitive, ValueHash: Hash(secret, CaseInsensitive, "FLAG{Hello}")}
	for _, in := range []string{"flag{hello}", "FLAG{HELLO}", "Flag{Hello}"} {
		if !Matches(secret, f, in, 1) {
			t.Errorf("%q rejected", in)
		}
	}
}

func TestPatternIsAnchored(t *testing.T) {
	f := Flag{Kind: Pattern, Pattern: `flag\{[0-9]{4}\}`}
	cases := map[string]bool{"flag{1234}": true, "flag{12345}": false, "xflag{1234}": false, "flag{1234}x": false}
	for in, want := range cases {
		if got := Matches(secret, f, in, 1); got != want {
			t.Errorf("Matches(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := CompilePattern("flag{("); err == nil {
		t.Error("bad pattern compiled")
	}
	if Matches(secret, Flag{Kind: Pattern, Pattern: "("}, "(", 1) {
		t.Error("bad pattern matched")
	}
}

func TestPerTeam(t *testing.T) {
	team := int64(7)
	f := Flag{Kind: PerTeam, ValueHash: Hash(secret, Exact, "flag{team7}"), TeamID: &team}
	if !Matches(secret, f, "flag{team7}", 7) {
		t.Error("own flag rejected")
	}
	if Matches(secret, f, "flag{team7}", 8) {
		t.Error("another team's flag accepted")
	}
}

func TestTooLong(t *testing.T) {
	long := "flag{" + strings.Repeat("a", MaxLength) + "}"
	f := Flag{Kind: Exact, ValueHash: Hash(secret, Exact, long)}
	if Matches(secret, f, long, 1) {
		t.Error("over-length submission accepted")
	}
}
