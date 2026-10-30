package app

import (
	"context"
	"errors"
	"strconv"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/opaqueref"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/controller"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// errWorkspaceVolumeClaimInvalid closes malformed claim/lease responses before a physical
// action; callers log only a bounded error class rather than private proof bytes.
// errWorkspaceVolumeClaimInvalid отклоняет malformed claim/lease responses до physical action;
// caller пишет только bounded error class вместо private proof bytes.
var errWorkspaceVolumeClaimInvalid = model.ErrWorkspaceVolumeClaimInvalid

// workspaceVolumeExecutor restricts this adapter to the two typed physical stages; it does not
// expose arbitrary Kubernetes commands, model execution or owner state mutation to a caller.
// workspaceVolumeExecutor ограничивает adapter двумя typed physical stages, без произвольных
// Kubernetes commands, model execution либо owner state mutation у caller.
type workspaceVolumeExecutor interface {
	CreateWorkspaceVolume(context.Context, runtimecontract.WorkspaceVolumeWork, func(context.Context) error) (string, error)
	PrepareWorkspaceVolume(context.Context, runtimecontract.WorkspaceVolumeWork, func(context.Context) error) (controller.WorkspaceVolumePreparationResult, *controller.WorkspaceVolumeResources, error)
}

// validWorkspaceVolumeLease checks an explicit bounded private tuple and current owner expiry;
// it never derives a lease from task names, payload scope or a model RuntimeRevision.
// validWorkspaceVolumeLease проверяет explicit bounded private tuple и current owner expiry;
// lease не выводится из task names, payload scope либо model RuntimeRevision.
func validWorkspaceVolumeLease(lease *cp.WorkLease, generation int64) bool {
	return lease != nil && len(lease.ProtoReflect().GetUnknown()) == 0 &&
		opaqueref.Valid(lease.GetRef(), "lea") && opaqueref.Valid(lease.GetFence(), "fnc") &&
		generation >= 1 && generation <= 5 && lease.GetGeneration() == generation &&
		lease.GetExpiresAt() != nil && len(lease.GetExpiresAt().ProtoReflect().GetUnknown()) == 0 &&
		lease.GetExpiresAt().CheckValid() == nil && time.Now().Before(lease.GetExpiresAt().AsTime())
}

// acknowledgeOwner retries only the same already constructed terminal RPC, at most
// twice and only after an uncertain transport result. It never repeats the physical operation,
// changes the idempotency key or turns an unknown external result into success.
// acknowledgeOwner повторяет только тот же заранее построенный terminal RPC максимум
// дважды и лишь после uncertain transport result. Physical operation не повторяется,
// idempotency key не меняется, неизвестный external result не становится success.
func acknowledgeOwner(ctx context.Context, timeout time.Duration, call func(context.Context) error) error {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		rpc, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		err = call(rpc)
		cancel()
		if errors.Is(err, errWorkspaceVolumeClaimInvalid) {
			return err
		}
		switch status.Code(err) {
		case codes.Unavailable, codes.DeadlineExceeded, codes.Unknown, codes.Canceled:
			continue
		default:
			return err
		}
	}
	return err
}

