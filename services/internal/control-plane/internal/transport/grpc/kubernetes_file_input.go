package grpc

import (
	"context"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// kubernetesFileViewToProto validates closed source metadata, preserving historical deadlines.
// No payload, provider receipt, prepared locator or native authority is added by this projection.
// kubernetesFileViewToProto проверяет closed source metadata и сохраняет historical deadlines.
// Payload, provider receipt, prepared locator либо native authority в projection не добавляются.
func kubernetesFileViewToProto(v entity.KubernetesFileInputView) (*cp.ManagedKubernetesFileInputView, error) {
	if !v.Valid() {
		return nil, errs.ErrUnavailable
	}
	state, known := cp.ManagedKubernetesFileInputState_value["MANAGED_KUBERNETES_FILE_INPUT_STATE_"+v.State]
	reason, knownReason := cp.ManagedKubernetesFileInputReason_value["MANAGED_KUBERNETES_FILE_INPUT_REASON_"+v.Reason]
	created, hard := managedKubernetesTimestamp(v.CreatedAt), managedKubernetesTimestamp(v.HardDeadline)
	if !known || state == 0 || !knownReason || reason == 0 || created == nil || hard == nil {
		return nil, errs.ErrUnavailable
	}
	var cleanup *cp.ManagedKubernetesStagingCleanupStatus
	if v.Cleanup != nil {
		var err error
		cleanup, err = kubernetesStagingCleanupToProto(v.Cleanup)
		if err != nil {
			return nil, err
		}
	}
	return &cp.ManagedKubernetesFileInputView{SourceRef: v.SourceRef, InvocationRef: v.InvocationRef, PlanRef: v.PlanRef, ChildRef: v.ChildRef, StreamRef: v.StreamRef, State: cp.ManagedKubernetesFileInputState(state), Version: v.Version, ArtifactRef: v.ArtifactRef, ArtifactRevisionRef: v.ArtifactRevisionRef, ArtifactSha256: v.ArtifactSHA256, ArtifactSizeBytes: v.ArtifactSizeBytes, TransferFormat: v.TransferFormat, NormalizedSha256: v.NormalizedSHA256, NormalizedSizeBytes: v.NormalizedSizeBytes, NormalizationRevision: v.NormalizationRevision, StagedBytes: v.StagedBytes, AcknowledgedBytes: v.AcknowledgedBytes, EofAcknowledged: v.EOFAcknowledged, Reason: cp.ManagedKubernetesFileInputReason(reason), CreatedAt: created, HardDeadline: hard, Cleanup: cleanup}, nil
}

// ListManagedKubernetesFileInputs discovers safe exact-invocation provenance after current original-root admission.
// An overfull, mixed-origin or duplicate response is discarded as a whole.
// ListManagedKubernetesFileInputs раскрывает safe exact-invocation provenance после current original-root admission.
// Overfull, mixed-origin либо duplicate response отбрасывается полностью.
func (server *Server) ListManagedKubernetesFileInputs(ctx context.Context, request *cp.ListManagedKubernetesFileInputsRequest) (*cp.ListManagedKubernetesFileInputsResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesFileService_ListManagedKubernetesFileInputs_FullMethodName, func(p value.Principal) (*cp.ListManagedKubernetesFileInputsResponse, error) {
		if request.GetCleanupOnly() {
			items, err := server.service.ListKubernetesFileCleanup(ctx, p, request.GetInvocationRef())
			if err != nil {
				return nil, err
			}
			if len(items) > 256 {
				return nil, errs.ErrUnavailable
			}
			result := &cp.ListManagedKubernetesFileInputsResponse{Projection: cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_CLEANUP_ONLY, CleanupItems: make([]*cp.ManagedKubernetesFileCleanupView, 0, len(items))}
			seen := make(map[string]bool, len(items))
			for _, item := range items {
				view, err := kubernetesFileCleanupViewToProto(item)
				if err != nil || item.InvocationRef != request.GetInvocationRef() || seen[item.SourceRef] {
					return nil, errs.ErrUnavailable
				}
				seen[item.SourceRef] = true
				result.CleanupItems = append(result.CleanupItems, view)
			}
			return result, nil
		}
		items, err := server.service.ListKubernetesFileInputs(ctx, p, request.GetInvocationRef())
		if err != nil {
			return nil, err
		}
		if len(items) > 256 {
			return nil, errs.ErrUnavailable
		}
		response := &cp.ListManagedKubernetesFileInputsResponse{Projection: cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_SOURCE, Items: make([]*cp.ManagedKubernetesFileInputView, 0, len(items))}
		seen := make(map[string]bool, len(items))
		for _, item := range items {
			view, err := kubernetesFileViewToProto(item)
			if err != nil || view.GetInvocationRef() != request.GetInvocationRef() || seen[item.SourceRef] {
				return nil, errs.ErrUnavailable
			}
			seen[item.SourceRef] = true
			response.Items = append(response.Items, view)
		}
		return response, nil
	})
}

