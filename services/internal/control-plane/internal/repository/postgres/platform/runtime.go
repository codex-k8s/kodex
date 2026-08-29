package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) ReadExecutionArtifact(ctx context.Context, principal value.Principal, leaseRef, fence string, generation int64, artifactRef string) (platformrepo.ArtifactDownload, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return platformrepo.ArtifactDownload{}, err
	}
	fenceDigest := sha256.Sum256([]byte(fence))
	item := entity.Artifact{}
	var objectKey, objectVersion, objectETag, objectDigest string
	var objectSize int64
	err = repository.pool.QueryRow(ctx, queryRuntimeReadexecutionartifactSelectArtifactContent, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"lease_ref":       leaseRef,
		"fence_digest":    hex.EncodeToString(fenceDigest[:]),
		"generation":      generation,
		"artifact_ref":    artifactRef,
	}).Scan(
		&item.Ref, &item.ProjectRef, &item.RunRef, &item.SessionRef, &item.FileName,
		&item.MediaType, &item.Digest, &item.ScanState, &item.PreviewState, &item.Source,
		&item.SizeBytes, &item.Revision, &item.Version, &item.CreatedAt,
		&objectKey, &objectVersion, &objectETag, &objectDigest, &objectSize,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return platformrepo.ArtifactDownload{}, errs.ErrNotFound
	}
	if err != nil {
		return platformrepo.ArtifactDownload{}, errs.ErrUnavailable
	}
	if objectKey == "" || objectDigest != item.Digest || objectSize != item.SizeBytes ||
		item.SizeBytes < 0 || item.SizeBytes > platformrepo.MaximumArtifactBytes {
		return platformrepo.ArtifactDownload{}, errs.ErrConflict
	}
	object, err := repository.objects.Get(ctx, objectKey, objectVersion)
	if err != nil {
		return platformrepo.ArtifactDownload{}, mapObjectStorageError(err)
	}
	if object.Digest != objectDigest || object.SizeBytes != objectSize ||
		(objectVersion != "" && object.VersionID != objectVersion) ||
		(objectETag != "" && object.ETag != objectETag) {
		_ = object.Body.Close()
		return platformrepo.ArtifactDownload{}, errs.ErrConflict
	}
	return platformrepo.ArtifactDownload{Artifact: item, Reader: object.Body}, nil
}

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
	case command.ProposeAssistantPlan:
		return repository.proposeAssistantPlan(ctx, tx, scope, input)
	case command.ProposeAssistantMetadata:
		return repository.proposeAssistantMetadata(ctx, tx, scope, input)
	case command.ProposeRunMetadata:
		return repository.proposeRunMetadata(ctx, tx, scope, input)
	case command.RecordRunToolCall:
		return repository.recordRunToolCall(ctx, tx, scope, input)
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
}

func (repository *Repository) proposeAssistantMetadata(ctx context.Context, tx pgx.Tx, machineScope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ProposeAssistantMetadataInput)
	title := strings.TrimSpace(payload.Title)
	if !ok || title == "" || len([]rune(title)) > 160 {
		return commandOutcome{}, errs.ErrInvalid
	}
	lease, err := repository.lease(ctx, tx, machineScope, command.LeaseInput{LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation}, true)
	if err != nil {
		return commandOutcome{}, err
	}
	var conversationID, conversationRef, projectID, projectRef string
	var assistantRef string
	var allowedOperations []string
	var conversationVersion int64
	actorScope := scope{correlationRef: machineScope.correlationRef}
	if err := tx.QueryRow(ctx, queryRuntimeProposeassistantplanSelectContext,
		machineScope.organizationID, lease["runID"],
	).Scan(&conversationID, &conversationRef, &conversationVersion, &projectID, &projectRef, &allowedOperations, &assistantRef,
		&actorScope.actorID, &actorScope.actorRef, &actorScope.actorName, &actorScope.role,
		&actorScope.organizationRef); err != nil {
		return commandOutcome{}, errs.ErrForbidden
	}
	_ = allowedOperations
	_ = assistantRef
	var conversation entity.AssistantConversation
	if err := tx.QueryRow(ctx, queryRuntimeProposeassistantmetadataUpdateConversation, conversationID, title).Scan(
		&conversation.Ref, &conversation.Title, &conversation.TitleSource, &conversation.TitleRevision,
		&conversation.State, &conversation.Version, &conversation.CreatedAt, &conversation.UpdatedAt,
	); err != nil {
		return commandOutcome{}, errs.ErrAlreadyResolved
	}
	conversation.ProjectRef = projectRef
	return commandOutcome{result: command.Result{Conversation: &conversation}, projectID: projectID, projectRef: projectRef,
		resourceKind: "ASSISTANT_CONVERSATION", resourceRef: conversationRef,
		summary: "i18n:ASSISTANT_CONVERSATION_TITLE_UPDATED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

func (repository *Repository) proposeRunMetadata(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ProposeRunMetadataInput)
	title, activity := strings.TrimSpace(payload.Title), strings.TrimSpace(payload.ActivitySummary)
	if !ok || (title == "" && activity == "") || len([]rune(title)) > 240 || len([]rune(activity)) > 500 {
		return commandOutcome{}, errs.ErrInvalid
	}
	lease, err := repository.lease(ctx, tx, scope, command.LeaseInput{LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation}, true)
	if err != nil {
		return commandOutcome{}, err
	}
	if _, err := tx.Exec(ctx, queryRuntimeProposerunmetadataUpdateRun, lease["runID"], title, activity); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	event, err := repository.emitRunEvent(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID"),
		stringMap(lease, "runRef"), "RUN_METADATA_UPDATED", stringMap(lease, "nodeRef"), "", "", "",
		activity, "", "")
	if err != nil {
		return commandOutcome{}, err
	}
	run, _, err := repository.readRunGraphTx(ctx, tx, scope, stringMap(lease, "runRef"))
	if err != nil {
		return commandOutcome{}, err
	}
	run.TitleSource, run.ActivitySummary = "AGENT_PROPOSED", activity
	return commandOutcome{result: command.Result{Run: &run, Event: &event}, projectID: stringMap(lease, "projectID"),
		projectRef: stringMap(lease, "projectRef"), resourceKind: "RUN", resourceRef: stringMap(lease, "runRef"),
		summary: "i18n:RUN_METADATA_UPDATED"}, nil
}

