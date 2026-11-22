package grpc

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// ManagedKubernetesPlanToProto projects an already owner-authorized immutable plan through the
// actual RPC mapper. It issues no authority; malformed children discard the complete projection.
// ManagedKubernetesPlanToProto проецирует owner-authorized immutable plan через actual RPC mapper.
// Не выдаёт authority; malformed child отбрасывает projection полностью.
func ManagedKubernetesPlanToProto(result entity.ManagedKubernetesPlan) *cp.PrepareManagedKubernetesChildrenResponse {
	if !managedkubernetes.ValidLocator(result.Ref) || !managedkubernetes.ValidDigest(result.Digest) || result.Revision < 1 || len(result.Children) > 256 {
		return nil
	}
	response := &cp.PrepareManagedKubernetesChildrenResponse{PlanRef: result.Ref, PlanRevision: result.Revision, PlanDigest: result.Digest}
	for _, child := range result.Children {
		entry := managedKubernetesChildToProto(child)
		if entry == nil {
			return nil
		}
		response.Children = append(response.Children, entry)
	}
	return response
}
