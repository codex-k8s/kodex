package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
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
	if _, err := repository.reconcileSystemAssistantProviderPolicy(ctx, tx, scope); err != nil {
		return entity.SystemAssistant{}, nil, false, err
	}
	sessionBinding, err := repository.lockWarmSessionBinding(ctx, tx, scope.organizationID)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, err
	}
	sessionMigrated := false
	if !sessionBinding.providerAccountEligible {
		providerAccountID, selectErr := repository.selectProviderAccountForAgent(
			ctx, tx, scope.organizationID, sessionBinding.assistantRef,
		)
		if errors.Is(selectErr, errs.ErrConflict) {
			if err := repository.markWarmRuntimeUnavailable(ctx, tx, scope.organizationID, sessionBinding); err != nil {
				return entity.SystemAssistant{}, nil, false, err
			}
			if err := tx.Commit(ctx); err != nil {
				return entity.SystemAssistant{}, nil, false, errs.ErrConflict
			}
			return entity.SystemAssistant{}, nil, false, errs.ErrUnavailable
		}
		if selectErr != nil {
			return entity.SystemAssistant{}, nil, false, selectErr
		}
		if err := repository.replaceWarmSession(ctx, tx, scope.organizationID, sessionBinding, providerAccountID); err != nil {
			return entity.SystemAssistant{}, nil, false, err
		}
		sessionMigrated = true
	}
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
	var rawEnvironmentValues, rawSecretProjections, rawEnvironmentTools []byte
	var rawResourcePolicy, rawVolumePolicy, rawNetworkPolicy, rawKubernetesAccessProfile []byte
	var environmentCoreDigest, resourcesDigest, volumesDigest, networkDigest, rbacDigest string
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
		&rawEnvironmentValues, &rawSecretProjections, &rawEnvironmentTools,
		&environmentCoreDigest, &rawResourcePolicy, &rawVolumePolicy, &rawNetworkPolicy, &rawKubernetesAccessProfile,
		&resourcesDigest, &volumesDigest, &networkDigest, &rbacDigest,
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
	var environmentTools []runtimecontract.RuntimeEnvironmentTool
	if err := decodeStrict(rawEnvironmentTools, &environmentTools); err != nil {
		return entity.SystemAssistant{}, nil, false, errs.ErrConflict
	}
	environmentImage := runtimecontract.RuntimeEnvironmentImage{
		Reference: repository.roleImages.DefaultImageReference,
		Digest:    repository.roleImages.DefaultImageDigest,
	}
	environmentPolicy, err := decodeRuntimeEnvironmentPolicy(rawResourcePolicy, rawVolumePolicy, rawNetworkPolicy,
		rawKubernetesAccessProfile, resourcesDigest, volumesDigest, networkDigest, rbacDigest)
	if err != nil || environmentPolicy.KubernetesAccess.Kind != runtimecontract.RuntimeKubernetesAccessNone {
		return entity.SystemAssistant{}, nil, false, errs.ErrConflict
	}
	effectiveKubernetesAccess, err := runtimecontract.RuntimeKubernetesAccessForExecution(
		environmentPolicy.KubernetesAccess, "agent-runner", "system-assistant-warm")
	if err != nil {
		return entity.SystemAssistant{}, nil, false, errs.ErrConflict
	}
	verifiedCoreDigest, err := runtimecontract.RuntimeEnvironmentCoreDigest(environmentValues, secretProjections, environmentImage, environmentTools)
	if err != nil || verifiedCoreDigest != environmentCoreDigest {
		return entity.SystemAssistant{}, nil, false, errs.ErrConflict
	}
	verifiedEnvironmentDigest, err := runtimecontract.RuntimeEnvironmentDigest(
		environmentValues, secretProjections, environmentImage, environmentTools, environmentPolicy)
	if err != nil || verifiedEnvironmentDigest != runtimeEnvironmentDigest {
		return entity.SystemAssistant{}, nil, false, errs.ErrConflict
	}
	_ = json.Unmarshal(limits, &assistant.ResourceLimits)
	assistant.OwnerInstructions = ownerInstructions
	assistant.WarmSessionRef = systemSessionRef
	assistant.System = true
	assistant.Deletable = false
	stale := assistant.LastHeartbeatAt == nil || time.Since(*assistant.LastHeartbeatAt) > 45*time.Second
	required := sessionMigrated || !contains([]string{"READY", "BUSY"}, assistant.RuntimeState) || assistant.RuntimeRevision != assistant.DesiredRuntimeRevision || warmInstance != instance || stale
	if required && !sessionMigrated {
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
	resolvedInstructionsSum := sha256.Sum256([]byte(resolvedInstructions))
	resolvedInstructionsDigest := hex.EncodeToString(resolvedInstructionsSum[:])
	workspacePolicy := runtimeWorkspacePolicy()
	snapshot := map[string]any{
		"organizationRef": scope.organizationRef, "assistantRef": assistant.Ref, "agentRef": assistant.Ref,
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
		"instructionRef":              promptRef,
		"instructionDigest":           resolvedInstructionsDigest,
		"promptTemplateRef":           promptRef,
		"promptTemplateDigest":        promptDigest,
		"promptMaterializationDigest": resolvedInstructionsDigest,
		"ownerInstructions":           ownerInstructions, "instructions": resolvedInstructions,
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
		"environmentTools":            environmentTools,
		"environmentPolicy":           environmentPolicy,
		"effectiveKubernetesAccess":   effectiveKubernetesAccess,
		"workspacePolicy":             workspacePolicy,
	}
	revisionDigest, err := runtimeRevisionDigestFromSnapshot(snapshot)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, errs.ErrConflict
	}
	snapshot["revisionDigest"] = revisionDigest
	if err := tx.Commit(ctx); err != nil {
		return entity.SystemAssistant{}, nil, false, errs.ErrConflict
	}
	return assistant, snapshot, required, nil
}

