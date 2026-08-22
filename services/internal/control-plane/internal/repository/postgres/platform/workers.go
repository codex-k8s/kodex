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
	var promptRef, promptDigest, promptContent, ownerInstructions, systemSessionRef, warmInstance string
	err = tx.QueryRow(ctx, `SELECT a.ref,ar.stable_key,a.name,a.purpose,ar.core_prompt_revision,ar.owner_instructions,ar.runtime_state,ar.runtime_revision,ar.desired_runtime_revision,ar.system_session_ref,ar.resource_limits,ar.last_heartbeat_at,ar.version,ar.updated_at,i.ref,i.digest,i.content,COALESCE(ar.warm_instance_ref,'') FROM control_plane.assistant_runtime ar JOIN control_plane.agents a ON a.id=ar.agent_id JOIN control_plane.instruction_versions i ON i.ref=ar.core_prompt_ref WHERE ar.organization_id=$1::uuid FOR UPDATE`, scope.organizationID).Scan(&assistant.Ref, &assistant.StableKey, &assistant.Name, &assistant.Purpose, &assistant.CorePromptRevision, &ownerInstructions, &assistant.RuntimeState, &assistant.RuntimeRevision, &assistant.DesiredRuntimeRevision, &systemSessionRef, &limits, &assistant.LastHeartbeatAt, &assistant.Version, &assistant.UpdatedAt, &promptRef, &promptDigest, &promptContent, &warmInstance)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, errs.ErrUnavailable
	}
	_ = json.Unmarshal(limits, &assistant.ResourceLimits)
	assistant.OwnerInstructions = ownerInstructions
	assistant.WarmSessionRef = systemSessionRef
	assistant.System = true
	assistant.Deletable = false
	stale := assistant.LastHeartbeatAt == nil || time.Since(*assistant.LastHeartbeatAt) > 45*time.Second
	required := assistant.RuntimeState != "READY" || assistant.RuntimeRevision != assistant.DesiredRuntimeRevision || warmInstance != instance || stale
	if required {
		if _, err := tx.Exec(ctx, `UPDATE control_plane.assistant_runtime SET runtime_state='RECOVERING',warm_instance_ref=$2,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid`, scope.organizationID, instance); err != nil {
			return entity.SystemAssistant{}, nil, false, errs.ErrUnavailable
		}
		assistant.RuntimeState = "RECOVERING"
		assistant.Version++
	}
	snapshot := map[string]any{"assistantRef": assistant.Ref, "stableKey": assistant.StableKey, "systemSessionRef": systemSessionRef, "runtimeRevision": assistant.DesiredRuntimeRevision, "corePromptRef": promptRef, "corePromptDigest": promptDigest, "corePrompt": promptContent, "ownerInstructions": ownerInstructions, "resourceLimits": assistant.ResourceLimits, "directSecretAccess": false}
	if err := tx.Commit(ctx); err != nil {
		return entity.SystemAssistant{}, nil, false, errs.ErrConflict
	}
	return assistant, snapshot, required, nil
}

