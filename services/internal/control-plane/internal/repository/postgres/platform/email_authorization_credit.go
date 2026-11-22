package platform

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
)

// emailAuthorizationUntil intersects the original mailbox binding and currently effective access allowance with the existing authorization window. Durations attach to the observation's monotonic clock; zero access means a currently unbounded winning alternative, never a renewed stored intent.
// emailAuthorizationUntil пересекает исходный mailbox binding и текущее access allowance с существующим окном авторизации. Duration привязывается к monotonic clock наблюдения; zero access означает текущую неограниченную winning alternative, но не продление сохранённого intent.
func emailAuthorizationUntil(observed, originalExpiry, accessUntil time.Time) time.Time {
	if observed.IsZero() || originalExpiry.IsZero() {
		return time.Time{}
	}
	until := observed.Add(emailpolicy.AuthorizationMaximumAge)
	for _, bound := range []time.Time{originalExpiry, accessUntil} {
		if !bound.IsZero() && bound.Before(until) {
			until = observed.Add(bound.Sub(observed))
		}
	}
	return until
}

// emailAuthorizationCreditError fences only new dispatch credit, preserving cancellation as infrastructure failure. Historical admitted receipt reporting has a separate exact-source maintenance path and does not call this gate.
// emailAuthorizationCreditError ограждает только новый dispatch credit, сохраняя cancellation как infrastructure failure. Исторические admitted receipt докладываются отдельным exact-source maintenance path без этого допуска.
func emailAuthorizationCreditError(ctx context.Context, until time.Time) error {
	if ctx.Err() != nil {
		return errs.ErrUnavailable
	}
	if until.IsZero() || !time.Now().Before(until) {
		return errs.ErrForbidden
	}
	return nil
}
