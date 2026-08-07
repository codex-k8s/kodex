package grpc

import (
	"errors"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func fromProtoKind(kind controlplanev1.ResourceKind) enum.Kind {
	switch kind {
	case controlplanev1.ResourceKind_RESOURCE_KIND_PROJECT:
		return enum.KindProject
	case controlplanev1.ResourceKind_RESOURCE_KIND_TEAM:
		return enum.KindTeam
	case controlplanev1.ResourceKind_RESOURCE_KIND_CHAT:
		return enum.KindChat
	case controlplanev1.ResourceKind_RESOURCE_KIND_ROLE:
		return enum.KindRole
	case controlplanev1.ResourceKind_RESOURCE_KIND_PROMPT_PROFILE:
		return enum.KindPromptProfile
	case controlplanev1.ResourceKind_RESOURCE_KIND_CREDENTIAL_BINDING:
		return enum.KindCredentialBinding
	case controlplanev1.ResourceKind_RESOURCE_KIND_REPOSITORY_WORKSPACE:
		return enum.KindRepositoryWorkspace
	case controlplanev1.ResourceKind_RESOURCE_KIND_INTEGRATION:
		return enum.KindIntegration
	case controlplanev1.ResourceKind_RESOURCE_KIND_RUNTIME_REVISION:
		return enum.KindRuntimeRevision
	case controlplanev1.ResourceKind_RESOURCE_KIND_SESSION:
		return enum.KindSession
	case controlplanev1.ResourceKind_RESOURCE_KIND_TURN:
		return enum.KindTurn
	case controlplanev1.ResourceKind_RESOURCE_KIND_PROCESS_RUN:
		return enum.KindProcessRun
	case controlplanev1.ResourceKind_RESOURCE_KIND_SCHEDULE:
		return enum.KindSchedule
	case controlplanev1.ResourceKind_RESOURCE_KIND_OWNER_GATE:
		return enum.KindOwnerGate
	case controlplanev1.ResourceKind_RESOURCE_KIND_MEMORY_RECORD:
		return enum.KindMemoryRecord
	case controlplanev1.ResourceKind_RESOURCE_KIND_WORK_CLAIM:
		return enum.KindWorkClaim
	case controlplanev1.ResourceKind_RESOURCE_KIND_ARTIFACT:
		return enum.KindArtifact
	case controlplanev1.ResourceKind_RESOURCE_KIND_ROLE_IMAGE_RECIPE:
		return enum.KindRoleImageRecipe
	case controlplanev1.ResourceKind_RESOURCE_KIND_IMAGE_BUILD:
		return enum.KindImageBuild
	case controlplanev1.ResourceKind_RESOURCE_KIND_IMAGE_ARTIFACT:
		return enum.KindImageArtifact
	case controlplanev1.ResourceKind_RESOURCE_KIND_ROLE_DEFINITION:
		return enum.KindRoleDefinition
	case controlplanev1.ResourceKind_RESOURCE_KIND_AGENT:
		return enum.KindAgent
	case controlplanev1.ResourceKind_RESOURCE_KIND_AGENT_ASSIGNMENT:
		return enum.KindAgentAssignment
	case controlplanev1.ResourceKind_RESOURCE_KIND_INSTRUCTION_SET:
		return enum.KindInstructionSet
	case controlplanev1.ResourceKind_RESOURCE_KIND_PROVIDER_CONNECTION_REFERENCE:
		return enum.KindProviderReference
	case controlplanev1.ResourceKind_RESOURCE_KIND_PROVIDER_POOL:
		return enum.KindProviderPool
	case controlplanev1.ResourceKind_RESOURCE_KIND_WORKSPACE_BACKUP:
		return enum.KindWorkspaceBackup
	case controlplanev1.ResourceKind_RESOURCE_KIND_WORKSPACE_RESTORE:
		return enum.KindWorkspaceRestore
	case controlplanev1.ResourceKind_RESOURCE_KIND_WORKSPACE_MATTERMOST_MAPPING:
		return enum.KindWorkspaceMapping
	default:
		return ""
	}
}

func toProtoKind(kind enum.Kind) controlplanev1.ResourceKind {
	switch kind {
	case enum.KindProject:
		return controlplanev1.ResourceKind_RESOURCE_KIND_PROJECT
	case enum.KindTeam:
		return controlplanev1.ResourceKind_RESOURCE_KIND_TEAM
	case enum.KindChat:
		return controlplanev1.ResourceKind_RESOURCE_KIND_CHAT
	case enum.KindRole:
		return controlplanev1.ResourceKind_RESOURCE_KIND_ROLE
	case enum.KindPromptProfile:
		return controlplanev1.ResourceKind_RESOURCE_KIND_PROMPT_PROFILE
	case enum.KindCredentialBinding:
		return controlplanev1.ResourceKind_RESOURCE_KIND_CREDENTIAL_BINDING
	case enum.KindRepositoryWorkspace:
		return controlplanev1.ResourceKind_RESOURCE_KIND_REPOSITORY_WORKSPACE
	case enum.KindIntegration:
		return controlplanev1.ResourceKind_RESOURCE_KIND_INTEGRATION
	case enum.KindRuntimeRevision:
		return controlplanev1.ResourceKind_RESOURCE_KIND_RUNTIME_REVISION
	case enum.KindSession:
		return controlplanev1.ResourceKind_RESOURCE_KIND_SESSION
	case enum.KindTurn:
		return controlplanev1.ResourceKind_RESOURCE_KIND_TURN
	case enum.KindProcessRun:
		return controlplanev1.ResourceKind_RESOURCE_KIND_PROCESS_RUN
	case enum.KindSchedule:
		return controlplanev1.ResourceKind_RESOURCE_KIND_SCHEDULE
	case enum.KindOwnerGate:
		return controlplanev1.ResourceKind_RESOURCE_KIND_OWNER_GATE
	case enum.KindMemoryRecord:
		return controlplanev1.ResourceKind_RESOURCE_KIND_MEMORY_RECORD
	case enum.KindWorkClaim:
		return controlplanev1.ResourceKind_RESOURCE_KIND_WORK_CLAIM
	case enum.KindArtifact:
		return controlplanev1.ResourceKind_RESOURCE_KIND_ARTIFACT
	case enum.KindRoleImageRecipe:
		return controlplanev1.ResourceKind_RESOURCE_KIND_ROLE_IMAGE_RECIPE
	case enum.KindImageBuild:
		return controlplanev1.ResourceKind_RESOURCE_KIND_IMAGE_BUILD
	case enum.KindImageArtifact:
		return controlplanev1.ResourceKind_RESOURCE_KIND_IMAGE_ARTIFACT
	case enum.KindRoleDefinition:
		return controlplanev1.ResourceKind_RESOURCE_KIND_ROLE_DEFINITION
	case enum.KindAgent:
		return controlplanev1.ResourceKind_RESOURCE_KIND_AGENT
	case enum.KindAgentAssignment:
		return controlplanev1.ResourceKind_RESOURCE_KIND_AGENT_ASSIGNMENT
	case enum.KindInstructionSet:
		return controlplanev1.ResourceKind_RESOURCE_KIND_INSTRUCTION_SET
	case enum.KindProviderReference:
		return controlplanev1.ResourceKind_RESOURCE_KIND_PROVIDER_CONNECTION_REFERENCE
	case enum.KindProviderPool:
		return controlplanev1.ResourceKind_RESOURCE_KIND_PROVIDER_POOL
	case enum.KindWorkspaceBackup:
		return controlplanev1.ResourceKind_RESOURCE_KIND_WORKSPACE_BACKUP
	case enum.KindWorkspaceRestore:
		return controlplanev1.ResourceKind_RESOURCE_KIND_WORKSPACE_RESTORE
	case enum.KindWorkspaceMapping:
		return controlplanev1.ResourceKind_RESOURCE_KIND_WORKSPACE_MATTERMOST_MAPPING
	default:
		return controlplanev1.ResourceKind_RESOURCE_KIND_UNSPECIFIED
	}
}

func fromProtoState(state controlplanev1.LifecycleState) enum.State {
	switch state {
	case controlplanev1.LifecycleState_LIFECYCLE_STATE_ACTIVE:
		return enum.StateActive
	case controlplanev1.LifecycleState_LIFECYCLE_STATE_PAUSED:
		return enum.StatePaused
	case controlplanev1.LifecycleState_LIFECYCLE_STATE_ARCHIVED:
		return enum.StateArchived
	case controlplanev1.LifecycleState_LIFECYCLE_STATE_DELETION_PENDING:
		return enum.StateDeletionPending
	case controlplanev1.LifecycleState_LIFECYCLE_STATE_DELETED:
		return enum.StateDeleted
	case controlplanev1.LifecycleState_LIFECYCLE_STATE_QUEUED:
		return enum.StateQueued
	case controlplanev1.LifecycleState_LIFECYCLE_STATE_CLAIMED:
		return enum.StateClaimed
	case controlplanev1.LifecycleState_LIFECYCLE_STATE_RUNNING:
		return enum.StateRunning
	case controlplanev1.LifecycleState_LIFECYCLE_STATE_WAITING_OWNER:
		return enum.StateWaitingOwner
	case controlplanev1.LifecycleState_LIFECYCLE_STATE_WAITING_EXTERNAL:
		return enum.StateWaitingExternal
	case controlplanev1.LifecycleState_LIFECYCLE_STATE_SUCCEEDED:
		return enum.StateSucceeded
	case controlplanev1.LifecycleState_LIFECYCLE_STATE_FAILED:
		return enum.StateFailed
	case controlplanev1.LifecycleState_LIFECYCLE_STATE_CANCELLED:
		return enum.StateCancelled
	case controlplanev1.LifecycleState_LIFECYCLE_STATE_EXPIRED:
		return enum.StateExpired
	case controlplanev1.LifecycleState_LIFECYCLE_STATE_BLOCKED:
		return enum.StateBlocked
	default:
		return ""
	}
}

func toProtoState(state enum.State) controlplanev1.LifecycleState {
	switch state {
	case enum.StateActive:
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_ACTIVE
	case enum.StatePaused:
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_PAUSED
	case enum.StateArchived:
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_ARCHIVED
	case enum.StateDeletionPending:
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_DELETION_PENDING
	case enum.StateDeleted:
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_DELETED
	case enum.StateQueued:
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_QUEUED
	case enum.StateClaimed:
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_CLAIMED
	case enum.StateRunning:
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_RUNNING
	case enum.StateWaitingOwner:
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_WAITING_OWNER
	case enum.StateWaitingExternal:
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_WAITING_EXTERNAL
	case enum.StateSucceeded:
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_SUCCEEDED
	case enum.StateFailed:
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_FAILED
	case enum.StateCancelled:
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_CANCELLED
	case enum.StateExpired:
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_EXPIRED
	case enum.StateBlocked:
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_BLOCKED
	default:
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_UNSPECIFIED
	}
}

func fromProtoSpec(spec *controlplanev1.ResourceSpec) (entity.Spec, error) {
	if spec == nil {
		return nil, errors.New("resource specification is required")
	}
	switch value := spec.GetValue().(type) {
	case *controlplanev1.ResourceSpec_Project:
		return entity.ProjectSpec{
			Slug:        value.Project.GetSlug(),
			Description: value.Project.GetDescription(),
			Locale:      value.Project.GetLocale(),
			Ownership:   configurationOwnershipFromProto(value.Project.GetOwnership()),
		}, nil
	case *controlplanev1.ResourceSpec_Team:
		return entity.TeamSpec{
			StableKey:       value.Team.GetStableKey(),
			ExternalTeamRef: value.Team.GetExternalTeamRef(),
			MemberActorIDs:  value.Team.GetMemberActorIds(),
			RoleIDs:         value.Team.GetRoleIds(),
			Ownership:       configurationOwnershipFromProto(value.Team.GetOwnership()),
		}, nil
	case *controlplanev1.ResourceSpec_Chat:
		return entity.ChatSpec{
			StableKey:          value.Chat.GetStableKey(),
			RoomType:           value.Chat.GetRoomType().String()[len("ROOM_TYPE_"):],
			DefaultAgentID:     value.Chat.GetDefaultAgentId(),
			ExternalChannelRef: value.Chat.GetExternalChannelRef(),
			WorkPolicy:         value.Chat.GetWorkPolicy(),
			Ownership:          configurationOwnershipFromProto(value.Chat.GetOwnership()),
		}, nil
	case *controlplanev1.ResourceSpec_Role:
		poolMaxAge, err := optionalDuration(
			value.Role.GetProviderAccountPool().GetObservationMaxAge(),
		)
		if err != nil {
			return nil, err
		}
		poolBindings := make(
			[]entity.ProviderAccountPoolBinding,
			0,
			len(value.Role.GetProviderAccountPool().GetBindings()),
		)
		for _, binding := range value.Role.GetProviderAccountPool().GetBindings() {
			poolBindings = append(poolBindings, entity.ProviderAccountPoolBinding{
				CredentialBindingID: binding.GetCredentialBindingId(),
				Weight:              binding.GetWeight(),
			})
		}
		return entity.RoleSpec{
			StableKey:                    value.Role.GetStableKey(),
			Capabilities:                 value.Role.GetCapabilities(),
			AllowedTargetRoleIDs:         value.Role.GetAllowedTargetRoleIds(),
			PromptProfileID:              value.Role.GetPromptProfileId(),
			ProviderCredentialBindingIDs: value.Role.GetProviderCredentialBindingIds(),
			RepositoryWorkspaceIDs:       value.Role.GetRepositoryWorkspaceIds(),
			IntegrationIDs:               value.Role.GetIntegrationIds(),
			ProviderAccountPool: entity.ProviderAccountPool{
				Policy:            value.Role.GetProviderAccountPool().GetPolicy(),
				PolicyRevision:    value.Role.GetProviderAccountPool().GetPolicyRevision(),
				ObservationMaxAge: poolMaxAge,
				Bindings:          poolBindings,
			},
			RoleImageRecipeID: value.Role.GetRoleImageRecipeId(),
			Ownership:         configurationOwnershipFromProto(value.Role.GetOwnership()),
		}, nil
	case *controlplanev1.ResourceSpec_PromptProfile:
		return entity.PromptProfileSpec{
			Revision: value.PromptProfile.GetRevision(), ContentSHA256: value.PromptProfile.GetContentSha256(),
			SourceRef: value.PromptProfile.GetSourceRef(), Locale: value.PromptProfile.GetLocale(),
			ContentArtifactID:      value.PromptProfile.GetContentArtifactId(),
			ContentArtifactVersion: value.PromptProfile.GetContentArtifactVersion(),
			Ownership:              configurationOwnershipFromProto(value.PromptProfile.GetOwnership()),
		}, nil
	case *controlplanev1.ResourceSpec_CredentialBinding:
		expiresAt, err := optionalTime(value.CredentialBinding.GetExpiresAt())
		if err != nil {
			return nil, err
		}
		observedAt, err := optionalTime(value.CredentialBinding.GetProviderObservedAt())
		if err != nil {
			return nil, err
		}
		return entity.CredentialBindingSpec{
			Purpose:                     value.CredentialBinding.GetPurpose(),
			SecretRef:                   value.CredentialBinding.GetSecretRef(),
			PrincipalRef:                value.CredentialBinding.GetPrincipalRef(),
			Revision:                    value.CredentialBinding.GetRevision(),
			ExpiresAt:                   expiresAt,
			ProviderEligible:            value.CredentialBinding.GetProviderEligible(),
			ProviderCapabilities:        value.CredentialBinding.GetProviderCapabilities(),
			ProviderObservedUsage:       value.CredentialBinding.GetProviderObservedUsage(),
			ProviderObservedLimit:       value.CredentialBinding.GetProviderObservedLimit(),
			ProviderObservationRevision: value.CredentialBinding.GetProviderObservationRevision(),
			ProviderObservedAt:          observedAt,
			ImmutableSecretRef:          value.CredentialBinding.GetImmutableSecretRef(),
			ProviderContentVersion:      value.CredentialBinding.GetProviderContentVersion(),
			ContentSHA256:               value.CredentialBinding.GetContentSha256(),
			Ownership:                   configurationOwnershipFromProto(value.CredentialBinding.GetOwnership()),
		}, nil
	case *controlplanev1.ResourceSpec_RepositoryWorkspace:
		return entity.RepositoryWorkspaceSpec{
			RepositoryRef:       value.RepositoryWorkspace.GetRepositoryRef(),
			WorkspaceMode:       value.RepositoryWorkspace.GetWorkspaceMode(),
			DefaultBranch:       value.RepositoryWorkspace.GetDefaultBranch(),
			CredentialBindingID: value.RepositoryWorkspace.GetCredentialBindingId(),
			SnapshotArtifactID:  value.RepositoryWorkspace.GetSnapshotArtifactId(),
			SnapshotVersion:     value.RepositoryWorkspace.GetSnapshotArtifactVersion(),
			SnapshotSHA256:      value.RepositoryWorkspace.GetSnapshotArtifactSha256(),
			Ownership:           configurationOwnershipFromProto(value.RepositoryWorkspace.GetOwnership()),
		}, nil
	case *controlplanev1.ResourceSpec_Integration:
		return entity.IntegrationSpec{
			DefinitionRef:        value.Integration.GetDefinitionRef(),
			DefinitionVersion:    value.Integration.GetDefinitionVersion(),
			Capabilities:         value.Integration.GetCapabilities(),
			CredentialBindingIDs: value.Integration.GetCredentialBindingIds(),
			EndpointRef:          value.Integration.GetEndpointRef(),
			Ownership:            configurationOwnershipFromProto(value.Integration.GetOwnership()),
		}, nil
	case *controlplanev1.ResourceSpec_RuntimeRevision:
		createdAt, err := requiredTime(value.RuntimeRevision.GetCreatedAt())
		if err != nil {
			return nil, err
		}
		components := make(
			[]entity.EffectiveResourceRef,
			0,
			len(value.RuntimeRevision.GetComponents()),
		)
		for _, component := range value.RuntimeRevision.GetComponents() {
			components = append(components, entity.EffectiveResourceRef{
				Kind:             fromProtoKind(component.GetKind()),
				ResourceID:       component.GetResourceId(),
				Version:          component.GetVersion(),
				ProjectionSHA256: component.GetProjectionSha256(),
			})
		}
		return entity.RuntimeRevisionSpec{
			ManifestSHA256:                         value.RuntimeRevision.GetManifestSha256(),
			ImageReference:                         value.RuntimeRevision.GetImageReference(),
			RoleImageRecipeID:                      value.RuntimeRevision.GetRoleImageRecipeId(),
			RoleImageRecipeVersion:                 value.RuntimeRevision.GetRoleImageRecipeVersion(),
			RoleImageSpecSHA256:                    value.RuntimeRevision.GetRoleImageSpecSha256(),
			ImageBuildID:                           value.RuntimeRevision.GetImageBuildId(),
			ImageBuildVersion:                      value.RuntimeRevision.GetImageBuildVersion(),
			ImageBuildAttempt:                      value.RuntimeRevision.GetImageBuildAttempt(),
			ImageArtifactID:                        value.RuntimeRevision.GetImageArtifactId(),
			ImageArtifactVersion:                   value.RuntimeRevision.GetImageArtifactVersion(),
			ImageManifestDigest:                    value.RuntimeRevision.GetImageManifestDigest(),
			ImageAdmissionRevision:                 value.RuntimeRevision.GetImageAdmissionRevision(),
			ImageAdmissionReceiptSHA256:            value.RuntimeRevision.GetImageAdmissionReceiptSha256(),
			ImageAdmissionReceiptOCIManifestDigest: value.RuntimeRevision.GetImageAdmissionReceiptOciManifestDigest(),
			ImagePolicyRevision:                    value.RuntimeRevision.GetImagePolicyRevision(),
			ImagePolicySHA256:                      value.RuntimeRevision.GetImagePolicySha256(),
			ImageSignatureSHA256:                   value.RuntimeRevision.GetImageSignatureSha256(),
			ImagePromotionReadbackSHA256:           value.RuntimeRevision.GetImagePromotionReadbackSha256(),
			RoleRuntimeContractRevision:            value.RuntimeRevision.GetRoleRuntimeContractRevision(),
			RoleRuntimeContractSHA256:              value.RuntimeRevision.GetRoleRuntimeContractSha256(),
			SessionID:                              value.RuntimeRevision.GetSessionId(),
			RoleID:                                 value.RuntimeRevision.GetRoleId(),
			ChatID:                                 value.RuntimeRevision.GetChatId(),
			ProviderCredentialBindingID:            value.RuntimeRevision.GetProviderCredentialBindingId(),
			ProviderAccountName:                    value.RuntimeRevision.GetProviderAccountName(),
			EffectiveRuntimeSHA256:                 value.RuntimeRevision.GetEffectiveRuntimeSha256(),
			CodexModel:                             value.RuntimeRevision.GetCodexModel(), CodexSandbox: value.RuntimeRevision.GetCodexSandbox(),
			CodexApprovalPolicy: value.RuntimeRevision.GetCodexApprovalPolicy(),
			ScheduledResultContract: scheduledResultContractFromProto(
				value.RuntimeRevision.GetScheduledResultContract(),
			),
			PromptProfileID:        value.RuntimeRevision.GetPromptProfileId(),
			PromptRevision:         value.RuntimeRevision.GetPromptRevision(),
			CredentialBindingIDs:   value.RuntimeRevision.GetCredentialBindingIds(),
			IntegrationIDs:         value.RuntimeRevision.GetIntegrationIds(),
			PredecessorRevisionID:  value.RuntimeRevision.GetPredecessorRevisionId(),
			AuthorityPolicyVersion: value.RuntimeRevision.GetAuthorityPolicyRevision(),
			AuthorityPolicySHA256:  value.RuntimeRevision.GetAuthorityPolicySha256(),
			Components:             components,
			CreatedAt:              createdAt,
			AgentID:                value.RuntimeRevision.GetAgentId(),
			AgentVersion:           value.RuntimeRevision.GetAgentVersion(),
			AgentSHA256:            value.RuntimeRevision.GetAgentSha256(),
			RoleDefinitionID:       value.RuntimeRevision.GetRoleDefinitionId(),
			RoleDefinitionVersion:  value.RuntimeRevision.GetRoleDefinitionVersion(),
			RoleDefinitionSHA256:   value.RuntimeRevision.GetRoleDefinitionSha256(),
			InstructionSetID:       value.RuntimeRevision.GetInstructionSetId(),
			InstructionSetVersion:  value.RuntimeRevision.GetInstructionSetVersion(),
			InstructionSetSHA256:   value.RuntimeRevision.GetInstructionSetSha256(),
			ProviderPoolID:         value.RuntimeRevision.GetProviderPoolId(),
			ProviderPoolVersion:    value.RuntimeRevision.GetProviderPoolVersion(),
			ProviderPoolSHA256:     value.RuntimeRevision.GetProviderPoolSha256(),
			AgentAssignmentID:      value.RuntimeRevision.GetAgentAssignmentId(),
			AgentAssignmentVersion: value.RuntimeRevision.GetAgentAssignmentVersion(),
			AgentAssignmentSHA256:  value.RuntimeRevision.GetAgentAssignmentSha256(),
		}, nil
	case *controlplanev1.ResourceSpec_Session:
		return entity.SessionSpec{
			AgentID:                    value.Session.GetAgentId(),
			AgentVersion:               value.Session.GetAgentVersion(),
			AgentSHA256:                value.Session.GetAgentSha256(),
			ProviderPoolID:             value.Session.GetProviderPoolId(),
			ProviderPoolVersion:        value.Session.GetProviderPoolVersion(),
			ProviderPoolSHA256:         value.Session.GetProviderPoolSha256(),
			AgentAssignmentID:          value.Session.GetAgentAssignmentId(),
			AgentAssignmentVersion:     value.Session.GetAgentAssignmentVersion(),
			AgentAssignmentSHA256:      value.Session.GetAgentAssignmentSha256(),
			ProviderAccountBindingID:   value.Session.GetProviderAccountBindingId(),
			ConversationID:             value.Session.GetConversationId(),
			ArchiveRef:                 value.Session.GetArchiveRef(),
			LastTurnSequence:           value.Session.GetLastTurnSequence(),
			AgentSessionKey:            value.Session.GetAgentSessionKey(),
			AgentSessionID:             value.Session.GetAgentSessionId(),
			AgentSessionBindingVersion: value.Session.GetAgentSessionBindingVersion(),
			AgentSessionBindingSHA256:  value.Session.GetAgentSessionBindingSha256(),
		}, nil
	case *controlplanev1.ResourceSpec_Turn:
		inputArtifacts := make([]entity.EffectiveArtifactRef, 0, len(value.Turn.GetInputArtifacts()))
		for _, artifact := range value.Turn.GetInputArtifacts() {
			inputArtifacts = append(inputArtifacts, entity.EffectiveArtifactRef{ArtifactID: artifact.GetArtifactId(),
				Version: artifact.GetVersion(), SHA256: artifact.GetSha256(), RelativePath: artifact.GetRelativePath(),
				MediaType: artifact.GetMediaType(), SizeBytes: artifact.GetSizeBytes()})
		}
		return entity.TurnSpec{
			SessionID:                     value.Turn.GetSessionId(),
			Sequence:                      value.Turn.GetSequence(),
			SourceRef:                     value.Turn.GetSourceRef(),
			PromptArtifactID:              value.Turn.GetPromptArtifactId(),
			RuntimeRevisionID:             value.Turn.GetRuntimeRevisionId(),
			ProcessRunID:                  value.Turn.GetProcessRunId(),
			Attempt:                       value.Turn.GetAttempt(),
			Outcome:                       value.Turn.GetOutcome(),
			ResultArtifactID:              value.Turn.GetResultArtifactId(),
			ResultArtifactVersion:         value.Turn.GetResultArtifactVersion(),
			ResultArtifactSHA256:          value.Turn.GetResultArtifactSha256(),
			EffectiveInputSHA256:          value.Turn.GetEffectiveInputSha256(),
			PredecessorTurnID:             value.Turn.GetPredecessorTurnId(),
			OwnerFeedback:                 value.Turn.GetOwnerFeedback(),
			OwnerFeedbackGateID:           value.Turn.GetOwnerFeedbackGateId(),
			OwnerFeedbackVersion:          value.Turn.GetOwnerFeedbackGateVersion(),
			OwnerFeedbackSHA256:           value.Turn.GetOwnerFeedbackSha256(),
			AgentSessionTurnID:            value.Turn.GetAgentSessionTurnId(),
			AgentRunID:                    value.Turn.GetAgentRunId(),
			AgentTurnBindingVersion:       value.Turn.GetAgentTurnBindingVersion(),
			AgentTurnBindingSHA256:        value.Turn.GetAgentTurnBindingSha256(),
			RestoreOperationID:            value.Turn.GetRestoreOperationId(),
			RestoreSourceExecutionID:      value.Turn.GetRestoreSourceExecutionId(),
			RestoreSourceVersion:          value.Turn.GetRestoreSourceVersion(),
			RestoreSourceArchiveSHA256:    value.Turn.GetRestoreSourceArchiveSha256(),
			RestoreSourceProvenanceSHA256: value.Turn.GetRestoreSourceProvenanceSha256(),
			RestoreOperationGeneration:    value.Turn.GetRestoreOperationGeneration(),
			RestoreSourceAuthoritySHA256:  value.Turn.GetRestoreSourceAuthoritySha256(),
			InputArtifacts:                inputArtifacts,
			ScheduledResultContract: scheduledResultContractFromProto(
				value.Turn.GetScheduledResultContract(),
			),
		}, nil
	case *controlplanev1.ResourceSpec_ProcessRun:
		return entity.ProcessRunSpec{
			ParentProcessRunID:                 value.ProcessRun.GetParentProcessRunId(),
			PlaybookRef:                        value.ProcessRun.GetPlaybookRef(),
			PolicyRevision:                     value.ProcessRun.GetPolicyRevision(),
			RootTriggerRef:                     value.ProcessRun.GetRootTriggerRef(),
			ResultArtifactID:                   value.ProcessRun.GetResultArtifactId(),
			RootInitiatorActorID:               value.ProcessRun.GetRootInitiatorActorId(),
			RootSessionID:                      value.ProcessRun.GetRootSessionId(),
			RootSessionVersion:                 value.ProcessRun.GetRootSessionVersion(),
			RootTurnID:                         value.ProcessRun.GetRootTurnId(),
			RootTurnVersion:                    value.ProcessRun.GetRootTurnVersion(),
			RootAttempt:                        value.ProcessRun.GetRootAttempt(),
			ImmutableInputSHA256:               value.ProcessRun.GetImmutableInputSha256(),
			RuntimeRevisionID:                  value.ProcessRun.GetRuntimeRevisionId(),
			LaunchingProcessRunID:              value.ProcessRun.GetLaunchingProcessRunId(),
			LaunchingTurnID:                    value.ProcessRun.GetLaunchingTurnId(),
			LaunchingAttempt:                   value.ProcessRun.GetLaunchingAttempt(),
			ScheduleID:                         value.ProcessRun.GetScheduleId(),
			OccurrenceID:                       value.ProcessRun.GetOccurrenceId(),
			DelegationID:                       value.ProcessRun.GetDelegationId(),
			TargetSessionID:                    value.ProcessRun.GetTargetSessionId(),
			TargetSessionVersion:               value.ProcessRun.GetTargetSessionVersion(),
			TargetTurnID:                       value.ProcessRun.GetTargetTurnId(),
			TargetTurnVersion:                  value.ProcessRun.GetTargetTurnVersion(),
			TargetAttempt:                      value.ProcessRun.GetTargetAttempt(),
			Outcome:                            value.ProcessRun.GetOutcome(),
			ContinuationGateID:                 value.ProcessRun.GetContinuationGateId(),
			ContinuationTurnID:                 value.ProcessRun.GetContinuationTurnId(),
			ContinuationTurnVersion:            value.ProcessRun.GetContinuationTurnVersion(),
			ContinuationAttempt:                value.ProcessRun.GetContinuationAttempt(),
			ContinuationRuntimeRevisionID:      value.ProcessRun.GetContinuationRuntimeRevisionId(),
			ContinuationRuntimeRevisionVersion: value.ProcessRun.GetContinuationRuntimeRevisionVersion(),
			ContinuationInputSHA256:            value.ProcessRun.GetContinuationInputSha256(),
			ContinuationKind:                   fromProtoProcessContinuationKind(value.ProcessRun.GetContinuationKind()),
			ContinuationIntegrationID:          value.ProcessRun.GetContinuationIntegrationId(),
			ContinuationOutcomeSHA256:          value.ProcessRun.GetContinuationOutcomeSha256(),
			OwnerFeedbackSHA256:                value.ProcessRun.GetOwnerFeedbackSha256(),
			CurrentSessionID:                   value.ProcessRun.GetCurrentSessionId(),
			CurrentSessionVersion:              value.ProcessRun.GetCurrentSessionVersion(),
			CurrentTurnID:                      value.ProcessRun.GetCurrentTurnId(),
			CurrentTurnVersion:                 value.ProcessRun.GetCurrentTurnVersion(),
			CurrentAttempt:                     value.ProcessRun.GetCurrentAttempt(),
			CurrentRuntimeRevisionID:           value.ProcessRun.GetCurrentRuntimeRevisionId(),
			CurrentRuntimeRevisionVersion:      value.ProcessRun.GetCurrentRuntimeRevisionVersion(),
			CurrentInputSHA256:                 value.ProcessRun.GetCurrentInputSha256(),
		}, nil
	case *controlplanev1.ResourceSpec_Schedule:
		interval, err := optionalDuration(value.Schedule.GetInterval())
		if err != nil {
			return nil, err
		}
		misfireGrace, err := optionalDuration(value.Schedule.GetMisfireGrace())
		if err != nil {
			return nil, err
		}
		nextRunAt, err := optionalTime(value.Schedule.GetNextRunAt())
		if err != nil {
			return nil, err
		}
		initialBackoff, err := optionalDuration(value.Schedule.GetInitialBackoff())
		if err != nil {
			return nil, err
		}
		maximumBackoff, err := optionalDuration(value.Schedule.GetMaximumBackoff())
		if err != nil {
			return nil, err
		}
		deadLetterAfter, err := optionalDuration(value.Schedule.GetDeadLetterAfter())
		if err != nil {
			return nil, err
		}
		return entity.ScheduleSpec{
			TargetResourceID:  value.Schedule.GetTargetResourceId(),
			TargetKind:        fromProtoKind(value.Schedule.GetTargetKind()),
			TargetVersion:     value.Schedule.GetTargetVersion(),
			EffectiveInputSHA: value.Schedule.GetEffectiveInputSha256(),
			Cron:              value.Schedule.GetCron(),
			Interval:          interval,
			Timezone:          value.Schedule.GetTimezone(),
			Calendar:          value.Schedule.GetCalendar(),
			OverlapPolicy: trimEnum(
				value.Schedule.GetOverlapPolicy().String(),
				"SCHEDULE_OVERLAP_POLICY_",
			),
			MisfirePolicy: trimEnum(
				value.Schedule.GetMisfirePolicy().String(),
				"SCHEDULE_MISFIRE_POLICY_",
			),
			MisfireGrace:      misfireGrace,
			NextRunAt:         nextRunAt,
			DeliveryPolicy:    value.Schedule.GetDeliveryPolicy(),
			MaximumAttempts:   value.Schedule.GetMaximumAttempts(),
			InitialBackoff:    initialBackoff,
			MaximumBackoff:    maximumBackoff,
			DeadLetterAfter:   deadLetterAfter,
			PromptProfileID:   value.Schedule.GetPromptProfileId(),
			PromptRevision:    value.Schedule.GetPromptRevision(),
			RuntimeRevisionID: value.Schedule.GetRuntimeRevisionId(),
			SessionPolicy: trimEnum(
				value.Schedule.GetSessionPolicy().String(),
				"SCHEDULE_SESSION_POLICY_",
			),
			RoomID: value.Schedule.GetRoomId(),
			NotificationPolicy: trimEnum(
				value.Schedule.GetNotificationPolicy().String(),
				"SCHEDULE_NOTIFICATION_POLICY_",
			),
			MaximumExecutionDuration: value.Schedule.GetMaximumExecutionDuration().AsDuration(),
			Coalesce:                 value.Schedule.GetCoalesce(),
			TargetType: trimEnum(
				value.Schedule.GetTargetType().String(),
				"SCHEDULE_TARGET_TYPE_",
			),
			PlaybookRef:             value.Schedule.GetPlaybookRef(),
			PlaybookVersion:         value.Schedule.GetPlaybookVersion(),
			PromptArtifactID:        value.Schedule.GetPromptArtifactId(),
			ExecutionSessionID:      value.Schedule.GetExecutionSessionId(),
			Ownership:               configurationOwnershipFromProto(value.Schedule.GetOwnership()),
			AgentID:                 value.Schedule.GetAgentId(),
			AgentVersion:            value.Schedule.GetAgentVersion(),
			AgentSHA256:             value.Schedule.GetAgentSha256(),
			InstructionSetID:        value.Schedule.GetInstructionSetId(),
			InstructionSetVersion:   value.Schedule.GetInstructionSetVersion(),
			InstructionSetSHA256:    value.Schedule.GetInstructionSetSha256(),
			RuntimeSelectionRef:     value.Schedule.GetRuntimeSelectionRef(),
			RuntimeSelectionVersion: value.Schedule.GetRuntimeSelectionVersion(),
			RuntimeSelectionSHA256:  value.Schedule.GetRuntimeSelectionSha256(),
			ProviderPoolID:          value.Schedule.GetProviderPoolId(),
			ProviderPoolVersion:     value.Schedule.GetProviderPoolVersion(),
			ProviderPoolSHA256:      value.Schedule.GetProviderPoolSha256(),
			AgentAssignmentID:       value.Schedule.GetAgentAssignmentId(),
			AgentAssignmentVersion:  value.Schedule.GetAgentAssignmentVersion(),
			AgentAssignmentSHA256:   value.Schedule.GetAgentAssignmentSha256(),
		}, nil
	case *controlplanev1.ResourceSpec_OwnerGate:
		expiresAt, err := requiredTime(value.OwnerGate.GetExpiresAt())
		if err != nil {
			return nil, err
		}
		deliveredAt, err := optionalTime(value.OwnerGate.GetDeliveredAt())
		if err != nil {
			return nil, err
		}
		claimExpiresAt, err := optionalTime(
			value.OwnerGate.GetDeliveryClaimExpiresAt(),
		)
		if err != nil {
			return nil, err
		}
		return entity.OwnerGateSpec{
			ProcessRunID:                  value.OwnerGate.GetProcessRunId(),
			ResultRef:                     value.OwnerGate.GetResultRef(),
			ResultSHA256:                  value.OwnerGate.GetResultSha256(),
			ExpiresAt:                     expiresAt,
			Decision:                      ownerDecisionFromProto(value.OwnerGate.GetDecision()),
			DecisionReason:                value.OwnerGate.GetDecisionReason(),
			RootInitiatorActorID:          value.OwnerGate.GetRootInitiatorActorId(),
			SessionID:                     value.OwnerGate.GetSessionId(),
			TurnID:                        value.OwnerGate.GetTurnId(),
			Attempt:                       value.OwnerGate.GetAttempt(),
			ImmutableInputSHA256:          value.OwnerGate.GetImmutableInputSha256(),
			RecipientActorID:              value.OwnerGate.GetRecipientActorId(),
			DeliveryWorkloadID:            value.OwnerGate.GetDeliveryWorkloadId(),
			DeliverySPIFFEID:              value.OwnerGate.GetDeliverySpiffeId(),
			DeliveryID:                    value.OwnerGate.GetDeliveryId(),
			DeliveryPayloadSHA256:         value.OwnerGate.GetDeliveryPayloadSha256(),
			DeliveryProviderReceiptSHA256: value.OwnerGate.GetDeliveryProviderReceiptSha256(),
			DeliveryClaimTokenSHA256:      value.OwnerGate.GetDeliveryClaimTokenSha256(),
			DeliveryFence:                 value.OwnerGate.GetDeliveryFence(),
			DeliveryClaimExpiresAt:        claimExpiresAt,
			MattermostPostID:              value.OwnerGate.GetMattermostPostId(),
			MattermostChannelID:           value.OwnerGate.GetMattermostChannelId(),
			MattermostRootPostID:          value.OwnerGate.GetMattermostRootPostId(),
			DeliveredAt:                   deliveredAt,
			ScheduleID:                    value.OwnerGate.GetScheduleId(),
			OccurrenceID:                  value.OwnerGate.GetOccurrenceId(),
			DecisionReceiptSHA256:         value.OwnerGate.GetDecisionReceiptSha256(),
			ContinuationTurnID:            value.OwnerGate.GetContinuationTurnId(),
			ContinuationTurnVersion:       value.OwnerGate.GetContinuationTurnVersion(),
			ContinuationInputSHA256:       value.OwnerGate.GetContinuationInputSha256(),
			NotificationRoomID:            value.OwnerGate.GetNotificationRoomId(),
		}, nil
	case *controlplanev1.ResourceSpec_MemoryRecord:
		return entity.MemoryRecordSpec{
			Scope:         value.MemoryRecord.GetScope(),
			RoleID:        value.MemoryRecord.GetRoleId(),
			Title:         value.MemoryRecord.GetTitle(),
			Content:       value.MemoryRecord.GetContent(),
			ContentSHA256: value.MemoryRecord.GetContentSha256(),
			Provenance:    value.MemoryRecord.GetProvenance(),
			Importance:    value.MemoryRecord.GetImportance(),
		}, nil
	case *controlplanev1.ResourceSpec_WorkClaim:
		expiresAt, err := optionalTime(value.WorkClaim.GetExpiresAt())
		if err != nil {
			return nil, err
		}
		return entity.WorkClaimSpec{
			ProcessRunID:        value.WorkClaim.GetProcessRunId(),
			TurnID:              value.WorkClaim.GetTurnId(),
			Summary:             value.WorkClaim.GetSummary(),
			Domains:             value.WorkClaim.GetDomains(),
			ResourceKeys:        value.WorkClaim.GetResourceKeys(),
			OwnerActorID:        value.WorkClaim.GetOwnerActorId(),
			WorkloadID:          value.WorkClaim.GetWorkloadId(),
			SessionID:           value.WorkClaim.GetSessionId(),
			Attempt:             value.WorkClaim.GetAttempt(),
			AuthorityGeneration: value.WorkClaim.GetAuthorityGeneration(),
			ExpiresAt:           expiresAt,
		}, nil
	case *controlplanev1.ResourceSpec_Artifact:
		scannedAt, err := optionalTime(value.Artifact.GetScannedAt())
		if err != nil {
			return nil, err
		}
		return entity.ArtifactSpec{
			ArtifactKind: value.Artifact.GetKind(),
			Direction:    value.Artifact.GetDirection(),
			StorageRef:   value.Artifact.GetStorageRef(),
			SizeBytes:    value.Artifact.GetSizeBytes(),
			MediaType:    value.Artifact.GetMediaType(),
			SHA256:       value.Artifact.GetSha256(),
			ScanStatus: trimEnum(
				value.Artifact.GetScanStatus().String(),
				"ARTIFACT_SCAN_STATE_",
			),
			RetentionPolicyRef: value.Artifact.GetRetentionPolicyRef(),
			ScanPolicyRevision: value.Artifact.GetScanPolicyRevision(),
			ScanEvidenceSHA256: value.Artifact.GetScanEvidenceSha256(),
			ScannerWorkloadID:  value.Artifact.GetScannerWorkloadId(),
			ScannedAt:          scannedAt,
		}, nil
	case *controlplanev1.ResourceSpec_RoleImageRecipe:
		input, err := roleImageInputFromProto(value.RoleImageRecipe.GetInput())
		if err != nil {
			return nil, err
		}
		return entity.RoleImageRecipeSpec{
			Input: input, Generation: value.RoleImageRecipe.GetGeneration(),
			SpecSHA256:                  value.RoleImageRecipe.GetSpecSha256(),
			PolicyRevision:              value.RoleImageRecipe.GetPolicyRevision(),
			PolicySHA256:                value.RoleImageRecipe.GetPolicySha256(),
			RoleRuntimeContractRevision: value.RoleImageRecipe.GetRoleRuntimeContractRevision(),
			RoleRuntimeContractSHA256:   value.RoleImageRecipe.GetRoleRuntimeContractSha256(),
		}, nil
	case *controlplanev1.ResourceSpec_ImageBuild:
		leaseExpiresAt, err := optionalTime(value.ImageBuild.GetLeaseExpiresAt())
		if err != nil {
			return nil, err
		}
		availableAt, err := requiredTime(value.ImageBuild.GetAvailableAt())
		if err != nil {
			return nil, err
		}
		return entity.ImageBuildSpec{
			RecipeID: value.ImageBuild.GetRecipeId(), RecipeVersion: value.ImageBuild.GetRecipeVersion(),
			RecipeGeneration: value.ImageBuild.GetRecipeGeneration(), SpecSHA256: value.ImageBuild.GetSpecSha256(),
			Attempt: value.ImageBuild.GetAttempt(), ClaimantWorkloadID: value.ImageBuild.GetClaimantWorkloadId(),
			ClaimantSPIFFEID: value.ImageBuild.GetClaimantSpiffeId(), AuthorityGeneration: value.ImageBuild.GetAuthorityGeneration(),
			Fence: value.ImageBuild.GetFence(), LeaseExpiresAt: leaseExpiresAt,
			Stage: imageBuildStageFromProto(value.ImageBuild.GetStage()), ProgressPercent: value.ImageBuild.GetProgressPercent(),
			StagingReference: value.ImageBuild.GetStagingReference(), ManifestDigest: value.ImageBuild.GetManifestDigest(),
			ProvenanceSHA256: value.ImageBuild.GetProvenanceSha256(), ImmutableBuildSHA256: value.ImageBuild.GetImmutableBuildSha256(),
			ErrorCode: value.ImageBuild.GetErrorCode(), AvailableAt: availableAt, MaximumAttempts: value.ImageBuild.GetMaximumAttempts(),
			LeaseTokenSHA256: value.ImageBuild.GetLeaseTokenSha256(), ClaimJTISHA256: value.ImageBuild.GetClaimJtiSha256(),
			DiagnosticCode: value.ImageBuild.GetDiagnosticCode(), DiagnosticSummary: value.ImageBuild.GetDiagnosticSummary(),
		}, nil
	case *controlplanev1.ResourceSpec_ImageArtifact:
		promotionClaimExpiresAt, err := optionalTime(value.ImageArtifact.GetPromotionClaimExpiresAt())
		if err != nil {
			return nil, err
		}
		promotedAt, err := optionalTime(value.ImageArtifact.GetPromotedAt())
		if err != nil {
			return nil, err
		}
		admissionClaimExpiresAt, err := optionalTime(value.ImageArtifact.GetAdmissionClaimExpiresAt())
		if err != nil {
			return nil, err
		}
		promotionAuthorizationExpiresAt, err := optionalTime(value.ImageArtifact.GetPromotionAuthorizationExpiresAt())
		if err != nil {
			return nil, err
		}
		return entity.ImageArtifactSpec{
			RecipeID: value.ImageArtifact.GetRecipeId(), RecipeVersion: value.ImageArtifact.GetRecipeVersion(),
			RecipeGeneration: value.ImageArtifact.GetRecipeGeneration(), SpecSHA256: value.ImageArtifact.GetSpecSha256(),
			BuildID: value.ImageArtifact.GetBuildId(), BuildVersion: value.ImageArtifact.GetBuildVersion(),
			BuildAttempt: value.ImageArtifact.GetBuildAttempt(), StagingReference: value.ImageArtifact.GetStagingReference(),
			ManifestDigest: value.ImageArtifact.GetManifestDigest(), ProvenanceSHA256: value.ImageArtifact.GetProvenanceSha256(),
			ImmutableBuildSHA256: value.ImageArtifact.GetImmutableBuildSha256(), SBOMSHA256: value.ImageArtifact.GetSbomSha256(),
			BaseImageDigest: value.ImageArtifact.GetBaseImageDigest(), SourceSHA256: value.ImageArtifact.GetSourceSha256(),
			ContextSHA256: value.ImageArtifact.GetContextSha256(), BuilderSHA256: value.ImageArtifact.GetBuilderSha256(),
			FrontendSHA256: value.ImageArtifact.GetFrontendSha256(), ToolchainSHA256: value.ImageArtifact.GetToolchainSha256(),
			Platforms:                   roleImagePlatformsFromProto(value.ImageArtifact.GetPlatforms()),
			VulnerabilityEvidenceSHA256: value.ImageArtifact.GetVulnerabilityEvidenceSha256(),
			PolicyRevision:              value.ImageArtifact.GetPolicyRevision(), PolicySHA256: value.ImageArtifact.GetPolicySha256(),
			AdmissionVerdict:  imageAdmissionVerdictFromProto(value.ImageArtifact.GetAdmissionVerdict()),
			SignatureIdentity: value.ImageArtifact.GetSignatureIdentity(), SignatureSHA256: value.ImageArtifact.GetSignatureSha256(),
			AdmissionRevision: value.ImageArtifact.GetAdmissionRevision(), AdmissionReceiptSHA256: value.ImageArtifact.GetAdmissionReceiptSha256(),
			AdmissionReceiptOCIManifestDigest: value.ImageArtifact.GetAdmissionReceiptOciManifestDigest(),
			RoleRuntimeContractRevision:       value.ImageArtifact.GetRoleRuntimeContractRevision(),
			RoleRuntimeContractSHA256:         value.ImageArtifact.GetRoleRuntimeContractSha256(),
			PromotionClaimJTISHA256:           value.ImageArtifact.GetPromotionClaimJtiSha256(), PromotionClaimExpiresAt: promotionClaimExpiresAt,
			PromotionClaimantWorkloadID:  value.ImageArtifact.GetPromotionClaimantWorkloadId(),
			PromotionClaimantSPIFFEID:    value.ImageArtifact.GetPromotionClaimantSpiffeId(),
			PromotionAuthorityGeneration: value.ImageArtifact.GetPromotionAuthorityGeneration(), PromotionFence: value.ImageArtifact.GetPromotionFence(),
			PromotionAuthorizationTokenSHA256: value.ImageArtifact.GetPromotionAuthorizationTokenSha256(),
			PromotionAuthorizationExpiresAt:   promotionAuthorizationExpiresAt,
			PromotedReference:                 value.ImageArtifact.GetPromotedReference(), PromotionReadbackSHA256: value.ImageArtifact.GetPromotionReadbackSha256(),
			PromotedAt: promotedAt, AdmissionClaimantWorkloadID: value.ImageArtifact.GetAdmissionClaimantWorkloadId(),
			AdmissionAuthorityGeneration: value.ImageArtifact.GetAdmissionAuthorityGeneration(), AdmissionFence: value.ImageArtifact.GetAdmissionFence(),
			AdmissionClaimTokenSHA256: value.ImageArtifact.GetAdmissionClaimTokenSha256(), AdmissionClaimExpiresAt: admissionClaimExpiresAt,
		}, nil
	case *controlplanev1.ResourceSpec_RoleDefinition:
		return roleDefinitionFromProto(value.RoleDefinition), nil
	case *controlplanev1.ResourceSpec_Agent:
		return agentFromProto(value.Agent), nil
	case *controlplanev1.ResourceSpec_AgentAssignment:
		return agentAssignmentFromProto(value.AgentAssignment), nil
	case *controlplanev1.ResourceSpec_InstructionSet:
		return instructionSetFromProto(value.InstructionSet), nil
	case *controlplanev1.ResourceSpec_ProviderConnectionReference:
		return providerReferenceFromProto(value.ProviderConnectionReference)
	case *controlplanev1.ResourceSpec_ProviderPool:
		return providerPoolFromProto(value.ProviderPool)
	case *controlplanev1.ResourceSpec_WorkspaceBackup:
		return workspaceBackupFromProto(value.WorkspaceBackup)
	case *controlplanev1.ResourceSpec_WorkspaceRestore:
		return workspaceRestoreFromProto(value.WorkspaceRestore), nil
	case *controlplanev1.ResourceSpec_WorkspaceMattermostMapping:
		return workspaceMappingFromProto(value.WorkspaceMattermostMapping)
	default:
		return nil, errors.New("resource specification is unknown")
	}
}

func toProtoResource(resource entity.Resource) (*controlplanev1.Resource, error) {
	spec, err := toProtoSpec(resource.Spec)
	if err != nil {
		return nil, err
	}
	projectionSHA256, err := entity.ProjectionSHA256(resource)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.Resource{
		Id:               resource.ID,
		Kind:             toProtoKind(resource.Kind),
		Name:             resource.Name,
		State:            toProtoState(resource.State),
		Version:          resource.Version,
		ProjectId:        resource.ProjectID,
		ParentId:         resource.ParentID,
		Spec:             spec,
		CreatedAt:        timestamppb.New(resource.CreatedAt),
		UpdatedAt:        timestamppb.New(resource.UpdatedAt),
		ProjectionSha256: projectionSHA256,
	}, nil
}

func toProtoSpec(spec entity.Spec) (*controlplanev1.ResourceSpec, error) {
	result := &controlplanev1.ResourceSpec{}
	switch value := spec.(type) {
	case entity.ProjectSpec:
		result.Value = &controlplanev1.ResourceSpec_Project{
			Project: &controlplanev1.ProjectSpec{
				Slug:        value.Slug,
				Description: value.Description,
				Locale:      value.Locale,
				Ownership:   configurationOwnershipToProto(value.Ownership),
			},
		}
	case entity.TeamSpec:
		result.Value = &controlplanev1.ResourceSpec_Team{
			Team: &controlplanev1.TeamSpec{
				StableKey:       value.StableKey,
				ExternalTeamRef: value.ExternalTeamRef,
				MemberActorIds:  value.MemberActorIDs,
				RoleIds:         value.RoleIDs,
				Ownership:       configurationOwnershipToProto(value.Ownership),
			},
		}
	case entity.ChatSpec:
		result.Value = &controlplanev1.ResourceSpec_Chat{
			Chat: &controlplanev1.ChatSpec{
				StableKey:          value.StableKey,
				RoomType:           roomType(value.RoomType),
				DefaultAgentId:     value.DefaultAgentID,
				ExternalChannelRef: value.ExternalChannelRef,
				WorkPolicy:         value.WorkPolicy,
				Ownership:          configurationOwnershipToProto(value.Ownership),
			},
		}
	case entity.RoleSpec:
		poolBindings := make(
			[]*controlplanev1.ProviderAccountPoolBinding,
			0,
			len(value.ProviderAccountPool.Bindings),
		)
		for _, binding := range value.ProviderAccountPool.Bindings {
			poolBindings = append(poolBindings, &controlplanev1.ProviderAccountPoolBinding{
				CredentialBindingId: binding.CredentialBindingID,
				Weight:              binding.Weight,
			})
		}
		result.Value = &controlplanev1.ResourceSpec_Role{
			Role: &controlplanev1.RoleSpec{
				StableKey:                    value.StableKey,
				Capabilities:                 value.Capabilities,
				AllowedTargetRoleIds:         value.AllowedTargetRoleIDs,
				PromptProfileId:              value.PromptProfileID,
				ProviderCredentialBindingIds: value.ProviderCredentialBindingIDs,
				RepositoryWorkspaceIds:       value.RepositoryWorkspaceIDs,
				IntegrationIds:               value.IntegrationIDs,
				ProviderAccountPool: &controlplanev1.ProviderAccountPool{
					Policy:         value.ProviderAccountPool.Policy,
					PolicyRevision: value.ProviderAccountPool.PolicyRevision,
					ObservationMaxAge: optionalProtoDuration(
						value.ProviderAccountPool.ObservationMaxAge,
					),
					Bindings: poolBindings,
				},
				RoleImageRecipeId: value.RoleImageRecipeID,
				Ownership:         configurationOwnershipToProto(value.Ownership),
			},
		}
	case entity.PromptProfileSpec:
		result.Value = &controlplanev1.ResourceSpec_PromptProfile{
			PromptProfile: &controlplanev1.PromptProfileSpec{
				Revision:               value.Revision,
				ContentSha256:          value.ContentSHA256,
				SourceRef:              value.SourceRef,
				Locale:                 value.Locale,
				ContentArtifactId:      value.ContentArtifactID,
				ContentArtifactVersion: value.ContentArtifactVersion,
				Ownership:              configurationOwnershipToProto(value.Ownership),
			},
		}
	case entity.CredentialBindingSpec:
		result.Value = &controlplanev1.ResourceSpec_CredentialBinding{
			CredentialBinding: &controlplanev1.CredentialBindingSpec{
				Purpose:                     value.Purpose,
				SecretRef:                   value.SecretRef,
				PrincipalRef:                value.PrincipalRef,
				Revision:                    value.Revision,
				ExpiresAt:                   optionalTimestamp(value.ExpiresAt),
				ProviderEligible:            value.ProviderEligible,
				ProviderCapabilities:        value.ProviderCapabilities,
				ProviderObservedUsage:       value.ProviderObservedUsage,
				ProviderObservedLimit:       value.ProviderObservedLimit,
				ProviderObservationRevision: value.ProviderObservationRevision,
				ProviderObservedAt:          optionalTimestamp(value.ProviderObservedAt),
				ImmutableSecretRef:          value.ImmutableSecretRef,
				ProviderContentVersion:      value.ProviderContentVersion,
				ContentSha256:               value.ContentSHA256,
				Ownership:                   configurationOwnershipToProto(value.Ownership),
			},
		}
	case entity.RepositoryWorkspaceSpec:
		result.Value = &controlplanev1.ResourceSpec_RepositoryWorkspace{
			RepositoryWorkspace: &controlplanev1.RepositoryWorkspaceSpec{
				RepositoryRef:           value.RepositoryRef,
				WorkspaceMode:           value.WorkspaceMode,
				DefaultBranch:           value.DefaultBranch,
				CredentialBindingId:     value.CredentialBindingID,
				SnapshotArtifactId:      value.SnapshotArtifactID,
				SnapshotArtifactVersion: value.SnapshotVersion,
				SnapshotArtifactSha256:  value.SnapshotSHA256,
				Ownership:               configurationOwnershipToProto(value.Ownership),
			},
		}
	case entity.IntegrationSpec:
		result.Value = &controlplanev1.ResourceSpec_Integration{
			Integration: &controlplanev1.IntegrationSpec{
				DefinitionRef:        value.DefinitionRef,
				DefinitionVersion:    value.DefinitionVersion,
				Capabilities:         value.Capabilities,
				CredentialBindingIds: value.CredentialBindingIDs,
				EndpointRef:          value.EndpointRef,
				Ownership:            configurationOwnershipToProto(value.Ownership),
			},
		}
	case entity.RuntimeRevisionSpec:
		components := make(
			[]*controlplanev1.EffectiveResourceRef,
			0,
			len(value.Components),
		)
		for _, component := range value.Components {
			components = append(components, &controlplanev1.EffectiveResourceRef{
				Kind:             toProtoKind(component.Kind),
				ResourceId:       component.ResourceID,
				Version:          component.Version,
				ProjectionSha256: component.ProjectionSHA256,
			})
		}
		result.Value = &controlplanev1.ResourceSpec_RuntimeRevision{
			RuntimeRevision: &controlplanev1.RuntimeRevisionSpec{
				ManifestSha256:                         value.ManifestSHA256,
				ImageReference:                         value.ImageReference,
				RoleImageRecipeId:                      value.RoleImageRecipeID,
				RoleImageRecipeVersion:                 value.RoleImageRecipeVersion,
				RoleImageSpecSha256:                    value.RoleImageSpecSHA256,
				ImageBuildId:                           value.ImageBuildID,
				ImageBuildVersion:                      value.ImageBuildVersion,
				ImageBuildAttempt:                      value.ImageBuildAttempt,
				ImageArtifactId:                        value.ImageArtifactID,
				ImageArtifactVersion:                   value.ImageArtifactVersion,
				ImageManifestDigest:                    value.ImageManifestDigest,
				ImageAdmissionRevision:                 value.ImageAdmissionRevision,
				ImageAdmissionReceiptSha256:            value.ImageAdmissionReceiptSHA256,
				ImageAdmissionReceiptOciManifestDigest: value.ImageAdmissionReceiptOCIManifestDigest,
				ImagePolicyRevision:                    value.ImagePolicyRevision,
				ImagePolicySha256:                      value.ImagePolicySHA256,
				ImageSignatureSha256:                   value.ImageSignatureSHA256,
				ImagePromotionReadbackSha256:           value.ImagePromotionReadbackSHA256,
				RoleRuntimeContractRevision:            value.RoleRuntimeContractRevision,
				RoleRuntimeContractSha256:              value.RoleRuntimeContractSHA256,
				SessionId:                              value.SessionID,
				RoleId:                                 value.RoleID,
				ChatId:                                 value.ChatID,
				ProviderCredentialBindingId:            value.ProviderCredentialBindingID,
				ProviderAccountName:                    value.ProviderAccountName,
				EffectiveRuntimeSha256:                 value.EffectiveRuntimeSHA256,
				CodexModel:                             value.CodexModel, CodexSandbox: value.CodexSandbox,
				CodexApprovalPolicy:     value.CodexApprovalPolicy,
				ScheduledResultContract: scheduledResultContractToProto(value.ScheduledResultContract),
				PromptProfileId:         value.PromptProfileID,
				PromptRevision:          value.PromptRevision,
				CredentialBindingIds:    value.CredentialBindingIDs,
				IntegrationIds:          value.IntegrationIDs,
				PredecessorRevisionId:   value.PredecessorRevisionID,
				AuthorityPolicyRevision: value.AuthorityPolicyVersion,
				AuthorityPolicySha256:   value.AuthorityPolicySHA256,
				Components:              components,
				CreatedAt:               timestamppb.New(value.CreatedAt),
				AgentId:                 value.AgentID,
				AgentVersion:            value.AgentVersion,
				AgentSha256:             value.AgentSHA256,
				RoleDefinitionId:        value.RoleDefinitionID,
				RoleDefinitionVersion:   value.RoleDefinitionVersion,
				RoleDefinitionSha256:    value.RoleDefinitionSHA256,
				InstructionSetId:        value.InstructionSetID,
				InstructionSetVersion:   value.InstructionSetVersion,
				InstructionSetSha256:    value.InstructionSetSHA256,
				ProviderPoolId:          value.ProviderPoolID,
				ProviderPoolVersion:     value.ProviderPoolVersion,
				ProviderPoolSha256:      value.ProviderPoolSHA256,
				AgentAssignmentId:       value.AgentAssignmentID,
				AgentAssignmentVersion:  value.AgentAssignmentVersion,
				AgentAssignmentSha256:   value.AgentAssignmentSHA256,
			},
		}
	case entity.SessionSpec:
		result.Value = &controlplanev1.ResourceSpec_Session{
			Session: &controlplanev1.SessionSpec{
				AgentId:                    value.AgentID,
				AgentVersion:               value.AgentVersion,
				AgentSha256:                value.AgentSHA256,
				ProviderPoolId:             value.ProviderPoolID,
				ProviderPoolVersion:        value.ProviderPoolVersion,
				ProviderPoolSha256:         value.ProviderPoolSHA256,
				AgentAssignmentId:          value.AgentAssignmentID,
				AgentAssignmentVersion:     value.AgentAssignmentVersion,
				AgentAssignmentSha256:      value.AgentAssignmentSHA256,
				ProviderAccountBindingId:   value.ProviderAccountBindingID,
				ConversationId:             value.ConversationID,
				ArchiveRef:                 value.ArchiveRef,
				LastTurnSequence:           value.LastTurnSequence,
				AgentSessionKey:            value.AgentSessionKey,
				AgentSessionId:             value.AgentSessionID,
				AgentSessionBindingVersion: value.AgentSessionBindingVersion,
				AgentSessionBindingSha256:  value.AgentSessionBindingSHA256,
			},
		}
	case entity.TurnSpec:
		inputArtifacts := make([]*controlplanev1.EffectiveArtifactRef, 0, len(value.InputArtifacts))
		for _, artifact := range value.InputArtifacts {
			inputArtifacts = append(inputArtifacts, &controlplanev1.EffectiveArtifactRef{ArtifactId: artifact.ArtifactID,
				Version: artifact.Version, Sha256: artifact.SHA256, RelativePath: artifact.RelativePath,
				MediaType: artifact.MediaType, SizeBytes: artifact.SizeBytes})
		}
		result.Value = &controlplanev1.ResourceSpec_Turn{
			Turn: &controlplanev1.TurnSpec{
				SessionId:                     value.SessionID,
				Sequence:                      value.Sequence,
				SourceRef:                     value.SourceRef,
				PromptArtifactId:              value.PromptArtifactID,
				RuntimeRevisionId:             value.RuntimeRevisionID,
				ProcessRunId:                  value.ProcessRunID,
				Attempt:                       value.Attempt,
				Outcome:                       value.Outcome,
				ResultArtifactId:              value.ResultArtifactID,
				ResultArtifactVersion:         value.ResultArtifactVersion,
				ResultArtifactSha256:          value.ResultArtifactSHA256,
				EffectiveInputSha256:          value.EffectiveInputSHA256,
				PredecessorTurnId:             value.PredecessorTurnID,
				OwnerFeedback:                 value.OwnerFeedback,
				OwnerFeedbackGateId:           value.OwnerFeedbackGateID,
				OwnerFeedbackGateVersion:      value.OwnerFeedbackVersion,
				OwnerFeedbackSha256:           value.OwnerFeedbackSHA256,
				AgentSessionTurnId:            value.AgentSessionTurnID,
				AgentRunId:                    value.AgentRunID,
				AgentTurnBindingVersion:       value.AgentTurnBindingVersion,
				AgentTurnBindingSha256:        value.AgentTurnBindingSHA256,
				RestoreOperationId:            value.RestoreOperationID,
				RestoreSourceExecutionId:      value.RestoreSourceExecutionID,
				RestoreSourceVersion:          value.RestoreSourceVersion,
				RestoreSourceArchiveSha256:    value.RestoreSourceArchiveSHA256,
				RestoreSourceProvenanceSha256: value.RestoreSourceProvenanceSHA256,
				RestoreOperationGeneration:    value.RestoreOperationGeneration,
				RestoreSourceAuthoritySha256:  value.RestoreSourceAuthoritySHA256,
				InputArtifacts:                inputArtifacts,
				ScheduledResultContract:       scheduledResultContractToProto(value.ScheduledResultContract),
			},
		}
	case entity.ProcessRunSpec:
		result.Value = &controlplanev1.ResourceSpec_ProcessRun{
			ProcessRun: &controlplanev1.ProcessRunSpec{
				ParentProcessRunId:                 value.ParentProcessRunID,
				PlaybookRef:                        value.PlaybookRef,
				PolicyRevision:                     value.PolicyRevision,
				RootTriggerRef:                     value.RootTriggerRef,
				ResultArtifactId:                   value.ResultArtifactID,
				RootInitiatorActorId:               value.RootInitiatorActorID,
				RootSessionId:                      value.RootSessionID,
				RootSessionVersion:                 value.RootSessionVersion,
				RootTurnId:                         value.RootTurnID,
				RootTurnVersion:                    value.RootTurnVersion,
				RootAttempt:                        value.RootAttempt,
				ImmutableInputSha256:               value.ImmutableInputSHA256,
				RuntimeRevisionId:                  value.RuntimeRevisionID,
				LaunchingProcessRunId:              value.LaunchingProcessRunID,
				LaunchingTurnId:                    value.LaunchingTurnID,
				LaunchingAttempt:                   value.LaunchingAttempt,
				ScheduleId:                         value.ScheduleID,
				OccurrenceId:                       value.OccurrenceID,
				DelegationId:                       value.DelegationID,
				TargetSessionId:                    value.TargetSessionID,
				TargetSessionVersion:               value.TargetSessionVersion,
				TargetTurnId:                       value.TargetTurnID,
				TargetTurnVersion:                  value.TargetTurnVersion,
				TargetAttempt:                      value.TargetAttempt,
				Outcome:                            value.Outcome,
				ContinuationGateId:                 value.ContinuationGateID,
				ContinuationTurnId:                 value.ContinuationTurnID,
				ContinuationTurnVersion:            value.ContinuationTurnVersion,
				ContinuationAttempt:                value.ContinuationAttempt,
				ContinuationRuntimeRevisionId:      value.ContinuationRuntimeRevisionID,
				ContinuationRuntimeRevisionVersion: value.ContinuationRuntimeRevisionVersion,
				ContinuationInputSha256:            value.ContinuationInputSHA256,
				ContinuationKind:                   toProtoProcessContinuationKind(value.ContinuationKind),
				ContinuationIntegrationId:          value.ContinuationIntegrationID,
				ContinuationOutcomeSha256:          value.ContinuationOutcomeSHA256,
				OwnerFeedbackSha256:                value.OwnerFeedbackSHA256,
				CurrentSessionId:                   value.CurrentSessionID,
				CurrentSessionVersion:              value.CurrentSessionVersion,
				CurrentTurnId:                      value.CurrentTurnID,
				CurrentTurnVersion:                 value.CurrentTurnVersion,
				CurrentAttempt:                     value.CurrentAttempt,
				CurrentRuntimeRevisionId:           value.CurrentRuntimeRevisionID,
				CurrentRuntimeRevisionVersion:      value.CurrentRuntimeRevisionVersion,
				CurrentInputSha256:                 value.CurrentInputSHA256,
			},
		}
	case entity.ScheduleSpec:
		result.Value = &controlplanev1.ResourceSpec_Schedule{
			Schedule: &controlplanev1.ScheduleSpec{
				TargetResourceId:         value.TargetResourceID,
				Cron:                     value.Cron,
				Interval:                 optionalProtoDuration(value.Interval),
				Timezone:                 value.Timezone,
				OverlapPolicy:            overlapPolicy(value.OverlapPolicy),
				MisfirePolicy:            misfirePolicy(value.MisfirePolicy),
				MisfireGrace:             optionalProtoDuration(value.MisfireGrace),
				NextRunAt:                timestamppb.New(value.NextRunAt),
				TargetKind:               toProtoKind(value.TargetKind),
				TargetVersion:            value.TargetVersion,
				EffectiveInputSha256:     value.EffectiveInputSHA,
				Calendar:                 value.Calendar,
				DeliveryPolicy:           value.DeliveryPolicy,
				MaximumAttempts:          value.MaximumAttempts,
				InitialBackoff:           optionalProtoDuration(value.InitialBackoff),
				MaximumBackoff:           optionalProtoDuration(value.MaximumBackoff),
				DeadLetterAfter:          optionalProtoDuration(value.DeadLetterAfter),
				PromptProfileId:          value.PromptProfileID,
				PromptRevision:           value.PromptRevision,
				RuntimeRevisionId:        value.RuntimeRevisionID,
				SessionPolicy:            scheduleSessionPolicy(value.SessionPolicy),
				RoomId:                   value.RoomID,
				NotificationPolicy:       scheduleNotificationPolicy(value.NotificationPolicy),
				MaximumExecutionDuration: optionalProtoDuration(value.MaximumExecutionDuration),
				Coalesce:                 value.Coalesce,
				TargetType:               scheduleTargetType(value.TargetType),
				PlaybookRef:              value.PlaybookRef,
				PlaybookVersion:          value.PlaybookVersion,
				PromptArtifactId:         value.PromptArtifactID,
				ExecutionSessionId:       value.ExecutionSessionID,
				Ownership:                configurationOwnershipToProto(value.Ownership),
				AgentId:                  value.AgentID,
				AgentVersion:             value.AgentVersion,
				AgentSha256:              value.AgentSHA256,
				InstructionSetId:         value.InstructionSetID,
				InstructionSetVersion:    value.InstructionSetVersion,
				InstructionSetSha256:     value.InstructionSetSHA256,
				RuntimeSelectionRef:      value.RuntimeSelectionRef,
				RuntimeSelectionVersion:  value.RuntimeSelectionVersion,
				RuntimeSelectionSha256:   value.RuntimeSelectionSHA256,
				ProviderPoolId:           value.ProviderPoolID,
				ProviderPoolVersion:      value.ProviderPoolVersion,
				ProviderPoolSha256:       value.ProviderPoolSHA256,
				AgentAssignmentId:        value.AgentAssignmentID,
				AgentAssignmentVersion:   value.AgentAssignmentVersion,
				AgentAssignmentSha256:    value.AgentAssignmentSHA256,
			},
		}
	case entity.OwnerGateSpec:
		result.Value = &controlplanev1.ResourceSpec_OwnerGate{
			OwnerGate: &controlplanev1.OwnerGateSpec{
				ProcessRunId:                  value.ProcessRunID,
				ResultRef:                     value.ResultRef,
				ResultSha256:                  value.ResultSHA256,
				ExpiresAt:                     timestamppb.New(value.ExpiresAt),
				Decision:                      ownerDecision(value.Decision),
				DecisionReason:                value.DecisionReason,
				RootInitiatorActorId:          value.RootInitiatorActorID,
				SessionId:                     value.SessionID,
				TurnId:                        value.TurnID,
				Attempt:                       value.Attempt,
				ImmutableInputSha256:          value.ImmutableInputSHA256,
				RecipientActorId:              value.RecipientActorID,
				DeliveryWorkloadId:            value.DeliveryWorkloadID,
				DeliverySpiffeId:              value.DeliverySPIFFEID,
				DeliveryId:                    value.DeliveryID,
				DeliveryPayloadSha256:         value.DeliveryPayloadSHA256,
				DeliveryProviderReceiptSha256: value.DeliveryProviderReceiptSHA256,
				DeliveryClaimTokenSha256:      value.DeliveryClaimTokenSHA256,
				DeliveryFence:                 value.DeliveryFence,
				DeliveryClaimExpiresAt:        optionalTimestamp(value.DeliveryClaimExpiresAt),
				MattermostPostId:              value.MattermostPostID,
				MattermostChannelId:           value.MattermostChannelID,
				MattermostRootPostId:          value.MattermostRootPostID,
				DeliveredAt:                   optionalTimestamp(value.DeliveredAt),
				ScheduleId:                    value.ScheduleID,
				OccurrenceId:                  value.OccurrenceID,
				DecisionReceiptSha256:         value.DecisionReceiptSHA256,
				ContinuationTurnId:            value.ContinuationTurnID,
				ContinuationTurnVersion:       value.ContinuationTurnVersion,
				ContinuationInputSha256:       value.ContinuationInputSHA256,
				NotificationRoomId:            value.NotificationRoomID,
			},
		}
	case entity.MemoryRecordSpec:
		result.Value = &controlplanev1.ResourceSpec_MemoryRecord{
			MemoryRecord: &controlplanev1.MemoryRecordSpec{
				Scope:         value.Scope,
				RoleId:        value.RoleID,
				Title:         value.Title,
				Content:       value.Content,
				ContentSha256: value.ContentSHA256,
				Provenance:    value.Provenance,
				Importance:    value.Importance,
			},
		}
	case entity.WorkClaimSpec:
		result.Value = &controlplanev1.ResourceSpec_WorkClaim{
			WorkClaim: &controlplanev1.WorkClaimSpec{
				ProcessRunId:        value.ProcessRunID,
				TurnId:              value.TurnID,
				Summary:             value.Summary,
				Domains:             value.Domains,
				ResourceKeys:        value.ResourceKeys,
				OwnerActorId:        value.OwnerActorID,
				WorkloadId:          value.WorkloadID,
				SessionId:           value.SessionID,
				Attempt:             value.Attempt,
				AuthorityGeneration: value.AuthorityGeneration,
				ExpiresAt:           timestamppb.New(value.ExpiresAt),
			},
		}
	case entity.ArtifactSpec:
		result.Value = &controlplanev1.ResourceSpec_Artifact{
			Artifact: &controlplanev1.ArtifactSpec{
				Kind:               value.ArtifactKind,
				Direction:          value.Direction,
				StorageRef:         value.StorageRef,
				SizeBytes:          value.SizeBytes,
				MediaType:          value.MediaType,
				Sha256:             value.SHA256,
				ScanStatus:         artifactScanState(value.ScanStatus),
				RetentionPolicyRef: value.RetentionPolicyRef,
				ScanPolicyRevision: value.ScanPolicyRevision,
				ScanEvidenceSha256: value.ScanEvidenceSHA256,
				ScannerWorkloadId:  value.ScannerWorkloadID,
				ScannedAt:          optionalTimestamp(value.ScannedAt),
			},
		}
	case entity.RoleImageRecipeSpec:
		result.Value = &controlplanev1.ResourceSpec_RoleImageRecipe{
			RoleImageRecipe: &controlplanev1.RoleImageRecipeSpec{
				Input: roleImageInputToProto(value.Input), Generation: value.Generation,
				SpecSha256: value.SpecSHA256, PolicyRevision: value.PolicyRevision,
				PolicySha256:                value.PolicySHA256,
				RoleRuntimeContractRevision: value.RoleRuntimeContractRevision,
				RoleRuntimeContractSha256:   value.RoleRuntimeContractSHA256,
			},
		}
	case entity.ImageBuildSpec:
		result.Value = &controlplanev1.ResourceSpec_ImageBuild{
			ImageBuild: &controlplanev1.ImageBuildSpec{
				RecipeId: value.RecipeID, RecipeVersion: value.RecipeVersion, RecipeGeneration: value.RecipeGeneration,
				SpecSha256: value.SpecSHA256, Attempt: value.Attempt, ClaimantWorkloadId: value.ClaimantWorkloadID,
				ClaimantSpiffeId: value.ClaimantSPIFFEID, AuthorityGeneration: value.AuthorityGeneration,
				Fence: value.Fence, LeaseExpiresAt: optionalTimestamp(value.LeaseExpiresAt),
				Stage: imageBuildStageToProto(value.Stage), ProgressPercent: value.ProgressPercent,
				StagingReference: value.StagingReference, ManifestDigest: value.ManifestDigest,
				ProvenanceSha256: value.ProvenanceSHA256, ImmutableBuildSha256: value.ImmutableBuildSHA256,
				ErrorCode: value.ErrorCode, AvailableAt: timestamppb.New(value.AvailableAt), MaximumAttempts: value.MaximumAttempts,
				LeaseTokenSha256: value.LeaseTokenSHA256, ClaimJtiSha256: value.ClaimJTISHA256,
				DiagnosticCode: value.DiagnosticCode, DiagnosticSummary: value.DiagnosticSummary,
			},
		}
	case entity.ImageArtifactSpec:
		result.Value = &controlplanev1.ResourceSpec_ImageArtifact{
			ImageArtifact: &controlplanev1.ImageArtifactSpec{
				RecipeId: value.RecipeID, RecipeVersion: value.RecipeVersion, RecipeGeneration: value.RecipeGeneration,
				SpecSha256: value.SpecSHA256, BuildId: value.BuildID, BuildVersion: value.BuildVersion,
				BuildAttempt: value.BuildAttempt, StagingReference: value.StagingReference,
				ManifestDigest: value.ManifestDigest, ProvenanceSha256: value.ProvenanceSHA256,
				ImmutableBuildSha256: value.ImmutableBuildSHA256, SbomSha256: value.SBOMSHA256,
				BaseImageDigest: value.BaseImageDigest, SourceSha256: value.SourceSHA256,
				ContextSha256: value.ContextSHA256, BuilderSha256: value.BuilderSHA256,
				FrontendSha256: value.FrontendSHA256, ToolchainSha256: value.ToolchainSHA256,
				Platforms:                   roleImagePlatformsToProto(value.Platforms),
				VulnerabilityEvidenceSha256: value.VulnerabilityEvidenceSHA256,
				PolicyRevision:              value.PolicyRevision, PolicySha256: value.PolicySHA256,
				AdmissionVerdict:  imageAdmissionVerdictToProto(value.AdmissionVerdict),
				SignatureIdentity: value.SignatureIdentity, SignatureSha256: value.SignatureSHA256,
				AdmissionRevision: value.AdmissionRevision, AdmissionReceiptSha256: value.AdmissionReceiptSHA256,
				AdmissionReceiptOciManifestDigest: value.AdmissionReceiptOCIManifestDigest,
				RoleRuntimeContractRevision:       value.RoleRuntimeContractRevision,
				RoleRuntimeContractSha256:         value.RoleRuntimeContractSHA256,
				PromotionClaimJtiSha256:           value.PromotionClaimJTISHA256, PromotionClaimExpiresAt: optionalTimestamp(value.PromotionClaimExpiresAt),
				PromotionClaimantWorkloadId:  value.PromotionClaimantWorkloadID,
				PromotionClaimantSpiffeId:    value.PromotionClaimantSPIFFEID,
				PromotionAuthorityGeneration: value.PromotionAuthorityGeneration, PromotionFence: value.PromotionFence,
				PromotionAuthorizationTokenSha256: value.PromotionAuthorizationTokenSHA256,
				PromotionAuthorizationExpiresAt:   optionalTimestamp(value.PromotionAuthorizationExpiresAt),
				PromotedReference:                 value.PromotedReference, PromotionReadbackSha256: value.PromotionReadbackSHA256,
				PromotedAt: optionalTimestamp(value.PromotedAt), AdmissionClaimantWorkloadId: value.AdmissionClaimantWorkloadID,
				AdmissionAuthorityGeneration: value.AdmissionAuthorityGeneration, AdmissionFence: value.AdmissionFence,
				AdmissionClaimTokenSha256: value.AdmissionClaimTokenSHA256, AdmissionClaimExpiresAt: optionalTimestamp(value.AdmissionClaimExpiresAt),
			},
		}
	case entity.RoleDefinitionSpec:
		result.Value = &controlplanev1.ResourceSpec_RoleDefinition{RoleDefinition: roleDefinitionToProto(value)}
	case entity.AgentSpec:
		result.Value = &controlplanev1.ResourceSpec_Agent{Agent: agentToProto(value)}
	case entity.AgentAssignmentSpec:
		result.Value = &controlplanev1.ResourceSpec_AgentAssignment{AgentAssignment: agentAssignmentToProto(value)}
	case entity.InstructionSetSpec:
		result.Value = &controlplanev1.ResourceSpec_InstructionSet{InstructionSet: instructionSetToProto(value)}
	case entity.ProviderConnectionReferenceSpec:
		result.Value = &controlplanev1.ResourceSpec_ProviderConnectionReference{ProviderConnectionReference: providerReferenceToProto(value)}
	case entity.ProviderPoolSpec:
		result.Value = &controlplanev1.ResourceSpec_ProviderPool{ProviderPool: providerPoolToProto(value)}
	case entity.WorkspaceBackupSpec:
		result.Value = &controlplanev1.ResourceSpec_WorkspaceBackup{WorkspaceBackup: workspaceBackupToProto(value)}
	case entity.WorkspaceRestoreSpec:
		result.Value = &controlplanev1.ResourceSpec_WorkspaceRestore{WorkspaceRestore: workspaceRestoreToProto(value)}
	case entity.WorkspaceMattermostMappingSpec:
		result.Value = &controlplanev1.ResourceSpec_WorkspaceMattermostMapping{WorkspaceMattermostMapping: workspaceMappingToProto(value)}
	default:
		return nil, errors.New("domain resource specification is unknown")
	}
	return result, nil
}

func requiredTime(value *timestamppb.Timestamp) (time.Time, error) {
	if value == nil || value.CheckValid() != nil {
		return time.Time{}, errors.New("timestamp is invalid")
	}
	return value.AsTime().UTC().Truncate(time.Microsecond), nil
}

func optionalTime(value *timestamppb.Timestamp) (time.Time, error) {
	if value == nil {
		return time.Time{}, nil
	}
	return requiredTime(value)
}

func artifactScanState(value string) controlplanev1.ArtifactScanState {
	switch value {
	case "PENDING":
		return controlplanev1.ArtifactScanState_ARTIFACT_SCAN_STATE_PENDING
	case "SCANNING":
		return controlplanev1.ArtifactScanState_ARTIFACT_SCAN_STATE_SCANNING
	case "CLEAN":
		return controlplanev1.ArtifactScanState_ARTIFACT_SCAN_STATE_CLEAN
	case "QUARANTINED":
		return controlplanev1.ArtifactScanState_ARTIFACT_SCAN_STATE_QUARANTINED
	case "FAILED":
		return controlplanev1.ArtifactScanState_ARTIFACT_SCAN_STATE_FAILED
	default:
		return controlplanev1.ArtifactScanState_ARTIFACT_SCAN_STATE_UNSPECIFIED
	}
}

func optionalDuration(value *durationpb.Duration) (time.Duration, error) {
	if value == nil {
		return 0, nil
	}
	if value.CheckValid() != nil {
		return 0, errors.New("duration is invalid")
	}
	return value.AsDuration(), nil
}

func optionalTimestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() || value.Equal(time.Unix(0, 0)) {
		return nil
	}
	return timestamppb.New(value)
}

