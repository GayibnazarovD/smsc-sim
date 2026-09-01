package throttle

import (
	"testing"
	"time"
)

func TestBucketBurstThenRefill(t *testing.T) {
	now := time.Unix(0, 0)
	b := New(10, 5) // 10/s, burst 5
	b.setClock(func() time.Time { return now })

	for i := 0; i < 5; i++ {
		if !b.Allow() {
			t.Fatalf("burst token %d denied", i)
		}
	}
	if b.Allow() {
		t.Fatal("6th call within burst should be denied")
	}

	now = now.Add(300 * time.Millisecond) // +3 tokens
	got := 0
	for i := 0; i < 5; i++ {
		if b.Allow() {
			got++
		}
	}
	if got != 3 {
		t.Fatalf("after 300ms refill got %d tokens, want 3", got)
	}
}

func TestBucketCapsAtBurst(t *testing.T) {
	now := time.Unix(0, 0)
	b := New(100, 10)
	b.setClock(func() time.Time { return now })
	now = now.Add(time.Hour)
	got := 0
	for i := 0; i < 50; i++ {
		if b.Allow() {
			got++
		}
	}
	if got != 10 {
		t.Fatalf("idle bucket dispensed %d, want burst cap 10", got)
	}
}