type systemAssistantProviderPolicySnapshot struct {
	agentID, agentRef, configID, runtimeProfileRef, provider, model, mode string
	configVersion                                                         int64
	currentCandidates, desiredCandidates                                  []entity.ProviderAccountCandidate
}

func (repository *Repository) reconcileSystemAssistantProviderPolicy(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
) (bool, error) {
	var snapshot systemAssistantProviderPolicySnapshot
	var rawCurrent, rawDesired []byte
	err := tx.QueryRow(ctx, queryWorkersReconcilewarmruntimeSelectSystemProviderPolicy, pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
	}).Scan(
		&snapshot.agentID,
		&snapshot.agentRef,
		&snapshot.configID,
		&snapshot.configVersion,
		&snapshot.runtimeProfileRef,
		&snapshot.provider,
		&snapshot.model,
		&snapshot.mode,
		&rawCurrent,
		&rawDesired,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, errs.ErrConflict
	}
	if err != nil {
		return false, errs.ErrUnavailable
	}
	if json.Unmarshal(rawCurrent, &snapshot.currentCandidates) != nil ||
		json.Unmarshal(rawDesired, &snapshot.desiredCandidates) != nil ||
		!validProviderPolicy(snapshot.mode, snapshot.currentCandidates) {
		return false, errs.ErrConflict
	}
	if len(snapshot.desiredCandidates) == 0 {
		return false, nil
	}
	desiredMode := "LEAST_USED"
	if len(snapshot.desiredCandidates) == 1 {
		desiredMode = "FIXED"
	}
	if !validProviderPolicy(desiredMode, snapshot.desiredCandidates) {
		return false, errs.ErrConflict
	}
	sort.Slice(snapshot.currentCandidates, func(left, right int) bool {
		return snapshot.currentCandidates[left].AccountRef < snapshot.currentCandidates[right].AccountRef
	})
	sort.Slice(snapshot.desiredCandidates, func(left, right int) bool {
		return snapshot.desiredCandidates[left].AccountRef < snapshot.desiredCandidates[right].AccountRef
	})
	if snapshot.mode == desiredMode && providerCandidatesEqual(snapshot.currentCandidates, snapshot.desiredCandidates) {
		return false, nil
	}
	rawDesired, err = json.Marshal(snapshot.desiredCandidates)
	if err != nil {
		return false, errs.ErrConflict
	}
	policyRef, err := newRef("ppol")
	if err != nil {
		return false, err
	}
	configRef, err := newRef("rconf")
	if err != nil {
		return false, err
	}
	auditRef, err := newRef("aud")
	if err != nil {
		return false, err
	}
	version := snapshot.configVersion + 1
	policyDigest := digestBytes([]byte(desiredMode), rawDesired)
	configDigest := digestBytes(
		[]byte(snapshot.runtimeProfileRef),
		[]byte(snapshot.provider),
		[]byte(snapshot.model),
		[]byte(policyRef),
		[]byte(strconvFormat(version)),
		[]byte(policyDigest),
	)
	var publishedRef string
	err = tx.QueryRow(ctx, queryWorkersReconcilewarmruntimePublishSystemProviderPolicy, pgx.StrictNamedArgs{
		"policy_ref": policyRef, "organization_id": current.organizationID, "agent_id": snapshot.agentID,
		"version_number": version, "policy_mode": desiredMode, "account_candidates": rawDesired,
		"policy_digest": policyDigest, "created_by": current.actorID, "config_ref": configRef,
		"runtime_profile_ref": snapshot.runtimeProfileRef, "provider": snapshot.provider, "model": snapshot.model,
		"config_digest": configDigest, "current_config_id": snapshot.configID,
		"next_runtime_revision": "system-assistant-runtime-" + configDigest, "audit_ref": auditRef,
	}).Scan(&publishedRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, errs.ErrConflict
	}
	if err != nil || publishedRef != configRef {
		return false, errs.ErrUnavailable
	}
	return true, nil
}

