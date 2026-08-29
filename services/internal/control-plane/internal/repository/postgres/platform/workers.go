package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	scheduleservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/schedule"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
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
	var providerAccountRef, providerCredentialRef, providerSecretName string
	var providerSecretUID, providerSecretResourceVersion, providerCredentialSHA256 string
	var runtimeConfigRef, runtimeConfigDigest, providerPolicyRef, providerPolicyDigest string
	var configOverlayRef, configOverlayDigest, configOverlay string
	var runtimeEnvironmentRef, runtimeEnvironmentDigest string
	var environmentBindingRef, environmentBindingDigest string
	var rawEnvironmentValues, rawSecretProjections []byte
	var providerCredentialRevisionNumber, runtimeConfigVersion, providerPolicyVersion int64
	var configOverlayVersion, runtimeEnvironmentVersion, environmentBindingVersion int64
	err = tx.QueryRow(ctx, queryWorkersReconcilewarmruntimeSelectAssistantRuntimeOrganizationId, scope.organizationID).Scan(
		&assistant.Ref, &assistant.StableKey, &assistant.Name, &assistant.Purpose,
		&assistant.CorePromptRevision, &ownerInstructions, &assistant.RuntimeState,
		&assistant.RuntimeRevision, &assistant.DesiredRuntimeRevision, &systemSessionRef,
		&limits, &assistant.LastHeartbeatAt, &assistant.Version, &assistant.UpdatedAt,
		&promptRef, &promptDigest, &promptContent, &warmInstance, &runtimeKey,
		&profileRevision, &provider, &model, &roleDefinitionRef, &providerAccountRef,
		&providerCredentialRef, &providerCredentialRevisionNumber, &providerSecretName,
		&providerSecretUID, &providerSecretResourceVersion, &providerCredentialSHA256,
		&runtimeConfigRef, &runtimeConfigVersion, &runtimeConfigDigest,
		&providerPolicyRef, &providerPolicyVersion, &providerPolicyDigest,
		&configOverlayRef, &configOverlayVersion, &configOverlayDigest, &configOverlay,
		&runtimeEnvironmentRef, &runtimeEnvironmentVersion, &runtimeEnvironmentDigest,
		&environmentBindingRef, &environmentBindingVersion, &environmentBindingDigest,
		&rawEnvironmentValues, &rawSecretProjections,
	)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, errs.ErrUnavailable
	}
	canonicalOverlay, verifiedOverlayDigest, err := runtimecontract.CanonicalConfigOverlay(configOverlay)
	if err != nil || canonicalOverlay != configOverlay || verifiedOverlayDigest != configOverlayDigest {
		return entity.SystemAssistant{}, nil, false, errs.ErrConflict
	}
	var environmentValues []runtimecontract.RuntimeEnvironmentValue
	var secretProjections []runtimecontract.RuntimeSecretProjection
	if err := decodeStoredRuntimeEnvironment(rawEnvironmentValues, rawSecretProjections, &environmentValues, &secretProjections); err != nil {
		return entity.SystemAssistant{}, nil, false, errs.ErrConflict
	}
	environmentImage := runtimecontract.RuntimeEnvironmentImage{
		Reference: repository.roleImages.DefaultImageReference,
		Digest:    repository.roleImages.DefaultImageDigest,
	}
	verifiedEnvironmentDigest, err := runtimecontract.RuntimeEnvironmentDigest(
		environmentValues, secretProjections, environmentImage, nil)
	if err != nil || verifiedEnvironmentDigest != runtimeEnvironmentDigest {
		return entity.SystemAssistant{}, nil, false, errs.ErrConflict
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
		providerAccountRef, providerCredentialRef, providerSecretName, providerSecretUID,
		providerSecretResourceVersion, providerCredentialSHA256,
		ownerInstructions, roleDefinitionRef, repository.roleImages.DefaultImageReference,
		repository.roleImages.DefaultImageDigest, repository.roleImages.RoleRuntimeContractSHA256,
		runtimeConfigRef, runtimeConfigDigest, providerPolicyRef, providerPolicyDigest,
		configOverlayRef, configOverlayDigest, runtimeEnvironmentRef, runtimeEnvironmentDigest,
		environmentBindingRef, environmentBindingDigest,
	}, "\x00")))
	snapshot := map[string]any{
		"assistantRef": assistant.Ref, "agentRef": assistant.Ref,
		"stableKey": assistant.StableKey, "sessionRef": systemSessionRef,
		"systemSessionRef": systemSessionRef, "runtimeRevisionRef": assistant.DesiredRuntimeRevision,
		"runtimeRevisionVersion": assistant.Version, "runtimeRevision": profileRevision,
		"runtimeKey": runtimeKey, "profileRevision": profileRevision,
		"runtimeProvider": provider, "runtimeModel": model, "corePromptRef": promptRef,
		"providerAccountRef":               providerAccountRef,
		"providerCredentialRevisionRef":    providerCredentialRef,
		"providerCredentialRevisionNumber": providerCredentialRevisionNumber,
		"providerSecretName":               providerSecretName,
		"providerSecretUID":                providerSecretUID,
		"providerSecretResourceVersion":    providerSecretResourceVersion,
		"providerCredentialSHA256":         providerCredentialSHA256,
		"corePromptDigest":                 promptDigest, "corePrompt": promptContent,
		"ownerInstructions": ownerInstructions, "instructions": resolvedInstructions,
		"resourceLimits": assistant.ResourceLimits, "directSecretAccess": false,
		"roleDefinitionRef":           roleDefinitionRef,
		"imageReference":              repository.roleImages.DefaultImageReference,
		"imageManifestDigest":         repository.roleImages.DefaultImageDigest,
		"roleRuntimeContractRevision": repository.roleImages.RoleRuntimeContractRevision,
		"roleRuntimeContractSHA256":   repository.roleImages.RoleRuntimeContractSHA256,
		"runtimeConfigRef":            runtimeConfigRef,
		"runtimeConfigVersion":        runtimeConfigVersion,
		"runtimeConfigDigest":         runtimeConfigDigest,
		"providerPolicyRef":           providerPolicyRef,
		"providerPolicyVersion":       providerPolicyVersion,
		"providerPolicyDigest":        providerPolicyDigest,
		"configOverlayRef":            configOverlayRef,
		"configOverlayVersion":        configOverlayVersion,
		"configOverlayDigest":         configOverlayDigest,
		"configOverlay":               configOverlay,
		"runtimeEnvironmentRef":       runtimeEnvironmentRef,
		"runtimeEnvironmentVersion":   runtimeEnvironmentVersion,
		"runtimeEnvironmentDigest":    runtimeEnvironmentDigest,
		"environmentBindingRef":       environmentBindingRef,
		"environmentBindingVersion":   environmentBindingVersion,
		"environmentBindingDigest":    environmentBindingDigest,
		"environmentValues":           environmentValues,
		"secretProjections":           secretProjections,
		"environmentImage":            environmentImage,
		"environmentTools":            []runtimecontract.RuntimeEnvironmentTool{},
		"revisionDigest":              hex.EncodeToString(revisionDigest[:]),
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.SystemAssistant{}, nil, false, errs.ErrConflict
	}
	return assistant, snapshot, required, nil
}

