package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestKubernetesCreditContextLossIsInfrastructure prevents cancelled requests from being interpreted as a semantic authority revoke while retaining strict equality expiry.
// TestKubernetesCreditContextLossIsInfrastructure не позволяет трактовать cancelled request как semantic authority revoke и сохраняет strict equality expiry.
func TestKubernetesCreditContextLossIsInfrastructure(t *testing.T) {
	live := time.Now().Add(time.Minute)
	owner := kubernetesOperationOwner{accessUntil: live}
	if err := owner.creditError(t.Context(), live); err != nil {
		t.Fatal("live owner credit rejected", err)
	}
	expired := owner
	expired.accessUntil = time.Now()
	if err := expired.creditError(t.Context(), live); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("expired original allowance escaped", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, candidate := range []kubernetesOperationOwner{owner, expired} {
		if err := candidate.creditError(ctx, live); !errors.Is(err, errs.ErrUnavailable) || errors.Is(err, errs.ErrForbidden) {
			t.Fatal("context loss became semantic closure", err)
		}
	}
}