func roleImageInputFromProto(input *controlplanev1.RoleImageRecipeInput) (entity.RoleImageRecipeInput, error) {
	if input == nil {
		return entity.RoleImageRecipeInput{}, errors.New("role image recipe input is absent")
	}
	result := entity.RoleImageRecipeInput{
		BaseImageReference: input.GetBaseImageReference(), BaseImageDigest: input.GetBaseImageDigest(),
		SourceRef: input.GetSourceRef(), SourceRevision: input.GetSourceRevision(), SourceSHA256: input.GetSourceSha256(),
		ContextRef: input.GetContextRef(), ContextSHA256: input.GetContextSha256(),
		BuilderSHA256: input.GetBuilderSha256(), FrontendSHA256: input.GetFrontendSha256(),
		InstallationBlock: input.GetInstallationBlock(),
		ToolchainSHA256:   input.GetToolchainSha256(),
	}
	for _, platform := range input.GetPlatforms() {
		result.Platforms = append(result.Platforms, entity.RoleImagePlatform{
			OS: platform.GetOs(), Architecture: platform.GetArchitecture(), Variant: platform.GetVariant(),
		})
	}
	for _, item := range input.GetPackages() {
		result.Packages = append(result.Packages, entity.RoleImagePackage{
			Manager: item.GetManager(), Name: item.GetName(), Version: item.GetVersion(), Digest: item.GetDigest(), SourceRef: item.GetSourceRef(),
		})
	}
	for _, item := range input.GetTools() {
		result.Tools = append(result.Tools, entity.RoleImageTool{
			Name: item.GetName(), Version: item.GetVersion(), SourceRef: item.GetSourceRef(), SHA256: item.GetSha256(),
		})
	}
	if result.Validate() != nil {
		return entity.RoleImageRecipeInput{}, errors.New("role image recipe input is invalid")
	}
	return result, nil
}