func (repository *Repository) recordRunToolCall(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.RunToolCallInput)
	if !ok || !validToolCallProjection(payload) {
		return commandOutcome{}, errs.ErrInvalid
	}
	lease, err := repository.lease(ctx, tx, scope, command.LeaseInput{LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation}, true)
	if err != nil {
		return commandOutcome{}, err
	}
	var actorRef, actorName string
	var systemAssistant, grantAllowed bool
	if err := tx.QueryRow(ctx, queryRuntimeRecordtoolcallSelectActorAndGrant, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "node_id": lease["nodeID"], "generation": payload.Generation,
		"grant_ref": payload.GrantRef, "capability_ref": payload.CapabilityRef,
	}).Scan(&actorRef, &actorName, &systemAssistant, &grantAllowed); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	} else if !grantAllowed {
		return commandOutcome{}, errs.ErrForbidden
	}
	if !toolCapabilityMatches(payload.Tool, payload.CapabilityRef, payload.GrantRef != "", systemAssistant) {
		return commandOutcome{}, errs.ErrInvalid
	}
	auditRef, err := newRef("aud")
	if err != nil {
		return commandOutcome{}, err
	}
	projectID := nullUUID(stringMap(lease, "projectID"))
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef, scope.organizationID,
		projectID, scope.actorID, "runtime.tool."+payload.Tool, "RUN_NODE", stringMap(lease, "nodeRef"),
		"i18n:RUNTIME_TOOL_CALL_RECORDED", scope.correlationRef); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	toolCall := &entity.RunToolCall{Ref: payload.CallRef, Tool: payload.Tool, SafeParameters: payload.SafeParameters,
		CapabilityRef: payload.CapabilityRef, GrantRef: payload.GrantRef, State: payload.State,
		DurationMS: payload.DurationMS, SafeResult: strings.TrimSpace(payload.SafeResult), AuditRef: auditRef}
	event, err := repository.emitRunEvent(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID"),
		payload.CallRef, "TOOL_CALL_RECORDED", stringMap(lease, "nodeRef"), "", "", "",
		"i18n:RUNTIME_TOOL_CALL_RECORDED", "", "")
	if err != nil {
		return commandOutcome{}, err
	}
	actorKind := "AGENT"
	if systemAssistant {
		actorKind = "SYSTEM_ASSISTANT"
	}
	if _, err := tx.Exec(ctx, queryRuntimeRecordtoolcallUpdateEvent, scope.organizationID, actorKind, actorRef,
		actorName, asJSON(toolCall), event.Ref); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeRecordtoolcallUpdateOutbox, scope.organizationID, asJSON(toolCall), event.Ref); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	event.Actor = entity.RunEventActor{Kind: actorKind, Ref: actorRef, Name: actorName}
	event.MessageKind, event.ToolCall = "TOOL_CALL", toolCall
	return commandOutcome{result: command.Result{Event: &event}, projectID: stringMap(lease, "projectID"),
		projectRef: stringMap(lease, "projectRef"), resourceKind: "RUN_TOOL_CALL", resourceRef: payload.CallRef,
		summary: "i18n:RUNTIME_TOOL_CALL_RECORDED"}, nil
}

func validToolCallProjection(input command.RunToolCallInput) bool {
	if len(input.CallRef) < 8 || len(input.CallRef) > 96 || len(input.Tool) < 1 || len(input.Tool) > 120 ||
		(input.State != "SUCCEEDED" && input.State != "FAILED") || input.DurationMS < 0 || input.DurationMS > 86_400_000 ||
		len([]rune(input.SafeResult)) > 2000 || input.SafeParameters == nil || len(input.SafeParameters) > 32 ||
		len(asJSON(input.SafeParameters)) > 4096 {
		return false
	}
	return !containsSensitiveToolKey(input.SafeParameters)
}

func containsSensitiveToolKey(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(key, "_", ""))
			if strings.Contains(normalized, "secret") || strings.Contains(normalized, "token") ||
				strings.Contains(normalized, "password") || strings.Contains(normalized, "credential") ||
				strings.Contains(normalized, "payload") || strings.Contains(normalized, "raw") || containsSensitiveToolKey(item) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if containsSensitiveToolKey(item) {
				return true
			}
		}
	}
	return false
}

func toolCapabilityMatches(tool, capability string, integration, systemAssistant bool) bool {
	if integration {
		return tool == "invoke_integration" && capability != ""
	}
	expected := map[string]string{
		"get_configuration_catalog":  "platform.configuration.read",
		"propose_configuration_plan": "platform.configuration.plan",
		"propose_assistant_metadata": "platform.presentation.propose",
		"propose_run_metadata":       "platform.presentation.propose",
		"delegate_agent":             "platform.run.delegate",
	}
	if (tool == "get_configuration_catalog" || tool == "propose_configuration_plan" || tool == "propose_assistant_metadata") && !systemAssistant {
		return false
	}
	return expected[tool] != "" && expected[tool] == capability
}

type claimableExecution struct {
	nodeID, nodeRef, runID, runRef, rootRunID, projectID, projectRef               string
	sessionID, sessionRef, task, agentRef, runtimeKey, runtimeRevision             string
	provider, model, providerAccountID, providerAccountRef                         string
	providerCredentialID, providerCredentialRef                                    string
	providerSecretName, providerSecretUID, providerSecretResourceVersion           string
	providerCredentialSHA256, instructionRef, instructionDigest, instructions      string
	turnRef, stableKey, callbackEdgeRef, turnID, agentID                           string
	roleDefinitionID, roleDefinitionRef, roleImageRecipeID, roleImageRecipeRef     string
	roleImageArtifactID, roleImageArtifactRef, imageReference, imageManifestDigest string
	roleRuntimeContractSHA256                                                      string
	runtimeConfigID, runtimeConfigRef, runtimeConfigDigest                         string
	providerPolicyID, providerPolicyRef, providerPolicyDigest, providerPolicyMode  string
	configOverlayID, configOverlayRef, configOverlayDigest, configOverlay          string
	environmentBindingID, environmentBindingRef, environmentBindingDigest          string
	runtimeEnvironmentID, runtimeEnvironmentRef, runtimeEnvironmentDigest          string
	codexSessionID                                                                 string
	providerCredentialRevisionNumber, generation, roleRuntimeContractRevision      int64
	runtimeConfigVersion, providerPolicyVersion, configOverlayVersion              int64
	environmentBindingVersion, runtimeEnvironmentVersion                           int64
	attempt                                                                        int32
	capabilities, knowledge                                                        []string
	rawInput, rawArtifacts, rawIntegrationGrants, rawDelegationTargets             []byte
	rawSessionContext                                                              []byte
	rawEnvironmentValues, rawSecretProjections                                     []byte
}

