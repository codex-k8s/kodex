package mailpolicy

const PublicationAdmissionName = "egress-mail-configmap-publication"

// PublicationAdmissionResources — общий source deploy policy и CP readback.
// CEL ограничивает выданный CP ConfigMap create; семантическая JSON validation
// и digest принадлежат typed producer/consumer, а прочие callers — своим RBAC.
func PublicationAdmissionResources() (map[string]any, map[string]any) {
	policy := map[string]any{
		"apiVersion": "admissionregistration.k8s.io/v1", "kind": "ValidatingAdmissionPolicy",
		"metadata": map[string]any{"name": PublicationAdmissionName},
		"spec": map[string]any{
			"failurePolicy": "Fail",
			"matchConstraints": map[string]any{
				"matchPolicy": "Equivalent", "namespaceSelector": map[string]any{}, "objectSelector": map[string]any{},
				"resourceRules": []any{map[string]any{"apiGroups": []any{""}, "apiVersions": []any{"v1"}, "operations": []any{"CREATE"}, "resources": []any{"configmaps"}, "scope": "*"}},
			},
			"matchConditions": []any{map[string]any{"name": "control-plane-create", "expression": "request.userInfo.username == 'system:serviceaccount:kodex-system:control-plane'"}},
			"validations": []any{
				map[string]any{"expression": "object.metadata.namespace == 'kodex-system' && object.metadata.name.matches('^egress-gateway-mail-[a-f0-9]{24}$')", "message": "control-plane may only create exact mail projection ConfigMaps", "reason": "Forbidden"},
				map[string]any{"expression": "has(object.immutable) && object.immutable == true && (!has(object.binaryData) || size(object.binaryData) == 0)", "message": "mail projection ConfigMaps must be immutable and cannot contain binary data", "reason": "Forbidden"},
				map[string]any{"expression": "has(object.metadata.labels) && object.metadata.labels['app.kubernetes.io/name'] == 'egress-gateway' && object.metadata.labels['app.kubernetes.io/component'] == 'platform-egress' && (!has(object.metadata.ownerReferences) || size(object.metadata.ownerReferences) == 0)", "message": "mail projection ConfigMap labels or ownership are invalid", "reason": "Forbidden"},
				map[string]any{"expression": "has(object.data) && size(object.data) == 1 && 'mail-policy.json' in object.data && size(object.data['mail-policy.json']) > 0 && size(object.data['mail-policy.json']) <= 65536 && object.data['mail-policy.json'].startsWith('{\"schema\":\"egress-mail/v1\",')", "message": "mail projection ConfigMap payload is outside its registered boundary", "reason": "Forbidden"},
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
