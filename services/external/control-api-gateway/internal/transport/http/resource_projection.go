package httptransport

import (
	"errors"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/google/uuid"
)

func resourceProjection(resource *controlplanev1.Resource) (generated.ResourceSpecProjection, error) {
	version := resource.GetVersion()
	switch value := resource.GetSpec().GetValue().(type) {
	case *controlplanev1.ResourceSpec_Project:
		ownership, err := projectionOwnership(value.Project.GetOwnership(), version)
		locale := generated.ProjectProjectionLocale(value.Project.GetLocale())
		if err != nil || !locale.Valid() {
			return generated.ResourceSpecProjection{}, errors.New("project projection is invalid")
		}
		return generated.ResourceSpecProjection{Project: &generated.ProjectProjection{Slug: value.Project.GetSlug(), Description: value.Project.GetDescription(), Locale: locale, Ownership: ownership}}, nil
	case *controlplanev1.ResourceSpec_Team:
		ownership, err := projectionOwnership(value.Team.GetOwnership(), version)
		members, memberErr := parseUUIDs(value.Team.GetMemberActorIds())
		roles, roleErr := parseUUIDs(value.Team.GetRoleIds())
		if err != nil || memberErr != nil || roleErr != nil {
			return generated.ResourceSpecProjection{}, errors.New("team projection is invalid")
		}
		return generated.ResourceSpecProjection{Team: &generated.TeamProjection{StableKey: value.Team.GetStableKey(), ExternalTeamRef: value.Team.GetExternalTeamRef(), MemberActorIds: members, RoleIds: roles, Ownership: ownership}}, nil
	case *controlplanev1.ResourceSpec_Chat:
		ownership, err := projectionOwnership(value.Chat.GetOwnership(), version)
		room := generated.ChatProjectionRoomType(strings.TrimPrefix(value.Chat.GetRoomType().String(), "ROOM_TYPE_"))
		agent, agentErr := optionalUUID(value.Chat.GetDefaultAgentId())
		if err != nil || agentErr != nil || !room.Valid() {
			return generated.ResourceSpecProjection{}, errors.New("chat projection is invalid")
		}
		return generated.ResourceSpecProjection{Chat: &generated.ChatProjection{StableKey: value.Chat.GetStableKey(), RoomType: room, DefaultAgentId: agent, ExternalChannelRef: value.Chat.GetExternalChannelRef(), WorkPolicy: value.Chat.GetWorkPolicy(), Ownership: ownership}}, nil
	case *controlplanev1.ResourceSpec_Role:
		return roleProjection(value.Role, version)
	case *controlplanev1.ResourceSpec_PromptProfile:
		ownership, err := projectionOwnership(value.PromptProfile.GetOwnership(), version)
		if err != nil || value.PromptProfile.GetRevision() == 0 {
			return generated.ResourceSpecProjection{}, errors.New("prompt profile projection is invalid")
		}
		return generated.ResourceSpecProjection{PromptProfile: &generated.PromptProfileProjection{Revision: int64(value.PromptProfile.GetRevision()), ContentSha256: value.PromptProfile.GetContentSha256(), SourceRef: value.PromptProfile.GetSourceRef(), Locale: value.PromptProfile.GetLocale(), Ownership: ownership}}, nil
	case *controlplanev1.ResourceSpec_CredentialBinding:
		ownership, err := projectionOwnership(value.CredentialBinding.GetOwnership(), version)
		var expires, observed *time.Time
		if value.CredentialBinding.GetExpiresAt() != nil {
			item := value.CredentialBinding.GetExpiresAt().AsTime()
			expires = &item
		}
		if value.CredentialBinding.GetProviderObservedAt() != nil {
			item := value.CredentialBinding.GetProviderObservedAt().AsTime()
			observed = &item
		}
		if err != nil || value.CredentialBinding.GetRevision() == 0 {
			return generated.ResourceSpecProjection{}, errors.New("credential binding projection is invalid")
		}
		return generated.ResourceSpecProjection{CredentialBinding: &generated.CredentialBindingProjection{Purpose: value.CredentialBinding.GetPurpose(), ImmutableSecretRef: value.CredentialBinding.GetImmutableSecretRef(), PrincipalRef: value.CredentialBinding.GetPrincipalRef(), Revision: int64(value.CredentialBinding.GetRevision()), ExpiresAt: expires, ProviderEligible: value.CredentialBinding.GetProviderEligible(), ProviderCapabilities: value.CredentialBinding.GetProviderCapabilities(), ProviderObservationRevision: int64(value.CredentialBinding.GetProviderObservationRevision()), ProviderObservedAt: observed, ContentSha256: value.CredentialBinding.GetContentSha256(), Ownership: ownership}}, nil
	case *controlplanev1.ResourceSpec_RepositoryWorkspace:
		ownership, err := projectionOwnership(value.RepositoryWorkspace.GetOwnership(), version)
		credential, credentialErr := optionalUUID(value.RepositoryWorkspace.GetCredentialBindingId())
		if err != nil || credentialErr != nil {
			return generated.ResourceSpecProjection{}, errors.New("repository workspace projection is invalid")
		}
		return generated.ResourceSpecProjection{RepositoryWorkspace: &generated.RepositoryWorkspaceProjection{RepositoryRef: value.RepositoryWorkspace.GetRepositoryRef(), WorkspaceMode: value.RepositoryWorkspace.GetWorkspaceMode(), DefaultBranch: value.RepositoryWorkspace.GetDefaultBranch(), CredentialBindingId: credential, Ownership: ownership}}, nil
	case *controlplanev1.ResourceSpec_Integration:
		ownership, err := projectionOwnership(value.Integration.GetOwnership(), version)
		bindings, bindingErr := parseUUIDs(value.Integration.GetCredentialBindingIds())
		if err != nil || bindingErr != nil || value.Integration.GetDefinitionVersion() == 0 {
			return generated.ResourceSpecProjection{}, errors.New("integration projection is invalid")
		}
		return generated.ResourceSpecProjection{Integration: &generated.IntegrationProjection{DefinitionRef: value.Integration.GetDefinitionRef(), DefinitionVersion: int64(value.Integration.GetDefinitionVersion()), Capabilities: value.Integration.GetCapabilities(), CredentialBindingIds: bindings, EndpointRef: value.Integration.GetEndpointRef(), Ownership: ownership}}, nil
	case *controlplanev1.ResourceSpec_RuntimeRevision:
		prompt, err := requiredUUID(value.RuntimeRevision.GetPromptProfileId())
		if err != nil {
			return generated.ResourceSpecProjection{}, err
		}
		sessionID, err := requiredUUID(value.RuntimeRevision.GetSessionId())
		if err != nil {
			return generated.ResourceSpecProjection{}, err
		}
		roleID, err := requiredUUID(value.RuntimeRevision.GetRoleId())
		if err != nil {
			return generated.ResourceSpecProjection{}, err
		}
		chatID, err := optionalUUID(value.RuntimeRevision.GetChatId())
		if err != nil {
			return generated.ResourceSpecProjection{}, err
		}
		return generated.ResourceSpecProjection{RuntimeRevision: &generated.RuntimeRevisionProjection{ManifestSha256: value.RuntimeRevision.GetManifestSha256(), ImageDigest: value.RuntimeRevision.GetImageDigest(), PromptProfileId: prompt, PromptRevision: int64(value.RuntimeRevision.GetPromptRevision()), SessionId: sessionID, RoleId: roleID, ChatId: chatID, EffectiveRuntimeSha256: value.RuntimeRevision.GetEffectiveRuntimeSha256(), AuthorityPolicyRevision: int64(value.RuntimeRevision.GetAuthorityPolicyRevision())}}, nil
	case *controlplanev1.ResourceSpec_Session:
		agent, err := requiredUUID(value.Session.GetAgentId())
		if err != nil {
			return generated.ResourceSpecProjection{}, err
		}
		binding, err := requiredUUID(value.Session.GetProviderAccountBindingId())
		if err != nil {
			return generated.ResourceSpecProjection{}, err
		}
		conversation, err := optionalUUID(value.Session.GetConversationId())
		if err != nil {
			return generated.ResourceSpecProjection{}, err
		}
		archive := optionalString(value.Session.GetArchiveRef())
		return generated.ResourceSpecProjection{Session: &generated.SessionProjection{AgentId: agent, ProviderAccountBindingId: binding, ConversationId: conversation, ArchiveRef: archive, LastTurnSequence: int64(value.Session.GetLastTurnSequence())}}, nil
	case *controlplanev1.ResourceSpec_Turn:
		return turnProjection(value.Turn)
	case *controlplanev1.ResourceSpec_ProcessRun:
		return processRunProjection(value.ProcessRun)
	case *controlplanev1.ResourceSpec_Schedule:
		ownership, err := projectionOwnership(value.Schedule.GetOwnership(), version)
		target, targetErr := requiredUUID(value.Schedule.GetTargetResourceId())
		kind := generated.ResourceKind(strings.TrimPrefix(value.Schedule.GetTargetKind().String(), "RESOURCE_KIND_"))
		if err != nil || targetErr != nil || !kind.Valid() || value.Schedule.GetNextRunAt() == nil {
			return generated.ResourceSpecProjection{}, errors.New("schedule projection is invalid")
		}
		cron := optionalString(value.Schedule.GetCron())
		return generated.ResourceSpecProjection{Schedule: &generated.ScheduleProjection{TargetResourceId: target, TargetKind: kind, TargetVersion: int64(value.Schedule.GetTargetVersion()), Cron: cron, Timezone: value.Schedule.GetTimezone(), NextRunAt: value.Schedule.GetNextRunAt().AsTime(), MaximumAttempts: int(value.Schedule.GetMaximumAttempts()), Ownership: ownership}}, nil
	case *controlplanev1.ResourceSpec_OwnerGate:
		return ownerGateProjection(value.OwnerGate)
	case *controlplanev1.ResourceSpec_MemoryRecord:
		role, err := optionalUUID(value.MemoryRecord.GetRoleId())
		if err != nil {
			return generated.ResourceSpecProjection{}, err
		}
		return generated.ResourceSpecProjection{MemoryRecord: &generated.MemoryRecordProjection{Scope: value.MemoryRecord.GetScope(), RoleId: role, Title: value.MemoryRecord.GetTitle(), ContentSha256: value.MemoryRecord.GetContentSha256(), Provenance: value.MemoryRecord.GetProvenance(), Importance: int(value.MemoryRecord.GetImportance())}}, nil
	case *controlplanev1.ResourceSpec_WorkClaim:
		return workClaimProjection(value.WorkClaim)
	case *controlplanev1.ResourceSpec_Artifact:
		status := generated.ArtifactProjectionScanStatus(strings.TrimPrefix(value.Artifact.GetScanStatus().String(), "ARTIFACT_SCAN_STATE_"))
		var scanned *time.Time
		if value.Artifact.GetScannedAt() != nil {
			item := value.Artifact.GetScannedAt().AsTime()
			scanned = &item
		}
		if !status.Valid() {
			return generated.ResourceSpecProjection{}, errors.New("artifact projection is invalid")
		}
		return generated.ResourceSpecProjection{Artifact: &generated.ArtifactProjection{ArtifactKind: value.Artifact.GetKind(), Direction: value.Artifact.GetDirection(), SizeBytes: int64(value.Artifact.GetSizeBytes()), MediaType: value.Artifact.GetMediaType(), Sha256: value.Artifact.GetSha256(), ScanStatus: status, ScanPolicyRevision: int64(value.Artifact.GetScanPolicyRevision()), ScanEvidenceSha256: optionalString(value.Artifact.GetScanEvidenceSha256()), ScannedAt: scanned}}, nil
	default:
		return generated.ResourceSpecProjection{}, errors.New("resource projection variant is unavailable")
	}
}

