package controlplane

import (
	"context"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// PrepareKubernetesFileInput uses the real protected owner RPC and full known readiness conjunction.
// It verifies source provenance against the already validated immutable plan/child, never caller selectors.
// PrepareKubernetesFileInput использует real protected owner RPC и full known readiness conjunction.
// Проверяет source provenance по validated immutable plan/child, не caller selectors.
func (owner *Owner) PrepareKubernetesFileInput(ctx context.Context, binding managedkubernetes.Binding, snapshot value.KubernetesOperationSnapshot, plan targetrepo.OperationPlan, child targetrepo.OperationChild) (targetrepo.FileInputPreparation, error) {
	var empty targetrepo.FileInputPreparation
	if !owner.kubernetesEffectReady(ctx, binding) || child.Operation != "kubernetes.cp.upload" || snapshot.PlanRef != plan.Ref || snapshot.PlanDigest != plan.Digest || snapshot.PlanRevision != plan.Revision || child.Ref == "" {
		return empty, targetrepo.ErrOperationUnavailable
	}
	profile, err := managedkubernetes.DecodeCopyProfile(child.Operation, child.Input)
	if err != nil {
		return empty, targetrepo.ErrOperationUnavailable
	}
	ready, err := owner.client.KubernetesOperations.CheckManagedKubernetesOwnerReadiness(ctx, &cp.CheckManagedKubernetesOwnerReadinessRequest{})
	if err != nil || ctx.Err() != nil || ready == nil || len(ready.ProtoReflect().GetUnknown()) != 0 || !ready.GetReady() || ready.GetDeliveryContractRevision() != 1 || ready.GetOutputContractRevision() != managedkubernetes.OutputContractRevision || ready.GetInputContractRevision() != managedkubernetes.InputContractRevision || ready.GetFileInputContractRevision() != managedkubernetes.FileInputContractRevision {
		return empty, targetrepo.ErrOperationUnavailable
	}
	response, err := owner.client.KubernetesOperations.PrepareManagedKubernetesFileInput(ctx, &cp.PrepareManagedKubernetesFileInputRequest{Binding: kubernetesHealthBindingToProto(binding), ChildRef: child.Ref})
	if err != nil || ctx.Err() != nil || response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.GetView() == nil || response.GetAuthorizedUntil() == nil || response.GetAuthorizedUntil().CheckValid() != nil || len(response.GetAuthorizedUntil().ProtoReflect().GetUnknown()) != 0 {
		return empty, targetrepo.ErrOperationUnavailable
	}
	view := response.GetView()
	until := response.GetAuthorizedUntil().AsTime()
	if view.GetCreatedAt() == nil || view.GetHardDeadline() == nil || view.GetCreatedAt().CheckValid() != nil || view.GetHardDeadline().CheckValid() != nil || len(view.GetCreatedAt().ProtoReflect().GetUnknown()) != 0 || len(view.GetHardDeadline().ProtoReflect().GetUnknown()) != 0 || !view.GetHardDeadline().AsTime().After(view.GetCreatedAt().AsTime()) || until.After(view.GetHardDeadline().AsTime()) || view.GetArtifactSizeBytes() > 16<<20 {
		return empty, targetrepo.ErrOperationUnavailable
	}
	if len(view.ProtoReflect().GetUnknown()) != 0 || !strings.HasPrefix(view.GetSourceRef(), "kfs_") || !managedkubernetes.ValidLocator(view.GetSourceRef()) || len(view.GetSourceRef()) < 12 || len(view.GetSourceRef()) > 96 || view.GetInvocationRef() != binding.InvocationRef || view.GetPlanRef() != plan.Ref || view.GetChildRef() != child.Ref || view.GetStreamRef() != "" || view.GetState() != cp.ManagedKubernetesFileInputState_MANAGED_KUBERNETES_FILE_INPUT_STATE_READY || view.GetVersion() < 1 || view.GetArtifactRef() != profile.ArtifactRef || view.GetArtifactSha256() != profile.ArtifactSHA256 || view.GetArtifactRevisionRef() == "" || !managedkubernetes.ValidLocator(view.GetArtifactRevisionRef()) || view.GetTransferFormat() != profile.TransferFormat || !managedkubernetes.ValidDigest(view.GetNormalizedSha256()) || view.GetNormalizedSizeBytes() < 1 || view.GetNormalizedSizeBytes() > profile.MaximumBytes || view.GetNormalizationRevision() != managedkubernetes.CopyArchiveNormalizationRevision || view.GetStagedBytes() != 0 || view.GetAcknowledgedBytes() != 0 || view.GetEofAcknowledged() || view.GetReason() != cp.ManagedKubernetesFileInputReason_MANAGED_KUBERNETES_FILE_INPUT_REASON_NONE || !until.After(time.Now()) || until.After(snapshot.ExpiresAt) {
		return empty, targetrepo.ErrOperationUnavailable
	}
	return targetrepo.FileInputPreparation{SourceRef: view.GetSourceRef(), Version: view.GetVersion(), AuthorizedUntil: until}, nil
}
