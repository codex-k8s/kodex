package resource

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

const runtimeIncidentRunbookURL = "https://github.com/codex-k8s/matter-codex/blob/main/docs/runbooks/control-plane-owner-readbacks.md"

func ownerOpaqueRef(kind, id string) string {
	return strings.ToLower(kind) + ":" + hashString(kind + "\x00" + id)[:24]
}

func (service *Service) AgentOwnerProjection(
	ctx context.Context,
	principal value.Principal,
	agent entity.Resource,
) (AgentOwnerProjection, error) {
	if agent.Kind != enum.KindAgent || agent.OwnerActorID != principal.ActorID {
		return AgentOwnerProjection{}, errs.ErrNotFound
	}
	spec, ok := agent.Spec.(entity.AgentSpec)
	if !ok {
		return AgentOwnerProjection{}, errs.ErrStateConflict
	}
	result := AgentOwnerProjection{AgentRef: agent.ID, DisplayName: agent.Name,
		StableKey: spec.StableKey, Version: agent.Version, State: agent.State, Enabled: spec.Enabled,
		Capabilities: slices.Clone(spec.Capabilities), BotIdentity: AgentBotIdentityProjection{Status: "UNBOUND"}}
	switch spec.BotMaskedStatus {
	case "AVAILABLE":
		if spec.BotUsername == "" || spec.BotProviderGeneration == 0 {
			return AgentOwnerProjection{}, errs.ErrStateConflict
		}
		result.BotIdentity = AgentBotIdentityProjection{Status: "BOUND", Username: spec.BotUsername,
			MaskedStatus: spec.BotMaskedStatus, ProviderGeneration: spec.BotProviderGeneration}
	case "REVOKED":
		if spec.BotProviderGeneration == 0 {
			return AgentOwnerProjection{}, errs.ErrStateConflict
		}
		result.BotIdentity = AgentBotIdentityProjection{Status: "REVOKED", MaskedStatus: spec.BotMaskedStatus,
			ProviderGeneration: spec.BotProviderGeneration}
	case "":
		if spec.BotIdentityRef != "" || spec.BotUsername != "" || spec.BotProviderRevision != 0 ||
			spec.BotProviderGeneration != 0 || spec.BotProviderTeamRef != "" || spec.BotReceiptID != "" ||
			spec.BotReceiptVersion != 0 || spec.BotReceiptSHA256 != "" {
			return AgentOwnerProjection{}, errs.ErrStateConflict
		}
	default:
		return AgentOwnerProjection{}, errs.ErrStateConflict
	}
	result.RuntimeSelection = AgentRuntimeSelectionProjection{
		RuntimeProfileVersion: spec.RuntimeProfileVersion, RuntimeProfileSHA256: spec.RuntimeProfileSHA256,
		RoleDefinitionVersion: spec.RoleDefinitionVersion, RoleDefinitionSHA256: spec.RoleDefinitionSHA256,
		Status: OwnerProjectionStale,
	}
	role, err := service.repository.Get(ctx, principal.OrganizationID, principal.ProjectID,
		spec.RoleDefinitionID, enum.KindRoleDefinition)
	if errors.Is(err, errs.ErrNotFound) {
		return result, nil
	}
	if err != nil {
		return AgentOwnerProjection{}, err
	}
	roleSpec, ok := role.Spec.(entity.RoleDefinitionSpec)
	digest, digestErr := entity.ProjectionSHA256(role)
	if !ok || digestErr != nil {
		return AgentOwnerProjection{}, errs.ErrStateConflict
	}
	if role.OwnerActorID != principal.ActorID {
		return AgentOwnerProjection{}, errs.ErrNotFound
	}
	result.RuntimeSelection.SelectionKey, result.RuntimeSelection.DisplayName = roleSpec.StableKey, role.Name
	if role.State != enum.StateActive {
		result.RuntimeSelection.Status = OwnerProjectionIneligible
		return result, nil
	}
	if roleSpec.RoleImageRecipeID == "" {
		return result, nil
	}
	recipe, err := service.repository.Get(ctx, principal.OrganizationID, principal.ProjectID,
		roleSpec.RoleImageRecipeID, enum.KindRoleImageRecipe)
	if errors.Is(err, errs.ErrNotFound) {
		return result, nil
	}
	if err != nil {
		return AgentOwnerProjection{}, err
	}
	if recipe.OwnerActorID != principal.ActorID {
		return AgentOwnerProjection{}, errs.ErrNotFound
	}
	if _, ok := recipe.Spec.(entity.RoleImageRecipeSpec); !ok {
		return AgentOwnerProjection{}, errs.ErrStateConflict
	}
	recipeDigest, err := entity.ProjectionSHA256(recipe)
	if err != nil {
		return AgentOwnerProjection{}, errs.ErrStateConflict
	}
	if recipe.State != enum.StateActive {
		result.RuntimeSelection.Status = OwnerProjectionIneligible
		return result, nil
	}
	if role.Version == spec.RoleDefinitionVersion && digest == spec.RoleDefinitionSHA256 &&
		spec.RuntimeProfileRef == "control-plane://runtime-profile/"+recipe.ID &&
		recipe.Version == roleSpec.RoleImageRecipeVersion && recipeDigest == roleSpec.RoleImageRecipeSHA256 &&
		roleSpec.RoleImageRecipeVersion == spec.RuntimeProfileVersion &&
		roleSpec.RoleImageRecipeSHA256 == spec.RuntimeProfileSHA256 {
		result.RuntimeSelection.Status = OwnerProjectionPresent
	}
	return result, nil
}

