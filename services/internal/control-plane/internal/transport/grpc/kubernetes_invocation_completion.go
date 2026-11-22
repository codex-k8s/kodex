package grpc

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// CompleteManagedKubernetesInvocation finalizes only owner-stored evidence for the original claim.
// The request has no outcome, result bytes or caller identity selector.
// CompleteManagedKubernetesInvocation завершает лишь owner-stored evidence original claim.
// Request не содержит outcome, result bytes либо caller identity selector.
func (server *Server) CompleteManagedKubernetesInvocation(ctx context.Context, request *cp.CompleteManagedKubernetesInvocationRequest) (*cp.CompleteManagedKubernetesInvocationResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_CompleteManagedKubernetesInvocation_FullMethodName, func(p value.Principal) (*cp.CompleteManagedKubernetesInvocationResponse, error) {
		preflight := ""
		switch request.GetPreflightRejection() {
		case cp.ManagedKubernetesPreflightRejection_MANAGED_KUBERNETES_PREFLIGHT_REJECTION_UNSPECIFIED:
		case cp.ManagedKubernetesPreflightRejection_MANAGED_KUBERNETES_PREFLIGHT_REJECTION_PORT_FORWARD_PROFILE_UNSUPPORTED:
			preflight = managedkubernetes.ReasonPortForwardProfileUnsupported
		default:
			return nil, errs.ErrInvalid
		}
		binding := managedKubernetesBindingFromProto(request.GetBinding())
		result, err := server.service.CompleteManagedKubernetesInvocation(ctx, p, binding, command.ManagedKubernetesCompleteInvocation{PlanRef: request.GetPlanRef(), PlanRevision: request.GetPlanRevision(), PlanDigest: request.GetPlanDigest(), PreflightRejection: preflight})
		if err != nil {
			return nil, err
		}
		receipt := managedKubernetesInvocationReceiptToProto(result, binding)
		if receipt == nil {
			return nil, errs.ErrUnavailable
		}
		return &cp.CompleteManagedKubernetesInvocationResponse{Receipt: receipt}, nil
	})
}

// GetManagedKubernetesInvocation returns only claim-bound reconciliation metadata.
// It cannot provide cached target data, another lease or output-delivery authority.
// GetManagedKubernetesInvocation возвращает лишь claim-bound reconciliation metadata.
// Не выдаёт cached target data, другой lease либо output-delivery authority.
func (server *Server) GetManagedKubernetesInvocation(ctx context.Context, request *cp.GetManagedKubernetesInvocationRequest) (*cp.GetManagedKubernetesInvocationResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_GetManagedKubernetesInvocation_FullMethodName, func(p value.Principal) (*cp.GetManagedKubernetesInvocationResponse, error) {
		binding := managedKubernetesBindingFromProto(request.GetBinding())
		result, err := server.service.GetManagedKubernetesInvocation(ctx, p, binding)
		if err != nil {
			return nil, err
		}
		receipt := managedKubernetesInvocationReceiptToProto(result, binding)
		if receipt == nil {
			return nil, errs.ErrUnavailable
		}
		return &cp.GetManagedKubernetesInvocationResponse{Receipt: receipt}, nil
	})
}

