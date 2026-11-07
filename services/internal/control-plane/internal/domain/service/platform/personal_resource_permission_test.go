package platform

import (
	"reflect"
	"slices"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestPersonalResourcePermissionPreflight admits only an authenticated personal key with exact
// READ observations and a mandatory object check; it never persists global WRITE confirmation.
// TestPersonalResourcePermissionPreflight допускает только проверенный personal key с exact READ
// и обязательной проверкой объекта; глобальное WRITE-подтверждение не сохраняется.
func TestPersonalResourcePermissionPreflight(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		change  func(*entity.IntegrationCredentialRequirement, *entity.PersonalIntegrationCredential)
		problem entity.IntegrationCredentialProblem
		invalid bool
	}{
		{"conditional", func(*entity.IntegrationCredentialRequirement, *entity.PersonalIntegrationCredential) {}, "", false},
		{"missing identity", func(_ *entity.IntegrationCredentialRequirement, b *entity.PersonalIntegrationCredential) {
			b.VerifiedCapabilities = []string{"confluence.space.read"}
		}, entity.IntegrationCredentialUnconfirmed, false},
		{"denied identity", func(_ *entity.IntegrationCredentialRequirement, b *entity.PersonalIntegrationCredential) {
			b.DeniedCapabilities = []string{"confluence.identity.read"}
		}, entity.IntegrationCredentialInsufficient, false},
		{"denied update", func(_ *entity.IntegrationCredentialRequirement, b *entity.PersonalIntegrationCredential) {
			b.DeniedCapabilities = []string{"confluence.page.update"}
		}, entity.IntegrationCredentialInsufficient, false},
		{"revoked", func(_ *entity.IntegrationCredentialRequirement, b *entity.PersonalIntegrationCredential) {
			b.State = "REVOKED"
		}, entity.IntegrationCredentialRevoked, false},
		{"missing profile", func(r *entity.IntegrationCredentialRequirement, _ *entity.PersonalIntegrationCredential) {
			r.ResourcePermissionProfile = ""
		}, "", true},
		{"unknown profile", func(r *entity.IntegrationCredentialRequirement, _ *entity.PersonalIntegrationCredential) {
			r.ResourcePermissionProfile = "UNKNOWN"
		}, "", true},
		{"wrong operation", func(r *entity.IntegrationCredentialRequirement, _ *entity.PersonalIntegrationCredential) {
			r.CapabilityKey = "confluence.page.create"
		}, "", true},
		{"wrong definition", func(r *entity.IntegrationCredentialRequirement, _ *entity.PersonalIntegrationCredential) {
			r.DefinitionKey = "confluence"
		}, "", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			requirement, initiator, binding, now := personalCredentialFixture()
			requirement.DefinitionKey, requirement.CapabilityKey = "confluence-server-dc", "confluence.page.update"
			requirement.ResourcePermissionProfile = integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1
			binding.VerifiedCapabilities = []string{"confluence.space.read", "confluence.identity.read"}
			scenario.change(&requirement, &binding)
			original := append([]string(nil), binding.VerifiedCapabilities...)
			selection, err := ResolveIntegrationCredential(requirement, initiator, binding.Credential, &binding, now)
			if (err != nil) != scenario.invalid || selection.Problem != scenario.problem || !reflect.DeepEqual(original, binding.VerifiedCapabilities) {
				t.Fatalf("incorrect conditional preflight: problem=%s error=%v", selection.Problem, err)
			}
			if err == nil && scenario.problem == "" {
				if selection.Credential == nil || selection.ResourcePermissionProfile != requirement.ResourcePermissionProfile {
					t.Fatal("conditional admission lost personal key or mandatory consumer profile")
				}
				issues, err := PreflightIntegrationCredentials([]entity.IntegrationCredentialRequirement{requirement}, initiator, nil,
					map[string]*entity.PersonalIntegrationCredential{requirement.ConnectionRef: &binding}, now)
				if err != nil || len(issues) != 0 {
					t.Fatal("conditional requirement was rejected by full preflight")
				}
			} else if selection.Credential != nil || selection.ResourcePermissionProfile != "" {
				t.Fatal("denied requirement returned execution material")
			}
		})
	}
}

