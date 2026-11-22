package grpc

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ListManagedKubernetesCaptures returns bounded safe history independently of Tool/integration authority.
// Every owner result is validated before transport; no task receipt or payload is accepted from callers.
// ListManagedKubernetesCaptures возвращает bounded safe history независимо от Tool/integration authority.
// Каждый owner result проверяется до transport; task receipt либо payload от caller не принимаются.
func (server *Server) ListManagedKubernetesCaptures(ctx context.Context, request *cp.ListManagedKubernetesCapturesRequest) (*cp.ListManagedKubernetesCapturesResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesCaptureService_ListManagedKubernetesCaptures_FullMethodName, func(p value.Principal) (*cp.ListManagedKubernetesCapturesResponse, error) {
		paging := page(request.GetPage())
		result, err := server.service.ListKubernetesCaptures(ctx, p, query.KubernetesCaptureList{RunRef: request.GetRunRef(), NodeRef: request.GetNodeRef(), Page: paging})
		if err != nil {
			return nil, err
		}
		if len(result.Items) > int(paging.Size) || result.Next != "" && !kubernetesInputPublicRef(result.Next, "koc_") {
			return nil, errs.ErrUnavailable
		}
		response := &cp.ListManagedKubernetesCapturesResponse{Items: make([]*cp.ManagedKubernetesCaptureStatus, 0, len(result.Items)), Page: &cp.PageInfo{NextPageToken: result.Next}}
		seen := map[string]bool{}
		previous := paging.Token
		for _, v := range result.Items {
			view, _, err := kubernetesCaptureStatusToProto(v, nil)
			if err != nil || v.RunRef != request.GetRunRef() || v.NodeRef != request.GetNodeRef() || seen[v.CaptureRef] || v.CaptureRef <= previous {
				return nil, errs.ErrUnavailable
			}
			seen[v.CaptureRef] = true
			previous = v.CaptureRef
			response.Items = append(response.Items, view)
		}
		if result.Next != "" && (len(result.Items) != int(paging.Size) || result.Next != previous) {
			return nil, errs.ErrUnavailable
		}
		return response, nil
	})
}

// kubernetesCaptureSummaryToProto validates bounded owner-discovery metadata; a capture state is
// technical publication progress and cannot prove native outcome or current execution authority.
// kubernetesCaptureSummaryToProto проверяет bounded owner-discovery metadata; capture state
// описывает publication progress, не native outcome либо current execution authority.
func kubernetesCaptureSummaryToProto(v entity.KubernetesCaptureSummary) (*cp.ManagedKubernetesCaptureSummary, error) {
	state, known := cp.ManagedKubernetesCaptureState_value["MANAGED_KUBERNETES_CAPTURE_STATE_"+string(v.State)]
	if !known || state == 0 || !kubernetesInputPublicRef(v.CaptureRef, "koc_") || !kubernetesInputPublicRef(v.StreamRef, "kst_") || v.Version < 1 || uint64(v.Version) > maximumKubernetesInputPublicSequence || v.CaptureGeneration < 1 || uint64(v.CaptureGeneration) > maximumKubernetesInputPublicSequence || !managedkubernetes.ValidCaptureReason(v.ReasonCode) {
		return nil, errs.ErrUnavailable
	}
	return &cp.ManagedKubernetesCaptureSummary{CaptureRef: v.CaptureRef, StreamRef: v.StreamRef, CaptureGeneration: v.CaptureGeneration, Version: v.Version, State: cp.ManagedKubernetesCaptureState(state), ReasonCode: v.ReasonCode}, nil
}

