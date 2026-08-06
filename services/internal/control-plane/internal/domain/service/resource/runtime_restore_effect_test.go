package resource

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
)

func TestValidateRuntimeRestoreEffectAuthorityRejectsRevokedOrStaleGeneration(t *testing.T) {
	digest := strings.Repeat("a", 64)
	execution := RuntimeExecution{ID: "11111111-1111-4111-8111-111111111111", State: "ADMITTED",
		RestoreOperationID:         "22222222-2222-4222-8222-222222222222",
		RestoreOperationGeneration: 3, RestoreSourceAuthoritySHA256: digest}
	operation := domainrepo.RuntimeRestoreOperation{ID: execution.RestoreOperationID,
		TargetExecutionID: execution.ID, Generation: 3, ConsumedGeneration: 3,
		RevokedGeneration: 2, SourceAuthoritySHA256: digest}
	input := RuntimeRestoreEffectInput{RestoreOperationID: execution.RestoreOperationID,
		RestoreOperationGeneration: 3, RestoreSourceAuthoritySHA256: digest,
		Effect: "S3_CREDENTIAL"}
	if err := validateRuntimeRestoreEffectAuthority(execution, operation, input); err != nil {
		t.Fatalf("current generation rejected: %v", err)
	}

	revoked := operation
	revoked.RevokedGeneration = revoked.Generation
	if err := validateRuntimeRestoreEffectAuthority(execution, revoked, input); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("revoked generation was not rejected: %v", err)
	}

	stale := input
	stale.RestoreOperationGeneration--
	if err := validateRuntimeRestoreEffectAuthority(execution, operation, stale); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("stale generation was not rejected: %v", err)
	}
}