// TestPersonalObjectReadPreflight admits registered resource checks without fabricating global
// READ proof. Every known key failure and denied prerequisite still closes before launch, and an
// available shared credential never substitutes for a missing or unusable personal binding.
// TestPersonalObjectReadPreflight допускает registered resource checks без выдуманного global
// READ proof. Известные отказы ключа и prerequisite закрывают запуск; пригодный общий ключ
// никогда не заменяет отсутствующую либо непригодную personal binding.
func TestPersonalObjectReadPreflight(t *testing.T) {
	for definition, operations := range map[string][]string{
		"gitlab":               {"gitlab.repository.file.read", "gitlab.issue.read", "gitlab.merge_request.read", "gitlab.pipeline.read"},
		"jira-server-dc":       {"jira.issue.search", "jira.issue.read", "jira.issue.transition.list"},
		"confluence-server-dc": {"confluence.page.search", "confluence.page.read"},
	} {
		for _, operation := range operations {
			for _, scenario := range []string{"ready", "missing metadata", "denied metadata", "denied object", "revoked", "expired", "old profile", "foreign profile", "rotated"} {
				t.Run(operation+"/"+scenario, func(t *testing.T) {
					requirement, initiator, binding, now := personalCredentialFixture()
					contract, exists := integrationpackage.CredentialResourcePermissionContractFor(definition, operation)
					if !exists {
						t.Fatal("object read contract missing")
					}
					requirement.DefinitionKey, requirement.CapabilityKey, requirement.ResourcePermissionProfile = definition, operation, contract.Profile
					binding.VerifiedCapabilities = slices.Clone(contract.ReadCapabilities)
					want, invalid := entity.IntegrationCredentialProblem(""), false
					switch scenario {
					case "missing metadata":
						binding.VerifiedCapabilities, want = nil, entity.IntegrationCredentialUnconfirmed
					case "denied metadata":
						binding.DeniedCapabilities, want = []string{contract.ReadCapabilities[0]}, entity.IntegrationCredentialInsufficient
					case "denied object":
						binding.DeniedCapabilities, want = []string{operation}, entity.IntegrationCredentialInsufficient
					case "revoked":
						binding.State, want = "REVOKED", entity.IntegrationCredentialRevoked
					case "expired":
						binding.ExpiresAt, want = &now, entity.IntegrationCredentialExpired
					case "old profile":
						requirement.ResourcePermissionProfile, invalid = "", true
					case "foreign profile":
						requirement.ResourcePermissionProfile, invalid = integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1, true
					case "rotated":
						binding.VerifiedCredentialRef, want = "icr_old", entity.IntegrationCredentialCheckRequired
					}
					before := slices.Clone(binding.VerifiedCapabilities)
					selection, err := ResolveIntegrationCredential(requirement, initiator, binding.Credential, &binding, now)
					if (err != nil) != invalid || selection.Problem != want || !slices.Equal(before, binding.VerifiedCapabilities) || slices.Contains(binding.VerifiedCapabilities, operation) {
						t.Fatal("object read eligibility fabricated permissions or misclassified a known refusal", err)
					}
					if !invalid && want == "" {
						if selection.Credential == nil || selection.PersonalBindingRef != binding.Ref || selection.ResourcePermissionProfile != contract.Profile {
							t.Fatal("object check lost root binding or mandatory profile")
						}
					} else if selection.Credential != nil {
						t.Fatal("refused object read selected shared execution material")
					}
				})
			}
		}
	}
}