// kubernetesCaptureStatusToProto refuses mixed state, provenance, artifact or repair metadata.
// Public metadata contains no task binding, original fence, source bytes or object coordinates.
// kubernetesCaptureStatusToProto отклоняет mixed state, provenance, artifact либо repair metadata.
// Public metadata не содержит task binding, original fence, source bytes либо object coordinates.
func kubernetesCaptureStatusToProto(v entity.KubernetesCaptureStatus, receipt *entity.KubernetesCaptureRepairReceipt) (*cp.ManagedKubernetesCaptureStatus, *cp.ManagedKubernetesCaptureRepairReceipt, error) {
	summary, err := kubernetesCaptureSummaryToProto(v.KubernetesCaptureSummary)
	if err != nil {
		return nil, nil, err
	}
	operation, known := integrationpackage.KubernetesOperationByKey(v.Operation)
	if !known || operation.Execution != integrationpackage.KubernetesStream || !kubernetesInputPublicRef(v.InvocationRef, "inv_") || !kubernetesInputPublicRef(v.RunRef, "run_") || !kubernetesInputPublicRef(v.NodeRef, "nod_") || v.AttemptCount < 0 || uint64(v.AttemptCount) > maximumKubernetesInputPublicSequence || v.RepairGeneration < 0 || uint64(v.RepairGeneration) > maximumKubernetesInputPublicSequence || v.SealedAt.IsZero() || len(v.Artifacts) > 5 {
		return nil, nil, errs.ErrUnavailable
	}
	result := &cp.ManagedKubernetesCaptureStatus{Summary: summary, InvocationRef: v.InvocationRef, RunRef: v.RunRef, NodeRef: v.NodeRef, Operation: v.Operation, AttemptCount: v.AttemptCount, RepairGeneration: v.RepairGeneration, SealedAt: timestamppb.New(v.SealedAt)}
	if result.SealedAt.CheckValid() != nil {
		return nil, nil, errs.ErrUnavailable
	}
	if v.RetainUntil != nil {
		result.RetainUntil = timestamppb.New(*v.RetainUntil)
		if result.RetainUntil.CheckValid() != nil || !v.RetainUntil.After(v.SealedAt) {
			return nil, nil, errs.ErrUnavailable
		}
	}
	kind, kindKnown := cp.ManagedKubernetesCaptureKind_value["MANAGED_KUBERNETES_CAPTURE_KIND_"+string(v.Kind)]
	if v.State == enum.KubernetesCaptureComplete {
		if !kindKnown || kind == 0 || v.Kind == enum.KubernetesCaptureEmpty && len(v.Artifacts) != 0 || v.Kind == enum.KubernetesCaptureFull && len(v.Artifacts) == 0 {
			return nil, nil, errs.ErrUnavailable
		}
		result.Kind = cp.ManagedKubernetesCaptureKind(kind)
	} else if v.Kind != "" || len(v.Artifacts) != 0 {
		return nil, nil, errs.ErrUnavailable
	}
	seen := map[string]bool{}
	for _, a := range v.Artifacts {
		if seen[a.Channel] || !capturePublicChannel(a.Channel) || !kubernetesInputPublicRef(a.ArtifactRef, "art_") || !kubernetesInputPublicRef(a.RevisionRef, "arv_") || !objectstorage.ValidDigest(a.Digest) || a.SizeBytes < 0 || a.SizeBytes > platformrepo.MaximumArtifactBytes || a.CaptureGeneration != v.CaptureGeneration || v.Kind == enum.KubernetesCaptureFull && a.Partial {
			return nil, nil, errs.ErrUnavailable
		}
		seen[a.Channel] = true
		result.Artifacts = append(result.Artifacts, &cp.ManagedKubernetesCapturedArtifact{Channel: a.Channel, ArtifactRef: a.ArtifactRef, RevisionRef: a.RevisionRef, Digest: a.Digest, SizeBytes: a.SizeBytes, CaptureGeneration: a.CaptureGeneration, Partial: a.Partial})
	}
	var repair *cp.ManagedKubernetesCaptureRepairReceipt
	if receipt != nil {
		if receipt.CaptureRef != v.CaptureRef || receipt.RepairGeneration < 1 || receipt.RepairGeneration > v.RepairGeneration || receipt.Version < 2 || receipt.Version > v.Version || receipt.AttemptCount < 0 || receipt.AttemptCount > v.AttemptCount || receipt.AcceptedAt.Before(v.SealedAt) {
			return nil, nil, errs.ErrUnavailable
		}
		repair = &cp.ManagedKubernetesCaptureRepairReceipt{CaptureRef: receipt.CaptureRef, RepairGeneration: receipt.RepairGeneration, Version: receipt.Version, AttemptCount: receipt.AttemptCount, AcceptedAt: timestamppb.New(receipt.AcceptedAt)}
		if repair.AcceptedAt.CheckValid() != nil {
			return nil, nil, errs.ErrUnavailable
		}
	}
	if v.Cleanup != nil {
		cleanup, err := kubernetesStagingCleanupToProto(v.Cleanup)
		if err != nil {
			return nil, nil, err
		}
		result.Cleanup = cleanup
	}
	return result, repair, nil
}

