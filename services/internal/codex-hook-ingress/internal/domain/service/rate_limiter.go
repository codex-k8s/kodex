package service

import (
	"context"
	"sync"
	"time"

	hookerrs "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/errs"
)

const (
	defaultRateLimitWindow = time.Minute
	defaultRateLimitBurst  = 300
)

// RateLimitConfig controls fixed-window logical admission.
type RateLimitConfig struct {
	Window time.Duration
	Burst  int
}

// NewFixedWindowRateLimiter creates a source/run/event fixed-window limiter.
func NewFixedWindowRateLimiter(cfg RateLimitConfig) RateLimiter {
	if cfg.Window <= 0 {
		cfg.Window = defaultRateLimitWindow
	}
	if cfg.Burst <= 0 {
		cfg.Burst = defaultRateLimitBurst
	}
	return &fixedWindowRateLimiter{
		window: cfg.Window,
		burst:  cfg.Burst,
		seen:   make(map[string]rateWindow),
	}
}

type fixedWindowRateLimiter struct {
	mu     sync.Mutex
	window time.Duration
	burst  int
	seen   map[string]rateWindow
}

type rateWindow struct {
	start time.Time
	count int
}

func (limiter *fixedWindowRateLimiter) Ready() bool {
	return limiter != nil && limiter.window > 0 && limiter.burst > 0 && limiter.seen != nil
}

func (limiter *fixedWindowRateLimiter) Allow(_ context.Context, check RateLimitCheck) (RateLimitDecision, error) {
	if !limiter.Ready() {
		return RateLimitDecision{}, hookerrs.ErrDependencyUnavailable
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	if check.At.IsZero() {
		check.At = time.Now().UTC()
	}
	key := check.SourceRef + "|" + check.RunID + "|" + string(check.HookEventName)
	window := limiter.seen[key]
	if window.start.IsZero() || !check.At.Before(window.start.Add(limiter.window)) {
		window = rateWindow{start: check.At}
	}
	window.count++
	limiter.seen[key] = window
	if window.count > limiter.burst {
		return RateLimitDecision{
			Allowed:    false,
			ReasonCode: string(hookerrs.ErrRateLimited),
			RetryAfter: window.start.Add(limiter.window).Sub(check.At),
		}, nil
	}
	return RateLimitDecision{Allowed: true}, nil
}

type noopRateLimiter struct{}

func (noopRateLimiter) Ready() bool {
	return true
}

func (noopRateLimiter) Allow(_ context.Context, _ RateLimitCheck) (RateLimitDecision, error) {
	return RateLimitDecision{Allowed: true}, nil
}
