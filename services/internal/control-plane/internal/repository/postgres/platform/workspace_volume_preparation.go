package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

var (
	// queryWorkspaceVolumeGet locks only the physical incarnation belonging to the already resolved work row.
	// queryWorkspaceVolumeGet блокирует только physical incarnation уже разрешённой work row.
	//go:embed sql/workspace_volume__get.sql
	queryWorkspaceVolumeGet string
	// queryWorkspaceVolumeCreate persists empty custody without adopting or creating a physical PVC.
	// queryWorkspaceVolumeCreate сохраняет empty custody без adoption либо создания physical PVC.
	//go:embed sql/workspace_volume__create.sql
	queryWorkspaceVolumeCreate string
	// queryWorkspaceVolumeOrigin resolves current origin/root/actor from server-owned rows before enqueue or grant.
	// queryWorkspaceVolumeOrigin разрешает current origin/root/actor из server-owned rows до enqueue либо grant.
	//go:embed sql/workspace_volume__origin.sql
	queryWorkspaceVolumeOrigin string
	// queryWorkspaceVolumeInsertTask freezes one logical task without a model revision, provider identity or secret fence.
	// queryWorkspaceVolumeInsertTask фиксирует logical task без model revision, provider identity либо secret fence.
	//go:embed sql/workspace_volume__insert_task.sql
	queryWorkspaceVolumeInsertTask string
	// queryWorkspaceVolumeIntentEligibility prevents concurrent or repeated logical preparation for the exact origin.
	// queryWorkspaceVolumeIntentEligibility предотвращает concurrent либо repeated logical preparation для exact origin.
	//go:embed sql/workspace_volume__intent_eligibility.sql
	queryWorkspaceVolumeIntentEligibility string
	// queryWorkspaceVolumePrepare pins the original preparation before inserting its deferred task reference.
	// queryWorkspaceVolumePrepare закрепляет original preparation до вставки deferred task reference.
	//go:embed sql/workspace_volume__prepare.sql
	queryWorkspaceVolumePrepare string
)

// workspaceVolumeCustody is an owner-locked row, never an authority claim made by a physical observer.
// workspaceVolumeCustody — owner-locked row, не authority claim physical observer.
type workspaceVolumeCustody struct {
	id, ref, state, createTaskRef, preparationTaskRef, pvcUID, readyRecord string
	generation, version                                                    int64
}

// scanWorkspaceVolumeCustody preserves the distinction between absence and a failed/corrupt custody lookup.
// scanWorkspaceVolumeCustody сохраняет отличие отсутствия от failed/corrupt custody lookup.
func scanWorkspaceVolumeCustody(row pgx.Row) (workspaceVolumeCustody, error) {
	var value workspaceVolumeCustody
	err := row.Scan(&value.id, &value.ref, &value.generation, &value.version, &value.state, &value.createTaskRef,
		&value.preparationTaskRef, &value.pvcUID, &value.readyRecord)
	return value, err
}

// authorizeWorkspaceVolumeOrigin checks the active exact employee origin and fresh root actor ACL without trusting a payload actor or project.
// It reuses the owner's existing subject resolution and access policy; volume preparation grants no model or integration capability.
// authorizeWorkspaceVolumeOrigin проверяет active exact employee origin и fresh root actor ACL без доверия к payload actor/project.
// Используются существующие owner subject resolution/access policy; volume preparation не выдаёт model/integration capability.
func (repository *Repository) authorizeWorkspaceVolumeOrigin(ctx context.Context, tx pgx.Tx, current scope, nodeID, rootRunID string, expected runtimecontract.WorkspaceStorageScope) (string, error) {
	var actorID, rootRef, projectRef, sessionRef, agentRef, rootTargetType, rootTargetRef string
	err := tx.QueryRow(ctx, queryWorkspaceVolumeOrigin, pgx.StrictNamedArgs{"organization_id": current.organizationID,
		"node_id": nodeID, "root_run_id": rootRunID}).Scan(&actorID, &rootRef, &projectRef, &sessionRef, &agentRef, &rootTargetType, &rootTargetRef)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", errs.ErrUnavailable
	}
	if err != nil || expected.OrganizationRef != current.organizationRef || expected.ProjectRef != projectRef ||
		expected.SessionRef != sessionRef || expected.AgentRef != agentRef {
		return "", errs.ErrForbidden
	}
	actor, err := repository.secretDraftOwnerScope(ctx, tx, current, actorID)
	if errors.Is(err, errs.ErrUnavailable) {
		return "", err
	}
	if err != nil {
		return "", errs.ErrForbidden
	}
	var launchPermission string
	switch rootTargetType {
	case "AGENT":
		launchPermission = "agent.launch"
	case "WORKFLOW":
		launchPermission = "workflow.launch"
	default:
		return "", errs.ErrForbidden
	}
	for _, target := range []struct{ permission, kind, ref string }{{launchPermission, rootTargetType, rootTargetRef}, {"run.view", "RUN", rootRef}} {
		err := repository.requireAccess(ctx, tx, actor, target.permission, entity.AccessScope{ResourceKind: target.kind, ResourceRef: target.ref})
		if errors.Is(err, errs.ErrUnavailable) {
			return "", err
		}
		if err != nil {
			return "", errs.ErrForbidden
		}
	}
	var originAllowed bool
	if err := tx.QueryRow(ctx, queryWorkflowLaunchClaimOrigin, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "root_run_id": rootRunID}).Scan(&originAllowed); err != nil {
		return "", errs.ErrUnavailable
	}
	if !originAllowed {
		return "", errs.ErrForbidden
	}
	return actorID, nil
}