func (repository *Repository) claimExecution(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.LeaseInput)
	if !ok || payload.WorkloadInstance == "" || payload.Limit < 1 || payload.Limit > 32 {
		return commandOutcome{}, errs.ErrInvalid
	}
	if _, err := tx.Exec(ctx, queryRuntimeClaimexecutionExpireStaleLeases, scope.organizationID); err != nil {
		return commandOutcome{}, fmt.Errorf("expire stale runtime leases: %w", errs.ErrUnavailable)
	}
	rows, err := tx.Query(ctx, queryRuntimeClaimExecutionSelectClaimableAgentExecutions,
		scope.organizationID, payload.Limit, repository.roleImages.DefaultImageReference,
		repository.roleImages.DefaultImageDigest, repository.roleImages.RoleRuntimeContractRevision,
		repository.roleImages.RoleRuntimeContractSHA256)
	if err != nil {
		return commandOutcome{}, fmt.Errorf("select claimable executions: %v: %w", err, errs.ErrUnavailable)
	}
	defer rows.Close()
	claimable := make([]claimableExecution, 0, payload.Limit)
	for rows.Next() {
		candidate := claimableExecution{}
		if err := rows.Scan(&candidate.nodeID, &candidate.nodeRef, &candidate.runID, &candidate.runRef,
			&candidate.rootRunID, &candidate.projectID, &candidate.projectRef, &candidate.sessionID,
			&candidate.sessionRef, &candidate.task, &candidate.agentRef, &candidate.runtimeKey,
			&candidate.runtimeRevision, &candidate.provider, &candidate.model, &candidate.providerAccountID,
			&candidate.providerAccountRef, &candidate.providerCredentialID, &candidate.providerCredentialRef,
			&candidate.providerCredentialRevisionNumber, &candidate.providerSecretName,
			&candidate.providerSecretUID, &candidate.providerSecretResourceVersion,
			&candidate.providerCredentialSHA256, &candidate.instructionRef, &candidate.instructionDigest,
			&candidate.instructions, &candidate.capabilities, &candidate.knowledge, &candidate.rawInput,
			&candidate.rawArtifacts,
			&candidate.attempt, &candidate.generation, &candidate.turnRef, &candidate.stableKey,
			&candidate.rawIntegrationGrants, &candidate.rawDelegationTargets, &candidate.callbackEdgeRef,
			&candidate.rawSessionContext, &candidate.turnID, &candidate.agentID,
			&candidate.roleDefinitionID, &candidate.roleDefinitionRef, &candidate.roleImageRecipeID,
			&candidate.roleImageRecipeRef, &candidate.roleImageArtifactID, &candidate.roleImageArtifactRef,
			&candidate.imageReference, &candidate.imageManifestDigest,
			&candidate.roleRuntimeContractRevision, &candidate.roleRuntimeContractSHA256,
			&candidate.runtimeConfigID, &candidate.runtimeConfigRef, &candidate.runtimeConfigVersion, &candidate.runtimeConfigDigest,
			&candidate.providerPolicyID, &candidate.providerPolicyRef, &candidate.providerPolicyVersion, &candidate.providerPolicyDigest, &candidate.providerPolicyMode,
			&candidate.configOverlayID, &candidate.configOverlayRef, &candidate.configOverlayVersion, &candidate.configOverlayDigest, &candidate.configOverlay,
			&candidate.environmentBindingID, &candidate.environmentBindingRef, &candidate.environmentBindingVersion, &candidate.environmentBindingDigest,
			&candidate.runtimeEnvironmentID, &candidate.runtimeEnvironmentRef, &candidate.runtimeEnvironmentVersion, &candidate.runtimeEnvironmentDigest,
			&candidate.rawEnvironmentValues, &candidate.rawSecretProjections,
			&candidate.codexSessionID); err != nil {
			return commandOutcome{}, fmt.Errorf("scan claimable execution: %v: %w", err, errs.ErrUnavailable)
		}
		claimable = append(claimable, candidate)
	}
	if err := rows.Err(); err != nil {
		return commandOutcome{}, fmt.Errorf("iterate claimable executions: %v: %w", err, errs.ErrUnavailable)
	}
	rows.Close()

	var items []map[string]any
	var firstProjectID, firstProjectRef, firstRunRef string
	for _, candidate := range claimable {
		nodeID, nodeRef, runID, runRef := candidate.nodeID, candidate.nodeRef, candidate.runID, candidate.runRef
		rootRunID, projectID, projectRef := candidate.rootRunID, candidate.projectID, candidate.projectRef
		sessionID, sessionRef, task, agentRef := candidate.sessionID, candidate.sessionRef, candidate.task, candidate.agentRef
		runtimeKey, runtimeRevision, provider, model := candidate.runtimeKey, candidate.runtimeRevision, candidate.provider, candidate.model
		providerAccountID, providerAccountRef := candidate.providerAccountID, candidate.providerAccountRef
		providerCredentialID, providerCredentialRef := candidate.providerCredentialID, candidate.providerCredentialRef
		providerCredentialRevisionNumber := candidate.providerCredentialRevisionNumber
		providerSecretName, providerSecretUID := candidate.providerSecretName, candidate.providerSecretUID
		providerSecretResourceVersion := candidate.providerSecretResourceVersion
		providerCredentialSHA256 := candidate.providerCredentialSHA256
		instructionRef, instructionDigest, instructions := candidate.instructionRef, candidate.instructionDigest, candidate.instructions
		capabilities, knowledge, rawInput := candidate.capabilities, candidate.knowledge, candidate.rawInput
		rawArtifacts := candidate.rawArtifacts
		attempt, generation, turnRef, stableKey := candidate.attempt, candidate.generation, candidate.turnRef, candidate.stableKey
		rawIntegrationGrants, rawDelegationTargets := candidate.rawIntegrationGrants, candidate.rawDelegationTargets
		callbackEdgeRef, rawSessionContext := candidate.callbackEdgeRef, candidate.rawSessionContext
		turnID, agentID := candidate.turnID, candidate.agentID
		roleDefinitionID, roleDefinitionRef := candidate.roleDefinitionID, candidate.roleDefinitionRef
		roleImageRecipeID, roleImageRecipeRef := candidate.roleImageRecipeID, candidate.roleImageRecipeRef
		roleImageArtifactID, roleImageArtifactRef := candidate.roleImageArtifactID, candidate.roleImageArtifactRef
		imageReference, imageManifestDigest := candidate.imageReference, candidate.imageManifestDigest
		roleRuntimeContractRevision := candidate.roleRuntimeContractRevision
		roleRuntimeContractSHA256 := candidate.roleRuntimeContractSHA256
		runtimeConfigID, runtimeConfigRef, runtimeConfigVersion, runtimeConfigDigest := candidate.runtimeConfigID, candidate.runtimeConfigRef, candidate.runtimeConfigVersion, candidate.runtimeConfigDigest
		providerPolicyID, providerPolicyRef, providerPolicyVersion, providerPolicyDigest := candidate.providerPolicyID, candidate.providerPolicyRef, candidate.providerPolicyVersion, candidate.providerPolicyDigest
		configOverlayID, configOverlayRef, configOverlayVersion, configOverlayDigest, configOverlay := candidate.configOverlayID, candidate.configOverlayRef, candidate.configOverlayVersion, candidate.configOverlayDigest, candidate.configOverlay
		environmentBindingID, environmentBindingRef, environmentBindingVersion, environmentBindingDigest := candidate.environmentBindingID, candidate.environmentBindingRef, candidate.environmentBindingVersion, candidate.environmentBindingDigest
		runtimeEnvironmentID, runtimeEnvironmentRef, runtimeEnvironmentVersion, runtimeEnvironmentDigest := candidate.runtimeEnvironmentID, candidate.runtimeEnvironmentRef, candidate.runtimeEnvironmentVersion, candidate.runtimeEnvironmentDigest
		codexSessionID := candidate.codexSessionID
		fence, err := newRef("fnc")
		if err != nil {
			return commandOutcome{}, err
		}
		fenceDigest := sha256.Sum256([]byte(fence))
		leaseRef, _ := newRef("lea")
		inputDigest := sha256.Sum256(rawInput)
		var inputMap map[string]any
		_ = jsonUnmarshal(rawInput, &inputMap)
		var delegationTargets []map[string]string
		_ = jsonUnmarshal(rawDelegationTargets, &delegationTargets)
		var integrationGrants []map[string]string
		_ = jsonUnmarshal(rawIntegrationGrants, &integrationGrants)
		var artifacts []map[string]any
		_ = jsonUnmarshal(rawArtifacts, &artifacts)
		var sessionContext []map[string]string
		_ = jsonUnmarshal(rawSessionContext, &sessionContext)
		var environmentValues []runtimecontract.RuntimeEnvironmentValue
		var secretProjections []runtimecontract.RuntimeSecretProjection
		if err := decodeStoredRuntimeEnvironment(candidate.rawEnvironmentValues, candidate.rawSecretProjections, &environmentValues, &secretProjections); err != nil {
			return commandOutcome{}, errs.ErrConflict
		}
		canonicalOverlay, verifiedOverlayDigest, err := runtimecontract.CanonicalConfigOverlay(configOverlay)
		if err != nil || canonicalOverlay != configOverlay || verifiedOverlayDigest != configOverlayDigest {
			return commandOutcome{}, errs.ErrConflict
		}
		verifiedEnvironmentDigest, err := runtimecontract.RuntimeEnvironmentDigest(environmentValues, secretProjections)
		if err != nil || verifiedEnvironmentDigest != runtimeEnvironmentDigest {
			return commandOutcome{}, errs.ErrConflict
		}
		var rawAssistantContext []byte
		if err := tx.QueryRow(ctx, queryRuntimeClaimexecutionSelectAssistantContext, scope.organizationID, sessionID).Scan(&rawAssistantContext); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		var assistantContext map[string]any
		_ = jsonUnmarshal(rawAssistantContext, &assistantContext)
		resolvedInstructionsDigest := sha256.Sum256([]byte(instructions))
		resolvedInstructionsDigestHex := hex.EncodeToString(resolvedInstructionsDigest[:])
		integrationGrantsDigest := sha256.Sum256(rawIntegrationGrants)
		integrationGrantsDigestHex := hex.EncodeToString(integrationGrantsDigest[:])
		revisionDigest := sha256.Sum256([]byte(strings.Join([]string{
			runtimeRevision, provider, model, resolvedInstructionsDigestHex,
			providerAccountRef, providerCredentialRef, providerSecretName,
			providerSecretUID, providerSecretResourceVersion, providerCredentialSHA256,
			strings.Join(capabilities, ","), strings.Join(knowledge, ","),
			integrationGrantsDigestHex, string(rawArtifacts), string(rawDelegationTargets), string(rawSessionContext), string(rawAssistantContext),
			roleDefinitionRef, roleImageRecipeRef, roleImageArtifactRef, imageReference,
			imageManifestDigest, roleRuntimeContractSHA256, hex.EncodeToString(inputDigest[:]),
			runtimeConfigRef, strconv.FormatInt(runtimeConfigVersion, 10), runtimeConfigDigest,
			providerPolicyRef, strconv.FormatInt(providerPolicyVersion, 10), providerPolicyDigest,
			configOverlayRef, strconv.FormatInt(configOverlayVersion, 10), configOverlayDigest,
			runtimeEnvironmentRef, strconv.FormatInt(runtimeEnvironmentVersion, 10), runtimeEnvironmentDigest,
			environmentBindingRef, strconv.FormatInt(environmentBindingVersion, 10), environmentBindingDigest,
			codexSessionID,
		}, "\x00")))
		revisionDigestHex := hex.EncodeToString(revisionDigest[:])
		revisionRef, err := newRef("rrev")
		if err != nil {
			return commandOutcome{}, err
		}
		snapshot := map[string]any{
			"runRef": runRef, "projectRef": projectRef, "nodeRef": nodeRef, "sessionRef": sessionRef,
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
			"knowledgeArtifactRefs": knowledge, "artifacts": artifacts, "delegationTargets": delegationTargets,
			"callbackEdgeRef": callbackEdgeRef, "sessionContext": sessionContext,
			"input": inputMap, "inputDigest": hex.EncodeToString(inputDigest[:]),
			"revisionDigest": revisionDigestHex, "runtimeRevisionRef": revisionRef,
			"runtimeRevisionVersion": generation, "roleDefinitionRef": roleDefinitionRef,
			"roleImageRecipeRef": roleImageRecipeRef, "roleImageArtifactRef": roleImageArtifactRef,
			"imageReference": imageReference, "imageManifestDigest": imageManifestDigest,
			"roleRuntimeContractRevision": roleRuntimeContractRevision,
			"roleRuntimeContractSHA256":   roleRuntimeContractSHA256,
			"runtimeConfigRef":            runtimeConfigRef, "runtimeConfigVersion": runtimeConfigVersion, "runtimeConfigDigest": runtimeConfigDigest,
			"providerPolicyRef": providerPolicyRef, "providerPolicyVersion": providerPolicyVersion, "providerPolicyDigest": providerPolicyDigest,
			"providerPolicyMode": candidate.providerPolicyMode,
			"configOverlayRef":   configOverlayRef, "configOverlayVersion": configOverlayVersion, "configOverlayDigest": configOverlayDigest, "configOverlay": configOverlay,
			"runtimeEnvironmentRef": runtimeEnvironmentRef, "runtimeEnvironmentVersion": runtimeEnvironmentVersion, "runtimeEnvironmentDigest": runtimeEnvironmentDigest,
			"environmentBindingRef": environmentBindingRef, "environmentBindingVersion": environmentBindingVersion, "environmentBindingDigest": environmentBindingDigest,
			"environmentValues": environmentValues, "secretProjections": secretProjections,
			"codexSessionID": codexSessionID,
		}
		if len(assistantContext) != 0 {
			snapshot["assistantContext"] = assistantContext
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
			roleRuntimeContractSHA256, runtimeConfigID, providerPolicyID, configOverlayID,
			runtimeEnvironmentID, environmentBindingID, runtimeConfigRef, runtimeConfigVersion, runtimeConfigDigest,
			providerPolicyRef, providerPolicyVersion, providerPolicyDigest, configOverlayRef, configOverlayVersion,
			configOverlayDigest, runtimeEnvironmentRef, runtimeEnvironmentVersion, runtimeEnvironmentDigest,
			environmentBindingRef, environmentBindingVersion, environmentBindingDigest,
			revisionDigestHex, rawSnapshot).Scan(&runtimeRevisionID); err != nil {
			return commandOutcome{}, fmt.Errorf("insert runtime revision: %w", errs.ErrConflict)
		}
		expiresAt := time.Now().UTC().Add(30 * time.Second)
		if _, err := tx.Exec(ctx, queryRuntimeClaimexecutionInsertRuntimeLeasesRefRunIdWorkloadInstance,
			leaseRef, scope.organizationID, runID, nodeID, runtimeRevisionID,
			payload.WorkloadInstance, hex.EncodeToString(fenceDigest[:]), generation,
			hex.EncodeToString(inputDigest[:]), expiresAt); err != nil {
			return commandOutcome{}, fmt.Errorf("insert runtime lease: %w", errs.ErrConflict)
		}
		if _, err := tx.Exec(ctx, queryRuntimeClaimexecutionUpdateRunNodesStateStartedAtVersion, nodeID); err != nil {
			return commandOutcome{}, fmt.Errorf("start claimed execution node: %w", errs.ErrUnavailable)
		}
		event, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID,
			nodeRef, "TURN_STARTED", nodeRef, "", "", "", "i18n:RUN_TURN_STARTED", "RUNNING", "RUNNING")
		if err != nil {
			return commandOutcome{}, fmt.Errorf("emit claimed execution event: %w", err)
		}
		snapshot["leaseRef"], snapshot["fence"], snapshot["generation"] = leaseRef, fence, generation
		snapshot["expiresAt"], snapshot["eventRef"] = expiresAt, event.Ref
		items = append(items, snapshot)
		if firstRunRef == "" {
			firstProjectID, firstProjectRef, firstRunRef = projectID, projectRef, runRef
		}
	}
	return commandOutcome{result: command.Result{RuntimeItems: items}, projectID: firstProjectID, projectRef: firstProjectRef, resourceKind: "RUNTIME_CLAIM", resourceRef: firstRunRef, summary: "i18n:RUNTIME_WORK_CLAIMS_MATERIALIZED"}, nil
}