func (service *Service) GetOwnerConfigurationCatalog(
	ctx context.Context,
	principal value.Principal,
	afterID string,
	limit int,
) (OwnerConfigurationCatalog, error) {
	roles, err := service.ListProtectedConfigurations(ctx, principal, enum.KindRoleDefinition,
		[]enum.State{enum.StateActive}, afterID, limit)
	if err != nil {
		return OwnerConfigurationCatalog{}, err
	}
	result := OwnerConfigurationCatalog{}
	for _, role := range roles {
		spec, ok := role.Spec.(entity.RoleDefinitionSpec)
		if !ok || role.OwnerActorID != principal.ActorID || spec.RoleImageRecipeID == "" {
			continue
		}
		digest, digestErr := entity.ProjectionSHA256(role)
		if digestErr != nil {
			return OwnerConfigurationCatalog{}, errs.ErrInternal
		}
		status := OwnerProjectionStale
		recipe, recipeErr := service.repository.Get(ctx, principal.OrganizationID, principal.ProjectID,
			spec.RoleImageRecipeID, enum.KindRoleImageRecipe)
		if recipeErr != nil && !errors.Is(recipeErr, errs.ErrNotFound) {
			return OwnerConfigurationCatalog{}, recipeErr
		}
		if recipeErr == nil {
			if recipe.OwnerActorID != principal.ActorID {
				return OwnerConfigurationCatalog{}, errs.ErrNotFound
			}
			if _, ok := recipe.Spec.(entity.RoleImageRecipeSpec); !ok {
				return OwnerConfigurationCatalog{}, errs.ErrStateConflict
			}
			recipeSHA, recipeSHAErr := entity.ProjectionSHA256(recipe)
			if recipeSHAErr != nil {
				return OwnerConfigurationCatalog{}, errs.ErrStateConflict
			}
			if recipe.State != enum.StateActive {
				status = OwnerProjectionIneligible
			} else if recipe.Version == spec.RoleImageRecipeVersion && recipeSHA == spec.RoleImageRecipeSHA256 {
				status = OwnerProjectionPresent
			}
		}
		result.RuntimeSelections = append(result.RuntimeSelections, OwnerRuntimeSelectionCatalogEntry{
			SelectionKey: spec.StableKey, DisplayName: role.Name, Description: spec.Description,
			RoleDefinitionVersion: role.Version, RoleDefinitionSHA256: digest,
			RuntimeProfileVersion: spec.RoleImageRecipeVersion, RuntimeProfileSHA256: spec.RoleImageRecipeSHA256,
			Capabilities: slices.Clone(spec.Capabilities), Status: status,
		})
	}
	result.SchedulePresets, err = ownerSchedulePresets()
	if err != nil {
		return OwnerConfigurationCatalog{}, errs.ErrInternal
	}
	result.ScheduleDefaults, err = ownerScheduleDefaults()
	if err != nil {
		return OwnerConfigurationCatalog{}, errs.ErrInternal
	}
	if len(roles) == limit {
		result.NextPageToken = roles[len(roles)-1].ID
	}
	return result, nil
}