// prepareWorkspaceVolumeTx persists a first CREATE intent or returns a previously accepted exact physical tuple.
// It is called only after candidate authority/configuration validation, before any model lease/RR/private write.
// prepareWorkspaceVolumeTx сохраняет первый CREATE intent либо возвращает ранее accepted exact physical tuple.
// Вызов допускается после candidate authority/configuration validation, до model lease/RR/private write.
func (repository *Repository) prepareWorkspaceVolumeTx(ctx context.Context, tx pgx.Tx, current scope, candidate claimableExecution, binding runtimecontract.WorkspaceStorageBinding) (*runtimecontract.WorkspaceVolumeReady, bool, error) {
	value, err := scanWorkspaceVolumeCustody(tx.QueryRow(ctx, queryWorkspaceVolumeGet, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "workspace_ref": binding.WorkspaceRef}))
	if errors.Is(err, pgx.ErrNoRows) {
		actorID, err := repository.authorizeWorkspaceVolumeOrigin(ctx, tx, current, candidate.nodeID, candidate.rootRunID, binding.Scope)
		if err != nil {
			return nil, false, err
		}
		volumeRef, err := newRef("wvl")
		if err != nil {
			return nil, false, err
		}
		taskRef, err := newRef("svt")
		if err != nil {
			return nil, false, err
		}
		value, err = scanWorkspaceVolumeCustody(tx.QueryRow(ctx, queryWorkspaceVolumeCreate, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "workspace_ref": binding.WorkspaceRef,
			"volume_ref": volumeRef, "create_task_ref": taskRef}))
		if err != nil {
			return nil, false, errs.ErrConflict
		}
		logical := runtimecontract.WorkspaceVolumeInput{Profile: runtimecontract.WorkspaceVolumeLocalPathProfile,
			Scope: binding.Scope, WorkspaceRef: binding.WorkspaceRef, VolumeGeneration: value.generation, CreateTaskRef: taskRef}
		if err := repository.enqueueWorkspaceVolumeIntentTx(ctx, tx, current, candidate, value, taskRef, actorID, runtimecontract.WorkspaceVolumeCreate, logical); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errs.ErrUnavailable
	}
	if value.state != "READY" {
		if err := repository.prepareWorkspaceVolumeIntentTx(ctx, tx, current, candidate, binding, value); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	ready, err := runtimecontract.DecodeWorkspaceVolumeReady([]byte(value.readyRecord))
	if err != nil || ready.Preparation.Scope != binding.Scope || ready.Preparation.WorkspaceRef != binding.WorkspaceRef ||
		ready.Preparation.TaskRef != value.preparationTaskRef || ready.Preparation.PVCUID != value.pvcUID ||
		ready.Preparation.VolumeGeneration != value.generation {
		return nil, false, errs.ErrConflict
	}
	return &ready, true, nil
}

// enqueueWorkspaceVolumeIntentTx stores the closed owner-built body and its audit/event without accepting actor or kind from an external payload.
// enqueueWorkspaceVolumeIntentTx сохраняет closed owner-built body и audit/event без actor/kind из external payload.
func (repository *Repository) enqueueWorkspaceVolumeIntentTx(ctx context.Context, tx pgx.Tx, current scope, candidate claimableExecution, custody workspaceVolumeCustody, taskRef, actorID, kind string, logical runtimecontract.WorkspaceVolumeInput) error {
	work := runtimecontract.WorkspaceVolumeWork{TaskRef: taskRef, Kind: kind, Attempt: 1, Volume: logical, ReconcileOnly: logical.ReadOnly}
	if _, err := work.Digest(); err != nil {
		return errs.ErrConflict
	}
	raw, err := json.Marshal(logical)
	if err != nil {
		return errs.ErrConflict
	}
	var storedRef string
	if err := tx.QueryRow(ctx, queryWorkspaceVolumeInsertTask, pgx.StrictNamedArgs{
		"task_ref": taskRef, "volume_id": custody.id, "volume_generation": custody.generation,
		"node_id": candidate.nodeID, "root_run_id": candidate.rootRunID, "actor_id": actorID,
		"kind": kind, "frozen_input": raw}).Scan(&storedRef); err != nil || storedRef != taskRef {
		return errs.ErrConflict
	}
	if err := auditWorkspaceVolumeTask(ctx, tx, current, candidate.projectID, taskRef, "Подготавливается рабочая папка"); err != nil {
		return err
	}
	return repository.emitSessionStorageRunChanged(ctx, tx, current, candidate.sessionID)
}