func roleProjection(value *controlplanev1.RoleSpec, version uint64) (generated.ResourceSpecProjection, error) {
	ownership, err := projectionOwnership(value.GetOwnership(), version)
	allowed, e1 := parseUUIDs(value.GetAllowedTargetRoleIds())
	prompt, e2 := requiredUUID(value.GetPromptProfileId())
	credentials, e3 := parseUUIDs(value.GetProviderCredentialBindingIds())
	workspaces, e4 := parseUUIDs(value.GetRepositoryWorkspaceIds())
	integrations, e5 := parseUUIDs(value.GetIntegrationIds())
	pool := value.GetProviderAccountPool()
	policy := generated.ProviderPoolProjectionPolicy("")
	bindings := []generated.ProviderPoolBindingProjection{}
	if pool != nil {
		policy = generated.ProviderPoolProjectionPolicy(pool.GetPolicy())
		for _, item := range pool.GetBindings() {
			id, parseErr := requiredUUID(item.GetCredentialBindingId())
			if parseErr != nil {
				err = parseErr
				break
			}
			bindings = append(bindings, generated.ProviderPoolBindingProjection{CredentialBindingId: id, Weight: int(item.GetWeight())})
		}
	}
	if err != nil || e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || pool == nil || !policy.Valid() {
		return generated.ResourceSpecProjection{}, errors.New("role projection is invalid")
	}
	return generated.ResourceSpecProjection{Role: &generated.RoleProjection{StableKey: value.GetStableKey(), Capabilities: value.GetCapabilities(), AllowedTargetRoleIds: allowed, PromptProfileId: prompt, ProviderCredentialBindingIds: credentials, RepositoryWorkspaceIds: workspaces, IntegrationIds: integrations, ProviderAccountPool: generated.ProviderPoolProjection{Policy: policy, PolicyRevision: int64(pool.GetPolicyRevision()), ObservationMaxAgeSeconds: int64(pool.GetObservationMaxAge().AsDuration().Seconds()), Bindings: bindings}, Ownership: ownership}}, nil
}

