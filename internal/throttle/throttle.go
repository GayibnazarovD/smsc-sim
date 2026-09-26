// Package throttle provides a monotonic-clock token bucket used to enforce a
// per-operator submit_sm rate limit.
package throttle

import (
	"sync"
	"time"
)

// Bucket is a lazily-refilled token bucket, safe for concurrent use.
type Bucket struct {
	mu     sync.Mutex
	rate   float64 // tokens added per second
	burst  float64 // maximum tokens held
	tokens float64
	last   time.Time
	now    func() time.Time // swappable for tests
}

// New returns a bucket that permits rate events/sec with the given burst
// capacity. It starts full.
func New(rate, burst float64) *Bucket {
	return &Bucket{
		rate:   rate,
		burst:  burst,
		tokens: burst,
		last:   time.Now(),
		now:    time.Now,
	}
}

// Allow consumes one token, returning true if one was available.
func (b *Bucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * b.rate
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// setClock swaps the time source and rebases the bucket to it. Test-only.
func (b *Bucket) setClock(now func() time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.now = now
	b.last = now()
	b.tokens = b.burst
}

// SetRate dynamically changes the rate and burst limit at runtime.
func (b *Bucket) SetRate(rate, burst float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rate = rate
	b.burst = burst
	if b.tokens > burst {
		b.tokens = burst
	}
}

// Tokens returns the current token count (for metrics/introspection).
func (b *Bucket) Tokens() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tokens
}