func roleImageInputToProto(input entity.RoleImageRecipeInput) *controlplanev1.RoleImageRecipeInput {
	result := &controlplanev1.RoleImageRecipeInput{
		BaseImageReference: input.BaseImageReference, BaseImageDigest: input.BaseImageDigest,
		SourceRef: input.SourceRef, SourceRevision: input.SourceRevision, SourceSha256: input.SourceSHA256,
		ContextRef: input.ContextRef, ContextSha256: input.ContextSHA256,
		BuilderSha256: input.BuilderSHA256, FrontendSha256: input.FrontendSHA256,
		InstallationBlock: input.InstallationBlock,
		ToolchainSha256:   input.ToolchainSHA256,
	}
	for _, platform := range input.Platforms {
		result.Platforms = append(result.Platforms, &controlplanev1.RoleImagePlatform{
			Os: platform.OS, Architecture: platform.Architecture, Variant: platform.Variant,
		})
	}
	for _, item := range input.Packages {
		result.Packages = append(result.Packages, &controlplanev1.RoleImagePackage{
			Manager: item.Manager, Name: item.Name, Version: item.Version, Digest: item.Digest, SourceRef: item.SourceRef,
		})
	}
	for _, item := range input.Tools {
		result.Tools = append(result.Tools, &controlplanev1.RoleImageTool{
			Name: item.Name, Version: item.Version, SourceRef: item.SourceRef, Sha256: item.SHA256,
		})
	}
	return result
}

