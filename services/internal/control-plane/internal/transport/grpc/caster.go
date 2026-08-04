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
			Ownership: configurationOwnershipFromProto(value.Role.GetOwnership()),
		}, nil
	case *controlplanev1.ResourceSpec_PromptProfile:
		return entity.PromptProfileSpec{
			Revision:      value.PromptProfile.GetRevision(),
			ContentSHA256: value.PromptProfile.GetContentSha256(),
			SourceRef:     value.PromptProfile.GetSourceRef(),
			Locale:        value.PromptProfile.GetLocale(),
			Ownership:     configurationOwnershipFromProto(value.PromptProfile.GetOwnership()),
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
			ManifestSHA256:              value.RuntimeRevision.GetManifestSha256(),
			ImageDigest:                 value.RuntimeRevision.GetImageDigest(),
			SessionID:                   value.RuntimeRevision.GetSessionId(),
			RoleID:                      value.RuntimeRevision.GetRoleId(),
			ChatID:                      value.RuntimeRevision.GetChatId(),
			ProviderCredentialBindingID: value.RuntimeRevision.GetProviderCredentialBindingId(),
			EffectiveRuntimeSHA256:      value.RuntimeRevision.GetEffectiveRuntimeSha256(),
			PromptProfileID:             value.RuntimeRevision.GetPromptProfileId(),
			PromptRevision:              value.RuntimeRevision.GetPromptRevision(),
			CredentialBindingIDs:        value.RuntimeRevision.GetCredentialBindingIds(),
			IntegrationIDs:              value.RuntimeRevision.GetIntegrationIds(),
			PredecessorRevisionID:       value.RuntimeRevision.GetPredecessorRevisionId(),
			AuthorityPolicyVersion:      value.RuntimeRevision.GetAuthorityPolicyRevision(),
			AuthorityPolicySHA256:       value.RuntimeRevision.GetAuthorityPolicySha256(),
			Components:                  components,
			CreatedAt:                   createdAt,
		}, nil
	case *controlplanev1.ResourceSpec_Session:
		return entity.SessionSpec{
			AgentID:                    value.Session.GetAgentId(),
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
		return entity.TurnSpec{
			SessionID:               value.Turn.GetSessionId(),
			Sequence:                value.Turn.GetSequence(),
			SourceRef:               value.Turn.GetSourceRef(),
			PromptArtifactID:        value.Turn.GetPromptArtifactId(),
			RuntimeRevisionID:       value.Turn.GetRuntimeRevisionId(),
			ProcessRunID:            value.Turn.GetProcessRunId(),
			Attempt:                 value.Turn.GetAttempt(),
			Outcome:                 value.Turn.GetOutcome(),
			ResultArtifactID:        value.Turn.GetResultArtifactId(),
			ResultArtifactVersion:   value.Turn.GetResultArtifactVersion(),
			ResultArtifactSHA256:    value.Turn.GetResultArtifactSha256(),
			EffectiveInputSHA256:    value.Turn.GetEffectiveInputSha256(),
			PredecessorTurnID:       value.Turn.GetPredecessorTurnId(),
			OwnerFeedback:           value.Turn.GetOwnerFeedback(),
			OwnerFeedbackGateID:     value.Turn.GetOwnerFeedbackGateId(),
			OwnerFeedbackVersion:    value.Turn.GetOwnerFeedbackGateVersion(),
			OwnerFeedbackSHA256:     value.Turn.GetOwnerFeedbackSha256(),
			AgentSessionTurnID:      value.Turn.GetAgentSessionTurnId(),
			AgentRunID:              value.Turn.GetAgentRunId(),
			AgentTurnBindingVersion: value.Turn.GetAgentTurnBindingVersion(),
			AgentTurnBindingSHA256:  value.Turn.GetAgentTurnBindingSha256(),
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
		nextRunAt, err := requiredTime(value.Schedule.GetNextRunAt())
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
			PlaybookRef:        value.Schedule.GetPlaybookRef(),
			PlaybookVersion:    value.Schedule.GetPlaybookVersion(),
			PromptArtifactID:   value.Schedule.GetPromptArtifactId(),
			ExecutionSessionID: value.Schedule.GetExecutionSessionId(),
			Ownership:          configurationOwnershipFromProto(value.Schedule.GetOwnership()),
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
			ProcessRunID:             value.OwnerGate.GetProcessRunId(),
			ResultRef:                value.OwnerGate.GetResultRef(),
			ResultSHA256:             value.OwnerGate.GetResultSha256(),
			ExpiresAt:                expiresAt,
			Decision:                 ownerDecisionFromProto(value.OwnerGate.GetDecision()),
			DecisionReason:           value.OwnerGate.GetDecisionReason(),
			RootInitiatorActorID:     value.OwnerGate.GetRootInitiatorActorId(),
			SessionID:                value.OwnerGate.GetSessionId(),
			TurnID:                   value.OwnerGate.GetTurnId(),
			Attempt:                  value.OwnerGate.GetAttempt(),
			ImmutableInputSHA256:     value.OwnerGate.GetImmutableInputSha256(),
			RecipientActorID:         value.OwnerGate.GetRecipientActorId(),
			DeliveryWorkloadID:       value.OwnerGate.GetDeliveryWorkloadId(),
			DeliverySPIFFEID:         value.OwnerGate.GetDeliverySpiffeId(),
			DeliveryID:               value.OwnerGate.GetDeliveryId(),
			DeliveryPayloadSHA256:    value.OwnerGate.GetDeliveryPayloadSha256(),
			DeliveryClaimTokenSHA256: value.OwnerGate.GetDeliveryClaimTokenSha256(),
			DeliveryFence:            value.OwnerGate.GetDeliveryFence(),
			DeliveryClaimExpiresAt:   claimExpiresAt,
			MattermostPostID:         value.OwnerGate.GetMattermostPostId(),
			MattermostChannelID:      value.OwnerGate.GetMattermostChannelId(),
			MattermostRootPostID:     value.OwnerGate.GetMattermostRootPostId(),
			DeliveredAt:              deliveredAt,
			ScheduleID:               value.OwnerGate.GetScheduleId(),
			OccurrenceID:             value.OwnerGate.GetOccurrenceId(),
			DecisionReceiptSHA256:    value.OwnerGate.GetDecisionReceiptSha256(),
			ContinuationTurnID:       value.OwnerGate.GetContinuationTurnId(),
			ContinuationTurnVersion:  value.OwnerGate.GetContinuationTurnVersion(),
			ContinuationInputSHA256:  value.OwnerGate.GetContinuationInputSha256(),
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
	default:
		return nil, errors.New("resource specification is unknown")
	}
}