func turnProjection(value *controlplanev1.TurnSpec) (generated.ResourceSpecProjection, error) {
	sessionID, err := requiredUUID(value.GetSessionId())
	if err != nil {
		return generated.ResourceSpecProjection{}, err
	}
	runtimeID, err := requiredUUID(value.GetRuntimeRevisionId())
	if err != nil {
		return generated.ResourceSpecProjection{}, err
	}
	processID, err := optionalUUID(value.GetProcessRunId())
	if err != nil {
		return generated.ResourceSpecProjection{}, err
	}
	artifactID, err := optionalUUID(value.GetResultArtifactId())
	if err != nil {
		return generated.ResourceSpecProjection{}, err
	}
	return generated.ResourceSpecProjection{Turn: &generated.TurnProjection{SessionId: sessionID, Sequence: int64(value.GetSequence()), SourceRef: value.GetSourceRef(), RuntimeRevisionId: runtimeID, ProcessRunId: processID, Attempt: int(value.GetAttempt()), ResultArtifactId: artifactID, EffectiveInputSha256: value.GetEffectiveInputSha256()}}, nil
}

func processRunProjection(value *controlplanev1.ProcessRunSpec) (generated.ResourceSpecProjection, error) {
	parent, err := optionalUUID(value.GetParentProcessRunId())
	if err != nil {
		return generated.ResourceSpecProjection{}, err
	}
	rootSession, err := requiredUUID(value.GetRootSessionId())
	if err != nil {
		return generated.ResourceSpecProjection{}, err
	}
	rootTurn, err := requiredUUID(value.GetRootTurnId())
	if err != nil {
		return generated.ResourceSpecProjection{}, err
	}
	runtimeID, err := requiredUUID(value.GetRuntimeRevisionId())
	if err != nil {
		return generated.ResourceSpecProjection{}, err
	}
	currentSession, err := optionalUUID(value.GetCurrentSessionId())
	if err != nil {
		return generated.ResourceSpecProjection{}, err
	}
	currentTurn, err := optionalUUID(value.GetCurrentTurnId())
	if err != nil {
		return generated.ResourceSpecProjection{}, err
	}
	var attempt *int
	if value.GetCurrentAttempt() != 0 {
		item := int(value.GetCurrentAttempt())
		attempt = &item
	}
	return generated.ResourceSpecProjection{ProcessRun: &generated.ProcessRunProjection{ParentProcessRunId: parent, PlaybookRef: value.GetPlaybookRef(), PolicyRevision: int64(value.GetPolicyRevision()), RootTriggerRef: value.GetRootTriggerRef(), RootSessionId: rootSession, RootTurnId: rootTurn, RootAttempt: int(value.GetRootAttempt()), ImmutableInputSha256: value.GetImmutableInputSha256(), RuntimeRevisionId: runtimeID, CurrentSessionId: currentSession, CurrentTurnId: currentTurn, CurrentAttempt: attempt}}, nil
}

