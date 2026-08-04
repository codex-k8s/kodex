package ratelimit

import (
	"sync"
	"time"
)

type Config struct {
	Window      time.Duration
	Limit       uint32
	MaximumKeys int
	Concurrency int
}

type bucket struct {
	started time.Time
	count   uint32
	seen    time.Time
}

type Limiter struct {
	mu      sync.Mutex
	config  Config
	buckets map[string]bucket
	global  chan struct{}
	now     func() time.Time
}

func New(config Config) *Limiter {
	return &Limiter{
		config: config, buckets: make(map[string]bucket, config.MaximumKeys),
		global: make(chan struct{}, config.Concurrency), now: time.Now,
	}
}

func (limiter *Limiter) Allow(key string) bool {
	if limiter == nil || key == "" || limiter.config.Window < time.Second || limiter.config.Limit == 0 ||
		limiter.config.MaximumKeys < 1 || limiter.config.Concurrency < 1 {
		return false
	}
	now := limiter.now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	value, ok := limiter.buckets[key]
	if !ok && len(limiter.buckets) >= limiter.config.MaximumKeys {
		var oldestKey string
		var oldest time.Time
		for candidate, entry := range limiter.buckets {
			if oldestKey == "" || entry.seen.Before(oldest) {
				oldestKey, oldest = candidate, entry.seen
			}
		}
		delete(limiter.buckets, oldestKey)
	}
	if !ok || now.Sub(value.started) >= limiter.config.Window {
		value = bucket{started: now}
	}
	value.seen = now
	if value.count >= limiter.config.Limit {
		limiter.buckets[key] = value
		return false
	}
	value.count++
	limiter.buckets[key] = value
	return true
}

func (limiter *Limiter) Acquire() (func(), bool) {
	if limiter == nil {
		return func() {}, false
	}
	select {
	case limiter.global <- struct{}{}:
		return func() { <-limiter.global }, true
	default:
		return func() {}, false
	}
}
