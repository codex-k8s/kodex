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
	var promptRef, promptDigest, promptContent, ownerInstructions, systemSessionRef string
	var warmInstance, runtimeKey, profileRevision, provider, model, roleDefinitionRef string
	err = tx.QueryRow(ctx, queryWorkersReconcilewarmruntimeSelectAssistantRuntimeOrganizationId, scope.organizationID).Scan(&assistant.Ref, &assistant.StableKey, &assistant.Name, &assistant.Purpose, &assistant.CorePromptRevision, &ownerInstructions, &assistant.RuntimeState, &assistant.RuntimeRevision, &assistant.DesiredRuntimeRevision, &systemSessionRef, &limits, &assistant.LastHeartbeatAt, &assistant.Version, &assistant.UpdatedAt, &promptRef, &promptDigest, &promptContent, &warmInstance, &runtimeKey, &profileRevision, &provider, &model, &roleDefinitionRef)
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
		if _, err := tx.Exec(ctx, queryWorkersReconcilewarmruntimeUpdateAssistantRuntimeRuntimeStateWarmInstanceRefVersion, scope.organizationID, instance); err != nil {
			return entity.SystemAssistant{}, nil, false, errs.ErrUnavailable
		}
		assistant.RuntimeState = "RECOVERING"
		assistant.Version++
	}
	resolvedInstructions := promptContent
	if ownerInstructions != "" {
		resolvedInstructions += "\n\n<owner-instructions>\n" + ownerInstructions + "\n</owner-instructions>"
	}
	revisionDigest := sha256.Sum256([]byte(strings.Join([]string{
		assistant.DesiredRuntimeRevision, profileRevision, provider, model, promptDigest,
		ownerInstructions, roleDefinitionRef, repository.roleImages.DefaultImageReference,
		repository.roleImages.DefaultImageDigest, repository.roleImages.RoleRuntimeContractSHA256,
	}, "\x00")))
	snapshot := map[string]any{
		"assistantRef": assistant.Ref, "agentRef": assistant.Ref,
		"stableKey": assistant.StableKey, "sessionRef": systemSessionRef,
		"systemSessionRef": systemSessionRef, "runtimeRevisionRef": assistant.DesiredRuntimeRevision,
		"runtimeRevisionVersion": assistant.Version, "runtimeRevision": profileRevision,
		"runtimeKey": runtimeKey, "profileRevision": profileRevision,
		"runtimeProvider": provider, "runtimeModel": model, "corePromptRef": promptRef,
		"corePromptDigest": promptDigest, "corePrompt": promptContent,
		"ownerInstructions": ownerInstructions, "instructions": resolvedInstructions,
		"resourceLimits": assistant.ResourceLimits, "directSecretAccess": false,
		"roleDefinitionRef":           roleDefinitionRef,
		"imageReference":              repository.roleImages.DefaultImageReference,
		"imageManifestDigest":         repository.roleImages.DefaultImageDigest,
		"roleRuntimeContractRevision": repository.roleImages.RoleRuntimeContractRevision,
		"roleRuntimeContractSHA256":   repository.roleImages.RoleRuntimeContractSHA256,
		"revisionDigest":              hex.EncodeToString(revisionDigest[:]),
	}
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
	err := tx.QueryRow(ctx, queryWorkersReportwarmruntimeUpdateAssistantRuntimeRuntimeStateRuntimeRevisionWarmInstanceRef, scope.organizationID, payload.WorkloadInstance, payload.RuntimeRevision, payload.State).Scan(&assistant.StableKey, &assistant.CorePromptRevision, &assistant.OwnerInstructions, &assistant.RuntimeState, &assistant.RuntimeRevision, &assistant.DesiredRuntimeRevision, &assistant.WarmSessionRef, &limits, &assistant.LastHeartbeatAt, &assistant.Version, &assistant.UpdatedAt)
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
	rows, err := tx.Query(ctx, queryWorkersClaimdueschedulesSelectSchedulesOrganizationId, scope.organizationID, limit)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	type dueSchedule struct {
		id, ref      string
		scheduledFor time.Time
		version      int64
	}
	due := make([]dueSchedule, 0, limit)
	for rows.Next() {
		var item dueSchedule
		if err := rows.Scan(&item.id, &item.ref, &item.scheduledFor, &item.version); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		due = append(due, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, errs.ErrUnavailable
	}
	result := make([]map[string]any, 0, len(due))
	for _, item := range due {
		occurrenceRef, _ := newRef("occ")
		leaseRef, _ := newRef("lea")
		fence, _ := newRef("fnc")
		digest := sha256.Sum256([]byte(fence))
		expires := time.Now().UTC().Add(30 * time.Second)
		if _, err := tx.Exec(ctx, queryWorkersClaimdueschedulesInsertScheduleOccurrencesRefScheduleIdState, occurrenceRef, scope.organizationID, item.id, item.scheduledFor, leaseRef, hex.EncodeToString(digest[:]), instance, expires); err != nil {
			return nil, mapWriteError(err)
		}
		result = append(result, map[string]any{"scheduleRef": item.ref, "occurrenceRef": occurrenceRef, "scheduledFor": item.scheduledFor, "leaseRef": leaseRef, "fence": fence, "generation": int64(1), "expiresAt": expires, "scheduleVersion": item.version})
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
	err := tx.QueryRow(ctx, queryWorkersChangeoccurrenceSelectScheduleOccurrencesOrganizationIdRefLeaseRef, scope.organizationID, payload.OccurrenceRef, payload.LeaseRef).Scan(&occurrenceID, &scheduleID, &projectID, &projectRef, &state, &storedDigest, &generation, &expires, &targetType, &targetRef, &name)
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
		if err := tx.QueryRow(ctx, queryWorkersChangeoccurrenceSelectSchedulesId, scheduleID).Scan(&scheduleInput); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		var data map[string]any
		_ = json.Unmarshal(scheduleInput, &data)
		nested := input
		nested.Kind = command.LaunchRun
		nested.Payload = command.LaunchRunInput{ProjectRef: projectRef, Title: name, Task: "i18n:SCHEDULED_RUN_TASK", Source: "SCHEDULE", Target: entity.RunTarget{Type: targetType, Ref: targetRef}, Input: data}
		outcome, err := repository.launchRun(ctx, tx, scope, nested)
		if err != nil {
			return commandOutcome{}, err
		}
		var runID string
		_ = tx.QueryRow(ctx, queryWorkersChangeoccurrenceSelectRunsRef, outcome.result.Run.Ref).Scan(&runID)
		_, _ = tx.Exec(ctx, queryWorkersChangeoccurrenceUpdateScheduleOccurrencesStateRunIdVersion, occurrenceID, runID)
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
	if _, err := tx.Exec(ctx, queryWorkersChangeoccurrenceUpdateScheduleOccurrencesStateVersionUpdatedAt, occurrenceID, outcomeState); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryWorkersChangeoccurrenceUpdateSchedulesLastRunAtNextRunAtVersion, scheduleID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	schedule := entity.Schedule{Ref: mustScheduleRef(ctx, tx, scheduleID), ProjectRef: projectRef}
	return commandOutcome{result: command.Result{Schedule: &schedule}, projectID: projectID, projectRef: projectRef, resourceKind: "SCHEDULE_OCCURRENCE", resourceRef: payload.OccurrenceRef, summary: "Schedule occurrence completed", platformEvent: "SCHEDULE_CHANGED"}, nil
}

func mustScheduleRef(ctx context.Context, tx pgx.Tx, id string) string {
	var ref string
	_ = tx.QueryRow(ctx, queryWorkersMustschedulerefSelectSchedulesId, id).Scan(&ref)
	return ref
}

func (repository *Repository) ClaimIntegrationConnectionTests(ctx context.Context, principal value.Principal, instance string, limit int32) ([]map[string]any, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, queryWorkersClaimintegrationtestsExpireStaleTestLeases, scope.organizationID); err != nil {
		return nil, errs.ErrUnavailable
	}
	rows, err := tx.Query(ctx, queryWorkersClaimintegrationtestsSelectIntegrationConnectionTestsOrganizationIdState, scope.organizationID, limit)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	type candidate struct {
		id, ref, connectionRef, definitionKey, credentialRef string
		generation                                           int64
		configuration                                        []byte
	}
	candidates := make([]candidate, 0, limit)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.id, &item.ref, &item.generation, &item.connectionRef, &item.definitionKey, &item.credentialRef, &item.configuration); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		candidates = append(candidates, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, errs.ErrUnavailable
	}
	result := make([]map[string]any, 0, len(candidates))
	for _, item := range candidates {
		leaseRef, _ := newRef("lea")
		fence, _ := newRef("fnc")
		digest := sha256.Sum256([]byte(fence))
		generation := item.generation + 1
		expiresAt := time.Now().UTC().Add(30 * time.Second)
		tag, err := tx.Exec(ctx, queryWorkersClaimintegrationtestsClaimTestLease, item.id, leaseRef, hex.EncodeToString(digest[:]), generation, instance, expiresAt)
		if err != nil || tag.RowsAffected() != 1 {
			return nil, errs.ErrConflict
		}
		configuration := map[string]any{}
		_ = json.Unmarshal(item.configuration, &configuration)
		result = append(result, map[string]any{"testRef": item.ref, "connectionRef": item.connectionRef, "definitionKey": item.definitionKey, "credentialRef": item.credentialRef, "configuration": configuration, "leaseRef": leaseRef, "fence": fence, "generation": generation, "expiresAt": expiresAt})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return result, nil
}

