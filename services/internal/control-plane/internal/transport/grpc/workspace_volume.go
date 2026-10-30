package grpc

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// workspaceVolumeMutation requires an explicit replay key and forbids OCC/unknown fields, so a
// lost claim response never falls back to a correlation-derived key or silently becomes new work.
// workspaceVolumeMutation требует явный replay key и запрещает OCC/unknown fields, поэтому
// неизвестный claim не получает correlation-derived key и не превращается незаметно в новую работу.
func workspaceVolumeMutation(input *cp.MutationContext) bool {
	return input != nil && input.ExpectedVersion == nil && len(input.ProtoReflect().GetUnknown()) == 0 &&
		strings.TrimSpace(input.GetIdempotencyKey()) != "" && len(input.GetIdempotencyKey()) <= 128
}

// ClaimWorkspaceVolumeTasks invokes only the specialized owner command and validates every
// returned immutable task, including JSON-shaped receipt replay, before returning its separate lease.
// ClaimWorkspaceVolumeTasks вызывает только отдельную owner command и проверяет каждую immutable
// task, включая JSON-shaped receipt replay, до выдачи её отдельного lease.
func (server *Server) ClaimWorkspaceVolumeTasks(ctx context.Context, request *cp.ClaimWorkspaceVolumeTasksRequest) (*cp.ClaimWorkspaceVolumeTasksResponse, error) {
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 || !workspaceVolumeMutation(request.GetMutation()) {
		return nil, transportError(errs.ErrInvalid)
	}
	result, err := execute(ctx, server.service, cp.WorkspaceVolumeWorkService_ClaimWorkspaceVolumeTasks_FullMethodName,
		command.ClaimWorkspaceVolumeTasks, request.GetMutation(), command.WorkspaceVolumeClaimInput{
			WorkloadInstance: request.GetWorkloadInstance(), Limit: request.GetLimit(),
		})
	if err != nil {
		return nil, err
	}
	response := &cp.ClaimWorkspaceVolumeTasksResponse{Tasks: make([]*cp.WorkspaceVolumeTask, 0, len(result.RuntimeItems))}
	for _, item := range result.RuntimeItems {
		task := castWorkspaceVolumeTask(item)
		if task == nil {
			return nil, transportError(errs.ErrUnavailable)
		}
		response.Tasks = append(response.Tasks, task)
	}
	return response, nil
}

// RenewWorkspaceVolumeTask checks the exact authenticated method and forwards only the issued
// attempt pins; the owner rechecks origin/deadline and never creates another attempt or model slot.
// RenewWorkspaceVolumeTask проверяет exact authenticated method и передаёт только выданные
// attempt pins; owner повторно проверяет origin/deadline без новой attempt либо model slot.
func (server *Server) RenewWorkspaceVolumeTask(ctx context.Context, request *cp.RenewWorkspaceVolumeTaskRequest) (*cp.RenewWorkspaceVolumeTaskResponse, error) {
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.WorkspaceVolumeWorkService_RenewWorkspaceVolumeTask_FullMethodName, func(p value.Principal) (*cp.RenewWorkspaceVolumeTaskResponse, error) {
		result, err := server.service.RenewWorkspaceVolumeTask(ctx, p, command.WorkspaceVolumeTaskInput{
			TaskRef: request.GetTaskRef(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(),
		})
		if err != nil {
			return nil, err
		}
		lease := castWorkspaceVolumeLease(result)
		if lease == nil || lease.Ref != request.GetLeaseRef() || lease.Fence != request.GetFence() || lease.Generation != request.GetGeneration() {
			return nil, errs.ErrUnavailable
		}
		return &cp.RenewWorkspaceVolumeTaskResponse{Lease: lease}, nil
	})
}

