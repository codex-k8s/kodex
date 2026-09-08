package mailpolicy

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

const minimumRefreshDelay = 100 * time.Millisecond

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
	if r.policy == nil || !r.policy.Configured() || r.resolver == nil {
		r.validUntil.Store(0)
		return
	}
	var earliest time.Time
	for _, destination := range r.policy.Destinations() {
		snapshot, err := r.resolver.Resolve(ctx, destination.Hostname)
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

func (r *Readiness) nextRefreshDelay(maximum time.Duration) time.Duration {
	remaining := time.Until(time.Unix(0, r.validUntil.Load()))
	if remaining <= 0 {
		return maximum
	}
	floor := minimumRefreshDelay
	if maximum < floor {
		floor = maximum
	}
	delay := remaining / 2
	if delay > maximum {
		delay = maximum
	}
	if delay < floor {
		delay = floor
	}
	return delay
}

func (r *Readiness) Run(interval time.Duration) func(context.Context) error {
	return func(ctx context.Context) error {
		defer r.validUntil.Store(0)
		if interval <= 0 {
			return errors.New("mail readiness refresh interval is invalid")
		}
		for {
			r.Check(ctx)
			timer := time.NewTimer(r.nextRefreshDelay(interval))
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
}
