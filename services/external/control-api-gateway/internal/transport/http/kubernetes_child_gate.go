package httptransport

import (
	"encoding/json"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// normalizeKubernetesGateShape restores only declared public zero fields and validates the closed child DTO.
// It never turns descriptive target/plan metadata into permission, lease or execution input.
// normalizeKubernetesGateShape восстанавливает только объявленные public zero fields и проверяет child DTO.
// Descriptive target/plan metadata не превращаются в permission, lease либо execution input.
func normalizeKubernetesGateShape(value map[string]any, descriptor protoreflect.MessageDescriptor) error {
	switch descriptor.FullName() {
	case "controlplane.v1.KubernetesOwnerGateResource":
		for _, key := range []string{"group", "version", "resource", "namespace", "name", "uid", "resourceVersion", "subresource"} {
			if _, present := value[key]; !present {
				value[key] = ""
			}
		}
		for _, key := range []string{"namespaced", "allNamespaces"} {
			if _, present := value[key]; !present {
				value[key] = false
			}
		}
		if _, ok := kubernetesGateResource(value); !ok {
			return errOwnerGateShape
		}
	case "controlplane.v1.KubernetesOwnerGateChild":
		for _, key := range []string{"intendedName", "intendedGenerateName"} {
			if _, present := value[key]; !present {
				value[key] = ""
			}
		}
		if len(value) != 11 {
			return errOwnerGateShape
		}
		var preview managedkubernetes.GateEffectPreview
		rawPreview, previewErr := json.Marshal(value["preview"])
		if previewErr != nil || internalrpcauth.DecodeExactJSON(rawPreview, &preview) != nil || preview.Validate(managedkubernetes.GatePreviewDetailBytes) != nil || preview.InputDigest != value["inputDigest"] {
			return errOwnerGateShape
		}
		for _, key := range []string{"planRef", "childRef", "targetRef"} {
			text, ok := value[key].(string)
			if !ok || !fileTargetRef(text) {
				return errOwnerGateShape
			}
		}
		for _, key := range []string{"planDigest", "inputDigest"} {
			text, ok := value[key].(string)
			if !ok || !managedkubernetes.ValidDigest(text) {
				return errOwnerGateShape
			}
		}
		if !positiveCredentialMetadataInteger(value["targetRevision"]) {
			return errOwnerGateShape
		}
		operation, ok := value["operation"].(string)
		registered, known := integrationpackage.KubernetesOperationByKey(operation)
		if !ok || !known || registered.Execution != integrationpackage.KubernetesResource && registered.Execution != integrationpackage.KubernetesStream || preview.ValidateForOperation(operation, managedkubernetes.GatePreviewDetailBytes) != nil {
			return errOwnerGateShape
		}
		resource, ok := kubernetesGateResource(value["resource"])
		if !ok || resource.AllNamespaces || operation == "kubernetes.port-forward" && (resource.Group != "" || resource.Version != "v1" || resource.Resource != "pods" || resource.Subresource != "" || !resource.Namespaced) {
			return errOwnerGateShape
		}
		name, named := value["intendedName"].(string)
		generate, generating := value["intendedGenerateName"].(string)
		if !named || !generating {
			return errOwnerGateShape
		}
		if operation == "kubernetes.create" {
			if resource.Name != "" || resource.UID != "" || resource.ResourceVersion != "" || resource.Subresource != "" || (name == "") == (generate == "") {
				return errOwnerGateShape
			}
			resource.Name = name
			if name == "" {
				resource.Name = generate
			}
			if resource.Validate() != nil {
				return errOwnerGateShape
			}
		} else if resource.Name == "" || resource.UID == "" || resource.ResourceVersion == "" || name != "" || generate != "" {
			return errOwnerGateShape
		}
	case "controlplane.v1.KubernetesOwnerGatePreview":
		for _, key := range []string{"options", "changes", "limitations"} {
			if _, present := value[key]; !present {
				value[key] = []any{}
			}
		}
		if _, present := value["secretMaterialRef"]; !present {
			value["secretMaterialRef"] = ""
		}
	case "controlplane.v1.KubernetesOwnerGatePreviewOption", "controlplane.v1.KubernetesOwnerGatePreviewChange":
		for _, key := range []string{"valueJson"} {
			if _, present := value[key]; !present {
				value[key] = ""
			}
		}
		if descriptor.FullName() == "controlplane.v1.KubernetesOwnerGatePreviewChange" {
			if _, present := value["from"]; !present {
				value["from"] = ""
			}
		}
		if _, present := value["redacted"]; !present {
			value["redacted"] = false
		}
	case "controlplane.v1.OwnerGate":
		if _, present := value["kubernetesChild"]; present {
			if value["integrationIntent"] != nil || value["reworkPlan"] != nil {
				return errOwnerGateShape
			}
			decisions, _ := value["allowedDecisions"].([]any)
			for _, decision := range decisions {
				if decision != "APPROVE" && decision != "REJECT" && decision != "CANCEL" {
					return errOwnerGateShape
				}
			}
		}
	}
	return nil
}

// kubernetesGateResource accepts exactly the normalized public address and shared Kubernetes validation.
// Empty CREATE incarnation fields remain empty; no UID/RV or namespace is fabricated.
// kubernetesGateResource допускает точный normalized public address с shared Kubernetes validation.
// Пустые CREATE incarnation fields не заполняются выдуманными UID/RV либо namespace.
func kubernetesGateResource(value any) (managedkubernetes.ResourceAddress, bool) {
	var address managedkubernetes.ResourceAddress
	object, ok := value.(map[string]any)
	if !ok || len(object) != 10 {
		return address, false
	}
	for _, key := range []string{"group", "version", "resource", "namespace", "name", "uid", "resourceVersion", "subresource"} {
		text, ok := object[key].(string)
		if !ok || !utf8.ValidString(text) {
			return address, false
		}
	}
	namespaced, ok := object["namespaced"].(bool)
	if !ok {
		return address, false
	}
	all, ok := object["allNamespaces"].(bool)
	if !ok {
		return address, false
	}
	address = managedkubernetes.ResourceAddress{Group: object["group"].(string), Version: object["version"].(string), Resource: object["resource"].(string), Namespace: object["namespace"].(string), Name: object["name"].(string), UID: object["uid"].(string), ResourceVersion: object["resourceVersion"].(string), Subresource: object["subresource"].(string), Namespaced: namespaced, AllNamespaces: all}
	return address, address.Validate() == nil
}
