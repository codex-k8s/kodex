package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
)

func testSTTCatalogAuthority(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: sttModelCatalogOperation, OwnerClaim: true}
	if authority, err := repository.ResolveProofAuthority(ctx, owner); err != nil || authority.ActorID == "" || authority.ProjectID != "" {
		t.Fatalf("catalog before configured STT: %v", err)
	}
	for _, kind := range []string{"project", "workload", "nonmember"} {
		input := owner
		switch kind {
		case "project":
			input.ProjectRef = "project-forbidden"
		case "workload":
			input.CallerWorkload = "stt-tts-service"
		case "nonmember":
			input.ExternalActorID = "20000000-0000-4000-8000-000000000099"
			input.OwnerClaim = false
		}
		if _, err := repository.ResolveProofAuthority(ctx, input); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("catalog %s authority accepted: %v", kind, err)
		}
	}
}
