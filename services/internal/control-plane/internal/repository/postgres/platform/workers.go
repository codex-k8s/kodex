package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) ReconcileWarmRuntime(ctx context.Context, principal value.Principal, instance string) (entity.SystemAssistant, map[string]any, bool, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.SystemAssistant{}, nil, false, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var assistant entity.SystemAssistant
	var limits []byte
	var promptRef, promptDigest, promptContent, ownerInstructions, systemSessionRef, warmInstance, runtimeKey, profileRevision, provider, model string
	err = tx.QueryRow(ctx, queryWorkersReconcilewarmruntime1, scope.organizationID).Scan(&assistant.Ref, &assistant.StableKey, &assistant.Name, &assistant.Purpose, &assistant.CorePromptRevision, &ownerInstructions, &assistant.RuntimeState, &assistant.RuntimeRevision, &assistant.DesiredRuntimeRevision, &systemSessionRef, &limits, &assistant.LastHeartbeatAt, &assistant.Version, &assistant.UpdatedAt, &promptRef, &promptDigest, &promptContent, &warmInstance, &runtimeKey, &profileRevision, &provider, &model)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, errs.ErrUnavailable
	}
	_ = json.Unmarshal(limits, &assistant.ResourceLimits)
	assistant.OwnerInstructions = ownerInstructions
	assistant.WarmSessionRef = systemSessionRef
	assistant.System = true
	assistant.Deletable = false
	stale := assistant.LastHeartbeatAt == nil || time.Since(*assistant.LastHeartbeatAt) > 45*time.Second
	required := !contains([]string{"READY", "BUSY"}, assistant.RuntimeState) || assistant.RuntimeRevision != assistant.DesiredRuntimeRevision || warmInstance != instance || stale
	if required {
		if _, err := tx.Exec(ctx, queryWorkersReconcilewarmruntime2, scope.organizationID, instance); err != nil {
			return entity.SystemAssistant{}, nil, false, errs.ErrUnavailable
		}
		assistant.RuntimeState = "RECOVERING"
		assistant.Version++
	}
	snapshot := map[string]any{"assistantRef": assistant.Ref, "stableKey": assistant.StableKey, "systemSessionRef": systemSessionRef, "runtimeRevision": assistant.DesiredRuntimeRevision, "runtimeKey": runtimeKey, "profileRevision": profileRevision, "runtimeProvider": provider, "runtimeModel": model, "corePromptRef": promptRef, "corePromptDigest": promptDigest, "corePrompt": promptContent, "ownerInstructions": ownerInstructions, "resourceLimits": assistant.ResourceLimits, "directSecretAccess": false}
	if err := tx.Commit(ctx); err != nil {
		return entity.SystemAssistant{}, nil, false, errs.ErrConflict
	}
	return assistant, snapshot, required, nil
}

