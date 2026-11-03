// Package ratelimit implements in-memory fixed-window rate limits.
//
// A Redis-backed implementation of the same interface is added in Phase 3 for
// multi-server installs.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter allows up to Limit events per Window for each key.
type Limiter struct {
	Limit  int
	Window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	sweeps  int
}

type bucket struct {
	start time.Time
	count int
}

// New returns a limiter allowing limit events per window per key.
func New(limit int, window time.Duration) *Limiter {
	return &Limiter{Limit: limit, Window: window, now: time.Now, buckets: map[string]*bucket{}}
}

// Allow records an event for key. It returns true if the event is within the
// limit, and otherwise how long until the window resets.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	return l.AllowN(key, l.Limit)
}

// AllowN is Allow with a per-call limit, for limits an admin can change at runtime.
func (l *Limiter) AllowN(key string, limit int) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweeps++
	if l.sweeps >= 1000 {
		l.sweeps = 0
		for k, b := range l.buckets {
			if now.Sub(b.start) >= l.Window {
				delete(l.buckets, k)
			}
		}
	}

	b := l.buckets[key]
	if b == nil || now.Sub(b.start) >= l.Window {
		b = &bucket{start: now}
		l.buckets[key] = b
	}
	if b.count >= limit {
		return false, b.start.Add(l.Window).Sub(now)
	}
	b.count++
	return true, 0
}

// Reset clears the count for key, e.g. after a successful sign-in.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	delete(l.buckets, key)
	l.mu.Unlock()
}