func (service *Service) ManageOwnerSchedule(
	ctx context.Context,
	input ManageOwnerScheduleInput,
) (OwnerScheduleProjection, error) {
	if input.Action != "create" && input.Action != "update" {
		return OwnerScheduleProjection{}, errs.ErrInvalidInput
	}
	if err := authorize(input.Principal, permissionScheduleCreateFromSelections); err != nil {
		return OwnerScheduleProjection{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || value.ValidateName(input.Name) != nil ||
		value.ValidateStableKey(input.AgentStableKey) != nil ||
		value.ValidateStableKey(input.InstructionSetStableKey) != nil ||
		value.ValidateStableKey(input.ProviderPoolStableKey) != nil ||
		(input.Selection.RoomStableKey != "" && value.ValidateStableKey(input.Selection.RoomStableKey) != nil) ||
		(input.Action == "create" && (input.ScheduleID != "" || input.ExpectedVersion != 0)) ||
		(input.Action == "update" && (value.ValidateID(input.ScheduleID) != nil || input.ExpectedVersion == 0)) {
		return OwnerScheduleProjection{}, errs.ErrInvalidInput
	}
	spec, err := buildOwnerScheduleSpec(input.Selection)
	if err != nil {
		return OwnerScheduleProjection{}, err
	}
	prompt, err := service.prepareOwnerSchedulePrompt(ctx, input.Principal, input.Selection.Prompt)
	if err != nil {
		return OwnerScheduleProjection{}, err
	}
	if input.Action == "create" {
		created, createErr := service.CreateScheduleFromOwnerSelections(ctx, CreateScheduleFromOwnerSelectionsInput{
			Principal: input.Principal, IdempotencyKey: input.IdempotencyKey, Name: input.Name,
			AgentStableKey: input.AgentStableKey, InstructionSetStableKey: input.InstructionSetStableKey,
			ProviderPoolStableKey: input.ProviderPoolStableKey, RoomStableKey: input.Selection.RoomStableKey,
			PromptArtifactName: prompt.ArtifactName, PromptKind: prompt.Kind,
			PromptInlineMarkdown: prompt.InlineMarkdown, PromptObject: prompt.Object, Spec: spec,
		})
		if createErr != nil {
			return OwnerScheduleProjection{}, createErr
		}
		projection, ok := scheduleResourceProjection(created)
		if !ok {
			return OwnerScheduleProjection{}, errs.ErrInternal
		}
		return projection, nil
	}
	requestHash, err := canonicalHash(struct {
		Identity                                           commandIdentity
		Action, ScheduleID, Name                           string
		ExpectedVersion                                    uint64
		Agent, Instruction, Pool, Preset, Timezone, Room   string
		PromptKind, PromptArtifactName, PromptInlineSHA256 string
		Overrides                                          OwnerScheduleOverrides
		Spec                                               entity.ScheduleSpec
	}{identity(input.Principal), input.Action, input.ScheduleID, input.Name, input.ExpectedVersion,
		input.AgentStableKey, input.InstructionSetStableKey, input.ProviderPoolStableKey,
		input.Selection.PresetKey, input.Selection.Timezone, input.Selection.RoomStableKey,
		prompt.Kind, prompt.ArtifactName, hashString(prompt.InlineMarkdown), input.Selection.Overrides, spec})
	if err != nil {
		return OwnerScheduleProjection{}, errs.ErrInvalidInput
	}
	updated, err := service.withOwnerLockedResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		"manage_owner_schedule_update", requestHash, input.ScheduleID, enum.KindSchedule, input.ExpectedVersion,
		func(stored entity.Resource) error {
			if stored.OwnerActorID != input.Principal.ActorID || stored.Version != input.ExpectedVersion+1 {
				return errs.ErrStateConflict
			}
			return nil
		}, func(tx domainrepo.Transaction) (entity.Resource, error) {
			return service.updateOwnerSchedule(ctx, tx, input, spec, prompt)
		})
	if err != nil {
		return OwnerScheduleProjection{}, err
	}
	projection, ok := scheduleResourceProjection(updated)
	if !ok {
		return OwnerScheduleProjection{}, errs.ErrInternal
	}
	return projection, nil
}

func (service *Service) updateOwnerSchedule(
	ctx context.Context,
	tx domainrepo.Transaction,
	input ManageOwnerScheduleInput,
	spec entity.ScheduleSpec,
	prompt OwnerSchedulePromptInput,
) (entity.Resource, error) {
	protected, ok := tx.(domainrepo.ProtectedTransaction)
	if !ok {
		return entity.Resource{}, errs.ErrInternal
	}
	current, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, input.ScheduleID)
	if err != nil {
		return entity.Resource{}, err
	}
	previous, ok := current.Spec.(entity.ScheduleSpec)
	if !ok || current.Kind != enum.KindSchedule || current.OwnerActorID != input.Principal.ActorID {
		return entity.Resource{}, errs.ErrNotFound
	}
	if current.Version != input.ExpectedVersion {
		return entity.Resource{}, errs.ErrVersionMismatch
	}
	if current.State != enum.StateActive && current.State != enum.StatePaused {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if previous.SessionPolicy != spec.SessionPolicy {
		return entity.Resource{}, errs.ErrStateConflict
	}
	open, err := tx.HasOpenScheduleOccurrence(ctx, current.OrganizationID, current.ProjectID, current.ID)
	if err != nil {
		return entity.Resource{}, err
	}
	if open {
		return entity.Resource{}, errs.ErrStateConflict
	}
	now, err := tx.CurrentTime(ctx)
	if err != nil {
		return entity.Resource{}, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	workspace, workspaceSHA, err := lockActiveWorkspace(ctx, tx, input.Principal)
	if err != nil {
		return entity.Resource{}, err
	}
	roomID := ""
	if input.Selection.RoomStableKey != "" {
		room, roomErr := protected.GetByStableKeyForUpdate(ctx, input.Principal.OrganizationID,
			input.Principal.ProjectID, enum.KindChat, input.Selection.RoomStableKey)
		if roomErr != nil {
			return entity.Resource{}, roomErr
		}
		if room.State != enum.StateActive || room.OwnerActorID != input.Principal.ActorID {
			return entity.Resource{}, errs.ErrNotFound
		}
		roomID = room.ID
	}
	agent, err := requireProtectedStable(ctx, protected, input.Principal, enum.KindAgent, input.AgentStableKey)
	if err != nil {
		return entity.Resource{}, err
	}
	instruction, err := requireProtectedStable(ctx, protected, input.Principal, enum.KindInstructionSet, input.InstructionSetStableKey)
	if err != nil {
		return entity.Resource{}, err
	}
	pool, err := requireProtectedStable(ctx, protected, input.Principal, enum.KindProviderPool, input.ProviderPoolStableKey)
	if err != nil {
		return entity.Resource{}, err
	}
	agentSpec, agentOK := agent.Spec.(entity.AgentSpec)
	instructionSpec, instructionOK := instruction.Spec.(entity.InstructionSetSpec)
	if !agentOK || !instructionOK || !agentSpec.Enabled || agent.State != enum.StateActive ||
		instructionSpec.VersionState != "PUBLISHED" || agentSpec.InstructionSetID != instruction.ID ||
		agentSpec.InstructionSetVersion != instruction.Version || agentSpec.ProviderPoolID != pool.ID ||
		agentSpec.ProviderPoolVersion != pool.Version {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if _, _, err := lockAgentRuntimeProfile(ctx, tx, input.Principal, agentSpec); err != nil {
		return entity.Resource{}, err
	}
	agentID, agentVersion, agentSHA, err := protectedTuple(agent)
	if err != nil {
		return entity.Resource{}, err
	}
	instructionID, instructionVersion, instructionSHA, err := protectedTuple(instruction)
	if err != nil {
		return entity.Resource{}, err
	}
	poolID, poolVersion, poolSHA, err := protectedTuple(pool)
	if err != nil {
		return entity.Resource{}, err
	}
	assignment, err := lockActiveAgentAssignment(ctx, tx, input.Principal, agent.ID, agent.Version,
		agentSHA, workspace.Version, workspaceSHA, roomID)
	if err != nil {
		return entity.Resource{}, err
	}
	assignmentSHA, err := entity.ProjectionSHA256(assignment)
	if err != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	promptResource, promptSpec, err := service.resolveOwnerSchedulePrompt(ctx, tx, protected,
		input.Principal, prompt, now)
	if err != nil {
		return entity.Resource{}, err
	}
	spec.TargetResourceID, spec.TargetKind, spec.TargetVersion = agentID, enum.KindAgent, agentVersion
	spec.TargetType, spec.PromptArtifactID, spec.RoomID = "AGENT", promptResource.ID, roomID
	spec.AgentID, spec.AgentVersion, spec.AgentSHA256 = agentID, agentVersion, agentSHA
	spec.InstructionSetID, spec.InstructionSetVersion, spec.InstructionSetSHA256 = instructionID, instructionVersion, instructionSHA
	spec.ProviderPoolID, spec.ProviderPoolVersion, spec.ProviderPoolSHA256 = poolID, poolVersion, poolSHA
	spec.RuntimeSelectionRef, spec.RuntimeSelectionVersion, spec.RuntimeSelectionSHA256 =
		agentSpec.RuntimeProfileRef, agentSpec.RuntimeProfileVersion, agentSpec.RuntimeProfileSHA256
	spec.AgentAssignmentID, spec.AgentAssignmentVersion, spec.AgentAssignmentSHA256 = assignment.ID, assignment.Version, assignmentSHA
	spec.PromptIntentKind, spec.PromptArtifactVersion, spec.PromptSHA256 = prompt.Kind, promptResource.Version, promptSpec.SHA256
	if prompt.Kind == "INLINE" {
		spec.PromptDisplay = "Встроенный prompt"
	} else {
		spec.PromptDisplay = promptResource.Name
	}
	spec.Ownership = previous.Ownership
	if spec.SessionPolicy == "NEW" {
		spec.ExecutionSessionID = ""
	} else {
		session, sessionErr := service.rebindScheduleSession(ctx, tx, input.Principal, current,
			previous.ExecutionSessionID, spec, now)
		if sessionErr != nil {
			return entity.Resource{}, sessionErr
		}
		spec.ExecutionSessionID = session.ID
	}
	spec.EffectiveInputSHA, err = targetScheduleEffectiveInput(spec, promptSpec.SHA256)
	if err != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	spec.NextRunAt, err = firstScheduleRun(spec, now)
	updatedSpec, ownershipErr := configurationUpdateSpec(ctx, tx, input.Principal, previous, spec, false)
	if err != nil || spec.Validate() != nil || ownershipErr != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	updated, err := current.Update(input.Name, updatedSpec, now)
	if err != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, updated, current.Version); err != nil {
		return entity.Resource{}, err
	}
	return updated, service.appendMutationRecords(ctx, tx, input.Principal, "manage_owner_schedule_update", updated)
}

func (service *Service) GetOwnerSchedule(
	ctx context.Context,
	principal value.Principal,
	id string,
) (OwnerScheduleProjection, error) {
	resource, err := service.GetProtectedConfiguration(ctx, principal, id, enum.KindSchedule)
	if err != nil {
		return OwnerScheduleProjection{}, err
	}
	projection, ok := scheduleResourceProjection(resource)
	if !ok {
		return OwnerScheduleProjection{}, errs.ErrStateConflict
	}
	return projection, nil
}

func (service *Service) ListOwnerSchedules(
	ctx context.Context,
	principal value.Principal,
	states []enum.State,
	afterID string,
	limit int,
) ([]OwnerScheduleProjection, string, error) {
	resources, err := service.ListProtectedConfigurations(ctx, principal, enum.KindSchedule, states, afterID, limit)
	if err != nil {
		return nil, "", err
	}
	result := make([]OwnerScheduleProjection, 0, len(resources))
	for _, item := range resources {
		projection, ok := scheduleResourceProjection(item)
		if !ok {
			return nil, "", errs.ErrStateConflict
		}
		result = append(result, projection)
	}
	next := ""
	if len(resources) == limit {
		next = resources[len(resources)-1].ID
	}
	return result, next, nil
}

func runNextActions(process entity.Resource, turn entity.Resource, runtime *domainrepo.RuntimeExecution) []string {
	if process.Kind != enum.KindProcessRun || process.State == enum.StateDeleted {
		return nil
	}
	if !process.State.Terminal() {
		return []string{"CANCEL"}
	}
	if turn.ID == "" || (turn.State != enum.StateFailed && turn.State != enum.StateCancelled && turn.State != enum.StateExpired) ||
		runtime == nil || (runtime.State != "FAILED" && runtime.State != "CANCELLED" && runtime.State != "EXPIRED") {
		return nil
	}
	return []string{"RETRY"}
}

func (service *Service) runOwnerProjection(
	ctx context.Context,
	principal value.Principal,
	detail RunDetailResult,
) (RunOwnerProjection, error) {
	if detail.ProcessRun.OwnerActorID != principal.ActorID {
		return RunOwnerProjection{}, errs.ErrNotFound
	}
	if !slices.Contains([]enum.State{enum.StateQueued, enum.StateClaimed, enum.StateRunning,
		enum.StateWaitingOwner, enum.StateWaitingExternal, enum.StateBlocked, enum.StateSucceeded,
		enum.StateFailed, enum.StateCancelled, enum.StateExpired}, detail.ProcessRun.State) {
		return RunOwnerProjection{}, errs.ErrStateConflict
	}
	spec, ok := detail.ProcessRun.Spec.(entity.ProcessRunSpec)
	if !ok {
		return RunOwnerProjection{}, errs.ErrStateConflict
	}
	result := RunOwnerProjection{RunRef: detail.ProcessRun.ID, DisplayName: detail.ProcessRun.Name,
		Version: detail.ProcessRun.Version, State: detail.ProcessRun.State, Attempt: spec.CurrentAttempt,
		StartedAt: detail.ProcessRun.CreatedAt, UpdatedAt: detail.ProcessRun.UpdatedAt,
		Workspace:     OwnerDisplayValue{Status: OwnerProjectionUnavailable},
		Trigger:       OwnerDisplayValue{Status: OwnerProjectionUnavailable},
		RuntimeStatus: OwnerDisplayValue{Status: OwnerProjectionUnavailable},
		NextActions:   runNextActions(detail.ProcessRun, detail.Turn, detail.Runtime)}
	if result.Attempt == 0 {
		result.Attempt = spec.RootAttempt
	}
	if !result.StartedAt.IsZero() && !result.UpdatedAt.Before(result.StartedAt) {
		result.Duration = result.UpdatedAt.Sub(result.StartedAt)
	}
	project, err := service.repository.Get(ctx, principal.OrganizationID, principal.ProjectID,
		principal.ProjectID, enum.KindProject)
	if err == nil && project.OwnerActorID == principal.ActorID {
		result.Workspace = OwnerDisplayValue{Status: OwnerProjectionPresent, Value: project.Name}
	} else if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return RunOwnerProjection{}, err
	}
	if spec.RootTriggerRef != "" {
		if display := safeTriggerDisplay(spec.RootTriggerRef); display != "" {
			result.Trigger = OwnerDisplayValue{Status: OwnerProjectionPresent, Value: display}
		}
	}
	if detail.Runtime != nil {
		if !validRuntimeExecutionState(detail.Runtime.State) {
			return RunOwnerProjection{}, errs.ErrStateConflict
		}
		result.RuntimeStatus = OwnerDisplayValue{Status: OwnerProjectionPresent, Value: detail.Runtime.State}
	}
	return result, nil
}

func (service *Service) RunOwnerProjection(
	ctx context.Context,
	principal value.Principal,
	detail RunDetailResult,
) (RunOwnerProjection, error) {
	return service.runOwnerProjection(ctx, principal, detail)
}

func RunTimelineOwnerProjections(items []domainrepo.Audit, nextActions []string) []RunTimelineProjection {
	result := make([]RunTimelineProjection, 0, len(items))
	for _, item := range items {
		display := map[string]string{
			"start_process": "Запуск создан", "cancel_process": "Запуск отменён",
			"retry_turn": "Создана новая попытка", "complete_process": "Запуск завершён",
		}[item.Action]
		if display == "" {
			display = "Состояние запуска изменено"
		}
		result = append(result, RunTimelineProjection{EventRef: ownerOpaqueRef("event", item.ID),
			Kind: "STATE_CHANGE", Display: display, Outcome: safeRunTimelineOutcome(item.Outcome), Version: item.ResourceVersion,
			OccurredAt: item.OccurredAt, NextActions: slices.Clone(nextActions)})
	}
	return result
}

func safeRunTimelineOutcome(outcome string) string {
	switch strings.ToUpper(outcome) {
	case "SUCCEEDED", "SUCCESS":
		return "SUCCEEDED"
	case "FAILED", "FAILURE", "LEASE_EXPIRED", "EXECUTION_FAILED":
		return "FAILED"
	case "CANCELLED":
		return "CANCELLED"
	case "EXPIRED":
		return "EXPIRED"
	case "RETRIED", "RETRY":
		return "RETRIED"
	case "REQUIRES_HUMAN", "WAITING_OWNER":
		return "REQUIRES_OWNER"
	default:
		return "OTHER"
	}
}

func RunLineageOwnerProjections(lineage RunLineageResult) ([]RunLineageProjection, error) {
	refs := make(map[string]string, len(lineage.Processes)+len(lineage.Attempts))
	for _, node := range append(slices.Clone(lineage.Processes), lineage.Attempts...) {
		if node.ID == "" || (node.NodeType != "PROCESS" && node.NodeType != "ATTEMPT") {
			return nil, errs.ErrStateConflict
		}
		if _, duplicate := refs[node.ID]; duplicate {
			return nil, errs.ErrStateConflict
		}
		refs[node.ID] = ownerOpaqueRef(strings.ToLower(node.NodeType), node.ID)
	}
	result := make([]RunLineageProjection, 0, len(refs))
	for _, node := range lineage.Processes {
		if !validRunLineageState(node.State) {
			return nil, errs.ErrStateConflict
		}
		if node.ParentProcessRunID != "" {
			if _, found := refs[node.ParentProcessRunID]; !found {
				return nil, errs.ErrStateConflict
			}
		}
		result = append(result, RunLineageProjection{NodeRef: refs[node.ID], ParentRef: refs[node.ParentProcessRunID],
			Kind: "PROCESS", State: node.State, Version: node.Version,
			CreatedAt: node.OccurredAt, UpdatedAt: node.UpdatedAt})
	}
	for _, node := range lineage.Attempts {
		if !validRunLineageState(node.State) {
			return nil, errs.ErrStateConflict
		}
		if _, found := refs[node.ProcessRunID]; !found {
			return nil, errs.ErrStateConflict
		}
		result = append(result, RunLineageProjection{NodeRef: refs[node.ID], ParentRef: refs[node.ProcessRunID],
			Kind: "ATTEMPT", State: node.State, Version: node.Version, Attempt: node.Attempt,
			CreatedAt: node.OccurredAt, UpdatedAt: node.UpdatedAt})
	}
	return result, nil
}

func validRunLineageState(state string) bool {
	return slices.Contains([]string{"PENDING", "ADMITTED", "QUEUED", "CLAIMED", "RUNNING",
		"WAITING_OWNER", "WAITING_EXTERNAL", "BLOCKED", "SUCCEEDED", "FAILED", "CANCELLED",
		"EXPIRED", "RETRIED", "SUSPENDED"}, state)
}

func validRuntimeExecutionState(state string) bool {
	return slices.Contains([]string{"PENDING", "ADMITTED", "RUNNING", "SUCCEEDED", "FAILED",
		"CANCELLED", "EXPIRED", "RETRIED", "SUSPENDED"}, state)
}

func RunArtifactOwnerProjections(items []entity.Resource) ([]RunArtifactProjection, error) {
	result := make([]RunArtifactProjection, 0, len(items))
	for _, item := range items {
		spec, ok := item.Spec.(entity.ArtifactSpec)
		if !ok || item.Kind != enum.KindArtifact || !slices.Contains([]string{
			"PENDING", "SCANNING", "CLEAN", "QUARANTINED", "FAILED"}, spec.ScanStatus) {
			return nil, errs.ErrStateConflict
		}
		result = append(result, RunArtifactProjection{ArtifactRef: ownerOpaqueRef("artifact", item.ID), DisplayName: item.Name,
			Kind: spec.ArtifactKind, MediaType: spec.MediaType, SizeBytes: spec.SizeBytes,
			SHA256: spec.SHA256, Status: spec.ScanStatus, CreatedAt: item.CreatedAt})
	}
	return result, nil
}

func safeTriggerDisplay(reference string) string {
	prefix := strings.SplitN(reference, ":", 2)[0]
	return map[string]string{"schedule-occurrence": "Расписание", "owner": "Владелец", "delegation": "Делегирование"}[prefix]
}

func (service *Service) ListOwnerRuns(
	ctx context.Context,
	principal value.Principal,
	states []enum.State,
	afterID string,
	limit int,
) ([]RunOwnerProjection, string, error) {
	resources, err := service.List(ctx, ListInput{Principal: principal, Filter: query.ResourceFilter{
		Kind: enum.KindProcessRun, States: states, AfterID: afterID, Limit: limit,
	}})
	if err != nil {
		return nil, "", err
	}
	result := make([]RunOwnerProjection, 0, len(resources))
	readPrincipal := principal
	readPrincipal.Permission = permissionRead
	for _, process := range resources {
		detail, detailErr := service.GetRunDetail(ctx, readPrincipal, process.ID)
		if detailErr != nil {
			return nil, "", detailErr
		}
		projection, projectionErr := service.runOwnerProjection(ctx, principal, detail)
		if projectionErr != nil {
			return nil, "", projectionErr
		}
		result = append(result, projection)
	}
	next := ""
	if len(resources) == limit {
		next = resources[len(resources)-1].ID
	}
	return result, next, nil
}

func runtimeIncidentNextActions(incidentState, executionState string, current bool) []string {
	switch incidentState {
	case "OPEN":
		actions := []string{"ACKNOWLEDGE"}
		if current && (executionState == "FAILED" || executionState == "EXPIRED") {
			actions = append(actions, "RETRY")
		}
		return actions
	case "ACKNOWLEDGED":
		actions := []string{}
		if current && (executionState == "FAILED" || executionState == "EXPIRED") {
			actions = append(actions, "RETRY")
		}
		if current && (executionState == "ADMITTED" || executionState == "RUNNING") {
			actions = append(actions, "RELEASE")
		}
		if runtimeTerminal(executionState) {
			actions = append(actions, "CLOSE")
		}
		return actions
	case "RETRYING":
		if runtimeTerminal(executionState) {
			return []string{"CLOSE"}
		}
	case "RELEASED":
		if !runtimeTerminal(executionState) {
			return nil
		}
		actions := []string{"CLOSE"}
		if current && executionState == "CANCELLED" {
			actions = append([]string{"RETRY"}, actions...)
		}
		return actions
	}
	return nil
}

func incidentSeverity(kind string) (string, string, bool) {
	switch kind {
	case "HEARTBEAT_MISSED":
		return "ERROR", "Выполнение остановлено и требует проверки", true
	case "WORKLOAD_UNAVAILABLE":
		return "CRITICAL", "Среда выполнения недоступна", true
	case "RECONCILE_FAILED":
		return "WARNING", "Не удалось согласовать состояние выполнения", true
	}
	return "", "", false
}

func (service *Service) RuntimeIncidentOwnerProjection(
	ctx context.Context,
	principal value.Principal,
	incident domainrepo.RuntimeIncident,
) (RuntimeIncidentOwnerProjection, error) {
	severity, impact, knownKind := incidentSeverity(incident.Kind)
	if !knownKind || !slices.Contains([]string{"OPEN", "ACKNOWLEDGED", "RETRYING", "RELEASED", "CLOSED"}, incident.State) {
		return RuntimeIncidentOwnerProjection{}, errs.ErrStateConflict
	}
	result := RuntimeIncidentOwnerProjection{IncidentRef: incident.ID,
		Version: incident.Version, Kind: incident.Kind, State: incident.State,
		Workspace: OwnerDisplayValue{Status: OwnerProjectionUnavailable},
		Run:       OwnerDisplayValue{Status: OwnerProjectionUnavailable}, RunbookURL: runtimeIncidentRunbookURL,
		OccurredAt: incident.OccurredAt, UpdatedAt: incident.UpdatedAt,
		SafeCorrelation: hashString(incident.ID + "\x00" + fmt.Sprint(incident.Version))[:16]}
	result.Severity, result.Impact = severity, impact
	result.DiagnosticSummary = result.Impact
	err := service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		execution, executionErr := tx.GetRuntimeExecutionForUpdate(ctx, incident.ExecutionID)
		if executionErr != nil {
			return executionErr
		}
		process, processErr := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, execution.ProcessID)
		if processErr != nil {
			return processErr
		}
		if process.Kind != enum.KindProcessRun || process.OwnerActorID != principal.ActorID {
			return errs.ErrNotFound
		}
		result.Run = OwnerDisplayValue{Status: OwnerProjectionPresent, Value: process.Name}
		spec, ok := process.Spec.(entity.ProcessRunSpec)
		if !ok || !validRuntimeExecutionState(execution.State) {
			return errs.ErrStateConflict
		}
		current := spec.CurrentTurnID == execution.TurnID && spec.CurrentAttempt == execution.Attempt
		result.NextActions = runtimeIncidentNextActions(incident.State, execution.State, current)
		project, projectErr := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, principal.ProjectID)
		if projectErr == nil && project.Kind == enum.KindProject && project.OwnerActorID == principal.ActorID {
			result.Workspace = OwnerDisplayValue{Status: OwnerProjectionPresent, Value: project.Name}
		} else if projectErr != nil && !errors.Is(projectErr, errs.ErrNotFound) {
			return projectErr
		}
		return nil
	})
	return result, err
}