func ownerGateProjection(value *controlplanev1.OwnerGateSpec) (generated.ResourceSpecProjection, error) {
	processID, err := requiredUUID(value.GetProcessRunId())
	if err != nil {
		return generated.ResourceSpecProjection{}, err
	}
	sessionID, err := requiredUUID(value.GetSessionId())
	if err != nil {
		return generated.ResourceSpecProjection{}, err
	}
	turnID, err := requiredUUID(value.GetTurnId())
	if err != nil {
		return generated.ResourceSpecProjection{}, err
	}
	decision := generated.OwnerGateProjectionDecision(strings.TrimPrefix(value.GetDecision().String(), "OWNER_GATE_DECISION_"))
	if value.GetDecision() == controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_UNSPECIFIED {
		decision = generated.PENDING
	}
	if value.GetExpiresAt() == nil || !decision.Valid() {
		return generated.ResourceSpecProjection{}, errors.New("owner gate projection is invalid")
	}
	return generated.ResourceSpecProjection{OwnerGate: &generated.OwnerGateProjection{ProcessRunId: processID, ResultSha256: value.GetResultSha256(), ExpiresAt: value.GetExpiresAt().AsTime(), Decision: decision, SessionId: sessionID, TurnId: turnID, Attempt: int(value.GetAttempt()), ImmutableInputSha256: value.GetImmutableInputSha256()}}, nil
}