func roleImagePlatformsFromProto(platforms []*controlplanev1.RoleImagePlatform) []entity.RoleImagePlatform {
	result := make([]entity.RoleImagePlatform, 0, len(platforms))
	for _, platform := range platforms {
		result = append(result, entity.RoleImagePlatform{
			OS: platform.GetOs(), Architecture: platform.GetArchitecture(), Variant: platform.GetVariant(),
		})
	}
	return result
}

func roleImagePlatformsToProto(platforms []entity.RoleImagePlatform) []*controlplanev1.RoleImagePlatform {
	result := make([]*controlplanev1.RoleImagePlatform, 0, len(platforms))
	for _, platform := range platforms {
		result = append(result, &controlplanev1.RoleImagePlatform{
			Os: platform.OS, Architecture: platform.Architecture, Variant: platform.Variant,
		})
	}
	return result
}

func imageBuildStageFromProto(stage controlplanev1.ImageBuildStage) entity.ImageBuildStage {
	return entity.ImageBuildStage(trimEnum(stage.String(), "IMAGE_BUILD_STAGE_"))
}

func imageBuildStageToProto(stage entity.ImageBuildStage) controlplanev1.ImageBuildStage {
	value, exists := controlplanev1.ImageBuildStage_value["IMAGE_BUILD_STAGE_"+string(stage)]
	if !exists {
		return controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_UNSPECIFIED
	}
	return controlplanev1.ImageBuildStage(value)
}

