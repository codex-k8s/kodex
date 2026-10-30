package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

var (
	// queryWorkspaceArchiveCandidates locks eligible sessions before any workspace/task mutation.
	// queryWorkspaceArchiveCandidates блокирует подходящие sessions до workspace/task mutation.
	//go:embed sql/workspace_archive__candidates.sql
	queryWorkspaceArchiveCandidates string
	// queryWorkspaceArchiveLock resolves the current released writer and immutable source revision.
	// queryWorkspaceArchiveLock разрешает current released writer и immutable source revision.
	//go:embed sql/workspace_archive__lock.sql
	queryWorkspaceArchiveLock string
	// queryWorkspaceArchiveTask locks the unique snapshot intent for one workspace generation.
	// queryWorkspaceArchiveTask блокирует единственный snapshot intent поколения workspace.
	//go:embed sql/workspace_archive__task.sql
	queryWorkspaceArchiveTask string
	// queryWorkspaceArchiveGeneration reads the shared session content high-watermark under its lock.
	// queryWorkspaceArchiveGeneration читает общий session content high-watermark под её lock.
	//go:embed sql/workspace_archive__generation.sql
	queryWorkspaceArchiveGeneration string
	// queryWorkspaceArchiveCreate persists the bounded owner-selected immutable task body.
	// queryWorkspaceArchiveCreate сохраняет bounded owner-selected immutable task body.
	//go:embed sql/workspace_archive__create.sql
	queryWorkspaceArchiveCreate string
	// queryWorkspaceArchiveClaim issues one fresh attempt and monotonically fenced lease.
	// queryWorkspaceArchiveClaim выдаёт одну fresh attempt и монотонно ограждённый lease.
	//go:embed sql/workspace_archive__claim.sql
	queryWorkspaceArchiveClaim string
	// queryWorkspaceArchiveExpire closes the final expired grant while retaining the checkpoint intent and PVC.
	// queryWorkspaceArchiveExpire закрывает последний expired grant, сохраняя checkpoint intent и PVC.
	//go:embed sql/workspace_archive__expire.sql
	queryWorkspaceArchiveExpire string
)

// workspaceArchiveState holds data custody only; it never creates execution authority for a terminal run.
// workspaceArchiveState содержит только data custody и не создаёт execution authority terminal run.
type workspaceArchiveState struct {
	id, ref, state, joinRaw, revisionID, revisionRef, revisionDigest, providerID string
	organizationID, projectID, sessionID                                         string
	generation, version, revisionVersion                                         int64
	released                                                                     bool
	scope                                                                        runtimecontract.WorkspaceStorageScope
	policy                                                                       runtimecontract.RuntimeWorkspacePolicy
	source                                                                       *runtimecontract.WorkspaceCheckpointBinding
	sourceArchiveID                                                              string
}

// workspaceArchiveTask is a stored intent plus its current lease; the task body is immutable across attempts.
// workspaceArchiveTask — сохранённый intent и current lease; task body неизменен между attempts.
type workspaceArchiveTask struct {
	id, ref, kind, state, digest, objectKey, objectVersion string
	contentGeneration, generation, workspaceGeneration     int64
	attempt, maximumAttempts                               int32
	body                                                   []byte
	leaseRef, fenceDigest                                  *string
	expiresAt                                              *time.Time
}

