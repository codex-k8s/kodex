package platform

import (
	"context"
	"testing"

	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

// assertKubernetesPreBeginRequirements proves that a visible actual Connection supplies safe
// export requirements before any setup intent, private upload hash or binding exists. The metadata
// comes from the same scoped read paths used by personal and manager UI, never a payload target.
// assertKubernetesPreBeginRequirements доказывает, что visible actual Connection выдаёт safe export
// requirements до setup intent, upload hash либо binding. Metadata поступают через scoped personal
// и manager read paths, не из payload target.
func assertKubernetesPreBeginRequirements(t *testing.T, ctx context.Context, fixture kubernetesSetupOwnerFixture, service *platformservice.Service, connection entity.IntegrationConnection) {
	t.Helper()
	expected := fixture.candidate.Targets[0].Target
	principal := fixture.principal
	principal.Permission = "platform.integrations.get"
	addressed, err := fixture.repository.GetIntegrationConnection(ctx, principal, connection.Ref)
	if err != nil || addressed.KubernetesSetupRequirements == nil {
		t.Fatal("scoped Connection lacked pre-Begin export requirements", err)
	}
	requirements := addressed.KubernetesSetupRequirements
	if !requirements.Valid() || requirements.ConnectionVersion != connection.Version || requirements.Policy != connection.CredentialPolicy || requirements.TargetRef != expected.Ref || requirements.TargetRevision != expected.Revision || requirements.CanonicalContext != expected.EmbeddedContext {
		t.Fatal("pre-Begin requirements borrowed another source")
	}
	principal.Permission = "platform.integrations.list"
	listed, _, err := fixture.repository.ListIntegrationConnections(ctx, principal, query.Filter{Page: query.Page{Size: 100}})
	if err != nil {
		t.Fatal("read manager pre-Begin catalog", err)
	}
	managerFound := false
	for _, item := range listed {
		if item.Ref == connection.Ref {
			managerFound = true
			if item.KubernetesSetupRequirements == nil || *item.KubernetesSetupRequirements != *requirements {
				t.Fatal("manager catalog lost pre-Begin requirements")
			}
		}
	}
	if !managerFound {
		t.Fatal("manager catalog omitted pre-Begin Connection")
	}
	caller := fixture.transport
	caller.Permission = "platform.personal-integrations.list"
	items, _, err := service.ListPersonalIntegrationConnections(ctx, caller, query.PersonalIntegrationConnectionFilter{Page: query.Page{Size: 50}})
	if err != nil {
		t.Fatal("read personal pre-Begin catalog", err)
	}
	found := false
	for _, item := range items {
		if item.Ref == connection.Ref {
			found = true
			if item.Credential != nil || item.KubernetesSetupRequirements == nil || *item.KubernetesSetupRequirements != *requirements {
				t.Fatal("personal catalog needed a prior binding or changed setup requirements")
			}
		}
	}
	if !found {
		t.Fatal("personal catalog omitted eligible pre-Begin Connection")
	}
	var setups int
	if fixture.repository.pool.QueryRow(ctx, "SELECT count(*) FROM control_plane.kubernetes_identity_setups WHERE organization_id=$1::uuid", fixture.current.organizationID).Scan(&setups) != nil || setups != 0 {
		t.Fatal("requirements read created a setup intent")
	}
}
