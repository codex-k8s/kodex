package resource

import (
	"context"
	"strings"
	"testing"

	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

type diagnosticsRepository struct {
	domainrepo.Repository
	scope domainrepo.Scope
}

func (repository *diagnosticsRepository) Diagnostics(_ context.Context, scope domainrepo.Scope) (domainrepo.Diagnostics, error) {
	repository.scope = scope
	return domainrepo.Diagnostics{SchemaVersion: 7}, nil
}

func TestDiagnosticsAllowsVerifiedTenantScopeWithoutProject(t *testing.T) {
	t.Parallel()
	repository := &diagnosticsRepository{}
	service := &Service{repository: repository}
	principal := value.Principal{
		ActorID: uuid.NewString(), OrganizationID: uuid.NewString(),
		Permission: permissionDiagnostics, CorrelationID: uuid.NewString(),
		PolicyRevision: 31, AuthorityGeneration: 1,
		CallerWorkload:  "control-api-gateway",
		CallerSPIFFEID:  "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway",
		AuthoritySource: "OIDC_SESSION", AuthorityReference: uuid.NewString(),
		AuthorityRevision: 1, AuthorityDigest: strings.Repeat("a", 64),
	}
	result, err := service.Diagnostics(context.Background(), DiagnosticsInput{Principal: principal})
	if err != nil || result.SchemaVersion != 7 {
		t.Fatalf("tenant-scoped diagnostics rejected: result=%+v err=%v", result, err)
	}
	if repository.scope.OrganizationID != principal.OrganizationID ||
		repository.scope.ActorID != principal.ActorID || repository.scope.ProjectID != "" {
		t.Fatalf("diagnostics scope changed: %+v", repository.scope)
	}
}
