package http

import (
	"testing"

	providerport "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/provider"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/enum"
)

func TestClassifyHTTPOutcomeRequiresExplicitNoEffectProof(t *testing.T) {
	t.Parallel()
	for _, statusCode := range []int{300, 400, 401, 409, 429, 500, 503} {
		status, effect := classifyHTTPOutcome(statusCode, "")
		if status != enum.InvocationUnknown || effect != providerport.EffectAmbiguous {
			t.Fatalf("status %d without proof = %s/%s", statusCode, status, effect)
		}
		status, effect = classifyHTTPOutcome(statusCode, "NO_EFFECT")
		if status != enum.InvocationFailed || effect != providerport.EffectNoEffect {
			t.Fatalf("status %d with NO_EFFECT = %s/%s", statusCode, status, effect)
		}
	}
	status, effect := classifyHTTPOutcome(204, "")
	if status != enum.InvocationSucceeded || effect != providerport.EffectCommitted {
		t.Fatalf("2xx outcome = %s/%s", status, effect)
	}
}