func (repository *Repository) completeIntegrationConnectionTest(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.IntegrationConnectionTestInput)
	if !ok || payload.Success && payload.SafeErrorCode != "" || !payload.Success && !safeIntegrationErrorCode(payload.SafeErrorCode) {
		return commandOutcome{}, errs.ErrInvalid
	}
	var testID, connectionID, connectionRef, storedDigest, state, leaseRef string
	var generation int64
	var expiresAt time.Time
	if err := tx.QueryRow(ctx, queryWorkersCompleteintegrationtestSelectIntegrationConnectionTestsOrganizationIdRef, scope.organizationID, payload.TestRef).Scan(&testID, &connectionID, &connectionRef, &storedDigest, &generation, &state, &leaseRef, &expiresAt); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	digest := sha256.Sum256([]byte(payload.Fence))
	if state != "CLAIMED" || leaseRef != payload.LeaseRef || generation != payload.Generation || storedDigest != hex.EncodeToString(digest[:]) || time.Now().After(expiresAt) {
		return commandOutcome{}, errs.ErrForbidden
	}
	nextTest, nextConnection, credentials := "SUCCEEDED", "CONNECTED", "CONFIGURED"
	if !payload.Success {
		nextTest, nextConnection = "FAILED", "DEGRADED"
		if payload.SafeErrorCode == "INTEGRATION_AUTH_REJECTED" || payload.SafeErrorCode == "INTEGRATION_CREDENTIAL_UNAVAILABLE" {
			credentials = "INVALID"
		}
	}
	if _, err := tx.Exec(ctx, queryWorkersCompleteintegrationtestUpdateIntegrationConnectionTestsStateResultSummarySafeErrorCode, testID, nextTest, truncate(payload.ResultSummary, 1000), payload.SafeErrorCode); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var item entity.IntegrationConnection
	if err := tx.QueryRow(ctx, queryWorkersCompleteintegrationtestUpdateIntegrationConnectionsStateMaskedCredentialsStateLastTestSummary, connectionID, nextConnection, credentials, truncate(payload.ResultSummary, 1000)).Scan(&item.Ref, &item.DefinitionKey, &item.Name, &item.State, &item.MaskedCredentialsState, &item.LastTestSummary, &item.Enabled, &item.Version, &item.LastTestedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return commandOutcome{}, errs.ErrConflict
	}
	item.NextActions = []string{"OPEN", "TEST"}
	return commandOutcome{result: command.Result{Connection: &item}, resourceKind: "INTEGRATION_CONNECTION", resourceRef: connectionRef, summary: "Integration connection test completed", platformEvent: "INTEGRATION_CONNECTION_CHANGED"}, nil
}

