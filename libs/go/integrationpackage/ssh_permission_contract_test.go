package integrationpackage

import "testing"

// TestSSHPermissionProfileUsesCurrentOperationChecks keeps identity health separate from host resource permission and preserves SCOPED versus EACH floors.
// TestSSHPermissionProfileUsesCurrentOperationChecks отделяет identity health от host resource permission и сохраняет SCOPED/EACH floors.
func TestSSHPermissionProfileUsesCurrentOperationChecks(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{SSHManagedDefinitionKey, SSHExecuteDefinitionKey} {
		definition, exists := definitions[key]
		if !exists || definition.Spec.Readiness != "READY" {
			t.Fatal("SSH package lost its explicit executable source profile")
		}
		for _, capability := range definition.Spec.Capabilities {
			profile := CredentialResourcePermissionProfile(definition, capability)
			contract, known := CredentialResourcePermissionContractForProfile(key, capability.Key, profile)
			if !known || profile != CredentialResourcePermissionSSHScopedV1 || contract.ResourceKind != "SSH_TARGET" || contract.Operation != capability.Operation || contract.Risk != capability.Risk || contract.ApprovalPolicy != capability.ApprovalPolicy || len(contract.ReadCapabilities) != 1 || contract.ReadCapabilities[0] != definition.Spec.HealthCheck.Operation || contract.RequiredObservationProfile != "" {
				t.Fatal("current SSH checks lost exact package or policy boundary")
			}
			contract.ReadCapabilities[0] = "changed-copy"
			fresh, _ := CredentialResourcePermissionContractForProfile(key, capability.Key, profile)
			if fresh.ReadCapabilities[0] == "changed-copy" {
				t.Fatal("caller changed another operation's health prerequisite")
			}
			changed := capability
			changed.Operation = "ssh.unknown"
			if CredentialResourcePermissionProfile(definition, changed) != "" {
				t.Fatal("undeclared capability acquired a required-check profile")
			}
		}
	}
	for _, scenario := range []struct{ key, operation, profile string }{
		{SSHManagedDefinitionKey, "cat /etc/passwd", CredentialResourcePermissionSSHScopedV1},
		{SSHManagedDefinitionKey, "ssh.execute", CredentialResourcePermissionSSHScopedV1},
		{SSHExecuteDefinitionKey, "ssh.file.create", CredentialResourcePermissionSSHScopedV1},
		{SSHManagedDefinitionKey, "ssh.file.create", "KUBERNETES_SCOPED_RESOURCE_V1"},
		{SSHManagedDefinitionKey, "ssh.file.create", ""},
	} {
		if _, known := CredentialResourcePermissionContractForProfile(scenario.key, scenario.operation, scenario.profile); known {
			t.Fatal("unknown or foreign SSH profile became permission metadata")
		}
	}
}
