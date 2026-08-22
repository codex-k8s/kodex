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
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) changeExecution(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	switch input.Kind {
	case command.ClaimExecution:
		return repository.claimExecution(ctx, tx, scope, input)
	case command.RenewExecution:
		return repository.renewExecution(ctx, tx, scope, input)
	case command.ReportExecutionProgress:
		return repository.reportProgress(ctx, tx, scope, input)
	case command.CompleteExecution:
		return repository.completeExecution(ctx, tx, scope, input)
	case command.DelegateExecution:
		return repository.delegateExecution(ctx, tx, scope, input)
	case command.DeliverCallback:
		return repository.deliverCallback(ctx, tx, scope, input)
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
}

func (repository *Repository) claimExecution(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.LeaseInput)
	if !ok || payload.WorkloadInstance == "" || payload.Limit < 1 || payload.Limit > 32 {
		return commandOutcome{}, errs.ErrInvalid
	}
	if _, err := tx.Exec(ctx, queryRuntimeClaimexecutionExpireStaleLeases, scope.organizationID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	rows, err := tx.Query(ctx, queryRuntimeClaimExecutionSelectClaimableAgentExecutions,
		scope.organizationID, payload.Limit, repository.roleImages.DefaultImageReference,
		repository.roleImages.DefaultImageDigest, repository.roleImages.RoleRuntimeContractRevision,
		repository.roleImages.RoleRuntimeContractSHA256)
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	defer rows.Close()
	var items []map[string]any
	var firstProjectID, firstProjectRef, firstRunRef string
	for rows.Next() {
		var nodeID, nodeRef, runID, runRef, rootRunID, projectID, projectRef string
		var sessionID, sessionRef, task, agentRef, runtimeKey, runtimeRevision string
		var provider, model, instructionRef, instructionDigest, instructions, turnRef string
		var providerAccountID, providerAccountRef, providerCredentialID, providerCredentialRef string
		var providerSecretName, providerSecretUID, providerSecretResourceVersion, providerCredentialSHA256 string
		var providerCredentialRevisionNumber int64
		var stableKey, callbackEdgeRef, turnID, agentID, roleDefinitionID, roleDefinitionRef string
		var roleImageRecipeID, roleImageRecipeRef, roleImageArtifactID, roleImageArtifactRef string
		var imageReference, imageManifestDigest, roleRuntimeContractSHA256 string
		var roleRuntimeContractRevision int64
		var attempt int32
		var capabilities, knowledge []string
		var rawInput, rawIntegrationGrants, rawDelegationTargets, rawSessionContext []byte
		if err := rows.Scan(&nodeID, &nodeRef, &runID, &runRef, &rootRunID, &projectID,
			&projectRef, &sessionID, &sessionRef, &task, &agentRef, &runtimeKey,
			&runtimeRevision, &provider, &model, &providerAccountID, &providerAccountRef,
			&providerCredentialID, &providerCredentialRef, &providerCredentialRevisionNumber,
			&providerSecretName, &providerSecretUID, &providerSecretResourceVersion,
			&providerCredentialSHA256, &instructionRef, &instructionDigest,
			&instructions, &capabilities, &knowledge, &rawInput, &attempt, &turnRef,
			&stableKey, &rawIntegrationGrants, &rawDelegationTargets, &callbackEdgeRef,
			&rawSessionContext, &turnID, &agentID, &roleDefinitionID, &roleDefinitionRef,
			&roleImageRecipeID, &roleImageRecipeRef, &roleImageArtifactID,
			&roleImageArtifactRef, &imageReference, &imageManifestDigest,
			&roleRuntimeContractRevision, &roleRuntimeContractSHA256); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		fence, err := newRef("fnc")
		if err != nil {
			return commandOutcome{}, err
		}
		fenceDigest := sha256.Sum256([]byte(fence))
		leaseRef, _ := newRef("lea")
		inputDigest := sha256.Sum256(rawInput)
		var generation int64
		if err := tx.QueryRow(ctx, queryRuntimeClaimexecutionSelectRuntimeLeasesNodeId, nodeID).Scan(&generation); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		var inputMap map[string]any
		_ = jsonUnmarshal(rawInput, &inputMap)
		var delegationTargets []map[string]string
		_ = jsonUnmarshal(rawDelegationTargets, &delegationTargets)
		var integrationGrants []map[string]string
		_ = jsonUnmarshal(rawIntegrationGrants, &integrationGrants)
		var sessionContext []map[string]string
		_ = jsonUnmarshal(rawSessionContext, &sessionContext)
		resolvedInstructionsDigest := sha256.Sum256([]byte(instructions))
		resolvedInstructionsDigestHex := hex.EncodeToString(resolvedInstructionsDigest[:])
		integrationGrantsDigest := sha256.Sum256(rawIntegrationGrants)
		integrationGrantsDigestHex := hex.EncodeToString(integrationGrantsDigest[:])
		revisionDigest := sha256.Sum256([]byte(strings.Join([]string{
			runtimeRevision, provider, model, resolvedInstructionsDigestHex,
			providerAccountRef, providerCredentialRef, providerSecretName,
			providerSecretUID, providerSecretResourceVersion, providerCredentialSHA256,
			strings.Join(capabilities, ","), strings.Join(knowledge, ","),
			integrationGrantsDigestHex, string(rawDelegationTargets), string(rawSessionContext),
			roleDefinitionRef, roleImageRecipeRef, roleImageArtifactRef, imageReference,
			imageManifestDigest, roleRuntimeContractSHA256, hex.EncodeToString(inputDigest[:]),
		}, "\x00")))
		revisionDigestHex := hex.EncodeToString(revisionDigest[:])
		revisionRef, err := newRef("rrev")
		if err != nil {
			return commandOutcome{}, err
		}
		snapshot := map[string]any{
			"runRef": runRef, "nodeRef": nodeRef, "sessionRef": sessionRef,
			"turnRef": turnRef, "attempt": attempt, "task": task,
			"agentRef": agentRef, "stableKey": stableKey, "runtimeKey": runtimeKey,
			"runtimeRevision": runtimeRevision, "runtimeProvider": provider,
			"runtimeModel": model, "instructionRef": instructionRef,
			"providerAccountRef":               providerAccountRef,
			"providerCredentialRevisionRef":    providerCredentialRef,
			"providerCredentialRevisionNumber": providerCredentialRevisionNumber,
			"providerSecretName":               providerSecretName,
			"providerSecretUID":                providerSecretUID,
			"providerSecretResourceVersion":    providerSecretResourceVersion,
			"providerCredentialSHA256":         providerCredentialSHA256,
			"instructionDigest":                instructionDigest, "instructions": instructions,
			"capabilities": capabilities, "integrationGrants": integrationGrants,
			"knowledgeArtifactRefs": knowledge, "delegationTargets": delegationTargets,
			"callbackEdgeRef": callbackEdgeRef, "sessionContext": sessionContext,
			"input": inputMap, "inputDigest": hex.EncodeToString(inputDigest[:]),
			"revisionDigest": revisionDigestHex, "runtimeRevisionRef": revisionRef,
			"runtimeRevisionVersion": generation, "roleDefinitionRef": roleDefinitionRef,
			"roleImageRecipeRef": roleImageRecipeRef, "roleImageArtifactRef": roleImageArtifactRef,
			"imageReference": imageReference, "imageManifestDigest": imageManifestDigest,
			"roleRuntimeContractRevision": roleRuntimeContractRevision,
			"roleRuntimeContractSHA256":   roleRuntimeContractSHA256,
		}
		rawSnapshot, err := json.Marshal(snapshot)
		if err != nil || len(rawSnapshot) > 256<<10 {
			return commandOutcome{}, errs.ErrConflict
		}
		var runtimeRevisionID string
		if err := tx.QueryRow(ctx, queryRuntimeClaimExecutionInsertRuntimeRevision,
			revisionRef, scope.organizationID, projectID, rootRunID, runID, nodeID,
			sessionID, turnID, agentID, roleDefinitionID, roleImageRecipeID,
			roleImageArtifactID, providerAccountID, providerCredentialID,
			generation, attempt, runtimeKey, runtimeRevision, provider, model,
			providerAccountRef, providerCredentialRef, providerCredentialRevisionNumber,
			providerSecretName, providerSecretUID, providerSecretResourceVersion,
			providerCredentialSHA256, instructionRef, resolvedInstructionsDigestHex,
			hex.EncodeToString(inputDigest[:]), capabilities, integrationGrantsDigestHex,
			imageReference, imageManifestDigest, roleRuntimeContractRevision,
			roleRuntimeContractSHA256, revisionDigestHex, rawSnapshot).Scan(&runtimeRevisionID); err != nil {
			return commandOutcome{}, errs.ErrConflict
		}
		expiresAt := time.Now().UTC().Add(30 * time.Second)
		if _, err := tx.Exec(ctx, queryRuntimeClaimexecutionInsertRuntimeLeasesRefRunIdWorkloadInstance,
			leaseRef, scope.organizationID, runID, nodeID, runtimeRevisionID,
			payload.WorkloadInstance, hex.EncodeToString(fenceDigest[:]), generation,
			hex.EncodeToString(inputDigest[:]), expiresAt); err != nil {
			return commandOutcome{}, errs.ErrConflict
		}
		if _, err := tx.Exec(ctx, queryRuntimeClaimexecutionUpdateRunNodesStateStartedAtVersion, nodeID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		event, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID,
			nodeRef, "TURN_STARTED", nodeRef, "", "", "", "i18n:RUN_TURN_STARTED", "RUNNING", "RUNNING")
		if err != nil {
			return commandOutcome{}, err
		}
		snapshot["leaseRef"], snapshot["fence"], snapshot["generation"] = leaseRef, fence, generation
		snapshot["expiresAt"], snapshot["eventRef"] = expiresAt, event.Ref
		items = append(items, snapshot)
		if firstRunRef == "" {
			firstProjectID, firstProjectRef, firstRunRef = projectID, projectRef, runRef
		}
	}
	if err := rows.Err(); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	resourceRef := firstRunRef
	if resourceRef == "" {
		resourceRef = payload.WorkloadInstance
	}
	return commandOutcome{result: command.Result{RuntimeItems: items}, projectID: firstProjectID, projectRef: firstProjectRef, resourceKind: "RUNTIME_CLAIM", resourceRef: resourceRef, summary: "Work claims materialized"}, nil
}

func jsonUnmarshal(raw []byte, target any) error { return json.Unmarshal(raw, target) }

func (repository *Repository) lease(ctx context.Context, tx pgx.Tx, scope scope, payload command.LeaseInput, lock bool) (map[string]any, error) {
	leaseQuery := queryRuntimeLeaseSelectRuntimeLeasesOrganizationIdRef
	if lock {
		leaseQuery = queryRuntimeLeaseForUpdateSelectRuntimeLeasesOrganizationIdRef
	}
	var leaseID, runID, nodeID, rootRunID, projectID, projectRef, runRef, nodeRef, storedDigest, state string
	var generation int64
	var expiresAt time.Time
	err := tx.QueryRow(ctx, leaseQuery, scope.organizationID, payload.LeaseRef).Scan(&leaseID, &runID, &nodeID, &rootRunID, &projectID, &projectRef, &runRef, &nodeRef, &storedDigest, &generation, &state, &expiresAt)
	if err != nil {
		return nil, errs.ErrNotFound
	}
	digest := sha256.Sum256([]byte(payload.Fence))
	if storedDigest != hex.EncodeToString(digest[:]) || generation != payload.Generation || state != "CLAIMED" || time.Now().After(expiresAt) {
		return nil, errs.ErrForbidden
	}
	return map[string]any{"leaseID": leaseID, "runID": runID, "nodeID": nodeID, "rootRunID": rootRunID, "projectID": projectID, "projectRef": projectRef, "runRef": runRef, "nodeRef": nodeRef, "generation": generation}, nil
}

func (repository *Repository) renewExecution(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.LeaseInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	lease, err := repository.lease(ctx, tx, scope, payload, true)
	if err != nil {
		return commandOutcome{}, err
	}
	expires := time.Now().UTC().Add(30 * time.Second)
	if _, err := tx.Exec(ctx, queryRuntimeRenewexecutionUpdateRuntimeLeasesExpiresAtUpdatedAt, lease["leaseID"], expires); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	return commandOutcome{result: command.Result{Runtime: map[string]any{"leaseRef": payload.LeaseRef, "fence": payload.Fence, "generation": payload.Generation, "expiresAt": expires}}, projectID: stringMap(lease, "projectID"), projectRef: stringMap(lease, "projectRef"), resourceKind: "RUNTIME_LEASE", resourceRef: payload.LeaseRef, summary: "Runtime lease renewed"}, nil
}

func (repository *Repository) reportProgress(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.LeaseInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	lease, err := repository.lease(ctx, tx, scope, payload, true)
	if err != nil {
		return commandOutcome{}, err
	}
	progress := truncate(payload.Progress, 2000)
	if _, err := tx.Exec(ctx, queryRuntimeReportprogressUpdateRunNodesProgressSummaryVersion, lease["nodeID"], progress); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	event, err := repository.emitRunEvent(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID"), stringMap(lease, "nodeRef"), "TURN_PROGRESS", stringMap(lease, "nodeRef"), "", "", "", progress, "RUNNING", "RUNNING")
	if err != nil {
		return commandOutcome{}, err
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, scope, stringMap(lease, "runRef"))
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Run: &run, Graph: &graph, Event: &event}, projectID: stringMap(lease, "projectID"), projectRef: stringMap(lease, "projectRef"), resourceKind: "RUN_NODE", resourceRef: stringMap(lease, "nodeRef"), summary: "Runtime progress recorded"}, nil
}

