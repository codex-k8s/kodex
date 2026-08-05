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
		locale := generated.ProjectLocale(value.Project.GetLocale())
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
		room := generated.ChatRoomType(strings.TrimPrefix(value.Chat.GetRoomType().String(), "ROOM_TYPE_"))
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
	case *controlplanev1.ResourceSpec_RoleImageRecipe:
		input, err := roleImageRecipeProjectionInput(value.RoleImageRecipe.GetInput())
		if err != nil || value.RoleImageRecipe.GetGeneration() == 0 || value.RoleImageRecipe.GetPolicyRevision() == 0 {
			return generated.ResourceSpecProjection{}, errors.New("role image recipe projection is invalid")
		}
		return generated.ResourceSpecProjection{RoleImageRecipe: &generated.RoleImageRecipeProjection{
			Input: input, Generation: int64(value.RoleImageRecipe.GetGeneration()),
			SpecSha256: value.RoleImageRecipe.GetSpecSha256(), PolicyRevision: int64(value.RoleImageRecipe.GetPolicyRevision()),
			PolicySha256: value.RoleImageRecipe.GetPolicySha256(),
		}}, nil
	case *controlplanev1.ResourceSpec_ImageBuild:
		recipeID, err := requiredUUID(value.ImageBuild.GetRecipeId())
		stage := generated.ImageBuildStage(strings.TrimPrefix(value.ImageBuild.GetStage().String(), "IMAGE_BUILD_STAGE_"))
		if err != nil || !stage.Valid() || value.ImageBuild.GetAvailableAt() == nil {
			return generated.ResourceSpecProjection{}, errors.New("image build projection is invalid")
		}
		return generated.ResourceSpecProjection{ImageBuild: &generated.ImageBuildProjection{
			RecipeId: recipeID, RecipeVersion: int64(value.ImageBuild.GetRecipeVersion()),
			RecipeGeneration: int64(value.ImageBuild.GetRecipeGeneration()), SpecSha256: value.ImageBuild.GetSpecSha256(),
			Attempt: int(value.ImageBuild.GetAttempt()), Stage: stage, ProgressPercent: int(value.ImageBuild.GetProgressPercent()),
			StagingReference: optionalString(value.ImageBuild.GetStagingReference()), ManifestDigest: optionalString(value.ImageBuild.GetManifestDigest()),
			ProvenanceSha256: optionalString(value.ImageBuild.GetProvenanceSha256()), ImmutableBuildSha256: value.ImageBuild.GetImmutableBuildSha256(),
			ErrorCode: optionalString(value.ImageBuild.GetErrorCode()), AvailableAt: value.ImageBuild.GetAvailableAt().AsTime(),
			MaximumAttempts: int(value.ImageBuild.GetMaximumAttempts()),
		}}, nil
	case *controlplanev1.ResourceSpec_ImageArtifact:
		recipeID, recipeErr := requiredUUID(value.ImageArtifact.GetRecipeId())
		buildID, buildErr := requiredUUID(value.ImageArtifact.GetBuildId())
		if recipeErr != nil || buildErr != nil {
			return generated.ResourceSpecProjection{}, errors.New("image artifact projection is invalid")
		}
		platforms, platformErr := roleImagePlatformsProjection(value.ImageArtifact.GetPlatforms())
		if platformErr != nil || len(platforms) == 0 {
			return generated.ResourceSpecProjection{}, errors.New("image artifact platforms are invalid")
		}
		projection := &generated.ImageArtifactProjection{
			RecipeId: recipeID, RecipeVersion: int64(value.ImageArtifact.GetRecipeVersion()),
			RecipeGeneration: int64(value.ImageArtifact.GetRecipeGeneration()), SpecSha256: value.ImageArtifact.GetSpecSha256(),
			BuildId: buildID, BuildVersion: int64(value.ImageArtifact.GetBuildVersion()), BuildAttempt: int(value.ImageArtifact.GetBuildAttempt()),
			StagingReference: value.ImageArtifact.GetStagingReference(), ManifestDigest: value.ImageArtifact.GetManifestDigest(),
			ProvenanceSha256: value.ImageArtifact.GetProvenanceSha256(), ImmutableBuildSha256: value.ImageArtifact.GetImmutableBuildSha256(),
			BaseImageDigest: value.ImageArtifact.GetBaseImageDigest(), SourceSha256: value.ImageArtifact.GetSourceSha256(),
			ContextSha256: value.ImageArtifact.GetContextSha256(), BuilderSha256: value.ImageArtifact.GetBuilderSha256(),
			FrontendSha256: value.ImageArtifact.GetFrontendSha256(), ToolchainSha256: value.ImageArtifact.GetToolchainSha256(),
			Platforms:  platforms,
			SbomSha256: optionalString(value.ImageArtifact.GetSbomSha256()), VulnerabilityEvidenceSha256: optionalString(value.ImageArtifact.GetVulnerabilityEvidenceSha256()),
			PolicyRevision: int64(value.ImageArtifact.GetPolicyRevision()), PolicySha256: value.ImageArtifact.GetPolicySha256(),
			SignatureIdentity: optionalString(value.ImageArtifact.GetSignatureIdentity()), SignatureSha256: optionalString(value.ImageArtifact.GetSignatureSha256()),
			AdmissionRevision: int64(value.ImageArtifact.GetAdmissionRevision()), AdmissionReceiptSha256: optionalString(value.ImageArtifact.GetAdmissionReceiptSha256()),
			AdmissionReceiptOciManifestDigest: optionalString(value.ImageArtifact.GetAdmissionReceiptOciManifestDigest()),
			PromotedReference:                 optionalString(value.ImageArtifact.GetPromotedReference()), PromotionReadbackSha256: optionalString(value.ImageArtifact.GetPromotionReadbackSha256()),
		}
		if verdict := value.ImageArtifact.GetAdmissionVerdict(); verdict != controlplanev1.ImageAdmissionVerdict_IMAGE_ADMISSION_VERDICT_UNSPECIFIED {
			cast := generated.ImageAdmissionVerdict(strings.TrimPrefix(verdict.String(), "IMAGE_ADMISSION_VERDICT_"))
			if !cast.Valid() {
				return generated.ResourceSpecProjection{}, errors.New("image artifact verdict is invalid")
			}
			projection.AdmissionVerdict = &cast
		}
		if value.ImageArtifact.GetPromotedAt() != nil {
			promotedAt := value.ImageArtifact.GetPromotedAt().AsTime()
			projection.PromotedAt = &promotedAt
		}
		return generated.ResourceSpecProjection{ImageArtifact: projection}, nil
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
		recipeID, err := requiredUUID(value.RuntimeRevision.GetRoleImageRecipeId())
		if err != nil {
			return generated.ResourceSpecProjection{}, err
		}
		buildID, err := requiredUUID(value.RuntimeRevision.GetImageBuildId())
		if err != nil {
			return generated.ResourceSpecProjection{}, err
		}
		artifactID, err := requiredUUID(value.RuntimeRevision.GetImageArtifactId())
		if err != nil {
			return generated.ResourceSpecProjection{}, err
		}
		return generated.ResourceSpecProjection{RuntimeRevision: &generated.RuntimeRevisionProjection{
			ManifestSha256: value.RuntimeRevision.GetManifestSha256(), ImageReference: value.RuntimeRevision.GetImageReference(),
			RoleImageRecipeId: recipeID, RoleImageRecipeVersion: int64(value.RuntimeRevision.GetRoleImageRecipeVersion()),
			RoleImageSpecSha256: value.RuntimeRevision.GetRoleImageSpecSha256(), ImageBuildId: buildID,
			ImageBuildVersion: int64(value.RuntimeRevision.GetImageBuildVersion()), ImageBuildAttempt: int(value.RuntimeRevision.GetImageBuildAttempt()),
			ImageArtifactId: artifactID, ImageArtifactVersion: int64(value.RuntimeRevision.GetImageArtifactVersion()),
			ImageManifestDigest: value.RuntimeRevision.GetImageManifestDigest(), ImageAdmissionRevision: int64(value.RuntimeRevision.GetImageAdmissionRevision()),
			ImageAdmissionReceiptSha256:            value.RuntimeRevision.GetImageAdmissionReceiptSha256(),
			ImageAdmissionReceiptOciManifestDigest: value.RuntimeRevision.GetImageAdmissionReceiptOciManifestDigest(),
			ImagePolicyRevision:                    int64(value.RuntimeRevision.GetImagePolicyRevision()), ImagePolicySha256: value.RuntimeRevision.GetImagePolicySha256(),
			ImageSignatureSha256: value.RuntimeRevision.GetImageSignatureSha256(), ImagePromotionReadbackSha256: value.RuntimeRevision.GetImagePromotionReadbackSha256(),
			PromptProfileId: prompt, PromptRevision: int64(value.RuntimeRevision.GetPromptRevision()),
			SessionId: sessionID, RoleId: roleID, ChatId: chatID,
			EffectiveRuntimeSha256: value.RuntimeRevision.GetEffectiveRuntimeSha256(), AuthorityPolicyRevision: int64(value.RuntimeRevision.GetAuthorityPolicyRevision()),
		}}, nil
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
		status := generated.ArtifactScanStatus(strings.TrimPrefix(value.Artifact.GetScanStatus().String(), "ARTIFACT_SCAN_STATE_"))
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
	recipeID, e6 := requiredUUID(value.GetRoleImageRecipeId())
	pool := value.GetProviderAccountPool()
	policy := generated.ProviderPoolPolicy("")
	bindings := []generated.ProviderPoolBindingProjection{}
	if pool != nil {
		policy = generated.ProviderPoolPolicy(pool.GetPolicy())
		for _, item := range pool.GetBindings() {
			id, parseErr := requiredUUID(item.GetCredentialBindingId())
			if parseErr != nil {
				err = parseErr
				break
			}
			bindings = append(bindings, generated.ProviderPoolBindingProjection{CredentialBindingId: id, Weight: int(item.GetWeight())})
		}
	}
	if err != nil || e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil || pool == nil || !policy.Valid() {
		return generated.ResourceSpecProjection{}, errors.New("role projection is invalid")
	}
	return generated.ResourceSpecProjection{Role: &generated.RoleProjection{StableKey: value.GetStableKey(), Capabilities: value.GetCapabilities(), AllowedTargetRoleIds: allowed, PromptProfileId: prompt, RoleImageRecipeId: recipeID, ProviderCredentialBindingIds: credentials, RepositoryWorkspaceIds: workspaces, IntegrationIds: integrations, ProviderAccountPool: generated.ProviderPoolProjection{Policy: policy, PolicyRevision: int64(pool.GetPolicyRevision()), ObservationMaxAgeSeconds: int64(pool.GetObservationMaxAge().AsDuration().Seconds()), Bindings: bindings}, Ownership: ownership}}, nil
}