func jsonUnmarshal(raw []byte, target any) error { return json.Unmarshal(raw, target) }

func decodeStoredRuntimeEnvironment(rawValues, rawSecrets []byte, values *[]runtimecontract.RuntimeEnvironmentValue, secrets *[]runtimecontract.RuntimeSecretProjection) error {
	decodedValues, decodedSecrets, err := runtimecontract.DecodeRuntimeEnvironment(rawValues, rawSecrets)
	if err != nil {
		return err
	}
	*values, *secrets = decodedValues, decodedSecrets
	return nil
}

func (repository *Repository) lease(ctx context.Context, tx pgx.Tx, scope scope, payload command.LeaseInput, lock bool) (map[string]any, error) {
	leaseQuery := queryRuntimeLeaseSelectRuntimeLeasesOrganizationIdRef
	if lock {
		leaseQuery = queryRuntimeLeaseForUpdateSelectRuntimeLeasesOrganizationIdRef
	}
	var leaseID, runID, nodeID, rootRunID, projectID, projectRef, runRef, nodeRef, storedDigest, state, turnRef, runtimeRevisionID string
	var generation int64
	var expiresAt time.Time
	err := tx.QueryRow(ctx, leaseQuery, scope.organizationID, payload.LeaseRef).Scan(&leaseID, &runID, &nodeID, &rootRunID, &projectID, &projectRef, &runRef, &nodeRef, &storedDigest, &generation, &state, &expiresAt, &turnRef, &runtimeRevisionID)
	if err != nil {
		return nil, errs.ErrNotFound
	}
	digest := sha256.Sum256([]byte(payload.Fence))
	if storedDigest != hex.EncodeToString(digest[:]) || generation != payload.Generation || state != "CLAIMED" || time.Now().After(expiresAt) {
		return nil, errs.ErrForbidden
	}
	return map[string]any{"leaseID": leaseID, "runID": runID, "nodeID": nodeID, "rootRunID": rootRunID, "projectID": projectID, "projectRef": projectRef, "runRef": runRef, "nodeRef": nodeRef, "turnRef": turnRef, "runtimeRevisionID": runtimeRevisionID, "generation": generation}, nil
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
	return commandOutcome{result: command.Result{Runtime: map[string]any{"leaseRef": payload.LeaseRef, "fence": payload.Fence, "generation": payload.Generation, "expiresAt": expires}}, projectID: stringMap(lease, "projectID"), projectRef: stringMap(lease, "projectRef"), resourceKind: "RUNTIME_LEASE", resourceRef: payload.LeaseRef, summary: "i18n:RUNTIME_LEASE_RENEWED"}, nil
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
	return commandOutcome{result: command.Result{Run: &run, Graph: &graph, Event: &event}, projectID: stringMap(lease, "projectID"), projectRef: stringMap(lease, "projectRef"), resourceKind: "RUN_NODE", resourceRef: stringMap(lease, "nodeRef"), summary: "i18n:RUNTIME_PROGRESS_RECORDED"}, nil
}

