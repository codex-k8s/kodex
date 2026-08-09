package resource

import (
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

func legacyOwnership(source entity.LegacyOperationSource) entity.ConfigurationOwnership {
	return entity.ConfigurationOwnership{
		ManagedBy: "UI", SourceRef: source.SourceRef,
		SourceRevision: source.SourceRevision, SourceSHA256: source.SourceSHA256,
	}
}

type legacyResourceResult struct {
	resource entity.Resource
	err      error
}

func legacyResource(principal value.Principal, projectID, parentID, targetID string,
	kind enum.Kind, name string, state enum.State, spec entity.Spec, at time.Time,
) legacyResourceResult {
	resource := entity.Resource{
		ID: targetID, OrganizationID: principal.OrganizationID, ProjectID: projectID,
		ParentID: parentID, OwnerActorID: principal.ActorID, Kind: kind, Name: name,
		State: state, Version: 1, Spec: spec,
		CreatedAt: at.UTC().Truncate(time.Microsecond), UpdatedAt: at.UTC().Truncate(time.Microsecond),
	}
	if resource.Validate() != nil || !validLegacyState(string(resource.State)) {
		return legacyResourceResult{err: errs.ErrInvalidInput}
	}
	return legacyResourceResult{resource: resource}
}

func (service *Service) compileLegacyGraph(principal value.Principal,
	plan entity.LegacyGraphPlan,
) (compiledLegacyGraph, error) {
	compiled := compiledLegacyGraph{
		Resources: make(map[string]entity.Resource, len(plan.Operations)),
		Sources:   make(map[string]entity.LegacyOperationSource, len(plan.Operations)),
		Kinds:     make(map[string]string, len(plan.Operations)),
		Derived:   make(map[string][]entity.Resource),
	}
	operations := make(map[string]entity.LegacyGraphOperation, len(plan.Operations))
	for _, operation := range plan.Operations {
		source, kind, err := legacyOperationSource(operation)
		if err != nil {
			return compiledLegacyGraph{}, err
		}
		compiled.Sources[source.LocalRef], compiled.Kinds[source.LocalRef] = source, kind
		operations[source.LocalRef] = operation
	}
	projectRef := plan.Operations[0].Project.Source.LocalRef
	projectID := plan.Operations[0].TargetID
	at := service.now().UTC().Truncate(time.Microsecond)
	add := func(reference string, result legacyResourceResult) error {
		if result.err != nil || result.resource.ID != operations[reference].TargetID || result.resource.ProjectID != projectID {
			if result.err != nil {
				return result.err
			}
			return errs.ErrInternal
		}
		compiled.Resources[reference] = result.resource
		return nil
	}
	digest := func(reference string, expected enum.Kind) (entity.Resource, string, error) {
		resource, ok := compiled.Resources[reference]
		if !ok || resource.Kind != expected || resource.OwnerActorID != principal.ActorID ||
			resource.OrganizationID != principal.OrganizationID || resource.ProjectID != projectID {
			return entity.Resource{}, "", errs.ErrFailedPrecondition
		}
		sha256, err := entity.ProjectionSHA256(resource)
		if err != nil {
			return entity.Resource{}, "", errs.ErrInternal
		}
		return resource, sha256, nil
	}

	projectOperation := operations[projectRef]
	project := projectOperation.Project
	if project == nil {
		return compiledLegacyGraph{}, errs.ErrInvalidInput
	}
	if err := add(projectRef, legacyResource(principal, projectID, "", projectOperation.TargetID,
		enum.KindProject, project.Name, enum.StateActive, entity.ProjectSpec{
			Slug: project.Slug, Description: project.Description, Locale: project.Locale,
			Ownership: legacyOwnership(project.Source),
		}, at)); err != nil {
		return compiledLegacyGraph{}, err
	}

	// Artifact, credential и workspace — минимальные immutable prerequisites.
	for reference, operation := range operations {
		switch input := operation.Artifact; {
		case input != nil:
			if input.StorageVersion == "" || !strings.Contains(input.StorageRef, input.StorageVersion) ||
				input.ScanEvidenceSHA256 == "" || !legacyTimeWithinBounds(input.ScannedAt) {
				return compiledLegacyGraph{}, errs.ErrFailedPrecondition
			}
			if err := add(reference, legacyResource(principal, projectID, "", operation.TargetID,
				enum.KindArtifact, input.Name, enum.StateActive, entity.ArtifactSpec{
					ArtifactKind: input.ArtifactKind, Direction: input.Direction, StorageRef: input.StorageRef,
					SizeBytes: input.SizeBytes, MediaType: input.MediaType, SHA256: input.SHA256,
					ScanStatus: "CLEAN", RetentionPolicyRef: input.RetentionPolicyRef,
					ScanPolicyRevision: input.ScanPolicyRevision, ScanEvidenceSHA256: input.ScanEvidenceSHA256,
					ScannerWorkloadID: input.ScannerWorkloadID, ScannedAt: input.ScannedAt,
				}, at)); err != nil {
				return compiledLegacyGraph{}, err
			}
		case operation.CredentialBinding != nil:
			input := operation.CredentialBinding
			providerEligible := input.Purpose == "provider-account"
			if err := add(reference, legacyResource(principal, projectID, "", operation.TargetID,
				enum.KindCredentialBinding, input.Name, enum.StateActive, entity.CredentialBindingSpec{
					Purpose: input.Purpose, SecretRef: input.SecretRef, ImmutableSecretRef: input.ImmutableSecretRef,
					PrincipalRef: input.PrincipalRef, Revision: input.Revision,
					ProviderEligible: providerEligible, ProviderCapabilities: slices.Clone(input.ProviderCapabilities),
					ProviderObservedUsage: input.ObservedUsage, ProviderObservedLimit: input.ObservedLimit,
					ProviderObservationRevision: input.ObservationRevision, ProviderObservedAt: input.ObservedAt,
					ProviderContentVersion: input.ContentVersion, ContentSHA256: input.ContentSHA256,
					Ownership: legacyOwnership(input.Source),
				}, at)); err != nil {
				return compiledLegacyGraph{}, err
			}
		}
	}
	for reference, operation := range operations {
		input := operation.RepositoryWorkspace
		if input == nil {
			continue
		}
		spec := entity.RepositoryWorkspaceSpec{
			RepositoryRef: input.RepositoryRef, WorkspaceMode: input.WorkspaceMode,
			DefaultBranch: input.DefaultBranch, Ownership: legacyOwnership(input.Source),
		}
		if input.CredentialBindingRef != "" {
			credential, _, err := digest(input.CredentialBindingRef, enum.KindCredentialBinding)
			if err != nil {
				return compiledLegacyGraph{}, err
			}
			spec.CredentialBindingID = credential.ID
		}
		if input.SnapshotArtifactRef != "" {
			artifact, artifactSHA, err := digest(input.SnapshotArtifactRef, enum.KindArtifact)
			if err != nil {
				return compiledLegacyGraph{}, err
			}
			spec.SnapshotArtifactID, spec.SnapshotVersion, spec.SnapshotSHA256 = artifact.ID, artifact.Version, artifactSHA
		}
		if err := add(reference, legacyResource(principal, projectID, "", operation.TargetID,
			enum.KindRepositoryWorkspace, input.Name, enum.StateActive, spec, at)); err != nil {
			return compiledLegacyGraph{}, err
		}
	}

	// Recipe materializes through the real specialized schema, with policy domains resolved server-side.
	for reference, operation := range operations {
		input := operation.RoleImageRecipe
		if input == nil {
			continue
		}
		spec, err := service.newRoleImageRecipeSpec(canonicalRoleImageInput(input.Input), input.Generation)
		if err != nil || spec.Generation != input.Generation || spec.SpecSHA256 != input.SpecSHA256 ||
			spec.PolicyRevision != input.PolicyRevision || spec.PolicySHA256 != input.PolicySHA256 ||
			spec.RoleRuntimeContractRevision != input.RuntimeContractRevision ||
			spec.RoleRuntimeContractSHA256 != input.RuntimeContractSHA256 {
			return compiledLegacyGraph{}, errs.ErrFailedPrecondition
		}
		if err := add(reference, legacyResource(principal, projectID, "", operation.TargetID,
			enum.KindRoleImageRecipe, input.Name, enum.StateActive, spec, at)); err != nil {
			return compiledLegacyGraph{}, err
		}
	}
	for reference, operation := range operations {
		input := operation.ImageBuild
		if input == nil {
			continue
		}
		recipe, _, err := digest(input.RecipeRef, enum.KindRoleImageRecipe)
		if err != nil {
			return compiledLegacyGraph{}, err
		}
		recipeSpec := recipe.Spec.(entity.RoleImageRecipeSpec)
		if input.TerminalState != string(enum.StateSucceeded) || input.TerminalEvidenceSHA256 != input.ProvenanceSHA256 {
			return compiledLegacyGraph{}, errs.ErrFailedPrecondition
		}
		immutableBuildSHA256, hashErr := canonicalHash(struct {
			RecipeID                        string
			RecipeVersion, RecipeGeneration uint64
			SpecSHA256                      string
			Input                           entity.RoleImageRecipeInput
		}{recipe.ID, recipe.Version, recipeSpec.Generation, recipeSpec.SpecSHA256, recipeSpec.Input})
		if hashErr != nil || input.ImmutableBuildSHA256 != "" {
			return compiledLegacyGraph{}, errs.ErrFailedPrecondition
		}
		spec := entity.ImageBuildSpec{
			RecipeID: recipe.ID, RecipeVersion: recipe.Version, RecipeGeneration: recipeSpec.Generation,
			SpecSHA256: recipeSpec.SpecSHA256, Attempt: input.Attempt,
			Stage: entity.ImageBuildStageCompleted, ProgressPercent: 100,
			StagingReference: input.StagingReference, ManifestDigest: input.ManifestDigest,
			ProvenanceSHA256: input.ProvenanceSHA256, ImmutableBuildSHA256: immutableBuildSHA256,
			AvailableAt: at, MaximumAttempts: service.imageMaximumAttempts,
		}
		if err := add(reference, legacyResource(principal, projectID, recipe.ID, operation.TargetID,
			enum.KindImageBuild, input.Name, enum.StateSucceeded, spec, at)); err != nil {
			return compiledLegacyGraph{}, err
		}
	}
	for reference, operation := range operations {
		input := operation.ImageArtifact
		if input == nil {
			continue
		}
		recipe, _, err := digest(input.RecipeRef, enum.KindRoleImageRecipe)
		if err != nil {
			return compiledLegacyGraph{}, err
		}
		build, _, err := digest(input.ImageBuildRef, enum.KindImageBuild)
		if err != nil {
			return compiledLegacyGraph{}, err
		}
		recipeSpec := recipe.Spec.(entity.RoleImageRecipeSpec)
		buildSpec := build.Spec.(entity.ImageBuildSpec)
		if input.ManifestDigest != buildSpec.ManifestDigest || input.PromotedReference == buildSpec.StagingReference {
			return compiledLegacyGraph{}, errs.ErrFailedPrecondition
		}
		spec := entity.ImageArtifactSpec{
			RecipeID: recipe.ID, RecipeVersion: recipe.Version, RecipeGeneration: recipeSpec.Generation,
			SpecSHA256: recipeSpec.SpecSHA256, BuildID: build.ID, BuildVersion: build.Version,
			BuildAttempt: buildSpec.Attempt, StagingReference: buildSpec.StagingReference,
			ManifestDigest: buildSpec.ManifestDigest, ProvenanceSHA256: buildSpec.ProvenanceSHA256,
			ImmutableBuildSHA256: buildSpec.ImmutableBuildSHA256,
			BaseImageDigest:      recipeSpec.Input.BaseImageDigest, SourceSHA256: recipeSpec.Input.SourceSHA256,
			ContextSHA256: recipeSpec.Input.ContextSHA256, BuilderSHA256: recipeSpec.Input.BuilderSHA256,
			FrontendSHA256: recipeSpec.Input.FrontendSHA256, ToolchainSHA256: recipeSpec.Input.ToolchainSHA256,
			Platforms: slices.Clone(recipeSpec.Input.Platforms), SBOMSHA256: input.SBOMSHA256,
			VulnerabilityEvidenceSHA256: input.VulnerabilityEvidenceSHA256,
			PolicyRevision:              recipeSpec.PolicyRevision, PolicySHA256: recipeSpec.PolicySHA256,
			AdmissionVerdict: entity.ImageAdmissionAccepted, SignatureIdentity: input.SignatureIdentity,
			SignatureSHA256: input.SignatureSHA256, AdmissionRevision: input.AdmissionRevision,
			AdmissionReceiptSHA256:            input.AdmissionReceiptSHA256,
			AdmissionReceiptOCIManifestDigest: input.AdmissionReceiptManifestDigest,
			RoleRuntimeContractRevision:       recipeSpec.RoleRuntimeContractRevision,
			RoleRuntimeContractSHA256:         recipeSpec.RoleRuntimeContractSHA256,
			PromotedReference:                 input.PromotedReference, PromotionReadbackSHA256: input.PromotionReadbackSHA256,
			PromotedAt: input.PromotedAt,
		}
		if err := add(reference, legacyResource(principal, projectID, recipe.ID, operation.TargetID,
			enum.KindImageArtifact, input.Name, enum.StateActive, spec, at)); err != nil {
			return compiledLegacyGraph{}, err
		}
	}

	// Protected catalog: each resource is version 1 and receives history in the owner transaction.
	for reference, operation := range operations {
		input := operation.InstructionSet
		if input == nil {
			continue
		}
		artifact, _, err := digest(input.ContentArtifactRef, enum.KindArtifact)
		if err != nil {
			return compiledLegacyGraph{}, err
		}
		spec := entity.InstructionSetSpec{
			StableKey: input.StableKey, Locale: input.Locale, CurrentVersion: 1, PublishedVersion: 1,
			Content: input.Content, ContentSHA256: input.ContentSHA256, VersionState: "PUBLISHED",
			ValidationSHA256: input.ValidationSHA256, ValidationSucceeded: true,
			ValidatedContentVersion: 1, ValidatedContentSHA256: input.ContentSHA256,
			ContentArtifactID: artifact.ID, ContentArtifactVersion: artifact.Version,
			Ownership: legacyOwnership(input.Source),
		}
		if err := add(reference, legacyResource(principal, projectID, "", operation.TargetID,
			enum.KindInstructionSet, input.Name, enum.StateActive, spec, at)); err != nil {
			return compiledLegacyGraph{}, err
		}
	}
	for reference, operation := range operations {
		input := operation.ProviderReference
		if input == nil {
			continue
		}
		credential, credentialSHA, err := digest(input.CredentialBindingRef, enum.KindCredentialBinding)
		if err != nil {
			return compiledLegacyGraph{}, err
		}
		spec := entity.ProviderConnectionReferenceSpec{
			StableKey: input.StableKey, Provider: input.Provider, ServerReference: input.ServerReference,
			ReferenceVersion: input.ReferenceVersion, ReferenceGeneration: input.ReferenceGeneration,
			ReferenceSHA256: input.ReferenceSHA256, MaskedLabel: input.MaskedLabel,
			MaskedStatus: input.MaskedStatus, Capabilities: slices.Clone(input.Capabilities),
			Eligible:   input.MaskedStatus == "AVAILABLE" || input.MaskedStatus == "DEGRADED",
			ObservedAt: input.ObservedAt, ReceiptID: input.ReceiptID, ReceiptVersion: input.ReceiptVersion,
			ReceiptSHA256: input.ReceiptSHA256, CredentialBindingID: credential.ID,
			CredentialBindingVersion: credential.Version, CredentialBindingSHA256: credentialSHA,
		}
		if err := add(reference, legacyResource(principal, projectID, "", operation.TargetID,
			enum.KindProviderReference, input.Name, enum.StateActive, spec, at)); err != nil {
			return compiledLegacyGraph{}, err
		}
	}
	for reference, operation := range operations {
		input := operation.ProviderPool
		if input == nil {
			continue
		}
		bindings := make([]entity.ProviderPoolBinding, 0, len(input.Bindings))
		for _, binding := range input.Bindings {
			provider, providerSHA, err := digest(binding.ProviderReferenceRef, enum.KindProviderReference)
			if err != nil {
				return compiledLegacyGraph{}, err
			}
			providerSpec := provider.Spec.(entity.ProviderConnectionReferenceSpec)
			bindings = append(bindings, entity.ProviderPoolBinding{
				ProviderConnectionReferenceID: provider.ID, ProviderConnectionStableKey: providerSpec.StableKey,
				ReferenceVersion: provider.Version, ReferenceSHA256: providerSHA, Weight: binding.Weight,
				Eligible: providerSpec.Eligible, MaskedStatus: providerSpec.MaskedStatus,
			})
		}
		slices.SortFunc(bindings, func(left, right entity.ProviderPoolBinding) int {
			return strings.Compare(left.ProviderConnectionReferenceID, right.ProviderConnectionReferenceID)
		})
		spec := entity.ProviderPoolSpec{
			StableKey: input.StableKey, Policy: input.Policy, PolicyRevision: input.PolicyRevision,
			ObservationMaxAge: input.ObservationMaxAge, Bindings: bindings,
			EligibilitySnapshotSHA256: strings.Repeat("0", 64), Ownership: legacyOwnership(input.Source),
		}
		digest, err := canonicalHash(spec)
		if err != nil || input.EligibilitySnapshotSHA256 != "" {
			return compiledLegacyGraph{}, errs.ErrFailedPrecondition
		}
		spec.EligibilitySnapshotSHA256 = digest
		if err := add(reference, legacyResource(principal, projectID, "", operation.TargetID,
			enum.KindProviderPool, input.Name, enum.StateActive, spec, at)); err != nil {
			return compiledLegacyGraph{}, err
		}
	}
	for reference, operation := range operations {
		input := operation.RoleDefinition
		if input == nil {
			continue
		}
		recipe, recipeSHA, err := digest(input.RoleImageRecipeRef, enum.KindRoleImageRecipe)
		if err != nil {
			return compiledLegacyGraph{}, err
		}
		allowed := make([]string, 0, len(input.AllowedRoleRefs))
		for _, allowedRef := range input.AllowedRoleRefs {
			allowed = append(allowed, operations[allowedRef].TargetID)
		}
		slices.Sort(allowed)
		spec := entity.RoleDefinitionSpec{
			StableKey: input.StableKey, Description: input.Description,
			Capabilities: slices.Clone(input.Capabilities), AllowedTargetRoleDefinitionIDs: allowed,
			RoleImageRecipeID: recipe.ID, RoleImageRecipeVersion: recipe.Version,
			RoleImageRecipeSHA256: recipeSHA, Ownership: legacyOwnership(input.Source),
		}
		if err := add(reference, legacyResource(principal, projectID, "", operation.TargetID,
			enum.KindRoleDefinition, input.Name, enum.StateActive, spec, at)); err != nil {
			return compiledLegacyGraph{}, err
		}
	}
	for reference, operation := range operations {
		input := operation.Agent
		if input == nil {
			continue
		}
		role, roleSHA, err := digest(input.RoleDefinitionRef, enum.KindRoleDefinition)
		if err != nil {
			return compiledLegacyGraph{}, err
		}
		instruction, instructionSHA, err := digest(input.InstructionSetRef, enum.KindInstructionSet)
		if err != nil {
			return compiledLegacyGraph{}, err
		}
		pool, poolSHA, err := digest(input.ProviderPoolRef, enum.KindProviderPool)
		if err != nil {
			return compiledLegacyGraph{}, err
		}
		recipe, recipeSHA, err := digest(input.RoleImageRecipeRef, enum.KindRoleImageRecipe)
		if err != nil {
			return compiledLegacyGraph{}, err
		}
		roleSpec, roleOK := role.Spec.(entity.RoleDefinitionSpec)
		instructionSpec, instructionOK := instruction.Spec.(entity.InstructionSetSpec)
		poolSpec, poolOK := pool.Spec.(entity.ProviderPoolSpec)
		if !roleOK || !instructionOK || !poolOK {
			return compiledLegacyGraph{}, errs.ErrStateConflict
		}
		spec := entity.AgentSpec{
			StableKey: input.StableKey, RoleDefinitionID: role.ID, RoleDefinitionVersion: role.Version,
			RoleDefinitionSHA256: roleSHA, InstructionSetID: instruction.ID,
			InstructionSetVersion: instruction.Version, InstructionSetSHA256: instructionSHA,
			ProviderPoolID: pool.ID, ProviderPoolVersion: pool.Version, ProviderPoolSHA256: poolSHA,
			OwnerRoleSelector: roleSpec.StableKey, OwnerInstructionSelector: instructionSpec.StableKey,
			OwnerProviderPoolSelector: poolSpec.StableKey,
			RuntimeProfileRef:         "control-plane://runtime-profile/" + recipe.ID,
			RuntimeProfileVersion:     recipe.Version, RuntimeProfileSHA256: recipeSHA,
			Capabilities: slices.Clone(input.Capabilities), Enabled: input.Enabled,
			BotIdentityRef: input.BotIdentityRef, BotUsername: input.BotUsername,
			BotProviderRevision: input.BotProviderRevision, BotProviderGeneration: input.BotProviderGeneration,
			BotProviderTeamRef: input.BotTeamRef, BotMaskedStatus: input.BotMaskedStatus,
			BotReceiptID: input.BotReceiptID, BotReceiptVersion: input.BotReceiptVersion,
			BotReceiptSHA256: input.BotReceiptSHA256, Ownership: legacyOwnership(input.Source),
		}
		if err := add(reference, legacyResource(principal, projectID, "", operation.TargetID,
			enum.KindAgent, input.Name, enum.StateActive, spec, at)); err != nil {
			return compiledLegacyGraph{}, err
		}
	}
	projectResource, projectSHA, _ := digest(projectRef, enum.KindProject)
	for reference, operation := range operations {
		input := operation.AgentAssignment
		if input == nil {
			continue
		}
		agent, agentSHA, err := digest(input.AgentRef, enum.KindAgent)
		if err != nil {
			return compiledLegacyGraph{}, err
		}
		spec := entity.AgentAssignmentSpec{
			AgentID: agent.ID, AgentVersion: agent.Version, AgentSHA256: agentSHA,
			WorkspaceID: projectResource.ID, WorkspaceVersion: projectResource.Version,
			WorkspaceSHA256: projectSHA, RoomID: legacyTargetID(plan, input.RoomRef),
			RootActorID: principal.ActorID, AssignmentGeneration: input.AssignmentGeneration,
		}
		if err := add(reference, legacyResource(principal, projectID, "", operation.TargetID,
			enum.KindAgentAssignment, input.Name, enum.StateActive, spec, at)); err != nil {
			return compiledLegacyGraph{}, err
		}
	}
	for reference, operation := range operations {
		switch input := operation.Chat; {
		case input != nil:
			if err := add(reference, legacyResource(principal, projectID, "", operation.TargetID,
				enum.KindChat, input.Name, enum.StateActive, entity.ChatSpec{
					StableKey: input.StableKey, RoomType: input.RoomType,
					DefaultAgentID:     legacyTargetID(plan, input.DefaultAgentRef),
					ExternalChannelRef: input.ExternalChannelRef, WorkPolicy: input.WorkPolicy,
					Ownership: legacyOwnership(input.Source),
				}, at)); err != nil {
				return compiledLegacyGraph{}, err
			}
		case operation.Team != nil:
			input := operation.Team
			roleIDs := make([]string, 0, len(input.RoleDefinitionRefs))
			for _, item := range input.RoleDefinitionRefs {
				roleIDs = append(roleIDs, operations[item].TargetID)
			}
			slices.Sort(roleIDs)
			if err := add(reference, legacyResource(principal, projectID, "", operation.TargetID,
				enum.KindTeam, input.Name, enum.StateActive, entity.TeamSpec{
					StableKey: input.StableKey, ExternalTeamRef: input.ExternalTeamRef,
					MemberActorIDs: []string{principal.ActorID}, RoleIDs: roleIDs,
					Ownership: legacyOwnership(input.Source),
				}, at)); err != nil {
				return compiledLegacyGraph{}, err
			}
		}
	}
	for reference, operation := range operations {
		input := operation.Session
		if input == nil {
			continue
		}
		agent, agentSHA, err := digest(input.AgentRef, enum.KindAgent)
		if err != nil {
			return compiledLegacyGraph{}, err
		}
		pool, poolSHA, err := digest(input.ProviderPoolRef, enum.KindProviderPool)
		if err != nil {
			return compiledLegacyGraph{}, err
		}
		assignment, assignmentSHA, err := digest(input.AssignmentRef, enum.KindAgentAssignment)
		if err != nil {
			return compiledLegacyGraph{}, err
		}
		spec := entity.SessionSpec{
			AgentID: agent.ID, AgentVersion: agent.Version, AgentSHA256: agentSHA,
			ProviderPoolID: pool.ID, ProviderPoolVersion: pool.Version, ProviderPoolSHA256: poolSHA,
			AgentAssignmentID: assignment.ID, AgentAssignmentVersion: assignment.Version,
			AgentAssignmentSHA256: assignmentSHA, ConversationID: legacyTargetID(plan, input.ChatRef),
			ArchiveRef: input.ArchiveRef, LastTurnSequence: input.LastTurnSequence,
		}
		if err := add(reference, legacyResource(principal, projectID, agent.ID, operation.TargetID,
			enum.KindSession, input.Name, input.State, spec, at)); err != nil {
			return compiledLegacyGraph{}, err
		}
	}

	if err := service.compileLegacyDerivedRuntime(principal, projectID, operations, &compiled, at); err != nil {
		return compiledLegacyGraph{}, err
	}
	if err := service.compileLegacyRuntimeRevisions(principal, projectID, operations, &compiled, at); err != nil {
		return compiledLegacyGraph{}, err
	}
	if err := service.compileLegacySchedules(principal, projectID, operations, &compiled, at); err != nil {
		return compiledLegacyGraph{}, err
	}
	if err := service.compileLegacyTurnsAndProcesses(principal, projectID, plan, operations, &compiled, at); err != nil {
		return compiledLegacyGraph{}, err
	}
	for reference, operation := range operations {
		input := operation.MemoryRecord
		if input == nil {
			continue
		}
		provenance := input.Source.SourceRef + "#kind=" + input.MemoryKind
		if err := add(reference, legacyResource(principal, projectID, "", operation.TargetID,
			enum.KindMemoryRecord, input.Name, input.State, entity.MemoryRecordSpec{
				Scope: "PROJECT", Title: input.Name, Content: input.Content,
				ContentSHA256: input.ContentSHA256, Provenance: provenance, Importance: 50,
			}, at)); err != nil {
			return compiledLegacyGraph{}, err
		}
	}
	return compiled, nil
}

func (service *Service) compileLegacyDerivedRuntime(principal value.Principal, projectID string,
	operations map[string]entity.LegacyGraphOperation, compiled *compiledLegacyGraph, at time.Time,
) error {
	for reference, operation := range operations {
		input := operation.Agent
		if input == nil {
			continue
		}
		agent := compiled.Resources[reference]
		agentSHA, _ := entity.ProjectionSHA256(agent)
		instruction := compiled.Resources[input.InstructionSetRef]
		instructionSHA, _ := entity.ProjectionSHA256(instruction)
		instructionSpec, instructionOK := instruction.Spec.(entity.InstructionSetSpec)
		pool := compiled.Resources[input.ProviderPoolRef]
		poolSpec, poolOK := pool.Spec.(entity.ProviderPoolSpec)
		if !instructionOK || !poolOK {
			return errs.ErrFailedPrecondition
		}
		credentialIDs := make([]string, 0, len(poolSpec.Bindings))
		roleBindings := make([]entity.ProviderAccountPoolBinding, 0, len(poolSpec.Bindings))
		for _, binding := range poolSpec.Bindings {
			provider := findLegacyResourceByID(compiled.Resources, binding.ProviderConnectionReferenceID)
			providerSpec, ok := provider.Spec.(entity.ProviderConnectionReferenceSpec)
			if !ok {
				return errs.ErrFailedPrecondition
			}
			credentialIDs = append(credentialIDs, providerSpec.CredentialBindingID)
			roleBindings = append(roleBindings, entity.ProviderAccountPoolBinding{
				CredentialBindingID: providerSpec.CredentialBindingID, Weight: binding.Weight,
			})
		}
		slices.Sort(credentialIDs)
		slices.SortFunc(roleBindings, func(left, right entity.ProviderAccountPoolBinding) int {
			return strings.Compare(left.CredentialBindingID, right.CredentialBindingID)
		})
		promptID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("mattercodex:agent-prompt:"+instruction.ID)).String()
		roleID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("mattercodex:agent-role:"+agent.ID)).String()
		prompt := entity.Resource{
			ID: promptID, OrganizationID: principal.OrganizationID, ProjectID: projectID,
			ParentID: instruction.ID, OwnerActorID: principal.ActorID, Kind: enum.KindPromptProfile,
			Name: "Derived Agent prompt", State: enum.StateActive, Version: instruction.Version,
			Spec: entity.PromptProfileSpec{
				Revision: instructionSpec.CurrentVersion, ContentSHA256: instructionSpec.ContentSHA256,
				SourceRef: "control-plane://instruction-set/" + instruction.ID, Locale: instructionSpec.Locale,
				ContentArtifactID:      instructionSpec.ContentArtifactID,
				ContentArtifactVersion: instructionSpec.ContentArtifactVersion,
				Ownership: entity.ConfigurationOwnership{ManagedBy: "UI",
					SourceRef:      "control-plane://instruction-set/" + instruction.ID,
					SourceRevision: instruction.Version, SourceSHA256: instructionSHA},
			}, CreatedAt: at, UpdatedAt: at,
		}
		agentSpec := agent.Spec.(entity.AgentSpec)
		role := entity.Resource{
			ID: roleID, OrganizationID: principal.OrganizationID, ProjectID: projectID,
			ParentID: agent.ID, OwnerActorID: principal.ActorID, Kind: enum.KindRole,
			Name: "Derived Agent runtime role", State: enum.StateActive, Version: agent.Version,
			Spec: entity.RoleSpec{
				StableKey: agentSpec.StableKey, Capabilities: slices.Clone(agentSpec.Capabilities),
				PromptProfileID: prompt.ID, ProviderCredentialBindingIDs: credentialIDs,
				ProviderAccountPool: entity.ProviderAccountPool{Policy: poolSpec.Policy,
					PolicyRevision: poolSpec.PolicyRevision, ObservationMaxAge: poolSpec.ObservationMaxAge,
					Bindings: roleBindings}, RoleImageRecipeID: legacyTargetIDFromOperations(operations, input.RoleImageRecipeRef),
				Ownership: entity.ConfigurationOwnership{ManagedBy: "UI",
					SourceRef:      "control-plane://agent/" + agent.ID,
					SourceRevision: agent.Version, SourceSHA256: agentSHA},
			}, CreatedAt: at, UpdatedAt: at,
		}
		if prompt.Validate() != nil || role.Validate() != nil {
			return errs.ErrInternal
		}
		compiled.Derived[reference] = []entity.Resource{prompt, role}
	}
	return nil
}

func legacyTargetIDFromOperations(operations map[string]entity.LegacyGraphOperation, reference string) string {
	if operation, ok := operations[reference]; ok {
		return operation.TargetID
	}
	return ""
}

func findLegacyResourceByID(resources map[string]entity.Resource, identifier string) entity.Resource {
	for _, resource := range resources {
		if resource.ID == identifier {
			return resource
		}
	}
	return entity.Resource{}
}
