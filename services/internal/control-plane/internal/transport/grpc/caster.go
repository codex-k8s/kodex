package grpc

import (
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	controlplanev1 "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/generated/controlplane/v1"
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
		}, nil
	case *controlplanev1.ResourceSpec_Team:
		return entity.TeamSpec{
			StableKey:       value.Team.GetStableKey(),
			ExternalTeamRef: value.Team.GetExternalTeamRef(),
			MemberActorIDs:  value.Team.GetMemberActorIds(),
			RoleIDs:         value.Team.GetRoleIds(),
		}, nil
	case *controlplanev1.ResourceSpec_Chat:
		return entity.ChatSpec{
			StableKey:          value.Chat.GetStableKey(),
			RoomType:           value.Chat.GetRoomType().String()[len("ROOM_TYPE_"):],
			DefaultAgentID:     value.Chat.GetDefaultAgentId(),
			ExternalChannelRef: value.Chat.GetExternalChannelRef(),
			WorkPolicy:         value.Chat.GetWorkPolicy(),
		}, nil
	case *controlplanev1.ResourceSpec_Role:
		return entity.RoleSpec{
			StableKey:            value.Role.GetStableKey(),
			Capabilities:         value.Role.GetCapabilities(),
			AllowedTargetRoleIDs: value.Role.GetAllowedTargetRoleIds(),
			PromptProfileID:      value.Role.GetPromptProfileId(),
		}, nil
	case *controlplanev1.ResourceSpec_PromptProfile:
		return entity.PromptProfileSpec{
			Revision:      value.PromptProfile.GetRevision(),
			ContentSHA256: value.PromptProfile.GetContentSha256(),
			SourceRef:     value.PromptProfile.GetSourceRef(),
			Locale:        value.PromptProfile.GetLocale(),
		}, nil
	case *controlplanev1.ResourceSpec_CredentialBinding:
		expiresAt, err := optionalTime(value.CredentialBinding.GetExpiresAt())
		if err != nil {
			return nil, err
		}
		return entity.CredentialBindingSpec{
			Purpose:      value.CredentialBinding.GetPurpose(),
			SecretRef:    value.CredentialBinding.GetSecretRef(),
			PrincipalRef: value.CredentialBinding.GetPrincipalRef(),
			Revision:     value.CredentialBinding.GetRevision(),
			ExpiresAt:    expiresAt,
		}, nil
	case *controlplanev1.ResourceSpec_RepositoryWorkspace:
		return entity.RepositoryWorkspaceSpec{
			RepositoryRef:       value.RepositoryWorkspace.GetRepositoryRef(),
			WorkspaceMode:       value.RepositoryWorkspace.GetWorkspaceMode(),
			DefaultBranch:       value.RepositoryWorkspace.GetDefaultBranch(),
			CredentialBindingID: value.RepositoryWorkspace.GetCredentialBindingId(),
		}, nil
	case *controlplanev1.ResourceSpec_Integration:
		return entity.IntegrationSpec{
			DefinitionRef:        value.Integration.GetDefinitionRef(),
			DefinitionVersion:    value.Integration.GetDefinitionVersion(),
			Capabilities:         value.Integration.GetCapabilities(),
			CredentialBindingIDs: value.Integration.GetCredentialBindingIds(),
			EndpointRef:          value.Integration.GetEndpointRef(),
		}, nil
	case *controlplanev1.ResourceSpec_RuntimeRevision:
		return entity.RuntimeRevisionSpec{
			ManifestSHA256:       value.RuntimeRevision.GetManifestSha256(),
			ImageDigest:          value.RuntimeRevision.GetImageDigest(),
			PromptProfileID:      value.RuntimeRevision.GetPromptProfileId(),
			PromptRevision:       value.RuntimeRevision.GetPromptRevision(),
			CredentialBindingIDs: value.RuntimeRevision.GetCredentialBindingIds(),
			IntegrationIDs:       value.RuntimeRevision.GetIntegrationIds(),
		}, nil
	case *controlplanev1.ResourceSpec_Session:
		return entity.SessionSpec{
			AgentID:                  value.Session.GetAgentId(),
			ProviderAccountBindingID: value.Session.GetProviderAccountBindingId(),
			ConversationID:           value.Session.GetConversationId(),
			ArchiveRef:               value.Session.GetArchiveRef(),
			LastTurnSequence:         value.Session.GetLastTurnSequence(),
		}, nil
	case *controlplanev1.ResourceSpec_Turn:
		return entity.TurnSpec{
			SessionID:         value.Turn.GetSessionId(),
			Sequence:          value.Turn.GetSequence(),
			SourceRef:         value.Turn.GetSourceRef(),
			PromptArtifactID:  value.Turn.GetPromptArtifactId(),
			RuntimeRevisionID: value.Turn.GetRuntimeRevisionId(),
			ProcessRunID:      value.Turn.GetProcessRunId(),
			Attempt:           value.Turn.GetAttempt(),
			Outcome:           value.Turn.GetOutcome(),
			ResultArtifactID:  value.Turn.GetResultArtifactId(),
		}, nil
	case *controlplanev1.ResourceSpec_ProcessRun:
		return entity.ProcessRunSpec{
			ParentProcessRunID: value.ProcessRun.GetParentProcessRunId(),
			PlaybookRef:        value.ProcessRun.GetPlaybookRef(),
			PolicyRevision:     value.ProcessRun.GetPolicyRevision(),
			RootTriggerRef:     value.ProcessRun.GetRootTriggerRef(),
			ResultArtifactID:   value.ProcessRun.GetResultArtifactId(),
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
		return entity.ScheduleSpec{
			TargetResourceID: value.Schedule.GetTargetResourceId(),
			Cron:             value.Schedule.GetCron(),
			Interval:         interval,
			Timezone:         value.Schedule.GetTimezone(),
			OverlapPolicy: trimEnum(
				value.Schedule.GetOverlapPolicy().String(),
				"SCHEDULE_OVERLAP_POLICY_",
			),
			MisfirePolicy: trimEnum(
				value.Schedule.GetMisfirePolicy().String(),
				"SCHEDULE_MISFIRE_POLICY_",
			),
			MisfireGrace: misfireGrace,
			NextRunAt:    nextRunAt,
		}, nil
	case *controlplanev1.ResourceSpec_OwnerGate:
		expiresAt, err := requiredTime(value.OwnerGate.GetExpiresAt())
		if err != nil {
			return nil, err
		}
		return entity.OwnerGateSpec{
			ProcessRunID:   value.OwnerGate.GetProcessRunId(),
			ResultRef:      value.OwnerGate.GetResultRef(),
			ResultSHA256:   value.OwnerGate.GetResultSha256(),
			ExpiresAt:      expiresAt,
			Decision:       ownerDecisionFromProto(value.OwnerGate.GetDecision()),
			DecisionReason: value.OwnerGate.GetDecisionReason(),
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
		return entity.WorkClaimSpec{
			ProcessRunID: value.WorkClaim.GetProcessRunId(),
			TurnID:       value.WorkClaim.GetTurnId(),
			Summary:      value.WorkClaim.GetSummary(),
			Domains:      value.WorkClaim.GetDomains(),
			ResourceKeys: value.WorkClaim.GetResourceKeys(),
		}, nil
	case *controlplanev1.ResourceSpec_Artifact:
		return entity.ArtifactSpec{
			ArtifactKind:       value.Artifact.GetKind(),
			Direction:          value.Artifact.GetDirection(),
			StorageRef:         value.Artifact.GetStorageRef(),
			SizeBytes:          value.Artifact.GetSizeBytes(),
			MediaType:          value.Artifact.GetMediaType(),
			SHA256:             value.Artifact.GetSha256(),
			ScanStatus:         value.Artifact.GetScanStatus(),
			RetentionPolicyRef: value.Artifact.GetRetentionPolicyRef(),
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
			},
		}
	case entity.TeamSpec:
		result.Value = &controlplanev1.ResourceSpec_Team{
			Team: &controlplanev1.TeamSpec{
				StableKey:       value.StableKey,
				ExternalTeamRef: value.ExternalTeamRef,
				MemberActorIds:  value.MemberActorIDs,
				RoleIds:         value.RoleIDs,
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
			},
		}
	case entity.RoleSpec:
		result.Value = &controlplanev1.ResourceSpec_Role{
			Role: &controlplanev1.RoleSpec{
				StableKey:            value.StableKey,
				Capabilities:         value.Capabilities,
				AllowedTargetRoleIds: value.AllowedTargetRoleIDs,
				PromptProfileId:      value.PromptProfileID,
			},
		}
	case entity.PromptProfileSpec:
		result.Value = &controlplanev1.ResourceSpec_PromptProfile{
			PromptProfile: &controlplanev1.PromptProfileSpec{
				Revision:      value.Revision,
				ContentSha256: value.ContentSHA256,
				SourceRef:     value.SourceRef,
				Locale:        value.Locale,
			},
		}
	case entity.CredentialBindingSpec:
		result.Value = &controlplanev1.ResourceSpec_CredentialBinding{
			CredentialBinding: &controlplanev1.CredentialBindingSpec{
				Purpose:      value.Purpose,
				SecretRef:    value.SecretRef,
				PrincipalRef: value.PrincipalRef,
				Revision:     value.Revision,
				ExpiresAt:    optionalTimestamp(value.ExpiresAt),
			},
		}
	case entity.RepositoryWorkspaceSpec:
		result.Value = &controlplanev1.ResourceSpec_RepositoryWorkspace{
			RepositoryWorkspace: &controlplanev1.RepositoryWorkspaceSpec{
				RepositoryRef:       value.RepositoryRef,
				WorkspaceMode:       value.WorkspaceMode,
				DefaultBranch:       value.DefaultBranch,
				CredentialBindingId: value.CredentialBindingID,
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
			},
		}
	case entity.RuntimeRevisionSpec:
		result.Value = &controlplanev1.ResourceSpec_RuntimeRevision{
			RuntimeRevision: &controlplanev1.RuntimeRevisionSpec{
				ManifestSha256:       value.ManifestSHA256,
				ImageDigest:          value.ImageDigest,
				PromptProfileId:      value.PromptProfileID,
				PromptRevision:       value.PromptRevision,
				CredentialBindingIds: value.CredentialBindingIDs,
				IntegrationIds:       value.IntegrationIDs,
			},
		}
	case entity.SessionSpec:
		result.Value = &controlplanev1.ResourceSpec_Session{
			Session: &controlplanev1.SessionSpec{
				AgentId:                  value.AgentID,
				ProviderAccountBindingId: value.ProviderAccountBindingID,
				ConversationId:           value.ConversationID,
				ArchiveRef:               value.ArchiveRef,
				LastTurnSequence:         value.LastTurnSequence,
			},
		}
	case entity.TurnSpec:
		result.Value = &controlplanev1.ResourceSpec_Turn{
			Turn: &controlplanev1.TurnSpec{
				SessionId:         value.SessionID,
				Sequence:          value.Sequence,
				SourceRef:         value.SourceRef,
				PromptArtifactId:  value.PromptArtifactID,
				RuntimeRevisionId: value.RuntimeRevisionID,
				ProcessRunId:      value.ProcessRunID,
				Attempt:           value.Attempt,
				Outcome:           value.Outcome,
				ResultArtifactId:  value.ResultArtifactID,
			},
		}
	case entity.ProcessRunSpec:
		result.Value = &controlplanev1.ResourceSpec_ProcessRun{
			ProcessRun: &controlplanev1.ProcessRunSpec{
				ParentProcessRunId: value.ParentProcessRunID,
				PlaybookRef:        value.PlaybookRef,
				PolicyRevision:     value.PolicyRevision,
				RootTriggerRef:     value.RootTriggerRef,
				ResultArtifactId:   value.ResultArtifactID,
			},
		}
	case entity.ScheduleSpec:
		result.Value = &controlplanev1.ResourceSpec_Schedule{
			Schedule: &controlplanev1.ScheduleSpec{
				TargetResourceId: value.TargetResourceID,
				Cron:             value.Cron,
				Interval:         optionalProtoDuration(value.Interval),
				Timezone:         value.Timezone,
				OverlapPolicy:    overlapPolicy(value.OverlapPolicy),
				MisfirePolicy:    misfirePolicy(value.MisfirePolicy),
				MisfireGrace:     optionalProtoDuration(value.MisfireGrace),
				NextRunAt:        timestamppb.New(value.NextRunAt),
			},
		}
	case entity.OwnerGateSpec:
		result.Value = &controlplanev1.ResourceSpec_OwnerGate{
			OwnerGate: &controlplanev1.OwnerGateSpec{
				ProcessRunId:   value.ProcessRunID,
				ResultRef:      value.ResultRef,
				ResultSha256:   value.ResultSHA256,
				ExpiresAt:      timestamppb.New(value.ExpiresAt),
				Decision:       ownerDecision(value.Decision),
				DecisionReason: value.DecisionReason,
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
				ProcessRunId: value.ProcessRunID,
				TurnId:       value.TurnID,
				Summary:      value.Summary,
				Domains:      value.Domains,
				ResourceKeys: value.ResourceKeys,
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
				ScanStatus:         value.ScanStatus,
				RetentionPolicyRef: value.RetentionPolicyRef,
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
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
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
