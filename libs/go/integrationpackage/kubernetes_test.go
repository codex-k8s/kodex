package integrationpackage

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// TestKubernetesDefinitionConsumesExactCommandInputRegistry proves every declared command reaches
// the shared typed input contract with matching risk, approval floor and broker-only delivery.
// TestKubernetesDefinitionConsumesExactCommandInputRegistry доказывает связь каждой declared command
// с shared typed input contract, matching risk, approval floor и broker-only delivery.
func TestKubernetesDefinitionConsumesExactCommandInputRegistry(t *testing.T) {
	shipped, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := shipped[KubernetesDefinitionKey]
	if validateKubernetesDefinition(definition) != nil || !definition.ExecutableBy(OwnerIntegrationGateway, RouteManagedMCP) {
		t.Fatal("typed Kubernetes definition is unavailable")
	}
	delivery, valid := definition.ConnectionCredentialDelivery()
	if !valid || delivery.Kind != CredentialDeliveryBrokerIdentity || delivery.BrokerPurpose != "KUBERNETES_TARGET" || definition.Spec.Credential.SecretKey != "" {
		t.Fatal("Kubernetes acquired generic token delivery")
	}
	keys := make([]string, 0, len(KubernetesOperations()))
	for _, operation := range KubernetesOperations() {
		keys = append(keys, operation.Key)
		capability, exists := definition.Capability(operation.Key)
		if !exists || capability.Risk != string(operation.Risk) || capability.ApprovalPolicy != string(operation.Approval) ||
			capability.Execution.Idempotency != string(operation.Idempotency) || capability.Execution.MaxAttempts != 1 {
			t.Fatalf("command classification drift for %s", operation.Key)
		}
		actual, err := capability.InputSchema()
		expected, expectedErr := managedkubernetes.OperationInputSchema(operation.Key)
		if err != nil || expectedErr != nil || !reflect.DeepEqual(actual, expected) {
			t.Fatalf("input schema drift for %s", operation.Key)
		}
		profile := CredentialResourcePermissionProfile(definition, capability)
		contract, exists := CredentialResourcePermissionContractForProfile(definition.Metadata.Key, capability.Key, profile)
		if !exists || profile != CredentialResourcePermissionKubernetesScopedV1 || contract.Operation != operation.Key ||
			!reflect.DeepEqual(contract.ReadCapabilities, []string{"kubernetes.identity"}) || contract.RequiredObservationProfile != "" {
			t.Fatalf("mandatory typed permission check missing for %s", operation.Key)
		}
	}
	if !reflect.DeepEqual(keys, managedkubernetes.OperationInputOperations()) {
		t.Fatal("input registry and command registry differ")
	}
	if _, ok := CredentialResourcePermissionContractForProfile(KubernetesDefinitionKey, "kubernetes.get", "UNKNOWN"); ok {
		t.Fatal("unknown profile acquired a weaker fallback")
	}
}

// TestKubernetesGrantNamespaceAndDynamicPolicy keeps source target and namespace authorization
// separate, rejecting a hidden cluster/RBAC mutation under otherwise reusable SCOPED consent.
// TestKubernetesGrantNamespaceAndDynamicPolicy разделяет source target и namespace authorization,
// отклоняя скрытую cluster/RBAC mutation под другим reusable SCOPED consent.
func TestKubernetesGrantNamespaceAndDynamicPolicy(t *testing.T) {
	shipped, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := shipped[KubernetesDefinitionKey]
	configuration := map[string]string{"target_ref": "target_example"}
	if definition.ValidateConfiguration(configuration) != nil {
		t.Fatal("valid target configuration rejected")
	}
	for _, invalid := range []map[string]string{
		{"target_ref": "target_example", "namespace": "example"}, {"target_ref": "https://other"}, {"target_ref": "short"},
	} {
		if definition.ValidateConfiguration(invalid) == nil {
			t.Fatal("target configuration widened")
		}
	}
	patch, _ := definition.Capability("kubernetes.patch")
	scope, err := patch.KubernetesResourceScope(configuration, "example")
	if err != nil || !reflect.DeepEqual(scope, map[string]string{"target_ref": "target_example", "namespace": "example"}) {
		t.Fatal("server-derived scope mismatch")
	}
	if _, err := patch.KubernetesResourceScope(configuration, "@all-namespaces"); err == nil {
		t.Fatal("mutation acquired all-namespace grant")
	}
	for _, namespace := range []string{"example", ""} {
		raw, _ := json.Marshal(map[string]any{"group": "", "version": "v1", "resource": "configmaps", "namespace": namespace,
			"name": "settings", "uid": "dc7b5a7f-2421-41bf-bf3b-71f7b362fda8", "resourceVersion": "17",
			"body": map[string]any{"data": map[string]any{"mode": "safe"}}, "options": map[string]any{"patchType": "MERGE"}})
		canonical, err := patch.ValidateInput(raw)
		if err != nil {
			t.Fatal(err)
		}
		err = definition.ValidateInvocationApprovalPolicy(patch, "HUMAN_SCOPED", canonical)
		if (err == nil) != (namespace != "") {
			t.Fatal("cluster mutation inherited SCOPED approval")
		}
		if definition.ValidateInvocationApprovalPolicy(patch, "HUMAN_EACH_EFFECT", canonical) != nil {
			t.Fatal("explicit EACH selection rejected")
		}
	}
}