func imageAdmissionVerdictFromProto(verdict controlplanev1.ImageAdmissionVerdict) entity.ImageAdmissionVerdict {
	if verdict == controlplanev1.ImageAdmissionVerdict_IMAGE_ADMISSION_VERDICT_UNSPECIFIED {
		return ""
	}
	return entity.ImageAdmissionVerdict(trimEnum(verdict.String(), "IMAGE_ADMISSION_VERDICT_"))
}

func imageAdmissionVerdictToProto(verdict entity.ImageAdmissionVerdict) controlplanev1.ImageAdmissionVerdict {
	value, exists := controlplanev1.ImageAdmissionVerdict_value["IMAGE_ADMISSION_VERDICT_"+string(verdict)]
	if !exists {
		return controlplanev1.ImageAdmissionVerdict_IMAGE_ADMISSION_VERDICT_UNSPECIFIED
	}
	return controlplanev1.ImageAdmissionVerdict(value)
}

func timestampOrZero(value *timestamppb.Timestamp) time.Time {
	if value == nil || !value.IsValid() {
		return time.Time{}
	}
	return value.AsTime()
}

func optionalProtoDuration(value time.Duration) *durationpb.Duration {
	if value == 0 {
		return nil
	}
	return durationpb.New(value)
}

func trimEnum(value, prefix string) string {
	if len(value) <= len(prefix) || value[:len(prefix)] != prefix {
		return ""
	}
	return value[len(prefix):]
}

