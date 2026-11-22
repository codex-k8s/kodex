package integrationpackage

import (
	"reflect"
	"testing"
)

// TestSSHSourcePackagesKeepBrokerCustodyAndPolicyFloors checks the selected policy floors and exact internal broker route while execution remains bound to the purpose owner and original claim.
// TestSSHSourcePackagesKeepBrokerCustodyAndPolicyFloors проверяет selected policy floors и exact internal broker route с сохранением purpose owner и original claim boundary при исполнении.
func TestSSHSourcePackagesKeepBrokerCustodyAndPolicyFloors(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{SSHManagedDefinitionKey, SSHExecuteDefinitionKey} {
		definition := definitions[key]
		if !definition.ExecutableBy(OwnerIntegrationGateway, RouteManagedMCP) || definition.Spec.Readiness != string(ReadinessReady) || validateSSHDefinition(definition) != nil {
			t.Fatal("SSH source lost its exact executable route")
		}
		for _, configuration := range []map[string]string{{}, {"target_ref": "short"}, {"target_ref": "target_native_ssh", "hostname": "example.invalid"}, {"target_ref": "target_native_ssh", "service_subject_ref": ""}} {
			if definition.ValidateConfiguration(configuration) == nil {
				t.Fatal("undeclared or incomplete target selector accepted")
			}
		}
		if err := definition.ValidateConfiguration(map[string]string{"target_ref": "target_native_ssh"}); err != nil {
			t.Fatal("owner target selector rejected", err)
		}
		for _, capability := range definition.Spec.Capabilities {
			result, err := capability.ResourceScopeValues(map[string]string{"target_ref": "target_native_ssh"})
			if err != nil || !reflect.DeepEqual(result, map[string]string{"target_ref": "target_native_ssh"}) {
				t.Fatal("typed target was rehashed or broadened", err)
			}
			capability.ResourceScope.ConnectionFields = nil
			if _, err := capability.ResourceScopeValues(map[string]string{"target_ref": "target_native_ssh"}); err == nil {
				t.Fatal("missing typed target became an empty scope")
			}
		}
		for _, destination := range []NetworkDestination{{Key: "ssh_broker", Source: "STATIC", Hostname: "foreign.kodex-system.svc", Port: 8443, TLS: "REQUIRED"}, {Key: "ssh_broker", Source: "STATIC", Hostname: "secret-broker.kodex-system.svc", Port: 22, TLS: "REQUIRED"}} {
			changed := definition
			changed.Spec.NetworkDestinations = []NetworkDestination{destination}
			if validate(&changed) == nil {
				t.Fatal("foreign internal RPC tuple inherited a TLS exception")
			}
		}
	}
	execute, _ := definitions[SSHExecuteDefinitionKey].Capability("ssh.execute")
	if execute.Risk != "SENSITIVE" || execute.ApprovalPolicy != "HUMAN_EACH_EFFECT" || !reflect.DeepEqual(execute.AllowedApprovalPolicies, []string{"HUMAN_EACH_EFFECT"}) || execute.Execution.Idempotency != "ONE_ATTEMPT" {
		t.Fatal("arbitrary shell lost exact EACH boundary")
	}
	for _, operation := range []string{"ssh.file.create", "ssh.file.replace"} {
		capability, _ := definitions[SSHManagedDefinitionKey].Capability(operation)
		if capability.ApprovalPolicy != "HUMAN_SCOPED" || capability.ApprovalScope == nil {
			t.Fatal("managed change lost SCOPED profile")
		}
	}
}
