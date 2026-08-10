package httptransport

import (
	"errors"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/google/uuid"
)

const (
	ownerConfigurationConfigured    = "CONFIGURED"
	ownerConfigurationNotConfigured = "NOT_CONFIGURED"
	ownerConfigurationUnavailable   = "UNAVAILABLE"
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
		if err != nil || len(value.Team.GetMemberActorIds()) > 200 || len(value.Team.GetRoleIds()) > 64 {
			return generated.ResourceSpecProjection{}, errors.New("team projection is invalid")
		}
		providerStatus := ownerConfigurationNotConfigured
		if value.Team.GetExternalTeamRef() != "" {
			providerStatus = ownerConfigurationConfigured
		}
		return generated.ResourceSpecProjection{Team: &generated.TeamProjection{StableKey: value.Team.GetStableKey(), ProviderBindingStatus: providerStatus, MemberCount: len(value.Team.GetMemberActorIds()), RoleCount: len(value.Team.GetRoleIds()), Ownership: ownership}}, nil
	case *controlplanev1.ResourceSpec_Chat:
		ownership, err := projectionOwnership(value.Chat.GetOwnership(), version)
		room := generated.ChatRoomType(strings.TrimPrefix(value.Chat.GetRoomType().String(), "ROOM_TYPE_"))
		if err != nil || !room.Valid() {
			return generated.ResourceSpecProjection{}, errors.New("chat projection is invalid")
		}
		agentStatus := ownerConfigurationNotConfigured
		if value.Chat.GetDefaultAgentId() != "" {
			agentStatus = ownerConfigurationConfigured
		}
		channelStatus := ownerConfigurationNotConfigured
		if value.Chat.GetExternalChannelRef() != "" {
			channelStatus = ownerConfigurationConfigured
		}
		return generated.ResourceSpecProjection{Chat: &generated.ChatProjection{StableKey: value.Chat.GetStableKey(), RoomType: room, DefaultAgentStatus: agentStatus, ProviderChannelStatus: channelStatus, WorkPolicy: value.Chat.GetWorkPolicy(), Ownership: ownership}}, nil
	case *controlplanev1.ResourceSpec_Role:
		return roleProjection(value.Role, version)
	case *controlplanev1.ResourceSpec_PromptProfile:
		ownership, err := projectionOwnership(value.PromptProfile.GetOwnership(), version)
		if err != nil || value.PromptProfile.GetRevision() == 0 {
			return generated.ResourceSpecProjection{}, errors.New("prompt profile projection is invalid")
		}
		sourceStatus := ownerConfigurationNotConfigured
		if value.PromptProfile.GetSourceRef() != "" {
			sourceStatus = ownerConfigurationConfigured
		}
		return generated.ResourceSpecProjection{PromptProfile: &generated.PromptProfileProjection{Revision: int64(value.PromptProfile.GetRevision()), ContentSha256: value.PromptProfile.GetContentSha256(), SourceStatus: sourceStatus, Locale: value.PromptProfile.GetLocale(), Ownership: ownership}}, nil
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
		bindingStatus := ownerConfigurationUnavailable
		if value.CredentialBinding.GetImmutableSecretRef() != "" && value.CredentialBinding.GetPrincipalRef() != "" {
			bindingStatus = ownerConfigurationConfigured
		}
		return generated.ResourceSpecProjection{CredentialBinding: &generated.CredentialBindingProjection{Purpose: value.CredentialBinding.GetPurpose(), Revision: int64(value.CredentialBinding.GetRevision()), ExpiresAt: expires, ProviderEligible: value.CredentialBinding.GetProviderEligible(), ProviderCapabilities: value.CredentialBinding.GetProviderCapabilities(), ProviderObservationRevision: int64(value.CredentialBinding.GetProviderObservationRevision()), ProviderObservedAt: observed, ContentSha256: value.CredentialBinding.GetContentSha256(), BindingStatus: bindingStatus, Ownership: ownership}}, nil
	case *controlplanev1.ResourceSpec_RepositoryWorkspace:
		ownership, err := projectionOwnership(value.RepositoryWorkspace.GetOwnership(), version)
		if err != nil {
			return generated.ResourceSpecProjection{}, errors.New("repository workspace projection is invalid")
		}
		repositoryStatus := ownerConfigurationUnavailable
		if value.RepositoryWorkspace.GetRepositoryRef() != "" {
			repositoryStatus = ownerConfigurationConfigured
		}
		credentialStatus := ownerConfigurationNotConfigured
		if value.RepositoryWorkspace.GetCredentialBindingId() != "" {
			credentialStatus = ownerConfigurationConfigured
		}
		return generated.ResourceSpecProjection{RepositoryWorkspace: &generated.RepositoryWorkspaceProjection{RepositoryStatus: repositoryStatus, WorkspaceMode: value.RepositoryWorkspace.GetWorkspaceMode(), DefaultBranch: value.RepositoryWorkspace.GetDefaultBranch(), CredentialBindingStatus: credentialStatus, Ownership: ownership}}, nil
	case *controlplanev1.ResourceSpec_Integration:
		ownership, err := projectionOwnership(value.Integration.GetOwnership(), version)
		if err != nil || value.Integration.GetDefinitionVersion() == 0 || len(value.Integration.GetCredentialBindingIds()) > 32 {
			return generated.ResourceSpecProjection{}, errors.New("integration projection is invalid")
		}
		endpointStatus := ownerConfigurationUnavailable
		if value.Integration.GetEndpointRef() != "" {
			endpointStatus = ownerConfigurationConfigured
		}
		return generated.ResourceSpecProjection{Integration: &generated.IntegrationProjection{DefinitionRef: value.Integration.GetDefinitionRef(), DefinitionVersion: int64(value.Integration.GetDefinitionVersion()), Capabilities: value.Integration.GetCapabilities(), CredentialBindingCount: len(value.Integration.GetCredentialBindingIds()), EndpointStatus: endpointStatus, Ownership: ownership}}, nil
	case *controlplanev1.ResourceSpec_RoleImageRecipe:
		input, err := roleImageRecipeProjectionInput(value.RoleImageRecipe.GetInput())
		if err != nil || value.RoleImageRecipe.GetGeneration() == 0 || value.RoleImageRecipe.GetPolicyRevision() == 0 {
			return generated.ResourceSpecProjection{}, errors.New("role image recipe projection is invalid")
		}
		return generated.ResourceSpecProjection{RoleImageRecipe: &generated.RoleImageRecipeProjection{
			Input: input, Generation: int64(value.RoleImageRecipe.GetGeneration()),
			SpecSha256: value.RoleImageRecipe.GetSpecSha256(), PolicyRevision: int64(value.RoleImageRecipe.GetPolicyRevision()),
			PolicySha256:                value.RoleImageRecipe.GetPolicySha256(),
			RoleRuntimeContractRevision: int64(value.RoleImageRecipe.GetRoleRuntimeContractRevision()),
			RoleRuntimeContractSha256:   value.RoleImageRecipe.GetRoleRuntimeContractSha256(),
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
			DiagnosticCode:    optionalString(value.ImageBuild.GetDiagnosticCode()),
			DiagnosticSummary: optionalString(value.ImageBuild.GetDiagnosticSummary()),
			MaximumAttempts:   int(value.ImageBuild.GetMaximumAttempts()),
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
			RoleRuntimeContractRevision: int64(value.ImageArtifact.GetRoleRuntimeContractRevision()),
			RoleRuntimeContractSha256:   value.ImageArtifact.GetRoleRuntimeContractSha256(),
			Platforms:                   platforms,
			SbomSha256:                  optionalString(value.ImageArtifact.GetSbomSha256()), VulnerabilityEvidenceSha256: optionalString(value.ImageArtifact.GetVulnerabilityEvidenceSha256()),
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
	case *controlplanev1.ResourceSpec_RoleDefinition:
		ownership, err := projectionOwnership(value.RoleDefinition.GetOwnership(), version)
		recipeRef, recipeVersion, recipeDigest := value.RoleDefinition.GetRoleImageRecipeId(), value.RoleDefinition.GetRoleImageRecipeVersion(), value.RoleDefinition.GetRoleImageRecipeSha256()
		hasRecipe := recipeRef != "" || recipeVersion != 0 || recipeDigest != ""
		if err != nil || value.RoleDefinition.GetStableKey() == "" || (hasRecipe && (recipeRef == "" || recipeVersion == 0 || !validSHA256(recipeDigest))) {
			return generated.ResourceSpecProjection{}, errors.New("role definition projection is invalid")
		}
		var recipeSHA *generated.Sha256
		if recipeDigest != "" {
			item := generated.Sha256(strings.ToLower(recipeDigest))
			recipeSHA = &item
		}
		return generated.ResourceSpecProjection{RoleDefinition: &generated.RoleDefinitionProjection{
			StableKey: value.RoleDefinition.GetStableKey(), Description: value.RoleDefinition.GetDescription(),
			Capabilities: value.RoleDefinition.GetCapabilities(), AllowedTargetRoleDefinitionRefs: value.RoleDefinition.GetAllowedTargetRoleDefinitionIds(),
			RoleImageRecipeRef: optionalString(value.RoleDefinition.GetRoleImageRecipeId()), RoleImageRecipeVersion: int64(value.RoleDefinition.GetRoleImageRecipeVersion()),
			RoleImageRecipeSha256: recipeSHA, Ownership: ownership,
		}}, nil
	case *controlplanev1.ResourceSpec_Agent:
		ownership, err := projectionOwnership(value.Agent.GetOwnership(), version)
		if err != nil || value.Agent.GetStableKey() == "" || value.Agent.GetRoleDefinitionId() == "" ||
			value.Agent.GetInstructionSetId() == "" || value.Agent.GetProviderPoolId() == "" {
			return generated.ResourceSpecProjection{}, errors.New("agent projection is invalid")
		}
		return generated.ResourceSpecProjection{Agent: &generated.AgentProjection{
			StableKey: value.Agent.GetStableKey(), RoleDefinitionRef: value.Agent.GetRoleDefinitionId(),
			InstructionSetRef: value.Agent.GetInstructionSetId(), ProviderPoolRef: value.Agent.GetProviderPoolId(),
			RuntimeProfileRef: optionalString(value.Agent.GetRuntimeProfileRef()), Capabilities: value.Agent.GetCapabilities(),
			BotUsername: optionalString(value.Agent.GetBotUsername()), BotMaskedStatus: optionalString(value.Agent.GetBotMaskedStatus()),
			Enabled: value.Agent.GetEnabled(), Ownership: ownership,
		}}, nil
	case *controlplanev1.ResourceSpec_AgentAssignment:
		if value.AgentAssignment.GetAgentId() == "" || value.AgentAssignment.GetWorkspaceId() == "" || value.AgentAssignment.GetAssignmentGeneration() == 0 {
			return generated.ResourceSpecProjection{}, errors.New("agent assignment projection is invalid")
		}
		return generated.ResourceSpecProjection{AgentAssignment: &generated.AgentAssignmentProjection{
			AgentRef: value.AgentAssignment.GetAgentId(), WorkspaceRef: value.AgentAssignment.GetWorkspaceId(),
			RoomRef: optionalString(value.AgentAssignment.GetRoomId()), Generation: int64(value.AgentAssignment.GetAssignmentGeneration()),
		}}, nil
	case *controlplanev1.ResourceSpec_InstructionSet:
		ownership, err := projectionOwnership(value.InstructionSet.GetOwnership(), version)
		state := generated.InstructionVersionState(strings.TrimPrefix(value.InstructionSet.GetVersionState().String(), "INSTRUCTION_VERSION_STATE_"))
		if err != nil || value.InstructionSet.GetStableKey() == "" || value.InstructionSet.GetCurrentVersion() == 0 || !validSHA256(value.InstructionSet.GetContentSha256()) || !state.Valid() {
			return generated.ResourceSpecProjection{}, errors.New("instruction set projection is invalid")
		}
		problems := make([]generated.InstructionValidationProblem, 0, len(value.InstructionSet.GetValidationErrors()))
		for _, problem := range value.InstructionSet.GetValidationErrors() {
			problems = append(problems, generated.InstructionValidationProblem{Code: problem.GetCode(), Field: problem.GetField(), Line: int(problem.GetLine()), Column: int(problem.GetColumn()), Message: problem.GetMessage()})
		}
		return generated.ResourceSpecProjection{InstructionSet: &generated.InstructionSetProjection{
			StableKey: value.InstructionSet.GetStableKey(), Locale: value.InstructionSet.GetLocale(),
			CurrentVersion: int64(value.InstructionSet.GetCurrentVersion()), PublishedVersion: int64(value.InstructionSet.GetPublishedVersion()),
			Content: value.InstructionSet.GetContent(), ContentSha256: generated.Sha256(strings.ToLower(value.InstructionSet.GetContentSha256())),
			VersionState: state, ValidationSucceeded: value.InstructionSet.GetValidationSucceeded(), ValidationProblems: problems, Ownership: ownership,
		}}, nil
	case *controlplanev1.ResourceSpec_ProviderConnectionReference:
		masked := generated.ProviderConnectionMaskedStatus(strings.TrimPrefix(value.ProviderConnectionReference.GetMaskedStatus().String(), "PROVIDER_CONNECTION_STATUS_"))
		if !masked.Valid() || value.ProviderConnectionReference.GetObservedAt() == nil || value.ProviderConnectionReference.GetObservedAt().CheckValid() != nil ||
			value.ProviderConnectionReference.GetObservationExpiresAt() == nil || value.ProviderConnectionReference.GetObservationExpiresAt().CheckValid() != nil {
			return generated.ResourceSpecProjection{}, errors.New("provider connection reference projection is invalid")
		}
		return generated.ResourceSpecProjection{ProviderConnectionReference: &generated.ProviderConnectionReferenceProjection{
			StableKey: value.ProviderConnectionReference.GetStableKey(), Provider: value.ProviderConnectionReference.GetProvider(),
			ReferenceVersion: int64(value.ProviderConnectionReference.GetReferenceVersion()), MaskedLabel: value.ProviderConnectionReference.GetMaskedLabel(),
			MaskedStatus: masked, Capabilities: value.ProviderConnectionReference.GetCapabilities(), Eligible: value.ProviderConnectionReference.GetEligible(),
			ObservedAt: value.ProviderConnectionReference.GetObservedAt().AsTime(), ObservedUsage: int64(value.ProviderConnectionReference.GetObservedUsage()),
			ObservedLimit: int64(value.ProviderConnectionReference.GetObservedLimit()), ObservationExpiresAt: value.ProviderConnectionReference.GetObservationExpiresAt().AsTime(),
		}}, nil
	case *controlplanev1.ResourceSpec_ProviderPool:
		ownership, err := projectionOwnership(value.ProviderPool.GetOwnership(), version)
		policy := generated.ProviderPoolPolicy(value.ProviderPool.GetPolicy())
		eligible := 0
		for _, member := range value.ProviderPool.GetBindings() {
			if member.GetEligible() {
				eligible++
			}
		}
		if err != nil || !policy.Valid() || value.ProviderPool.GetPolicyRevision() == 0 || value.ProviderPool.GetObservationMaxAge() == nil ||
			value.ProviderPool.GetObservationMaxAge().CheckValid() != nil || value.ProviderPool.GetObservationMaxAge().AsDuration() <= 0 || !validSHA256(value.ProviderPool.GetEligibilitySnapshotSha256()) {
			return generated.ResourceSpecProjection{}, errors.New("provider pool projection is invalid")
		}
		return generated.ResourceSpecProjection{ProviderPool: &generated.ProviderPoolResourceProjection{
			StableKey: value.ProviderPool.GetStableKey(), Policy: policy, PolicyRevision: int64(value.ProviderPool.GetPolicyRevision()),
			ObservationMaxAgeSeconds: int64(value.ProviderPool.GetObservationMaxAge().AsDuration() / time.Second), EligibleMembers: eligible,
			TotalMembers: len(value.ProviderPool.GetBindings()), EligibilitySnapshotSha256: generated.Sha256(strings.ToLower(value.ProviderPool.GetEligibilitySnapshotSha256())), Ownership: ownership,
		}}, nil
	case *controlplanev1.ResourceSpec_WorkspaceBackup:
		scope := generated.WorkspaceBackupScope(strings.TrimPrefix(value.WorkspaceBackup.GetScope().String(), "WORKSPACE_BACKUP_SCOPE_"))
		state := generated.WorkspaceBackupState(strings.TrimPrefix(value.WorkspaceBackup.GetBackupState().String(), "WORKSPACE_BACKUP_STATE_"))
		if !scope.Valid() || !state.Valid() || value.WorkspaceBackup.GetAttempt() == 0 || value.WorkspaceBackup.GetGeneration() == 0 ||
			!validSHA256(value.WorkspaceBackup.GetMembershipSha256()) || value.WorkspaceBackup.GetRetainUntil() == nil || value.WorkspaceBackup.GetRetainUntil().CheckValid() != nil || len(value.WorkspaceBackup.GetMembers()) == 0 {
			return generated.ResourceSpecProjection{}, errors.New("workspace backup projection is invalid")
		}
		return generated.ResourceSpecProjection{WorkspaceBackup: &generated.WorkspaceBackupProjection{
			Scope: scope, MemberCount: len(value.WorkspaceBackup.GetMembers()), MembershipSha256: generated.Sha256(strings.ToLower(value.WorkspaceBackup.GetMembershipSha256())),
			State: state, Attempt: int(value.WorkspaceBackup.GetAttempt()), Generation: int64(value.WorkspaceBackup.GetGeneration()),
			TerminalReasonCode: optionalString(value.WorkspaceBackup.GetTerminalReasonCode()), RetainUntil: value.WorkspaceBackup.GetRetainUntil().AsTime(),
		}}, nil
	case *controlplanev1.ResourceSpec_WorkspaceRestore:
		state := generated.WorkspaceRestoreState(strings.TrimPrefix(value.WorkspaceRestore.GetRestoreState().String(), "WORKSPACE_RESTORE_STATE_"))
		if !state.Valid() || value.WorkspaceRestore.GetBackupId() == "" || value.WorkspaceRestore.GetBackupVersion() == 0 || !validSHA256(value.WorkspaceRestore.GetMembershipSha256()) ||
			value.WorkspaceRestore.GetAttempt() == 0 || value.WorkspaceRestore.GetGeneration() == 0 || len(value.WorkspaceRestore.GetMembers()) == 0 {
			return generated.ResourceSpecProjection{}, errors.New("workspace restore projection is invalid")
		}
		return generated.ResourceSpecProjection{WorkspaceRestore: &generated.WorkspaceRestoreProjection{
			BackupRef: value.WorkspaceRestore.GetBackupId(), BackupVersion: int64(value.WorkspaceRestore.GetBackupVersion()),
			MembershipSha256: generated.Sha256(strings.ToLower(value.WorkspaceRestore.GetMembershipSha256())), MemberCount: len(value.WorkspaceRestore.GetMembers()),
			State: state, Attempt: int(value.WorkspaceRestore.GetAttempt()), Generation: int64(value.WorkspaceRestore.GetGeneration()),
			Partial: value.WorkspaceRestore.GetPartial(), TerminalReasonCode: optionalString(value.WorkspaceRestore.GetTerminalReasonCode()),
		}}, nil
	case *controlplanev1.ResourceSpec_WorkspaceMattermostMapping:
		state := generated.WorkspaceMattermostMappingState(strings.TrimPrefix(value.WorkspaceMattermostMapping.GetMappingState().String(), "WORKSPACE_MATTERMOST_MAPPING_STATE_"))
		if !state.Valid() || value.WorkspaceMattermostMapping.GetWorkspaceId() == "" || value.WorkspaceMattermostMapping.GetWorkspaceVersion() == 0 ||
			value.WorkspaceMattermostMapping.GetMappingGeneration() == 0 || value.WorkspaceMattermostMapping.GetProviderObservedAt() == nil || value.WorkspaceMattermostMapping.GetProviderObservedAt().CheckValid() != nil ||
			value.WorkspaceMattermostMapping.GetProviderEffectVersion() == 0 || value.WorkspaceMattermostMapping.GetProviderEffectGeneration() == 0 {
			return generated.ResourceSpecProjection{}, errors.New("workspace Mattermost mapping projection is invalid")
		}
		return generated.ResourceSpecProjection{WorkspaceMattermostMapping: &generated.WorkspaceMattermostMappingProjection{
			WorkspaceRef: value.WorkspaceMattermostMapping.GetWorkspaceId(), WorkspaceVersion: int64(value.WorkspaceMattermostMapping.GetWorkspaceVersion()),
			MappingGeneration: int64(value.WorkspaceMattermostMapping.GetMappingGeneration()), State: state,
			ProviderObservedAt: value.WorkspaceMattermostMapping.GetProviderObservedAt().AsTime(), ProviderEffectVersion: int64(value.WorkspaceMattermostMapping.GetProviderEffectVersion()),
			ProviderEffectGeneration: int64(value.WorkspaceMattermostMapping.GetProviderEffectGeneration()),
		}}, nil
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
			RoleRuntimeContractRevision: int64(value.RuntimeRevision.GetRoleRuntimeContractRevision()),
			RoleRuntimeContractSha256:   value.RuntimeRevision.GetRoleRuntimeContractSha256(),
			PromptProfileId:             prompt, PromptRevision: int64(value.RuntimeRevision.GetPromptRevision()),
			SessionId: sessionID, RoleId: roleID, ChatId: chatID,
			EffectiveRuntimeSha256: value.RuntimeRevision.GetEffectiveRuntimeSha256(), AuthorityPolicyRevision: int64(value.RuntimeRevision.GetAuthorityPolicyRevision()),
		}}, nil
	case *controlplanev1.ResourceSpec_Session:
		return generated.ResourceSpecProjection{Session: &generated.SessionProjection{LastTurnSequence: int64(value.Session.GetLastTurnSequence())}}, nil
	case *controlplanev1.ResourceSpec_Turn:
		return turnProjection(value.Turn)
	case *controlplanev1.ResourceSpec_ProcessRun:
		return processRunProjection(value.ProcessRun)
	case *controlplanev1.ResourceSpec_Schedule:
		ownership, err := projectionOwnership(value.Schedule.GetOwnership(), version)
		target, targetErr := requiredUUID(value.Schedule.GetTargetResourceId())
		promptProfile, promptErr := requiredUUID(value.Schedule.GetPromptProfileId())
		runtimeRevision, runtimeErr := requiredUUID(value.Schedule.GetRuntimeRevisionId())
		promptArtifact, artifactErr := requiredUUID(value.Schedule.GetPromptArtifactId())
		room, roomErr := optionalUUID(value.Schedule.GetRoomId())
		executionSession, sessionErr := optionalUUID(value.Schedule.GetExecutionSessionId())
		kind := generated.ResourceKind(strings.TrimPrefix(value.Schedule.GetTargetKind().String(), "RESOURCE_KIND_"))
		overlap := generated.ScheduleOverlapPolicy(strings.TrimPrefix(value.Schedule.GetOverlapPolicy().String(), "SCHEDULE_OVERLAP_POLICY_"))
		misfire := generated.ScheduleMisfirePolicy(strings.TrimPrefix(value.Schedule.GetMisfirePolicy().String(), "SCHEDULE_MISFIRE_POLICY_"))
		calendar := generated.ScheduleCalendar(value.Schedule.GetCalendar())
		delivery := generated.ScheduleDeliveryPolicy(value.Schedule.GetDeliveryPolicy())
		sessionPolicy := generated.ScheduleSessionPolicy(strings.TrimPrefix(value.Schedule.GetSessionPolicy().String(), "SCHEDULE_SESSION_POLICY_"))
		notification := generated.ScheduleNotificationPolicy(strings.TrimPrefix(value.Schedule.GetNotificationPolicy().String(), "SCHEDULE_NOTIFICATION_POLICY_"))
		targetType := generated.ScheduleTargetType(strings.TrimPrefix(value.Schedule.GetTargetType().String(), "SCHEDULE_TARGET_TYPE_"))
		if err != nil || targetErr != nil || promptErr != nil || runtimeErr != nil || artifactErr != nil ||
			roomErr != nil || sessionErr != nil || !kind.Valid() || !calendar.Valid() || !overlap.Valid() || !misfire.Valid() || !delivery.Valid() ||
			!sessionPolicy.Valid() || !notification.Valid() || !targetType.Valid() ||
			value.Schedule.GetNextRunAt() == nil || value.Schedule.GetMisfireGrace() == nil ||
			value.Schedule.GetInitialBackoff() == nil || value.Schedule.GetMaximumBackoff() == nil ||
			value.Schedule.GetDeadLetterAfter() == nil || value.Schedule.GetMaximumExecutionDuration() == nil {
			return generated.ResourceSpecProjection{}, errors.New("schedule projection is invalid")
		}
		cron := optionalString(value.Schedule.GetCron())
		var interval *int64
		if value.Schedule.GetInterval() != nil {
			seconds := int64(value.Schedule.GetInterval().AsDuration() / time.Second)
			interval = &seconds
		}
		playbookRef := optionalString(value.Schedule.GetPlaybookRef())
		var playbookVersion *int64
		if value.Schedule.GetPlaybookVersion() != 0 {
			converted := int64(value.Schedule.GetPlaybookVersion())
			playbookVersion = &converted
		}
		return generated.ResourceSpecProjection{Schedule: &generated.ScheduleProjection{
			TargetResourceId: target, TargetKind: kind, TargetVersion: int64(value.Schedule.GetTargetVersion()),
			Cron: cron, IntervalSeconds: interval, Timezone: value.Schedule.GetTimezone(),
			NextRunAt: value.Schedule.GetNextRunAt().AsTime(), Calendar: calendar,
			OverlapPolicy: overlap, MisfirePolicy: misfire,
			MisfireGraceSeconds:    int64(value.Schedule.GetMisfireGrace().AsDuration() / time.Second),
			DeliveryPolicy:         delivery,
			MaximumAttempts:        int(value.Schedule.GetMaximumAttempts()),
			InitialBackoffSeconds:  int64(value.Schedule.GetInitialBackoff().AsDuration() / time.Second),
			MaximumBackoffSeconds:  int64(value.Schedule.GetMaximumBackoff().AsDuration() / time.Second),
			DeadLetterAfterSeconds: int64(value.Schedule.GetDeadLetterAfter().AsDuration() / time.Second),
			PromptProfileId:        promptProfile, PromptRevision: int64(value.Schedule.GetPromptRevision()),
			SessionPolicy: sessionPolicy, RoomId: room, NotificationPolicy: notification,
			MaximumExecutionSeconds: int64(value.Schedule.GetMaximumExecutionDuration().AsDuration() / time.Second),
			Coalesce:                value.Schedule.GetCoalesce(), RuntimeRevisionId: runtimeRevision, TargetType: targetType,
			PlaybookRef: playbookRef, PlaybookVersion: playbookVersion, PromptArtifactId: promptArtifact,
			ExecutionSessionId: executionSession, Ownership: ownership,
		}}, nil
	case *controlplanev1.ResourceSpec_OwnerGate:
		return ownerGateProjection(resource, value.OwnerGate)
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
	pool := value.GetProviderAccountPool()
	policy := generated.ProviderPoolPolicy("")
	if pool != nil {
		policy = generated.ProviderPoolPolicy(pool.GetPolicy())
	}
	if err != nil || pool == nil || !policy.Valid() || len(value.GetAllowedTargetRoleIds()) > 64 || len(value.GetProviderCredentialBindingIds()) > 32 || len(value.GetRepositoryWorkspaceIds()) > 32 || len(value.GetIntegrationIds()) > 32 || len(pool.GetBindings()) > 8 {
		return generated.ResourceSpecProjection{}, errors.New("role projection is invalid")
	}
	promptStatus := ownerConfigurationNotConfigured
	if value.GetPromptProfileId() != "" {
		promptStatus = ownerConfigurationConfigured
	}
	recipeStatus := ownerConfigurationNotConfigured
	if value.GetRoleImageRecipeId() != "" {
		recipeStatus = ownerConfigurationConfigured
	}
	return generated.ResourceSpecProjection{Role: &generated.RoleProjection{StableKey: value.GetStableKey(), Capabilities: value.GetCapabilities(), AllowedTargetRoleCount: len(value.GetAllowedTargetRoleIds()), PromptProfileStatus: promptStatus, RoleImageRecipeStatus: recipeStatus, ProviderCredentialBindingCount: len(value.GetProviderCredentialBindingIds()), RepositoryWorkspaceCount: len(value.GetRepositoryWorkspaceIds()), IntegrationCount: len(value.GetIntegrationIds()), ProviderAccountPool: generated.ProviderPoolProjection{Policy: policy, PolicyRevision: int64(pool.GetPolicyRevision()), ObservationMaxAgeSeconds: int64(pool.GetObservationMaxAge().AsDuration().Seconds()), BindingCount: len(pool.GetBindings())}, Ownership: ownership}}, nil
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
			Manager: manager, Name: item.GetName(), Version: item.GetVersion(), Digest: item.GetDigest(), SourceRef: item.GetSourceRef(),
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

func ownerGateProjection(resource *controlplanev1.Resource, value *controlplanev1.OwnerGateSpec) (generated.ResourceSpecProjection, error) {
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
	deliveryState := generated.OwnerGateDeliveryStateAWAITINGDELIVERYPROOF
	nextAction := generated.OwnerGateNextActionWAITFORDELIVERY
	resolvable := false
	var deliveredAt *time.Time
	if value.GetDeliveredAt() != nil {
		item := value.GetDeliveredAt().AsTime()
		deliveredAt = &item
	}
	state := strings.TrimPrefix(resource.GetState().String(), "LIFECYCLE_STATE_")
	if state == "EXPIRED" {
		deliveryState = generated.OwnerGateDeliveryStateEXPIRED
		nextAction = generated.OwnerGateNextActionREADTERMINAL
	} else if state != "WAITING_OWNER" {
		deliveryState = generated.OwnerGateDeliveryStateTERMINAL
		nextAction = generated.OwnerGateNextActionREADTERMINAL
	} else if value.GetDeliveryProviderReceiptSha256() != "" && deliveredAt != nil {
		deliveryState = generated.OwnerGateDeliveryStateREADY
		nextAction = generated.OwnerGateNextActionRESOLVE
		resolvable = true
	}
	return generated.ResourceSpecProjection{OwnerGate: &generated.OwnerGateProjection{
		ProcessRunId: processID, ResultSha256: value.GetResultSha256(), ExpiresAt: value.GetExpiresAt().AsTime(),
		Decision: decision, SessionId: sessionID, TurnId: turnID, Attempt: int(value.GetAttempt()),
		ImmutableInputSha256: value.GetImmutableInputSha256(), DeliveryState: deliveryState,
		Resolvable: resolvable, DeliveredAt: deliveredAt, NextAction: nextAction,
	}}, nil
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
	drift, driftErr := castClosedEnum(value.GetDrift().String(), "CONFIGURATION_DRIFT_", generated.ConfigurationDrift.Valid)
	if driftErr != nil {
		return generated.ConfigurationOwnershipProjection{}, errors.New("configuration drift is unavailable")
	}
	var sourceSHA256 *generated.Sha256
	if value.GetSourceSha256() != "" {
		if !validSHA256(value.GetSourceSha256()) {
			return generated.ConfigurationOwnershipProjection{}, errors.New("configuration source digest is invalid")
		}
		digest := generated.Sha256(strings.ToLower(value.GetSourceSha256()))
		sourceSHA256 = &digest
	}
	switch value.GetManagedBy() {
	case controlplanev1.ConfigurationManager_CONFIGURATION_MANAGER_UI:
		if value.GetSourceRef() != "" && value.GetSourceRevision() != 0 {
			return generated.ConfigurationOwnershipProjection{ManagedBy: generated.Ui, Source: value.GetSourceRef(), Revision: int64(value.GetSourceRevision()), SourceSha256: sourceSHA256, Drift: drift}, nil
		}
		if value.GetSourceRef() != "" || value.GetSourceRevision() != 0 {
			return generated.ConfigurationOwnershipProjection{}, errors.New("ui ownership lineage is incomplete")
		}
		return generated.ConfigurationOwnershipProjection{ManagedBy: generated.Ui, Source: "owner-ui", Revision: int64(resourceVersion), SourceSha256: sourceSHA256, Drift: drift}, nil
	case controlplanev1.ConfigurationManager_CONFIGURATION_MANAGER_GIT:
		if value.GetSourceRef() == "" || value.GetSourceRevision() == 0 {
			return generated.ConfigurationOwnershipProjection{}, errors.New("git ownership is incomplete")
		}
		return generated.ConfigurationOwnershipProjection{ManagedBy: generated.Git, Source: value.GetSourceRef(), Revision: int64(value.GetSourceRevision()), SourceSha256: sourceSHA256, Drift: drift}, nil
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

func ConvertIncidentOwnerProjection(input *controlplanev1.RuntimeIncidentOwnerProjection) (generated.IncidentView, error) {
	return castIncidentView(input)
}

func ConvertRunOwnerProjection(input *controlplanev1.RunOwnerProjection) (generated.RunView, error) {
	return castRunView(input)
}
