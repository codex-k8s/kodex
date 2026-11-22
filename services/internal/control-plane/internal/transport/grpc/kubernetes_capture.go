package grpc

import (
	"context"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/opaqueref"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// captureTaskBindingFromProto preserves exact task coordinates without deriving an actor or grant.
// captureTaskBindingFromProto сохраняет exact task coordinates без вывода actor либо grant.
func captureTaskBindingFromProto(input *cp.ManagedKubernetesCaptureTaskBinding) value.ManagedKubernetesCaptureTaskBinding {
	return value.ManagedKubernetesCaptureTaskBinding{CaptureRef: input.GetCaptureRef(), LeaseRef: input.GetLeaseRef(), Fence: input.GetFence(), Generation: input.GetGeneration(), WorkloadInstance: input.GetWorkloadInstance(), SnapshotDigest: input.GetSnapshotDigest()}
}

// FinalizeManagedKubernetesCapture admits only a metadata task binding; all output is owner-derived.
// FinalizeManagedKubernetesCapture принимает только metadata task binding; весь output owner-derived.
func (server *Server) FinalizeManagedKubernetesCapture(ctx context.Context, request *cp.FinalizeManagedKubernetesCaptureRequest) (*cp.FinalizeManagedKubernetesCaptureResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_FinalizeManagedKubernetesCapture_FullMethodName, func(p value.Principal) (*cp.FinalizeManagedKubernetesCaptureResponse, error) {
		binding := captureTaskBindingFromProto(request.GetBinding())
		receipt, err := server.service.FinalizeManagedKubernetesCapture(ctx, p, binding)
		if err != nil {
			return nil, err
		}
		result := kubernetesCaptureReceiptToProto(receipt, binding)
		if result == nil {
			return nil, errs.ErrUnavailable
		}
		return &cp.FinalizeManagedKubernetesCaptureResponse{Receipt: result}, nil
	})
}

// GetManagedKubernetesCapture exposes exact task readback without payload or a refreshed lease.
// GetManagedKubernetesCapture выдаёт exact task readback без payload либо refreshed lease.
func (server *Server) GetManagedKubernetesCapture(ctx context.Context, request *cp.GetManagedKubernetesCaptureRequest) (*cp.GetManagedKubernetesCaptureResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_GetManagedKubernetesCapture_FullMethodName, func(p value.Principal) (*cp.GetManagedKubernetesCaptureResponse, error) {
		binding := captureTaskBindingFromProto(request.GetBinding())
		receipt, err := server.service.GetManagedKubernetesCapture(ctx, p, binding)
		if err != nil {
			return nil, err
		}
		result := kubernetesCaptureReceiptToProto(receipt, binding)
		if result == nil {
			return nil, errs.ErrUnavailable
		}
		return &cp.GetManagedKubernetesCaptureResponse{Receipt: result}, nil
	})
}

// RenewManagedKubernetesCapture returns only the owner's current task deadline for the same attempt.
// RenewManagedKubernetesCapture возвращает только owner current task deadline той же попытки.
func (server *Server) RenewManagedKubernetesCapture(ctx context.Context, request *cp.RenewManagedKubernetesCaptureRequest) (*cp.RenewManagedKubernetesCaptureResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_RenewManagedKubernetesCapture_FullMethodName, func(p value.Principal) (*cp.RenewManagedKubernetesCaptureResponse, error) {
		binding := captureTaskBindingFromProto(request.GetBinding())
		expires, err := server.service.RenewManagedKubernetesCapture(ctx, p, binding)
		if err != nil {
			return nil, err
		}
		timestamp := timestamppb.New(expires)
		if !expires.After(time.Now()) || timestamp.CheckValid() != nil {
			return nil, errs.ErrUnavailable
		}
		return &cp.RenewManagedKubernetesCaptureResponse{LeaseRef: binding.LeaseRef, Generation: binding.Generation, ExpiresAt: timestamp}, nil
	})
}

// CheckManagedKubernetesCaptureReadiness verifies the producer contract without assuming a live worker.
// CheckManagedKubernetesCaptureReadiness проверяет producer contract без предположения live worker.
func (server *Server) CheckManagedKubernetesCaptureReadiness(ctx context.Context, request *cp.CheckManagedKubernetesCaptureReadinessRequest) (*cp.CheckManagedKubernetesCaptureReadinessResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_CheckManagedKubernetesCaptureReadiness_FullMethodName, func(p value.Principal) (*cp.CheckManagedKubernetesCaptureReadinessResponse, error) {
		revision, err := server.service.CheckManagedKubernetesCaptureReadiness(ctx, p)
		if err != nil {
			return nil, err
		}
		if revision != 0 && revision != managedkubernetes.CaptureContractRevision {
			return nil, errs.ErrUnavailable
		}
		return &cp.CheckManagedKubernetesCaptureReadinessResponse{Ready: revision == managedkubernetes.CaptureContractRevision, ContractRevision: revision}, nil
	})
}