func providerCandidatesEqual(left, right []entity.ProviderAccountCandidate) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type warmSessionBinding struct {
	assistantRef            string
	sessionID               string
	sessionRef              string
	createdBy               string
	providerAccountEligible bool
}

func (repository *Repository) lockWarmSessionBinding(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
) (warmSessionBinding, error) {
	var binding warmSessionBinding
	err := tx.QueryRow(ctx, queryWorkersReconcilewarmruntimeLockSessionBinding, pgx.StrictNamedArgs{
		"organization_id": organizationID,
	}).Scan(
		&binding.assistantRef,
		&binding.sessionID,
		&binding.sessionRef,
		&binding.createdBy,
		&binding.providerAccountEligible,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return warmSessionBinding{}, errs.ErrConflict
	}
	if err != nil {
		return warmSessionBinding{}, errs.ErrUnavailable
	}
	return binding, nil
}

func (repository *Repository) replaceWarmSession(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	current warmSessionBinding,
	providerAccountID string,
) error {
	if providerAccountID == "" {
		return errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryWorkersReconcilewarmruntimeCloseSession, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"session_id":      current.sessionID,
	}); err != nil {
		return errs.ErrUnavailable
	}
	nextSessionRef, err := newRef("ses")
	if err != nil {
		return err
	}
	var nextSessionID string
	if err := tx.QueryRow(ctx, queryWorkersReconcilewarmruntimeInsertSession, pgx.StrictNamedArgs{
		"session_ref":         nextSessionRef,
		"organization_id":     organizationID,
		"provider_account_id": providerAccountID,
		"created_by":          current.createdBy,
	}).Scan(&nextSessionID); err != nil || nextSessionID == "" {
		return errs.ErrUnavailable
	}
	var runtimeVersion int64
	if err := tx.QueryRow(ctx, queryWorkersReconcilewarmruntimeSwitchSession, pgx.StrictNamedArgs{
		"organization_id":     organizationID,
		"current_session_ref": current.sessionRef,
		"next_session_ref":    nextSessionRef,
	}).Scan(&runtimeVersion); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	} else if err != nil || runtimeVersion < 1 {
		return errs.ErrUnavailable
	}
	return nil
}

