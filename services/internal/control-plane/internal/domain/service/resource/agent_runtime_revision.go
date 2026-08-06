package resource

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

func (service *Service) createAgentRuntimeRevision(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	session entity.Resource,
	sessionSpec entity.SessionSpec,
	scheduledResultContract *entity.ScheduledResultContractRef,
	resources []entity.Resource,
	agent entity.Resource,
) (entity.Resource, error) {
	byID := make(map[string]entity.Resource, len(resources))
	for _, item := range resources {
		byID[item.ID] = item
	}
	selected := make(map[string]entity.Resource)
	add := func(identifier string, kind enum.Kind) (entity.Resource, error) {
		item, ok := byID[identifier]
		if !ok || item.Kind != kind || item.State != enum.StateActive {
			return entity.Resource{}, errs.ErrNotFound
		}
		selected[item.ID] = item
		return item, nil
	}
	project, err := add(principal.ProjectID, enum.KindProject)
	if err != nil {
		return entity.Resource{}, err
	}
	_ = project
	selected[session.ID], selected[agent.ID] = session, agent
	agentSpec, ok := agent.Spec.(entity.AgentSpec)
	if !ok || agent.OwnerActorID != principal.ActorID || sessionSpec.AgentID != agent.ID ||
		sessionSpec.AgentVersion != agent.Version || sessionSpec.ProviderPoolID != agentSpec.ProviderPoolID ||
		sessionSpec.ProviderPoolVersion != agentSpec.ProviderPoolVersion {
		return entity.Resource{}, errs.ErrStateConflict
	}
	agentSHA, err := entity.ProjectionSHA256(agent)
	if err != nil || agentSHA != sessionSpec.AgentSHA256 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	roleDefinition, err := add(agentSpec.RoleDefinitionID, enum.KindRoleDefinition)
	if err != nil {
		return entity.Resource{}, err
	}
	instruction, err := add(agentSpec.InstructionSetID, enum.KindInstructionSet)
	if err != nil {
		return entity.Resource{}, err
	}
	pool, err := add(agentSpec.ProviderPoolID, enum.KindProviderPool)
	if err != nil {
		return entity.Resource{}, err
	}
	roleDefinitionSHA, err := entity.ProjectionSHA256(roleDefinition)
	if err != nil || roleDefinition.Version != agentSpec.RoleDefinitionVersion ||
		roleDefinitionSHA != agentSpec.RoleDefinitionSHA256 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	instructionSHA, err := entity.ProjectionSHA256(instruction)
	instructionSpec, instructionOK := instruction.Spec.(entity.InstructionSetSpec)
	if err != nil || !instructionOK || instructionSpec.VersionState != "PUBLISHED" ||
		instruction.Version != agentSpec.InstructionSetVersion || instructionSHA != agentSpec.InstructionSetSHA256 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	poolSHA, err := entity.ProjectionSHA256(pool)
	poolSpec, poolOK := pool.Spec.(entity.ProviderPoolSpec)
	if err != nil || !poolOK || pool.Version != agentSpec.ProviderPoolVersion || poolSHA != agentSpec.ProviderPoolSHA256 ||
		poolSHA != sessionSpec.ProviderPoolSHA256 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	for _, binding := range poolSpec.Bindings {
		reference, err := add(binding.ProviderConnectionReferenceID, enum.KindProviderReference)
		if err != nil {
			return entity.Resource{}, err
		}
		referenceSHA, digestErr := entity.ProjectionSHA256(reference)
		if digestErr != nil || reference.Version != binding.ReferenceVersion || referenceSHA != binding.ReferenceSHA256 {
			return entity.Resource{}, errs.ErrStateConflict
		}
	}
	roleSpec, ok := roleDefinition.Spec.(entity.RoleDefinitionSpec)
	if !ok || roleSpec.RoleImageRecipeID == "" {
		return entity.Resource{}, errs.ErrStateConflict
	}
	recipe, err := add(roleSpec.RoleImageRecipeID, enum.KindRoleImageRecipe)
	if err != nil {
		return entity.Resource{}, err
	}
	recipeSHA, err := entity.ProjectionSHA256(recipe)
	recipeSpec, recipeOK := recipe.Spec.(entity.RoleImageRecipeSpec)
	if err != nil || !recipeOK || recipe.Version != roleSpec.RoleImageRecipeVersion ||
		recipeSHA != roleSpec.RoleImageRecipeSHA256 || recipeSpec.PolicyRevision != service.imagePolicyRevision ||
		recipeSpec.PolicySHA256 != service.imagePolicySHA256 ||
		recipeSpec.RoleRuntimeContractRevision != service.roleRuntimeContractRevision ||
		recipeSpec.RoleRuntimeContractSHA256 != service.roleRuntimeContractSHA256 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	imageTx, ok := tx.(domainrepo.ImageTransaction)
	if !ok {
		return entity.Resource{}, errs.ErrInternal
	}
	imageArtifact, err := imageTx.PromotedImageArtifactBySpec(ctx, principal.OrganizationID,
		principal.ProjectID, recipe.OwnerActorID, recipeSpec.SpecSHA256, recipeSpec.PolicyRevision, recipeSpec.PolicySHA256)
	if err != nil {
		return entity.Resource{}, err
	}
	artifactSpec, ok := imageArtifact.Spec.(entity.ImageArtifactSpec)
	if !ok || imageArtifact.State != enum.StateActive || artifactSpec.SpecSHA256 != recipeSpec.SpecSHA256 ||
		artifactSpec.PolicyRevision != recipeSpec.PolicyRevision || artifactSpec.PolicySHA256 != recipeSpec.PolicySHA256 ||
		artifactSpec.AdmissionVerdict != entity.ImageAdmissionAccepted || artifactSpec.AdmissionRevision == 0 ||
		!validSHA256Text(artifactSpec.AdmissionReceiptSHA256) || !validDigest(artifactSpec.AdmissionReceiptOCIManifestDigest) ||
		!validSHA256Text(artifactSpec.SignatureSHA256) || !validSHA256Text(artifactSpec.PromotionReadbackSHA256) {
		return entity.Resource{}, errs.ErrStateConflict
	}
	imageBuild, err := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, artifactSpec.BuildID)
	if err != nil {
		return entity.Resource{}, err
	}
	buildSpec, ok := imageBuild.Spec.(entity.ImageBuildSpec)
	if !ok || imageBuild.State != enum.StateSucceeded || imageBuild.Version != artifactSpec.BuildVersion ||
		buildSpec.Attempt != artifactSpec.BuildAttempt || buildSpec.ManifestDigest != artifactSpec.ManifestDigest ||
		buildSpec.ProvenanceSHA256 != artifactSpec.ProvenanceSHA256 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	selected[imageArtifact.ID], selected[imageBuild.ID] = imageArtifact, imageBuild
	if sessionSpec.ConversationID != "" {
		if _, err := add(sessionSpec.ConversationID, enum.KindChat); err != nil {
			return entity.Resource{}, err
		}
	}
	requiredCredentials := map[string]string{
		"control-plane-application-grant": "", "runtime-materialization-application-grant": "",
		"handoff-private-key": "", "control-plane-client-tls": "",
		"interaction-gateway-client-tls": "", "mcp-client-tls": "",
	}
	for _, item := range resources {
		if item.Kind != enum.KindCredentialBinding || item.State != enum.StateActive {
			continue
		}
		spec, ok := item.Spec.(entity.CredentialBindingSpec)
		if !ok {
			return entity.Resource{}, errs.ErrInternal
		}
		if _, required := requiredCredentials[spec.Purpose]; !required {
			continue
		}
		if spec.Ownership.ManagedBy != "GIT" || requiredCredentials[spec.Purpose] != "" {
			return entity.Resource{}, errs.ErrStateConflict
		}
		requiredCredentials[spec.Purpose] = item.ID
	}
	credentialIDs := make([]string, 0, len(requiredCredentials)+1)
	for _, identifier := range requiredCredentials {
		if identifier == "" {
			return entity.Resource{}, errs.ErrStateConflict
		}
		credentialIDs = append(credentialIDs, identifier)
		selected[identifier] = byID[identifier]
	}
	if sessionSpec.ConversationID != "" {
		mcpBindingID := sessionMCPBindingID(session.ID)
		binding, exists := byID[mcpBindingID]
		if !exists || binding.Kind != enum.KindCredentialBinding || binding.State != enum.StateActive {
			return entity.Resource{}, errs.ErrStateConflict
		}
		credentialIDs = append(credentialIDs, binding.ID)
		selected[binding.ID] = binding
	}
	slices.Sort(credentialIDs)
	components := make([]entity.EffectiveResourceRef, 0, len(selected))
	for _, item := range selected {
		digest, err := entity.ProjectionSHA256(item)
		if err != nil {
			return entity.Resource{}, errs.ErrInternal
		}
		components = append(components, entity.EffectiveResourceRef{Kind: item.Kind,
			ResourceID: item.ID, Version: item.Version, ProjectionSHA256: digest})
	}
	slices.SortFunc(components, func(left, right entity.EffectiveResourceRef) int {
		if left.Kind != right.Kind {
			return compareText(string(left.Kind), string(right.Kind))
		}
		return compareText(left.ResourceID, right.ResourceID)
	})
	now := service.now().UTC().Truncate(time.Microsecond)
	effectiveRuntimeSHA256, err := canonicalHash(struct {
		ImageReference, ImageManifestDigest     string
		AgentID, AgentSHA256                    string
		AgentVersion                            uint64
		RoleDefinitionID, RoleDefinitionSHA256  string
		RoleDefinitionVersion                   uint64
		InstructionSetID, InstructionSetSHA256  string
		InstructionSetVersion                   uint64
		ProviderPoolID, ProviderPoolSHA256      string
		ProviderPoolVersion                     uint64
		RuntimeProfileRef, RuntimeProfileSHA256 string
		RuntimeProfileVersion                   uint64
		AuthorityPolicySHA256                   string
		AuthorityPolicyVersion                  uint64
		Components                              []entity.EffectiveResourceRef
	}{artifactSpec.PromotedReference, artifactSpec.ManifestDigest, agent.ID, agentSHA, agent.Version,
		roleDefinition.ID, roleDefinitionSHA, roleDefinition.Version, instruction.ID, instructionSHA,
		instruction.Version, pool.ID, poolSHA, pool.Version, agentSpec.RuntimeProfileRef,
		agentSpec.RuntimeProfileSHA256, agentSpec.RuntimeProfileVersion, service.authorityPolicySHA256,
		service.authorityPolicyRevision, components})
	if err != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	predecessorID := ""
	predecessor, err := tx.LatestRuntimeRevision(ctx, principal.OrganizationID, principal.ProjectID)
	if err == nil {
		predecessorID = predecessor.ID
	} else if !errors.Is(err, errs.ErrNotFound) {
		return entity.Resource{}, err
	}
	manifestSHA256, err := canonicalHash(struct {
		EffectiveRuntimeSHA256, PredecessorID string
		CreatedAt                             time.Time
	}{effectiveRuntimeSHA256, predecessorID, now})
	if err != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	revision, err := entity.New(uuid.NewString(), principal.OrganizationID, principal.ProjectID,
		session.ID, session.OwnerActorID, enum.KindRuntimeRevision, "Effective Agent runtime revision",
		entity.RuntimeRevisionSpec{
			ManifestSHA256: manifestSHA256, ImageReference: artifactSpec.PromotedReference,
			RoleImageRecipeID: recipe.ID, RoleImageRecipeVersion: recipe.Version, RoleImageSpecSHA256: recipeSpec.SpecSHA256,
			ImageBuildID: imageBuild.ID, ImageBuildVersion: imageBuild.Version, ImageBuildAttempt: buildSpec.Attempt,
			ImageArtifactID: imageArtifact.ID, ImageArtifactVersion: imageArtifact.Version,
			ImageManifestDigest: artifactSpec.ManifestDigest, ImageAdmissionRevision: artifactSpec.AdmissionRevision,
			ImageAdmissionReceiptSHA256:            artifactSpec.AdmissionReceiptSHA256,
			ImageAdmissionReceiptOCIManifestDigest: artifactSpec.AdmissionReceiptOCIManifestDigest,
			ImagePolicyRevision:                    artifactSpec.PolicyRevision, ImagePolicySHA256: artifactSpec.PolicySHA256,
			ImageSignatureSHA256:         artifactSpec.SignatureSHA256,
			ImagePromotionReadbackSHA256: artifactSpec.PromotionReadbackSHA256,
			RoleRuntimeContractRevision:  artifactSpec.RoleRuntimeContractRevision,
			RoleRuntimeContractSHA256:    artifactSpec.RoleRuntimeContractSHA256,
			CredentialBindingIDs:         credentialIDs, PredecessorRevisionID: predecessorID,
			AuthorityPolicyVersion: service.authorityPolicyRevision, AuthorityPolicySHA256: service.authorityPolicySHA256,
			Components: components, CreatedAt: now, SessionID: session.ID, ChatID: sessionSpec.ConversationID,
			EffectiveRuntimeSHA256: effectiveRuntimeSHA256, CodexModel: "gpt-5.4",
			CodexSandbox: "workspace-write", CodexApprovalPolicy: "never", ScheduledResultContract: scheduledResultContract,
			AgentID: agent.ID, AgentVersion: agent.Version, AgentSHA256: agentSHA,
			RoleDefinitionID: roleDefinition.ID, RoleDefinitionVersion: roleDefinition.Version,
			RoleDefinitionSHA256: roleDefinitionSHA, InstructionSetID: instruction.ID,
			InstructionSetVersion: instruction.Version, InstructionSetSHA256: instructionSHA,
			ProviderPoolID: pool.ID, ProviderPoolVersion: pool.Version, ProviderPoolSHA256: poolSHA,
		}, now)
	if err != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	if err := tx.Insert(ctx, revision); err != nil {
		return entity.Resource{}, err
	}
	if err := service.appendMutationRecords(ctx, tx, principal, "create_agent_runtime_revision", revision); err != nil {
		return entity.Resource{}, err
	}
	return revision, nil
}
