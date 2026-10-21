package mailpolicy

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
)

// minimumRefreshDelay prevents a short TTL from creating a tight refresh loop.
// minimumRefreshDelay не даёт короткому TTL превратить обновление в тесный цикл.
const minimumRefreshDelay = 100 * time.Millisecond

// Readiness tracks the expiry of exact mail DNS pins without affecting other listeners.
// Readiness отслеживает срок точных почтовых DNS-pins, не затрагивая другие listeners.
type Readiness struct {
	policy     *MailActive
	resolver   ReadinessResolver
	validUntil atomic.Int64
}

// ReadinessResolver can refresh authoritative DNS snapshots for readiness.
// ReadinessResolver обновляет авторитетные снимки DNS для проверки готовности.
type ReadinessResolver interface {
	Resolver
	// Refresh obtains a fresh authoritative snapshot for a mail hostname.
	// Refresh получает свежий авторитетный снимок для почтового узла.
	Refresh(context.Context, string) (dnsresolver.Snapshot, error)
}

// NewReadiness binds one mail projection to its DNS resolver.
// NewReadiness связывает одну почтовую проекцию с её DNS-resolver.
func NewReadiness(active *MailActive, resolver ReadinessResolver) *Readiness {
	return &Readiness{policy: active, resolver: resolver}
}

// Ready expires the mail listener when its last validated DNS snapshot expires.
// Ready снимает готовность почты после истечения последнего проверенного DNS-снимка.
func (r *Readiness) Ready() (bool, string) {
	if r != nil && time.Now().UnixNano() < r.validUntil.Load() {
		return true, "ready"
	}
	return false, "mail projection is not ready"
}

// Check requires every configured mail destination and DNS answer to remain pinned.
// Check требует действительных pins для каждого почтового назначения и DNS-ответа.
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

// nextRefreshDelay schedules a bounded refresh before the current pin expires.
// nextRefreshDelay назначает ограниченное обновление до истечения текущего pin.
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

// Run repeatedly refreshes readiness and revokes it when the worker exits.
// Run периодически обновляет готовность и отзывает её при выходе worker.
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
