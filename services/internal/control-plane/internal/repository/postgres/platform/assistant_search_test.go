package platform

import (
	"strings"
	"testing"
)

// TestAssistantSearchExtraKeepsTenantScopeAndSecretMetadataOnly checks exact counts of
// tenant/project-lifecycle SQL guards and forbids sensitive
// description/credential/public-configuration source fields. It inspects embedded query text only,
// not database visibility or a live search result.
// TestAssistantSearchExtraKeepsTenantScopeAndSecretMetadataOnly проверяет точное число
// tenant/project-lifecycle SQL guards и запрещает sensitive
// description/credential/public-configuration source fields. Анализирует лишь embedded query text,
// а не DB visibility и live search result.
func TestAssistantSearchExtraKeepsTenantScopeAndSecretMetadataOnly(t *testing.T) {
	t.Parallel()
	if strings.Count(queryAssistantSearchExtra, "organization_id = @organization_id::uuid") != 5 ||
		strings.Count(queryAssistantSearchExtra, "project.lifecycle NOT IN ('TRASHED', 'PURGE_PENDING')") != 4 {
		t.Fatal("assistant search query lost tenant or project lifecycle scope")
	}
	for _, forbidden := range []string{"secret.description", "runtime_secret_revisions", "credential_materialization_ref", "public_configuration"} {
		if strings.Contains(queryAssistantSearchExtra, forbidden) {
			t.Fatalf("assistant search query projects sensitive field %q", forbidden)
		}
	}
}

// TestAssistantIntegrationCatalogOnlyReturnsEnabledPublicMetadata checks enabled-only bounded
// integration-definition selection and absence of credential/secret/connection-data SQL fields.
// This static string guard does not query a catalog or prove runtime row authorization.
// TestAssistantIntegrationCatalogOnlyReturnsEnabledPublicMetadata проверяет enabled-only bounded
// integration-definition selection и отсутствие credential/secret/connection-data SQL fields.
// Static string guard не читает каталог и не доказывает runtime row authorization.
func TestAssistantIntegrationCatalogOnlyReturnsEnabledPublicMetadata(t *testing.T) {
	t.Parallel()
	if !strings.Contains(queryAssistantIntegrationDefinitions, "WHERE enabled") ||
		!strings.Contains(queryAssistantIntegrationDefinitions, "LIMIT @limit OFFSET @offset") {
		t.Fatal("assistant integration catalog lost bounded enabled-only selection")
	}
	for _, forbidden := range []string{"credential_materialization_ref", "secret_value", "integration_credential_revisions", "public_configuration"} {
		if strings.Contains(queryAssistantIntegrationDefinitions, forbidden) {
			t.Fatalf("assistant integration catalog projects secret or connection data %q", forbidden)
		}
	}
}
