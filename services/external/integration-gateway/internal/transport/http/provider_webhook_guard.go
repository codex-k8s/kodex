package httptransport

import (
	stdhttp "net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultProviderWebhookMaxInFlight     = 32
	defaultProviderWebhookRateLimitBurst  = 120
	defaultProviderWebhookRateLimitWindow = time.Second
	defaultProviderWebhookRetryAfter      = time.Second

	routeIDProviderWebhook  = "provider_webhook"
	routeIDExternalCallback = "external_callback"
)

type providerWebhookGuard struct {
	mu     sync.Mutex
	states map[providerWebhookGuardKey]*providerWebhookGuardState
	cfg    providerWebhookGuardConfig
	now    func() time.Time
}

type providerWebhookGuardConfig struct {
	MaxInFlight     int
	RateLimitBurst  int
	RateLimitWindow time.Duration
	RetryAfter      time.Duration
}

type providerWebhookGuardKey struct {
	routeID string
	source  string
}

type providerWebhookGuardState struct {
	inFlight    int
	windowStart time.Time
	windowCount int
}

type providerWebhookLease struct {
	guard providerWebhookGuardReleaser
	key   providerWebhookGuardKey
	once  sync.Once
}

type providerWebhookGuardReleaser interface {
	release(providerWebhookGuardKey)
}

func newProviderWebhookGuard(cfg Config) *providerWebhookGuard {
	return &providerWebhookGuard{
		states: make(map[providerWebhookGuardKey]*providerWebhookGuardState),
		cfg: providerWebhookGuardConfig{
			MaxInFlight:     positiveOrDefault(cfg.ProviderWebhookMaxInFlight, defaultProviderWebhookMaxInFlight),
			RateLimitBurst:  positiveOrDefault(cfg.ProviderWebhookRateLimitBurst, defaultProviderWebhookRateLimitBurst),
			RateLimitWindow: durationOrDefault(cfg.ProviderWebhookRateLimitWindow, defaultProviderWebhookRateLimitWindow),
			RetryAfter:      durationOrDefault(cfg.ProviderWebhookRetryAfter, defaultProviderWebhookRetryAfter),
		},
		now: time.Now,
	}
}

func (g *providerWebhookGuard) acquire(routeID string, source string) (*providerWebhookLease, *SafeError) {
	if g == nil {
		return &providerWebhookLease{}, nil
	}
	key := providerWebhookGuardKey{
		routeID: strings.ToLower(strings.TrimSpace(routeID)),
		source:  strings.ToLower(strings.TrimSpace(source)),
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	state := g.states[key]
	if state == nil {
		state = &providerWebhookGuardState{}
		g.states[key] = state
	}
	if state.inFlight >= g.cfg.MaxInFlight {
		return nil, NewSafeError(stdhttp.StatusServiceUnavailable, CodeBackpressure, "provider webhook route is under backpressure", true).WithRetryAfter(g.cfg.RetryAfter)
	}
	now := g.now().UTC()
	if state.windowStart.IsZero() || now.Sub(state.windowStart) >= g.cfg.RateLimitWindow {
		state.windowStart = now
		state.windowCount = 0
	}
	if state.windowCount >= g.cfg.RateLimitBurst {
		return nil, NewSafeError(stdhttp.StatusTooManyRequests, CodeRateLimited, "provider webhook rate limit is active", true).WithRetryAfter(g.cfg.RetryAfter)
	}
	state.windowCount++
	state.inFlight++
	return &providerWebhookLease{guard: g, key: key}, nil
}

func (g *providerWebhookGuard) release(key providerWebhookGuardKey) {
	g.mu.Lock()
	defer g.mu.Unlock()
	state := g.states[key]
	if state == nil || state.inFlight <= 0 {
		return
	}
	state.inFlight--
}

func (l *providerWebhookLease) Release() {
	if l == nil || l.guard == nil {
		return
	}
	l.once.Do(func() {
		l.guard.release(l.key)
	})
}

func positiveOrDefault(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func durationOrDefault(value time.Duration, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}
