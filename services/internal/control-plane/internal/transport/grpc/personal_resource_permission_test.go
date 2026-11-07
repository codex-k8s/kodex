package grpc

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// TestPersonalResourcePermissionClaimCaster preserves exact typed profiles and keeps malformed
// owner metadata invalid instead of silently removing a mandatory consumer check.
// TestPersonalResourcePermissionClaimCaster сохраняет exact typed profiles и оставляет malformed
// owner metadata неверными вместо молчаливого удаления обязательной проверки consumer.
func TestPersonalResourcePermissionClaimCaster(t *testing.T) {
	for _, scenario := range []struct {
		input string
		want  cp.IntegrationCredentialResourcePermissionProfile
	}{
		{"", 0},
		{integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1, cp.IntegrationCredentialResourcePermissionProfile_INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_CONFLUENCE_SERVER_PAGE_UPDATE_V1},
		{"UNKNOWN", -1},
		{"UNSPECIFIED", -1},
	} {
		claim := CastIntegrationInvocationClaim(map[string]any{"resourcePermissionProfile": scenario.input, "credentialAdmissionRequired": true})
		if claim.GetResourcePermissionProfile() != scenario.want || !claim.GetCredentialAdmissionRequired() {
			t.Fatal("owner claim lost its personal resource permission boundary")
		}
	}
}
