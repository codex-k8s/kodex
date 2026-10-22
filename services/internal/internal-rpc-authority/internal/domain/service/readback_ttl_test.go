package service

import (
	"testing"
	"time"
)

// TestReadbackTTLWindowsCoverProjectedSecretDelivery pins strict TTL/rotation overlap inequalities
// against local Secret-projection and retry budgets; it does not measure real Kubernetes delivery
// latency.
// TestReadbackTTLWindowsCoverProjectedSecretDelivery закрепляет строгие неравенства сроков и
// перекрытия ротации относительно локальных бюджетов доставки Secret и повторов; реальную задержку
// Kubernetes не измеряет.
func TestReadbackTTLWindowsCoverProjectedSecretDelivery(t *testing.T) {
	const (
		// projectedSecretLagBudget reserves one minute for projected Secret delivery.
		// projectedSecretLagBudget отводит минуту на доставку projected Secret.
		projectedSecretLagBudget = time.Minute
		// startupRetryBudget reserves one minute for the fixture's startup retries.
		// startupRetryBudget отводит минуту на начальные повторы в проверке.
		startupRetryBudget = time.Minute
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
