package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestKubernetesInputFirstCreditRejectsExpiredRecovery protects all Claim dispositions without refreshing a resolved bound.
// TestKubernetesInputFirstCreditRejectsExpiredRecovery защищает все Claim dispositions без обновления resolved bound.
func TestKubernetesInputFirstCreditRejectsExpiredRecovery(t *testing.T) {
	for _, until := range []time.Time{{}, time.Now().Add(-time.Second)} {
		if err := kubernetesInputClaimCreditError(t.Context(), until); err == nil {
			t.Fatal("missing or expired first horizon admitted")
		}
	}
	until := time.Now().Add(time.Minute)
	if err := kubernetesInputClaimCreditError(t.Context(), until); err != nil {
		t.Fatal("live first horizon rejected", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := kubernetesInputClaimCreditError(ctx, until); !errors.Is(err, errs.ErrUnavailable) || errors.Is(err, errs.ErrForbidden) {
		t.Fatal("cancelled request became semantic revoke", err)
	}
}
