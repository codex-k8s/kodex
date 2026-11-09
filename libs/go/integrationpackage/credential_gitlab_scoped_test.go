package integrationpackage

import "testing"

// TestGitLabScopedPersonalContracts requires a closed per-call consumer for each selectable operation and never promotes scope health to a global WRITE observation.
// TestGitLabScopedPersonalContracts требует closed per-call consumer для каждой selectable operation и не превращает scope health в global WRITE observation.
func TestGitLabScopedPersonalContracts(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions["gitlab"]
	if len(definition.Spec.Capabilities) != 40 {
		t.Fatal("scoped GitLab operation coverage changed")
	}
	for _, capability := range definition.Spec.Capabilities {
		profile := CredentialResourcePermissionProfile(definition, capability)
		if capability.Operation == "gitlab.scope.health.read" {
			if profile != "" {
				t.Fatal("health acquired an object permission profile")
			}
			continue
		}
		contract, exists := CredentialResourcePermissionContractForProfile("gitlab", capability.Key, profile)
		if profile != CredentialResourcePermissionGitLabScopedV2 || !exists || contract.Risk != capability.Risk || contract.ResourceKind != "GITLAB_SCOPE" ||
			len(contract.ReadCapabilities) != 1 || contract.ReadCapabilities[0] != "gitlab.scope.health.read" || contract.RequiredObservationProfile != "" {
			t.Fatalf("missing scoped per-call credential contract: %s", capability.Key)
		}
		changed := capability
		changed.Risk = "UNKNOWN"
		if CredentialResourcePermissionProfile(definition, changed) != "" {
			t.Fatal("unknown risk inherited credential authority")
		}
		contract.ReadCapabilities[0] = "foreign.read"
		fresh, _ := CredentialResourcePermissionContractForProfile("gitlab", capability.Key, profile)
		if fresh.ReadCapabilities[0] != "gitlab.scope.health.read" {
			t.Fatal("scoped consumer contracts share mutable state")
		}
	}
	profiles, keys := CredentialObservationRequests(definition)
	if len(profiles) != 0 || len(keys) != 0 {
		t.Fatal("scoped health requested a single-project global write observation")
	}
	if _, exists := CredentialResourcePermissionContractForProfile("gitlab", "gitlab.any.http", CredentialResourcePermissionGitLabScopedV2); exists {
		t.Fatal("arbitrary operation acquired a credential consumer")
	}
}
