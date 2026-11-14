package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestLokiPersonalCredentialPreflight binds the conditional ruler profile to the initiating user's exact current verified key; the owner grants no global WRITE proof and a shared service key never substitutes a missing personal binding.
// TestLokiPersonalCredentialPreflight связывает conditional ruler profile с exact current verified key инициирующего пользователя; owner не выдаёт global WRITE proof, shared service key не подменяет missing personal binding.
func TestLokiPersonalCredentialPreflight(t *testing.T) {
	for _, scenario := range []string{"ready", "foreign-user", "revoked", "rotated", "missing-personal", "missing-health", "known-denied", "missing-profile"} {
		t.Run(scenario, func(t *testing.T) {
			requirement, initiator, binding, now := personalCredentialFixture()
			requirement.DefinitionKey = "loki-ruler-namespace"
			requirement.CapabilityKey = "loki.ruler.group.replace"
			requirement.ResourcePermissionProfile = integrationpackage.CredentialResourcePermissionLokiRulerMutationV1
			binding.VerifiedCapabilities = []string{"loki.connection.read"}
			binding.ObservedPermissionProfiles = nil
			shared := *binding.Credential
			personal := &binding
			expected := entity.IntegrationCredentialProblem("")
			switch scenario {
			case "foreign-user":
				binding.SubjectID = "usr_other"
			case "revoked":
				binding.State = "REVOKED"
				expected = entity.IntegrationCredentialRevoked
			case "rotated":
				binding.Credential.Ref = "icr_other"
				expected = entity.IntegrationCredentialCheckRequired
			case "missing-personal":
				personal = nil
				expected = entity.IntegrationCredentialMissing
			case "missing-health":
				binding.VerifiedCapabilities = nil
				expected = entity.IntegrationCredentialUnconfirmed
			case "known-denied":
				binding.DeniedCapabilities = []string{requirement.CapabilityKey}
				expected = entity.IntegrationCredentialInsufficient
			case "missing-profile":
				requirement.ResourcePermissionProfile = ""
			}
			selected, err := ResolveIntegrationCredential(requirement, initiator, &shared, personal, now)
			if scenario == "foreign-user" {
				if !errors.Is(err, errs.ErrForbidden) {
					t.Fatal("foreign Loki user admitted", err)
				}
				return
			}
			if scenario == "missing-profile" {
				if !errors.Is(err, errs.ErrInvalid) {
					t.Fatal("untyped Loki mutation admitted", err)
				}
				return
			}
			if err != nil || selected.Problem != expected || expected != "" && selected.Credential != nil {
				t.Fatal("Loki personal key boundary failed", err, selected.Problem)
			}
			if scenario == "ready" && (selected.Credential == nil || selected.ResourcePermissionProfile != requirement.ResourcePermissionProfile || selected.BindingGeneration != binding.Generation) {
				t.Fatal("Loki selection lost exact conditional profile")
			}
		})
	}
}
