package resource

import (
	"context"
	"crypto/md5"
	"errors"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

const (
	permissionLegacyCutoverRead      = "controlplane.legacy_configuration.read"
	permissionLegacyCutoverReconcile = "controlplane.legacy_configuration.reconcile"
)

func (service *Service) GetLegacyConfigurationCutover(ctx context.Context, principal value.Principal,
	legacyRoleID string,
) (domainrepo.LegacyConfigurationCutover, error) {
	if err := authorize(principal, permissionLegacyCutoverRead); err != nil {
		return domainrepo.LegacyConfigurationCutover{}, err
	}
	if value.ValidateID(legacyRoleID) != nil {
		return domainrepo.LegacyConfigurationCutover{}, errs.ErrInvalidInput
	}
	repository, ok := service.repository.(domainrepo.LegacyConfigurationCutoverRepository)
	if !ok {
		return domainrepo.LegacyConfigurationCutover{}, errs.ErrUnavailable
	}
	return repository.GetLegacyConfigurationCutover(ctx, principal.OrganizationID, principal.ProjectID,
		principal.ActorID, legacyRoleID)
}

func (service *Service) ListLegacyConfigurationCutovers(ctx context.Context, principal value.Principal,
	afterLegacyRoleID string, limit int,
) ([]domainrepo.LegacyConfigurationCutover, error) {
	if err := authorize(principal, permissionLegacyCutoverRead); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 || afterLegacyRoleID != "" && value.ValidateID(afterLegacyRoleID) != nil {
		return nil, errs.ErrInvalidInput
	}
	repository, ok := service.repository.(domainrepo.LegacyConfigurationCutoverRepository)
	if !ok {
		return nil, errs.ErrUnavailable
	}
	return repository.ListLegacyConfigurationCutovers(ctx, principal.OrganizationID, principal.ProjectID,
		principal.ActorID, afterLegacyRoleID, limit)
}

func (service *Service) ResolveLegacyConfigurationCutover(ctx context.Context,
	input ResolveLegacyConfigurationCutoverInput,
) (ResolveLegacyConfigurationCutoverResult, error) {
	if err := authorize(input.Principal, permissionLegacyCutoverReconcile); err != nil {
		return ResolveLegacyConfigurationCutoverResult{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || value.ValidateID(input.LegacyRoleID) != nil ||
		input.ExpectedLegacyRoleVersion == 0 || input.ExpectedLegacyPromptVersion == 0 ||
		len(input.InstructionContent) == 0 || len(input.InstructionContent) > 262144 {
		return ResolveLegacyConfigurationCutoverResult{}, errs.ErrInvalidInput
	}
	repository, ok := service.repository.(domainrepo.LegacyConfigurationCutoverRepository)
	if !ok {
		return ResolveLegacyConfigurationCutoverResult{}, errs.ErrUnavailable
	}
	// Owner lookup and immutable source tuple are resolved before idempotency.
	cutover, err := repository.GetLegacyConfigurationCutover(ctx, input.Principal.OrganizationID,
		input.Principal.ProjectID, input.Principal.ActorID, input.LegacyRoleID)
	if err != nil {
		return ResolveLegacyConfigurationCutoverResult{}, err
	}
	if cutover.LegacyRoleVersion != input.ExpectedLegacyRoleVersion ||
		cutover.LegacyPromptVersion != input.ExpectedLegacyPromptVersion {
		return ResolveLegacyConfigurationCutoverResult{}, errs.ErrVersionMismatch
	}
	contentSHA := hashString(input.InstructionContent)
	if contentSHA != cutover.SourcePromptSHA256 {
		return ResolveLegacyConfigurationCutoverResult{}, errs.ErrStateConflict
	}
	requestHash, err := semanticCommandHash(input.Principal, struct {
		LegacyRoleID, ContentSHA256            string
		LegacyRoleVersion, LegacyPromptVersion uint64
	}{input.LegacyRoleID, contentSHA, input.ExpectedLegacyRoleVersion, input.ExpectedLegacyPromptVersion})
	if err != nil {
		return ResolveLegacyConfigurationCutoverResult{}, errs.ErrInvalidInput
	}
	agent, err := service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		"legacy_configuration_reconcile", requestHash, func(tx domainrepo.Transaction) (entity.Resource, error) {
			return service.materializeLegacyConfiguration(ctx, tx, input, cutover)
		})
	if err != nil {
		return ResolveLegacyConfigurationCutoverResult{}, err
	}
	cutover, err = repository.GetLegacyConfigurationCutover(ctx, input.Principal.OrganizationID,
		input.Principal.ProjectID, input.Principal.ActorID, input.LegacyRoleID)
	if err != nil {
		return ResolveLegacyConfigurationCutoverResult{}, err
	}
	return ResolveLegacyConfigurationCutoverResult{Cutover: cutover, Agent: agent}, nil
}

func (service *Service) materializeLegacyConfiguration(ctx context.Context, tx domainrepo.Transaction,
	input ResolveLegacyConfigurationCutoverInput, expected domainrepo.LegacyConfigurationCutover,
) (entity.Resource, error) {
	cutoverTx, ok := tx.(domainrepo.LegacyConfigurationCutoverTransaction)
	protected, protectedOK := tx.(domainrepo.ProtectedTransaction)
	if !ok || !protectedOK {
		return entity.Resource{}, errs.ErrInternal
	}
	cutover, err := cutoverTx.GetLegacyConfigurationCutoverForUpdate(ctx, input.LegacyRoleID)
	if err != nil {
		return entity.Resource{}, err
	}
	if !reflect.DeepEqual(cutover, expected) || cutover.State != "BLOCKED" ||
		cutover.LegacyRoleVersion != input.ExpectedLegacyRoleVersion ||
		cutover.LegacyPromptVersion != input.ExpectedLegacyPromptVersion {
		return entity.Resource{}, errs.ErrStateConflict
	}
	legacyRole, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		cutover.LegacyRoleID)
	if err != nil {
		return entity.Resource{}, err
	}
	legacyPrompt, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		cutover.LegacyPromptProfileID)
	if err != nil {
		return entity.Resource{}, err
	}
	roleSpec, roleOK := legacyRole.Spec.(entity.RoleSpec)
	promptSpec, promptOK := legacyPrompt.Spec.(entity.PromptProfileSpec)
	if !roleOK || !promptOK || legacyRole.Kind != enum.KindRole || legacyPrompt.Kind != enum.KindPromptProfile ||
		legacyRole.OwnerActorID != input.Principal.ActorID || legacyPrompt.OwnerActorID != input.Principal.ActorID ||
		legacyRole.Version != cutover.LegacyRoleVersion || legacyPrompt.Version != cutover.LegacyPromptVersion ||
		roleSpec.PromptProfileID != legacyPrompt.ID || promptSpec.ContentSHA256 != hashString(input.InstructionContent) ||
		promptSpec.ContentArtifactID == "" || promptSpec.ContentArtifactVersion == 0 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if validation := validateInstructionContent(input.InstructionContent); len(validation) != 0 {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	artifact, artifactSpec, err := service.requireCleanArtifactResource(ctx, tx, input.Principal,
		promptSpec.ContentArtifactID)
	if err != nil || artifact.Version != promptSpec.ContentArtifactVersion || artifactSpec.Direction != "INPUT" ||
		artifactSpec.MediaType != "text/markdown" || artifactSpec.SHA256 != promptSpec.ContentSHA256 {
		if err != nil {
			return entity.Resource{}, err
		}
		return entity.Resource{}, errs.ErrStateConflict
	}
	project, projectSHA, err := lockActiveWorkspace(ctx, tx, input.Principal)
	if err != nil {
		return entity.Resource{}, err
	}
	recipe, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		roleSpec.RoleImageRecipeID)
	if err != nil {
		return entity.Resource{}, err
	}
	recipeSHA, err := entity.ProjectionSHA256(recipe)
	if err != nil || recipe.Kind != enum.KindRoleImageRecipe || recipe.State != enum.StateActive ||
		recipe.OwnerActorID != input.Principal.ActorID {
		return entity.Resource{}, errs.ErrStateConflict
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	ownership := func(source entity.ConfigurationOwnership, sourceID string, version uint64, digest string) entity.ConfigurationOwnership {
		if source.ManagedBy == "GIT" {
			return source
		}
		return entity.ConfigurationOwnership{ManagedBy: "UI", SourceRef: "control-plane://legacy/" + sourceID,
			SourceRevision: version, SourceSHA256: digest}
	}
	roleDigest, err := entity.ProjectionSHA256(legacyRole)
	if err != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	promptDigest, err := entity.ProjectionSHA256(legacyPrompt)
	if err != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	validationSHA, err := canonicalHash(struct {
		ContentVersion uint64
		ContentSHA256  string
		Succeeded      bool
		Errors         []entity.InstructionValidationError
	}{promptSpec.Revision, promptSpec.ContentSHA256, true, nil})
	if err != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	instructionSpec := entity.InstructionSetSpec{StableKey: legacyPromptStableKey(legacyPrompt.ID),
		Locale: promptSpec.Locale, CurrentVersion: promptSpec.Revision, PublishedVersion: promptSpec.Revision,
		Content: input.InstructionContent, ContentSHA256: promptSpec.ContentSHA256, VersionState: "PUBLISHED",
		ValidationSHA256: validationSHA, ValidationSucceeded: true, ValidatedContentVersion: promptSpec.Revision,
		ValidatedContentSHA256: promptSpec.ContentSHA256, ContentArtifactID: artifact.ID,
		ContentArtifactVersion: artifact.Version, Ownership: ownership(promptSpec.Ownership, legacyPrompt.ID,
			legacyPrompt.Version, promptDigest)}
	instruction, err := newLegacyTarget(cutover.TargetInstructionSetID, input.Principal, legacyPrompt.ID,
		legacyPrompt.Name, enum.KindInstructionSet, instructionSpec, legacyPrompt.UpdatedAt)
	if err != nil {
		return entity.Resource{}, err
	}
	if err := insertLegacyTarget(ctx, tx, protected, service, input.Principal, instruction); err != nil {
		return entity.Resource{}, err
	}
	instructionSHA, _ := entity.ProjectionSHA256(instruction)

	poolBindings := make([]entity.ProviderPoolBinding, 0, len(cutover.SourceCredentialIDs))
	legacyWeights := make(map[string]uint32, len(roleSpec.ProviderAccountPool.Bindings))
	for _, binding := range roleSpec.ProviderAccountPool.Bindings {
		legacyWeights[binding.CredentialBindingID] = binding.Weight
	}
	for credentialIndex, credentialID := range cutover.SourceCredentialIDs {
		credential, getErr := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, credentialID)
		if getErr != nil {
			return entity.Resource{}, getErr
		}
		credentialSpec, cast := credential.Spec.(entity.CredentialBindingSpec)
		credentialSHA, digestErr := entity.ProjectionSHA256(credential)
		if !cast || digestErr != nil || credential.Kind != enum.KindCredentialBinding || credential.State != enum.StateActive ||
			credential.OwnerActorID != input.Principal.ActorID || credentialSpec.Purpose != "provider-account" ||
			!credentialSpec.ProviderEligible {
			return entity.Resource{}, errs.ErrStateConflict
		}
		providerID := deterministicLegacyID("mattercodex:legacy-provider-reference:" + credential.ID)
		if credentialIndex >= len(cutover.TargetProviderReferenceIDs) ||
			cutover.TargetProviderReferenceIDs[credentialIndex] != providerID {
			return entity.Resource{}, errs.ErrStateConflict
		}
		maskedLabel := providerPublicAccountName(credentialSpec.PrincipalRef)
		if maskedLabel == "" {
			maskedLabel = "legacy-account"
		}
		weight := legacyWeights[credential.ID]
		if weight == 0 {
			return entity.Resource{}, errs.ErrStateConflict
		}
		providerSpec := entity.ProviderConnectionReferenceSpec{StableKey: legacyProviderStableKey(credential.ID),
			Provider: "legacy", ServerReference: "control-plane://legacy-credential/" + credential.ID,
			ReferenceVersion: credential.Version, ReferenceGeneration: credentialSpec.ProviderObservationRevision,
			ReferenceSHA256: credentialSHA, MaskedLabel: maskedLabel, MaskedStatus: "AVAILABLE",
			Capabilities: slices.Clone(credentialSpec.ProviderCapabilities), Eligible: true,
			ObservedAt:     credentialSpec.ProviderObservedAt,
			ReceiptID:      deterministicLegacyID("mattercodex:legacy-provider-receipt:" + credential.ID),
			ReceiptVersion: credential.Version, ReceiptSHA256: credentialSHA,
			CredentialBindingID: credential.ID, CredentialBindingVersion: credential.Version,
			CredentialBindingSHA256: credentialSHA}
		reference, createErr := newLegacyTarget(providerID, input.Principal, credential.ID,
			"Migrated provider reference", enum.KindProviderReference, providerSpec, credential.UpdatedAt)
		if createErr != nil {
			return entity.Resource{}, createErr
		}
		if insertErr := insertLegacyTarget(ctx, tx, protected, service, input.Principal, reference); insertErr != nil {
			return entity.Resource{}, insertErr
		}
		referenceSHA, _ := entity.ProjectionSHA256(reference)
		poolBindings = append(poolBindings, entity.ProviderPoolBinding{ProviderConnectionReferenceID: reference.ID,
			ProviderConnectionStableKey: providerSpec.StableKey, ReferenceVersion: reference.Version,
			ReferenceSHA256: referenceSHA, Weight: weight, Eligible: true, MaskedStatus: "AVAILABLE"})
	}
	if len(poolBindings) == 0 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	slices.SortFunc(poolBindings, func(left, right entity.ProviderPoolBinding) int {
		return strings.Compare(left.ProviderConnectionReferenceID, right.ProviderConnectionReferenceID)
	})
	poolSpec := entity.ProviderPoolSpec{StableKey: roleSpec.StableKey, Policy: roleSpec.ProviderAccountPool.Policy,
		PolicyRevision: roleSpec.ProviderAccountPool.PolicyRevision, ObservationMaxAge: roleSpec.ProviderAccountPool.ObservationMaxAge,
		Bindings: poolBindings, EligibilitySnapshotSHA256: strings.Repeat("0", 64),
		Ownership: ownership(roleSpec.Ownership, legacyRole.ID, legacyRole.Version, roleDigest)}
	snapshot := poolSpec
	poolSHA, err := canonicalHash(snapshot)
	if err != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	poolSpec.EligibilitySnapshotSHA256 = poolSHA
	pool, err := newLegacyTarget(cutover.TargetProviderPoolID, input.Principal, legacyRole.ID,
		legacyRole.Name, enum.KindProviderPool, poolSpec, now)
	if err != nil {
		return entity.Resource{}, err
	}
	if err := insertLegacyTarget(ctx, tx, protected, service, input.Principal, pool); err != nil {
		return entity.Resource{}, err
	}
	poolSHA, _ = entity.ProjectionSHA256(pool)

	allowedTargets := make([]string, 0, len(roleSpec.AllowedTargetRoleIDs))
	for _, legacyTarget := range roleSpec.AllowedTargetRoleIDs {
		allowedTargets = append(allowedTargets, deterministicLegacyID("mattercodex:legacy-role-definition:"+legacyTarget))
	}
	slices.Sort(allowedTargets)
	roleDefinitionSpec := entity.RoleDefinitionSpec{StableKey: roleSpec.StableKey,
		Description:  "Migrated immutable legacy role " + legacyRole.ID,
		Capabilities: slices.Clone(roleSpec.Capabilities), AllowedTargetRoleDefinitionIDs: allowedTargets,
		RoleImageRecipeID: recipe.ID, RoleImageRecipeVersion: recipe.Version, RoleImageRecipeSHA256: recipeSHA,
		Ownership: ownership(roleSpec.Ownership, legacyRole.ID, legacyRole.Version, roleDigest)}
	roleDefinition, err := newLegacyTarget(cutover.TargetRoleDefinitionID, input.Principal, legacyRole.ID,
		legacyRole.Name, enum.KindRoleDefinition, roleDefinitionSpec, now)
	if err != nil {
		return entity.Resource{}, err
	}
	if err := insertLegacyTarget(ctx, tx, protected, service, input.Principal, roleDefinition); err != nil {
		return entity.Resource{}, err
	}
	roleDefinitionSHA, _ := entity.ProjectionSHA256(roleDefinition)
	agentSpec := entity.AgentSpec{StableKey: roleSpec.StableKey,
		RoleDefinitionID: roleDefinition.ID, RoleDefinitionVersion: roleDefinition.Version,
		RoleDefinitionSHA256: roleDefinitionSHA, InstructionSetID: instruction.ID,
		InstructionSetVersion: instruction.Version, InstructionSetSHA256: instructionSHA,
		ProviderPoolID: pool.ID, ProviderPoolVersion: pool.Version, ProviderPoolSHA256: poolSHA,
		RuntimeProfileRef:     "control-plane://runtime-profile/" + recipe.ID,
		RuntimeProfileVersion: recipe.Version, RuntimeProfileSHA256: recipeSHA,
		Capabilities: slices.Clone(roleSpec.Capabilities), Enabled: true,
		Ownership: ownership(roleSpec.Ownership, legacyRole.ID, legacyRole.Version, roleDigest)}
	agent, err := newLegacyTarget(cutover.TargetAgentID, input.Principal, legacyRole.ID,
		legacyRole.Name, enum.KindAgent, agentSpec, now)
	if err != nil {
		return entity.Resource{}, err
	}
	if err := insertLegacyTarget(ctx, tx, protected, service, input.Principal, agent); err != nil {
		return entity.Resource{}, err
	}
	agentSHA, _ := entity.ProjectionSHA256(agent)
	assignmentSpec := entity.AgentAssignmentSpec{AgentID: agent.ID, AgentVersion: agent.Version,
		AgentSHA256: agentSHA, WorkspaceID: project.ID, WorkspaceVersion: project.Version,
		WorkspaceSHA256: projectSHA, RootActorID: input.Principal.ActorID, AssignmentGeneration: 1}
	assignment, err := newLegacyTarget(cutover.TargetAgentAssignmentID, input.Principal, agent.ID,
		legacyRole.Name, enum.KindAgentAssignment, assignmentSpec, now)
	if err != nil {
		return entity.Resource{}, err
	}
	if err := insertLegacyTarget(ctx, tx, protected, service, input.Principal, assignment); err != nil {
		return entity.Resource{}, err
	}
	cutover.State, cutover.BlockCode, cutover.ManualAction = "MIGRATED", "", ""
	cutover.ResultAgentVersion, cutover.ResultAgentSHA256, cutover.ResolvedAt = agent.Version, agentSHA, now
	if err := cutoverTx.MarkLegacyConfigurationCutoverMigrated(ctx, cutover); err != nil {
		return entity.Resource{}, err
	}
	return agent, nil
}