// capturePublicChannel accepts only the registered captured output classes, not request data.
// capturePublicChannel допускает только registered captured output classes, не request data.
func capturePublicChannel(channel string) bool {
	switch channel {
	case "STDOUT", "STDERR", "CONTROL", "PORT_FORWARD", "FILE_CONTENT":
		return true
	default:
		return false
	}
}

// GetManagedKubernetesCaptureStatus performs current owner visibility and exact lost-ACK readback.
// GetManagedKubernetesCaptureStatus выполняет current owner visibility и exact lost-ACK readback.
func (server *Server) GetManagedKubernetesCaptureStatus(ctx context.Context, request *cp.GetManagedKubernetesCaptureStatusRequest) (*cp.GetManagedKubernetesCaptureStatusResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesCaptureService_GetManagedKubernetesCaptureStatus_FullMethodName, func(p value.Principal) (*cp.GetManagedKubernetesCaptureStatusResponse, error) {
		result, err := server.service.GetKubernetesCaptureStatus(ctx, p, query.KubernetesCaptureStatusRead{CaptureRef: request.GetCaptureRef(), RepairIdempotencyKey: request.GetRepairIdempotencyKey(), CleanupIdempotencyKey: request.GetCleanupIdempotencyKey()})
		if err != nil {
			return nil, err
		}
		view, receipt, err := kubernetesCaptureStatusToProto(result.View, result.RepairReceipt)
		if err != nil || result.View.CaptureRef != request.GetCaptureRef() || request.GetRepairIdempotencyKey() == "" && receipt != nil {
			return nil, errs.ErrUnavailable
		}
		cleanupReceipt, err := kubernetesStagingCleanupReceiptToProto(result.View, result.CleanupReceipt)
		if err != nil || request.GetCleanupIdempotencyKey() == "" && cleanupReceipt != nil {
			return nil, errs.ErrUnavailable
		}
		return &cp.GetManagedKubernetesCaptureStatusResponse{View: view, RepairReceipt: receipt, CleanupReceipt: cleanupReceipt}, nil
	})
}

// RepairManagedKubernetesCapture admits only a purpose-specific metadata repair with owner OCC.
// A missing acknowledgement is recovered by Get(original key), never an automatic mutation retry.
// RepairManagedKubernetesCapture допускает лишь purpose-specific metadata repair с owner OCC.
// Потерянный acknowledgement восстанавливается Get(original key), не automatic mutation retry.
func (server *Server) RepairManagedKubernetesCapture(ctx context.Context, request *cp.RepairManagedKubernetesCaptureRequest) (*cp.RepairManagedKubernetesCaptureResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesCaptureService_RepairManagedKubernetesCapture_FullMethodName, func(p value.Principal) (*cp.RepairManagedKubernetesCaptureResponse, error) {
		result, err := server.service.RepairKubernetesCapture(ctx, p, mutation(request.GetMutation()), command.KubernetesCaptureRepair{CaptureRef: request.GetCaptureRef()})
		if err != nil {
			return nil, err
		}
		view, receipt, err := kubernetesCaptureStatusToProto(result.View, result.RepairReceipt)
		if err != nil || receipt == nil || result.View.CaptureRef != request.GetCaptureRef() {
			return nil, errs.ErrUnavailable
		}
		return &cp.RepairManagedKubernetesCaptureResponse{View: view, RepairReceipt: receipt}, nil
	})
}