func stringMap(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func (repository *Repository) completeExecution(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.CompleteExecutionInput)
	if !ok || !payload.Usage.Valid() || payload.Success && payload.SafeErrorCode != "" || !payload.Success && !runtimeSafeErrorCode(payload.SafeErrorCode) {
		return commandOutcome{}, errs.ErrInvalid
	}
	hasArchiveBinding := payload.CodexSessionID != "" || payload.ArchiveRelativePath != "" || payload.ArchiveSHA256 != "" || payload.ArchiveSizeBytes != 0
	if hasArchiveBinding && (runtimecontract.ValidateCodexArchiveIdentity(payload.CodexSessionID, payload.ArchiveRelativePath) != nil ||
		len(payload.ArchiveSHA256) != 64 ||
		payload.ArchiveSizeBytes < 1 || payload.ArchiveSizeBytes > runtimecontract.MaximumSessionSourceBytes) {
		return commandOutcome{}, errs.ErrInvalid
	}
	lease, err := repository.lease(ctx, tx, scope, command.LeaseInput{LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation}, true)
	if err != nil {
		return commandOutcome{}, err
	}
	var lockedRootID string
	if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionLockRootRun, lease["rootRunID"]).Scan(&lockedRootID); err != nil || lockedRootID != stringMap(lease, "rootRunID") {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if len(payload.Artifacts) > 0 {
		var allowed bool
		if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionSelectAgentCapability, scope.organizationID, runtimecontract.ArtifactCapability, lease["nodeID"]).Scan(&allowed); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if !allowed {
			return commandOutcome{}, errs.ErrForbidden
		}
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
		if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateSessionTurnsStateCompletedAt, turnID, map[bool]string{true: "COMPLETED", false: "FAILED"}[payload.Success]); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionSelectRunsId, lease["runID"]).Scan(&sessionID, &targetType); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if hasArchiveBinding && targetType != "SYSTEM_ASSISTANT" {
		if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpsertSessionStorage, pgx.StrictNamedArgs{
			"organization_id":      scope.organizationID,
			"session_id":           sessionID,
			"runtime_revision_id":  lease["runtimeRevisionID"],
			"codex_session_id":     payload.CodexSessionID,
			"source_relative_path": payload.ArchiveRelativePath,
			"source_sha256":        payload.ArchiveSHA256,
			"source_size_bytes":    payload.ArchiveSizeBytes,
			"retention_seconds":    int64((30 * 24 * time.Hour) / time.Second),
		}); err != nil {
			return commandOutcome{}, fmt.Errorf("record session storage binding: %w", errs.ErrUnavailable)
		}
	}
	artifactRefs := []string{}
	var artifactBytes int64
	for _, artifact := range payload.Artifacts {
		projectID := stringMap(lease, "projectID")
		prepared, preparedErr := preparedArtifact(artifact)
		if preparedErr != nil || projectID == "" || len(payload.Artifacts) > 16 ||
			artifact.FileName == "" || safeFileName(artifact.FileName) != artifact.FileName ||
			artifact.SizeBytes < 0 || artifact.SizeBytes > 1<<20 {
			return commandOutcome{}, errs.ErrInvalid
		}
		artifactBytes += artifact.SizeBytes
		if artifactBytes > maximumArtifactBytes {
			return commandOutcome{}, errs.ErrInvalid
		}
		if prepared.ObjectKey != artifactObjectKey(scope.organizationRef, stringMap(lease, "projectRef"), prepared.Ref, prepared.Digest) {
			return commandOutcome{}, errs.ErrInvalid
		}
		receiptRef, _ := newRef("obj")
		var artifactID string
		if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionInsertArtifactsRefProjectIdNodeId,
			prepared.Ref, scope.organizationID, projectID, lease["runID"], lease["nodeID"],
			artifact.FileName, prepared.MediaType, artifact.SizeBytes, prepared.Digest,
			prepared.ScanState, receiptRef, prepared.PreviewState, scope.actorID).Scan(&artifactID); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionInsertArtifactContentArtifactId,
			artifactID, prepared.ObjectKey, prepared.ObjectVersion, prepared.ObjectETag,
			prepared.Digest, prepared.SizeBytes); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionInsertArtifactBindingsArtifactIdTargetRef, artifactID, stringMap(lease, "runRef"), scope.actorID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, stringMap(lease, "rootRunID"), prepared.Ref, "ARTIFACT_AVAILABLE", stringMap(lease, "nodeRef"), "", "", prepared.Ref, "i18n:RESULT_ARTIFACT_AVAILABLE", runState, nodeState); err != nil {
			return commandOutcome{}, err
		}
		artifactRefs = append(artifactRefs, prepared.Ref)
	}
	usage, err := json.Marshal(payload.Usage)
	if err != nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	turnRef := stringMap(lease, "turnRef")
	if turnRef == "" {
		return commandOutcome{}, errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateRunUsage, lease["runID"], turnRef, usage); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateRootUsage, lease["rootRunID"]); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateCurrentRunOutcome, lease["runID"], map[bool]string{true: "SUCCEEDED", false: "FAILED"}[payload.Success], truncate(payload.ResultSummary, 4000), truncate(payload.SafeErrorCode, 100), ""); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if payload.Success {
		var callbackEdgeID, callbackEdgeRef, parentNodeID, parentNodeRef, parentRunID string
		err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionSelectRunEdgesRootRunIdSourceNodeIdType, lease["rootRunID"], lease["nodeID"]).Scan(&callbackEdgeID, &callbackEdgeRef, &parentNodeID, &parentNodeRef, &parentRunID)
		if err == nil {
			if _, callbackErr := repository.recordChildCallback(ctx, tx, scope, callbackRecord{
				childRunID: lease["runID"].(string), childRunRef: stringMap(lease, "runRef"),
				rootRunID: stringMap(lease, "rootRunID"), projectID: stringMap(lease, "projectID"),
				parentRunID: parentRunID, resultSummary: payload.ResultSummary, callbackEdgeID: callbackEdgeID,
				callbackEdgeRef: callbackEdgeRef, parentNodeID: parentNodeID, parentNodeRef: parentNodeRef,
			}); callbackErr != nil {
				return commandOutcome{}, callbackErr
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, continuationErr := repository.scheduleCallbackContinuation(ctx, tx, scope, stringMap(lease, "nodeID"), stringMap(lease, "projectID")); continuationErr != nil {
			return commandOutcome{}, continuationErr
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
		if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateSessionsNextTurnNumberVersionUpdatedAt, sessionID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateAssistantConversationsVersionUpdatedAt, sessionID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	if payload.Success && humanGateAfter {
		gateNodeRef, _ := newRef("nod")
		var gateNodeID string
		if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionInsertRunNodesRefRootRunIdParentNodeId, gateNodeRef, scope.organizationID, lease["rootRunID"], lease["runID"], lease["nodeID"]).Scan(&gateNodeID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		edgeRef, _ := newRef("edg")
		if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionInsertRunEdgesRefRootRunIdTargetNodeId, edgeRef, scope.organizationID, lease["rootRunID"], lease["nodeID"], gateNodeID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		gateRef, _ := newRef("gat")
		var gateID string
		if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionInsertOwnerGatesRefProjectIdNodeId, pgx.StrictNamedArgs{
			"gate_ref":        gateRef,
			"organization_id": scope.organizationID,
			"project_id":      lease["projectID"],
			"root_run_id":     lease["rootRunID"],
			"node_id":         gateNodeID,
			"context_summary": truncate(payload.ResultSummary, 1000),
		}).Scan(&gateID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if err := repository.enqueueGateInteractionDeliveries(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID"), gateID); err != nil {
			return commandOutcome{}, err
		}
		if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateRunsStateVersionUpdatedAt, lease["rootRunID"]); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		runState = "WAITING_HUMAN"
		if _, err := repository.emitRunEvent(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID"), gateRef, "OWNER_GATE_OPENED", gateNodeRef, edgeRef, gateRef, "", "i18n:OWNER_DECISION_REQUIRED", runState, "WAITING"); err != nil {
			return commandOutcome{}, err
		}
	}
	terminalRootNodeRef := ""
	if !payload.Success {
		if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionFailRootRun, lease["rootRunID"], truncate(payload.ResultSummary, 4000), truncate(payload.SafeErrorCode, 100), ""); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionUpdateRunNodesStateFinishedAtVersion, lease["rootRunID"], "FAILED").Scan(&terminalRootNodeRef); err != nil && !directRootWithoutProcessNode(err, lease) {
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
			if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionUpdateRunNodesStateFinishedAtVersion, lease["rootRunID"], "SUCCEEDED").Scan(&terminalRootNodeRef); err != nil && !directRootWithoutProcessNode(err, lease) {
				return commandOutcome{}, errs.ErrUnavailable
			}
		}
	}
	if runState == "SUCCEEDED" || runState == "FAILED" {
		var scheduleID string
		err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionUpdateScheduleOccurrencesStateLeaseRefFenceDigest, lease["rootRunID"], map[bool]string{true: "COMPLETED", false: "FAILED"}[runState == "SUCCEEDED"]).Scan(&scheduleID)
		if err == nil {
			if _, updateErr := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateSchedulesLastRunAtUpdatedAt, scheduleID); updateErr != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if err := repository.enqueueTerminalInteractionDeliveries(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID")); err != nil {
			return commandOutcome{}, err
		}
	}
	event, err := repository.emitRunEvent(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID"), stringMap(lease, "nodeRef"), "TURN_COMPLETED", stringMap(lease, "nodeRef"), "", "", "", nonEmptyResult(payload), runState, nodeState)
	if err != nil {
		return commandOutcome{}, err
	}
	if terminalRootNodeRef != "" && terminalRootNodeRef != stringMap(lease, "nodeRef") {
		if _, err := repository.emitRunEvent(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID"), terminalRootNodeRef, "NODE_STATE_CHANGED", terminalRootNodeRef, "", "", "", "i18n:ROOT_PROCESS_COMPLETED", runState, map[bool]string{true: "SUCCEEDED", false: "FAILED"}[payload.Success]); err != nil {
			return commandOutcome{}, err
		}
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, scope, stringMap(lease, "runRef"))
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Run: &run, Graph: &graph, Event: &event, CreatedRefs: artifactRefs}, projectID: stringMap(lease, "projectID"), projectRef: stringMap(lease, "projectRef"), resourceKind: "RUN_NODE", resourceRef: stringMap(lease, "nodeRef"), summary: "i18n:RUNTIME_EXECUTION_COMPLETED"}, nil
}

