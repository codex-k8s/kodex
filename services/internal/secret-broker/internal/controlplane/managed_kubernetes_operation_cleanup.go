package controlplane

import (
	"context"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"google.golang.org/protobuf/proto"
)

// ResolveKubernetesStreamCleanup reads irreversible closure for an original binding without obtaining credentials or execution credit.
// The closed proof is a separate response variant; mixed snapshots, changed lineage and unknown states fail closed without retry.
// ResolveKubernetesStreamCleanup читает необратимое закрытие original binding без credentials либо execution credit.
// Closed proof является отдельным response variant; mixed snapshots, чужая lineage и unknown states закрыто отклоняются без retry.
func (owner *Owner) ResolveKubernetesStreamCleanup(ctx context.Context, binding managedkubernetes.Binding, streamRef string) (targetrepo.StreamCleanupReceipt, error) {
	var empty targetrepo.StreamCleanupReceipt
	digest, digestErr := managedkubernetes.BindingDigest(binding)
	if ctx == nil || ctx.Err() != nil || owner == nil || owner.client == nil || owner.client.KubernetesOperations == nil ||
		digestErr != nil || binding.InvocationRef == "" || !managedkubernetes.ValidLocator(streamRef) || len(streamRef) > 96 {
		return empty, targetrepo.ErrOperationUnavailable
	}
	response, err := owner.client.KubernetesOperations.ResolveManagedKubernetesOperation(ctx, &cp.ResolveManagedKubernetesOperationRequest{
		Binding: kubernetesHealthBindingToProto(binding), StreamRef: streamRef, CleanupOnly: true,
	})
	if err != nil || ctx.Err() != nil || response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		return empty, targetrepo.ErrOperationUnavailable
	}
	proof := response.GetCleanupReceipt()
	if proof == nil || len(proof.ProtoReflect().GetUnknown()) != 0 || !proof.GetExecutionClosed() ||
		proof.GetInvocationRef() != binding.InvocationRef || proof.GetStreamRef() != streamRef || proof.GetGeneration() != binding.Generation ||
		proof.GetBindingDigest() != digest || !managedkubernetes.ValidDigest(proof.GetSnapshotDigest()) ||
		!proto.Equal(response, &cp.ResolveManagedKubernetesOperationResponse{CleanupReceipt: proof}) {
		return empty, targetrepo.ErrOperationUnavailable
	}
	state := strings.TrimPrefix(proof.GetStreamState().String(), "MANAGED_KUBERNETES_OWNER_STREAM_STATE_")
	if _, valid := kubernetesStreamStateToProto(state); !valid {
		return empty, targetrepo.ErrOperationUnavailable
	}
	return targetrepo.StreamCleanupReceipt{InvocationRef: proof.GetInvocationRef(), StreamRef: proof.GetStreamRef(), Generation: proof.GetGeneration(),
		BindingDigest: digest, SnapshotDigest: proof.GetSnapshotDigest(), StreamState: state, ExecutionClosed: true}, nil
}
