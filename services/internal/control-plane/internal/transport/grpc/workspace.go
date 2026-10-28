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
	case controlplanev1.RuntimeWorkspaceObservationPhase_RUNTIME_WORKSPACE_OBSERVATION_PHASE_BLOCKED:
		phase = runtimecontract.WorkspaceObserveBlocked
	case controlplanev1.RuntimeWorkspaceObservationPhase_RUNTIME_WORKSPACE_OBSERVATION_PHASE_RELEASED:
		phase = runtimecontract.WorkspaceObserveReleased
	default:
		return nil, transportError(errs.ErrInvalid)
	}
	payload := command.WorkspaceObservationInput{LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(),
		RuntimeRevisionDigest: request.GetRuntimeRevisionDigest(), ExecutionBindingDigest: request.GetExecutionBindingDigest(),
		WorkspaceRef: request.GetWorkspaceRef(), WorkspaceGeneration: request.GetWorkspaceGeneration(), Phase: phase,
		PodUID: request.GetPodUid(), PVCUID: request.GetPvcUid(), Quiescence: append([]byte(nil), request.GetQuiescence()...), BlockReason: request.GetBlockReason()}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_ObserveWorkspace_FullMethodName,
		command.ObserveWorkspace, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	raw := []byte(mapString(result.Runtime, "workspaceWriterAck"))
	ack, decodeErr := runtimecontract.DecodeWorkspaceWriterAcknowledgement(raw)
	state, reason, joined := mapString(result.Runtime, "workspaceStorageState"), mapString(result.Runtime, "workspaceBlockReason"), []byte(mapString(result.Runtime, "workspaceQuiescence"))
	currentTuple, released := mapBool(result.Runtime, "workspaceCurrentTuple"), mapBool(result.Runtime, "workspaceWriterReleased")
	if decodeErr != nil || currentTuple && !runtimecontract.ValidWorkspaceStorageState(state) || !currentTuple && (state != "" || reason != "" || len(joined) != 0 || released) || len(joined) > runtimecontract.MaximumWorkspaceQuiescenceBytes ||
		state == "BLOCKED" && (!runtimecontract.ValidWorkspaceBlockReason(reason) || len(joined) != 0) || state != "BLOCKED" && reason != "" {
		return nil, transportError(errs.ErrUnavailable)
	}
	if len(joined) > 0 {
		record, err := runtimecontract.DecodeWorkspaceQuiescence(joined)
		if err != nil || record.Writer != ack.Writer || record.Scope != ack.Scope || record.WorkspaceRef != ack.WorkspaceRef || record.Generation != ack.Generation {
			return nil, transportError(errs.ErrUnavailable)
		}
	}
	committedPhase, committed := mapString(result.Runtime, "workspaceCommittedPhase"), []byte(mapString(result.Runtime, "workspaceCommittedQuiescence"))
	switch committedPhase {
	case runtimecontract.WorkspaceObserveBind, runtimecontract.WorkspaceObserveBlocked:
		if len(committed) != 0 {
			return nil, transportError(errs.ErrUnavailable)
		}
	case runtimecontract.WorkspaceObserveQuiescent, runtimecontract.WorkspaceObserveReleased:
		record, err := runtimecontract.DecodeWorkspaceQuiescence(committed)
		if err != nil || record.Writer != ack.Writer || record.Scope != ack.Scope || record.WorkspaceRef != ack.WorkspaceRef || record.Generation != ack.Generation {
			return nil, transportError(errs.ErrUnavailable)
		}
	default:
		return nil, transportError(errs.ErrUnavailable)
	}
	return &controlplanev1.ObserveWorkspaceResponse{WriterAcknowledgement: raw,
		StorageState: state, Quiescence: joined, BlockReason: reason, CurrentTuple: currentTuple, WriterReleased: released,
		CommittedPhase: committedPhase, CommittedQuiescence: committed}, nil
}
