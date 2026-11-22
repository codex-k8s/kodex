package platform

import (
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"testing"
	"time"
)

// TestPurposeInvocationUsePolicyKeepsGrantManagementSeparate verifies the actual common ACL predicate against canonical registered non-read operations, rejecting a foreign connection or altered capability while preserving ordinary adapter management and separate grant ownership. It does not mark NOT_READY SSH packages runnable.
// TestPurposeInvocationUsePolicyKeepsGrantManagementSeparate проверяет actual common ACL predicate на canonical registered non-read operations, отвергает foreign connection и altered capability, сохраняет management обычных adapters и отдельное управление grant. NOT_READY SSH packages не становятся runnable.
func TestPurposeInvocationUsePolicyKeepsGrantManagementSeparate(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	subject := entity.AccessSubject{Ref: "usr_actor", Kind: "USER", Active: true}
	target := entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: "int_allowed"}
	authority := capabilityAuthority{subject: subject, evaluatedAt: time.Now().UTC(), bindings: []entity.AccessBinding{{State: "ACTIVE", Subject: subject, RoleVersion: entity.AccessRoleVersion{PermissionKeys: []string{"integration.view"}}, Scope: target}}}
	for _, key := range []string{integrationpackage.KubernetesDefinitionKey, integrationpackage.SSHManagedDefinitionKey, integrationpackage.SSHExecuteDefinitionKey} {
		t.Run(key, func(t *testing.T) {
			definition := definitions[key]
			if integrationInvocationActorPermission(definition) != "integration.view" {
				t.Fatal("assigned purpose use required Connection editing")
			}
			checked := false
			for _, capability := range definition.Spec.Capabilities {
				if capability.Risk == "READ" || !capability.CallableByAgent() {
					continue
				}
				checked = true
				if !integrationCapabilityAllowed(authority, definition, capability, target) || authority.allowed("integration.manage", target) {
					t.Fatal("assigned use altered grant management")
				}
				foreign := target
				foreign.ResourceRef = "int_foreign"
				if integrationCapabilityAllowed(authority, definition, capability, foreign) {
					t.Fatal("assigned use escaped Connection scope")
				}
				capability.Operation = "unregistered.operation"
				if integrationCapabilityAllowed(authority, definition, capability, target) {
					t.Fatal("mutated operation acquired purpose use")
				}
				break
			}
			if !checked {
				t.Fatal("fixture omitted registered non-read purpose operation")
			}
		})
	}
	if integrationInvocationActorPermission(definitions["github"]) != "integration.manage" || integrationInvocationActorPermission(definitions["ssh-compose"]) != "integration.manage" {
		t.Fatal("ordinary adapter management policy changed")
	}
}
