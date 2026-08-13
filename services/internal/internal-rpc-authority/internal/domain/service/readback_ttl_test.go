package service

import (
	"testing"
	"time"
)

func TestReadbackTTLWindowsCoverProjectedSecretDelivery(t *testing.T) {
	const (
		projectedSecretLagBudget = time.Minute
		startupRetryBudget       = time.Minute
	)
	if readbackCredentialTTL <= projectedSecretLagBudget+startupRetryBudget {
		t.Fatalf(
			"readback credential TTL %s does not cover Secret lag and startup retry budgets",
			readbackCredentialTTL,
		)
	}
	if publishedReadbackIntentTTL <= readbackCredentialTTL+readbackChallengeTTL {
		t.Fatalf(
			"readback intent TTL %s does not cover credential and challenge TTLs",
			publishedReadbackIntentTTL,
		)
	}
	if readbackCredentialTTL-readbackMaterialRotationInterval <=
		projectedSecretLagBudget+startupRetryBudget {
		t.Fatalf(
			"readback material overlap %s does not cover Secret lag and startup retry budgets",
			readbackCredentialTTL-readbackMaterialRotationInterval,
		)
	}
}
