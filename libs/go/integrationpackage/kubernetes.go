package integrationpackage

import (
	"errors"
	"reflect"
	"slices"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// KubernetesDefinitionKey pins the only typed broker-owned Kubernetes package.
// KubernetesDefinitionKey закрепляет единственный typed broker-owned Kubernetes package.
const KubernetesDefinitionKey = "kubernetes-managed"

// CredentialResourcePermissionKubernetesScopedV1 requires current identity/discovery and per-call
// SSAR checks. A successful identity health probe never proves cluster-wide READ or WRITE access.
// CredentialResourcePermissionKubernetesScopedV1 требует current identity/discovery и per-call
// SSAR checks. Успешная identity health probe не доказывает cluster-wide READ либо WRITE access.
const CredentialResourcePermissionKubernetesScopedV1 = "KUBERNETES_SCOPED_RESOURCE_V1"

// errKubernetesDefinition prevents malformed package/configuration data from entering broker dispatch.
// errKubernetesDefinition не допускает malformed package/configuration data в broker dispatch.
var errKubernetesDefinition = errors.New("managed Kubernetes definition is invalid")

// kubernetesInputCapability limits shared schemas to the exact registered operation/resource shape.
// The complete package validator separately binds it to the fixed adapter and broker purpose.
// kubernetesInputCapability ограничивает shared schemas точной registered operation/resource shape.
// Complete package validator отдельно связывает её с fixed adapter и broker purpose.
func kubernetesInputCapability(capability Capability) bool {
	_, known := KubernetesOperationByKey(capability.Operation)
	return known && capability.Key == capability.Operation && capability.ResourceScope.Kind == "KUBERNETES_TARGET" &&
		capability.OpenAPI == nil && len(capability.InputFields) == 0
}

// KubernetesResourceScope adds only an explicitly chosen validated grant namespace to target_ref.
// The target is derived from Connection configuration, not an input actor, URL or scope blob.
// KubernetesResourceScope добавляет только явно выбранный validated grant namespace к target_ref.
// Target выводится из Connection configuration, не input actor, URL либо scope blob.
func (capability Capability) KubernetesResourceScope(configuration map[string]string, namespace string) (map[string]string, error) {
	if !kubernetesInputCapability(capability) || !validKubernetesConfiguration(configuration) ||
		!managedkubernetes.ValidIdentityReference(configuration["target_ref"]) ||
		managedkubernetes.ValidateGrantNamespace(capability.Operation, namespace) != nil {
		return nil, errKubernetesDefinition
	}
	return map[string]string{"target_ref": configuration["target_ref"], "namespace": namespace}, nil
}

// ValidateKubernetesCandidateScope checks safe owner metadata before public projection. A base
// candidate may omit the unselected namespace; a selected scope must match its exact operation.
// Neither form constitutes a grant or a permission observation.
// ValidateKubernetesCandidateScope проверяет safe owner metadata до public projection. Base
// candidate может не содержать ещё не выбранный namespace; selected scope соответствует exact operation.
// Ни одна форма не является grant либо permission observation.
func ValidateKubernetesCandidateScope(operation string, values map[string]string) error {
	if _, known := KubernetesOperationByKey(operation); !known || len(values) < 1 || len(values) > 2 ||
		!managedkubernetes.ValidIdentityReference(values["target_ref"]) {
		return errKubernetesDefinition
	}
	for key := range values {
		if key != "target_ref" && key != "namespace" {
			return errKubernetesDefinition
		}
	}
	if namespace, present := values["namespace"]; present && managedkubernetes.ValidateGrantNamespace(operation, namespace) != nil {
		return errKubernetesDefinition
	}
	return nil
}

// kubernetesPermissionContract describes a mandatory typed check, not reusable permission evidence.
// Its prerequisite is inputless selected-identity health; actual object access remains per-call.
// kubernetesPermissionContract описывает mandatory typed check, не reusable permission evidence.
// Prerequisite — inputless selected-identity health; actual object access остаётся per-call.
func kubernetesPermissionContract(definitionKey, capabilityKey string) (CredentialResourcePermissionContract, bool) {
	operation, ok := KubernetesOperationByKey(capabilityKey)
	if definitionKey != KubernetesDefinitionKey || !ok {
		return CredentialResourcePermissionContract{}, false
	}
	return CredentialResourcePermissionContract{Profile: CredentialResourcePermissionKubernetesScopedV1,
		Operation: operation.Key, ResourceKind: "KUBERNETES_TARGET", Risk: string(operation.Risk),
		ApprovalPolicy: string(operation.Approval), ReadCapabilities: []string{"kubernetes.identity"}}, true
}

// validateKubernetesDefinition keeps fixed broker routing, input registry and approval floors closed.
// Display metadata may change, but the package cannot add commands, token delivery or permissions.
// validateKubernetesDefinition закрывает fixed broker routing, input registry и approval floors.
// Display metadata может меняться; package не добавляет commands, token delivery либо permissions.
func validateKubernetesDefinition(definition Package) error {
	if definition.Metadata.Key != KubernetesDefinitionKey || definition.Spec.Adapter != string(AdapterKubernetes) ||
		!validConnectionCredential(definition.Spec.Adapter, definition.Spec.Credential) ||
		len(definition.Spec.ConfigurationFields) != 2 ||
		definition.Metadata.Origin == Origin && len(definition.Spec.Capabilities) != len(KubernetesOperations()) ||
		definition.Spec.HealthCheck.Operation != "kubernetes.identity" || definition.Spec.HealthCheck.MaxAttempts != 1 ||
		!reflect.DeepEqual(definition.Spec.NetworkDestinations, []NetworkDestination{kubernetesBrokerDestination()}) {
		return errKubernetesDefinition
	}
	field := definition.Spec.ConfigurationFields[0]
	if field.Key != "target_ref" || field.Type != "STRING" || field.Format != "IDENTIFIER" || !field.Required ||
		field.MaximumLength != 96 || field.AllowEmpty || len(field.AllowedValues) != 0 {
		return errKubernetesDefinition
	}
	serviceField := definition.Spec.ConfigurationFields[1]
	if serviceField.Key != "service_subject_ref" || serviceField.Type != "STRING" || serviceField.Format != "IDENTIFIER" ||
		serviceField.Required || serviceField.MaximumLength != 96 || serviceField.AllowEmpty || len(serviceField.AllowedValues) != 0 {
		return errKubernetesDefinition
	}
	for _, capability := range definition.Spec.Capabilities {
		operation, ok := KubernetesOperationByKey(capability.Key)
		if !ok || !kubernetesInputCapability(capability) || capability.Risk != string(operation.Risk) ||
			capability.ApprovalPolicy != string(operation.Approval) || capability.Execution.Idempotency != string(operation.Idempotency) ||
			!reflect.DeepEqual(capability.ResourceScope.ConnectionFields, []string{"target_ref"}) ||
			!reflect.DeepEqual(capability.AllowedApprovalPolicies, kubernetesApprovalPolicies(operation)) ||
			!reflect.DeepEqual(capability.OutputFields, kubernetesOutputFields()) ||
			capability.Execution.LeaseProfile != "MANAGED_KUBERNETES_V1" || capability.Execution.MaxAttempts != 1 {
			return errKubernetesDefinition
		}
		if operation.Approval == ApprovalHumanScoped {
			if capability.ApprovalScope == nil || !slices.Equal(capability.ApprovalScope.RequiredInputPaths, kubernetesApprovalPaths(operation.Key)) {
				return errKubernetesDefinition
			}
		} else if capability.ApprovalScope != nil {
			return errKubernetesDefinition
		}
	}
	return nil
}

// validKubernetesConfiguration checks selectors only; the owner must resolve an active SERVICE
// subject for PROJECT_SHARED and reject nonempty service selectors for a personal policy.
// validKubernetesConfiguration проверяет только selectors; owner разрешает active SERVICE
// subject для PROJECT_SHARED и отклоняет nonempty service selectors при personal policy.
func validKubernetesConfiguration(configuration map[string]string) bool {
	if !managedkubernetes.ValidIdentityReference(configuration["target_ref"]) || len(configuration) < 1 || len(configuration) > 2 {
		return false
	}
	for key, value := range configuration {
		if key != "target_ref" && (key != "service_subject_ref" || !managedkubernetes.ValidIdentityReference(value)) {
			return false
		}
	}
	return true
}

// kubernetesBrokerDestination pins the existing internal mTLS RPC, never the selected cluster API.
// kubernetesBrokerDestination закрепляет existing internal mTLS RPC, не selected cluster API.
func kubernetesBrokerDestination() NetworkDestination {
	return NetworkDestination{Key: "kubernetes_broker", Source: "STATIC", Hostname: "secret-broker.kodex-system.svc", Port: 8443, TLS: "REQUIRED"}
}

// kubernetesApprovalPolicies permits stronger EACH choice without lowering a command's policy floor.
// kubernetesApprovalPolicies допускает более строгий EACH choice без снижения policy floor команды.
func kubernetesApprovalPolicies(operation KubernetesOperation) []string {
	if operation.Approval == ApprovalHumanScoped {
		return []string{string(ApprovalHumanScoped), string(ApprovalHumanEachEffect)}
	}
	return []string{string(operation.Approval)}
}

// kubernetesApprovalPaths binds namespace, GVR and exact resource identity, excluding changing RV.
// Create/run consent also binds the complete proposed metadata or generated Pod name.
// kubernetesApprovalPaths связывает namespace, GVR и exact resource identity без меняющегося RV.
// Create/run consent также связывает полные proposed metadata либо generated Pod name.
func kubernetesApprovalPaths(operation string) []string {
	paths := []string{"/group", "/version", "/resource", "/namespace"}
	switch operation {
	case "kubernetes.create":
		return append(paths, "/body/metadata")
	case "kubernetes.run":
		return append(paths, "/options/podName")
	default:
		return append(paths, "/name", "/uid")
	}
}

// kubernetesOutputFields is the safe completed-result envelope; streams use a separate typed lane.
// kubernetesOutputFields — safe completed-result envelope; streams используют отдельную typed lane.
func kubernetesOutputFields() []Field {
	return []Field{
		{Key: "result", Type: "STRING", Format: "PLAIN", Required: true, MaximumLength: 65536},
		{Key: "outcome", Type: "STRING", Format: "PLAIN", Required: true, MaximumLength: 9, AllowedValues: []string{"SUCCEEDED", "FAILED", "UNKNOWN"}},
		{Key: "effect_ref", Type: "STRING", Format: "PLAIN", Required: false, MaximumLength: 256},
	}
}
