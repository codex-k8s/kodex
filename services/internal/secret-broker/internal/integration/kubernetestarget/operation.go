package kubernetestarget

import (
	"errors"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// OperationAccessRequirements resolves the RBAC tuple for a single exact registered resource
// operation. Plans and streams are rejected: their handlers must supply their full compound check
// set. The returned tuple does not replace fresh owner permission, body policy or UID/RV CAS.
// OperationAccessRequirements разрешает RBAC tuple для одной exact registered resource operation.
// Plans и streams отклоняются: handlers обязаны задать полный compound check set.
// Tuple не заменяет fresh owner permission, body policy либо UID/RV CAS.
func OperationAccessRequirements(operationKey string, address ResourceAddress) ([]AccessRequirement, error) {
	operation, known := integrationpackage.KubernetesOperationByKey(operationKey)
	if !known || operation.Execution != integrationpackage.KubernetesResource || address.Validate() != nil ||
		address.Subresource != operation.Subresource || address.AllNamespaces != (operationKey == "kubernetes.list.all-namespaces") {
		return nil, errors.New(accessUnknown)
	}
	switch operation.Verb {
	case "list", "create":
		if address.Name != "" || address.UID != "" || address.ResourceVersion != "" {
			return nil, errors.New(accessUnknown)
		}
	case "get":
		if address.Name == "" {
			return nil, errors.New(accessUnknown)
		}
	case "update", "patch", "delete":
		if address.Name == "" || address.UID == "" || address.ResourceVersion == "" {
			return nil, errors.New(accessUnknown)
		}
	default:
		return nil, errors.New(accessUnknown)
	}
	requirement := AccessRequirement{Group: address.Group, Version: address.Version,
		Resource: address.Resource, Namespace: address.Namespace, Name: address.Name,
		Subresource: address.Subresource, Verb: operation.Verb, Namespaced: address.Namespaced, AllNamespaces: address.AllNamespaces}
	if requirement.Validate() != nil {
		return nil, errors.New(accessUnknown)
	}
	return []AccessRequirement{requirement}, nil
}
