package resource

import (
	"context"
	"errors"
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
		candidate, ok := byID[identifier]
		if !ok || candidate.Kind != kind {
			return entity.Resource{}, errs.ErrNotFound
		}
		item, lockErr := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, identifier)
		if lockErr != nil {
			return entity.Resource{}, lockErr
		}
		if item.Kind != kind || item.State != enum.StateActive {
			return entity.Resource{}, errs.ErrStateConflict
		}
		byID[identifier] = item
		return item, nil
	}
	project, err := add(principal.ProjectID, enum.KindProject)
	if err != nil {
		return entity.Resource{}, err
	}
	selected[project.ID] = project
	projectSHA, err := entity.ProjectionSHA256(project)
	if err != nil || project.ID != principal.ProjectID || project.ProjectID != principal.ProjectID ||
		project.OrganizationID != principal.OrganizationID || project.OwnerActorID != principal.ActorID {
		return entity.Resource{}, errs.ErrStateConflict
	}
	selected[session.ID] = session
	agent, err = add(agent.ID, enum.KindAgent)
	if err != nil {
		return entity.Resource{}, err
	}
	agentSpec, ok := agent.Spec.(entity.AgentSpec)
	if !ok || !agentSpec.Enabled || agent.State != enum.StateActive || agent.OwnerActorID != principal.ActorID || sessionSpec.AgentID != agent.ID ||
		sessionSpec.AgentVersion != agent.Version || sessionSpec.ProviderPoolID != agentSpec.ProviderPoolID ||
		sessionSpec.ProviderPoolVersion != agentSpec.ProviderPoolVersion {
		return entity.Resource{}, errs.ErrStateConflict
	}
	agentSHA, err := entity.ProjectionSHA256(agent)
	if err != nil || agentSHA != sessionSpec.AgentSHA256 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	assignment, err := add(sessionSpec.AgentAssignmentID, enum.KindAgentAssignment)
	if err != nil {
		return entity.Resource{}, err
	}
	assignmentSpec, ok := assignment.Spec.(entity.AgentAssignmentSpec)
	assignmentSHA, digestErr := entity.ProjectionSHA256(assignment)
	if !ok || digestErr != nil || assignment.Version != sessionSpec.AgentAssignmentVersion ||
		assignmentSHA != sessionSpec.AgentAssignmentSHA256 || assignmentSpec.AgentID != agent.ID ||
		assignment.OwnerActorID != principal.ActorID || assignmentSpec.RootActorID != principal.ActorID ||
		assignmentSpec.AgentVersion != agent.Version || assignmentSpec.AgentSHA256 != agentSHA ||
		assignmentSpec.WorkspaceID != principal.ProjectID || assignmentSpec.WorkspaceVersion != project.Version ||
		assignmentSpec.WorkspaceSHA256 != projectSHA || assignmentSpec.RoomID != sessionSpec.ConversationID {
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
	type providerCandidate struct {
		resource entity.Resource
		weight   uint32
	}
	selectionTime := service.now().UTC().Truncate(time.Microsecond)
	providerCandidates := make([]providerCandidate, 0, len(poolSpec.Bindings))
	for _, binding := range poolSpec.Bindings {
		reference, err := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID,
			binding.ProviderConnectionReferenceID)
		if err != nil {
			return entity.Resource{}, err
		}
		referenceSHA, digestErr := entity.ProjectionSHA256(reference)
		referenceSpec, referenceOK := reference.Spec.(entity.ProviderConnectionReferenceSpec)
		if digestErr != nil || !referenceOK || reference.Kind != enum.KindProviderReference ||
			reference.Version != binding.ReferenceVersion || referenceSHA != binding.ReferenceSHA256 {
			continue
		}
		if reference.State != enum.StateActive || !referenceSpec.Eligible ||
			referenceSpec.ObservedAt.After(selectionTime.Add(5*time.Second)) ||
			selectionTime.Sub(referenceSpec.ObservedAt) > poolSpec.ObservationMaxAge {
			continue
		}
		credential, err := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID,
			referenceSpec.CredentialBindingID)
		if err != nil {
			return entity.Resource{}, err
		}
		_, credentialOK := credential.Spec.(entity.CredentialBindingSpec)
		credentialSHA, digestErr := entity.ProjectionSHA256(credential)
		if !credentialOK || digestErr != nil || credential.Kind != enum.KindCredentialBinding ||
			credential.Version != referenceSpec.CredentialBindingVersion ||
			credentialSHA != referenceSpec.CredentialBindingSHA256 {
			continue
		}
		providerCandidates = append(providerCandidates, providerCandidate{resource: credential, weight: binding.Weight})
	}
	if len(providerCandidates) == 0 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	now := selectionTime
	derivedPromptID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("mattercodex:agent-prompt:"+instruction.ID)).String()
	derivedRoleID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("mattercodex:agent-role:"+agent.ID)).String()
	roleBindings := make([]entity.ProviderAccountPoolBinding, 0, len(providerCandidates))
	roleCredentialIDs := make([]string, 0, len(providerCandidates))
	for _, candidate := range providerCandidates {
		roleBindings = append(roleBindings, entity.ProviderAccountPoolBinding{CredentialBindingID: candidate.resource.ID, Weight: candidate.weight})
		roleCredentialIDs = append(roleCredentialIDs, candidate.resource.ID)
	}
	slices.Sort(roleCredentialIDs)
	slices.SortFunc(roleBindings, func(left, right entity.ProviderAccountPoolBinding) int {
		return compareText(left.CredentialBindingID, right.CredentialBindingID)
	})
	selectionSpec := entity.RoleSpec{ProviderCredentialBindingIDs: roleCredentialIDs,
		ProviderAccountPool: entity.ProviderAccountPool{Policy: poolSpec.Policy,
			PolicyRevision: poolSpec.PolicyRevision, ObservationMaxAge: poolSpec.ObservationMaxAge,
			Bindings: roleBindings}}
	providerBinding, err := service.selectProviderBinding(ctx, tx, principal, derivedRoleID, selectionSpec, "", now)
	if err != nil {
		return entity.Resource{}, err
	}
	providerSpec, ok := providerBinding.Spec.(entity.CredentialBindingSpec)
	if !ok {
		return entity.Resource{}, errs.ErrStateConflict
	}
	providerAccountName := providerPublicAccountName(providerSpec.PrincipalRef)
	if providerAccountName == "" {
		return entity.Resource{}, errs.ErrStateConflict
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
		agentSpec.RuntimeProfileRef != "control-plane://runtime-profile/"+recipe.ID ||
		agentSpec.RuntimeProfileVersion != recipe.Version || agentSpec.RuntimeProfileSHA256 != recipeSHA ||
		recipe.OwnerActorID != principal.ActorID ||
		recipeSpec.PolicySHA256 != service.imagePolicySHA256 ||
		recipeSpec.RoleRuntimeContractRevision != service.roleRuntimeContractRevision ||
		recipeSpec.RoleRuntimeContractSHA256 != service.roleRuntimeContractSHA256 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	selected[recipe.ID] = recipe
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
		chat, err := add(sessionSpec.ConversationID, enum.KindChat)
		if err != nil {
			return entity.Resource{}, err
		}
		selected[chat.ID] = chat
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
	credentialIDs := make([]string, 0, len(requiredCredentials)+2)
	credentialIDs = append(credentialIDs, providerBinding.ID)
	selected[providerBinding.ID] = providerBinding
	requiredPurposes := make([]string, 0, len(requiredCredentials))
	for purpose := range requiredCredentials {
		requiredPurposes = append(requiredPurposes, purpose)
	}
	slices.Sort(requiredPurposes)
	for _, purpose := range requiredPurposes {
		identifier := requiredCredentials[purpose]
		if identifier == "" {
			return entity.Resource{}, errs.ErrStateConflict
		}
		credential, credentialErr := add(identifier, enum.KindCredentialBinding)
		if credentialErr != nil {
			return entity.Resource{}, credentialErr
		}
		credentialSpec, credentialOK := credential.Spec.(entity.CredentialBindingSpec)
		if !credentialOK || credentialSpec.Purpose != purpose || credentialSpec.Ownership.ManagedBy != "GIT" {
			return entity.Resource{}, errs.ErrStateConflict
		}
		credentialIDs = append(credentialIDs, identifier)
		selected[credential.ID] = credential
	}
	if sessionSpec.ConversationID != "" {
		mcpBindingID := sessionMCPBindingID(session.ID)
		binding, bindingErr := add(mcpBindingID, enum.KindCredentialBinding)
		if bindingErr != nil {
			return entity.Resource{}, bindingErr
		}
		credentialIDs = append(credentialIDs, binding.ID)
		selected[binding.ID] = binding
	}
	projectionTx, ok := tx.(domainrepo.RuntimeProjectionTransaction)
	if !ok {
		return entity.Resource{}, errs.ErrInternal
	}
	derivedPrompt := entity.Resource{
		ID: derivedPromptID, OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
		ParentID: instruction.ID, OwnerActorID: principal.ActorID, Kind: enum.KindPromptProfile,
		Name: "Derived Agent prompt", State: enum.StateActive, Version: instruction.Version,
		Spec: entity.PromptProfileSpec{Revision: instructionSpec.CurrentVersion, ContentSHA256: instructionSpec.ContentSHA256,
			ContentArtifactID: instructionSpec.ContentArtifactID, ContentArtifactVersion: instructionSpec.ContentArtifactVersion,
			SourceRef: "control-plane://instruction-set/" + instruction.ID, Locale: instructionSpec.Locale,
			Ownership: entity.ConfigurationOwnership{ManagedBy: "UI", SourceRef: "control-plane://instruction-set/" + instruction.ID,
				SourceRevision: instruction.Version, SourceSHA256: instructionSHA}},
		CreatedAt: instruction.UpdatedAt, UpdatedAt: instruction.UpdatedAt,
	}
	derivedRole := entity.Resource{
		ID: derivedRoleID, OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
		ParentID: agent.ID, OwnerActorID: principal.ActorID, Kind: enum.KindRole,
		Name: "Derived Agent runtime role", State: enum.StateActive, Version: agent.Version,
		Spec: entity.RoleSpec{StableKey: agentSpec.StableKey, Capabilities: slices.Clone(agentSpec.Capabilities),
			PromptProfileID: derivedPromptID, ProviderCredentialBindingIDs: roleCredentialIDs,
			ProviderAccountPool: entity.ProviderAccountPool{Policy: poolSpec.Policy, PolicyRevision: poolSpec.PolicyRevision,
				ObservationMaxAge: poolSpec.ObservationMaxAge, Bindings: roleBindings},
			RoleImageRecipeID: recipe.ID, Ownership: entity.ConfigurationOwnership{ManagedBy: "UI",
				SourceRef: "control-plane://agent/" + agent.ID, SourceRevision: agent.Version, SourceSHA256: agentSHA}},
		CreatedAt: agent.UpdatedAt, UpdatedAt: agent.UpdatedAt,
	}
	if derivedPrompt.Validate() != nil || derivedRole.Validate() != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	if err := projectionTx.InsertDerivedRuntimeResource(ctx, derivedPrompt, enum.KindInstructionSet,
		instruction.ID, instruction.Version, instructionSHA); err != nil {
		return entity.Resource{}, err
	}
	if err := projectionTx.InsertDerivedRuntimeResource(ctx, derivedRole, enum.KindAgent,
		agent.ID, agent.Version, agentSHA); err != nil {
		return entity.Resource{}, err
	}
	selected[derivedPrompt.ID], selected[derivedRole.ID] = derivedPrompt, derivedRole
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
			PromptProfileID:              derivedPrompt.ID, PromptRevision: instructionSpec.CurrentVersion,
			RoleID: derivedRole.ID, ProviderCredentialBindingID: providerBinding.ID,
			ProviderAccountName:  providerAccountName,
			CredentialBindingIDs: credentialIDs, PredecessorRevisionID: predecessorID,
			AuthorityPolicyVersion: service.authorityPolicyRevision, AuthorityPolicySHA256: service.authorityPolicySHA256,
			Components: components, CreatedAt: now, SessionID: session.ID, ChatID: sessionSpec.ConversationID,
			EffectiveRuntimeSHA256: effectiveRuntimeSHA256, CodexModel: "gpt-5.4",
			CodexSandbox: "workspace-write", CodexApprovalPolicy: "never", ScheduledResultContract: scheduledResultContract,
			AgentID: agent.ID, AgentVersion: agent.Version, AgentSHA256: agentSHA,
			RoleDefinitionID: roleDefinition.ID, RoleDefinitionVersion: roleDefinition.Version,
			RoleDefinitionSHA256: roleDefinitionSHA, InstructionSetID: instruction.ID,
			InstructionSetVersion: instruction.Version, InstructionSetSHA256: instructionSHA,
			ProviderPoolID: pool.ID, ProviderPoolVersion: pool.Version, ProviderPoolSHA256: poolSHA,
			AgentAssignmentID: assignment.ID, AgentAssignmentVersion: assignment.Version,
			AgentAssignmentSHA256: assignmentSHA,
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

func providerPublicAccountName(principalRef string) string {
	name := strings.ToLower(strings.TrimSpace(principalRef))
	if separator := strings.LastIndexByte(name, ':'); separator >= 0 {
		name = name[separator+1:]
	}
	if !publicProviderAccountNamePattern.MatchString(name) {
		return ""
	}
	return name
}