func roleImageRecipeProjectionInput(input *controlplanev1.RoleImageRecipeInput) (generated.RoleImageRecipeStatusInput, error) {
	if input == nil {
		return generated.RoleImageRecipeStatusInput{}, errors.New("role image recipe input is missing")
	}
	result := generated.RoleImageRecipeStatusInput{
		BaseImageReference: input.GetBaseImageReference(), BaseImageDigest: input.GetBaseImageDigest(),
		SourceRef: input.GetSourceRef(), SourceRevision: input.GetSourceRevision(), SourceSha256: input.GetSourceSha256(),
		ContextRef: input.GetContextRef(), ContextSha256: input.GetContextSha256(), BuilderSha256: input.GetBuilderSha256(),
		FrontendSha256: input.GetFrontendSha256(), ToolchainSha256: input.GetToolchainSha256(),
		Packages:  make([]generated.RoleImagePackage, 0, len(input.GetPackages())),
		Tools:     make([]generated.RoleImageTool, 0, len(input.GetTools())),
		Platforms: make([]generated.RoleImagePlatform, 0, len(input.GetPlatforms())),
	}
	for _, item := range input.GetPackages() {
		manager := generated.RoleImagePackageManager(item.GetManager())
		if !manager.Valid() {
			return generated.RoleImageRecipeStatusInput{}, errors.New("role image package manager is invalid")
		}
		result.Packages = append(result.Packages, generated.RoleImagePackage{
			Manager: manager, Name: item.GetName(), Version: item.GetVersion(), Digest: item.GetDigest(),
		})
	}
	for _, item := range input.GetTools() {
		result.Tools = append(result.Tools, generated.RoleImageTool{
			Name: item.GetName(), Version: item.GetVersion(), SourceRef: item.GetSourceRef(), Sha256: item.GetSha256(),
		})
	}
	platforms, err := roleImagePlatformsProjection(input.GetPlatforms())
	if err != nil {
		return generated.RoleImageRecipeStatusInput{}, err
	}
	result.Platforms = platforms
	return result, nil
}

