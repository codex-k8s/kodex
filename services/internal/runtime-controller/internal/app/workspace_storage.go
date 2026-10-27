package app

import (
	"context"
	"errors"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/workload"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// reconcileWorkspaceStorage commits exact stopped writers before releasing their retained Pod finalizer.
// The existing poll owns cancellation and one request budget; this never starts another worker or model.
// reconcileWorkspaceStorage фиксирует exact stopped writer до снятия retained Pod finalizer.
// Existing poll владеет cancellation и request budget; отдельный worker или model не запускается.
func (runtime *runtime) reconcileWorkspaceStorage(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, runtime.config.RequestTimeout)
	defer cancel()
	values, err := runtime.manager.PendingWorkspaceQuiescence(ctx)
	if err != nil {
		return err
	}
	for _, value := range values {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		absent, absenceErr := runtime.manager.WorkspacePodAbsent(ctx, value.Input)
		if absenceErr != nil {
			continue
		}
		if absent {
			released, err := runtime.observeWorkspaceReleased(ctx, value.Input)
			if err == nil {
				ack, record, proofErr := releasedWorkspaceReceipt(released, value.Input)
				if proofErr == nil {
					value.Record = record
					_ = runtime.manager.CleanupWorkspaceDependencies(ctx, value, ack)
				}
				continue
			}
			if status.Code(err) != codes.PermissionDenied && status.Code(err) != codes.NotFound {
				continue
			}
		}
		response, err := runtime.observeWorkspaceStorage(ctx, value)
		if err != nil || !response.GetCurrentTuple() {
			continue
		}
		ack, err := runtimecontract.DecodeWorkspaceWriterAcknowledgement(response.GetWriterAcknowledgement())
		if err != nil || !ack.Matches(value.Input) {
			continue
		}
		if len(response.GetQuiescence()) > 0 {
			record, err := runtimecontract.DecodeWorkspaceQuiescence(response.GetQuiescence())
			if err != nil || record.Writer != ack.Writer || !record.Matches(value.Input, *value.Input.WorkspaceStorage, ack.Writer.PodUID, ack.Writer.PVCUID) ||
				!workspaceCheckpointState(response.GetStorageState()) || response.GetBlockReason() != "" {
				continue
			}
			value.Record, value.BlockReason = record, ""
			_ = runtime.reconcileWorkspaceRelease(ctx, value, ack)
			continue
		}
		if response.GetStorageState() != "BLOCKED" || !runtimecontract.ValidWorkspaceBlockReason(response.GetBlockReason()) {
			continue
		}
		if record, ok := runtime.manager.QuiescenceBeforeMain(ctx, value.Input, ack); ok {
			value.Record, value.BlockReason = record, ""
			confirmed, err := runtime.observeWorkspaceStorage(ctx, value)
			if err != nil || !confirmed.GetCurrentTuple() {
				continue
			}
			confirmedAck, err := runtimecontract.DecodeWorkspaceWriterAcknowledgement(confirmed.GetWriterAcknowledgement())
			stored, decodeErr := runtimecontract.DecodeWorkspaceQuiescence(confirmed.GetQuiescence())
			if err != nil || decodeErr != nil || confirmedAck != ack || stored != record || !workspaceCheckpointState(confirmed.GetStorageState()) {
				continue
			}
			_ = runtime.reconcileWorkspaceRelease(ctx, value, ack)
		}
	}
	return nil
}

// observeWorkspaceStorage sends one immutable controller-owned report using pins from the retained ticket.
// UNKNOWN/missing objects report BLOCKED, never an invented quiescence or successful business outcome.
// observeWorkspaceStorage отправляет один immutable controller-owned report по retained ticket pins.
// UNKNOWN/missing object передаёт BLOCKED, не выдуманную quiescence или successful business outcome.
func (runtime *runtime) observeWorkspaceStorage(ctx context.Context, value workload.WorkspaceQuiescenceObservation) (*controlplanev1.ObserveWorkspaceResponse, error) {
	input := value.Input
	if input.WorkspaceStorage == nil {
		return nil, errors.New("runtime workspace observation input is invalid")
	}
	request := &controlplanev1.ObserveWorkspaceRequest{LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration,
		RuntimeRevisionDigest: input.RuntimeRevisionDigest, ExecutionBindingDigest: input.ExecutionBindingDigest,
		WorkspaceRef: input.WorkspaceStorage.WorkspaceRef, WorkspaceGeneration: input.WorkspaceStorage.Generation}
	key := "workspace:" + runtimecontract.WorkspaceObserveQuiescent
	if value.BlockReason != "" {
		if !runtimecontract.ValidWorkspaceBlockReason(value.BlockReason) {
			return nil, errors.New("runtime workspace blocked reason is invalid")
		}
		request.Phase = controlplanev1.RuntimeWorkspaceObservationPhase_RUNTIME_WORKSPACE_OBSERVATION_PHASE_BLOCKED
		request.BlockReason = value.BlockReason
		key = "workspace:" + runtimecontract.WorkspaceObserveBlocked + ":" + value.BlockReason
	} else {
		raw, err := runtimecontract.EncodeWorkspaceQuiescence(value.Record)
		if err != nil || !value.Record.Matches(input, *input.WorkspaceStorage, value.Record.Writer.PodUID, value.Record.Writer.PVCUID) {
			return nil, errors.New("runtime workspace quiescence is invalid")
		}
		request.Phase = controlplanev1.RuntimeWorkspaceObservationPhase_RUNTIME_WORKSPACE_OBSERVATION_PHASE_QUIESCENT
		request.Quiescence, request.PodUid, request.PvcUid = raw, value.Record.Writer.PodUID, value.Record.Writer.PVCUID
	}
	request.Mutation = &controlplanev1.MutationContext{IdempotencyKey: stableIdempotency(input.LeaseRef, key)}
	return runtime.control.ObserveWorkspace(ctx, request)
}