func roomType(value string) controlplanev1.RoomType {
	return controlplanev1.RoomType(controlplanev1.RoomType_value["ROOM_TYPE_"+value])
}

func overlapPolicy(value string) controlplanev1.ScheduleOverlapPolicy {
	return controlplanev1.ScheduleOverlapPolicy(
		controlplanev1.ScheduleOverlapPolicy_value["SCHEDULE_OVERLAP_POLICY_"+value],
	)
}

func misfirePolicy(value string) controlplanev1.ScheduleMisfirePolicy {
	return controlplanev1.ScheduleMisfirePolicy(
		controlplanev1.ScheduleMisfirePolicy_value["SCHEDULE_MISFIRE_POLICY_"+value],
	)
}

func scheduleSessionPolicy(value string) controlplanev1.ScheduleSessionPolicy {
	return controlplanev1.ScheduleSessionPolicy(
		controlplanev1.ScheduleSessionPolicy_value["SCHEDULE_SESSION_POLICY_"+value],
	)
}

func scheduleTargetType(value string) controlplanev1.ScheduleTargetType {
	if result, ok := controlplanev1.ScheduleTargetType_value["SCHEDULE_TARGET_TYPE_"+value]; ok {
		return controlplanev1.ScheduleTargetType(result)
	}
	return controlplanev1.ScheduleTargetType_SCHEDULE_TARGET_TYPE_UNSPECIFIED
}

