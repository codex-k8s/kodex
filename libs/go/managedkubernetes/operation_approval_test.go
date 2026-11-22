package managedkubernetes

import "testing"

// TestKubernetesApprovalRequiresEachForPrivilege rejects a lower grant rather than upgrading it.
// TestKubernetesApprovalRequiresEachForPrivilege отклоняет меньший grant, не повышая его.
func TestKubernetesApprovalRequiresEachForPrivilege(t *testing.T) {
	for _, testCase := range []struct {
		group, namespace, risk, selected string
		allowed                          bool
	}{
		{"apps", "example", "WRITE", "HUMAN_SCOPED", true},
		{"apps", "example", "WRITE", "HUMAN_EACH_EFFECT", true},
		{"", "", "WRITE", "HUMAN_SCOPED", false},
		{"", "", "WRITE", "HUMAN_EACH_EFFECT", true},
		{"rbac.authorization.k8s.io", "example", "WRITE", "HUMAN_SCOPED", false},
		{"authentication.k8s.io", "example", "WRITE", "HUMAN_SCOPED", false},
		{"authorization.k8s.io", "example", "WRITE", "HUMAN_SCOPED", false},
		{"certificates.k8s.io", "example", "WRITE", "HUMAN_SCOPED", false},
		{"apps", "example", "DESTRUCTIVE", "HUMAN_SCOPED", false},
		{"apps", "example", "SENSITIVE", "HUMAN_SCOPED", false},
		{"apps", "example", "WRITE", "NONE", false},
		{"apps", "example", "UNKNOWN", "HUMAN_EACH_EFFECT", false},
		{"", "", "READ", "NONE", true},
	} {
		input := OperationInput{Group: testCase.group, Namespace: testCase.namespace}
		if (ValidateInvocationApprovalPolicy(input, testCase.risk, testCase.selected) == nil) != testCase.allowed {
			t.Fatalf("unexpected approval admission for %s/%s/%s", testCase.group, testCase.risk, testCase.selected)
		}
	}
}

// TestKubernetesSecretEffectsNeverInheritScopedMetadataApproval keeps the provider-resolved core
// Secret boundary distinct from READ projection and from an unrelated API group's resource name.
// TestKubernetesSecretEffectsNeverInheritScopedMetadataApproval отделяет provider-resolved core
// Secret boundary от READ projection и одноимённого resource посторонней API group.
func TestKubernetesSecretEffectsNeverInheritScopedMetadataApproval(t *testing.T) {
	input := OperationInput{Version: "v1", Resource: "secrets", Namespace: "selected"}
	if ValidateInvocationApprovalPolicy(input, "WRITE", "HUMAN_SCOPED") == nil ||
		ValidateInvocationApprovalPolicy(input, "WRITE", "HUMAN_EACH_EFFECT") != nil {
		t.Fatal("Secret metadata effect lost its explicit EACH floor")
	}
	if ValidateInvocationApprovalPolicy(input, "READ", "NONE") != nil {
		t.Fatal("Secret metadata-only read inherited write consent")
	}
	input.Group = "example.test"
	if ValidateInvocationApprovalPolicy(input, "WRITE", "HUMAN_SCOPED") != nil {
		t.Fatal("unrelated API group was misclassified as a core Secret")
	}
}
