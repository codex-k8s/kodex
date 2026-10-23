package integrationpolicy

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
)

// Resolver refreshes authoritative DNS snapshots per integration origin.
// Resolver обновляет авторитетные DNS-снимки каждого origin интеграции.
type Resolver interface {
	// Resolve obtains the current snapshot for one integration origin.
	// Resolve получает текущий снимок одного origin интеграции.
	Resolve(context.Context, string) (dnsresolver.Snapshot, error)
	// Refresh obtains a fresh authoritative snapshot for the same origin.
	// Refresh получает свежий авторитетный снимок того же origin.
	Refresh(context.Context, string) (dnsresolver.Snapshot, error)
}

// Readiness tracks each integration origin independently and denies an empty set.
// Readiness отслеживает каждый origin отдельно и отвергает пустой набор.
type Readiness struct {
	policy     *Active
	resolver   Resolver
	validUntil atomic.Int64
	mu         sync.RWMutex
	perHost    map[string]int64
}

// NewReadiness binds one integration projection to a DNS resolver.
// NewReadiness связывает одну проекцию интеграций с DNS-resolver.
func NewReadiness(active *Active, resolver Resolver) *Readiness {
	return &Readiness{policy: active, resolver: resolver}
}

// Ready reports listener readiness when at least one origin has a fresh pin.
// Ready отмечает готовность listener при наличии свежего pin хотя бы одного origin.
func (r *Readiness) Ready() (bool, string) {
	if r != nil {
		r.mu.RLock()
		defer r.mu.RUnlock()
		now := time.Now().UnixNano()
		for _, until := range r.perHost {
			if now < until {
				return true, "ready"
			}
		}
	}
	return false, "integration projection is not ready"
}

// ReadyFor denies only the unavailable origin without blocking other origins.
// ReadyFor отклоняет только недоступный origin, не блокируя остальные.
func (r *Readiness) ReadyFor(host string) (bool, string) {
	if r != nil {
		r.mu.RLock()
		until := r.perHost[host]
		r.mu.RUnlock()
		if time.Now().UnixNano() < until {
			return true, "ready"
		}
	}
	return false, "integration destination is not ready"
}

// setReady replaces the locked per-origin map and records the refresh expiry.
// setReady заменяет карту origin под блокировкой и записывает срок обновления.
func (r *Readiness) setReady(ready map[string]int64, earliest int64) {
	r.mu.Lock()
	r.perHost = ready
	r.mu.Unlock()
	r.validUntil.Store(earliest)
}

// Check refreshes each origin and retains only fresh public answers overlapping pins.
// Check обновляет каждый origin и оставляет только свежие публичные ответы с pin.
func (r *Readiness) Check(ctx context.Context) {
	if r.policy == nil || !r.policy.Configured() || r.resolver == nil {
		r.setReady(nil, 0)
		return
	}
	var earliest time.Time
	ready := make(map[string]int64)
	for _, destination := range r.policy.Destinations() {
		snapshot, err := r.resolver.Refresh(ctx, destination.Hostname)
		if err != nil || mailpolicy.ValidateAddresses(snapshot.Addresses) != nil || !time.Now().Before(snapshot.ExpiresAt) {
			continue
		}
		allowed := false
		for _, address := range snapshot.Addresses {
			if r.policy.AllowsLiteral(destination.Hostname, destination.Port, address) {
				allowed = true
			}
		}
		if !allowed {
			continue
		}
		ready[destination.Hostname] = snapshot.ExpiresAt.UnixNano()
		if earliest.IsZero() || snapshot.ExpiresAt.Before(earliest) {
			earliest = snapshot.ExpiresAt
		}
	}
	if ctx.Err() != nil {
		r.setReady(nil, 0)
		return
	}
	if earliest.IsZero() {
		r.setReady(nil, 0)
		return
	}
	r.setReady(ready, earliest.UnixNano())
}

// Run periodically refreshes origin readiness and clears it when the worker exits.
// Run периодически обновляет готовность origin и сбрасывает её при выходе worker.
func (r *Readiness) Run(interval time.Duration) func(context.Context) error {
	return func(ctx context.Context) error {
		defer r.setReady(nil, 0)
		if interval <= 0 {
			return errors.New("integration readiness refresh interval is invalid")
		}
		for {
			r.Check(ctx)
			delay := interval
			if remaining := time.Until(time.Unix(0, r.validUntil.Load())); remaining > 0 && remaining/2 < delay {
				delay = remaining / 2
				if delay < 100*time.Millisecond {
					delay = 100 * time.Millisecond
				}
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
}
