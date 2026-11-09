package integrationpackage

import "testing"

// TestConfigurableApprovalPoliciesSeparateRiskFromConsent checks all declared choices without weakening legacy profiles or accepting an undeclared policy.
// TestConfigurableApprovalPoliciesSeparateRiskFromConsent проверяет объявленные варианты без ослабления legacy profiles и без допуска необъявленной policy.
func TestConfigurableApprovalPoliciesSeparateRiskFromConsent(t *testing.T) {
	definition := Package{Spec: Spec{Adapter: string(AdapterGitLab)}}
	for _, risk := range []Risk{RiskRead, RiskWrite, RiskSensitive, RiskDestructive} {
		for _, selected := range []ApprovalPolicy{ApprovalNone, ApprovalHumanEachEffect, ApprovalHumanScoped} {
			capability := Capability{Risk: string(risk), ApprovalPolicy: string(selected),
				AllowedApprovalPolicies: []string{"NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"},
				ApprovalScope:           &ApprovalScopeContract{Name: "repository", RequiredInputPaths: []string{}},
			}
			if !validCapabilityApprovalPolicies(&definition, capability) || definition.ValidateInvocationApprovalPolicy(capability, string(selected), []byte(`{}`)) != nil {
				t.Fatalf("explicit configurable policy refused: %s/%s", risk, selected)
			}
			capability.AllowedApprovalPolicies = []string{"HUMAN_EACH_EFFECT"}
			if definition.ValidateInvocationApprovalPolicy(capability, "NONE", []byte(`{}`)) == nil || definition.ValidateInvocationApprovalPolicy(capability, "UNKNOWN", []byte(`{}`)) == nil {
				t.Fatal("scope metadata granted a policy missing from the declared set")
			}
		}
	}
	legacy := Capability{Risk: "READ", ApprovalPolicy: "NONE", AllowedApprovalPolicies: []string{"NONE", "HUMAN_EACH_EFFECT"}}
	if validCapabilityApprovalPolicies(&definition, legacy) {
		t.Fatal("legacy READ inherited new human policy authority")
	}
	legacy.Risk = "WRITE"
	if validCapabilityApprovalPolicies(&definition, legacy) {
		t.Fatal("legacy WRITE inherited autonomous authority")
	}
}

// TestConfigurableApprovalPoliciesRejectMalformedSets requires a declared default and bounded unique known choices even in the explicit new profile.
// TestConfigurableApprovalPoliciesRejectMalformedSets требует объявленный default и ограниченный набор уникальных известных вариантов даже в новом профиле.
func TestConfigurableApprovalPoliciesRejectMalformedSets(t *testing.T) {
	definition := Package{Spec: Spec{Adapter: string(AdapterGitLab)}}
	for _, allowed := range [][]string{nil, {}, {"UNKNOWN"}, {"NONE", "NONE"}, {"HUMAN_SCOPED"}, {"NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED", "NONE"}} {
		capability := Capability{Risk: "WRITE", ApprovalPolicy: "NONE", AllowedApprovalPolicies: allowed,
			ApprovalScope: &ApprovalScopeContract{Name: "repository", RequiredInputPaths: []string{}},
		}
		if validCapabilityApprovalPolicies(&definition, capability) {
			t.Fatal("invalid configurable approval policy set admitted")
		}
	}
}
