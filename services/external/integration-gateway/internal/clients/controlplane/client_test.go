package controlplane

import (
	"testing"

	domainservice "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/service/gateway"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
)

func TestConnectionSnapshotIDBindsExactAuthorityAndCredentialSet(t *testing.T) {
	t.Parallel()
	authority := domainservice.Authority{
		TenantID: "tenant", ProjectID: "project", RuntimeRevisionID: "runtime",
		RuntimeRevisionVersion: 3, RuntimeRevisionDigest: "runtime-digest",
		RuntimeManifestDigest: "manifest-digest", GrantGeneration: 5,
	}
	connection := entity.Connection{
		IntegrationID: "integration", IntegrationVersion: 7, IntegrationDigest: "integration-digest",
		DefinitionID: "definition", DefinitionVersion: 2, EndpointRef: "provider",
		CredentialBindingRefs: []entity.CredentialBinding{
			{ID: "b", Version: 2, Revision: 3, ProjectionDigest: "b-digest", Purpose: "second", SecretRef: "b", PrincipalRef: "b-principal"},
			{ID: "a", Version: 1, Revision: 2, ProjectionDigest: "a-digest", Purpose: "first", SecretRef: "a", PrincipalRef: "a-principal"},
		},
	}
	first := connectionSnapshotID(authority, connection)
	connection.CredentialBindingRefs[0], connection.CredentialBindingRefs[1] =
		connection.CredentialBindingRefs[1], connection.CredentialBindingRefs[0]
	if first != connectionSnapshotID(authority, connection) {
		t.Fatal("connection snapshot ID depends on credential order")
	}
	connection.CredentialBindingRefs[0].ProjectionDigest = "changed-digest"
	if first == connectionSnapshotID(authority, connection) {
		t.Fatal("connection snapshot ID does not bind credential projection")
	}
}
