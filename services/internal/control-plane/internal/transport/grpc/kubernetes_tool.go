package grpc

import (
	"context"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// kubernetesToolViewToProto validates closed safe metadata without converting owner closure into a native-join claim.
// kubernetesToolViewToProto проверяет безопасную проекцию, не выдавая закрытие owner за подтверждение native join.
func kubernetesToolViewToProto(v entity.KubernetesToolView) (*cp.ManagedKubernetesToolView, error) {
	_, operationKnown := integrationpackage.KubernetesOperationByKey(v.Operation)
	state, stateKnown := cp.ManagedKubernetesToolState_value["MANAGED_KUBERNETES_TOOL_STATE_"+string(v.State)]
	if !operationKnown || !stateKnown || state == 0 || !v.State.Known() || v.Version < 1 || uint64(v.Version) > maximumKubernetesInputPublicSequence || v.TargetRevision < 1 || uint64(v.TargetRevision) > maximumKubernetesInputPublicSequence || !managedkubernetes.ValidLocator(v.TargetRef) || !kubernetesInputPublicRef(v.InvocationRef, "inv_") || !kubernetesInputPublicRef(v.RunRef, "run_") || !kubernetesInputPublicRef(v.NodeRef, "nod_") || !kubernetesInputPublicRef(v.ConnectionRef, "int_") || v.State.Terminal() && v.WaitingForGate {
		return nil, errs.ErrUnavailable
	}
	reason := cp.ManagedKubernetesToolSafeReasonCode_MANAGED_KUBERNETES_TOOL_SAFE_REASON_CODE_NONE
	switch v.SafeReasonCode {
	case enum.KubernetesToolReasonNone:
	case enum.KubernetesToolReasonPortForwardProfileUnsupported:
		if v.Operation != "kubernetes.port-forward" || v.State != "FAILED" {
			return nil, errs.ErrUnavailable
		}
		reason = cp.ManagedKubernetesToolSafeReasonCode_MANAGED_KUBERNETES_TOOL_SAFE_REASON_CODE_PORT_FORWARD_PROFILE_UNSUPPORTED
	default:
		return nil, errs.ErrUnavailable
	}
	result := &cp.ManagedKubernetesToolView{InvocationRef: v.InvocationRef, RunRef: v.RunRef, NodeRef: v.NodeRef, ConnectionRef: v.ConnectionRef, TargetRef: v.TargetRef, TargetRevision: v.TargetRevision, Operation: v.Operation, State: cp.ManagedKubernetesToolState(state), Version: v.Version, WaitingForGate: v.WaitingForGate, SafeReasonCode: reason}
	if len(v.CaptureStatuses) > 256 {
		return nil, errs.ErrUnavailable
	}
	seenCaptures := map[string]bool{}
	for _, capture := range v.CaptureStatuses {
		summary, err := kubernetesCaptureSummaryToProto(capture)
		if err != nil || seenCaptures[capture.CaptureRef] {
			return nil, errs.ErrUnavailable
		}
		seenCaptures[capture.CaptureRef] = true
		result.CaptureStatuses = append(result.CaptureStatuses, summary)
	}
	return result, nil
}

// ListManagedKubernetesTools validates a bounded exact-node page before public disclosure.
// ListManagedKubernetesTools проверяет ограниченную страницу точного узла перед публичной выдачей.
func (server *Server) ListManagedKubernetesTools(ctx context.Context, request *cp.ListManagedKubernetesToolsRequest) (*cp.ListManagedKubernetesToolsResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesToolService_ListManagedKubernetesTools_FullMethodName, func(p value.Principal) (*cp.ListManagedKubernetesToolsResponse, error) {
		paging := page(request.GetPage())
		result, err := server.service.ListKubernetesTools(ctx, p, query.KubernetesToolList{RunRef: request.GetRunRef(), NodeRef: request.GetNodeRef(), Page: paging})
		if err != nil {
			return nil, err
		}
		if len(result.Items) > int(paging.Size) || result.Next != "" && !kubernetesInputPublicRef(result.Next, "inv_") {
			return nil, errs.ErrUnavailable
		}
		response := &cp.ListManagedKubernetesToolsResponse{Items: make([]*cp.ManagedKubernetesToolView, 0, len(result.Items)), Page: &cp.PageInfo{NextPageToken: result.Next}}
		seen := make(map[string]bool, len(result.Items))
		for _, item := range result.Items {
			view, err := kubernetesToolViewToProto(item)
			if err != nil || item.RunRef != request.GetRunRef() || item.NodeRef != request.GetNodeRef() || seen[item.InvocationRef] {
				return nil, errs.ErrUnavailable
			}
			seen[item.InvocationRef] = true
			response.Items = append(response.Items, view)
		}
		return response, nil
	})
}

// GetManagedKubernetesTool rejects mixed-origin metadata after the authoritative owner read.
// GetManagedKubernetesTool отклоняет смешанные метаданные после авторитетного чтения владельца.
func (server *Server) GetManagedKubernetesTool(ctx context.Context, request *cp.GetManagedKubernetesToolRequest) (*cp.GetManagedKubernetesToolResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesToolService_GetManagedKubernetesTool_FullMethodName, func(p value.Principal) (*cp.GetManagedKubernetesToolResponse, error) {
		result, err := server.service.GetKubernetesTool(ctx, p, request.GetInvocationRef())
		if err != nil {
			return nil, err
		}
		view, err := kubernetesToolViewToProto(result)
		if err != nil || result.InvocationRef != request.GetInvocationRef() {
			return nil, errs.ErrUnavailable
		}
		return &cp.GetManagedKubernetesToolResponse{View: view}, nil
	})
}

// StopManagedKubernetesTool forwards closure-only intent and validates a terminal owner response without claiming remote termination.
// StopManagedKubernetesTool передаёт запрос закрытия и проверяет terminal ответ владельца без утверждения remote termination.
func (server *Server) StopManagedKubernetesTool(ctx context.Context, request *cp.StopManagedKubernetesToolRequest) (*cp.StopManagedKubernetesToolResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesToolService_StopManagedKubernetesTool_FullMethodName, func(p value.Principal) (*cp.StopManagedKubernetesToolResponse, error) {
		result, err := server.service.StopKubernetesTool(ctx, p, mutation(request.GetMutation()), request.GetInvocationRef())
		if err != nil {
			return nil, err
		}
		view, err := kubernetesToolViewToProto(result)
		if err != nil || result.InvocationRef != request.GetInvocationRef() || !result.State.Terminal() {
			return nil, errs.ErrUnavailable
		}
		return &cp.StopManagedKubernetesToolResponse{View: view}, nil
	})
}