func (repository *Repository) ReportWarmRuntime(ctx context.Context, principal value.Principal, payload command.WarmRuntimeInput) (entity.SystemAssistant, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.SystemAssistant{}, err
	}
	if principal.CallerWorkload != "runtime-controller" || principal.Permission != "platform.runtime.warm.report" ||
		payload.WorkloadInstance == "" || payload.RuntimeRevision == "" ||
		!contains([]string{"STARTING", "READY", "BUSY", "RECOVERING", "UNAVAILABLE"}, payload.State) {
		return entity.SystemAssistant{}, errs.ErrForbidden
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return entity.SystemAssistant{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var assistant entity.SystemAssistant
	var limits []byte
	var changed bool
	err = tx.QueryRow(ctx, queryWorkersReportwarmruntimeUpdateAssistantRuntimeRuntimeStateRuntimeRevisionWarmInstanceRef, pgx.StrictNamedArgs{
		"organization_id":   scope.organizationID,
		"workload_instance": payload.WorkloadInstance,
		"runtime_revision":  payload.RuntimeRevision,
		"runtime_state":     payload.State,
	}).Scan(&assistant.StableKey, &assistant.CorePromptRevision, &assistant.OwnerInstructions, &assistant.RuntimeState, &assistant.RuntimeRevision, &assistant.DesiredRuntimeRevision, &assistant.WarmSessionRef, &limits, &assistant.LastHeartbeatAt, &assistant.Version, &assistant.UpdatedAt, &changed)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.SystemAssistant{}, errs.ErrConflict
	}
	if err != nil {
		return entity.SystemAssistant{}, errs.ErrUnavailable
	}
	_ = json.Unmarshal(limits, &assistant.ResourceLimits)
	assistant.System = true
	assistant.Ready = contains([]string{"READY", "BUSY"}, payload.State)
	if changed {
		auditRef, refErr := newRef("aud")
		if refErr != nil {
			return entity.SystemAssistant{}, refErr
		}
		if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction,
			auditRef,
			scope.organizationID,
			nil,
			scope.actorID,
			"controlplane.report_warm_runtime",
			"SYSTEM_ASSISTANT",
			assistant.StableKey,
			"i18n:SYSTEM_ASSISTANT_HEARTBEAT_RECORDED",
			principal.CorrelationRef,
		); err != nil {
			return entity.SystemAssistant{}, errs.ErrUnavailable
		}
		if err := repository.emitPlatformEvent(ctx, tx, scope, "SYSTEM_ASSISTANT_CHANGED", "", assistant.StableKey, "i18n:SYSTEM_ASSISTANT_HEARTBEAT_RECORDED"); err != nil {
			return entity.SystemAssistant{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.SystemAssistant{}, errs.ErrConflict
	}
	return assistant, nil
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
	result := make([]map[string]any, 0, limit)
	expiredRows, err := tx.Query(ctx, queryWorkersClaimdueschedulesSelectExpiredOccurrences, scope.organizationID, limit)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	type expiredOccurrence struct {
		id, ref, scheduleRef, inputDigest string
		scheduledFor                      time.Time
		scheduleVersion, generation       int64
	}
	expired := make([]expiredOccurrence, 0, limit)
	for expiredRows.Next() {
		var item expiredOccurrence
		if err := expiredRows.Scan(&item.id, &item.ref, &item.scheduleRef, &item.scheduledFor, &item.scheduleVersion, &item.inputDigest, &item.generation); err != nil {
			expiredRows.Close()
			return nil, errs.ErrUnavailable
		}
		expired = append(expired, item)
	}
	expiredRows.Close()
	if err := expiredRows.Err(); err != nil {
		return nil, errs.ErrUnavailable
	}
	for _, item := range expired {
		leaseRef, _ := newRef("lea")
		fence, _ := newRef("fnc")
		digest := sha256.Sum256([]byte(fence))
		expires := time.Now().UTC().Add(30 * time.Second)
		var generation int64
		if err := tx.QueryRow(ctx, queryWorkersClaimdueschedulesReclaimExpiredOccurrence, item.id, leaseRef, hex.EncodeToString(digest[:]), instance, expires, item.generation).Scan(&generation); errors.Is(err, pgx.ErrNoRows) {
			return nil, errs.ErrConflict
		} else if err != nil {
			return nil, errs.ErrUnavailable
		}
		result = append(result, map[string]any{"scheduleRef": item.scheduleRef, "occurrenceRef": item.ref, "scheduledFor": item.scheduledFor, "leaseRef": leaseRef, "fence": fence, "generation": generation, "expiresAt": expires, "scheduleVersion": item.scheduleVersion, "inputDigest": item.inputDigest})
	}
	remaining := int(limit) - len(result)
	if remaining == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, errs.ErrConflict
		}
		return result, nil
	}
	rows, err := tx.Query(ctx, queryWorkersClaimdueschedulesSelectSchedulesOrganizationId, scope.organizationID, remaining)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	type dueSchedule struct {
		id, ref, preset, cron, timezone, name, targetType, targetRef string
		input                                                        []byte
		scheduledFor                                                 time.Time
		version                                                      int64
	}
	due := make([]dueSchedule, 0, limit)
	for rows.Next() {
		var item dueSchedule
		if err := rows.Scan(&item.id, &item.ref, &item.scheduledFor, &item.version, &item.preset, &item.cron, &item.timezone, &item.name, &item.targetType, &item.targetRef, &item.input); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		due = append(due, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, errs.ErrUnavailable
	}
	now := time.Now().UTC()
	for _, item := range due {
		after := item.scheduledFor
		if now.After(after) {
			after = now
		}
		next, nextErr := scheduleservice.Next(item.preset, item.cron, item.timezone, after)
		if nextErr != nil {
			return nil, errs.ErrUnavailable
		}
		tag, updateErr := tx.Exec(ctx, queryWorkersClaimdueschedulesUpdateSchedulesNextRunAt, item.id, next, item.scheduledFor)
		if updateErr != nil || tag.RowsAffected() != 1 {
			return nil, errs.ErrConflict
		}
		occurrenceRef, _ := newRef("occ")
		leaseRef, _ := newRef("lea")
		fence, _ := newRef("fnc")
		digest := sha256.Sum256([]byte(fence))
		inputDigest := sha256.Sum256(item.input)
		expires := time.Now().UTC().Add(30 * time.Second)
		if _, err := tx.Exec(ctx, queryWorkersClaimdueschedulesInsertScheduleOccurrencesRefScheduleIdState, occurrenceRef, scope.organizationID, item.id, item.scheduledFor, item.version, item.targetType, item.targetRef, item.name, item.input, hex.EncodeToString(inputDigest[:]), leaseRef, hex.EncodeToString(digest[:]), instance, expires); err != nil {
			return nil, mapWriteError(err)
		}
		result = append(result, map[string]any{"scheduleRef": item.ref, "occurrenceRef": occurrenceRef, "scheduledFor": item.scheduledFor, "leaseRef": leaseRef, "fence": fence, "generation": int64(1), "expiresAt": expires, "scheduleVersion": item.version, "inputDigest": hex.EncodeToString(inputDigest[:])})
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
	var occurrenceID, scheduleID, projectID, projectRef, state, storedDigest, targetType, targetRef, name, storedInputDigest string
	var scheduleVersion, generation int64
	var occurrenceInput []byte
	var expires time.Time
	err := tx.QueryRow(ctx, queryWorkersChangeoccurrenceSelectScheduleOccurrencesOrganizationIdRefLeaseRef, scope.organizationID, payload.OccurrenceRef, payload.LeaseRef).Scan(&occurrenceID, &scheduleID, &projectID, &projectRef, &state, &storedDigest, &generation, &expires, &targetType, &targetRef, &name, &occurrenceInput, &scheduleVersion, &storedInputDigest)
	if err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	digest := sha256.Sum256([]byte(payload.Fence))
	if storedDigest != hex.EncodeToString(digest[:]) || generation != payload.Generation || time.Now().After(expires) {
		return commandOutcome{}, errs.ErrForbidden
	}
	inputDigest := sha256.Sum256(occurrenceInput)
	if storedInputDigest != hex.EncodeToString(inputDigest[:]) || scheduleVersion < 1 {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var schedule entity.Schedule
	var scheduleInput []byte
	if err := tx.QueryRow(ctx, queryWorkersChangeoccurrenceSelectSchedulesId, scheduleID).Scan(&schedule.Ref, &schedule.ProjectRef, &schedule.Name, &schedule.Target.Type, &schedule.Target.Ref, &schedule.Target.Name, &schedule.Preset, &schedule.CronExpression, &schedule.Timezone, &scheduleInput, &schedule.SessionPolicy, &schedule.NotificationPolicy, &schedule.State, &schedule.Enabled, &schedule.Version, &schedule.NextRunAt, &schedule.LastRunAt, &schedule.CreatedAt, &schedule.UpdatedAt); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if schedule.State != "ACTIVE" || !schedule.Enabled {
		return commandOutcome{}, errs.ErrConflict
	}
	if json.Unmarshal(scheduleInput, &schedule.Input) != nil || attachScheduleDisplay(&schedule) != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	schedule.NextActions = scheduleActions(schedule, true)
	if state != "CLAIMED" {
		return commandOutcome{}, errs.ErrConflict
	}
	var immutableInput map[string]any
	if json.Unmarshal(occurrenceInput, &immutableInput) != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	nested := input
	nested.Kind = command.LaunchRun
	nested.Payload = command.LaunchRunInput{ProjectRef: projectRef, Title: name, Task: "i18n:SCHEDULED_RUN_TASK", Source: "SCHEDULE", Target: entity.RunTarget{Type: targetType, Ref: targetRef}, Input: immutableInput}
	outcome, err := repository.launchRun(ctx, tx, scope, nested)
	if err != nil {
		return commandOutcome{}, err
	}
	var runID string
	_ = tx.QueryRow(ctx, queryWorkersChangeoccurrenceSelectRunsRef, outcome.result.Run.Ref).Scan(&runID)
	_, _ = tx.Exec(ctx, queryWorkersChangeoccurrenceUpdateScheduleOccurrencesStateRunIdVersion, occurrenceID, runID)
	outcome.resourceKind = "SCHEDULE_OCCURRENCE"
	outcome.resourceRef = payload.OccurrenceRef
	outcome.summary = "i18n:SCHEDULE_OCCURRENCE_MATERIALIZED"
	outcome.result.Schedule = &schedule
	return outcome, nil
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
		id, ref, connectionRef, definitionKey, definitionVersion, definitionDigest string
		generation                                                                 int64
		configuration                                                              []byte
		credential                                                                 entity.IntegrationCredentialRevision
		credentialCreatedAt                                                        *time.Time
	}
	candidates := make([]candidate, 0, limit)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(
			&item.id, &item.ref, &item.generation, &item.connectionRef, &item.definitionKey,
			&item.configuration, &item.definitionVersion, &item.definitionDigest,
			&item.credential.Ref, &item.credential.Revision, &item.credential.SecretRef,
			&item.credential.SecretUID, &item.credential.SecretResourceVersion,
			&item.credential.ContentSHA256, &item.credentialCreatedAt,
		); err != nil {
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
		claim := map[string]any{
			"testRef": item.ref, "connectionRef": item.connectionRef, "definitionKey": item.definitionKey,
			"definitionVersion": item.definitionVersion, "definitionDigest": item.definitionDigest,
			"configuration": configuration, "leaseRef": leaseRef, "fence": fence,
			"generation": generation, "expiresAt": expiresAt,
		}
		if item.credential.Ref != "" && item.credentialCreatedAt != nil {
			item.credential.CreatedAt = *item.credentialCreatedAt
			claim["credential"] = item.credential
		}
		result = append(result, claim)
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
	summary := "i18n:INTEGRATION_TEST_SUCCEEDED"
	if !payload.Success {
		nextTest, nextConnection = "FAILED", "DEGRADED"
		summary = "i18n:" + payload.SafeErrorCode
		if payload.SafeErrorCode == "INTEGRATION_AUTH_REJECTED" || payload.SafeErrorCode == "INTEGRATION_CREDENTIAL_UNAVAILABLE" {
			credentials = "INVALID"
		}
	}
	if _, err := tx.Exec(ctx, queryWorkersCompleteintegrationtestUpdateIntegrationConnectionTestsStateResultSummarySafeErrorCode, testID, nextTest, summary, payload.SafeErrorCode); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var item entity.IntegrationConnection
	if err := tx.QueryRow(ctx, queryWorkersCompleteintegrationtestUpdateIntegrationConnectionsStateMaskedCredentialsStateLastTestSummary, connectionID, nextConnection, credentials, summary).Scan(&item.Ref, &item.DefinitionKey, &item.Name, &item.State, &item.MaskedCredentialsState, &item.LastTestSummary, &item.Enabled, &item.Version, &item.LastTestedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return commandOutcome{}, errs.ErrConflict
	}
	item, err := readConnection(ctx, tx, scope, connectionRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Connection: &item}, resourceKind: "INTEGRATION_CONNECTION", resourceRef: connectionRef, summary: "i18n:INTEGRATION_CONNECTION_TEST_COMPLETED", platformEvent: "INTEGRATION_CONNECTION_CHANGED"}, nil
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
	var runID, nodeID, connectionID, grantID, grantRef, projectID, rootRunID string
	var definitionKey, definitionVersion, definitionDigest, risk, approvalPolicy, resourceKind, resourceScopeDigest string
	var encodedScope []byte
	err = tx.QueryRow(ctx, queryWorkersResolveintegrationinvocationSelectRunsIdOrganizationIdRef,
		scope.organizationID, input["run_ref"], input["node_ref"], input["connection_ref"], input["capability_key"],
	).Scan(
		&runID, &nodeID, &connectionID, &grantID, &grantRef, &projectID, &rootRunID,
		&definitionKey, &definitionVersion, &definitionDigest, &risk, &approvalPolicy,
		&resourceKind, &encodedScope, &resourceScopeDigest,
	)
	if err != nil {
		return nil, errs.ErrForbidden
	}
	definition, exists := repository.integrationDefinitions[definitionKey]
	capability, capabilityExists := definition.Capability(input["capability_key"])
	if !exists || !capabilityExists || definition.Metadata.Version != definitionVersion || definition.Digest != definitionDigest ||
		capability.Risk != risk || capability.ApprovalPolicy != approvalPolicy || capability.ResourceScope.Kind != resourceKind {
		return nil, errs.ErrForbidden
	}
	canonicalInput, err := capability.ValidateInput(encodedInput)
	if err != nil {
		return nil, errs.ErrInvalid
	}
	var resourceScope map[string]string
	if json.Unmarshal(encodedScope, &resourceScope) != nil {
		return nil, errs.ErrUnavailable
	}
	invocationRef, _ := newRef("inv")
	inputDigest := sha256.Sum256(canonicalInput)
	inputDigestHex := hex.EncodeToString(inputDigest[:])
	intentDigest := sha256.Sum256([]byte(strings.Join([]string{
		input["node_ref"], input["idempotency_key"], input["connection_ref"], input["capability_key"],
		inputDigestHex, definitionDigest, resourceScopeDigest,
	}, "\x00")))
	intentDigestHex := hex.EncodeToString(intentDigest[:])
	effectKey := "eff_" + intentDigestHex[:32]
	state := "READY"
	if risk != "READ" {
		state = "WAITING_APPROVAL"
	}
	var invocationID, resolvedRef, resolvedState string
	if err := tx.QueryRow(ctx, queryWorkersResolveintegrationinvocationInsertIntegrationInvocationsRefRunIdConnectionId,
		invocationRef, scope.organizationID, runID, nodeID, connectionID, grantID, input["capability_key"],
		capability.Operation, input["idempotency_key"], intentDigestHex, inputDigestHex, canonicalInput, state,
		definitionVersion, definitionDigest, risk, approvalPolicy, resourceKind, encodedScope, resourceScopeDigest, effectKey,
	).Scan(&invocationID, &resolvedRef, &resolvedState); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errs.ErrIdempotencyReuse
		}
		return nil, mapWriteError(err)
	}
	gateRef := ""
	if resolvedState == "WAITING_APPROVAL" {
		err := tx.QueryRow(ctx, queryWorkersResolveintegrationinvocationSelectGate, invocationID).Scan(&gateRef)
		if errors.Is(err, pgx.ErrNoRows) {
			gateNodeRef, _ := newRef("nod")
			var gateNodeID string
			if err := tx.QueryRow(ctx, queryWorkersResolveintegrationinvocationInsertGateNode,
				gateNodeRef, scope.organizationID, rootRunID, runID, nodeID,
			).Scan(&gateNodeID); err != nil {
				return nil, errs.ErrUnavailable
			}
			edgeRef, _ := newRef("edg")
			if _, err := tx.Exec(ctx, queryWorkersResolveintegrationinvocationInsertGateEdge,
				edgeRef, scope.organizationID, rootRunID, nodeID, gateNodeID,
			); err != nil {
				return nil, errs.ErrUnavailable
			}
			gateRef, _ = newRef("gat")
			var gateID string
			if err := tx.QueryRow(ctx, queryWorkersResolveintegrationinvocationInsertOwnerGate,
				gateRef, scope.organizationID, projectID, rootRunID, gateNodeID,
				truncate(input["capability_key"]+" "+string(encodedScope), 1000), invocationID,
			).Scan(&gateID); err != nil {
				return nil, errs.ErrUnavailable
			}
			if _, err := tx.Exec(ctx, queryWorkersResolveintegrationinvocationUpdateRunWaitingHuman, rootRunID); err != nil {
				return nil, errs.ErrUnavailable
			}
			if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, gateRef,
				"OWNER_GATE_OPENED", gateNodeRef, edgeRef, gateRef, "", "i18n:INTEGRATION_EFFECT_OWNER_DECISION_REQUIRED",
				"WAITING_HUMAN", "WAITING",
			); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, errs.ErrUnavailable
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return map[string]any{
		"invocationRef": resolvedRef, "grantRef": grantRef, "operation": capability.Operation,
		"state": resolvedState, "gateRef": gateRef, "risk": risk, "resourceKind": resourceKind,
		"resourceScope": resourceScope, "resourceScopeDigest": resourceScopeDigest, "projectID": projectID,
	}, nil
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
		id, ref, connectionRef, definitionKey, capabilityKey                 string
		definitionVersion, definitionDigest, operation, risk, approvalPolicy string
		resourceKind, resourceScopeDigest, effectKey, inputDigest            string
		generation                                                           int64
		configuration, boundedInput, resourceScope                           []byte
		credential                                                           entity.IntegrationCredentialRevision
		credentialCreatedAt                                                  *time.Time
	}
	candidates := make([]candidate, 0, limit)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(
			&item.id, &item.ref, &item.generation, &item.connectionRef, &item.definitionKey,
			&item.configuration, &item.capabilityKey, &item.boundedInput, &item.definitionVersion,
			&item.definitionDigest, &item.operation, &item.risk, &item.approvalPolicy,
			&item.resourceKind, &item.resourceScope, &item.resourceScopeDigest, &item.effectKey,
			&item.inputDigest, &item.credential.Ref, &item.credential.Revision, &item.credential.SecretRef,
			&item.credential.SecretUID, &item.credential.SecretResourceVersion, &item.credential.ContentSHA256,
			&item.credentialCreatedAt,
		); err != nil {
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
		resourceScope := map[string]string{}
		_ = json.Unmarshal(item.configuration, &configuration)
		_ = json.Unmarshal(item.boundedInput, &bounded)
		_ = json.Unmarshal(item.resourceScope, &resourceScope)
		claim := map[string]any{
			"invocationRef": item.ref, "connectionRef": item.connectionRef, "definitionKey": item.definitionKey,
			"capabilityKey": item.capabilityKey, "configuration": configuration, "boundedInput": bounded,
			"definitionVersion": item.definitionVersion, "definitionDigest": item.definitionDigest,
			"operation": item.operation, "risk": item.risk, "approvalPolicy": item.approvalPolicy,
			"resourceKind": item.resourceKind, "resourceScope": resourceScope,
			"resourceScopeDigest": item.resourceScopeDigest, "effectKey": item.effectKey, "inputDigest": item.inputDigest,
			"leaseRef": leaseRef, "fence": fence, "generation": generation, "expiresAt": expiresAt,
		}
		if item.credential.Ref != "" && item.credentialCreatedAt != nil {
			item.credential.CreatedAt = *item.credentialCreatedAt
			claim["credential"] = item.credential
		}
		result = append(result, claim)
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
	var state, resultSummary, safeErrorCode, gateRef, receiptRef string
	if err := repository.pool.QueryRow(ctx, queryWorkersGetintegrationinvocationSelectIntegrationInvocationsOrganizationIdRef, scope.organizationID, ref).Scan(&state, &resultSummary, &safeErrorCode, &gateRef, &receiptRef); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errs.ErrNotFound
		}
		return nil, errs.ErrUnavailable
	}
	return map[string]any{"state": state, "resultSummary": resultSummary, "safeErrorCode": safeErrorCode, "gateRef": gateRef, "effectReceiptRef": receiptRef}, nil
}