func (repository *Repository) ResolveIntegrationInvocation(ctx context.Context, principal value.Principal, input map[string]string, boundedInput map[string]any) (map[string]any, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	encodedInput, err := json.Marshal(boundedInput)
	if err != nil || len(encodedInput) > 64<<10 {
		return nil, errs.ErrInvalid
	}
	var runID, nodeID, connectionID, grantID, projectID string
	err = tx.QueryRow(ctx, queryWorkersResolveintegrationinvocationSelectRunsIdOrganizationIdRef, scope.organizationID, input["run_ref"], input["node_ref"], input["connection_ref"], input["capability_key"]).Scan(&runID, &nodeID, &connectionID, &grantID, &projectID)
	if err != nil {
		return nil, errs.ErrForbidden
	}
	invocationRef, _ := newRef("inv")
	inputDigest := sha256.Sum256(encodedInput)
	intentDigest := sha256.Sum256([]byte(strings.Join([]string{input["connection_ref"], input["capability_key"], hex.EncodeToString(inputDigest[:])}, "\x00")))
	var resolvedRef string
	if err := tx.QueryRow(ctx, queryWorkersResolveintegrationinvocationInsertIntegrationInvocationsRefRunIdConnectionId, invocationRef, scope.organizationID, runID, nodeID, connectionID, grantID, input["capability_key"], input["idempotency_key"], hex.EncodeToString(intentDigest[:]), hex.EncodeToString(inputDigest[:]), encodedInput).Scan(&resolvedRef); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errs.ErrIdempotencyReuse
		}
		return nil, mapWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return map[string]any{"invocationRef": resolvedRef, "grantRef": grantID, "operation": input["capability_key"], "projectID": projectID}, nil
}