// GetManagedKubernetesFileInput reads one original owner source without materializing or replaying input.
// GetManagedKubernetesFileInput читает original owner source без materialization либо replay input.
func (server *Server) GetManagedKubernetesFileInput(ctx context.Context, request *cp.GetManagedKubernetesFileInputRequest) (*cp.GetManagedKubernetesFileInputResponse, error) {
	if !managedKubernetesWireValid(request) || !request.GetCleanupOnly() && request.GetCleanupIdempotencyKey() != "" {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesFileService_GetManagedKubernetesFileInput_FullMethodName, func(p value.Principal) (*cp.GetManagedKubernetesFileInputResponse, error) {
		if request.GetCleanupOnly() {
			result, err := server.service.GetKubernetesFileCleanup(ctx, p, query.KubernetesFileCleanupRead{InvocationRef: request.GetInvocationRef(), SourceRef: request.GetSourceRef(), CleanupIdempotencyKey: request.GetCleanupIdempotencyKey()})
			if err != nil {
				return nil, err
			}
			view, receipt, err := kubernetesFileCleanupReadbackToProto(result)
			if err != nil || result.View.InvocationRef != request.GetInvocationRef() || result.View.SourceRef != request.GetSourceRef() || request.GetCleanupIdempotencyKey() == "" && receipt != nil {
				return nil, errs.ErrUnavailable
			}
			return &cp.GetManagedKubernetesFileInputResponse{Projection: cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_CLEANUP_ONLY, CleanupView: view, CleanupReceipt: receipt}, nil
		}
		item, err := server.service.GetKubernetesFileInput(ctx, p, request.GetInvocationRef(), request.GetSourceRef())
		if err != nil {
			return nil, err
		}
		view, err := kubernetesFileViewToProto(item)
		if err != nil || view.GetInvocationRef() != request.GetInvocationRef() || view.GetSourceRef() != request.GetSourceRef() {
			return nil, errs.ErrUnavailable
		}
		return &cp.GetManagedKubernetesFileInputResponse{Projection: cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_SOURCE, View: view}, nil
	})
}

// kubernetesFileCleanupViewToProto projects only minimal closure metadata, independent of source visibility.
// A FILE owns at most one normalized object; metadata frames never increase physical object counts.
// kubernetesFileCleanupViewToProto раскрывает лишь minimal closure metadata независимо от source visibility.
// FILE владеет не более чем одним normalized object; metadata frames не увеличивают physical object counts.
func kubernetesFileCleanupViewToProto(v entity.KubernetesFileCleanupView) (*cp.ManagedKubernetesFileCleanupView, error) {
	if !strings.HasPrefix(v.SourceRef, "kfs_") || len(v.SourceRef) < 12 || len(v.SourceRef) > 96 || !managedkubernetes.ValidLocator(v.SourceRef) || !strings.HasPrefix(v.InvocationRef, "inv_") || len(v.InvocationRef) < 12 || len(v.InvocationRef) > 96 || !managedkubernetes.ValidLocator(v.InvocationRef) || v.Cleanup.OutstandingCount > 1 || v.Cleanup.OutstandingBytes > 16<<20 {
		return nil, errs.ErrUnavailable
	}
	cleanup, err := kubernetesStagingCleanupToProto(&v.Cleanup)
	if err != nil {
		return nil, err
	}
	return &cp.ManagedKubernetesFileCleanupView{SourceRef: v.SourceRef, InvocationRef: v.InvocationRef, Cleanup: cleanup}, nil
}

