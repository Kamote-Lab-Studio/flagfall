package ratelimit

import (
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	now := time.Unix(0, 0)
	l := New(3, time.Minute)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("team:1"); !ok {
			t.Fatalf("attempt %d blocked", i+1)
		}
	}
	ok, wait := l.Allow("team:1")
	if ok || wait != time.Minute {
		t.Fatalf("4th attempt: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.Allow("team:2"); !ok {
		t.Fatal("other key blocked")
	}

	now = now.Add(61 * time.Second)
	if ok, _ := l.Allow("team:1"); !ok {
		t.Fatal("not reset after the window")
	}

	l.Reset("team:2")
	if _, exists := l.buckets["team:2"]; exists {
		t.Fatal("Reset did not clear the key")
	}
}
