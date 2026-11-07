package integrationpackage

import "testing"

// TestCredentialObjectReadContracts checks the exact shipped minimal READ surface independently
// of the registry helper. Risk/policy/scope/adapter mutation cannot retain conditional admission.
// TestCredentialObjectReadContracts проверяет точный минимальный shipped READ surface независимо
// от registry helper. Смена risk/policy/scope/adapter не сохраняет условный допуск.
func TestCredentialObjectReadContracts(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for key, operations := range map[string][]string{
		"gitlab":               {"gitlab.repository.file.read", "gitlab.issue.read", "gitlab.merge_request.read", "gitlab.pipeline.read"},
		"jira-server-dc":       {"jira.issue.search", "jira.issue.read", "jira.issue.transition.list"},
		"confluence-server-dc": {"confluence.page.search", "confluence.page.read"},
	} {
		definition := definitions[key]
		for _, operation := range operations {
			capability, exists := definition.Capability(operation)
			if !exists || CredentialResourcePermissionProfile(definition, capability) == "" {
				t.Fatalf("shipped object read is missing: %s/%s", key, operation)
			}
			contract, exists := CredentialResourcePermissionContractFor(key, operation)
			if !exists || contract.Operation != operation || contract.Risk != "READ" || contract.ApprovalPolicy != "NONE" || len(contract.ReadCapabilities) == 0 {
				t.Fatal("object read lost its scoped consumer contract")
			}
			for _, change := range []func(*Package, *Capability){
				func(_ *Package, c *Capability) { c.Risk = "WRITE" },
				func(_ *Package, c *Capability) { c.ApprovalPolicy = "HUMAN_EACH_EFFECT" },
				func(_ *Package, c *Capability) { c.ResourceScope.Kind = "OTHER" },
				func(p *Package, _ *Capability) { p.Spec.Adapter = "OTHER" },
				func(_ *Package, c *Capability) { c.Operation = "other.read" },
			} {
				copyDefinition, copyCapability := definition, capability
				change(&copyDefinition, &copyCapability)
				if CredentialResourcePermissionProfile(copyDefinition, copyCapability) != "" {
					t.Fatal("changed object-read contract retained admission")
				}
			}
			contract.ReadCapabilities[0] = "foreign.read"
			fresh, _ := CredentialResourcePermissionContractFor(key, operation)
			if fresh.ReadCapabilities[0] == "foreign.read" {
				t.Fatal("caller mutated another consumer's permission prerequisites")
			}
		}
	}
	for _, key := range []string{"jira", "confluence", "unknown"} {
		for _, operation := range []string{"jira.issue.read", "confluence.page.read", "gitlab.repository.file.read"} {
			if _, exists := CredentialResourcePermissionContractFor(key, operation); exists {
				t.Fatal("Cloud or unknown definition inherited a Server object check")
			}
		}
	}
}
