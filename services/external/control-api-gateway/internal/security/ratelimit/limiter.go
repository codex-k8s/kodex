package ratelimit

import (
	"sync"
	"time"
)

type Config struct {
	Window                         time.Duration
	Limit                          uint32
	MaximumKeys                    int
	PreAuthConcurrency             int
	GlobalHTTPConcurrency          int
	PerSubjectHTTPConcurrency      int
	GlobalWebSocketConcurrency     int
	PerSubjectWebSocketConcurrency int
}

type bucket struct {
	started time.Time
	count   uint32
	seen    time.Time
	http    int
	ws      int
}

type Limiter struct {
	mu      sync.Mutex
	config  Config
	buckets map[string]bucket
	preauth chan struct{}
	http    chan struct{}
	ws      chan struct{}
	now     func() time.Time
}

func New(config Config) *Limiter {
	return &Limiter{
		config: config, buckets: make(map[string]bucket, config.MaximumKeys),
		preauth: make(chan struct{}, config.PreAuthConcurrency),
		http:    make(chan struct{}, config.GlobalHTTPConcurrency),
		ws:      make(chan struct{}, config.GlobalWebSocketConcurrency), now: time.Now,
	}
}

func (limiter *Limiter) valid() bool {
	return limiter != nil && limiter.config.Window >= time.Second && limiter.config.Limit > 0 && limiter.config.MaximumKeys > 0 &&
		limiter.config.PreAuthConcurrency > 0 && limiter.config.GlobalHTTPConcurrency > 0 && limiter.config.PerSubjectHTTPConcurrency > 0 &&
		limiter.config.PerSubjectHTTPConcurrency < limiter.config.GlobalHTTPConcurrency && limiter.config.GlobalWebSocketConcurrency > 0 &&
		limiter.config.PerSubjectWebSocketConcurrency > 0 && limiter.config.PerSubjectWebSocketConcurrency < limiter.config.GlobalWebSocketConcurrency
}

func (limiter *Limiter) Allow(key string) bool {
	if !limiter.valid() || key == "" {
		return false
	}
	now := limiter.now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	value, ok := limiter.bucketLocked(key, now)
	if !ok {
		return false
	}
	if now.Sub(value.started) >= limiter.config.Window {
		value.started, value.count = now, 0
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

func (limiter *Limiter) AcquirePreAuth() (func(), bool) {
	if !limiter.valid() {
		return func() {}, false
	}
	return acquireChannel(limiter.preauth)
}

func (limiter *Limiter) AcquireHTTP(key string) (func(), bool) {
	return limiter.acquireSubject(key, false)
}

func (limiter *Limiter) AcquireWebSocket(key string) (func(), bool) {
	return limiter.acquireSubject(key, true)
}

func (limiter *Limiter) acquireSubject(key string, websocket bool) (func(), bool) {
	if !limiter.valid() || key == "" {
		return func() {}, false
	}
	global := limiter.http
	if websocket {
		global = limiter.ws
	}
	releaseGlobal, ok := acquireChannel(global)
	if !ok {
		return func() {}, false
	}
	now := limiter.now()
	limiter.mu.Lock()
	value, ok := limiter.bucketLocked(key, now)
	if ok {
		if websocket {
			ok = value.ws < limiter.config.PerSubjectWebSocketConcurrency
			if ok {
				value.ws++
			}
		} else {
			ok = value.http < limiter.config.PerSubjectHTTPConcurrency
			if ok {
				value.http++
			}
		}
		value.seen = now
		limiter.buckets[key] = value
	}
	limiter.mu.Unlock()
	if !ok {
		releaseGlobal()
		return func() {}, false
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			limiter.mu.Lock()
			entry, exists := limiter.buckets[key]
			if exists {
				if websocket && entry.ws > 0 {
					entry.ws--
				}
				if !websocket && entry.http > 0 {
					entry.http--
				}
				entry.seen = limiter.now()
				limiter.buckets[key] = entry
			}
			limiter.mu.Unlock()
			releaseGlobal()
		})
	}, true
}

func (limiter *Limiter) bucketLocked(key string, now time.Time) (bucket, bool) {
	if value, ok := limiter.buckets[key]; ok {
		return value, true
	}
	if len(limiter.buckets) >= limiter.config.MaximumKeys {
		var oldestKey string
		var oldest time.Time
		for candidate, entry := range limiter.buckets {
			if entry.http != 0 || entry.ws != 0 || now.Sub(entry.seen) < limiter.config.Window {
				continue
			}
			if oldestKey == "" || entry.seen.Before(oldest) || (entry.seen.Equal(oldest) && candidate < oldestKey) {
				oldestKey, oldest = candidate, entry.seen
			}
		}
		if oldestKey == "" {
			return bucket{}, false
		}
		delete(limiter.buckets, oldestKey)
	}
	return bucket{started: now, seen: now}, true
}

func acquireChannel(channel chan struct{}) (func(), bool) {
	select {
	case channel <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-channel }) }, true
	default:
		return func() {}, false
	}
}