func (repository *Repository) markWarmRuntimeUnavailable(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	current warmSessionBinding,
) error {
	if _, err := tx.Exec(ctx, queryWorkersReconcilewarmruntimeCloseSession, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"session_id":      current.sessionID,
	}); err != nil {
		return errs.ErrUnavailable
	}
	var runtimeVersion int64
	if err := tx.QueryRow(ctx, queryWorkersReconcilewarmruntimeMarkUnavailable, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"session_ref":     current.sessionRef,
	}).Scan(&runtimeVersion); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	} else if err != nil || runtimeVersion < 1 {
		return errs.ErrUnavailable
	}
	return nil
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
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var now time.Time
	if err := tx.QueryRow(ctx, queryWorkersScheduleClock).Scan(&now); err != nil {
		return nil, errs.ErrUnavailable
	}
	deadRows, err := tx.Query(ctx, queryWorkersScheduleOccurrenceExpireDeadLetter, scope.organizationID, limit)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	deadIDs, err := pgx.CollectRows(deadRows, pgx.RowTo[string])
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	for _, occurrenceID := range deadIDs {
		if err := repository.emitScheduleOccurrenceChange(ctx, tx, scope, occurrenceID); err != nil {
			return nil, err
		}
	}
	result := make([]map[string]any, 0, limit)
	expiredRows, err := tx.Query(ctx, queryWorkersClaimdueschedulesSelectExpiredOccurrences, scope.organizationID, limit)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	type expiredOccurrence struct {
		id, ref, scheduleRef, inputDigest, scheduleRevisionRef, scheduleRevisionDigest string
		targetRef, targetDigest, automationTextDigest, promptInputsDigest              string
		scheduledFor                                                                   time.Time
		scheduleVersion, generation, scheduleRevision, targetVersion                   int64
		attempt                                                                        int32
	}
	expired := make([]expiredOccurrence, 0, limit)
	for expiredRows.Next() {
		var item expiredOccurrence
		if err := expiredRows.Scan(&item.id, &item.ref, &item.scheduleRef, &item.scheduledFor, &item.scheduleVersion,
			&item.inputDigest, &item.generation, &item.attempt, &item.scheduleRevisionRef, &item.scheduleRevision, &item.scheduleRevisionDigest,
			&item.targetRef, &item.targetVersion, &item.targetDigest, &item.automationTextDigest, &item.promptInputsDigest); err != nil {
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
		expires := now.Add(30 * time.Second)
		if _, err := tx.Exec(ctx, queryWorkersScheduleAttemptFinish, item.id, "EXPIRED", "SCHEDULE_LEASE_EXPIRED", item.attempt, item.generation); err != nil {
			return nil, errs.ErrUnavailable
		}
		var generation int64
		var attempt int32
		if err := tx.QueryRow(ctx, queryWorkersClaimdueschedulesReclaimExpiredOccurrence, item.id, leaseRef, hex.EncodeToString(digest[:]), instance, expires, item.generation).Scan(&generation, &attempt); errors.Is(err, pgx.ErrNoRows) {
			return nil, errs.ErrConflict
		} else if err != nil {
			return nil, errs.ErrUnavailable
		}
		attemptRef, _ := newRef("satt")
		if _, err := tx.Exec(ctx, queryWorkersClaimdueschedulesInsertAttempt, attemptRef, scope.organizationID, item.id,
			attempt, generation, leaseRef, hex.EncodeToString(digest[:]), instance, item.inputDigest,
			item.scheduleRevisionDigest, expires, principal.CredentialRevision); err != nil {
			return nil, errs.ErrUnavailable
		}
		if err := repository.emitScheduleOccurrenceChange(ctx, tx, scope, item.id); err != nil {
			return nil, err
		}
		result = append(result, map[string]any{"scheduleRef": item.scheduleRef, "occurrenceRef": item.ref, "scheduledFor": item.scheduledFor,
			"leaseRef": leaseRef, "fence": fence, "generation": generation, "expiresAt": expires,
			"attempt": attempt, "targetRef": item.targetRef, "targetVersion": item.targetVersion,
			"targetDigest": item.targetDigest, "automationTextDigest": item.automationTextDigest,
			"promptInputsDigest": item.promptInputsDigest,
			"scheduleVersion":    item.scheduleVersion, "scheduleRevisionRef": item.scheduleRevisionRef,
			"scheduleRevision": item.scheduleRevision, "scheduleRevisionDigest": item.scheduleRevisionDigest,
			"inputDigest": item.inputDigest})
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
		currentRevisionID, currentRevisionRef, currentRevisionDigest string
		input, promptInputs                                          []byte
		dstGapPolicy, dstFoldPolicy, misfirePolicy, overlapPolicy    string
		targetDigest, automationText                                 string
		initiatedBy                                                  string
		scheduledFor                                                 time.Time
		version, currentRevision, targetVersion                      int64
	}
	due := make([]dueSchedule, 0, limit)
	for rows.Next() {
		var item dueSchedule
		if err := rows.Scan(&item.id, &item.ref, &item.scheduledFor, &item.version, &item.preset, &item.cron, &item.timezone,
			&item.name, &item.targetType, &item.targetRef, &item.input, &item.currentRevisionID,
			&item.currentRevisionRef, &item.currentRevision, &item.currentRevisionDigest,
			&item.dstGapPolicy, &item.dstFoldPolicy, &item.misfirePolicy, &item.overlapPolicy,
			&item.targetVersion, &item.targetDigest, &item.automationText, &item.promptInputs, &item.initiatedBy); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		due = append(due, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, errs.ErrUnavailable
	}
	for _, item := range due {
		occurrence, next, nextErr := scheduleservice.ResolveDue(scheduleservice.Spec{
			Preset: item.preset, CronExpression: item.cron, Timezone: item.timezone,
			DSTGapPolicy: item.dstGapPolicy, DSTFoldPolicy: item.dstFoldPolicy,
			MisfirePolicy: item.misfirePolicy, OverlapPolicy: item.overlapPolicy,
		}, item.scheduledFor, now)
		if nextErr != nil {
			return nil, errs.ErrUnavailable
		}
		tag, updateErr := tx.Exec(ctx, queryWorkersClaimdueschedulesUpdateSchedulesNextRunAt, item.id, next, item.scheduledFor)
		if updateErr != nil || tag.RowsAffected() != 1 {
			return nil, errs.ErrConflict
		}
		skipped := occurrence == nil
		if skipped {
			occurrence = &item.scheduledFor
		}
		occurrenceRef, _ := newRef("occ")
		leaseRef, _ := newRef("lea")
		fence, _ := newRef("fnc")
		digest := sha256.Sum256([]byte(fence))
		inputDigest := sha256.Sum256(item.input)
		automationTextDigest := sha256.Sum256([]byte(item.automationText))
		promptInputsDigest := sha256.Sum256(item.promptInputs)
		expires := now.Add(30 * time.Second)
		var occurrenceID string
		if err := tx.QueryRow(ctx, queryWorkersClaimdueschedulesInsertScheduleOccurrencesRefScheduleIdState, occurrenceRef, scope.organizationID, item.id, *occurrence, item.version, item.targetType, item.targetRef, item.name, item.input, hex.EncodeToString(inputDigest[:]), leaseRef, hex.EncodeToString(digest[:]), instance, expires, item.currentRevisionID, item.targetVersion, item.targetDigest, item.automationText, hex.EncodeToString(automationTextDigest[:]), item.promptInputs, hex.EncodeToString(promptInputsDigest[:]), item.initiatedBy, skipped).Scan(&occurrenceID); err != nil {
			return nil, mapWriteError(err)
		}
		if skipped {
			if err := repository.emitScheduleOccurrenceChange(ctx, tx, scope, occurrenceID); err != nil {
				return nil, err
			}
			continue
		}
		attemptRef, _ := newRef("satt")
		if _, err := tx.Exec(ctx, queryWorkersClaimdueschedulesInsertAttempt, attemptRef, scope.organizationID, occurrenceID,
			int32(1), int64(1), leaseRef, hex.EncodeToString(digest[:]), instance,
			hex.EncodeToString(inputDigest[:]), item.currentRevisionDigest, expires, principal.CredentialRevision); err != nil {
			return nil, errs.ErrUnavailable
		}
		if err := repository.emitScheduleOccurrenceChange(ctx, tx, scope, occurrenceID); err != nil {
			return nil, err
		}
		result = append(result, map[string]any{"scheduleRef": item.ref, "occurrenceRef": occurrenceRef, "scheduledFor": *occurrence,
			"leaseRef": leaseRef, "fence": fence, "generation": int64(1), "expiresAt": expires,
			"attempt": int32(1), "targetRef": item.targetRef, "targetVersion": item.targetVersion,
			"targetDigest": item.targetDigest, "automationTextDigest": hex.EncodeToString(automationTextDigest[:]),
			"promptInputsDigest": hex.EncodeToString(promptInputsDigest[:]),
			"scheduleVersion":    item.version, "scheduleRevisionRef": item.currentRevisionRef,
			"scheduleRevision": item.currentRevision, "scheduleRevisionDigest": item.currentRevisionDigest,
			"inputDigest": hex.EncodeToString(inputDigest[:])})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return result, nil
}

func (repository *Repository) RenewScheduleOccurrence(ctx context.Context, principal value.Principal, input command.OccurrenceInput) (map[string]any, error) {
	if input.OccurrenceRef == "" || input.LeaseRef == "" || input.Fence == "" || input.Generation < 1 {
		return nil, errs.ErrInvalid
	}
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	digest := sha256.Sum256([]byte(input.Fence))
	var leaseRef string
	var generation int64
	var expiresAt time.Time
	if err := tx.QueryRow(ctx, queryWorkersScheduleOccurrenceRenew, scope.organizationID, input.OccurrenceRef,
		input.LeaseRef, hex.EncodeToString(digest[:]), input.Generation, principal.CredentialRevision).Scan(&leaseRef, &generation, &expiresAt); errors.Is(err, pgx.ErrNoRows) {
		return nil, errs.ErrForbidden
	} else if err != nil {
		return nil, errs.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return map[string]any{"leaseRef": leaseRef, "fence": input.Fence, "generation": generation, "expiresAt": expiresAt}, nil
}

func (repository *Repository) changeOccurrence(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.OccurrenceInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	var occurrenceID, scheduleID, projectID, projectRef, state, storedDigest, targetType, targetRef, name, storedInputDigest string
	var revisionDigest, targetDigest, automationText, automationTextDigest, promptInputsDigest string
	var initiatedBy, initiatorRef, initiatorName string
	var sessionPolicy, continueSessionRef string
	var scheduleVersion, generation, targetVersion int64
	var attempt int32
	var occurrenceInput, promptInputs []byte
	var expires time.Time
	err := tx.QueryRow(ctx, queryWorkersChangeoccurrenceSelectScheduleOccurrencesOrganizationIdRefLeaseRef, scope.organizationID, payload.OccurrenceRef, payload.LeaseRef, input.Principal.CredentialRevision).Scan(&occurrenceID, &scheduleID, &projectID, &projectRef, &state, &storedDigest, &generation, &expires, &targetType, &targetRef, &name, &occurrenceInput, &scheduleVersion, &storedInputDigest, &attempt, &revisionDigest, &targetVersion, &targetDigest, &automationText, &automationTextDigest, &promptInputs, &promptInputsDigest, &initiatedBy, &initiatorRef, &initiatorName, &sessionPolicy, &continueSessionRef)
	if err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	digest := sha256.Sum256([]byte(payload.Fence))
	var now time.Time
	if err := tx.QueryRow(ctx, queryWorkersScheduleClock).Scan(&now); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if storedDigest != hex.EncodeToString(digest[:]) || generation != payload.Generation || !now.Before(expires) {
		return commandOutcome{}, errs.ErrForbidden
	}
	inputDigest := sha256.Sum256(occurrenceInput)
	storedAutomationDigest := sha256.Sum256([]byte(automationText))
	storedPromptInputsDigest := sha256.Sum256(promptInputs)
	if storedInputDigest != hex.EncodeToString(inputDigest[:]) ||
		automationTextDigest != hex.EncodeToString(storedAutomationDigest[:]) ||
		promptInputsDigest != hex.EncodeToString(storedPromptInputsDigest[:]) ||
		scheduleVersion < 1 || targetVersion < 1 || revisionDigest == "" || initiatedBy == "" {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if state != "CLAIMED" {
		return commandOutcome{}, errs.ErrConflict
	}
	if input.Kind == command.FailScheduleOccurrence {
		if !validScheduleErrorCode(payload.SafeErrorCode) {
			return commandOutcome{}, errs.ErrInvalid
		}
		if _, err := tx.Exec(ctx, queryWorkersScheduleAttemptFinish, occurrenceID, "FAILED", payload.SafeErrorCode, attempt, generation); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		var nextState string
		var nextAttempt int32
		if err := tx.QueryRow(ctx, queryWorkersScheduleOccurrenceFail, occurrenceID, payload.Retryable, payload.SafeErrorCode).Scan(&nextState, &nextAttempt); err != nil {
			return commandOutcome{}, errs.ErrConflict
		}
		if err := repository.emitScheduleOccurrenceChange(ctx, tx, scope, occurrenceID); err != nil {
			return commandOutcome{}, err
		}
		return commandOutcome{result: command.Result{Runtime: map[string]any{"state": nextState, "attempt": nextAttempt}},
			projectID: projectID, projectRef: projectRef, resourceKind: "SCHEDULE_OCCURRENCE",
			resourceRef: payload.OccurrenceRef, summary: "i18n:SCHEDULE_OCCURRENCE_FAILED"}, nil
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
	currentTargetVersion, currentTargetDigest, err := repository.validateScheduleTarget(ctx, tx, scope.organizationID, projectID, entity.RunTarget{Type: targetType, Ref: targetRef})
	if err != nil || currentTargetVersion != targetVersion || currentTargetDigest != targetDigest {
		return commandOutcome{}, errs.ErrConflict
	}
	var immutableInput map[string]any
	var immutablePromptInputs map[string]any
	if json.Unmarshal(occurrenceInput, &immutableInput) != nil || json.Unmarshal(promptInputs, &immutablePromptInputs) != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	nested := input
	nested.Kind = command.LaunchRun
	if sessionPolicy != "CONTINUE_ONE" {
		continueSessionRef = ""
	}
	nested.Payload = command.LaunchRunInput{ProjectRef: projectRef, Title: name, Task: automationText, Source: "SCHEDULE", SessionRef: continueSessionRef, Target: entity.RunTarget{Type: targetType, Ref: targetRef}, Input: immutableInput}
	ownerScope := scope
	ownerScope.actorID, ownerScope.actorRef, ownerScope.actorName = initiatedBy, initiatorRef, initiatorName
	if err := repository.authorizeCommand(ctx, tx, ownerScope, nested); err != nil {
		return commandOutcome{}, err
	}
	outcome, err := repository.launchRun(ctx, tx, ownerScope, nested)
	if err != nil {
		return commandOutcome{}, err
	}
	var runID string
	if err := tx.QueryRow(ctx, queryWorkersChangeoccurrenceSelectRunsRef, outcome.result.Run.Ref).Scan(&runID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if tag, updateErr := tx.Exec(ctx, queryWorkersChangeoccurrenceUpdateScheduleOccurrencesStateRunIdVersion, occurrenceID, runID); updateErr != nil || tag.RowsAffected() != 1 {
		return commandOutcome{}, errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryWorkersScheduleAttemptFinish, occurrenceID, "MATERIALIZED", "", attempt, generation); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	schedule, err = scanSchedule(tx.QueryRow(ctx, queryQueriesGetscheduleSelectSchedulesOrganizationIdRef, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "schedule_ref": schedule.Ref,
		"role": scope.role, "actor_id": initiatedBy,
	}))
	if err != nil {
		return commandOutcome{}, err
	}
	outcome.resourceKind = "SCHEDULE_OCCURRENCE"
	outcome.resourceRef = payload.OccurrenceRef
	outcome.summary = "i18n:SCHEDULE_OCCURRENCE_MATERIALIZED"
	outcome.result.Schedule = &schedule
	return outcome, nil
}

func (repository *Repository) emitScheduleOccurrenceChange(ctx context.Context, tx pgx.Tx, scope scope, occurrenceID string) error {
	var scheduleRef, projectRef string
	var version int64
	if err := tx.QueryRow(ctx, queryWorkersScheduleEventScope, occurrenceID, scope.organizationID).Scan(&scheduleRef, &projectRef, &version); err != nil {
		return errs.ErrUnavailable
	}
	return repository.emitPlatformEventSnapshot(ctx, tx, scope, "SCHEDULE_CHANGED", projectRef, scheduleRef, "i18n:SCHEDULE_UPDATED", version, "")
}

func validScheduleErrorCode(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character != '_' && (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
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
	return retrySerializableTransaction(ctx, func() (map[string]any, error) {
		return repository.resolveIntegrationInvocation(ctx, principal, input, boundedInput)
	})
}

func (repository *Repository) resolveIntegrationInvocation(ctx context.Context, principal value.Principal, input map[string]string, boundedInput map[string]any) (map[string]any, error) {
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
	var runID, nodeID, connectionID, grantID, grantRef, projectID, rootRunID, initiatorRef string
	var definitionKey, definitionVersion, definitionDigest, risk, approvalPolicy, resourceKind, resourceScopeDigest string
	var encodedScope []byte
	err = tx.QueryRow(ctx, queryWorkersResolveintegrationinvocationSelectRunsIdOrganizationIdRef,
		scope.organizationID, input["run_ref"], input["node_ref"], input["connection_ref"], input["capability_key"],
	).Scan(
		&runID, &nodeID, &connectionID, &grantID, &grantRef, &projectID, &rootRunID,
		&definitionKey, &definitionVersion, &definitionDigest, &risk, &approvalPolicy,
		&resourceKind, &encodedScope, &resourceScopeDigest, &initiatorRef,
	)
	if err != nil {
		if serializableTransactionConflict(err) {
			return nil, serializableTransactionError(err, errs.ErrUnavailable)
		}
		return nil, errs.ErrForbidden
	}
	initiatorScope := scope
	initiatorScope.actorRef = initiatorRef
	if err := repository.requireAccess(ctx, tx, initiatorScope, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: input["connection_ref"]}); err != nil {
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
		return nil, serializableTransactionError(err, mapWriteError(err))
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
				return nil, serializableTransactionError(err, errs.ErrUnavailable)
			}
			edgeRef, _ := newRef("edg")
			if _, err := tx.Exec(ctx, queryWorkersResolveintegrationinvocationInsertGateEdge,
				edgeRef, scope.organizationID, rootRunID, nodeID, gateNodeID,
			); err != nil {
				return nil, serializableTransactionError(err, errs.ErrUnavailable)
			}
			gateRef, _ = newRef("gat")
			var gateID string
			if err := tx.QueryRow(ctx, queryWorkersResolveintegrationinvocationInsertOwnerGate,
				gateRef, scope.organizationID, projectID, rootRunID, gateNodeID,
				truncate(input["capability_key"]+" "+string(encodedScope), 1000), invocationID,
			).Scan(&gateID); err != nil {
				return nil, serializableTransactionError(err, errs.ErrUnavailable)
			}
			if _, err := tx.Exec(ctx, queryWorkersResolveintegrationinvocationUpdateRunWaitingHuman, rootRunID); err != nil {
				return nil, serializableTransactionError(err, errs.ErrUnavailable)
			}
			if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, gateRef,
				"OWNER_GATE_OPENED", gateNodeRef, edgeRef, gateRef, "", "i18n:INTEGRATION_EFFECT_OWNER_DECISION_REQUIRED",
				"WAITING_HUMAN", "WAITING",
			); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, serializableTransactionError(err, errs.ErrUnavailable)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, serializableTransactionError(err, errs.ErrUnavailable)
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
		initiatorRef                                                         string
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
			&item.credentialCreatedAt, &item.initiatorRef,
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
		initiatorScope := scope
		initiatorScope.actorRef = item.initiatorRef
		if err := repository.requireAccess(ctx, tx, initiatorScope, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: item.connectionRef}); err != nil {
			continue
		}
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
	if payload.Success && payload.UnknownOutcome || payload.UnknownOutcome != (payload.SafeErrorCode == "INTEGRATION_OUTCOME_UNKNOWN") ||
		len(payload.ResultSummary) > 64<<10 ||
		payload.Success && (payload.SafeErrorCode != "" || payload.EffectKey == "" || payload.InputDigest == "" ||
			payload.ProviderEffectRef == "" || len(payload.ResponseDigest) != sha256.Size*2 || payload.ResultSummary == "") ||
		!payload.Success && (!safeIntegrationErrorCode(payload.SafeErrorCode) || payload.EffectKey != "" ||
			payload.InputDigest != "" || payload.ProviderEffectRef != "" || payload.ResponseDigest != "") {
		return commandOutcome{}, errs.ErrInvalid
	}
	if payload.Success {
		digest := sha256.Sum256([]byte(payload.ResultSummary))
		if hex.EncodeToString(digest[:]) != payload.ResponseDigest {
			return commandOutcome{}, errs.ErrInvalid
		}
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
		receiptResult == payload.ResultSummary {
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
	if payload.UnknownOutcome {
		next = "UNKNOWN_OUTCOME"
	} else if !payload.Success {
		next = "FAILED"
	}
	var receiptID any
	if payload.Success {
		generatedReceiptRef, _ := newRef("erc")
		var storedReceiptRef string
		var storedReceiptID string
		if err := tx.QueryRow(ctx, queryWorkersCompleteintegrationinvocationInsertEffectReceipt,
			generatedReceiptRef, scope.organizationID, invocationID, payload.EffectKey, payload.InputDigest,
			truncate(payload.ProviderEffectRef, 256), payload.ResponseDigest, payload.ResultSummary,
		).Scan(&storedReceiptID, &storedReceiptRef); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return commandOutcome{}, errs.ErrConflict
			}
			return commandOutcome{}, errs.ErrUnavailable
		}
		receiptID = storedReceiptID
	}
	if _, err := tx.Exec(ctx, queryWorkersCompleteintegrationinvocationUpdateIntegrationInvocationsStateResultSummarySafeErrorCode,
		invocationID, next, payload.ResultSummary, truncate(payload.SafeErrorCode, 100), receiptID,
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
	case "INTEGRATION_AUTH_REJECTED", "INTEGRATION_CREDENTIAL_UNAVAILABLE", "INTEGRATION_UNAVAILABLE", "INTEGRATION_RATE_LIMITED", "INTEGRATION_CONFIGURATION_INVALID", "INTEGRATION_CAPABILITY_UNSUPPORTED", "INTEGRATION_ROUTE_NOT_OWNED", "INTEGRATION_REQUEST_REJECTED", "INTEGRATION_RESPONSE_INVALID", "INTEGRATION_OUTCOME_UNKNOWN":
		return true
	default:
		return false
	}
}