func (repository *Repository) reportWarmRuntime(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.WarmRuntimeInput)
	if !ok || payload.WorkloadInstance == "" || payload.RuntimeRevision == "" || !contains([]string{"STARTING", "READY", "BUSY", "RECOVERING", "UNAVAILABLE"}, payload.State) {
		return commandOutcome{}, errs.ErrInvalid
	}
	var assistant entity.SystemAssistant
	var limits []byte
	err := tx.QueryRow(ctx, queryWorkersReportwarmruntime1, scope.organizationID, payload.WorkloadInstance, payload.RuntimeRevision, payload.State).Scan(&assistant.StableKey, &assistant.CorePromptRevision, &assistant.OwnerInstructions, &assistant.RuntimeState, &assistant.RuntimeRevision, &assistant.DesiredRuntimeRevision, &assistant.WarmSessionRef, &limits, &assistant.LastHeartbeatAt, &assistant.Version, &assistant.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrConflict
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	_ = json.Unmarshal(limits, &assistant.ResourceLimits)
	assistant.System = true
	assistant.Ready = contains([]string{"READY", "BUSY"}, payload.State)
	return commandOutcome{result: command.Result{Assistant: &assistant}, resourceKind: "SYSTEM_ASSISTANT", resourceRef: assistant.StableKey, summary: "Warm runtime heartbeat recorded", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

func (repository *Repository) ClaimDueSchedules(ctx context.Context, principal value.Principal, instance string, limit int32) ([]map[string]any, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, queryWorkersClaimdueschedules1, scope.organizationID, limit)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		var scheduleID, scheduleRef string
		var scheduledFor time.Time
		var version int64
		if err := rows.Scan(&scheduleID, &scheduleRef, &scheduledFor, &version); err != nil {
			return nil, errs.ErrUnavailable
		}
		occurrenceRef, _ := newRef("occ")
		leaseRef, _ := newRef("lea")
		fence, _ := newRef("fnc")
		digest := sha256.Sum256([]byte(fence))
		expires := time.Now().UTC().Add(30 * time.Second)
		if _, err := tx.Exec(ctx, queryWorkersClaimdueschedules2, occurrenceRef, scope.organizationID, scheduleID, scheduledFor, leaseRef, hex.EncodeToString(digest[:]), instance, expires); err != nil {
			return nil, mapWriteError(err)
		}
		result = append(result, map[string]any{"scheduleRef": scheduleRef, "occurrenceRef": occurrenceRef, "scheduledFor": scheduledFor, "leaseRef": leaseRef, "fence": fence, "generation": int64(1), "expiresAt": expires, "scheduleVersion": version})
	}
	if err := rows.Err(); err != nil {
		return nil, errs.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return result, nil
}

func (repository *Repository) changeOccurrence(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.OccurrenceInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	var occurrenceID, scheduleID, projectID, projectRef, state, storedDigest, targetType, targetRef, name string
	var generation int64
	var expires time.Time
	err := tx.QueryRow(ctx, queryWorkersChangeoccurrence1, scope.organizationID, payload.OccurrenceRef, payload.LeaseRef).Scan(&occurrenceID, &scheduleID, &projectID, &projectRef, &state, &storedDigest, &generation, &expires, &targetType, &targetRef, &name)
	if err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	digest := sha256.Sum256([]byte(payload.Fence))
	if storedDigest != hex.EncodeToString(digest[:]) || generation != payload.Generation || time.Now().After(expires) {
		return commandOutcome{}, errs.ErrForbidden
	}
	if input.Kind == command.MaterializeOccurrence {
		if state != "CLAIMED" {
			return commandOutcome{}, errs.ErrConflict
		}
		var scheduleInput []byte
		if err := tx.QueryRow(ctx, queryWorkersChangeoccurrence2, scheduleID).Scan(&scheduleInput); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		var data map[string]any
		_ = json.Unmarshal(scheduleInput, &data)
		nested := input
		nested.Kind = command.LaunchRun
		nested.Payload = command.LaunchRunInput{ProjectRef: projectRef, Title: name, Task: "Запуск по расписанию", Source: "SCHEDULE", Target: entity.RunTarget{Type: targetType, Ref: targetRef}, Input: data}
		outcome, err := repository.launchRun(ctx, tx, scope, nested)
		if err != nil {
			return commandOutcome{}, err
		}
		var runID string
		_ = tx.QueryRow(ctx, queryWorkersChangeoccurrence3, outcome.result.Run.Ref).Scan(&runID)
		_, _ = tx.Exec(ctx, queryWorkersChangeoccurrence4, occurrenceID, runID)
		outcome.resourceKind = "SCHEDULE_OCCURRENCE"
		outcome.resourceRef = payload.OccurrenceRef
		outcome.summary = "Schedule occurrence materialized"
		return outcome, nil
	}
	if !contains([]string{"MATERIALIZED", "CLAIMED"}, state) {
		return commandOutcome{}, errs.ErrConflict
	}
	outcomeState := "COMPLETED"
	if strings.ToUpper(payload.Outcome) != "SUCCEEDED" {
		outcomeState = "FAILED"
	}
	if _, err := tx.Exec(ctx, queryWorkersChangeoccurrence5, occurrenceID, outcomeState); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryWorkersChangeoccurrence6, scheduleID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	schedule := entity.Schedule{Ref: mustScheduleRef(ctx, tx, scheduleID), ProjectRef: projectRef}
	return commandOutcome{result: command.Result{Schedule: &schedule}, projectID: projectID, projectRef: projectRef, resourceKind: "SCHEDULE_OCCURRENCE", resourceRef: payload.OccurrenceRef, summary: "Schedule occurrence completed", platformEvent: "SCHEDULE_CHANGED"}, nil
}

func mustScheduleRef(ctx context.Context, tx pgx.Tx, id string) string {
	var ref string
	_ = tx.QueryRow(ctx, queryWorkersMustscheduleref1, id).Scan(&ref)
	return ref
}

func (repository *Repository) ResolveIntegrationInvocation(ctx context.Context, principal value.Principal, input map[string]string) (map[string]any, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var runID, nodeID, connectionID, grantID, projectID string
	var publicConfig []byte
	err = tx.QueryRow(ctx, queryWorkersResolveintegrationinvocation1, scope.organizationID, input["run_ref"], input["node_ref"], input["connection_ref"], input["capability_key"]).Scan(&runID, &nodeID, &connectionID, &grantID, &projectID, &publicConfig)
	if err != nil {
		return nil, errs.ErrForbidden
	}
	invocationRef, _ := newRef("inv")
	fence, _ := newRef("eff")
	fenceDigest := sha256.Sum256([]byte(fence))
	var bounded map[string]any
	_ = json.Unmarshal(publicConfig, &bounded)
	if _, err := tx.Exec(ctx, queryWorkersResolveintegrationinvocation2, invocationRef, scope.organizationID, runID, nodeID, connectionID, grantID, input["capability_key"], input["input_digest"], publicConfig, hex.EncodeToString(fenceDigest[:])); err != nil {
		return nil, mapWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return map[string]any{"invocationRef": invocationRef, "grantRef": grantID, "operation": input["capability_key"], "boundedInput": bounded, "effectFence": fence, "projectID": projectID}, nil
}

func (repository *Repository) completeIntegrationInvocation(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.IntegrationInvocationInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	var invocationID, runID, rootRunID, projectID, projectRef, nodeRef, storedDigest, state string
	err := tx.QueryRow(ctx, queryWorkersCompleteintegrationinvocation1, scope.organizationID, payload.InvocationRef).Scan(&invocationID, &runID, &rootRunID, &projectID, &projectRef, &nodeRef, &storedDigest, &state)
	if err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	digest := sha256.Sum256([]byte(payload.EffectFence))
	if storedDigest != hex.EncodeToString(digest[:]) || state != "READY" {
		return commandOutcome{}, errs.ErrForbidden
	}
	next := "SUCCEEDED"
	if !payload.Success {
		next = "FAILED"
	}
	if _, err := tx.Exec(ctx, queryWorkersCompleteintegrationinvocation2, invocationID, next, truncate(payload.ResultSummary, 2000), truncate(payload.SafeErrorCode, 100)); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	event, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, payload.InvocationRef, "NODE_STATE_CHANGED", nodeRef, "", "", "", "Внешнее действие завершено", "RUNNING", next)
	if err != nil {
		return commandOutcome{}, err
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, scope, mustRunRef(ctx, tx, runID))
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Run: &run, Graph: &graph, Event: &event}, projectID: projectID, projectRef: projectRef, resourceKind: "INTEGRATION_INVOCATION", resourceRef: payload.InvocationRef, summary: "Integration invocation completed"}, nil
}
