package managedkubernetes

import "k8s.io/apimachinery/pkg/util/validation"

// ValidateStreamInputViewResource checks only read-only owner-selected stream coordinates.
// Collection watches retain empty Pod/container and explicit namespace sentinels; Pod transports require the exact observed incarnation.
// This validation neither grants input authority nor requires a namespace GET.
// ValidateStreamInputViewResource проверяет только read-only owner-selected координаты stream.
// Collection watches сохраняют пустые Pod/container и explicit namespace sentinels; Pod transports требуют exact observed incarnation.
// Проверка не выдаёт input authority и не требует namespace GET.
func ValidateStreamInputViewResource(operation, namespace, pod, container string, address ResourceAddress) error {
	if address.Validate() != nil {
		return ErrInvalidResourceAddress
	}
	if operation == "kubernetes.watch" || operation == "kubernetes.watch.all-namespaces" {
		if pod != "" || container != "" || address.Name != "" || address.UID != "" || address.ResourceVersion != "" || address.Subresource != "" || address.AllNamespaces != (operation == "kubernetes.watch.all-namespaces") {
			return ErrInvalidResourceAddress
		}
		expected := "@cluster"
		if address.AllNamespaces {
			expected = "@all-namespaces"
		} else if address.Namespaced {
			expected = address.Namespace
		}
		if namespace != expected {
			return ErrInvalidResourceAddress
		}
		return nil
	}
	switch operation {
	case "kubernetes.logs", "kubernetes.exec", "kubernetes.attach", "kubernetes.debug", "kubernetes.cp.upload", "kubernetes.cp.download", "kubernetes.port-forward":
	default:
		return ErrInvalidResourceAddress
	}
	if address.Group != "" || address.Version != "v1" || address.Resource != "pods" || !address.Namespaced || address.AllNamespaces || address.Namespace != namespace || address.Name != pod || address.UID == "" || address.ResourceVersion == "" || address.Subresource != "" || len(validation.IsDNS1123Subdomain(pod)) != 0 || (container == "" && operation != "kubernetes.logs" || container != "" && len(validation.IsDNS1123Label(container)) != 0) {
		return ErrInvalidResourceAddress
	}
	return nil
}
