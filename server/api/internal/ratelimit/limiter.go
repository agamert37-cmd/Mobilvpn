// Package ratelimit implements a small per-key token bucket, used to cap
// how many tunnel-provisioning requests one source IP can make per minute.
// The connect endpoint does real work (key generation, firewall/peer
// programming, cert issuance) — it is a resource an anonymous caller could
// otherwise hammer, since the existing Android client contract has no
// per-app credential to gate on instead (see docs/SECURITY.md).
package ratelimit

import (
	"sync"
	"time"
)

type bucket struct {
	tokens     float64
	lastRefill time.Time
}

type Limiter struct {
	mu         sync.Mutex
	buckets    map[string]*bucket
	ratePerSec float64
	burst      float64
	lastSweep  time.Time
}

// New creates a limiter allowing ratePerMinute sustained requests per key,
// with burst as the maximum instantaneous allowance.
func New(ratePerMinute, burst int) *Limiter {
	if ratePerMinute <= 0 {
		ratePerMinute = 60
	}
	if burst <= 0 {
		burst = ratePerMinute
	}
	return &Limiter{
		buckets:    make(map[string]*bucket),
		ratePerSec: float64(ratePerMinute) / 60.0,
		burst:      float64(burst),
		lastSweep:  time.Now(),
	}
}

// Allow reports whether the caller identified by key may proceed now,
// consuming one token if so.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst - 1, lastRefill: now}
		l.buckets[key] = b
		l.maybeSweep(now)
		return true
	}

	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * l.ratePerSec
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.lastRefill = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	l.maybeSweep(now)
	return true
}

// maybeSweep evicts long-idle buckets so a limiter fronting an internet-
// facing endpoint doesn't accumulate one entry per distinct source IP
// forever. Must be called with l.mu held.
func (l *Limiter) maybeSweep(now time.Time) {
	if now.Sub(l.lastSweep) < 10*time.Minute {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if now.Sub(b.lastRefill) > 30*time.Minute {
			delete(l.buckets, k)
		}
	}
}