func roleImagePlatformsProjection(platforms []*controlplanev1.RoleImagePlatform) ([]generated.RoleImagePlatform, error) {
	result := make([]generated.RoleImagePlatform, 0, len(platforms))
	for _, item := range platforms {
		osValue := generated.RoleImagePlatformOS(item.GetOs())
		architecture := generated.RoleImagePlatformArchitecture(item.GetArchitecture())
		if !osValue.Valid() || !architecture.Valid() {
			return nil, errors.New("role image platform is invalid")
		}
		result = append(result, generated.RoleImagePlatform{
			Os: osValue, Architecture: architecture, Variant: optionalString(item.GetVariant()),
		})
	}
	return result, nil
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
	decision := generated.OwnerGateDecision(strings.TrimPrefix(value.GetDecision().String(), "OWNER_GATE_DECISION_"))
	if value.GetDecision() == controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_UNSPECIFIED {
		decision = generated.OwnerGateDecisionPENDING
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
		if value.GetSourceRef() != "" && value.GetSourceRevision() != 0 {
			return generated.ConfigurationOwnershipProjection{ManagedBy: generated.Ui, Source: value.GetSourceRef(), Revision: int64(value.GetSourceRevision())}, nil
		}
		if value.GetSourceRef() != "" || value.GetSourceRevision() != 0 {
			return generated.ConfigurationOwnershipProjection{}, errors.New("UI ownership lineage is incomplete")
		}
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