func stringMap(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func (repository *Repository) completeExecution(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.CompleteExecutionInput)
	if !ok || payload.Success && payload.SafeErrorCode != "" || !payload.Success && !runtimeSafeErrorCode(payload.SafeErrorCode) {
		return commandOutcome{}, errs.ErrInvalid
	}
	lease, err := repository.lease(ctx, tx, scope, command.LeaseInput{LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation}, true)
	if err != nil {
		return commandOutcome{}, err
	}
	nodeState, runState := "SUCCEEDED", "RUNNING"
	if !payload.Success {
		nodeState, runState = "FAILED", "FAILED"
	}
	if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateRuntimeLeasesStateUpdatedAt, lease["leaseID"]); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var humanGateAfter bool
	var turnID, sessionID, targetType string
	if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionUpdateRunNodesStateProgressSummarySafeErrorCode, lease["nodeID"], nodeState, truncate(payload.ResultSummary, 2000), truncate(payload.SafeErrorCode, 100), "").Scan(&humanGateAfter, &turnID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if turnID != "" {
		_, _ = tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateSessionTurnsStateCompletedAt, turnID, map[bool]string{true: "COMPLETED", false: "FAILED"}[payload.Success])
	}
	if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionSelectRunsId, lease["runID"]).Scan(&sessionID, &targetType); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	artifactRefs := []string{}
	var artifactBytes int64
	for _, artifact := range payload.Artifacts {
		projectID := stringMap(lease, "projectID")
		if projectID == "" || len(payload.Artifacts) > 16 || artifact.FileName == "" || safeFileName(artifact.FileName) != artifact.FileName || artifact.SizeBytes != int64(len(artifact.Content)) || artifact.SizeBytes < 0 || artifact.SizeBytes > 1<<20 {
			return commandOutcome{}, errs.ErrInvalid
		}
		artifactBytes += artifact.SizeBytes
		if artifactBytes > maximumArtifactBytes {
			return commandOutcome{}, errs.ErrInvalid
		}
		digest := sha256.Sum256(artifact.Content)
		digestHex := hex.EncodeToString(digest[:])
		if !strings.EqualFold(strings.TrimSpace(artifact.SHA256), digestHex) {
			return commandOutcome{}, errs.ErrInvalid
		}
		scanState, previewState := scanArtifactBody(artifact.MediaType, artifact.Content)
		if scanState != "CLEAN" || previewState != "AVAILABLE" {
			return commandOutcome{}, errs.ErrInvalid
		}
		ref, _ := newRef("art")
		receiptRef, _ := newRef("obj")
		var artifactID string
		if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionInsertArtifactsRefProjectIdNodeId, ref, scope.organizationID, projectID, lease["runID"], lease["nodeID"], artifact.FileName, artifact.MediaType, artifact.SizeBytes, "sha256:"+digestHex, receiptRef, scope.actorID).Scan(&artifactID); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionInsertArtifactContentArtifactId, artifactID, artifact.Content); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionInsertArtifactBindingsArtifactIdTargetRef, artifactID, stringMap(lease, "runRef"), scope.actorID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, stringMap(lease, "rootRunID"), ref, "ARTIFACT_AVAILABLE", stringMap(lease, "nodeRef"), "", "", ref, "i18n:RESULT_ARTIFACT_AVAILABLE", runState, nodeState); err != nil {
			return commandOutcome{}, err
		}
		artifactRefs = append(artifactRefs, ref)
	}
	if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateCurrentRunOutcome, lease["runID"], map[bool]string{true: "SUCCEEDED", false: "FAILED"}[payload.Success], truncate(payload.ResultSummary, 4000), truncate(payload.SafeErrorCode, 100), ""); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if payload.Success {
		var callbackEdgeID, callbackEdgeRef, parentNodeID string
		err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionSelectRunEdgesRootRunIdSourceNodeIdType, lease["rootRunID"], lease["nodeID"]).Scan(&callbackEdgeID, &callbackEdgeRef, &parentNodeID)
		if err == nil {
			tag, insertErr := tx.Exec(ctx, queryRuntimeCompleteexecutionInsertCallbackReceiptsChildRunId, lease["runID"], callbackEdgeID)
			if insertErr != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
			if tag.RowsAffected() == 1 {
				if _, updateErr := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateRunNodesCallbackSummaryVersion, parentNodeID, truncate(payload.ResultSummary, 2000)); updateErr != nil {
					return commandOutcome{}, errs.ErrUnavailable
				}
				if _, eventErr := repository.emitRunEvent(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID"), stringMap(lease, "runRef"), "CALLBACK_DELIVERED", "", callbackEdgeRef, "", "", "i18n:CHILD_AGENT_RESULT_DELIVERED", "RUNNING", ""); eventErr != nil {
					return commandOutcome{}, eventErr
				}
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	if targetType == "SYSTEM_ASSISTANT" {
		turnRef, _ := newRef("trn")
		var next int64
		if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionSelectSessionsId, sessionID).Scan(&next); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionInsertSessionTurnsRefSessionIdTurnNumber, turnRef, scope.organizationID, sessionID, lease["runID"], next, nonEmptyResult(payload), artifactRefs, map[bool]string{true: "COMPLETED", false: "FAILED"}[payload.Success]); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		_, _ = tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateSessionsNextTurnNumberVersionUpdatedAt, sessionID)
		_, _ = tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateAssistantConversationsVersionUpdatedAt, sessionID)
	}
	if payload.Success && humanGateAfter {
		gateNodeRef, _ := newRef("nod")
		var gateNodeID string
		if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionInsertRunNodesRefRootRunIdParentNodeId, gateNodeRef, scope.organizationID, lease["rootRunID"], lease["runID"], lease["nodeID"]).Scan(&gateNodeID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		edgeRef, _ := newRef("edg")
		_, _ = tx.Exec(ctx, queryRuntimeCompleteexecutionInsertRunEdgesRefRootRunIdTargetNodeId, edgeRef, scope.organizationID, lease["rootRunID"], lease["nodeID"], gateNodeID)
		gateRef, _ := newRef("gat")
		if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionInsertOwnerGatesRefProjectIdNodeId, gateRef, scope.organizationID, lease["projectID"], lease["rootRunID"], gateNodeID, truncate(payload.ResultSummary, 1000)); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateRunsStateVersionUpdatedAt, lease["rootRunID"]); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		runState = "WAITING_HUMAN"
		if _, err := repository.emitRunEvent(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID"), gateRef, "OWNER_GATE_OPENED", gateNodeRef, edgeRef, gateRef, "", "i18n:OWNER_DECISION_REQUIRED", runState, "WAITING"); err != nil {
			return commandOutcome{}, err
		}
	}
	if !payload.Success {
		if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionFailRootRun, lease["rootRunID"], truncate(payload.ResultSummary, 4000), truncate(payload.SafeErrorCode, 100), ""); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	} else if !humanGateAfter {
		var active int
		if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionSelectRunNodesRootRunIdType, lease["rootRunID"]).Scan(&active); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if active == 0 {
			runState = "SUCCEEDED"
			if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateRunsStateResultSummaryFinishedAt, lease["rootRunID"], truncate(payload.ResultSummary, 4000)); err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
			_, _ = tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateRunNodesStateFinishedAtVersion, lease["rootRunID"])
		}
	}
	if runState == "SUCCEEDED" || runState == "FAILED" {
		var scheduleID string
		err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionUpdateScheduleOccurrencesStateLeaseRefFenceDigest, lease["rootRunID"], map[bool]string{true: "COMPLETED", false: "FAILED"}[runState == "SUCCEEDED"]).Scan(&scheduleID)
		if err == nil {
			if _, updateErr := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateSchedulesLastRunAtVersionUpdatedAt, scheduleID); updateErr != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	event, err := repository.emitRunEvent(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID"), stringMap(lease, "nodeRef"), "TURN_COMPLETED", stringMap(lease, "nodeRef"), "", "", "", nonEmptyResult(payload), runState, nodeState)
	if err != nil {
		return commandOutcome{}, err
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, scope, stringMap(lease, "runRef"))
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Run: &run, Graph: &graph, Event: &event, CreatedRefs: artifactRefs}, projectID: stringMap(lease, "projectID"), projectRef: stringMap(lease, "projectRef"), resourceKind: "RUN_NODE", resourceRef: stringMap(lease, "nodeRef"), summary: "Runtime execution completed"}, nil
}

