package platform

import (
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
)

// kubernetesToolSafeReason projects only a registered persisted preflight reason for its exact terminal operation; arbitrary provider diagnostics never leave the owner.
// kubernetesToolSafeReason выдаёт только зарегистрированную persisted preflight причину для exact terminal operation; произвольные provider diagnostics остаются у owner.
func kubernetesToolSafeReason(operation string, state enum.KubernetesToolState, persisted string) enum.KubernetesToolSafeReasonCode {
	if operation == "kubernetes.port-forward" && state == "FAILED" && persisted == managedkubernetes.ReasonPortForwardProfileUnsupported {
		return enum.KubernetesToolReasonPortForwardProfileUnsupported
	}
	return enum.KubernetesToolReasonNone
}