// kubernetesStagingCleanupToProto validates physical counters without exposing cleanup object pins.
// kubernetesStagingCleanupToProto проверяет physical counters без выдачи cleanup object pins.
func kubernetesStagingCleanupToProto(v *entity.KubernetesStagingCleanupStatus) (*cp.ManagedKubernetesStagingCleanupStatus, error) {
	if v == nil || v.Version < 1 || v.RepairGeneration < 0 || v.OutstandingCount < 0 || v.OutstandingBytes < 0 || v.ExhaustedCount < 0 || v.ExhaustedCount > v.OutstandingCount || v.EligibleExhaustedCount < 0 || v.EligibleExhaustedCount > v.ExhaustedCount {
		return nil, errs.ErrUnavailable
	}
	for _, n := range []int64{v.Version, v.RepairGeneration, v.OutstandingCount, v.OutstandingBytes} {
		if uint64(n) > maximumKubernetesInputPublicSequence {
			return nil, errs.ErrUnavailable
		}
	}
	return &cp.ManagedKubernetesStagingCleanupStatus{Version: v.Version, RepairGeneration: v.RepairGeneration, OutstandingCount: v.OutstandingCount, OutstandingBytes: v.OutstandingBytes, ExhaustedCount: v.ExhaustedCount, EligibleExhaustedCount: v.EligibleExhaustedCount}, nil
}

// kubernetesStagingCleanupReceiptToProto checks the immutable acknowledgement against current repair metadata.
// kubernetesStagingCleanupReceiptToProto проверяет immutable acknowledgement по current repair metadata.
func kubernetesStagingCleanupReceiptToProto(v entity.KubernetesCaptureStatus, r *entity.KubernetesStagingCleanupRepairReceipt) (*cp.ManagedKubernetesStagingCleanupRepairReceipt, error) {
	if r == nil {
		return nil, nil
	}
	if v.Cleanup == nil || r.CaptureRef != v.CaptureRef || r.Version < 2 || r.Version > v.Cleanup.Version || r.RepairGeneration < 1 || r.RepairGeneration > v.Cleanup.RepairGeneration || r.RepairedCount < 1 || r.RepairedCount > 100 || r.RemainingExhaustedCount < 0 || uint64(r.RemainingExhaustedCount) > maximumKubernetesInputPublicSequence || r.AcceptedAt.Before(v.SealedAt) {
		return nil, errs.ErrUnavailable
	}
	accepted := timestamppb.New(r.AcceptedAt)
	if accepted.CheckValid() != nil {
		return nil, errs.ErrUnavailable
	}
	return &cp.ManagedKubernetesStagingCleanupRepairReceipt{CaptureRef: r.CaptureRef, Version: r.Version, RepairGeneration: r.RepairGeneration, RepairedCount: r.RepairedCount, RemainingExhaustedCount: r.RemainingExhaustedCount, AcceptedAt: accepted}, nil
}

// RepairManagedKubernetesStagingCleanup opens a bounded storage cleanup cycle, never native execution.
// Missing ACK is reconciled by Get(original cleanup key); the RPC performs no object deletion.
// RepairManagedKubernetesStagingCleanup открывает bounded storage cleanup cycle, не native execution.
// Missing ACK сверяется Get(original cleanup key); RPC не выполняет object deletion.
func (server *Server) RepairManagedKubernetesStagingCleanup(ctx context.Context, request *cp.RepairManagedKubernetesStagingCleanupRequest) (*cp.RepairManagedKubernetesStagingCleanupResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesCaptureService_RepairManagedKubernetesStagingCleanup_FullMethodName, func(p value.Principal) (*cp.RepairManagedKubernetesStagingCleanupResponse, error) {
		result, err := server.service.RepairKubernetesStagingCleanup(ctx, p, mutation(request.GetMutation()), command.KubernetesCaptureRepair{CaptureRef: request.GetCaptureRef()})
		if err != nil {
			return nil, err
		}
		view, _, err := kubernetesCaptureStatusToProto(result.View, nil)
		if err != nil || result.View.CaptureRef != request.GetCaptureRef() {
			return nil, errs.ErrUnavailable
		}
		receipt, err := kubernetesStagingCleanupReceiptToProto(result.View, result.CleanupReceipt)
		if err != nil || receipt == nil {
			return nil, errs.ErrUnavailable
		}
		return &cp.RepairManagedKubernetesStagingCleanupResponse{View: view, CleanupReceipt: receipt}, nil
	})
}