type storedRunUsage struct {
	entity.TokenUsage
	Turns map[string]entity.TokenUsage `json:"turns,omitempty"`
}

func decodeRunUsage(raw []byte) (entity.TokenUsage, error) {
	var stored storedRunUsage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&stored) != nil || decoder.Decode(&struct{}{}) != io.EOF || !stored.TokenUsage.Valid() {
		return entity.TokenUsage{}, errs.ErrUnavailable
	}
	for ref, usage := range stored.Turns {
		if ref == "" || !usage.Valid() {
			return entity.TokenUsage{}, errs.ErrUnavailable
		}
	}
	return stored.TokenUsage, nil
}

func directRootWithoutProcessNode(err error, lease map[string]any) bool {
	return errors.Is(err, pgx.ErrNoRows) &&
		stringMap(lease, "runID") != "" &&
		stringMap(lease, "runID") == stringMap(lease, "rootRunID")
}

func nonEmptyResult(payload command.CompleteExecutionInput) string {
	if text := strings.TrimSpace(payload.ResultSummary); text != "" {
		return truncate(text, 2000)
	}
	if payload.Success {
		return "i18n:RUN_COMPLETED"
	}
	return "i18n:" + payload.SafeErrorCode
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
	if !ok || payload.TargetAgentRef == "" || strings.TrimSpace(payload.Task) == "" || len(payload.Task) > 64<<10 {
		return commandOutcome{}, errs.ErrInvalid
	}
	lease, err := repository.lease(ctx, tx, scope, command.LeaseInput{LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation}, true)
	if err != nil {
		return commandOutcome{}, err
	}
	var capabilityAllowed, relationshipAllowed bool
	var workflowInstructions, workflowStepName, plannedNodeID, plannedNodeRef, plannedEdgeRef string
	if err := tx.QueryRow(ctx, queryRuntimeDelegateexecutionSelectRunNodesId, pgx.StrictNamedArgs{
		"parent_node_id":    lease["nodeID"],
		"target_agent_ref":  payload.TargetAgentRef,
		"workflow_step_key": payload.WorkflowStepKey,
	}).Scan(&capabilityAllowed, &relationshipAllowed, &workflowInstructions, &workflowStepName, &plannedNodeID, &plannedNodeRef, &plannedEdgeRef); err != nil || !capabilityAllowed || !relationshipAllowed {
		return commandOutcome{}, errs.ErrForbidden
	}
	var agentID, agentName, role string
	if err := tx.QueryRow(ctx, queryRuntimeDelegateexecutionSelectAgentsOrganizationIdProjectIdRef, scope.organizationID, lease["projectID"], payload.TargetAgentRef).Scan(&agentID, &agentName, &role); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	childRef, _ := newRef("run")
	var initiatorID, parentRunID string
	if err := tx.QueryRow(ctx, queryRuntimeDelegateexecutionSelectRunsId, pgx.StrictNamedArgs{
		"parent_run_id":   lease["runID"],
		"organization_id": scope.organizationID,
	}).Scan(&initiatorID, &parentRunID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	providerAccountID, err := repository.selectProviderAccountForAgent(ctx, tx, scope.organizationID, payload.TargetAgentRef)
	if err != nil {
		return commandOutcome{}, err
	}
	childSessionRef, _ := newRef("ses")
	var childSessionID string
	if err := tx.QueryRow(ctx, queryRuntimeDelegateexecutionInsertChildSession, pgx.StrictNamedArgs{
		"session_ref":         childSessionRef,
		"organization_id":     scope.organizationID,
		"project_id":          lease["projectID"],
		"target_agent_ref":    payload.TargetAgentRef,
		"provider_account_id": providerAccountID,
		"created_by":          initiatorID,
	}).Scan(&childSessionID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	childTask := strings.TrimSpace(payload.Task)
	if workflowInstructions != "" {
		childTask = strings.TrimSpace(workflowInstructions) + "\n\nCoordinator assignment:\n" + childTask
	}
	childTask = truncate(childTask, 19_000)
	var childID string
	childTitle := agentName + ": " + truncate(payload.Task, 100)
	if workflowStepName != "" {
		childTitle = workflowStepName
	}
	if err := tx.QueryRow(ctx, queryRuntimeDelegateexecutionInsertRunsRefProjectIdRootRunId, pgx.StrictNamedArgs{
		"run_ref":          childRef,
		"organization_id":  scope.organizationID,
		"project_id":       lease["projectID"],
		"session_id":       childSessionID,
		"root_run_id":      lease["rootRunID"],
		"parent_run_id":    parentRunID,
		"target_agent_ref": payload.TargetAgentRef,
		"title":            childTitle,
		"task":             childTask,
		"input":            asJSON(payload.Input),
		"initiated_by":     initiatorID,
	}).Scan(&childID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	turnRef, _ := newRef("trn")
	var turnID string
	var turnNumber int64
	if err := tx.QueryRow(ctx, queryRuntimeDelegateexecutionSelectSessionsId, childSessionID).Scan(&turnNumber); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if err := tx.QueryRow(ctx, queryRuntimeDelegateexecutionInsertSessionTurnsRefSessionIdTurnNumber, turnRef, scope.organizationID, childSessionID, childID, turnNumber, stringMap(lease, "nodeRef"), childTask).Scan(&turnID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeDelegateexecutionUpdateSessionsNextTurnNumberVersionUpdatedAt, childSessionID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	nodeRef := plannedNodeRef
	if nodeRef == "" {
		nodeRef, _ = newRef("nod")
	}
	var nodeID string
	if plannedNodeID != "" {
		nodeID = plannedNodeID
		if err := tx.QueryRow(ctx, queryRuntimeDelegateexecutionMaterializePlannedNode, pgx.StrictNamedArgs{
			"node_id": plannedNodeID, "run_id": childID, "turn_id": turnID,
			"input_summary": truncate(childTask, 1000),
		}).Scan(&nodeRef); err != nil {
			return commandOutcome{}, errs.ErrConflict
		}
	} else {
		if err := tx.QueryRow(ctx, queryRuntimeDelegateexecutionInsertRunNodesRefRootRunIdParentNodeId, pgx.StrictNamedArgs{
			"node_ref":          nodeRef,
			"organization_id":   scope.organizationID,
			"root_run_id":       lease["rootRunID"],
			"run_id":            childID,
			"parent_node_id":    lease["nodeID"],
			"display_name":      agentName,
			"role":              role,
			"agent_id":          agentID,
			"turn_id":           turnID,
			"workflow_step_key": payload.WorkflowStepKey,
			"input_summary":     truncate(childTask, 1000),
		}).Scan(&nodeID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	delegateEdgeRef := plannedEdgeRef
	if delegateEdgeRef == "" {
		delegateEdgeRef, _ = newRef("edg")
		if _, err := tx.Exec(ctx, queryRuntimeDelegateexecutionInsertDelegationEdge, delegateEdgeRef, scope.organizationID, lease["rootRunID"], lease["nodeID"], nodeID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	callbackEdgeRef, _ := newRef("edg")
	if _, err := tx.Exec(ctx, queryRuntimeDelegateexecutionInsertCallbackEdge, callbackEdgeRef, scope.organizationID, lease["rootRunID"], nodeID, lease["nodeID"]); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	event, err := repository.emitRunEvent(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID"), childRef, "DELEGATION_CREATED", nodeRef, delegateEdgeRef, "", "", "i18n:CHILD_AGENT_STARTED", "RUNNING", "QUEUED")
	if err != nil {
		return commandOutcome{}, err
	}
	if _, err := repository.emitRunEvent(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID"), callbackEdgeRef, "EDGE_ADDED", "", callbackEdgeRef, "", "", "i18n:CHILD_CALLBACK_REGISTERED", "RUNNING", ""); err != nil {
		return commandOutcome{}, err
	}
	child, graph, err := repository.readRunGraphTx(ctx, tx, scope, childRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Run: &child, Graph: &graph, Event: &event, Runtime: map[string]any{"callbackEdgeRef": callbackEdgeRef}}, projectID: stringMap(lease, "projectID"), projectRef: stringMap(lease, "projectRef"), resourceKind: "RUN", resourceRef: childRef, summary: "i18n:CHILD_RUN_DELEGATED"}, nil
}

type callbackRecord struct {
	childRunID, childRunRef, rootRunID, projectID, parentRunID string
	resultSummary, callbackEdgeID, callbackEdgeRef             string
	parentNodeID, parentNodeRef                                string
}

func (repository *Repository) recordChildCallback(ctx context.Context, tx pgx.Tx, scope scope, record callbackRecord) (bool, error) {
	tag, err := tx.Exec(ctx, queryRuntimeCompleteexecutionInsertCallbackReceiptsChildRunId, record.childRunID, record.callbackEdgeID)
	if err != nil {
		return false, errs.ErrUnavailable
	}
	if tag.RowsAffected() == 0 {
		return true, nil
	}
	if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateRunNodesCallbackSummaryVersion, record.parentNodeID, truncate(record.resultSummary, 2000)); err != nil {
		return false, errs.ErrUnavailable
	}
	parentRunID := record.parentRunID
	if parentRunID == "" {
		return false, errs.ErrUnavailable
	}
	var sessionID string
	var turnNumber int64
	if err := tx.QueryRow(ctx, queryRuntimeCallbackSelectParentSession, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"parent_run_id":   parentRunID,
	}).Scan(&sessionID, &turnNumber); err != nil {
		return false, errs.ErrUnavailable
	}
	turnRef, _ := newRef("trn")
	var callbackTurnID string
	if err := tx.QueryRow(ctx, queryRuntimeCallbackInsertCompletedTurn, pgx.StrictNamedArgs{
		"turn_ref":        turnRef,
		"organization_id": scope.organizationID,
		"session_id":      sessionID,
		"parent_run_id":   parentRunID,
		"turn_number":     turnNumber,
		"child_run_ref":   record.childRunRef,
		"content":         truncate(record.resultSummary, 4000),
	}).Scan(&callbackTurnID); err != nil {
		return false, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeCallbackUpdateSession, pgx.StrictNamedArgs{"session_id": sessionID}); err != nil {
		return false, errs.ErrUnavailable
	}
	if _, err := repository.emitRunEvent(ctx, tx, scope, record.projectID, record.rootRunID, record.childRunRef, "CALLBACK_DELIVERED", record.parentNodeRef, record.callbackEdgeRef, "", "", "i18n:CHILD_AGENT_RESULT_DELIVERED", "RUNNING", "RUNNING"); err != nil {
		return false, err
	}
	if _, err := repository.scheduleCallbackContinuation(ctx, tx, scope, record.parentNodeID, record.projectID); err != nil {
		return false, err
	}
	return false, nil
}

func (repository *Repository) scheduleCallbackContinuation(ctx context.Context, tx pgx.Tx, scope scope, parentNodeID, projectID string) (bool, error) {
	var parentRunID, rootRunID, agentID, displayName, role, sessionID, agentRef, workflowVersionID string
	var attempt int32
	var humanGateAfter bool
	err := tx.QueryRow(ctx, queryRuntimeCallbackResolveContinuation, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"parent_node_id":  parentNodeID,
	}).Scan(&parentRunID, &rootRunID, &agentID, &attempt, &displayName, &role, &sessionID, &agentRef, &workflowVersionID, &humanGateAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, errs.ErrUnavailable
	}
	var lockedSessionID string
	var turnNumber int64
	if err := tx.QueryRow(ctx, queryRuntimeCallbackSelectParentSession, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"parent_run_id":   parentRunID,
	}).Scan(&lockedSessionID, &turnNumber); err != nil || lockedSessionID != sessionID {
		return false, errs.ErrUnavailable
	}
	const continuationTask = "Continue the task using all completed child-agent results in the session context. Produce the final response and do not repeat completed delegations."
	turnRef, _ := newRef("trn")
	var turnID string
	if err := tx.QueryRow(ctx, queryRuntimeCallbackInsertContinuationTurn, pgx.StrictNamedArgs{
		"turn_ref":        turnRef,
		"organization_id": scope.organizationID,
		"session_id":      sessionID,
		"parent_run_id":   parentRunID,
		"turn_number":     turnNumber,
		"agent_ref":       agentRef,
		"content":         continuationTask,
	}).Scan(&turnID); err != nil {
		return false, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeCallbackUpdateSession, pgx.StrictNamedArgs{"session_id": sessionID}); err != nil {
		return false, errs.ErrUnavailable
	}
	nodeRef, _ := newRef("nod")
	workflowStepKey := ""
	if workflowVersionID != "" {
		workflowStepKey = fmt.Sprintf("workflow.coordinator.continue.%d", attempt+1)
	}
	var nodeID string
	if err := tx.QueryRow(ctx, queryRuntimeCallbackInsertContinuationNode, pgx.StrictNamedArgs{
		"node_ref":          nodeRef,
		"organization_id":   scope.organizationID,
		"root_run_id":       rootRunID,
		"parent_run_id":     parentRunID,
		"parent_node_id":    parentNodeID,
		"display_name":      displayName,
		"role":              role,
		"agent_id":          agentID,
		"turn_id":           turnID,
		"workflow_step_key": workflowStepKey,
		"human_gate_after":  humanGateAfter,
		"attempt":           attempt + 1,
		"input_summary":     continuationTask,
	}).Scan(&nodeID); err != nil {
		return false, errs.ErrUnavailable
	}
	edgeRef, _ := newRef("edg")
	if _, err := tx.Exec(ctx, queryRuntimeCallbackInsertContinuesEdge, pgx.StrictNamedArgs{
		"edge_ref":        edgeRef,
		"organization_id": scope.organizationID,
		"root_run_id":     rootRunID,
		"source_node_id":  parentNodeID,
		"target_node_id":  nodeID,
	}); err != nil {
		return false, errs.ErrUnavailable
	}
	if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, nodeRef, "TURN_QUEUED", nodeRef, edgeRef, "", "", "i18n:CALLBACK_CONTINUATION_QUEUED", "RUNNING", "QUEUED"); err != nil {
		return false, err
	}
	return true, nil
}