// kubernetesCaptureReceiptToProto rejects unknown/mixed task pins and unbound artifact metadata.
// It deliberately carries no authorization deadline, object locator or native success assertion.
// kubernetesCaptureReceiptToProto отклоняет unknown/mixed task pins и unbound artifact metadata.
// Не передаёт authorization deadline, object locator либо native success assertion.
func kubernetesCaptureReceiptToProto(receipt entity.ManagedKubernetesCaptureReceipt, binding value.ManagedKubernetesCaptureTaskBinding) *cp.ManagedKubernetesCaptureReceipt {
	if receipt.ValidateFor(binding) != nil {
		return nil
	}
	digest, err := binding.Digest()
	if err != nil || receipt.CaptureRef != binding.CaptureRef || receipt.SnapshotDigest != binding.SnapshotDigest || receipt.TaskGeneration != binding.Generation || receipt.TaskBindingDigest != digest || receipt.CaptureGeneration < 1 || !opaqueref.Valid(receipt.StreamRef, "kst") {
		return nil
	}
	states := map[enum.ManagedKubernetesCaptureState]cp.ManagedKubernetesCaptureState{
		enum.KubernetesCaptureReady:        cp.ManagedKubernetesCaptureState_MANAGED_KUBERNETES_CAPTURE_STATE_READY,
		enum.KubernetesCaptureClaimed:      cp.ManagedKubernetesCaptureState_MANAGED_KUBERNETES_CAPTURE_STATE_CLAIMED,
		enum.KubernetesCaptureComplete:     cp.ManagedKubernetesCaptureState_MANAGED_KUBERNETES_CAPTURE_STATE_COMPLETE,
		enum.KubernetesCaptureUnknown:      cp.ManagedKubernetesCaptureState_MANAGED_KUBERNETES_CAPTURE_STATE_UNKNOWN,
		enum.KubernetesCaptureRejected:     cp.ManagedKubernetesCaptureState_MANAGED_KUBERNETES_CAPTURE_STATE_REJECTED,
		enum.KubernetesCaptureWaitingOwner: cp.ManagedKubernetesCaptureState_MANAGED_KUBERNETES_CAPTURE_STATE_WAITING_OWNER,
		enum.KubernetesCaptureSuperseded:   cp.ManagedKubernetesCaptureState_MANAGED_KUBERNETES_CAPTURE_STATE_SUPERSEDED,
	}
	state, known := states[receipt.State]
	if !known || !managedkubernetes.ValidCaptureReason(receipt.ReasonCode) {
		return nil
	}
	kind := cp.ManagedKubernetesCaptureKind_MANAGED_KUBERNETES_CAPTURE_KIND_UNSPECIFIED
	if receipt.State == enum.KubernetesCaptureComplete {
		switch receipt.Kind {
		case enum.KubernetesCaptureEmpty:
			kind = cp.ManagedKubernetesCaptureKind_MANAGED_KUBERNETES_CAPTURE_KIND_EMPTY
		case enum.KubernetesCaptureFull:
			kind = cp.ManagedKubernetesCaptureKind_MANAGED_KUBERNETES_CAPTURE_KIND_FULL
		case enum.KubernetesCapturePartial:
			kind = cp.ManagedKubernetesCaptureKind_MANAGED_KUBERNETES_CAPTURE_KIND_PARTIAL
		default:
			return nil
		}
	} else if receipt.Kind != "" {
		return nil
	}
	if len(receipt.Artifacts) > 5 || receipt.Kind == enum.KubernetesCaptureEmpty && len(receipt.Artifacts) != 0 || receipt.Kind == enum.KubernetesCaptureFull && len(receipt.Artifacts) == 0 {
		return nil
	}
	result := &cp.ManagedKubernetesCaptureReceipt{CaptureRef: receipt.CaptureRef, StreamRef: receipt.StreamRef, SnapshotDigest: receipt.SnapshotDigest, TaskBindingDigest: receipt.TaskBindingDigest, TaskGeneration: receipt.TaskGeneration, CaptureGeneration: receipt.CaptureGeneration, State: state, Kind: kind, ReasonCode: receipt.ReasonCode}
	seen := map[string]bool{}
	for _, artifact := range receipt.Artifacts {
		if !(artifact.Channel == "STDOUT" || artifact.Channel == "STDERR" || artifact.Channel == "CONTROL" || artifact.Channel == "PORT_FORWARD" || artifact.Channel == "FILE_CONTENT") || seen[artifact.Channel] || !opaqueref.Valid(artifact.ArtifactRef, "art") || !opaqueref.Valid(artifact.RevisionRef, "arv") || !objectstorage.ValidDigest(artifact.Digest) || artifact.SizeBytes < 0 || artifact.SizeBytes > platformrepo.MaximumArtifactBytes || artifact.CaptureGeneration != receipt.CaptureGeneration || receipt.Kind == enum.KubernetesCaptureFull && artifact.Partial {
			return nil
		}
		seen[artifact.Channel] = true
		result.Artifacts = append(result.Artifacts, &cp.ManagedKubernetesCapturedArtifact{Channel: artifact.Channel, ArtifactRef: artifact.ArtifactRef, RevisionRef: artifact.RevisionRef, Digest: artifact.Digest, SizeBytes: artifact.SizeBytes, CaptureGeneration: artifact.CaptureGeneration, Partial: artifact.Partial})
	}
	return result
}
