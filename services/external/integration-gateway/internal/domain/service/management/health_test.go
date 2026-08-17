package management

import (
	"testing"

	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	"github.com/google/uuid"
)

func TestHealthScopeAllowsVerifiedTenantWithoutProject(t *testing.T) {
	t.Parallel()
	scope := domainrepo.Scope{TenantID: uuid.NewString(), ActorID: uuid.NewString()}
	if !validHealthScope(scope) {
		t.Fatal("tenant-scoped health authority was rejected")
	}
	if validScope(scope) {
		t.Fatal("regular integration operation accepted missing project")
	}
}
