package platform

import (
	"strings"
	"testing"
)

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
