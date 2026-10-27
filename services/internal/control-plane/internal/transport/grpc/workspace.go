package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

// ObserveWorkspace accepts only closed writer phases through the exact controller-owned RPC.
// A storage-only terminal observation returns its immutable ACK without changing run authority.
// ObserveWorkspace принимает только closed writer phases через exact controller-owned RPC.
// Storage-only terminal observation возвращает immutable ACK без изменения run authority.
func (server *Server) ObserveWorkspace(ctx context.Context, request *controlplanev1.ObserveWorkspaceRequest) (*controlplanev1.ObserveWorkspaceResponse, error) {
	if request == nil || len(request.GetQuiescence()) > runtimecontract.MaximumWorkspaceQuiescenceBytes {
		return nil, transportError(errs.ErrInvalid)
	}
	var phase string
	switch request.GetPhase() {
	case controlplanev1.RuntimeWorkspaceObservationPhase_RUNTIME_WORKSPACE_OBSERVATION_PHASE_BIND:
		phase = runtimecontract.WorkspaceObserveBind
	case controlplanev1.RuntimeWorkspaceObservationPhase_RUNTIME_WORKSPACE_OBSERVATION_PHASE_QUIESCENT:
		phase = runtimecontract.WorkspaceObserveQuiescent
	default:
		return nil, transportError(errs.ErrInvalid)
	}
	payload := command.WorkspaceObservationInput{LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(),
		RuntimeRevisionDigest: request.GetRuntimeRevisionDigest(), ExecutionBindingDigest: request.GetExecutionBindingDigest(),
		WorkspaceRef: request.GetWorkspaceRef(), WorkspaceGeneration: request.GetWorkspaceGeneration(), Phase: phase,
		PodUID: request.GetPodUid(), PVCUID: request.GetPvcUid(), Quiescence: append([]byte(nil), request.GetQuiescence()...)}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_ObserveWorkspace_FullMethodName,
		command.ObserveWorkspace, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	raw := []byte(mapString(result.Runtime, "workspaceWriterAck"))
	if _, err := runtimecontract.DecodeWorkspaceWriterAcknowledgement(raw); err != nil {
		return nil, transportError(errs.ErrUnavailable)
	}
	return &controlplanev1.ObserveWorkspaceResponse{WriterAcknowledgement: raw}, nil
}
