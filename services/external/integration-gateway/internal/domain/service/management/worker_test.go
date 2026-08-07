package management

import (
	"errors"
	"testing"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/managementeffect"
)

func TestSingleDispatchEffectClosesReclaimedExternalWork(t *testing.T) {
	for _, kind := range []string{
		"PROVIDER_AUTHORIZE",
		"PROVIDER_REFERENCE_SYNC",
		"PROVIDER_REVOKE",
		"PROVIDER_POOL_SYNC",
		"INTEGRATION_TEST",
		"GIT_APPLY",
	} {
		if !singleDispatchEffect(kind) {
			t.Fatalf("external effect %s can be dispatched after a reclaimed lease", kind)
		}
	}
	if singleDispatchEffect("GIT_FETCH") {
		t.Fatal("read-only Git fetch was classified as an ambiguous mutation")
	}
}

func TestEffectFailureStatusPreservesAmbiguousOutcome(t *testing.T) {
	if got := effectFailureStatus(errors.Join(managementeffect.ErrOutcomeUnknown, errors.New("transport failed"))); got != "UNKNOWN" {
		t.Fatalf("ambiguous dispatch classified as %s", got)
	}
	if got := effectFailureStatus(errors.New("request validation failed")); got != "FAILED" {
		t.Fatalf("pre-dispatch failure classified as %s", got)
	}
}

func TestClosedFailureCategoryDoesNotReflectDiagnostics(t *testing.T) {
	secretBearing := errors.New("provider rejected bearer super-secret-value")
	if got := closedFailureCategory(secretBearing); got != "PROTOCOL_ERROR" {
		t.Fatalf("unbounded provider diagnostic escaped taxonomy: %s", got)
	}
}
