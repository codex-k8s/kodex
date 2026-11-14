package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestGrafanaPersonalCredentialPreflight preserves root-user ownership, current key/configuration pins and known-deny metadata for each explicit organization connection; a valid shared key never repairs a missing personal key.
// TestGrafanaPersonalCredentialPreflight сохраняет root-user ownership, текущие key/configuration pins и known-deny metadata каждого явного organization подключения; пригодный общий ключ не заменяет отсутствующий личный.
func TestGrafanaPersonalCredentialPreflight(t *testing.T) {
	for _, key := range []string{"grafana-organization", "grafana-dashboard-annotations", "grafana-organization-rule-delete"} {
		t.Run(key, func(t *testing.T) {
			for _, scenario := range []string{"ready", "no-scope-observation", "known-readonly", "foreign-user", "foreign-project", "revoked", "expired", "rotated", "changed-host", "missing-personal", "missing-profile"} {
				t.Run(scenario, func(t *testing.T) {
					requirement, initiator, binding, now := personalCredentialFixture()
					requirement.DefinitionKey, requirement.CapabilityKey, requirement.ResourcePermissionProfile = key, "grafana.annotation.update", integrationpackage.CredentialResourcePermissionGrafanaWriteV1
					if key == "grafana-organization-rule-delete" {
						requirement.CapabilityKey = "grafana.rule.delete"
					}
					binding.VerifiedCapabilities = []string{"grafana.connection.read"}
					binding.ObservedPermissionProfiles = []string{integrationpackage.CredentialObservationGrafanaPermissionsV1}
					shared := *binding.Credential
					personal := &binding
					expected := entity.IntegrationCredentialProblem("")
					forbidden, invalid := false, false
					switch scenario {
					case "no-scope-observation":
						binding.ObservedPermissionProfiles = nil
						expected = entity.IntegrationCredentialUnconfirmed
					case "known-readonly":
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
							t.Fatal("missing typed Grafana profile admitted", err)
						}
						return
					}
					if err != nil || selected.Problem != expected {
						t.Fatalf("Grafana credential classification mismatch: %v %v", err, selected.Problem)
					}
					if expected != "" && selected.Credential != nil {
						t.Fatal("failed personal selection fell back to shared key")
					}
					if expected == "" && (selected.Credential == nil || selected.Credential.Ref != binding.Credential.Ref || selected.BindingGeneration != binding.Generation) {
						t.Fatal("Grafana personal selection lost exact current lineage")
					}
				})
			}
		})
	}
}