// TestJiraPersonalResourcePermissionPreflight requires exact READ proof pins for each registered
// Server write and retains known key failures without converting them into shared fallback.
// TestJiraPersonalResourcePermissionPreflight требует exact READ proof pins для каждой Server
// записи и сохраняет известные отказы ключа, не превращая их в shared fallback.
func TestJiraPersonalResourcePermissionPreflight(t *testing.T) {
	for _, capability := range []string{"jira.issue.create", "jira.issue.comment.write", "jira.issue.transition.apply"} {
		for _, scenario := range []string{"ready", "missing", "denied", "revoked", "expired", "wrong profile", "missing profile"} {
			t.Run(capability+"/"+scenario, func(t *testing.T) {
				requirement, initiator, binding, now := personalCredentialFixture()
				requirement.DefinitionKey, requirement.CapabilityKey = "jira-server-dc", capability
				contract, exists := integrationpackage.CredentialResourcePermissionContractFor(requirement.DefinitionKey, capability)
				if !exists {
					t.Fatal("registered Jira write contract missing")
				}
				requirement.ResourcePermissionProfile = contract.Profile
				binding.VerifiedCapabilities = append([]string(nil), contract.ReadCapabilities...)
				problem := entity.IntegrationCredentialProblem("")
				invalid := false
				switch scenario {
				case "missing":
					binding.VerifiedCapabilities = nil
					problem = entity.IntegrationCredentialUnconfirmed
				case "denied":
					binding.DeniedCapabilities = []string{capability}
					problem = entity.IntegrationCredentialInsufficient
				case "revoked":
					binding.State, problem = "REVOKED", entity.IntegrationCredentialRevoked
				case "expired":
					binding.ExpiresAt, problem = &now, entity.IntegrationCredentialExpired
				case "wrong profile":
					requirement.ResourcePermissionProfile, invalid = integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1, true
				case "missing profile":
					requirement.ResourcePermissionProfile, invalid = "", true
				}
				selection, err := ResolveIntegrationCredential(requirement, initiator, binding.Credential, &binding, now)
				if (err != nil) != invalid || selection.Problem != problem {
					t.Fatal("conditional Jira preflight classified the key incorrectly", err)
				}
				if !invalid && problem == "" && (selection.Credential == nil || selection.ResourcePermissionProfile != contract.Profile) {
					t.Fatal("Jira selection lost the mandatory object check")
				}
				if containsString(binding.VerifiedCapabilities, capability) {
					t.Fatal("conditional Jira preflight fabricated global WRITE")
				}
			})
		}
	}
}

// TestGitLabPersonalWritePreflight requires exact READ proof pins for each registered
// project write and retains known key failures without converting them into shared fallback.
// TestGitLabPersonalWritePreflight требует exact READ proof pins для каждой project
// записи и сохраняет известные отказы ключа, не превращая их в shared fallback.
func TestGitLabPersonalWritePreflight(t *testing.T) {
	for _, capability := range []string{"gitlab.issue.create", "gitlab.branch.create", "gitlab.merge_request.create"} {
		for _, scenario := range []string{"ready", "unobserved metadata", "missing", "denied", "revoked", "expired", "wrong profile", "missing profile"} {
			t.Run(capability+"/"+scenario, func(t *testing.T) {
				requirement, initiator, binding, now := personalCredentialFixture()
				requirement.DefinitionKey, requirement.CapabilityKey = "gitlab", capability
				contract, exists := integrationpackage.CredentialResourcePermissionContractFor(requirement.DefinitionKey, capability)
				if !exists {
					t.Fatal("registered GitLab write contract missing")
				}
				requirement.ResourcePermissionProfile = contract.Profile
				binding.VerifiedCapabilities = append([]string(nil), contract.ReadCapabilities...)
				binding.ObservedPermissionProfiles = []string{contract.RequiredObservationProfile}
				problem := entity.IntegrationCredentialProblem("")
				invalid := false
				switch scenario {
				case "unobserved metadata":
					binding.ObservedPermissionProfiles = nil
					problem = entity.IntegrationCredentialUnconfirmed
				case "missing":
					binding.VerifiedCapabilities = nil
					problem = entity.IntegrationCredentialUnconfirmed
				case "denied":
					binding.DeniedCapabilities = []string{capability}
					problem = entity.IntegrationCredentialInsufficient
				case "revoked":
					binding.State, problem = "REVOKED", entity.IntegrationCredentialRevoked
				case "expired":
					binding.ExpiresAt, problem = &now, entity.IntegrationCredentialExpired
				case "wrong profile":
					requirement.ResourcePermissionProfile, invalid = integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1, true
				case "missing profile":
					requirement.ResourcePermissionProfile, invalid = "", true
				}
				selection, err := ResolveIntegrationCredential(requirement, initiator, binding.Credential, &binding, now)
				if (err != nil) != invalid || selection.Problem != problem {
					t.Fatal("conditional GitLab preflight classified the key incorrectly", err)
				}
				if !invalid && problem == "" && (selection.Credential == nil || selection.ResourcePermissionProfile != contract.Profile) {
					t.Fatal("GitLab selection lost the mandatory object check")
				}
				if containsString(binding.VerifiedCapabilities, capability) {
					t.Fatal("conditional GitLab preflight fabricated global WRITE")
				}
			})
		}
	}
}