func (repository *Repository) reportWarmRuntime(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.WarmRuntimeInput)
	if !ok || payload.WorkloadInstance == "" || payload.RuntimeRevision == "" || !contains([]string{"STARTING", "READY", "RECOVERING", "UNAVAILABLE"}, payload.State) {
		return commandOutcome{}, errs.ErrInvalid
	}
	var assistant entity.SystemAssistant
	var limits []byte
	err := tx.QueryRow(ctx, `UPDATE control_plane.assistant_runtime SET runtime_state=$4,runtime_revision=$3,warm_instance_ref=$2,last_heartbeat_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND desired_runtime_revision=$3 AND (warm_instance_ref IS NULL OR warm_instance_ref=$2) RETURNING stable_key,core_prompt_revision,owner_instructions,runtime_state,runtime_revision,desired_runtime_revision,system_session_ref,resource_limits,last_heartbeat_at,version,updated_at`, scope.organizationID, payload.WorkloadInstance, payload.RuntimeRevision, payload.State).Scan(&assistant.StableKey, &assistant.CorePromptRevision, &assistant.OwnerInstructions, &assistant.RuntimeState, &assistant.RuntimeRevision, &assistant.DesiredRuntimeRevision, &assistant.WarmSessionRef, &limits, &assistant.LastHeartbeatAt, &assistant.Version, &assistant.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrConflict
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	_ = json.Unmarshal(limits, &assistant.ResourceLimits)
	assistant.System = true
	assistant.Ready = payload.State == "READY"
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
	rows, err := tx.Query(ctx, `SELECT s.id::text,s.ref,s.next_run_at,s.version FROM control_plane.schedules s WHERE s.organization_id=$1::uuid AND s.enabled AND s.next_run_at<=clock_timestamp() ORDER BY s.next_run_at FOR UPDATE SKIP LOCKED LIMIT $2`, scope.organizationID, limit)
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
		if _, err := tx.Exec(ctx, `INSERT INTO control_plane.schedule_occurrences(ref,organization_id,schedule_id,scheduled_for,state,lease_ref,fence_digest,generation,workload_instance,lease_expires_at) VALUES($1,$2::uuid,$3::uuid,$4,'CLAIMED',$5,$6,1,$7,$8)`, occurrenceRef, scope.organizationID, scheduleID, scheduledFor, leaseRef, hex.EncodeToString(digest[:]), instance, expires); err != nil {
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
	err := tx.QueryRow(ctx, `SELECT o.id::text,o.schedule_id::text,s.project_id::text,p.ref,o.state,o.fence_digest,o.generation,o.lease_expires_at,s.target_type,s.target_ref,s.name FROM control_plane.schedule_occurrences o JOIN control_plane.schedules s ON s.id=o.schedule_id JOIN control_plane.projects p ON p.id=s.project_id WHERE o.organization_id=$1::uuid AND o.ref=$2 AND o.lease_ref=$3 FOR UPDATE`, scope.organizationID, payload.OccurrenceRef, payload.LeaseRef).Scan(&occurrenceID, &scheduleID, &projectID, &projectRef, &state, &storedDigest, &generation, &expires, &targetType, &targetRef, &name)
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
		if err := tx.QueryRow(ctx, `SELECT input FROM control_plane.schedules WHERE id=$1::uuid`, scheduleID).Scan(&scheduleInput); err != nil {
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
		_ = tx.QueryRow(ctx, `SELECT id::text FROM control_plane.runs WHERE ref=$1`, outcome.result.Run.Ref).Scan(&runID)
		_, _ = tx.Exec(ctx, `UPDATE control_plane.schedule_occurrences SET state='MATERIALIZED',run_id=$2::uuid,version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid`, occurrenceID, runID)
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
	if _, err := tx.Exec(ctx, `UPDATE control_plane.schedule_occurrences SET state=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid`, occurrenceID, outcomeState); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, `UPDATE control_plane.schedules SET last_run_at=clock_timestamp(),next_run_at=clock_timestamp()+interval '1 day',version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid`, scheduleID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	schedule := entity.Schedule{Ref: mustScheduleRef(ctx, tx, scheduleID), ProjectRef: projectRef}
	return commandOutcome{result: command.Result{Schedule: &schedule}, projectID: projectID, projectRef: projectRef, resourceKind: "SCHEDULE_OCCURRENCE", resourceRef: payload.OccurrenceRef, summary: "Schedule occurrence completed", platformEvent: "SCHEDULE_CHANGED"}, nil
}

func mustScheduleRef(ctx context.Context, tx pgx.Tx, id string) string {
	var ref string
	_ = tx.QueryRow(ctx, `SELECT ref FROM control_plane.schedules WHERE id=$1::uuid`, id).Scan(&ref)
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
	err = tx.QueryRow(ctx, `SELECT r.id::text,n.id::text,c.id::text,g.id::text,r.project_id::text,c.public_configuration FROM control_plane.runs r JOIN control_plane.run_nodes n ON n.run_id=r.id JOIN control_plane.integration_connections c ON c.organization_id=r.organization_id AND c.ref=$4 AND c.enabled AND c.state='CONNECTED' JOIN control_plane.integration_grants g ON g.connection_id=c.id AND g.capability_key=$5 AND g.target_kind='AGENT' AND g.target_ref=(SELECT a.ref FROM control_plane.agents a WHERE a.id=n.agent_id) AND g.enabled WHERE r.organization_id=$1::uuid AND r.ref=$2 AND n.ref=$3 FOR UPDATE OF n,c,g`, scope.organizationID, input["run_ref"], input["node_ref"], input["connection_ref"], input["capability_key"]).Scan(&runID, &nodeID, &connectionID, &grantID, &projectID, &publicConfig)
	if err != nil {
		return nil, errs.ErrForbidden
	}
	invocationRef, _ := newRef("inv")
	fence, _ := newRef("eff")
	fenceDigest := sha256.Sum256([]byte(fence))
	var bounded map[string]any
	_ = json.Unmarshal(publicConfig, &bounded)
	if _, err := tx.Exec(ctx, `INSERT INTO control_plane.integration_invocations(ref,organization_id,run_id,node_id,connection_id,grant_id,capability_key,operation,input_digest,bounded_input,effect_fence_digest,state) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7,$7,$8,$9,$10,'READY')`, invocationRef, scope.organizationID, runID, nodeID, connectionID, grantID, input["capability_key"], input["input_digest"], publicConfig, hex.EncodeToString(fenceDigest[:])); err != nil {
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
	err := tx.QueryRow(ctx, `SELECT i.id::text,i.run_id::text,r.root_run_id::text,r.project_id::text,p.ref,n.ref,i.effect_fence_digest,i.state FROM control_plane.integration_invocations i JOIN control_plane.runs r ON r.id=i.run_id JOIN control_plane.projects p ON p.id=r.project_id JOIN control_plane.run_nodes n ON n.id=i.node_id WHERE i.organization_id=$1::uuid AND i.ref=$2 FOR UPDATE`, scope.organizationID, payload.InvocationRef).Scan(&invocationID, &runID, &rootRunID, &projectID, &projectRef, &nodeRef, &storedDigest, &state)
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
	if _, err := tx.Exec(ctx, `UPDATE control_plane.integration_invocations SET state=$2,result_summary=$3,safe_error_code=$4,version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid`, invocationID, next, truncate(payload.ResultSummary, 2000), truncate(payload.SafeErrorCode, 100)); err != nil {
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
