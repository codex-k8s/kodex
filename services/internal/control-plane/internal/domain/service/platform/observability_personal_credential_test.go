package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestObservabilityPersonalCredentialPreflight preserves root-user ownership, current key/configuration pins and known-deny metadata for each whole-endpoint connection; a valid shared key never repairs a missing personal key.
// TestObservabilityPersonalCredentialPreflight сохраняет root-user ownership, текущие key/configuration pins и known-deny metadata каждого whole-endpoint подключения; пригодный общий ключ не заменяет отсутствующий личный.
func TestObservabilityPersonalCredentialPreflight(t *testing.T) {
	for _, key := range []string{"prometheus", "thanos"} {
		t.Run(key, func(t *testing.T) {
			for _, scenario := range []string{"ready", "known-denied", "foreign-user", "foreign-project", "revoked", "expired", "rotated", "changed-host", "missing-personal", "missing-profile"} {
				t.Run(scenario, func(t *testing.T) {
					requirement, initiator, binding, now := personalCredentialFixture()
					requirement.DefinitionKey, requirement.CapabilityKey, requirement.ResourcePermissionProfile = key, key+".query.instant", integrationpackage.CredentialResourcePermissionObservabilityReadV1
					binding.VerifiedCapabilities = []string{key + ".connection.read"}
					shared := *binding.Credential
					personal := &binding
					expected := entity.IntegrationCredentialProblem("")
					forbidden, invalid := false, false
					switch scenario {
					case "known-denied":
						binding.DeniedCapabilities = []string{requirement.CapabilityKey}
						expected = entity.IntegrationCredentialInsufficient
					case "foreign-user":
						binding.SubjectID = "usr_other"
						forbidden = true
					case "foreign-project":
						binding.ConnectionRef = "int_other"
						forbidden = true
					case "revoked":
						binding.State = "REVOKED"
						expected = entity.IntegrationCredentialRevoked
					case "expired":
						binding.ExpiresAt = &now
						expected = entity.IntegrationCredentialExpired
					case "rotated":
						binding.Credential.Ref = "icr_other"
						expected = entity.IntegrationCredentialCheckRequired
					case "changed-host":
						binding.VerifiedConfigurationDigest = ""
						expected = entity.IntegrationCredentialCheckRequired
					case "missing-personal":
						personal = nil
						expected = entity.IntegrationCredentialMissing
					case "missing-profile":
						requirement.ResourcePermissionProfile = ""
						invalid = true
					}
					selected, err := ResolveIntegrationCredential(requirement, initiator, &shared, personal, now)
					if forbidden {
						if !errors.Is(err, errs.ErrForbidden) {
							t.Fatal("foreign owner admitted", err)
						}
						return
					}
					if invalid {
						if !errors.Is(err, errs.ErrInvalid) {
							t.Fatal("missing typed observability profile admitted", err)
						}
						return
					}
					if err != nil || selected.Problem != expected {
						t.Fatalf("observability credential classification mismatch: %v %v", err, selected.Problem)
					}
					if expected != "" && selected.Credential != nil {
						t.Fatal("failed personal selection fell back to shared key")
					}
					if expected == "" && (selected.Credential == nil || selected.Credential.Ref != binding.Credential.Ref || selected.BindingGeneration != binding.Generation) {
						t.Fatal("observability personal selection lost exact current lineage")
					}
				})
			}
		})
	}
}