func nonEmptyResult(payload command.CompleteExecutionInput) string {
	if text := strings.TrimSpace(payload.ResultSummary); text != "" {
		return truncate(text, 2000)
	}
	if payload.Success {
		return "RUN_COMPLETED"
	}
	return payload.SafeErrorCode
}

func runtimeSafeErrorCode(code string) bool {
	switch code {
	case "PROVIDER_AUTH_UNAVAILABLE", "PROVIDER_AUTH_REJECTED", "PROVIDER_UNAVAILABLE", "PROVIDER_RATE_LIMITED", "PROVIDER_REQUEST_REJECTED", "PROVIDER_RESPONSE_INVALID", "PROVIDER_EMPTY_RESULT", "PROVIDER_TOOL_INVALID", "PROVIDER_TOOL_LIMIT", "RUNTIME_PROFILE_UNSUPPORTED", "RUNTIME_INPUT_INVALID", "RUNTIME_INPUT_TOO_LARGE", "RUNTIME_UNAVAILABLE", "RUNTIME_LIMIT_EXCEEDED":
		return true
	default:
		return false
	}
}

func (repository *Repository) delegateExecution(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.DelegateInput)
	if !ok || payload.TargetAgentRef == "" || strings.TrimSpace(payload.Task) == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	lease, err := repository.lease(ctx, tx, scope, command.LeaseInput{LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation}, true)
	if err != nil {
		return commandOutcome{}, err
	}
	var allowed bool
	if err := tx.QueryRow(ctx, queryRuntimeDelegateexecutionSelectRunNodesId, lease["nodeID"]).Scan(&allowed); err != nil || !allowed {
		return commandOutcome{}, errs.ErrForbidden
	}
	var agentID, agentName, role string
	if err := tx.QueryRow(ctx, queryRuntimeDelegateexecutionSelectAgentsOrganizationIdProjectIdRef, scope.organizationID, lease["projectID"], payload.TargetAgentRef).Scan(&agentID, &agentName, &role); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	childRef, _ := newRef("run")
	var sessionID, initiatorID, parentRunID string
	if err := tx.QueryRow(ctx, queryRuntimeDelegateexecutionSelectRunsId, lease["runID"]).Scan(&sessionID, &initiatorID, &parentRunID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var childID string
	if err := tx.QueryRow(ctx, queryRuntimeDelegateexecutionInsertRunsRefProjectIdRootRunId, childRef, scope.organizationID, lease["projectID"], sessionID, lease["rootRunID"], parentRunID, payload.TargetAgentRef, agentName+": "+truncate(payload.Task, 100), payload.Task, asJSON(payload.Input), initiatorID).Scan(&childID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	turnRef, _ := newRef("trn")
	var turnID string
	var turnNumber int64
	if err := tx.QueryRow(ctx, queryRuntimeDelegateexecutionSelectSessionsId, sessionID).Scan(&turnNumber); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if err := tx.QueryRow(ctx, queryRuntimeDelegateexecutionInsertSessionTurnsRefSessionIdTurnNumber, turnRef, scope.organizationID, sessionID, childID, turnNumber, stringMap(lease, "nodeRef"), payload.Task).Scan(&turnID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	_, _ = tx.Exec(ctx, queryRuntimeDelegateexecutionUpdateSessionsNextTurnNumberVersionUpdatedAt, sessionID)
	nodeRef, _ := newRef("nod")
	var nodeID string
	if err := tx.QueryRow(ctx, queryRuntimeDelegateexecutionInsertRunNodesRefRootRunIdParentNodeId, nodeRef, scope.organizationID, lease["rootRunID"], childID, lease["nodeID"], agentName, role, agentID, turnID, truncate(payload.Task, 1000)).Scan(&nodeID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	delegateEdgeRef, _ := newRef("edg")
	if _, err := tx.Exec(ctx, queryRuntimeDelegateexecutionInsertDelegationEdge, delegateEdgeRef, scope.organizationID, lease["rootRunID"], lease["nodeID"], nodeID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	callbackEdgeRef, _ := newRef("edg")
	if _, err := tx.Exec(ctx, queryRuntimeDelegateexecutionInsertCallbackEdge, callbackEdgeRef, scope.organizationID, lease["rootRunID"], nodeID, lease["nodeID"]); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	event, err := repository.emitRunEvent(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID"), childRef, "DELEGATION_CREATED", nodeRef, delegateEdgeRef, "", "", "i18n:CHILD_AGENT_STARTED", "RUNNING", "QUEUED")
	if err != nil {
		return commandOutcome{}, err
	}
	child, graph, err := repository.readRunGraphTx(ctx, tx, scope, childRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Run: &child, Graph: &graph, Event: &event, Runtime: map[string]any{"callbackEdgeRef": callbackEdgeRef}}, projectID: stringMap(lease, "projectID"), projectRef: stringMap(lease, "projectRef"), resourceKind: "RUN", resourceRef: childRef, summary: "Child run delegated"}, nil
}

func (repository *Repository) deliverCallback(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.CallbackInput)
	if !ok || payload.ChildRunRef == "" || payload.CallbackEdgeRef == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	var childID, rootRunID, projectID, projectRef, parentRunID, resultSummary, edgeID, parentNodeID string
	var childState string
	if err := tx.QueryRow(ctx, queryRuntimeDelivercallbackSelectRunsOrganizationIdRef, scope.organizationID, payload.ChildRunRef, payload.CallbackEdgeRef).Scan(&childID, &rootRunID, &projectID, &projectRef, &parentRunID, &resultSummary, &childState, &edgeID, &parentNodeID); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if childState != "SUCCEEDED" {
		return commandOutcome{}, errs.ErrConflict
	}
	tag, err := tx.Exec(ctx, queryRuntimeDelivercallbackInsertCallbackReceiptsChildRunId, childID, edgeID)
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	duplicate := tag.RowsAffected() == 0
	if !duplicate {
		if _, err := tx.Exec(ctx, queryRuntimeDelivercallbackUpdateRunNodesCallbackSummaryVersion, parentNodeID, truncate(resultSummary, 2000)); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, payload.ChildRunRef, "CALLBACK_DELIVERED", "", payload.CallbackEdgeRef, "", "", "i18n:CHILD_AGENT_RESULT_DELIVERED", "RUNNING", ""); err != nil {
			return commandOutcome{}, err
		}
	}
	parentRef := mustRunRef(ctx, tx, parentRunID)
	parent, graph, err := repository.readRunGraphTx(ctx, tx, scope, parentRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Run: &parent, Graph: &graph, Duplicate: duplicate}, projectID: projectID, projectRef: projectRef, resourceKind: "CALLBACK", resourceRef: payload.CallbackEdgeRef, summary: "Child callback processed"}, nil
}
