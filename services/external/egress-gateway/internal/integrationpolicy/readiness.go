package integrationpolicy

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
)

type Resolver interface {
	Resolve(context.Context, string) (dnsresolver.Snapshot, error)
	Refresh(context.Context, string) (dnsresolver.Snapshot, error)
}

// Readiness проверяет полный актуальный DNS snapshot; пустой список закрыт.
// Не влияет на готовность остальных listener и корневого Pod.
type Readiness struct {
	policy     *Active
	resolver   Resolver
	validUntil atomic.Int64
}

func NewReadiness(active *Active, resolver Resolver) *Readiness {
	return &Readiness{policy: active, resolver: resolver}
}

func (r *Readiness) Ready() (bool, string) {
	if r != nil && time.Now().UnixNano() < r.validUntil.Load() {
		return true, "ready"
	}
	return false, "integration projection is not ready"
}

func (r *Readiness) Check(ctx context.Context) {
	if r.policy == nil || !r.policy.Configured() || r.resolver == nil {
		r.validUntil.Store(0)
		return
	}
	var earliest time.Time
	for _, destination := range r.policy.Destinations() {
		snapshot, err := r.resolver.Refresh(ctx, destination.Hostname)
		if err != nil || len(snapshot.Addresses) == 0 || !time.Now().Before(snapshot.ExpiresAt) {
			r.validUntil.Store(0)
			return
		}
		for _, address := range snapshot.Addresses {
			if !r.policy.AllowsLiteral(destination.Hostname, destination.Port, address) {
				r.validUntil.Store(0)
				return
			}
		}
		if earliest.IsZero() || snapshot.ExpiresAt.Before(earliest) {
			earliest = snapshot.ExpiresAt
		}
	}
	if ctx.Err() != nil {
		r.validUntil.Store(0)
		return
	}
	r.validUntil.Store(earliest.UnixNano())
}

func (r *Readiness) Run(interval time.Duration) func(context.Context) error {
	return func(ctx context.Context) error {
		defer r.validUntil.Store(0)
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