func scheduleNotificationPolicy(value string) controlplanev1.ScheduleNotificationPolicy {
	return controlplanev1.ScheduleNotificationPolicy(
		controlplanev1.ScheduleNotificationPolicy_value["SCHEDULE_NOTIFICATION_POLICY_"+value],
	)
}

func configurationOwnershipFromProto(
	ownership *controlplanev1.ConfigurationOwnership,
) entity.ConfigurationOwnership {
	if ownership == nil {
		return entity.ConfigurationOwnership{}
	}
	return entity.ConfigurationOwnership{
		ManagedBy: strings.TrimPrefix(
			ownership.GetManagedBy().String(),
			"CONFIGURATION_MANAGER_",
		),
		SourceRef:      ownership.GetSourceRef(),
		SourceRevision: ownership.GetSourceRevision(),
		SourceSHA256:   ownership.GetSourceSha256(),
	}
}

func configurationOwnershipToProto(
	ownership entity.ConfigurationOwnership,
) *controlplanev1.ConfigurationOwnership {
	return &controlplanev1.ConfigurationOwnership{
		ManagedBy: controlplanev1.ConfigurationManager(
			controlplanev1.ConfigurationManager_value["CONFIGURATION_MANAGER_"+ownership.ManagedBy],
		),
		SourceRef:      ownership.SourceRef,
		SourceRevision: ownership.SourceRevision,
		SourceSha256:   ownership.SourceSHA256,
	}
}

