package mailpolicy

import (
	"context"
	"sync/atomic"
	"time"
)

// Readiness не расширяет pins и не влияет на независимые HTTPS/STT listeners.
type Readiness struct {
	policy     *MailActive
	resolver   Resolver
	validUntil atomic.Int64
}

func NewReadiness(active *MailActive, resolver Resolver) *Readiness {
	return &Readiness{policy: active, resolver: resolver}
}

func (r *Readiness) Ready() (bool, string) {
	if r != nil && time.Now().UnixNano() < r.validUntil.Load() {
		return true, "ready"
	}
	return false, "mail projection is not ready"
}

func (r *Readiness) Check(ctx context.Context) {
	r.validUntil.Store(0)
	if r.policy == nil || !r.policy.Configured() || r.resolver == nil {
		return
	}
	var earliest time.Time
	for _, destination := range r.policy.Destinations() {
		snapshot, err := r.resolver.Resolve(ctx, destination.Hostname)
		if err != nil || len(snapshot.Addresses) == 0 || !time.Now().Before(snapshot.ExpiresAt) {
			return
		}
		for _, address := range snapshot.Addresses {
			if !r.policy.AllowsLiteral(destination.Hostname, destination.Port, address) {
				return
			}
		}
		if earliest.IsZero() || snapshot.ExpiresAt.Before(earliest) {
			earliest = snapshot.ExpiresAt
		}
	}
	if ctx.Err() == nil {
		r.validUntil.Store(earliest.UnixNano())
	}
}

func (r *Readiness) Run(interval time.Duration) func(context.Context) error {
	return func(ctx context.Context) error {
		defer r.validUntil.Store(0)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			r.Check(ctx)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}