func insertLegacyTarget(ctx context.Context, tx domainrepo.Transaction, protected domainrepo.ProtectedTransaction,
	service *Service, principal value.Principal, target entity.Resource,
) error {
	existing, err := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, target.ID)
	if err == nil {
		existingSHA, digestErr := entity.ProjectionSHA256(existing)
		targetSHA, targetDigestErr := entity.ProjectionSHA256(target)
		if digestErr != nil || targetDigestErr != nil || existingSHA != targetSHA {
			return errs.ErrStateConflict
		}
		return nil
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return err
	}
	if err := tx.Insert(ctx, target); err != nil {
		return err
	}
	return service.appendProtectedRecords(ctx, tx, protected, principal, "migrate_legacy", target)
}

func newLegacyTarget(id string, principal value.Principal, parentID, name string, kind enum.Kind,
	spec entity.Spec, now time.Time,
) (entity.Resource, error) {
	resource, err := entity.New(id, principal.OrganizationID, principal.ProjectID, parentID,
		principal.ActorID, kind, name, spec, now)
	if err != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	return resource, nil
}

func deterministicLegacyID(name string) string {
	sum := md5.Sum([]byte(name))
	identifier, err := uuid.FromBytes(sum[:])
	if err != nil {
		return ""
	}
	return identifier.String()
}

func legacyPromptStableKey(id string) string {
	return "legacy-prompt-" + strings.ReplaceAll(id, "-", "")[:24]
}

func legacyProviderStableKey(id string) string {
	return "legacy-provider-" + strings.ReplaceAll(id, "-", "")[:24]
}