// kubernetesFileCleanupReadbackToProto binds the retained acknowledgement to the same source and current repair counters.
// Neither current counters nor an accepted repair receipt prove deletion or renew execution authority.
// kubernetesFileCleanupReadbackToProto связывает retained acknowledgement с тем же source и current repair counters.
// Current counters и accepted repair receipt не доказывают deletion и не продлевают execution authority.
func kubernetesFileCleanupReadbackToProto(result entity.KubernetesFileCleanupReadback) (*cp.ManagedKubernetesFileCleanupView, *cp.ManagedKubernetesFileCleanupRepairReceipt, error) {
	view, err := kubernetesFileCleanupViewToProto(result.View)
	if err != nil {
		return nil, nil, err
	}
	r := result.Receipt
	if r == nil {
		return view, nil, nil
	}
	if r.SourceRef != result.View.SourceRef || r.Version < 2 || r.Version > result.View.Cleanup.Version || r.RepairGeneration < 1 || r.RepairGeneration > result.View.Cleanup.RepairGeneration || r.RepairedCount != 1 || r.RemainingExhaustedCount < 0 || r.RemainingExhaustedCount > 1 || r.AcceptedAt.IsZero() || r.AcceptedAt.After(time.Now().Add(30*time.Second)) {
		return nil, nil, errs.ErrUnavailable
	}
	accepted := managedKubernetesTimestamp(r.AcceptedAt)
	if accepted == nil {
		return nil, nil, errs.ErrUnavailable
	}
	return view, &cp.ManagedKubernetesFileCleanupRepairReceipt{SourceRef: r.SourceRef, Version: r.Version, RepairGeneration: r.RepairGeneration, RepairedCount: r.RepairedCount, RemainingExhaustedCount: r.RemainingExhaustedCount, AcceptedAt: accepted}, nil
}

// RepairManagedKubernetesFileCleanup admits one terminal cleanup cycle without reading source bytes or reopening native work.
// Lost acknowledgement is recovered only by cleanup-only Get(original key), never automatic POST replay.
// RepairManagedKubernetesFileCleanup допускает terminal cleanup cycle без чтения source bytes либо открытия native work.
// Lost acknowledgement восстанавливается только cleanup-only Get(original key), не automatic POST replay.
func (server *Server) RepairManagedKubernetesFileCleanup(ctx context.Context, request *cp.RepairManagedKubernetesFileCleanupRequest) (*cp.RepairManagedKubernetesFileCleanupResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesFileService_RepairManagedKubernetesFileCleanup_FullMethodName, func(p value.Principal) (*cp.RepairManagedKubernetesFileCleanupResponse, error) {
		result, err := server.service.RepairKubernetesFileCleanup(ctx, p, mutation(request.GetMutation()), command.KubernetesFileCleanupRepair{InvocationRef: request.GetInvocationRef(), SourceRef: request.GetSourceRef()})
		if err != nil {
			return nil, err
		}
		view, receipt, err := kubernetesFileCleanupReadbackToProto(result)
		if err != nil || receipt == nil || result.View.InvocationRef != request.GetInvocationRef() || result.View.SourceRef != request.GetSourceRef() {
			return nil, errs.ErrUnavailable
		}
		return &cp.RepairManagedKubernetesFileCleanupResponse{Projection: cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_CLEANUP_ONLY, CleanupView: view, CleanupReceipt: receipt}, nil
	})
}

// PrepareManagedKubernetesFileInput invokes only the protected source owner before native Begin.
// The request carries an original binding and child, never bytes, actor, artifact or storage selectors.
// PrepareManagedKubernetesFileInput вызывает лишь protected source owner до native Begin.
// Request несёт original binding и child, не bytes, actor, artifact либо storage selectors.
func (server *Server) PrepareManagedKubernetesFileInput(ctx context.Context, request *cp.PrepareManagedKubernetesFileInputRequest) (*cp.PrepareManagedKubernetesFileInputResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_PrepareManagedKubernetesFileInput_FullMethodName, func(p value.Principal) (*cp.PrepareManagedKubernetesFileInputResponse, error) {
		result, err := server.service.PrepareManagedKubernetesFileInput(ctx, p, managedKubernetesBindingFromProto(request.GetBinding()), request.GetChildRef())
		if err != nil {
			return nil, err
		}
		view, err := kubernetesFileViewToProto(result.View)
		until := managedKubernetesTimestamp(result.AuthorizedUntil)
		if err != nil || until == nil || !result.AuthorizedUntil.After(time.Now()) || result.View.State != "READY" || view.GetInvocationRef() != request.GetBinding().GetInvocationRef() || view.GetChildRef() != request.GetChildRef() {
			return nil, errs.ErrUnavailable
		}
		return &cp.PrepareManagedKubernetesFileInputResponse{View: view, AuthorizedUntil: until}, nil
	})
}