// TestKubernetesDefinitionRejectsContractSubstitution prevents display/package editing from lowering
// gates, changing broker purpose, adding token keys, accepting scalar inputs or widening outcomes.
// TestKubernetesDefinitionRejectsContractSubstitution не позволяет package/display editing снизить
// gates, заменить broker purpose, добавить token keys, scalar inputs либо расширить outcomes.
func TestKubernetesDefinitionRejectsContractSubstitution(t *testing.T) {
	shipped, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	baseline, _ := json.Marshal(shipped[KubernetesDefinitionKey])
	for _, mutate := range []func(*Package){
		func(definition *Package) { definition.Metadata.Key = "other" },
		func(definition *Package) { definition.Spec.Credential.SecretKey = "kubeconfig" },
		func(definition *Package) { definition.Spec.Credential.BrokerPurpose = "SSH_TARGET" },
		func(definition *Package) { definition.Spec.NetworkDestinations[0].Hostname = "other.example" },
		func(definition *Package) {
			definition.Spec.Capabilities[0].InputFields = []Field{{Key: "raw", Type: "STRING", MaximumLength: 20}}
		},
		func(definition *Package) { definition.Spec.Capabilities[0].Risk = "WRITE" },
		func(definition *Package) { definition.Spec.Capabilities[0].OutputFields[0].MaximumLength++ },
		func(definition *Package) { definition.Spec.Capabilities[0].Execution.MaxAttempts = 2 },
	} {
		var candidate Package
		if json.Unmarshal(baseline, &candidate) != nil {
			t.Fatal("cannot decode owned fixture")
		}
		mutate(&candidate)
		raw, _ := json.Marshal(candidate)
		if _, err := Parse(raw); err == nil {
			t.Fatal("substituted Kubernetes contract was admitted")
		}
	}
}

// TestKubernetesManagedReadOnlyRevisionKeepsTypedBoundary permits removing write capabilities
// while keeping identity health, exact broker purpose and mandatory fresh read admission.
// TestKubernetesManagedReadOnlyRevisionKeepsTypedBoundary допускает удаление write capabilities
// с сохранением identity health, exact broker purpose и mandatory fresh read admission.
func TestKubernetesManagedReadOnlyRevisionKeepsTypedBoundary(t *testing.T) {
	shipped, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	baseline := shipped[KubernetesDefinitionKey]
	raw, _ := json.Marshal(baseline)
	candidate, _, err := NormalizeManagedRevision(raw, OriginUI, shipped)
	if err != nil {
		t.Fatal(err)
	}
	candidate.Spec.Capabilities = nil
	for _, capability := range baseline.Spec.Capabilities {
		if capability.Risk == "READ" {
			candidate.Spec.Capabilities = append(candidate.Spec.Capabilities, capability)
		}
	}
	raw, _ = json.Marshal(candidate)
	candidate, err = Parse(raw)
	if err != nil || ValidateExecutableRevision(candidate, baseline) != nil {
		t.Fatal("managed removal of effects was rejected")
	}
	for _, capability := range candidate.Spec.Capabilities {
		if CredentialResourcePermissionProfile(candidate, capability) != CredentialResourcePermissionKubernetesScopedV1 {
			t.Fatal("narrowing removed mandatory typed consumer checks")
		}
	}
	candidate.Metadata.Origin = Origin
	raw, _ = json.Marshal(candidate)
	if _, err := Parse(raw); err == nil {
		t.Fatal("shipped command catalog silently lost capabilities")
	}
}

// TestKubernetesPublicCandidateScope confines optional selection metadata to safe target/namespace
// coordinates without projecting service selectors, raw URLs or grant/credential authority.
// TestKubernetesPublicCandidateScope ограничивает optional selection metadata safe target/namespace
// coordinates без выдачи service selectors, raw URLs либо grant/credential authority.
func TestKubernetesPublicCandidateScope(t *testing.T) {
	for _, testCase := range []struct {
		operation string
		values    map[string]string
		allowed   bool
	}{
		{"kubernetes.get", map[string]string{"target_ref": "target_example"}, true},
		{"kubernetes.get", map[string]string{"target_ref": "target_example", "namespace": "example"}, true},
		{"kubernetes.list.all-namespaces", map[string]string{"target_ref": "target_example", "namespace": "@all-namespaces"}, true},
		{"kubernetes.get", map[string]string{"target_ref": "target_example", "namespace": "@all-namespaces"}, false},
		{"kubernetes.get", map[string]string{"target_ref": "target_example", "namespace": "*"}, false},
		{"kubernetes.get", map[string]string{"target_ref": "target_example", "service_subject_ref": "service_example"}, false},
		{"kubernetes.get", map[string]string{"target_ref": "https://other.example"}, false},
		{"kubernetes.unknown", map[string]string{"target_ref": "target_example"}, false},
		{"kubernetes.get", nil, false},
	} {
		if (ValidateKubernetesCandidateScope(testCase.operation, testCase.values) == nil) != testCase.allowed {
			t.Fatal("candidate scope crossed the closed safe metadata boundary")
		}
	}
}