func (repository *Repository) ClaimIntegrationInvocations(ctx context.Context, principal value.Principal, instance string, limit int32) ([]map[string]any, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, queryWorkersClaimintegrationinvocationsExpireStaleInvocationLeases, scope.organizationID); err != nil {
		return nil, errs.ErrUnavailable
	}
	rows, err := tx.Query(ctx, queryWorkersClaimintegrationinvocationsSelectIntegrationInvocationsOrganizationIdState, scope.organizationID, limit)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	type candidate struct {
		id, ref, connectionRef, definitionKey, credentialRef, capabilityKey string
		generation                                                          int64
		configuration, boundedInput                                         []byte
	}
	candidates := make([]candidate, 0, limit)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.id, &item.ref, &item.generation, &item.connectionRef, &item.definitionKey, &item.credentialRef, &item.configuration, &item.capabilityKey, &item.boundedInput); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		candidates = append(candidates, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, errs.ErrUnavailable
	}
	result := make([]map[string]any, 0, len(candidates))
	for _, item := range candidates {
		leaseRef, _ := newRef("lea")
		fence, _ := newRef("eff")
		digest := sha256.Sum256([]byte(fence))
		generation := item.generation + 1
		expiresAt := time.Now().UTC().Add(30 * time.Second)
		tag, err := tx.Exec(ctx, queryWorkersClaimintegrationinvocationsClaimInvocationLease, item.id, leaseRef, hex.EncodeToString(digest[:]), generation, instance, expiresAt)
		if err != nil || tag.RowsAffected() != 1 {
			return nil, errs.ErrConflict
		}
		configuration, bounded := map[string]any{}, map[string]any{}
		_ = json.Unmarshal(item.configuration, &configuration)
		_ = json.Unmarshal(item.boundedInput, &bounded)
		result = append(result, map[string]any{"invocationRef": item.ref, "connectionRef": item.connectionRef, "definitionKey": item.definitionKey, "credentialRef": item.credentialRef, "capabilityKey": item.capabilityKey, "configuration": configuration, "boundedInput": bounded, "leaseRef": leaseRef, "fence": fence, "generation": generation, "expiresAt": expiresAt})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return result, nil
}

