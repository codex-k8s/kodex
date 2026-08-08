package resource

import (
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

func compiledLegacyTuple(compiled *compiledLegacyGraph, reference string,
	expected enum.Kind,
) (entity.Resource, string, error) {
	resource, ok := compiled.Resources[reference]
	if !ok || resource.Kind != expected {
		return entity.Resource{}, "", errs.ErrFailedPrecondition
	}
	digest, err := entity.ProjectionSHA256(resource)
	if err != nil {
		return entity.Resource{}, "", errs.ErrInternal
	}
	return resource, digest, nil
}

func (service *Service) compileLegacyRuntimeRevisions(principal value.Principal, projectID string,
	operations map[string]entity.LegacyGraphOperation, compiled *compiledLegacyGraph, at time.Time,
) error {
	for reference, operation := range operations {
		input := operation.RuntimeRevision
		if input == nil {
			continue
		}
		if input.AuthorityPolicyRevision != service.authorityPolicyRevision ||
			input.AuthorityPolicySHA256 != service.authorityPolicySHA256 {
			return errs.ErrFailedPrecondition
		}
		session, _, err := compiledLegacyTuple(compiled, input.SessionRef, enum.KindSession)
		if err != nil {
			return err
		}
		agent, agentSHA, err := compiledLegacyTuple(compiled, input.AgentRef, enum.KindAgent)
		if err != nil {
			return err
		}
		assignment, assignmentSHA, err := compiledLegacyTuple(compiled, input.AssignmentRef, enum.KindAgentAssignment)
		if err != nil {
			return err
		}
		roleDefinition, roleDefinitionSHA, err := compiledLegacyTuple(compiled, input.RoleDefinitionRef, enum.KindRoleDefinition)
		if err != nil {
			return err
		}
		instruction, instructionSHA, err := compiledLegacyTuple(compiled, input.InstructionSetRef, enum.KindInstructionSet)
		if err != nil {
			return err
		}
		pool, poolSHA, err := compiledLegacyTuple(compiled, input.ProviderPoolRef, enum.KindProviderPool)
		if err != nil {
			return err
		}
		providerCredential, _, err := compiledLegacyTuple(compiled, input.ProviderCredentialRef, enum.KindCredentialBinding)
		if err != nil {
			return err
		}
		recipe, _, err := compiledLegacyTuple(compiled, input.RoleImageRecipeRef, enum.KindRoleImageRecipe)
		if err != nil {
			return err
		}
		build, _, err := compiledLegacyTuple(compiled, input.ImageBuildRef, enum.KindImageBuild)
		if err != nil {
			return err
		}
		imageArtifact, _, err := compiledLegacyTuple(compiled, input.ImageArtifactRef, enum.KindImageArtifact)
		if err != nil {
			return err
		}
		promptArtifact, _, err := compiledLegacyTuple(compiled, input.PromptArtifactRef, enum.KindArtifact)
		if err != nil {
			return err
		}
		recipeSpec := recipe.Spec.(entity.RoleImageRecipeSpec)
		buildSpec := build.Spec.(entity.ImageBuildSpec)
		artifactSpec := imageArtifact.Spec.(entity.ImageArtifactSpec)
		instructionSpec := instruction.Spec.(entity.InstructionSetSpec)
		credentialSpec := providerCredential.Spec.(entity.CredentialBindingSpec)
		if promptArtifact.ID != instructionSpec.ContentArtifactID ||
			input.ImageReference != artifactSpec.PromotedReference ||
			input.ProviderAccountName != providerPublicAccountName(credentialSpec.PrincipalRef) ||
			input.CreatedAt.IsZero() || input.CreatedAt.After(at.Add(5*time.Minute)) {
			return errs.ErrFailedPrecondition
		}
		components := make([]entity.EffectiveResourceRef, 0, len(input.Components)+2)
		componentIDs := make(map[string]struct{}, len(input.Components)+2)
		credentialIDs := []string{providerCredential.ID}
		for _, component := range input.Components {
			resource, ok := compiled.Resources[component.LocalRef]
			if !ok {
				return errs.ErrFailedPrecondition
			}
			projectionSHA256, err := entity.ProjectionSHA256(resource)
			if err != nil || component.ProjectionSHA256 != "" {
				return errs.ErrFailedPrecondition
			}
			if _, exists := componentIDs[resource.ID]; exists {
				return errs.ErrInvalidInput
			}
			componentIDs[resource.ID] = struct{}{}
			components = append(components, entity.EffectiveResourceRef{
				Kind: resource.Kind, ResourceID: resource.ID, Version: resource.Version,
				ProjectionSHA256: projectionSHA256,
			})
			if resource.Kind == enum.KindCredentialBinding {
				credentialIDs = append(credentialIDs, resource.ID)
			}
		}
		derived := compiled.Derived[input.AgentRef]
		if len(derived) != 2 {
			return errs.ErrInternal
		}
		var prompt, role entity.Resource
		for _, resource := range derived {
			projectionSHA256, err := entity.ProjectionSHA256(resource)
			if err != nil {
				return errs.ErrInternal
			}
			components = append(components, entity.EffectiveResourceRef{
				Kind: resource.Kind, ResourceID: resource.ID, Version: resource.Version,
				ProjectionSHA256: projectionSHA256,
			})
			if resource.Kind == enum.KindPromptProfile {
				prompt = resource
			} else if resource.Kind == enum.KindRole {
				role = resource
			}
		}
		slices.Sort(credentialIDs)
		credentialIDs = slices.Compact(credentialIDs)
		slices.SortFunc(components, func(left, right entity.EffectiveResourceRef) int {
			if left.Kind != right.Kind {
				return strings.Compare(string(left.Kind), string(right.Kind))
			}
			return strings.Compare(left.ResourceID, right.ResourceID)
		})
		effectiveSHA256, err := canonicalHash(struct {
			ImageReference, ImageManifestDigest    string
			AgentID, AgentSHA256                   string
			AgentVersion                           uint64
			RoleDefinitionID, RoleDefinitionSHA256 string
			RoleDefinitionVersion                  uint64
			InstructionSetID, InstructionSetSHA256 string
			InstructionSetVersion                  uint64
			ProviderPoolID, ProviderPoolSHA256     string
			ProviderPoolVersion                    uint64
			AuthorityPolicySHA256                  string
			AuthorityPolicyVersion                 uint64
			Components                             []entity.EffectiveResourceRef
		}{artifactSpec.PromotedReference, artifactSpec.ManifestDigest,
			agent.ID, agentSHA, agent.Version, roleDefinition.ID, roleDefinitionSHA,
			roleDefinition.Version, instruction.ID, instructionSHA, instruction.Version,
			pool.ID, poolSHA, pool.Version, service.authorityPolicySHA256,
			service.authorityPolicyRevision, components})
		if err != nil || input.EffectiveRuntimeSHA256 != "" {
			return errs.ErrFailedPrecondition
		}
		manifestSHA256, err := canonicalHash(struct {
			EffectiveRuntimeSHA256 string
			CreatedAt              time.Time
		}{effectiveSHA256, input.CreatedAt.UTC().Truncate(time.Microsecond)})
		if err != nil || input.ManifestSHA256 != "" {
			return errs.ErrFailedPrecondition
		}
		spec := entity.RuntimeRevisionSpec{
			ManifestSHA256: manifestSHA256, ImageReference: artifactSpec.PromotedReference,
			RoleImageRecipeID: recipe.ID, RoleImageRecipeVersion: recipe.Version,
			RoleImageSpecSHA256: recipeSpec.SpecSHA256,
			ImageBuildID:        build.ID, ImageBuildVersion: build.Version, ImageBuildAttempt: buildSpec.Attempt,
			ImageArtifactID: imageArtifact.ID, ImageArtifactVersion: imageArtifact.Version,
			ImageManifestDigest:                    artifactSpec.ManifestDigest,
			ImageAdmissionRevision:                 artifactSpec.AdmissionRevision,
			ImageAdmissionReceiptSHA256:            artifactSpec.AdmissionReceiptSHA256,
			ImageAdmissionReceiptOCIManifestDigest: artifactSpec.AdmissionReceiptOCIManifestDigest,
			ImagePolicyRevision:                    artifactSpec.PolicyRevision, ImagePolicySHA256: artifactSpec.PolicySHA256,
			ImageSignatureSHA256:         artifactSpec.SignatureSHA256,
			ImagePromotionReadbackSHA256: artifactSpec.PromotionReadbackSHA256,
			RoleRuntimeContractRevision:  artifactSpec.RoleRuntimeContractRevision,
			RoleRuntimeContractSHA256:    artifactSpec.RoleRuntimeContractSHA256,
			PromptProfileID:              prompt.ID, PromptRevision: instructionSpec.CurrentVersion,
			CredentialBindingIDs: credentialIDs, AuthorityPolicyVersion: service.authorityPolicyRevision,
			AuthorityPolicySHA256: service.authorityPolicySHA256, Components: components,
			CreatedAt: input.CreatedAt.UTC().Truncate(time.Microsecond), SessionID: session.ID,
			RoleID: role.ID, ChatID: legacyTargetIDFromOperations(operations, input.ChatRef),
			ProviderCredentialBindingID: providerCredential.ID,
			ProviderAccountName:         input.ProviderAccountName, EffectiveRuntimeSHA256: effectiveSHA256,
			CodexModel: input.CodexModel, CodexSandbox: input.CodexSandbox,
			CodexApprovalPolicy: input.CodexApprovalPolicy,
			AgentID:             agent.ID, AgentVersion: agent.Version, AgentSHA256: agentSHA,
			RoleDefinitionID: roleDefinition.ID, RoleDefinitionVersion: roleDefinition.Version,
			RoleDefinitionSHA256: roleDefinitionSHA, InstructionSetID: instruction.ID,
			InstructionSetVersion: instruction.Version, InstructionSetSHA256: instructionSHA,
			ProviderPoolID: pool.ID, ProviderPoolVersion: pool.Version, ProviderPoolSHA256: poolSHA,
			AgentAssignmentID: assignment.ID, AgentAssignmentVersion: assignment.Version,
			AgentAssignmentSHA256: assignmentSHA,
		}
		result := legacyResource(principal, projectID, session.ID, operation.TargetID,
			enum.KindRuntimeRevision, input.Name, enum.StateActive, spec, input.CreatedAt)
		if result.err != nil {
			return result.err
		}
		compiled.Resources[reference] = result.resource
	}
	return nil
}

func (service *Service) compileLegacySchedules(principal value.Principal, projectID string,
	operations map[string]entity.LegacyGraphOperation, compiled *compiledLegacyGraph, at time.Time,
) error {
	for reference, operation := range operations {
		input := operation.Schedule
		if input == nil {
			continue
		}
		agent, agentSHA, err := compiledLegacyTuple(compiled, input.AgentRef, enum.KindAgent)
		if err != nil {
			return err
		}
		assignment, assignmentSHA, err := compiledLegacyTuple(compiled, input.AssignmentRef, enum.KindAgentAssignment)
		if err != nil {
			return err
		}
		instruction, instructionSHA, err := compiledLegacyTuple(compiled, input.InstructionSetRef, enum.KindInstructionSet)
		if err != nil {
			return err
		}
		pool, poolSHA, err := compiledLegacyTuple(compiled, input.ProviderPoolRef, enum.KindProviderPool)
		if err != nil {
			return err
		}
		recipe, recipeSHA, err := compiledLegacyTuple(compiled, input.RoleImageRecipeRef, enum.KindRoleImageRecipe)
		if err != nil {
			return err
		}
		agentSpec := agent.Spec.(entity.AgentSpec)
		instructionSpec := instruction.Spec.(entity.InstructionSetSpec)
		effectiveSHA256, err := canonicalHash(struct {
			AgentID, AgentSHA256, InstructionID, InstructionSHA256 string
			PoolID, PoolSHA256, AssignmentID, AssignmentSHA256     string
			RoomID, RuntimeProfileRef                              string
		}{agent.ID, agentSHA, instruction.ID, instructionSHA, pool.ID, poolSHA,
			assignment.ID, assignmentSHA, legacyTargetIDFromOperations(operations, input.RoomRef), agentSpec.RuntimeProfileRef})
		if err != nil || input.EffectiveInputSHA256 != "" ||
			agentSpec.RuntimeProfileRef != "control-plane://runtime-profile/"+recipe.ID ||
			agentSpec.RuntimeProfileVersion != recipe.Version || agentSpec.RuntimeProfileSHA256 != recipeSHA {
			return errs.ErrFailedPrecondition
		}
		spec := entity.ScheduleSpec{
			TargetResourceID: agent.ID, TargetKind: enum.KindAgent, TargetVersion: agent.Version,
			EffectiveInputSHA: effectiveSHA256, Cron: input.CronExpression,
			Timezone: input.Timezone, Calendar: input.Calendar, OverlapPolicy: input.OverlapPolicy,
			MisfirePolicy: input.MisfirePolicy, MisfireGrace: input.MisfireGrace,
			NextRunAt: input.NextRunAt, DeliveryPolicy: input.DeliveryPolicy,
			MaximumAttempts: input.MaximumAttempts, InitialBackoff: input.InitialBackoff,
			MaximumBackoff: input.MaximumBackoff, DeadLetterAfter: input.DeadLetterAfter,
			SessionPolicy:            input.SessionPolicy,
			RoomID:                   legacyTargetIDFromOperations(operations, input.RoomRef),
			NotificationPolicy:       input.NotificationPolicy,
			MaximumExecutionDuration: input.MaximumExecutionDuration, Coalesce: input.Coalesce,
			TargetType: "AGENT", PromptArtifactID: instructionSpec.ContentArtifactID,
			AgentID: agent.ID, AgentVersion: agent.Version, AgentSHA256: agentSHA,
			InstructionSetID: instruction.ID, InstructionSetVersion: instruction.Version,
			InstructionSetSHA256:    instructionSHA,
			RuntimeSelectionRef:     agentSpec.RuntimeProfileRef,
			RuntimeSelectionVersion: agentSpec.RuntimeProfileVersion,
			RuntimeSelectionSHA256:  agentSpec.RuntimeProfileSHA256,
			ProviderPoolID:          pool.ID, ProviderPoolVersion: pool.Version, ProviderPoolSHA256: poolSHA,
			AgentAssignmentID: assignment.ID, AgentAssignmentVersion: assignment.Version,
			AgentAssignmentSHA256: assignmentSHA, Ownership: legacyOwnership(input.Source),
		}
		result := legacyResource(principal, projectID, "", operation.TargetID,
			enum.KindSchedule, input.Name, input.State, spec, at)
		if result.err != nil {
			return result.err
		}
		compiled.Resources[reference] = result.resource
	}
	return nil
}

func (service *Service) compileLegacyTurnsAndProcesses(principal value.Principal, projectID string,
	plan entity.LegacyGraphPlan, operations map[string]entity.LegacyGraphOperation,
	compiled *compiledLegacyGraph, at time.Time,
) error {
	for reference, operation := range operations {
		input := operation.Turn
		if input == nil {
			continue
		}
		session, _, err := compiledLegacyTuple(compiled, input.SessionRef, enum.KindSession)
		if err != nil {
			return err
		}
		prompt, _, err := compiledLegacyTuple(compiled, input.PromptArtifactRef, enum.KindArtifact)
		if err != nil {
			return err
		}
		runtimeRevision, _, err := compiledLegacyTuple(compiled, input.RuntimeRevisionRef, enum.KindRuntimeRevision)
		if err != nil {
			return err
		}
		spec := entity.TurnSpec{
			SessionID: session.ID, Sequence: input.Sequence, SourceRef: input.SourceTurnRef,
			PromptArtifactID: prompt.ID, RuntimeRevisionID: runtimeRevision.ID,
			ProcessRunID: legacyTargetIDFromOperations(operations, input.ProcessRunRef),
			Attempt:      input.Attempt, Outcome: input.Outcome,
			ResultArtifactID:     legacyTargetIDFromOperations(operations, input.ResultArtifactRef),
			EffectiveInputSHA256: input.EffectiveInputSHA256,
			PredecessorTurnID:    legacyTargetIDFromOperations(operations, input.PredecessorTurnRef),
		}
		if input.ResultArtifactRef != "" {
			result, resultSHA, err := compiledLegacyTuple(compiled, input.ResultArtifactRef, enum.KindArtifact)
			if err != nil {
				return err
			}
			spec.ResultArtifactVersion, spec.ResultArtifactSHA256 = result.Version, resultSHA
		}
		result := legacyResource(principal, projectID, session.ID, operation.TargetID,
			enum.KindTurn, input.Name, input.State, spec, at)
		if result.err != nil {
			return result.err
		}
		compiled.Resources[reference] = result.resource
	}
	for reference, operation := range operations {
		input := operation.ProcessRun
		if input == nil {
			continue
		}
		rootSession, _, err := compiledLegacyTuple(compiled, input.RootSessionRef, enum.KindSession)
		if err != nil {
			return err
		}
		rootTurn, _, err := compiledLegacyTuple(compiled, input.RootTurnRef, enum.KindTurn)
		if err != nil {
			return err
		}
		runtimeRevision, _, err := compiledLegacyTuple(compiled, input.RuntimeRevisionRef, enum.KindRuntimeRevision)
		if err != nil {
			return err
		}
		rootAttemptIndex := legacyRefIndex(plan, input.RootAttemptRef)
		if rootAttemptIndex < 0 || plan.Operations[rootAttemptIndex].TurnAttempt == nil {
			return errs.ErrFailedPrecondition
		}
		rootAttempt := plan.Operations[rootAttemptIndex].TurnAttempt
		spec := entity.ProcessRunSpec{
			ParentProcessRunID: legacyTargetIDFromOperations(operations, input.ParentProcessRef),
			PlaybookRef:        input.PlaybookRef, PolicyRevision: service.authorityPolicyRevision,
			RootTriggerRef: input.RootTriggerRef, RootInitiatorActorID: principal.ActorID,
			RootSessionID: rootSession.ID, RootSessionVersion: rootSession.Version,
			RootTurnID: rootTurn.ID, RootTurnVersion: rootTurn.Version, RootAttempt: rootAttempt.Attempt,
			ImmutableInputSHA256: input.ImmutableInputSHA256, RuntimeRevisionID: runtimeRevision.ID,
			Outcome: input.Outcome,
		}
		if input.ParentProcessRef != "" {
			launchingTurn, _, launchErr := compiledLegacyTuple(compiled, input.LaunchingTurnRef, enum.KindTurn)
			if launchErr != nil {
				return launchErr
			}
			launchIndex := legacyRefIndex(plan, input.LaunchingAttemptRef)
			targetIndex := legacyRefIndex(plan, input.TargetAttemptRef)
			if launchIndex < 0 || targetIndex < 0 || plan.Operations[launchIndex].TurnAttempt == nil ||
				plan.Operations[targetIndex].TurnAttempt == nil {
				return errs.ErrFailedPrecondition
			}
			targetSession, _, targetErr := compiledLegacyTuple(compiled, input.TargetSessionRef, enum.KindSession)
			if targetErr != nil {
				return targetErr
			}
			targetTurn, _, targetErr := compiledLegacyTuple(compiled, input.TargetTurnRef, enum.KindTurn)
			if targetErr != nil {
				return targetErr
			}
			spec.LaunchingProcessRunID = spec.ParentProcessRunID
			spec.LaunchingTurnID = launchingTurn.ID
			spec.LaunchingAttempt = plan.Operations[launchIndex].TurnAttempt.Attempt
			spec.DelegationID = legacyTargetIDFromOperations(operations, input.DelegationRef)
			spec.TargetSessionID, spec.TargetSessionVersion = targetSession.ID, targetSession.Version
			spec.TargetTurnID, spec.TargetTurnVersion = targetTurn.ID, targetTurn.Version
			spec.TargetAttempt = plan.Operations[targetIndex].TurnAttempt.Attempt
		}
		if !input.State.Terminal() {
			currentSession, currentTurn, currentAttempt := rootSession, rootTurn, rootAttempt
			if input.ParentProcessRef != "" {
				currentSession = compiled.Resources[input.TargetSessionRef]
				currentTurn = compiled.Resources[input.TargetTurnRef]
				currentAttempt = plan.Operations[legacyRefIndex(plan, input.TargetAttemptRef)].TurnAttempt
			}
			spec.CurrentSessionID, spec.CurrentSessionVersion = currentSession.ID, currentSession.Version
			spec.CurrentTurnID, spec.CurrentTurnVersion = currentTurn.ID, currentTurn.Version
			spec.CurrentAttempt = currentAttempt.Attempt
			spec.CurrentRuntimeRevisionID, spec.CurrentRuntimeRevisionVersion = runtimeRevision.ID, runtimeRevision.Version
			spec.CurrentInputSHA256 = currentAttempt.ImmutableInputSHA256
		}
		result := legacyResource(principal, projectID, rootSession.ID, operation.TargetID,
			enum.KindProcessRun, input.Name, input.State, spec, at)
		if result.err != nil {
			return result.err
		}
		compiled.Resources[reference] = result.resource
	}
	return nil
}