func workspaceRestoreNextActions(resource entity.Resource, backup entity.Resource, now time.Time) []string {
	spec, ok := resource.Spec.(entity.WorkspaceRestoreSpec)
	if !ok || spec.Partial {
		return nil
	}
	if resource.State == enum.StateQueued || resource.State == enum.StateRunning {
		return []string{"CANCEL"}
	}
	backupSpec, backupOK := backup.Spec.(entity.WorkspaceBackupSpec)
	if (resource.State == enum.StateFailed || resource.State == enum.StateCancelled || resource.State == enum.StateExpired) &&
		backupOK && backup.State == enum.StateSucceeded && backupSpec.BackupState == "AVAILABLE" &&
		backupSpec.MembershipSHA256 == spec.MembershipSHA256 && backupSpec.RetainUntil.After(now) {
		return []string{"RETRY"}
	}
	return nil
}

func (service *Service) WorkspaceRestoreOwnerProjection(
	ctx context.Context,
	principal value.Principal,
	resource entity.Resource,
) (WorkspaceRestoreOwnerProjection, error) {
	if resource.Kind != enum.KindWorkspaceRestore || resource.OwnerActorID != principal.ActorID {
		return WorkspaceRestoreOwnerProjection{}, errs.ErrNotFound
	}
	spec, ok := resource.Spec.(entity.WorkspaceRestoreSpec)
	if !ok || spec.Validate() != nil || string(resource.State) != spec.RestoreState {
		return WorkspaceRestoreOwnerProjection{}, errs.ErrStateConflict
	}
	result := WorkspaceRestoreOwnerProjection{RestoreRef: resource.ID,
		DisplayName: resource.Name, Version: resource.Version, State: spec.RestoreState,
		Attempt: spec.Attempt, Generation: spec.Generation, MemberCount: uint32(len(spec.Members)),
		TerminalReasonCode: spec.TerminalReasonCode, CreatedAt: resource.CreatedAt, UpdatedAt: resource.UpdatedAt}
	err := service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		now, timeErr := tx.CurrentTime(ctx)
		if timeErr != nil {
			return timeErr
		}
		backup, backupErr := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, spec.BackupID)
		if errors.Is(backupErr, errs.ErrNotFound) {
			result.NextActions = workspaceRestoreNextActions(resource, entity.Resource{}, now)
			return nil
		}
		if backupErr != nil {
			return backupErr
		}
		if backup.OwnerActorID != principal.ActorID {
			return errs.ErrNotFound
		}
		result.NextActions = workspaceRestoreNextActions(resource, backup, now)
		return nil
	})
	return result, err
}
