package platform

import (
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// validKubernetesPortForwardPreflight independently derives the unsupported lifecycle profile from the persisted exact plan and current operator pin.
// It rejects unknown protocols, attempted effects, outputs and open Gates instead of trusting a caller's proposed reason or fabricating a native receipt.
// validKubernetesPortForwardPreflight независимо выводит unsupported lifecycle profile из persisted exact plan и current operator pin.
// Unknown protocols, attempted effects, outputs и open Gates отклоняются; caller reason не считается authority, native receipt не создаётся.
func validKubernetesPortForwardPreflight(operation string, original []entity.ManagedKubernetesChildEffect, children []kubernetesInvocationChildResult) bool {
	if operation != "kubernetes.port-forward" || len(original) != 1 || len(children) != 1 {
		return false
	}
	child, fact := original[0], children[0]
	return child.Operation == operation && child.State == "READY" && child.NodeTransport != nil && child.NodeTransport.Protocol == managedkubernetes.PortForwardWebSocketV4 && fact.ordinal == 0 && fact.operation == operation && fact.current && !fact.openGate && !fact.hasOutput && fact.streamRef == "" && fact.state == "" && fact.outcome == "" && fact.result == "" && fact.digest == "" && fact.reason == "" && !fact.applied
}
