package integrationegresspolicy

const PublicationAdmissionName = "egress-integration-configmap-publication"
const CreationBoundaryName = "control-plane-egress-configmap-boundary"

// PublicationAdmissionResources ограничивает право control-plane создать
// immutable ConfigMap точной формы. Содержимое строго проверяет consumer.
func PublicationAdmissionResources() (map[string]any, map[string]any) {
	policy := map[string]any{
		"apiVersion": "admissionregistration.k8s.io/v1", "kind": "ValidatingAdmissionPolicy",
		"metadata": map[string]any{"name": PublicationAdmissionName},
		"spec": map[string]any{
			"failurePolicy": "Fail",
			"matchConstraints": map[string]any{
				"matchPolicy": "Equivalent", "namespaceSelector": map[string]any{}, "objectSelector": map[string]any{},
				"resourceRules": []any{map[string]any{"apiGroups": []any{""}, "apiVersions": []any{"v1"},
					"operations": []any{"CREATE"}, "resources": []any{"configmaps"}, "scope": "*"}},
			},
			"matchConditions": []any{map[string]any{"name": "control-plane-integration-create", "expression": "request.userInfo.username == 'system:serviceaccount:kodex-system:control-plane' && object.metadata.name.startsWith('egress-gateway-integration-')"}},
			"validations": []any{
				map[string]any{"expression": "object.metadata.namespace == 'kodex-system' && object.metadata.name.matches('^egress-gateway-integration-[a-f0-9]{24}$')", "message": "control-plane may only create exact integration projection ConfigMaps", "reason": "Forbidden"},
				map[string]any{"expression": "has(object.immutable) && object.immutable == true && (!has(object.binaryData) || size(object.binaryData) == 0)", "message": "integration projection ConfigMaps must be immutable", "reason": "Forbidden"},
				map[string]any{"expression": "has(object.metadata.labels) && object.metadata.labels['app.kubernetes.io/name'] == 'egress-gateway' && object.metadata.labels['app.kubernetes.io/component'] == 'platform-egress' && (!has(object.metadata.ownerReferences) || size(object.metadata.ownerReferences) == 0)", "message": "integration projection ConfigMap labels or ownership are invalid", "reason": "Forbidden"},
				map[string]any{"expression": "has(object.data) && size(object.data) == 1 && 'integration-policy.json' in object.data && size(object.data['integration-policy.json']) > 0 && size(object.data['integration-policy.json']) <= 65536 && object.data['integration-policy.json'].startsWith('{\"schema\":\"egress-integration/v1\",')", "message": "integration projection ConfigMap payload is outside its registered boundary", "reason": "Forbidden"},
			},
		},
	}
	binding := map[string]any{
		"apiVersion": "admissionregistration.k8s.io/v1", "kind": "ValidatingAdmissionPolicyBinding",
		"metadata": map[string]any{"name": PublicationAdmissionName},
		"spec": map[string]any{"policyName": PublicationAdmissionName, "validationActions": []any{"Deny"},
			"matchResources": map[string]any{"matchPolicy": "Equivalent", "namespaceSelector": map[string]any{}, "objectSelector": map[string]any{}}},
	}
	return policy, binding
}

// CreationBoundaryResources запрещает CP пользоваться широким RBAC CREATE
// для любых ConfigMap вне двух закрытых видов projection. Дополнительные
// видовые VAP проверяют schema, labels и bounded payload.
func CreationBoundaryResources() (map[string]any, map[string]any) {
	policy := map[string]any{
		"apiVersion": "admissionregistration.k8s.io/v1", "kind": "ValidatingAdmissionPolicy",
		"metadata": map[string]any{"name": CreationBoundaryName},
		"spec": map[string]any{
			"failurePolicy": "Fail",
			"matchConstraints": map[string]any{
				"matchPolicy": "Equivalent", "namespaceSelector": map[string]any{}, "objectSelector": map[string]any{},
				"resourceRules": []any{map[string]any{"apiGroups": []any{""}, "apiVersions": []any{"v1"},
					"operations": []any{"CREATE"}, "resources": []any{"configmaps"}, "scope": "*"}},
			},
			"matchConditions": []any{map[string]any{"name": "control-plane-create", "expression": "request.userInfo.username == 'system:serviceaccount:kodex-system:control-plane'"}},
			"validations": []any{map[string]any{
				"expression": "object.metadata.namespace == 'kodex-system' && (object.metadata.name.matches('^egress-gateway-mail-[a-f0-9]{24}$') || object.metadata.name.matches('^egress-gateway-integration-[a-f0-9]{24}$'))",
				"message":    "control-plane may only create registered egress projection ConfigMaps", "reason": "Forbidden",
			}},
		},
	}
	binding := map[string]any{
		"apiVersion": "admissionregistration.k8s.io/v1", "kind": "ValidatingAdmissionPolicyBinding",
		"metadata": map[string]any{"name": CreationBoundaryName},
		"spec": map[string]any{"policyName": CreationBoundaryName, "validationActions": []any{"Deny"},
			"matchResources": map[string]any{"matchPolicy": "Equivalent", "namespaceSelector": map[string]any{}, "objectSelector": map[string]any{}}},
	}
	return policy, binding
}