// workspaceCheckpointState closes the owner phases that retain a positive committed stopped writer.
// workspaceCheckpointState замыкает owner phases с positive committed stopped writer.
func workspaceCheckpointState(value string) bool {
	switch value {
	case "CHECKPOINT_PENDING", "CHECKPOINT_READY", "DELETE_READY", "ARCHIVED":
		return true
	default:
		return false
	}
}

// reconcileWorkspaceRelease first releases the joined Pod, then commits observed absence before private cleanup.
// An uncertain patch/RPC retains dependencies; a later poll replays exactly the same immutable request.
// reconcileWorkspaceRelease снимает joined Pod, затем фиксирует observed absence до private cleanup.
// Uncertain patch/RPC сохраняет dependencies; поздний poll повторяет exact immutable request.
func (runtime *runtime) reconcileWorkspaceRelease(ctx context.Context, value workload.WorkspaceQuiescenceObservation, ack runtimecontract.WorkspaceWriterAcknowledgement) error {
	_ = runtime.manager.ReleaseWorkspacePod(ctx, value, ack)
	absent, err := runtime.manager.WorkspacePodAbsent(ctx, value.Input)
	if err != nil || !absent {
		return errors.New("runtime workspace Pod release is unconfirmed")
	}
	response, err := runtime.observeWorkspaceReleased(ctx, value.Input)
	if err != nil {
		return err
	}
	confirmed, record, err := releasedWorkspaceReceipt(response, value.Input)
	if err != nil || confirmed != ack || record != value.Record {
		return errors.New("runtime workspace released receipt differs")
	}
	return runtime.manager.CleanupWorkspaceDependencies(ctx, value, confirmed)
}

// observeWorkspaceReleased uses only frozen tuple fields; the owner resolves its already-saved positive join.
// Missing Pod without that saved proof cannot create a release receipt or advance the workspace.
// observeWorkspaceReleased использует frozen tuple; owner разрешает свой already-saved positive join.
// Missing Pod без saved proof не создаёт release receipt и не продвигает workspace.
func (runtime *runtime) observeWorkspaceReleased(ctx context.Context, input runtimecontract.RunnerInput) (*controlplanev1.ObserveWorkspaceResponse, error) {
	if input.WorkspaceStorage == nil || input.WorkspaceStorage.ValidateForInput(input) != nil {
		return nil, errors.New("runtime workspace release input is invalid")
	}
	return runtime.control.ObserveWorkspace(ctx, &controlplanev1.ObserveWorkspaceRequest{
		Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableIdempotency(input.LeaseRef, "workspace:"+runtimecontract.WorkspaceObserveReleased)},
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, RuntimeRevisionDigest: input.RuntimeRevisionDigest,
		ExecutionBindingDigest: input.ExecutionBindingDigest, WorkspaceRef: input.WorkspaceStorage.WorkspaceRef, WorkspaceGeneration: input.WorkspaceStorage.Generation,
		Phase: controlplanev1.RuntimeWorkspaceObservationPhase_RUNTIME_WORKSPACE_OBSERVATION_PHASE_RELEASED})
}

// releasedWorkspaceReceipt validates immutable release proof independently of later current tuple metadata.
// It authorizes only exact old private-resource cleanup, never restore, a new writer or PVC deletion.
// releasedWorkspaceReceipt проверяет immutable release proof отдельно от поздней current tuple metadata.
// Разрешён лишь exact old private-resource cleanup, не restore, новый writer или PVC deletion.
func releasedWorkspaceReceipt(response *controlplanev1.ObserveWorkspaceResponse, input runtimecontract.RunnerInput) (runtimecontract.WorkspaceWriterAcknowledgement, runtimecontract.WorkspaceQuiescence, error) {
	ack, err := runtimecontract.DecodeWorkspaceWriterAcknowledgement(response.GetWriterAcknowledgement())
	record, recordErr := runtimecontract.DecodeWorkspaceQuiescence(response.GetCommittedQuiescence())
	if err != nil || recordErr != nil || response.GetCommittedPhase() != runtimecontract.WorkspaceObserveReleased || !ack.Matches(input) ||
		ack.Writer != record.Writer || !record.Matches(input, *input.WorkspaceStorage, ack.Writer.PodUID, ack.Writer.PVCUID) {
		return runtimecontract.WorkspaceWriterAcknowledgement{}, runtimecontract.WorkspaceQuiescence{}, errors.New("runtime workspace immutable release receipt is invalid")
	}
	return ack, record, nil
}