// processWorkspaceVolume executes one exact owner-selected physical claim, independently
// accounts positive Create responses, and preserves stable terminal receipts. Ambiguous ACKs
// retain all physical evidence; only a successful positive-fact ACK releases completed Pods.
// processWorkspaceVolume исполняет один exact owner-selected physical claim, независимо учитывает
// positive Create responses и сохраняет stable terminal receipts. Неизвестный ACK сохраняет всё
// physical evidence; только успешный positive-fact ACK освобождает completed Pods.
func processWorkspaceVolume(ctx context.Context, client cp.WorkspaceVolumeWorkServiceClient, executor workspaceVolumeExecutor, claim *cp.WorkspaceVolumeTask, metrics *archiveMetrics, timeout time.Duration) error {
	work, err := model.WorkspaceVolumeFromProto(claim)
	if err != nil || !validWorkspaceVolumeLease(claim.GetLease(), int64(work.Attempt)) {
		metrics.tasks.WithLabelValues("INVALID", "error").Inc()
		return errWorkspaceVolumeClaimInvalid
	}
	lease := claim.GetLease()
	metrics.active.Inc()
	defer metrics.active.Dec()
	started := time.Now()
	defer func() { metrics.duration.WithLabelValues(work.Kind).Observe(time.Since(started).Seconds()) }()
	renew := func(call context.Context) error {
		rpc, cancel := context.WithTimeout(call, timeout)
		defer cancel()
		response, err := client.RenewWorkspaceVolumeTask(rpc, &cp.RenewWorkspaceVolumeTaskRequest{TaskRef: work.TaskRef,
			LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration()})
		if err != nil {
			return err
		}
		if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 ||
			!validWorkspaceVolumeLease(response.GetLease(), lease.GetGeneration()) ||
			response.GetLease().GetRef() != lease.GetRef() || response.GetLease().GetFence() != lease.GetFence() {
			return errWorkspaceVolumeClaimInvalid
		}
		return nil
	}
	var pvcUID string
	var prepared controller.WorkspaceVolumePreparationResult
	var retained *controller.WorkspaceVolumeResources
	if work.Kind == runtimecontract.WorkspaceVolumeCreate {
		pvcUID, err = executor.CreateWorkspaceVolume(ctx, work, renew)
		if err == nil {
			metrics.volumeEffects.WithLabelValues("PVC").Inc()
		}
	} else {
		prepared, retained, err = executor.PrepareWorkspaceVolume(ctx, work, renew)
		if prepared.Ready != nil {
			pvcUID = prepared.Ready.Preparation.PVCUID
		}
		inputCreated, jobCreated := retained.ConfirmedCreates()
		if inputCreated {
			metrics.volumeEffects.WithLabelValues("INPUT").Inc()
		}
		if jobCreated {
			metrics.volumeEffects.WithLabelValues("JOB").Inc()
		}
	}
	if err == nil && runtimecontract.ValidateWorkspaceVolumeObservation(work, pvcUID, prepared.JobUID, prepared.PodUID, prepared.Ready) != nil {
		err = errWorkspaceVolumeClaimInvalid
	}
	key := func(stage string) *cp.MutationContext {
		return &cp.MutationContext{IdempotencyKey: uuid.NewSHA1(uuid.NameSpaceOID,
			[]byte("workspace-volume\x00"+work.TaskRef+"\x00"+strconv.FormatInt(lease.GetGeneration(), 10)+"\x00"+work.InputDigest+"\x00"+stage)).String()}
	}
	if err != nil {
		code := "PREPARE_UNCONFIRMED"
		if work.Kind == runtimecontract.WorkspaceVolumeCreate {
			code = "CREATE_UNCONFIRMED"
		}
		request := &cp.FailWorkspaceVolumeTaskRequest{Mutation: key("fail"), TaskRef: work.TaskRef, LeaseRef: lease.GetRef(),
			Fence: lease.GetFence(), Generation: lease.GetGeneration(), SafeErrorCode: code}
		ackErr := acknowledgeOwner(ctx, timeout, func(rpc context.Context) error {
			response, err := client.FailWorkspaceVolumeTask(rpc, request)
			if err != nil {
				return err
			}
			if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.GetTaskRef() != work.TaskRef ||
				response.GetRecordedTaskState() != "DEAD_LETTER" || response.GetRecordedVolumeState() != "BLOCKED" {
				return errWorkspaceVolumeClaimInvalid
			}
			return nil
		})
		metrics.tasks.WithLabelValues(work.Kind, "error").Inc()
		if ackErr != nil {
			return ackErr
		}
		return err
	}
	request := &cp.CompleteWorkspaceVolumeTaskRequest{Mutation: key("complete"), TaskRef: work.TaskRef, LeaseRef: lease.GetRef(),
		Fence: lease.GetFence(), Generation: lease.GetGeneration(), ObservedPvcUid: pvcUID, JobUid: prepared.JobUID, PodUid: prepared.PodUID,
		Ready: workspaceVolumeReadyProto(prepared.Ready)}
	pastFact := false
	err = acknowledgeOwner(ctx, timeout, func(rpc context.Context) error {
		response, err := client.CompleteWorkspaceVolumeTask(rpc, request)
		if err != nil {
			return err
		}
		if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.GetTaskRef() != work.TaskRef ||
			!validWorkspaceVolumeReceiptState(response.GetRecordedTaskState(), response.GetRecordedVolumeState()) {
			return errWorkspaceVolumeClaimInvalid
		}
		pastFact = response.GetPastFact()
		return nil
	})
	outcome := "success"
	if err != nil {
		outcome = "error"
	} else if pastFact {
		outcome = "past_fact"
	}
	metrics.tasks.WithLabelValues(work.Kind, outcome).Inc()
	if err != nil || retained == nil {
		return err
	}
	return retained.ReleaseAfterAcknowledgement(context.WithoutCancel(ctx))
}

// validWorkspaceVolumeReceiptState accepts only the owner's closed recorded state sets;
// historical metadata is not interpreted as a new live execution or model grant.
// validWorkspaceVolumeReceiptState принимает только closed recorded state sets владельца;
// historical metadata не трактуются как новое live execution либо model grant.
func validWorkspaceVolumeReceiptState(task, volume string) bool {
	if task != "SUCCEEDED" && task != "CANCELLED" && task != "DEAD_LETTER" {
		return false
	}
	switch volume {
	case "CREATE_PENDING", "CREATED", "PREPARE_PENDING", "READY", "BLOCKED", "PURGED":
		return true
	default:
		return false
	}
}

// workspaceVolumeReadyProto projects the complete sealed observation without adding authority,
// credentials, cleanup permission or a user-facing CLEAN artifact state.
// workspaceVolumeReadyProto преобразует complete sealed observation без добавления authority,
// credentials, cleanup permission либо пользовательского CLEAN artifact state.
func workspaceVolumeReadyProto(ready *runtimecontract.WorkspaceVolumeReady) *cp.RuntimeWorkspaceVolumeReady {
	if ready == nil {
		return nil
	}
	p := ready.Preparation
	return &cp.RuntimeWorkspaceVolumeReady{Schema: ready.Schema, Device: ready.Device, Inode: ready.Inode,
		Preparation: &cp.RuntimeWorkspaceVolumePreparation{Scope: &cp.RuntimeWorkspaceScope{OrganizationRef: p.Scope.OrganizationRef,
			ProjectRef: p.Scope.ProjectRef, SessionRef: p.Scope.SessionRef, AgentRef: p.Scope.AgentRef}, WorkspaceRef: p.WorkspaceRef,
			TaskRef: p.TaskRef, PvcUid: p.PVCUID, VolumeGeneration: p.VolumeGeneration}}
}