// managedKubernetesInvocationReceiptToProto rejects mixed/noPlan pins and false success promotion.
// A closed owner state without a stored completion remains explicitly unrecorded metadata.
// managedKubernetesInvocationReceiptToProto отклоняет mixed/noPlan pins и false success promotion.
// Closed owner state без stored completion остаётся явно unrecorded metadata.
func managedKubernetesInvocationReceiptToProto(receipt entity.ManagedKubernetesInvocationReceipt, binding value.ManagedKubernetesBinding) *cp.ManagedKubernetesInvocationReceipt {
	digest, err := managedkubernetes.BindingDigest(binding)
	if err != nil || receipt.InvocationRef != binding.InvocationRef || receipt.Generation != binding.Generation || receipt.InputDigest != binding.InputDigest || receipt.BindingDigest != digest || !receipt.State.Known() {
		return nil
	}
	if receipt.PlanRef == "" {
		if receipt.PlanRevision != 0 || receipt.PlanDigest != "" || receipt.SnapshotDigest != "" || receipt.CompletionRecorded && receipt.State != enum.KubernetesInvocationFailed {
			return nil
		}
	} else if !managedkubernetes.ValidLocator(receipt.PlanRef) || len(receipt.PlanRef) > 96 || receipt.PlanRevision < 1 || !managedkubernetes.ValidDigest(receipt.PlanDigest) || !managedkubernetes.ValidDigest(receipt.SnapshotDigest) {
		return nil
	}
	if receipt.CompletionRecorded {
		if receipt.State == enum.KubernetesInvocationRunning || !managedkubernetes.ValidLocator(receipt.ReceiptRef) || len(receipt.ReceiptRef) > 96 || !managedkubernetes.ValidDigest(receipt.ResultDigest) {
			return nil
		}
	} else if receipt.ReceiptRef != "" || receipt.ResultDigest != "" || receipt.State == enum.KubernetesInvocationSucceeded {
		return nil
	}
	pending := cp.ManagedKubernetesCompletionPendingReason_MANAGED_KUBERNETES_COMPLETION_PENDING_REASON_NONE
	switch receipt.CompletionPendingReason {
	case enum.KubernetesCompletionPendingNone:
	case enum.KubernetesCompletionCapturePending:
		if receipt.State != enum.KubernetesInvocationRunning || receipt.PlanRef == "" || receipt.CompletionRecorded || receipt.ReceiptRef != "" || receipt.ResultDigest != "" {
			return nil
		}
		pending = cp.ManagedKubernetesCompletionPendingReason_MANAGED_KUBERNETES_COMPLETION_PENDING_REASON_CAPTURE_PENDING
	default:
		return nil
	}
	return &cp.ManagedKubernetesInvocationReceipt{InvocationRef: receipt.InvocationRef, Generation: receipt.Generation, InputDigest: receipt.InputDigest, BindingDigest: receipt.BindingDigest, State: managedKubernetesInvocationStateToProto(receipt.State), PlanRef: receipt.PlanRef, PlanRevision: receipt.PlanRevision, PlanDigest: receipt.PlanDigest, SnapshotDigest: receipt.SnapshotDigest, ReceiptRef: receipt.ReceiptRef, ResultDigest: receipt.ResultDigest, CompletionRecorded: receipt.CompletionRecorded, CompletionPendingReason: pending}
}

// managedKubernetesInvocationStateToProto keeps the existing finite invocation lifecycle closed.
// managedKubernetesInvocationStateToProto сохраняет закрытый finite existing invocation lifecycle.
func managedKubernetesInvocationStateToProto(state enum.ManagedKubernetesInvocationState) cp.ManagedKubernetesInvocationState {
	switch state {
	case enum.KubernetesInvocationRunning:
		return cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_RUNNING
	case enum.KubernetesInvocationSucceeded:
		return cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_SUCCEEDED
	case enum.KubernetesInvocationFailed:
		return cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_FAILED
	case enum.KubernetesInvocationUnknown:
		return cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_UNKNOWN_OUTCOME
	case enum.KubernetesInvocationCancelled:
		return cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_CANCELLED
	default:
		return cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_UNSPECIFIED
	}
}

// CancelManagedKubernetesInvocation dispatches only the original binding to purpose-owned graph closure.
// Empty or future wire fields fail before semantic use; no caller outcome/result is accepted.
// CancelManagedKubernetesInvocation передаёт лишь original binding purpose-owned graph closure.
// Empty/future wire fields отклоняются до semantics; caller outcome/result не принимаются.
func (server *Server) CancelManagedKubernetesInvocation(ctx context.Context, request *cp.CancelManagedKubernetesInvocationRequest) (*cp.CancelManagedKubernetesInvocationResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_CancelManagedKubernetesInvocation_FullMethodName, func(p value.Principal) (*cp.CancelManagedKubernetesInvocationResponse, error) {
		binding := managedKubernetesBindingFromProto(request.GetBinding())
		result, err := server.service.CancelManagedKubernetesInvocation(ctx, p, binding)
		if err != nil {
			return nil, err
		}
		receipt := managedKubernetesInvocationReceiptToProto(result, binding)
		if receipt == nil {
			return nil, errs.ErrUnavailable
		}
		return &cp.CancelManagedKubernetesInvocationResponse{Receipt: receipt}, nil
	})
}
