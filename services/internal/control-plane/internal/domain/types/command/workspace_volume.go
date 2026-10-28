package command

import "github.com/codex-k8s/kodex/libs/go/runtimecontract"

const (
	// ClaimWorkspaceVolumeTasks issues owner-selected immutable attempts through a durable idempotency receipt.
	// ClaimWorkspaceVolumeTasks выдаёт owner-selected immutable attempts через durable idempotency receipt.
	ClaimWorkspaceVolumeTasks Kind = "CLAIM_WORKSPACE_VOLUME_TASKS"
	// CompleteWorkspaceVolumeTask records a positive physical observation without reopening an expired or cancelled grant.
	// CompleteWorkspaceVolumeTask сохраняет positive physical observation без открытия expired/cancelled grant.
	CompleteWorkspaceVolumeTask Kind = "COMPLETE_WORKSPACE_VOLUME_TASK"
	// FailWorkspaceVolumeTask closes the exact current grant while retaining uncertain physical custody.
	// FailWorkspaceVolumeTask закрывает exact current grant, сохраняя uncertain physical custody.
	FailWorkspaceVolumeTask Kind = "FAIL_WORKSPACE_VOLUME_TASK"
	// WorkspaceVolumeRenewPermission authorizes only the dedicated physical-task renewal RPC.
	// WorkspaceVolumeRenewPermission разрешает только dedicated physical-task renewal RPC.
	WorkspaceVolumeRenewPermission = "platform.workspace-volumes.tasks.renew"
)

// WorkspaceVolumeCommandPermission closes kind-to-operation routing; an unknown command receives no physical-work authority.
// WorkspaceVolumeCommandPermission замыкает kind-to-operation routing; unknown command не получает physical-work authority.
func WorkspaceVolumeCommandPermission(kind Kind) string {
	switch kind {
	case ClaimWorkspaceVolumeTasks:
		return "platform.workspace-volumes.tasks.claim"
	case CompleteWorkspaceVolumeTask:
		return "platform.workspace-volumes.tasks.complete"
	case FailWorkspaceVolumeTask:
		return "platform.workspace-volumes.tasks.fail"
	default:
		return ""
	}
}

// WorkspaceVolumeClaimInput bounds the registered controller batch without accepting actor, scope or operation authority.
// WorkspaceVolumeClaimInput ограничивает batch зарегистрированного controller без caller authority для actor/scope/operation.
type WorkspaceVolumeClaimInput struct {
	WorkloadInstance string
	Limit            int32
}

// WorkspaceVolumeTaskInput addresses an exact durable attempt; persisted task kind selects the positive report shape.
// Ready contains observation metadata, never an instruction to assign a caller-selected owner state.
// WorkspaceVolumeTaskInput адресует exact durable attempt; persisted task kind выбирает форму positive report.
// Ready содержит observation metadata, не команду присвоить caller-selected owner state.
type WorkspaceVolumeTaskInput struct {
	TaskRef, LeaseRef, Fence, ObservedPVCUID, PodUID, SafeErrorCode string
	Generation                                                      int64
	Ready                                                           *runtimecontract.WorkspaceVolumeReady
}
