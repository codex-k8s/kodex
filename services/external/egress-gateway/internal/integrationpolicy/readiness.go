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

type Resolver interface {
	Resolve(context.Context, string) (dnsresolver.Snapshot, error)
	Refresh(context.Context, string) (dnsresolver.Snapshot, error)
}

// Readiness проверяет актуальный DNS snapshot каждого origin отдельно;
// пустой список закрыт. Не влияет на готовность других listener и корневого Pod.
type Readiness struct {
	policy     *Active
	resolver   Resolver
	validUntil atomic.Int64
	mu         sync.RWMutex
	perHost    map[string]int64
}

func NewReadiness(active *Active, resolver Resolver) *Readiness {
	return &Readiness{policy: active, resolver: resolver}
}

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

// ReadyFor закрывает только недоступный origin, не блокируя остальные
// независимые интеграции того же listener.
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

func (r *Readiness) setReady(ready map[string]int64, earliest int64) {
	r.mu.Lock()
	r.perHost = ready
	r.mu.Unlock()
	r.validUntil.Store(earliest)
}

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