func scheduledResultContractFromProto(
	contract *controlplanev1.ScheduledResultContract,
) *entity.ScheduledResultContractRef {
	if contract == nil {
		return nil
	}
	return &entity.ScheduledResultContractRef{
		Schema: contract.GetSchema(), Path: contract.GetPath(), Format: contract.GetFormat(),
		SchemaSHA256: contract.GetSchemaSha256(), MaximumBytes: contract.GetMaximumBytes(),
	}
}

func scheduledResultContractToProto(
	contract *entity.ScheduledResultContractRef,
) *controlplanev1.ScheduledResultContract {
	if contract == nil {
		return nil
	}
	return &controlplanev1.ScheduledResultContract{
		Schema: contract.Schema, Path: contract.Path, Format: contract.Format,
		SchemaSha256: contract.SchemaSHA256, MaximumBytes: contract.MaximumBytes,
	}
}

func fromProtoProcessContinuationKind(
	kind controlplanev1.ProcessContinuationKind,
) enum.ProcessContinuationKind {
	switch kind {
	case controlplanev1.ProcessContinuationKind_PROCESS_CONTINUATION_KIND_OWNER_GATE:
		return enum.ProcessContinuationOwnerGate
	case controlplanev1.ProcessContinuationKind_PROCESS_CONTINUATION_KIND_INTEGRATION:
		return enum.ProcessContinuationIntegration
	case controlplanev1.ProcessContinuationKind_PROCESS_CONTINUATION_KIND_UNSPECIFIED:
		return enum.ProcessContinuationNone
	default:
		// Неизвестное wire-значение сохраняется как invalid domain enum,
		// чтобы Validate закрыло запрос до семантического использования.
		return enum.ProcessContinuationKind(kind.String())
	}
}

func toProtoProcessContinuationKind(
	kind enum.ProcessContinuationKind,
) controlplanev1.ProcessContinuationKind {
	switch kind {
	case enum.ProcessContinuationOwnerGate:
		return controlplanev1.ProcessContinuationKind_PROCESS_CONTINUATION_KIND_OWNER_GATE
	case enum.ProcessContinuationIntegration:
		return controlplanev1.ProcessContinuationKind_PROCESS_CONTINUATION_KIND_INTEGRATION
	default:
		return controlplanev1.ProcessContinuationKind_PROCESS_CONTINUATION_KIND_UNSPECIFIED
	}
}

func ownerDecision(value string) controlplanev1.OwnerGateDecision {
	if value == "" {
		return controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_UNSPECIFIED
	}
	return controlplanev1.OwnerGateDecision(
		controlplanev1.OwnerGateDecision_value["OWNER_GATE_DECISION_"+value],
	)
}

func ownerDecisionFromProto(value controlplanev1.OwnerGateDecision) string {
	if value == controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_UNSPECIFIED {
		return ""
	}
	return trimEnum(value.String(), "OWNER_GATE_DECISION_")
}