func (repository *Repository) completeIntegrationInvocation(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.IntegrationInvocationInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	if payload.Success && (payload.SafeErrorCode != "" || payload.EffectKey == "" || payload.InputDigest == "" ||
		payload.ProviderEffectRef == "" || len(payload.ResponseDigest) != sha256.Size*2 || payload.ResultSummary == "") ||
		!payload.Success && (!safeIntegrationErrorCode(payload.SafeErrorCode) || payload.EffectKey != "" ||
			payload.InputDigest != "" || payload.ProviderEffectRef != "" || payload.ResponseDigest != "") {
		return commandOutcome{}, errs.ErrInvalid
	}
	var invocationID, runID, rootRunID, projectID, projectRef, nodeRef, storedDigest, state, leaseRef string
	var effectKey, inputDigest, receiptRef, receiptEffectKey, receiptInputDigest string
	var receiptProviderRef, receiptResponseDigest, receiptResult string
	var generation int64
	var expiresAt *time.Time
	err := tx.QueryRow(ctx, queryWorkersCompleteintegrationinvocationSelectIntegrationInvocationsOrganizationIdRef, scope.organizationID, payload.InvocationRef).Scan(
		&invocationID, &runID, &rootRunID, &projectID, &projectRef, &nodeRef, &storedDigest,
		&generation, &state, &leaseRef, &expiresAt, &effectKey, &inputDigest, &receiptRef,
		&receiptEffectKey, &receiptInputDigest, &receiptProviderRef, &receiptResponseDigest, &receiptResult,
	)
	if err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if state == "SUCCEEDED" && payload.Success && receiptRef != "" &&
		receiptEffectKey == payload.EffectKey && receiptInputDigest == payload.InputDigest &&
		receiptProviderRef == payload.ProviderEffectRef && receiptResponseDigest == payload.ResponseDigest &&
		receiptResult == truncate(payload.ResultSummary, 2000) {
		runRef, runErr := mustRunRef(ctx, tx, runID)
		if runErr != nil {
			return commandOutcome{}, runErr
		}
		run, graph, readErr := repository.readRunGraphTx(ctx, tx, scope, runRef)
		if readErr != nil {
			return commandOutcome{}, readErr
		}
		return commandOutcome{result: command.Result{Run: &run, Graph: &graph, Duplicate: true}, projectID: projectID, projectRef: projectRef, resourceKind: "INTEGRATION_INVOCATION", resourceRef: payload.InvocationRef, summary: "i18n:INTEGRATION_INVOCATION_COMPLETED"}, nil
	}
	digest := sha256.Sum256([]byte(payload.Fence))
	if storedDigest != hex.EncodeToString(digest[:]) || state != "RUNNING" || leaseRef != payload.LeaseRef ||
		generation != payload.Generation || expiresAt == nil || time.Now().After(*expiresAt) ||
		payload.Success && (effectKey != payload.EffectKey || inputDigest != payload.InputDigest) {
		return commandOutcome{}, errs.ErrForbidden
	}
	next := "SUCCEEDED"
	if !payload.Success {
		next = "FAILED"
	}
	var receiptID any
	if payload.Success {
		generatedReceiptRef, _ := newRef("erc")
		var storedReceiptRef string
		var storedReceiptID string
		if err := tx.QueryRow(ctx, queryWorkersCompleteintegrationinvocationInsertEffectReceipt,
			generatedReceiptRef, scope.organizationID, invocationID, payload.EffectKey, payload.InputDigest,
			truncate(payload.ProviderEffectRef, 256), payload.ResponseDigest, truncate(payload.ResultSummary, 2000),
		).Scan(&storedReceiptID, &storedReceiptRef); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return commandOutcome{}, errs.ErrConflict
			}
			return commandOutcome{}, errs.ErrUnavailable
		}
		receiptID = storedReceiptID
	}
	if _, err := tx.Exec(ctx, queryWorkersCompleteintegrationinvocationUpdateIntegrationInvocationsStateResultSummarySafeErrorCode,
		invocationID, next, truncate(payload.ResultSummary, 2000), truncate(payload.SafeErrorCode, 100), receiptID,
	); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	event, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, payload.InvocationRef, "TURN_PROGRESS", nodeRef, "", "", "", "i18n:INTEGRATION_ACTION_COMPLETED", "RUNNING", "RUNNING")
	if err != nil {
		return commandOutcome{}, err
	}
	runRef, err := mustRunRef(ctx, tx, runID)
	if err != nil {
		return commandOutcome{}, err
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, scope, runRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Run: &run, Graph: &graph, Event: &event}, projectID: projectID, projectRef: projectRef, resourceKind: "INTEGRATION_INVOCATION", resourceRef: payload.InvocationRef, summary: "i18n:INTEGRATION_INVOCATION_COMPLETED"}, nil
}

func safeIntegrationErrorCode(code string) bool {
	switch code {
	case "INTEGRATION_AUTH_REJECTED", "INTEGRATION_CREDENTIAL_UNAVAILABLE", "INTEGRATION_UNAVAILABLE", "INTEGRATION_RATE_LIMITED", "INTEGRATION_CONFIGURATION_INVALID", "INTEGRATION_CAPABILITY_UNSUPPORTED", "INTEGRATION_REQUEST_REJECTED", "INTEGRATION_RESPONSE_INVALID":
		return true
	default:
		return false
	}
}
