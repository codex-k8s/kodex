package ratelimit

import (
	"testing"
	"time"
)

func TestLimiterBoundsRateAndConcurrency(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	limiter := New(Config{Window: time.Minute, Limit: 2, MaximumKeys: 2, Concurrency: 1})
	limiter.now = func() time.Time { return now }
	if !limiter.Allow("actor-a") || !limiter.Allow("actor-a") || limiter.Allow("actor-a") {
		t.Fatal("fixed window limit is not closed")
	}
	release, ok := limiter.Acquire()
	if !ok {
		t.Fatal("first concurrency slot was rejected")
	}
	if _, ok := limiter.Acquire(); ok {
		t.Fatal("second concurrency slot was accepted")
	}
	release()
	if _, ok := limiter.Acquire(); !ok {
		t.Fatal("released concurrency slot was not reusable")
	}
	now = now.Add(time.Minute)
	if !limiter.Allow("actor-a") {
		t.Fatal("new fixed window was rejected")
	}
	if !limiter.Allow("actor-b") || !limiter.Allow("actor-c") {
		t.Fatal("bounded key registry rejected a valid new key")
	}
}
