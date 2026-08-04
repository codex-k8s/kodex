package ratelimit

import (
	"testing"
	"time"
)

func TestLimiterBoundsRateAndConcurrency(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	limiter := New(Config{Window: time.Minute, Limit: 2, MaximumKeys: 2, PreAuthConcurrency: 1, GlobalHTTPConcurrency: 2, PerSubjectHTTPConcurrency: 1, GlobalWebSocketConcurrency: 2, PerSubjectWebSocketConcurrency: 1})
	limiter.now = func() time.Time { return now }
	if !limiter.Allow("actor-a") || !limiter.Allow("actor-a") || limiter.Allow("actor-a") {
		t.Fatal("fixed window limit is not closed")
	}
	release, ok := limiter.AcquireHTTP("actor-a")
	if !ok {
		t.Fatal("first concurrency slot was rejected")
	}
	if _, ok := limiter.AcquireHTTP("actor-a"); ok {
		t.Fatal("second subject HTTP slot was accepted")
	}
	otherRelease, ok := limiter.AcquireHTTP("actor-b")
	if !ok {
		t.Fatal("one subject exhausted another subject quota")
	}
	otherRelease()
	release()
	if release, ok := limiter.AcquireHTTP("actor-a"); !ok {
		t.Fatal("released concurrency slot was not reusable")
	} else {
		release()
	}
	now = now.Add(time.Minute)
	if !limiter.Allow("actor-a") {
		t.Fatal("new fixed window was rejected")
	}
	if !limiter.Allow("actor-b") || limiter.Allow("actor-c") {
		t.Fatal("bounded key registry did not fail closed while all keys were current")
	}
	now = now.Add(time.Minute)
	if !limiter.Allow("actor-c") {
		t.Fatal("expired inactive key was not cleaned deterministically")
	}
}