// CompleteWorkspaceVolumeTask preserves a typed positive observation without accepting caller
// state or task kind; durable receipt fields describe the recorded fact rather than fresh authority.
// CompleteWorkspaceVolumeTask сохраняет typed positive observation без caller state либо task kind;
// durable receipt fields описывают записанный факт, а не свежие полномочия.
func (server *Server) CompleteWorkspaceVolumeTask(ctx context.Context, request *cp.CompleteWorkspaceVolumeTaskRequest) (*cp.CompleteWorkspaceVolumeTaskResponse, error) {
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 || !workspaceVolumeMutation(request.GetMutation()) {
		return nil, transportError(errs.ErrInvalid)
	}
	ready, valid := decodeWorkspaceVolumeReady(request.GetReady())
	if !valid {
		return nil, transportError(errs.ErrInvalid)
	}
	result, err := execute(ctx, server.service, cp.WorkspaceVolumeWorkService_CompleteWorkspaceVolumeTask_FullMethodName,
		command.CompleteWorkspaceVolumeTask, request.GetMutation(), command.WorkspaceVolumeTaskInput{
			TaskRef: request.GetTaskRef(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(),
			ObservedPVCUID: request.GetObservedPvcUid(), JobUID: request.GetJobUid(), PodUID: request.GetPodUid(), Ready: ready,
		})
	if err != nil {
		return nil, err
	}
	return &cp.CompleteWorkspaceVolumeTaskResponse{TaskRef: mapString(result.Runtime, "taskRef"),
		RecordedTaskState: mapString(result.Runtime, "recordedTaskState"), RecordedVolumeState: mapString(result.Runtime, "recordedVolumeState"),
		PastFact: mapBool(result.Runtime, "pastFact"), MatchedVolumeIncarnation: mapBool(result.Runtime, "matchedVolumeIncarnation")}, nil
}

// FailWorkspaceVolumeTask passes only exact lease pins and a closed diagnostic to the owner;
// failure does not manufacture cleanup permission, retry credit or a model continuation.
// FailWorkspaceVolumeTask передаёт owner только exact lease pins и закрытую диагностику;
// отказ не создаёт разрешение cleanup, retry credit либо model continuation.
func (server *Server) FailWorkspaceVolumeTask(ctx context.Context, request *cp.FailWorkspaceVolumeTaskRequest) (*cp.FailWorkspaceVolumeTaskResponse, error) {
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 || !workspaceVolumeMutation(request.GetMutation()) {
		return nil, transportError(errs.ErrInvalid)
	}
	result, err := execute(ctx, server.service, cp.WorkspaceVolumeWorkService_FailWorkspaceVolumeTask_FullMethodName,
		command.FailWorkspaceVolumeTask, request.GetMutation(), command.WorkspaceVolumeTaskInput{
			TaskRef: request.GetTaskRef(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(),
			SafeErrorCode: request.GetSafeErrorCode(),
		})
	if err != nil {
		return nil, err
	}
	return &cp.FailWorkspaceVolumeTaskResponse{TaskRef: mapString(result.Runtime, "taskRef"),
		RecordedTaskState: mapString(result.Runtime, "recordedTaskState"), RecordedVolumeState: mapString(result.Runtime, "recordedVolumeState")}, nil
}

// castWorkspaceVolumeTask accepts the same closed typed work for fresh and replayed JSON maps;
// canonical input hashing follows typed decoding rather than the map serializer's key order.
// castWorkspaceVolumeTask принимает одинаковый closed typed work для fresh и replayed JSON maps;
// canonical input hashing выполняется после typed decoding, независимо от порядка ключей map.
func castWorkspaceVolumeTask(values map[string]any) *cp.WorkspaceVolumeTask {
	raw, err := json.Marshal(values["work"])
	var work runtimecontract.WorkspaceVolumeWork
	if err != nil || len(raw) > runtimecontract.MaximumWorkspaceVolumeReadyBytes ||
		runtimecontract.DecodeWorkspaceStorageTaskJSON(raw, &work) != nil || work.Validate() != nil {
		return nil
	}
	lease := castWorkspaceVolumeLease(values)
	if lease == nil || lease.Generation != int64(work.Attempt) {
		return nil
	}
	kind := cp.WorkspaceVolumeTaskKind_WORKSPACE_VOLUME_TASK_KIND_UNSPECIFIED
	switch work.Kind {
	case runtimecontract.WorkspaceVolumeCreate:
		kind = cp.WorkspaceVolumeTaskKind_WORKSPACE_VOLUME_TASK_KIND_CREATE
	case runtimecontract.WorkspaceVolumePrepare:
		kind = cp.WorkspaceVolumeTaskKind_WORKSPACE_VOLUME_TASK_KIND_PREPARE
	default:
		return nil
	}
	input := work.Volume
	return &cp.WorkspaceVolumeTask{TaskRef: work.TaskRef, Kind: kind, Attempt: work.Attempt,
		InputDigest: work.InputDigest, ReconcileOnly: work.ReconcileOnly, Lease: lease,
		Volume: &cp.WorkspaceVolumeInput{Profile: input.Profile, Scope: castWorkspaceScope(input.Scope),
			WorkspaceRef: input.WorkspaceRef, VolumeGeneration: input.VolumeGeneration, CreateTaskRef: input.CreateTaskRef,
			PreparationTaskRef: input.PreparationTaskRef, PvcUid: input.PVCUID, ReadOnly: input.ReadOnly}}
}

// castWorkspaceVolumeLease preserves the exact timestamp across native results and durable JSON
// receipt replay, rejects fractional generations and does not revive expired historical authority.
// castWorkspaceVolumeLease сохраняет exact timestamp у native result и durable JSON receipt replay,
// отклоняет дробные поколения и не возобновляет истёкшие исторические полномочия.
func castWorkspaceVolumeLease(values map[string]any) *cp.WorkLease {
	var generation int64
	raw, err := json.Marshal(values["generation"])
	if err != nil || json.Unmarshal(raw, &generation) != nil || generation < 1 || generation > 5 {
		return nil
	}
	expires, ok := values["expiresAt"].(time.Time)
	if !ok {
		text, valid := values["expiresAt"].(string)
		if !valid || len(text) > 40 {
			return nil
		}
		expires, err = time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return nil
		}
	}
	lease := &cp.WorkLease{Ref: mapString(values, "leaseRef"), Fence: mapString(values, "fence"), Generation: generation,
		ExpiresAt: timestamppb.New(expires)}
	if lease.Ref == "" || lease.Fence == "" || expires.IsZero() || lease.ExpiresAt.CheckValid() != nil {
		return nil
	}
	return lease
}

// decodeWorkspaceVolumeReady rejects unknown nested wire fields and validates the complete
// sealed observation; nil remains valid only as transport data for the owner-selected CREATE case.
// decodeWorkspaceVolumeReady отклоняет unknown nested wire fields и проверяет всё sealed observation;
// nil остаётся допустимыми transport data лишь для выбранного owner случая CREATE.
func decodeWorkspaceVolumeReady(input *cp.RuntimeWorkspaceVolumeReady) (*runtimecontract.WorkspaceVolumeReady, bool) {
	if input == nil {
		return nil, true
	}
	preparation := input.GetPreparation()
	if len(input.ProtoReflect().GetUnknown()) != 0 || preparation == nil || len(preparation.ProtoReflect().GetUnknown()) != 0 ||
		preparation.GetScope() == nil || len(preparation.GetScope().ProtoReflect().GetUnknown()) != 0 {
		return nil, false
	}
	scope := preparation.GetScope()
	result := &runtimecontract.WorkspaceVolumeReady{Schema: input.GetSchema(), Device: input.GetDevice(), Inode: input.GetInode(),
		Preparation: runtimecontract.WorkspaceVolumePreparation{Scope: runtimecontract.WorkspaceStorageScope{
			OrganizationRef: scope.GetOrganizationRef(), ProjectRef: scope.GetProjectRef(), SessionRef: scope.GetSessionRef(), AgentRef: scope.GetAgentRef()},
			WorkspaceRef: preparation.GetWorkspaceRef(), TaskRef: preparation.GetTaskRef(), PVCUID: preparation.GetPvcUid(), VolumeGeneration: preparation.GetVolumeGeneration()}}
	if _, err := runtimecontract.EncodeWorkspaceVolumeReady(*result); err != nil {
		return nil, false
	}
	return result, true
}