func workClaimProjection(value *controlplanev1.WorkClaimSpec) (generated.ResourceSpecProjection, error) {
	processID, err := requiredUUID(value.GetProcessRunId())
	if err != nil {
		return generated.ResourceSpecProjection{}, err
	}
	turnID, err := requiredUUID(value.GetTurnId())
	if err != nil {
		return generated.ResourceSpecProjection{}, err
	}
	sessionID, err := requiredUUID(value.GetSessionId())
	if err != nil {
		return generated.ResourceSpecProjection{}, err
	}
	if value.GetExpiresAt() == nil {
		return generated.ResourceSpecProjection{}, errors.New("work claim projection is invalid")
	}
	return generated.ResourceSpecProjection{WorkClaim: &generated.WorkClaimProjection{ProcessRunId: processID, TurnId: turnID, Domains: value.GetDomains(), ResourceKeys: value.GetResourceKeys(), WorkloadId: value.GetWorkloadId(), SessionId: sessionID, Attempt: int(value.GetAttempt()), ExpiresAt: value.GetExpiresAt().AsTime(), AuthorityGeneration: int64(value.GetAuthorityGeneration())}}, nil
}

func projectionOwnership(value *controlplanev1.ConfigurationOwnership, resourceVersion uint64) (generated.ConfigurationOwnershipProjection, error) {
	if value == nil || resourceVersion == 0 {
		return generated.ConfigurationOwnershipProjection{}, errors.New("configuration ownership is unavailable")
	}
	switch value.GetManagedBy() {
	case controlplanev1.ConfigurationManager_CONFIGURATION_MANAGER_UI:
		return generated.ConfigurationOwnershipProjection{ManagedBy: generated.Ui, Source: "owner-ui", Revision: int64(resourceVersion)}, nil
	case controlplanev1.ConfigurationManager_CONFIGURATION_MANAGER_GIT:
		if value.GetSourceRef() == "" || value.GetSourceRevision() == 0 {
			return generated.ConfigurationOwnershipProjection{}, errors.New("Git ownership is incomplete")
		}
		return generated.ConfigurationOwnershipProjection{ManagedBy: generated.Git, Source: value.GetSourceRef(), Revision: int64(value.GetSourceRevision())}, nil
	default:
		return generated.ConfigurationOwnershipProjection{}, errors.New("configuration ownership is invalid")
	}
}

func requiredUUID(value string) (uuid.UUID, error) { return uuid.Parse(value) }
func optionalUUID(value string) (*uuid.UUID, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := uuid.Parse(value)
	return &parsed, err
}

func parseUUIDs(values []string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, 0, len(values))
	for _, item := range values {
		parsed, err := requiredUUID(item)
		if err != nil {
			return nil, err
		}
		result = append(result, parsed)
	}
	return result, nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func ConvertRuntimeIncident(input *controlplanev1.RuntimeIncident) (generated.RuntimeIncident, error) {
	if input == nil || input.GetOccurredAt() == nil || input.GetExecutionFence() == 0 {
		return generated.RuntimeIncident{}, errors.New("runtime incident is incomplete")
	}
	incidentID, err := requiredUUID(input.GetIncidentId())
	if err != nil {
		return generated.RuntimeIncident{}, err
	}
	executionID, err := requiredUUID(input.GetExecutionId())
	if err != nil {
		return generated.RuntimeIncident{}, err
	}
	kind := generated.RuntimeIncidentKind(strings.TrimPrefix(input.GetKind().String(), "RUNTIME_INCIDENT_KIND_"))
	if !kind.Valid() {
		return generated.RuntimeIncident{}, errors.New("runtime incident kind is invalid")
	}
	return generated.RuntimeIncident{IncidentId: incidentID, ExecutionId: executionID, ExecutionFence: int64(input.GetExecutionFence()), Kind: kind, EvidenceSha256: input.GetEvidenceSha256(), WorkloadId: input.GetWorkloadId(), OccurredAt: input.GetOccurredAt().AsTime()}, nil
}