// prepareWorkspaceVolumeIntentTx advances a known positive Create or grants one read-only recovery after a retained positive preparation fact.
// Unknown or partial custody receives no init credit, and the same physical origin cannot retry by manufacturing new logical tasks.
// prepareWorkspaceVolumeIntentTx продвигает known positive Create либо выдаёт один read-only recovery после retained positive preparation fact.
// Unknown/partial custody не получает init credit; тот же physical origin не обходит retry limit созданием новых logical tasks.
func (repository *Repository) prepareWorkspaceVolumeIntentTx(ctx context.Context, tx pgx.Tx, current scope, candidate claimableExecution, binding runtimecontract.WorkspaceStorageBinding, custody workspaceVolumeCustody) error {
	if custody.pvcUID == "" || custody.state != "CREATED" && custody.state != "PREPARE_PENDING" && custody.state != "BLOCKED" {
		return nil
	}
	readOnly := custody.preparationTaskRef != ""
	if !readOnly && custody.state != "CREATED" {
		return nil
	}
	if readOnly {
		if custody.readyRecord == "" {
			// Unknown first preparation waits for a positive exact Job join without creating another initializer.
			// Unknown первая подготовка ждёт positive exact Job join без нового initializer.
			return nil
		}
		ready, err := runtimecontract.DecodeWorkspaceVolumeReady([]byte(custody.readyRecord))
		if err != nil {
			return errs.ErrConflict
		}
		if ready.Preparation.Scope != binding.Scope || ready.Preparation.WorkspaceRef != binding.WorkspaceRef ||
			ready.Preparation.TaskRef != custody.preparationTaskRef || ready.Preparation.PVCUID != custody.pvcUID ||
			ready.Preparation.VolumeGeneration != custody.generation {
			return errs.ErrConflict
		}
	}
	actorID, err := repository.authorizeWorkspaceVolumeOrigin(ctx, tx, current, candidate.nodeID, candidate.rootRunID, binding.Scope)
	if err != nil {
		return err
	}
	var open, repeated bool
	if err := tx.QueryRow(ctx, queryWorkspaceVolumeIntentEligibility, pgx.StrictNamedArgs{
		"volume_id": custody.id, "generation": custody.generation, "node_id": candidate.nodeID,
		"root_run_id": candidate.rootRunID, "actor_id": actorID, "read_only": readOnly}).Scan(&open, &repeated); err != nil {
		return errs.ErrUnavailable
	}
	if open || repeated {
		return nil
	}
	taskRef, err := newRef("svt")
	if err != nil {
		return err
	}
	preparationRef := custody.preparationTaskRef
	if preparationRef == "" {
		preparationRef = taskRef
	}
	custody, err = scanWorkspaceVolumeCustody(tx.QueryRow(ctx, queryWorkspaceVolumePrepare, pgx.StrictNamedArgs{
		"volume_id": custody.id, "generation": custody.generation, "pvc_uid": custody.pvcUID,
		"preparation_task_ref": preparationRef}))
	if err != nil {
		return errs.ErrConflict
	}
	logical := runtimecontract.WorkspaceVolumeInput{Profile: runtimecontract.WorkspaceVolumeLocalPathProfile,
		Scope: binding.Scope, WorkspaceRef: binding.WorkspaceRef, VolumeGeneration: custody.generation,
		CreateTaskRef: custody.createTaskRef, PreparationTaskRef: preparationRef, PVCUID: custody.pvcUID, ReadOnly: readOnly}
	return repository.enqueueWorkspaceVolumeIntentTx(ctx, tx, current, candidate, custody, taskRef, actorID, runtimecontract.WorkspaceVolumePrepare, logical)
}

// auditWorkspaceVolumeTask records one owner mutation without input bytes, secret fences or physical data in the public audit summary.
// auditWorkspaceVolumeTask записывает owner mutation без input bytes, secret fences либо physical data в public audit summary.
func auditWorkspaceVolumeTask(ctx context.Context, tx pgx.Tx, current scope, projectID, taskRef, summary string) error {
	ref, err := newRef("aud")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, ref, current.organizationID, projectID,
		current.actorID, "workspace.volume.task", "WORKSPACE_VOLUME_TASK", taskRef, summary, current.correlationRef); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}
