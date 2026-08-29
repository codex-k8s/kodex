package access

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestEvaluateRestrictsAgentAndProjectInstance(t *testing.T) {
	subject := entity.AccessSubject{Ref: "usr_alice", Kind: "USER", Active: true}
	binding := entity.AccessBinding{
		Ref: "abnd_agent_a", State: "ACTIVE", Subject: subject,
		RoleVersion: entity.AccessRoleVersion{Ref: "arv_launcher", RoleRef: "arole_launcher", PermissionKeys: []string{"agent.view", "agent.launch"}},
		Scope:       entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: "prj_a", ResourceKind: "AGENT", ResourceRef: "agt_a"},
	}
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		target  entity.AccessScope
		allowed bool
	}{
		{name: "exact agent", target: entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: "prj_a", ResourceKind: "AGENT", ResourceRef: "agt_a"}, allowed: true},
		{name: "other agent", target: entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: "prj_a", ResourceKind: "AGENT", ResourceRef: "agt_b"}},
		{name: "same agent locator in other project", target: entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: "prj_b", ResourceKind: "AGENT", ResourceRef: "agt_a"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := Evaluate(subject, "agent.launch", test.target, "", []entity.AccessBinding{binding}, now)
			if decision.Allowed != test.allowed {
				t.Fatalf("unexpected decision: allowed=%v explanation=%#v", decision.Allowed, decision.Explanation)
			}
			if !test.allowed && (len(decision.Explanation) != 1 || decision.Explanation[0].Code != "NO_ALLOW_BINDING" || decision.Explanation[0].BindingRef != "") {
				t.Fatalf("denial exposed binding details: %#v", decision.Explanation)
			}
		})
	}
}

func TestEvaluateRestrictsOrganizationIntegrationInstance(t *testing.T) {
	subject := entity.AccessSubject{Ref: "usr_alice", Kind: "USER", Active: true}
	binding := entity.AccessBinding{
		Ref: "abnd_integration_a", State: "ACTIVE", Subject: subject,
		RoleVersion: entity.AccessRoleVersion{Ref: "arv_integration", RoleRef: "arole_integration", PermissionKeys: []string{"integration.manage"}},
		Scope:       entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: "int_a"},
	}
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	if err := ValidateScope(binding.Scope); err != nil {
		t.Fatalf("organization integration scope was rejected: %v", err)
	}
	if decision := Evaluate(subject, "integration.manage", binding.Scope, "", []entity.AccessBinding{binding}, now); !decision.Allowed {
		t.Fatalf("exact integration binding was not applied: %#v", decision)
	}
	other := entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: "int_b"}
	if decision := Evaluate(subject, "integration.manage", other, "", []entity.AccessBinding{binding}, now); decision.Allowed {
		t.Fatalf("integration binding escaped its exact instance: %#v", decision)
	}
}

func TestValidateScopeRequiresProjectForProjectOwnedResourceInstance(t *testing.T) {
	if err := ValidateScope(entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "AGENT", ResourceRef: "agt_a"}); err == nil {
		t.Fatal("project-owned agent scope without project was accepted")
	}
	if err := ValidateScope(entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: "prj_a", ResourceKind: "INTEGRATION", ResourceRef: "int_a"}); err == nil {
		t.Fatal("organization-owned integration scope with project was accepted")
	}
}

func TestEvaluateAppliesBoundedWindowAndOIDCGroup(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	from, until := now.Add(-time.Minute), now.Add(time.Minute)
	subject := entity.AccessSubject{Ref: "usr_alice", Kind: "USER", Active: true, OIDCGroupRefs: []string{"grp_operators"}}
	binding := entity.AccessBinding{
		Ref: "abnd_group", State: "ACTIVE", Subject: entity.AccessSubject{Ref: "grp_operators", Kind: "OIDC_GROUP", Active: true},
		RoleVersion: entity.AccessRoleVersion{PermissionKeys: []string{"agent.launch"}},
		Scope:       entity.AccessScope{Kind: "RESOURCE_KIND", ProjectRef: "prj_a", ResourceKind: "AGENT"},
		Conditions:  entity.AccessConditions{ValidFrom: &from, ValidUntil: &until},
	}
	target := entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: "prj_a", ResourceKind: "AGENT", ResourceRef: "agt_a"}
	if decision := Evaluate(subject, "agent.launch", target, "", []entity.AccessBinding{binding}, now); !decision.Allowed || decision.Explanation[0].Code != "OIDC_GROUP_BINDING" {
		t.Fatalf("active group binding was not applied: %#v", decision)
	}
	if decision := Evaluate(subject, "agent.launch", target, "", []entity.AccessBinding{binding}, until); decision.Allowed {
		t.Fatalf("expired group binding was applied: %#v", decision)
	}
}