func toProtoResource(resource entity.Resource) (*controlplanev1.Resource, error) {
	spec, err := toProtoSpec(resource.Spec)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.Resource{
		Id:        resource.ID,
		Kind:      toProtoKind(resource.Kind),
		Name:      resource.Name,
		State:     toProtoState(resource.State),
		Version:   resource.Version,
		ProjectId: resource.ProjectID,
		ParentId:  resource.ParentID,
		Spec:      spec,
		CreatedAt: timestamppb.New(resource.CreatedAt),
		UpdatedAt: timestamppb.New(resource.UpdatedAt),
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
				Ownership: configurationOwnershipToProto(value.Ownership),
			},
		}
	case entity.PromptProfileSpec:
		result.Value = &controlplanev1.ResourceSpec_PromptProfile{
			PromptProfile: &controlplanev1.PromptProfileSpec{
				Revision:      value.Revision,
				ContentSha256: value.ContentSHA256,
				SourceRef:     value.SourceRef,
				Locale:        value.Locale,
				Ownership:     configurationOwnershipToProto(value.Ownership),
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
				RepositoryRef:       value.RepositoryRef,
				WorkspaceMode:       value.WorkspaceMode,
				DefaultBranch:       value.DefaultBranch,
				CredentialBindingId: value.CredentialBindingID,
				Ownership:           configurationOwnershipToProto(value.Ownership),
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
				ManifestSha256:              value.ManifestSHA256,
				ImageDigest:                 value.ImageDigest,
				SessionId:                   value.SessionID,
				RoleId:                      value.RoleID,
				ChatId:                      value.ChatID,
				ProviderCredentialBindingId: value.ProviderCredentialBindingID,
				EffectiveRuntimeSha256:      value.EffectiveRuntimeSHA256,
				PromptProfileId:             value.PromptProfileID,
				PromptRevision:              value.PromptRevision,
				CredentialBindingIds:        value.CredentialBindingIDs,
				IntegrationIds:              value.IntegrationIDs,
				PredecessorRevisionId:       value.PredecessorRevisionID,
				AuthorityPolicyRevision:     value.AuthorityPolicyVersion,
				AuthorityPolicySha256:       value.AuthorityPolicySHA256,
				Components:                  components,
				CreatedAt:                   timestamppb.New(value.CreatedAt),
			},
		}
	case entity.SessionSpec:
		result.Value = &controlplanev1.ResourceSpec_Session{
			Session: &controlplanev1.SessionSpec{
				AgentId:                    value.AgentID,
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
		result.Value = &controlplanev1.ResourceSpec_Turn{
			Turn: &controlplanev1.TurnSpec{
				SessionId:                value.SessionID,
				Sequence:                 value.Sequence,
				SourceRef:                value.SourceRef,
				PromptArtifactId:         value.PromptArtifactID,
				RuntimeRevisionId:        value.RuntimeRevisionID,
				ProcessRunId:             value.ProcessRunID,
				Attempt:                  value.Attempt,
				Outcome:                  value.Outcome,
				ResultArtifactId:         value.ResultArtifactID,
				ResultArtifactVersion:    value.ResultArtifactVersion,
				ResultArtifactSha256:     value.ResultArtifactSHA256,
				EffectiveInputSha256:     value.EffectiveInputSHA256,
				PredecessorTurnId:        value.PredecessorTurnID,
				OwnerFeedback:            value.OwnerFeedback,
				OwnerFeedbackGateId:      value.OwnerFeedbackGateID,
				OwnerFeedbackGateVersion: value.OwnerFeedbackVersion,
				OwnerFeedbackSha256:      value.OwnerFeedbackSHA256,
				AgentSessionTurnId:       value.AgentSessionTurnID,
				AgentRunId:               value.AgentRunID,
				AgentTurnBindingVersion:  value.AgentTurnBindingVersion,
				AgentTurnBindingSha256:   value.AgentTurnBindingSHA256,
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
			},
		}
	case entity.OwnerGateSpec:
		result.Value = &controlplanev1.ResourceSpec_OwnerGate{
			OwnerGate: &controlplanev1.OwnerGateSpec{
				ProcessRunId:             value.ProcessRunID,
				ResultRef:                value.ResultRef,
				ResultSha256:             value.ResultSHA256,
				ExpiresAt:                timestamppb.New(value.ExpiresAt),
				Decision:                 ownerDecision(value.Decision),
				DecisionReason:           value.DecisionReason,
				RootInitiatorActorId:     value.RootInitiatorActorID,
				SessionId:                value.SessionID,
				TurnId:                   value.TurnID,
				Attempt:                  value.Attempt,
				ImmutableInputSha256:     value.ImmutableInputSHA256,
				RecipientActorId:         value.RecipientActorID,
				DeliveryWorkloadId:       value.DeliveryWorkloadID,
				DeliverySpiffeId:         value.DeliverySPIFFEID,
				DeliveryId:               value.DeliveryID,
				DeliveryPayloadSha256:    value.DeliveryPayloadSHA256,
				DeliveryClaimTokenSha256: value.DeliveryClaimTokenSHA256,
				DeliveryFence:            value.DeliveryFence,
				DeliveryClaimExpiresAt:   optionalTimestamp(value.DeliveryClaimExpiresAt),
				MattermostPostId:         value.MattermostPostID,
				MattermostChannelId:      value.MattermostChannelID,
				MattermostRootPostId:     value.MattermostRootPostID,
				DeliveredAt:              optionalTimestamp(value.DeliveredAt),
				ScheduleId:               value.ScheduleID,
				OccurrenceId:             value.OccurrenceID,
				DecisionReceiptSha256:    value.DecisionReceiptSHA256,
				ContinuationTurnId:       value.ContinuationTurnID,
				ContinuationTurnVersion:  value.ContinuationTurnVersion,
				ContinuationInputSha256:  value.ContinuationInputSHA256,
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
