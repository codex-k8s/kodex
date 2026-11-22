package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
)

// TestEmailAuthorizationCreditRetainsOriginalCeiling checks independent lease/access bounds and rejects revival of an expired stored intent even when current access becomes unbounded.
// TestEmailAuthorizationCreditRetainsOriginalCeiling проверяет независимые lease/access bounds и отказ оживлению истёкшего сохранённого intent даже при новом неограниченном access.
func TestEmailAuthorizationCreditRetainsOriginalCeiling(t *testing.T) {
	now := time.Now()
	for _, item := range []struct {
		name             string
		original, access time.Time
		want             time.Time
	}{
		{"authorization window", now.Add(time.Hour), time.Time{}, now.Add(emailpolicy.AuthorizationMaximumAge)},
		{"original lease", now.Add(time.Second), now.Add(2 * time.Second), now.Add(time.Second)},
		{"finite permission", now.Add(2 * time.Second), now.Add(time.Second), now.Add(time.Second)},
		{"restored permission", now.Add(-time.Second), time.Time{}, now.Add(-time.Second)},
	} {
		t.Run(item.name, func(t *testing.T) {
			until := emailAuthorizationUntil(now, item.original, item.access)
			if !until.Equal(item.want) {
				t.Fatal("mailbox authorization exceeded an original credit bound")
			}
			if item.name == "restored permission" && !errors.Is(emailAuthorizationCreditError(t.Context(), until), errs.ErrForbidden) {
				t.Fatal("restored access renewed an expired original intent")
			}
		})
	}
	if !emailAuthorizationUntil(now, time.Time{}, time.Time{}).IsZero() {
		t.Fatal("missing original expiry created a mailbox credit")
	}
}

// TestEmailAuthorizationCreditPreservesCancellation distinguishes an expired allowance from an infrastructure cancellation so owner callers do not terminalize incomplete evidence.
// TestEmailAuthorizationCreditPreservesCancellation отличает истёкшее allowance от infrastructure cancellation, чтобы owner caller не завершал состояние по неполному evidence.
func TestEmailAuthorizationCreditPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if !errors.Is(emailAuthorizationCreditError(ctx, time.Now().Add(-time.Second)), errs.ErrUnavailable) {
		t.Fatal("cancelled authorization became a semantic expiry")
	}
	if err := emailAuthorizationCreditError(t.Context(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal("valid mailbox credit was rejected", err)
	}
}