// readWorkspaceArchiveState requires the caller to hold the shared session lock before locking custody.
// readWorkspaceArchiveState требует shared session lock до блокировки custody.
func readWorkspaceArchiveState(ctx context.Context, tx pgx.Tx, organizationID, sessionID string) (workspaceArchiveState, error) {
	result := workspaceArchiveState{sessionID: sessionID}
	var policyRaw []byte
	err := tx.QueryRow(ctx, queryWorkspaceArchiveLock, pgx.StrictNamedArgs{"organization_id": organizationID, "session_id": sessionID}).Scan(
		&result.id, &result.ref, &result.generation, &result.version, &result.state, &result.released, &result.joinRaw,
		&result.scope.OrganizationRef, &result.scope.ProjectRef, &result.scope.SessionRef, &result.scope.AgentRef,
		&result.revisionID, &result.revisionRef, &result.revisionVersion, &result.revisionDigest, &result.providerID,
		&policyRaw, &result.organizationID, &result.projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	var policy entity.RuntimeWorkspacePolicy
	if runtimecontract.DecodeWorkspaceStorageTaskJSON(policyRaw, &policy) != nil {
		return result, errs.ErrConflict
	}
	result.policy = runtimeWorkspacePolicyContract(policy)
	if result.policy.Validate() != nil {
		return result, errs.ErrForbidden
	}
	if result.state == "CHECKPOINT_PENDING" && result.released && result.joinRaw != "" {
		return result, nil
	}
	checkpointed := result.state == "CHECKPOINT_READY" && result.released && result.joinRaw != ""
	restoring := (result.state == "RESTORE_READY" || result.state == "RESTORING") && !result.released && result.joinRaw == ""
	if !checkpointed && !restoring {
		return result, errs.ErrForbidden
	}
	source, sourceID, err := readWorkspaceRestoreSource(ctx, tx, result)
	if err != nil {
		return result, err
	}
	result.source, result.sourceArchiveID = &source, sourceID
	return result, nil
}

// runtimeWorkspacePolicyContract copies the frozen domain policy without selecting new limits or rules.
// runtimeWorkspacePolicyContract копирует frozen domain policy без выбора новых limits либо rules.
func runtimeWorkspacePolicyContract(policy entity.RuntimeWorkspacePolicy) runtimecontract.RuntimeWorkspacePolicy {
	result := runtimecontract.RuntimeWorkspacePolicy{Revision: policy.Revision, Root: policy.Root, Digest: policy.Digest,
		MaximumWritableBytes: policy.MaximumWritableBytes, MaximumFileCount: policy.MaximumFileCount,
		DenialReasons: append([]string(nil), policy.DenialReasons...)}
	for _, rule := range policy.Rules {
		result.Rules = append(result.Rules, runtimecontract.RuntimeWorkspacePathRule{Path: rule.Path, Access: rule.Access})
	}
	return result
}

// readWorkspaceArchiveTask distinguishes an absent intent from a corrupted or unavailable stored task.
// readWorkspaceArchiveTask отличает отсутствующий intent от повреждённой либо недоступной stored task.
func readWorkspaceArchiveTask(ctx context.Context, tx pgx.Tx, state workspaceArchiveState) (workspaceArchiveTask, error) {
	var task workspaceArchiveTask
	err := tx.QueryRow(ctx, queryWorkspaceArchiveTask, pgx.StrictNamedArgs{"organization_id": state.organizationID,
		"workspace_id": state.id, "workspace_generation": state.generation, "kind": workspaceArchiveTaskKind(state)}).Scan(&task.id, &task.ref, &task.kind, &task.state,
		&task.contentGeneration, &task.digest, &task.objectKey, &task.objectVersion, &task.attempt, &task.maximumAttempts,
		&task.generation, &task.workspaceGeneration, &task.body, &task.leaseRef, &task.fenceDigest, &task.expiresAt)
	return task, err
}

// workspaceArchiveWork reconstructs the exact attempt from immutable state and checks its complete source binding.
// The stored body retains first-attempt semantics; later grants receive reconciliation-only credit.
// workspaceArchiveWork восстанавливает exact attempt из immutable state и проверяет полный source binding.
// Stored body сохраняет first-attempt semantics; поздние grants получают только reconciliation credit.
func workspaceArchiveWork(state workspaceArchiveState, task workspaceArchiveTask, attempt int32) (runtimecontract.WorkspaceArchiveWork, error) {
	var body runtimecontract.WorkspaceArchiveTask
	if runtimecontract.DecodeWorkspaceStorageTaskJSON(task.body, &body) != nil || body.ReconcileOnly ||
		body.Target.Scope != state.scope || body.Target.WorkspaceRef != state.ref || body.Target.Generation != state.generation ||
		body.Quiescence != state.joinRaw || body.Policy.Digest != state.policy.Digest || task.workspaceGeneration != state.generation ||
		task.kind != workspaceArchiveTaskKind(state) {
		return runtimecontract.WorkspaceArchiveWork{}, errs.ErrConflict
	}
	if task.kind == "RESTORE" && (body.Checkpoint == nil || state.source == nil || *body.Checkpoint != *state.source) {
		return runtimecontract.WorkspaceArchiveWork{}, errs.ErrConflict
	}
	body.ReconcileOnly = attempt > 1
	pvc, err := runtimecontract.SessionPVCName(state.scope.SessionRef)
	if err != nil {
		return runtimecontract.WorkspaceArchiveWork{}, errs.ErrConflict
	}
	work := runtimecontract.WorkspaceArchiveWork{TaskRef: task.ref, Kind: task.kind, OrganizationRef: state.scope.OrganizationRef,
		ProjectRef: state.scope.ProjectRef, SessionRef: state.scope.SessionRef, RuntimeRevisionRef: state.revisionRef,
		RuntimeRevisionVersion: state.revisionVersion, RuntimeRevisionDigest: state.revisionDigest, ContentGeneration: task.contentGeneration,
		PVCName: pvc, InputDigest: task.digest, TargetObjectKey: task.objectKey, TargetObjectVersion: task.objectVersion, Attempt: attempt, Workspace: &body}
	if work.Validate() != nil {
		return runtimecontract.WorkspaceArchiveWork{}, errs.ErrConflict
	}
	return work, nil
}

// workspaceArchiveDigest binds every typed attempt field except the digest's own self-reference.
// workspaceArchiveDigest связывает все typed attempt fields, исключая собственную self-reference digest.
func workspaceArchiveDigest(work runtimecontract.WorkspaceArchiveWork) (string, error) {
	work.InputDigest = ""
	raw, err := json.Marshal(work)
	if err != nil || len(raw) > runtimecontract.MaximumWorkspaceManifestBytes {
		return "", errs.ErrConflict
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// createWorkspaceSnapshot persists a single logical intent without granting a worker or changing workspace state.
// createWorkspaceSnapshot сохраняет единственный logical intent без worker grant и смены workspace state.
func createWorkspaceSnapshot(ctx context.Context, tx pgx.Tx, state workspaceArchiveState) (workspaceArchiveTask, error) {
	var task workspaceArchiveTask
	var highWatermark int64
	if err := tx.QueryRow(ctx, queryWorkspaceArchiveGeneration, pgx.StrictNamedArgs{"session_id": state.sessionID}).Scan(&highWatermark); err != nil {
		return task, errs.ErrUnavailable
	}
	if highWatermark >= math.MaxInt64 {
		return task, errs.ErrConflict
	}
	checkpoint, err := newRef("wcp")
	if err != nil {
		return task, err
	}
	joined, err := runtimecontract.DecodeWorkspaceQuiescence([]byte(state.joinRaw))
	if err != nil {
		return task, errs.ErrConflict
	}
	body := runtimecontract.WorkspaceArchiveTask{Target: runtimecontract.WorkspaceStorageBinding{Profile: runtimecontract.WorkspaceStorageProfile,
		Scope: state.scope, WorkspaceRef: state.ref, Generation: state.generation}, CheckpointRef: checkpoint, Quiescence: state.joinRaw,
		PVCUID: joined.Writer.PVCUID, Policy: state.policy}
	identity, err := body.SnapshotIdentity()
	if err != nil {
		return task, errs.ErrConflict
	}
	task.ref, err = newRef("sat")
	if err != nil {
		return task, err
	}
	task.kind, task.state, task.contentGeneration, task.workspaceGeneration = "SNAPSHOT", "READY", highWatermark+1, state.generation
	task.maximumAttempts, task.objectKey = sessionArchiveMaxAttempts, identity.ObjectPrefix()+"manifest.json"
	task.body, err = json.Marshal(body)
	if err != nil {
		return task, errs.ErrConflict
	}
	// A temporary syntactic digest permits validation before computing the closed immutable input hash.
	// Временный syntactic digest позволяет validation до расчёта закрытого immutable input hash.
	task.digest = state.revisionDigest
	work, err := workspaceArchiveWork(state, task, 1)
	if err != nil {
		return task, err
	}
	task.digest, err = workspaceArchiveDigest(work)
	if err != nil {
		return task, err
	}
	err = tx.QueryRow(ctx, queryWorkspaceArchiveCreate, pgx.StrictNamedArgs{"task_ref": task.ref, "organization_id": state.organizationID,
		"project_id": state.projectID, "session_id": state.sessionID, "content_generation": task.contentGeneration, "input_digest": task.digest,
		"object_key": task.objectKey, "workspace_id": state.id, "workspace_generation": state.generation, "workspace_input": string(task.body)}).Scan(&task.id)
	if err != nil {
		return task, errs.ErrUnavailable
	}
	return task, nil
}

// claimWorkspaceArchiveTasks runs under the outer claim transaction and consumes only its existing task budget.
// It never allocates a second logical checkpoint when a terminal or uncertain intent already exists.
// claimWorkspaceArchiveTasks работает в outer claim transaction и использует только её существующий task budget.
// Второй logical checkpoint не создаётся при уже существующем terminal либо uncertain intent.
func (repository *Repository) claimWorkspaceArchiveTasks(ctx context.Context, tx pgx.Tx, current scope, instance string, limit int32) ([]map[string]any, error) {
	rows, err := tx.Query(ctx, queryWorkspaceArchiveCandidates, pgx.StrictNamedArgs{"organization_id": current.organizationID, "limit": limit})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	var sessions []string
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		sessions = append(sessions, sessionID)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	items := make([]map[string]any, 0, len(sessions))
	for _, sessionID := range sessions {
		state, err := readWorkspaceArchiveState(ctx, tx, current.organizationID, sessionID)
		if err != nil {
			return nil, err
		}
		task, err := readWorkspaceArchiveTask(ctx, tx, state)
		if errors.Is(err, pgx.ErrNoRows) && state.state == "CHECKPOINT_PENDING" {
			task, err = createWorkspaceSnapshot(ctx, tx, state)
		}
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		if task.attempt >= task.maximumAttempts {
			var ref string
			if err := tx.QueryRow(ctx, queryWorkspaceArchiveExpire, pgx.StrictNamedArgs{"task_id": task.id, "generation": task.generation}).Scan(&ref); err != nil || ref != task.ref {
				return nil, errs.ErrConflict
			}
			if err := auditWorkspaceArchiveClaim(ctx, tx, current, state, task.ref, "DEAD_LETTER"); err != nil {
				return nil, err
			}
			if err := repository.emitSessionStorageRunChanged(ctx, tx, current, sessionID); err != nil {
				return nil, err
			}
			continue
		}
		work, err := workspaceArchiveWork(state, task, task.attempt+1)
		if err != nil {
			return nil, err
		}
		work.InputDigest, err = workspaceArchiveDigest(work)
		if err != nil {
			return nil, err
		}
		lease, err := newRef("lea")
		if err != nil {
			return nil, err
		}
		fence, err := newRef("fnc")
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256([]byte(fence))
		expires := time.Now().UTC().Add(sessionArchiveLeaseDuration)
		var attempt int32
		var generation int64
		if err := tx.QueryRow(ctx, queryWorkspaceArchiveClaim, pgx.StrictNamedArgs{"task_id": task.id, "organization_id": state.organizationID,
			"workspace_id": state.id, "workspace_generation": state.generation, "expected_generation": task.generation,
			"input_digest": work.InputDigest, "workload_instance": instance, "lease_ref": lease,
			"fence_digest": hex.EncodeToString(digest[:]), "lease_expires_at": expires}).Scan(&attempt, &generation); err != nil || attempt != work.Attempt {
			return nil, errs.ErrConflict
		}
		items = append(items, map[string]any{"workspaceWork": work, "leaseRef": lease, "fence": fence, "generation": generation, "expiresAt": expires})
		if err := auditWorkspaceArchiveClaim(ctx, tx, current, state, task.ref, "READY"); err != nil {
			return nil, err
		}
		if err := repository.emitSessionStorageRunChanged(ctx, tx, current, sessionID); err != nil {
			return nil, err
		}
	}
	return items, nil
}

// auditWorkspaceArchiveClaim records prepared or exhausted archival work in the same owner transaction.
// It stores no fence, source path, object key or private task bytes in the audit projection.
// auditWorkspaceArchiveClaim записывает prepared либо exhausted archival work в той же owner transaction.
// Fence, source path, object key и private task bytes в audit projection не попадают.
func auditWorkspaceArchiveClaim(ctx context.Context, tx pgx.Tx, current scope, state workspaceArchiveState, taskRef, outcome string) error {
	ref, err := newRef("aud")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, ref, state.organizationID, state.projectID, current.actorID,
		"platform.session-archive.tasks.claim", "SESSION_ARCHIVE_TASK", taskRef, sessionArchiveTaskSummary(outcome), current.correlationRef); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}
