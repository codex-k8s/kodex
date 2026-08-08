package grpc

import (
	"context"
	"errors"
	"strings"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/grpcserver"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/resource"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/proto"
)

const maximumLegacyGraphProtoBytes = 8 << 20

func (server *Server) PrepareLegacyGraphMigration(ctx context.Context,
	request *controlplanev1.PrepareLegacyGraphMigrationRequest,
) (*controlplanev1.PrepareLegacyGraphMigrationResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_PrepareLegacyGraphMigration_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	if grpcserver.HasMalformedProto(request) || proto.Size(request) > maximumLegacyGraphProtoBytes {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	plan, err := legacyPlanFromProto(request)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	result, err := server.service.PrepareLegacyGraphMigration(ctx, resource.PrepareLegacyGraphMigrationInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(), Plan: plan,
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := legacyMigrationToProto(result)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.PrepareLegacyGraphMigrationResponse{Migration: encoded}, nil
}

func (server *Server) MaterializeLegacyGraphMigration(ctx context.Context,
	request *controlplanev1.MaterializeLegacyGraphMigrationRequest,
) (*controlplanev1.MaterializeLegacyGraphMigrationResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_MaterializeLegacyGraphMigration_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	if grpcserver.HasMalformedProto(request) {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	result, err := server.service.MaterializeLegacyGraphMigration(ctx, resource.LegacyGraphMigrationCommandInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(), PlanID: request.GetPlanId(),
		SemanticSHA256: request.GetExpectedSemanticSha256(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := legacyMigrationToProto(result)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.MaterializeLegacyGraphMigrationResponse{Migration: encoded}, nil
}

func (server *Server) GetLegacyGraphMigration(ctx context.Context,
	request *controlplanev1.GetLegacyGraphMigrationRequest,
) (*controlplanev1.GetLegacyGraphMigrationResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_GetLegacyGraphMigration_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	if grpcserver.HasMalformedProto(request) {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	result, err := server.service.GetLegacyGraphMigration(ctx, resource.GetLegacyGraphMigrationInput{
		Principal: principal, PlanID: request.GetPlanId(), Verify: request.GetVerifyCommitted(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := legacyMigrationToProto(result)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.GetLegacyGraphMigrationResponse{Migration: encoded}, nil
}

func (server *Server) AbortLegacyGraphMigration(ctx context.Context,
	request *controlplanev1.AbortLegacyGraphMigrationRequest,
) (*controlplanev1.AbortLegacyGraphMigrationResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_AbortLegacyGraphMigration_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	if grpcserver.HasMalformedProto(request) {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	result, err := server.service.AbortLegacyGraphMigration(ctx, resource.LegacyGraphMigrationCommandInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(), PlanID: request.GetPlanId(),
		SemanticSHA256: request.GetExpectedSemanticSha256(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := legacyMigrationToProto(result)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.AbortLegacyGraphMigrationResponse{Migration: encoded}, nil
}

func legacyPlanFromProto(request *controlplanev1.PrepareLegacyGraphMigrationRequest) (entity.LegacyGraphPlan, error) {
	if request == nil || len(request.GetOperations()) == 0 || len(request.GetSourceDispositions()) == 0 {
		return entity.LegacyGraphPlan{}, errors.New("legacy migration plan is absent")
	}
	result := entity.LegacyGraphPlan{
		PlanID: request.GetPlanId(), SourceRootReference: request.GetSourceRootReference(),
		SourceRootSHA256: request.GetSourceRootSha256(), SourceSnapshotSHA256: request.GetSourceSnapshotSha256(),
	}
	for _, input := range request.GetSourceDispositions() {
		table, err := legacySourceTable(input.GetSourceTable())
		if err != nil {
			return entity.LegacyGraphPlan{}, err
		}
		disposition := trimEnum(input.GetDisposition().String(), "LEGACY_SOURCE_DISPOSITION_KIND_")
		if disposition == "UNSPECIFIED" {
			return entity.LegacyGraphPlan{}, errors.New("legacy source disposition is invalid")
		}
		result.Dispositions = append(result.Dispositions, entity.LegacySourceDisposition{
			SourceTable: table, Disposition: disposition, RowCount: input.GetRowCount(),
			SourceSHA256: input.GetSourceSha256(), TerminalStateSHA256: input.GetTerminalStateSha256(),
		})
	}
	for _, input := range request.GetOperations() {
		operation, err := legacyOperationFromProto(input)
		if err != nil {
			return entity.LegacyGraphPlan{}, err
		}
		result.Operations = append(result.Operations, operation)
	}
	return result, nil
}

func legacySourceTable(input controlplanev1.LegacySourceTable) (string, error) {
	index := int(input) - 1
	if index < 0 || index >= len(entity.LegacySourceTables) {
		return "", errors.New("legacy source table is invalid")
	}
	return entity.LegacySourceTables[index], nil
}

func legacySourceFromProto(input *controlplanev1.LegacyOperationSource) (entity.LegacyOperationSource, error) {
	if input == nil {
		return entity.LegacyOperationSource{}, errors.New("legacy operation source is absent")
	}
	table, err := legacySourceTable(input.GetSourceTable())
	if err != nil {
		return entity.LegacyOperationSource{}, err
	}
	return entity.LegacyOperationSource{
		SourceTable: table, SourceRef: input.GetSourceRef(), SourceRevision: input.GetSourceRevision(),
		SourceSHA256: input.GetSourceSha256(), LocalRef: input.GetLocalRef(),
	}, nil
}

func legacyOperationFromProto(input *controlplanev1.LegacyGraphOperation) (entity.LegacyGraphOperation, error) {
	if input == nil {
		return entity.LegacyGraphOperation{}, errors.New("legacy operation is absent")
	}
	var result entity.LegacyGraphOperation
	source := func(value *controlplanev1.LegacyOperationSource) (entity.LegacyOperationSource, error) {
		return legacySourceFromProto(value)
	}
	switch value := input.GetOperation().(type) {
	case *controlplanev1.LegacyGraphOperation_Project:
		item := value.Project
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		result.Project = &entity.LegacyProjectInput{Source: origin, Name: item.GetName(), Slug: item.GetSlug(), Description: item.GetDescription(), Locale: item.GetLocale()}
	case *controlplanev1.LegacyGraphOperation_Team:
		item := value.Team
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		result.Team = &entity.LegacyTeamInput{Source: origin, Name: item.GetName(), StableKey: item.GetStableKey(), ExternalTeamRef: item.GetExternalTeamRef(), RoleDefinitionRefs: slicesClone(item.GetRoleDefinitionRefs())}
	case *controlplanev1.LegacyGraphOperation_Chat:
		item := value.Chat
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		result.Chat = &entity.LegacyChatInput{Source: origin, Name: item.GetName(), StableKey: item.GetStableKey(), RoomType: trimEnum(item.GetRoomType().String(), "ROOM_TYPE_"), ExternalChannelRef: item.GetExternalChannelRef(), WorkPolicy: item.GetWorkPolicy(), DefaultAgentRef: item.GetDefaultAgentRef()}
	case *controlplanev1.LegacyGraphOperation_Artifact:
		item := value.Artifact
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		scannedAt, err := requiredTime(item.GetScannedAt())
		if err != nil {
			return result, err
		}
		result.Artifact = &entity.LegacyArtifactInput{Source: origin, Name: item.GetName(), ArtifactKind: item.GetArtifactKind(), Direction: item.GetDirection(), StorageRef: item.GetStorageRef(), StorageVersion: item.GetStorageVersion(), SizeBytes: item.GetSizeBytes(), MediaType: item.GetMediaType(), SHA256: item.GetSha256(), RetentionPolicyRef: item.GetRetentionPolicyRef(), ScanPolicyRevision: item.GetScanPolicyRevision(), ScanEvidenceSHA256: item.GetScanEvidenceSha256(), ScannerWorkloadID: item.GetScannerWorkloadId(), ScannedAt: scannedAt}
	case *controlplanev1.LegacyGraphOperation_CredentialBinding:
		item := value.CredentialBinding
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		observedAt, err := optionalTime(item.GetObservedAt())
		if err != nil {
			return result, err
		}
		result.CredentialBinding = &entity.LegacyCredentialBindingInput{Source: origin, Name: item.GetName(), Purpose: item.GetPurpose(), SecretRef: item.GetSecretRef(), ImmutableSecretRef: item.GetImmutableSecretRef(), PrincipalRef: item.GetPrincipalRef(), Revision: item.GetRevision(), ProviderCapabilities: slicesClone(item.GetProviderCapabilities()), ObservedUsage: item.GetObservedUsage(), ObservedLimit: item.GetObservedLimit(), ObservationRevision: item.GetObservationRevision(), ObservedAt: observedAt, ContentVersion: item.GetContentVersion(), ContentSHA256: item.GetContentSha256()}
	case *controlplanev1.LegacyGraphOperation_RepositoryWorkspace:
		item := value.RepositoryWorkspace
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		result.RepositoryWorkspace = &entity.LegacyRepositoryWorkspaceInput{Source: origin, Name: item.GetName(), RepositoryRef: item.GetRepositoryRef(), WorkspaceMode: item.GetWorkspaceMode(), DefaultBranch: item.GetDefaultBranch(), CredentialBindingRef: item.GetCredentialBindingRef(), SnapshotArtifactRef: item.GetSnapshotArtifactRef()}
	case *controlplanev1.LegacyGraphOperation_RoleDefinition:
		item := value.RoleDefinition
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		result.RoleDefinition = &entity.LegacyRoleDefinitionInput{Source: origin, Name: item.GetName(), StableKey: item.GetStableKey(), Description: item.GetDescription(), Capabilities: slicesClone(item.GetCapabilities()), AllowedRoleRefs: slicesClone(item.GetAllowedRoleRefs()), RoleImageRecipeRef: item.GetRoleImageRecipeRef()}
	case *controlplanev1.LegacyGraphOperation_InstructionSet:
		item := value.InstructionSet
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		result.InstructionSet = &entity.LegacyInstructionSetInput{Source: origin, Name: item.GetName(), StableKey: item.GetStableKey(), Locale: item.GetLocale(), Content: item.GetContent(), ContentSHA256: item.GetContentSha256(), ValidationSHA256: item.GetValidationSha256(), ContentArtifactRef: item.GetContentArtifactRef()}
	case *controlplanev1.LegacyGraphOperation_ProviderReference:
		item := value.ProviderReference
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		observedAt, err := requiredTime(item.GetObservedAt())
		if err != nil {
			return result, err
		}
		result.ProviderReference = &entity.LegacyProviderReferenceInput{Source: origin, Name: item.GetName(), StableKey: item.GetStableKey(), Provider: item.GetProvider(), ServerReference: item.GetServerReference(), ReferenceVersion: item.GetReferenceVersion(), ReferenceGeneration: item.GetReferenceGeneration(), ReferenceSHA256: item.GetReferenceSha256(), MaskedLabel: item.GetMaskedLabel(), MaskedStatus: item.GetMaskedStatus(), Capabilities: slicesClone(item.GetCapabilities()), ObservedAt: observedAt, ReceiptID: item.GetReceiptId(), ReceiptVersion: item.GetReceiptVersion(), ReceiptSHA256: item.GetReceiptSha256(), CredentialBindingRef: item.GetCredentialBindingRef()}
	case *controlplanev1.LegacyGraphOperation_ProviderPool:
		item := value.ProviderPool
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		age, err := optionalDuration(item.GetObservationMaxAge())
		if err != nil {
			return result, err
		}
		pool := &entity.LegacyProviderPoolInput{Source: origin, Name: item.GetName(), StableKey: item.GetStableKey(), Policy: item.GetPolicy(), PolicyRevision: item.GetPolicyRevision(), ObservationMaxAge: age, EligibilitySnapshotSHA256: item.GetEligibilitySnapshotSha256()}
		for _, binding := range item.GetBindings() {
			pool.Bindings = append(pool.Bindings, entity.LegacyProviderPoolBinding{ProviderReferenceRef: binding.GetProviderReferenceRef(), Weight: binding.GetWeight()})
		}
		result.ProviderPool = pool
	case *controlplanev1.LegacyGraphOperation_RoleImageRecipe:
		item := value.RoleImageRecipe
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		recipeInput, err := roleImageInputFromProto(item.GetInput())
		if err != nil {
			return result, err
		}
		result.RoleImageRecipe = &entity.LegacyRoleImageRecipeInput{Source: origin, Name: item.GetName(), Input: recipeInput, Generation: item.GetGeneration(), SpecSHA256: item.GetSpecSha256(), PolicyRevision: item.GetPolicyRevision(), PolicySHA256: item.GetPolicySha256(), RuntimeContractRevision: item.GetRuntimeContractRevision(), RuntimeContractSHA256: item.GetRuntimeContractSha256()}
	case *controlplanev1.LegacyGraphOperation_ImageBuild:
		item := value.ImageBuild
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		result.ImageBuild = &entity.LegacyImageBuildInput{Source: origin, Name: item.GetName(), RecipeRef: item.GetRecipeRef(), Attempt: item.GetAttempt(), ImmutableBuildSHA256: item.GetImmutableBuildSha256(), TerminalState: string(fromProtoState(item.GetTerminalState())), TerminalEvidenceSHA256: item.GetTerminalEvidenceSha256(), StagingReference: item.GetStagingReference(), ManifestDigest: item.GetManifestDigest(), ProvenanceSHA256: item.GetProvenanceSha256()}
	case *controlplanev1.LegacyGraphOperation_ImageArtifact:
		item := value.ImageArtifact
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		promotedAt, err := requiredTime(item.GetPromotedAt())
		if err != nil {
			return result, err
		}
		result.ImageArtifact = &entity.LegacyImageArtifactInput{Source: origin, Name: item.GetName(), RecipeRef: item.GetRecipeRef(), ImageBuildRef: item.GetImageBuildRef(), ManifestDigest: item.GetManifestDigest(), PromotedReference: item.GetPromotedReference(), AdmissionRevision: item.GetAdmissionRevision(), AdmissionReceiptSHA256: item.GetAdmissionReceiptSha256(), AdmissionReceiptManifestDigest: item.GetAdmissionReceiptManifestDigest(), SignatureSHA256: item.GetSignatureSha256(), PromotionReadbackSHA256: item.GetPromotionReadbackSha256(), SBOMSHA256: item.GetSbomSha256(), VulnerabilityEvidenceSHA256: item.GetVulnerabilityEvidenceSha256(), SignatureIdentity: item.GetSignatureIdentity(), PromotedAt: promotedAt}
	case *controlplanev1.LegacyGraphOperation_Agent:
		item := value.Agent
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		result.Agent = &entity.LegacyAgentInput{Source: origin, Name: item.GetName(), StableKey: item.GetStableKey(), RoleDefinitionRef: item.GetRoleDefinitionRef(), InstructionSetRef: item.GetInstructionSetRef(), ProviderPoolRef: item.GetProviderPoolRef(), RoleImageRecipeRef: item.GetRoleImageRecipeRef(), Capabilities: slicesClone(item.GetCapabilities()), Enabled: item.GetEnabled(), BotIdentityRef: item.GetBotIdentityRef(), BotUsername: item.GetBotUsername(), BotTeamRef: item.GetBotTeamRef(), BotMaskedStatus: item.GetBotMaskedStatus(), BotReceiptID: item.GetBotReceiptId(), BotReceiptVersion: item.GetBotReceiptVersion(), BotReceiptSHA256: item.GetBotReceiptSha256(), BotProviderRevision: item.GetBotProviderRevision(), BotProviderGeneration: item.GetBotProviderGeneration()}
	case *controlplanev1.LegacyGraphOperation_AgentAssignment:
		item := value.AgentAssignment
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		result.AgentAssignment = &entity.LegacyAgentAssignmentInput{Source: origin, Name: item.GetName(), AgentRef: item.GetAgentRef(), RoomRef: item.GetRoomRef(), AssignmentGeneration: item.GetAssignmentGeneration()}
	case *controlplanev1.LegacyGraphOperation_Schedule:
		item := value.Schedule
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		nextRunAt, err := requiredTime(item.GetNextRunAt())
		if err != nil {
			return result, err
		}
		misfireGrace, err := optionalDuration(item.GetMisfireGrace())
		if err != nil {
			return result, err
		}
		initialBackoff, err := optionalDuration(item.GetInitialBackoff())
		if err != nil {
			return result, err
		}
		maximumBackoff, err := optionalDuration(item.GetMaximumBackoff())
		if err != nil {
			return result, err
		}
		deadLetterAfter, err := optionalDuration(item.GetDeadLetterAfter())
		if err != nil {
			return result, err
		}
		maximumExecution, err := optionalDuration(item.GetMaximumExecutionDuration())
		if err != nil {
			return result, err
		}
		result.Schedule = &entity.LegacyScheduleInput{Source: origin, Name: item.GetName(), StableKey: item.GetStableKey(), CronExpression: item.GetCronExpression(), Timezone: item.GetTimezone(), OverlapPolicy: trimEnum(item.GetOverlapPolicy().String(), "SCHEDULE_OVERLAP_POLICY_"), MisfirePolicy: trimEnum(item.GetMisfirePolicy().String(), "SCHEDULE_MISFIRE_POLICY_"), AgentRef: item.GetAgentRef(), AssignmentRef: item.GetAssignmentRef(), InstructionSetRef: item.GetInstructionSetRef(), ProviderPoolRef: item.GetProviderPoolRef(), RoomRef: item.GetRoomRef(), RoleImageRecipeRef: item.GetRoleImageRecipeRef(), EffectiveInputSHA256: item.GetEffectiveInputSha256(), NextRunAt: nextRunAt, State: fromProtoState(item.GetState()), Calendar: item.GetCalendar(), MisfireGrace: misfireGrace, DeliveryPolicy: item.GetDeliveryPolicy(), MaximumAttempts: item.GetMaximumAttempts(), InitialBackoff: initialBackoff, MaximumBackoff: maximumBackoff, DeadLetterAfter: deadLetterAfter, SessionPolicy: item.GetSessionPolicy(), NotificationPolicy: item.GetNotificationPolicy(), MaximumExecutionDuration: maximumExecution, Coalesce: item.GetCoalesce()}
	case *controlplanev1.LegacyGraphOperation_RuntimeRevision:
		item := value.RuntimeRevision
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		createdAt, err := requiredTime(item.GetCreatedAt())
		if err != nil {
			return result, err
		}
		revision := &entity.LegacyRuntimeRevisionInput{Source: origin, Name: item.GetName(), SessionRef: item.GetSessionRef(), ChatRef: item.GetChatRef(), AgentRef: item.GetAgentRef(), AssignmentRef: item.GetAssignmentRef(), RoleDefinitionRef: item.GetRoleDefinitionRef(), InstructionSetRef: item.GetInstructionSetRef(), ProviderPoolRef: item.GetProviderPoolRef(), ProviderCredentialRef: item.GetProviderCredentialRef(), RoleImageRecipeRef: item.GetRoleImageRecipeRef(), ImageBuildRef: item.GetImageBuildRef(), ImageArtifactRef: item.GetImageArtifactRef(), PromptArtifactRef: item.GetPromptArtifactRef(), ManifestSHA256: item.GetManifestSha256(), ImageReference: item.GetImageReference(), EffectiveRuntimeSHA256: item.GetEffectiveRuntimeSha256(), ProviderAccountName: item.GetProviderAccountName(), CodexModel: item.GetCodexModel(), CodexSandbox: item.GetCodexSandbox(), CodexApprovalPolicy: item.GetCodexApprovalPolicy(), AuthorityPolicyRevision: item.GetAuthorityPolicyRevision(), AuthorityPolicySHA256: item.GetAuthorityPolicySha256(), CreatedAt: createdAt}
		for _, component := range item.GetComponents() {
			revision.Components = append(revision.Components, entity.LegacyRuntimeComponent{LocalRef: component.GetLocalRef(), ProjectionSHA256: component.GetProjectionSha256()})
		}
		result.RuntimeRevision = revision
	case *controlplanev1.LegacyGraphOperation_Session:
		item := value.Session
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		result.Session = &entity.LegacySessionInput{Source: origin, Name: item.GetName(), AgentRef: item.GetAgentRef(), ProviderPoolRef: item.GetProviderPoolRef(), AssignmentRef: item.GetAssignmentRef(), ChatRef: item.GetChatRef(), LastTurnSequence: item.GetLastTurnSequence(), ArchiveRef: item.GetArchiveRef(), State: fromProtoState(item.GetState())}
	case *controlplanev1.LegacyGraphOperation_Turn:
		item := value.Turn
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		result.Turn = &entity.LegacyTurnInput{Source: origin, Name: item.GetName(), SessionRef: item.GetSessionRef(), Sequence: item.GetSequence(), SourceTurnRef: item.GetSourceTurnRef(), PromptArtifactRef: item.GetPromptArtifactRef(), RuntimeRevisionRef: item.GetRuntimeRevisionRef(), PredecessorTurnRef: item.GetPredecessorTurnRef(), ParentTurnRef: item.GetParentTurnRef(), ProcessRunRef: item.GetProcessRunRef(), Attempt: item.GetAttempt(), EffectiveInputSHA256: item.GetEffectiveInputSha256(), Outcome: item.GetOutcome(), ResultArtifactRef: item.GetResultArtifactRef(), State: fromProtoState(item.GetState())}
	case *controlplanev1.LegacyGraphOperation_TurnAttempt:
		item := value.TurnAttempt
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		startedAt, err := requiredTime(item.GetStartedAt())
		if err != nil {
			return result, err
		}
		finishedAt, err := optionalTime(item.GetFinishedAt())
		if err != nil {
			return result, err
		}
		result.TurnAttempt = &entity.LegacyTurnAttemptInput{Source: origin, TurnRef: item.GetTurnRef(), Attempt: item.GetAttempt(), ImmutableInputSHA256: item.GetImmutableInputSha256(), RuntimeRevisionRef: item.GetRuntimeRevisionRef(), State: string(fromProtoState(item.GetState())), Outcome: item.GetOutcome(), StartedAt: startedAt, FinishedAt: finishedAt}
	case *controlplanev1.LegacyGraphOperation_ProcessRun:
		item := value.ProcessRun
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		result.ProcessRun = &entity.LegacyProcessRunInput{Source: origin, Name: item.GetName(), RootSessionRef: item.GetRootSessionRef(), RootTurnRef: item.GetRootTurnRef(), RootAttemptRef: item.GetRootAttemptRef(), RuntimeRevisionRef: item.GetRuntimeRevisionRef(), ParentProcessRef: item.GetParentProcessRef(), LaunchingTurnRef: item.GetLaunchingTurnRef(), LaunchingAttemptRef: item.GetLaunchingAttemptRef(), ImmutableInputSHA256: item.GetImmutableInputSha256(), LegacyPolicyRevision: item.GetLegacyPolicyRevision(), LegacyPolicySHA256: item.GetLegacyPolicySha256(), PlaybookRef: item.GetPlaybookRef(), RootTriggerRef: item.GetRootTriggerRef(), Outcome: item.GetOutcome(), State: fromProtoState(item.GetState()), DelegationRef: item.GetDelegationRef(), TargetSessionRef: item.GetTargetSessionRef(), TargetTurnRef: item.GetTargetTurnRef(), TargetAttemptRef: item.GetTargetAttemptRef()}
	case *controlplanev1.LegacyGraphOperation_DelegationEdge:
		item := value.DelegationEdge
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		result.DelegationEdge = &entity.LegacyDelegationEdgeInput{Source: origin, ParentProcessRef: item.GetParentProcessRef(), ParentSessionRef: item.GetParentSessionRef(), ParentTurnRef: item.GetParentTurnRef(), ParentAttemptRef: item.GetParentAttemptRef(), ChildRoleRef: item.GetChildRoleRef(), ChildSessionRef: item.GetChildSessionRef(), ChildTurnRef: item.GetChildTurnRef(), ChildAttemptRef: item.GetChildAttemptRef(), ChildProcessRef: item.GetChildProcessRef(), GrantGeneration: item.GetGrantGeneration()}
	case *controlplanev1.LegacyGraphOperation_CallbackManifest:
		item := value.CallbackManifest
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		result.CallbackManifest = &entity.LegacyCallbackManifestInput{Source: origin, DelegationRef: item.GetDelegationRef(), CallbackProcessRef: item.GetCallbackProcessRef(), Destinations: slicesClone(item.GetDestinations()), ManifestSHA256: item.GetManifestSha256()}
	case *controlplanev1.LegacyGraphOperation_CallbackDelivery:
		item := value.CallbackDelivery
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		deliveredAt, err := requiredTime(item.GetDeliveredAt())
		if err != nil {
			return result, err
		}
		result.CallbackDelivery = &entity.LegacyCallbackDeliveryInput{Source: origin, CallbackManifestRef: item.GetCallbackManifestRef(), Destination: item.GetDestination(), ReceiptSHA256: item.GetReceiptSha256(), TerminalState: trimEnum(item.GetTerminalState().String(), "LEGACY_CALLBACK_DELIVERY_STATE_"), DeliveredAt: deliveredAt}
	case *controlplanev1.LegacyGraphOperation_MemoryRecord:
		item := value.MemoryRecord
		origin, err := source(item.GetSource())
		if err != nil {
			return result, err
		}
		result.MemoryRecord = &entity.LegacyMemoryRecordInput{Source: origin, Name: item.GetName(), MemoryKind: item.GetMemoryKind(), Content: item.GetContent(), ContentSHA256: item.GetContentSha256(), SourceVersion: item.GetSourceVersion(), State: fromProtoState(item.GetState())}
	default:
		return result, errors.New("legacy operation variant is invalid")
	}
	return result, nil
}

func slicesClone(values []string) []string {
	return append([]string(nil), values...)
}

func legacyMigrationToProto(input entity.LegacyGraphMigration) (*controlplanev1.LegacyGraphMigration, error) {
	stateValue, ok := controlplanev1.LegacyGraphMigrationState_value["LEGACY_GRAPH_MIGRATION_STATE_"+input.State]
	if !ok {
		return nil, errors.New("legacy migration state is invalid")
	}
	verificationValue, ok := controlplanev1.LegacyGraphVerificationState_value["LEGACY_GRAPH_VERIFICATION_STATE_"+input.VerificationState]
	if !ok {
		return nil, errors.New("legacy verification state is invalid")
	}
	result := &controlplanev1.LegacyGraphMigration{
		PlanId: input.PlanID, State: controlplanev1.LegacyGraphMigrationState(stateValue),
		VerificationState: controlplanev1.LegacyGraphVerificationState(verificationValue),
		SemanticSha256:    input.SemanticSHA256, SourceSnapshotSha256: input.SourceSnapshotSHA256,
		ProjectId: input.ProjectID, OperationCount: input.OperationCount,
		ArchivedSourceCount: input.ArchivedSourceCount,
		PreparedAt:          optionalTimestamp(input.PreparedAt), TerminalAt: optionalTimestamp(input.TerminalAt),
	}
	for _, receipt := range input.OperationReceipts {
		result.OperationReceipts = append(result.OperationReceipts, &controlplanev1.LegacyOperationReceipt{
			Ordinal: receipt.Ordinal, OperationKind: receipt.OperationKind, InputSha256: receipt.InputSHA256,
			TargetId: receipt.TargetID, TargetKind: receipt.TargetKind, TargetVersion: receipt.TargetVersion,
			TargetState: toProtoState(receipt.TargetState), ProjectionSha256: receipt.ProjectionSHA256,
			ProvenanceSha256: receipt.ProvenanceSHA256, AuditIds: slicesClone(receipt.AuditIDs),
			EventIds: slicesClone(receipt.EventIDs), EventSequences: append([]uint64(nil), receipt.EventSequences...),
			ProvenanceEvidenceSha256: receipt.ProvenanceEvidenceSHA256,
		})
	}
	for _, drift := range input.Drift {
		if strings.TrimSpace(drift.Predicate) == "" || len(drift.Predicate) > 256 {
			return nil, errors.New("legacy drift predicate is invalid")
		}
		result.Drift = append(result.Drift, &controlplanev1.LegacyGraphDrift{Ordinal: drift.Ordinal, Predicate: drift.Predicate})
	}
	return result, nil
}