func (repository *Repository) GetIntegrationInvocation(ctx context.Context, principal value.Principal, ref string) (map[string]any, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	var state, resultSummary, safeErrorCode string
	if err := repository.pool.QueryRow(ctx, queryWorkersGetintegrationinvocationSelectIntegrationInvocationsOrganizationIdRef, scope.organizationID, ref).Scan(&state, &resultSummary, &safeErrorCode); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errs.ErrNotFound
		}
		return nil, errs.ErrUnavailable
	}
	return map[string]any{"state": state, "resultSummary": resultSummary, "safeErrorCode": safeErrorCode}, nil
}

func (repository *Repository) completeIntegrationInvocation(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.IntegrationInvocationInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	if payload.Success && payload.SafeErrorCode != "" || !payload.Success && !safeIntegrationErrorCode(payload.SafeErrorCode) {
		return commandOutcome{}, errs.ErrInvalid
	}
	var invocationID, runID, rootRunID, projectID, projectRef, nodeRef, storedDigest, state, leaseRef string
	var generation int64
	var expiresAt time.Time
	err := tx.QueryRow(ctx, queryWorkersCompleteintegrationinvocationSelectIntegrationInvocationsOrganizationIdRef, scope.organizationID, payload.InvocationRef).Scan(&invocationID, &runID, &rootRunID, &projectID, &projectRef, &nodeRef, &storedDigest, &generation, &state, &leaseRef, &expiresAt)
	if err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	digest := sha256.Sum256([]byte(payload.Fence))
	if storedDigest != hex.EncodeToString(digest[:]) || state != "RUNNING" || leaseRef != payload.LeaseRef || generation != payload.Generation || time.Now().After(expiresAt) {
		return commandOutcome{}, errs.ErrForbidden
	}
	next := "SUCCEEDED"
	if !payload.Success {
		next = "FAILED"
	}
	if _, err := tx.Exec(ctx, queryWorkersCompleteintegrationinvocationUpdateIntegrationInvocationsStateResultSummarySafeErrorCode, invocationID, next, truncate(payload.ResultSummary, 2000), truncate(payload.SafeErrorCode, 100)); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	event, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, payload.InvocationRef, "TURN_PROGRESS", nodeRef, "", "", "", "INTEGRATION_ACTION_COMPLETED", "RUNNING", "RUNNING")
	if err != nil {
		return commandOutcome{}, err
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, scope, mustRunRef(ctx, tx, runID))
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Run: &run, Graph: &graph, Event: &event}, projectID: projectID, projectRef: projectRef, resourceKind: "INTEGRATION_INVOCATION", resourceRef: payload.InvocationRef, summary: "Integration invocation completed"}, nil
}

func safeIntegrationErrorCode(code string) bool {
	switch code {
	case "INTEGRATION_AUTH_REJECTED", "INTEGRATION_CREDENTIAL_UNAVAILABLE", "INTEGRATION_UNAVAILABLE", "INTEGRATION_RATE_LIMITED", "INTEGRATION_CONFIGURATION_INVALID", "INTEGRATION_CAPABILITY_UNSUPPORTED", "INTEGRATION_REQUEST_REJECTED", "INTEGRATION_RESPONSE_INVALID":
		return true
	default:
		return false
	}
}
