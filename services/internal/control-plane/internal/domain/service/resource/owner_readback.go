package resource

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	domainobjectstore "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/client/objectstore"
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
	var result AgentOwnerProjection
	err := service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		current, getErr := tx.Get(ctx, principal.OrganizationID, principal.ProjectID, agent.ID)
		if getErr != nil {
			return getErr
		}
		if current.Version != agent.Version {
			return errs.ErrVersionMismatch
		}
		var projectionErr error
		result, projectionErr = service.agentOwnerProjectionFromTx(ctx, tx, principal, current)
		return projectionErr
	})
	return result, err
}

func (service *Service) agentOwnerProjectionFromTx(
	ctx context.Context,
	tx domainrepo.Transaction,
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
	result.InstructionSelection = OwnerSafeSelection{StableSelector: spec.OwnerInstructionSelector,
		Version: spec.InstructionSetVersion,
		SHA256:  spec.InstructionSetSHA256, Status: OwnerProjectionStale}
	instruction, instructionErr := tx.Get(ctx, principal.OrganizationID, principal.ProjectID, spec.InstructionSetID)
	if instructionErr == nil {
		instructionSpec, instructionOK := instruction.Spec.(entity.InstructionSetSpec)
		instructionSHA, digestErr := entity.ProjectionSHA256(instruction)
		if !instructionOK || digestErr != nil || instruction.OwnerActorID != principal.ActorID {
			return AgentOwnerProjection{}, errs.ErrStateConflict
		}
		if result.InstructionSelection.StableSelector == "" {
			result.InstructionSelection.StableSelector = instructionSpec.StableKey
		}
		result.InstructionSelection.DisplayName = instruction.Name
		result.InstructionSelection.MaskedStatus = instructionSpec.VersionState
		if instruction.State != enum.StateActive || instructionSpec.VersionState != "PUBLISHED" ||
			result.InstructionSelection.StableSelector != instructionSpec.StableKey {
			result.InstructionSelection.Status = OwnerProjectionIneligible
		} else if instruction.Version == spec.InstructionSetVersion &&
			instructionSHA == spec.InstructionSetSHA256 {
			result.InstructionSelection.Status = OwnerProjectionPresent
		}
	} else if !errors.Is(instructionErr, errs.ErrNotFound) {
		return AgentOwnerProjection{}, instructionErr
	}
	result.ProviderPoolSelection = OwnerSafeSelection{StableSelector: spec.OwnerProviderPoolSelector,
		Version: spec.ProviderPoolVersion,
		SHA256:  spec.ProviderPoolSHA256, Status: OwnerProjectionStale}
	pool, poolErr := tx.Get(ctx, principal.OrganizationID, principal.ProjectID, spec.ProviderPoolID)
	if poolErr == nil {
		poolSpec, poolOK := pool.Spec.(entity.ProviderPoolSpec)
		poolSHA, digestErr := entity.ProjectionSHA256(pool)
		if !poolOK || digestErr != nil || pool.OwnerActorID != principal.ActorID {
			return AgentOwnerProjection{}, errs.ErrStateConflict
		}
		if result.ProviderPoolSelection.StableSelector == "" {
			result.ProviderPoolSelection.StableSelector = poolSpec.StableKey
		}
		result.ProviderPoolSelection.DisplayName = pool.Name
		result.ProviderPoolSelection.MaskedStatus = ownerProviderPoolStatus(poolSpec)
		if pool.State != enum.StateActive || result.ProviderPoolSelection.StableSelector != poolSpec.StableKey {
			result.ProviderPoolSelection.Status = OwnerProjectionIneligible
		} else if pool.Version == spec.ProviderPoolVersion && poolSHA == spec.ProviderPoolSHA256 {
			result.ProviderPoolSelection.Status = OwnerProjectionPresent
		}
	} else if !errors.Is(poolErr, errs.ErrNotFound) {
		return AgentOwnerProjection{}, poolErr
	}
	result.RuntimeSelection = AgentRuntimeSelectionProjection{
		RuntimeProfileVersion: spec.RuntimeProfileVersion, RuntimeProfileSHA256: spec.RuntimeProfileSHA256,
		RoleDefinitionVersion: spec.RoleDefinitionVersion, RoleDefinitionSHA256: spec.RoleDefinitionSHA256,
		SelectionKey: spec.OwnerRoleSelector, Status: OwnerProjectionStale,
	}
	role, err := tx.Get(ctx, principal.OrganizationID, principal.ProjectID, spec.RoleDefinitionID)
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
	if result.RuntimeSelection.SelectionKey == "" {
		result.RuntimeSelection.SelectionKey = roleSpec.StableKey
	}
	result.RuntimeSelection.DisplayName = role.Name
	if role.State != enum.StateActive || result.RuntimeSelection.SelectionKey != roleSpec.StableKey {
		result.RuntimeSelection.Status = OwnerProjectionIneligible
		return result, nil
	}
	if roleSpec.RoleImageRecipeID == "" {
		return result, nil
	}
	recipe, err := tx.Get(ctx, principal.OrganizationID, principal.ProjectID, roleSpec.RoleImageRecipeID)
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

func ownerProviderPoolStatus(spec entity.ProviderPoolSpec) string {
	status := "AVAILABLE"
	for _, binding := range spec.Bindings {
		if !binding.Eligible || (binding.MaskedStatus != "AVAILABLE" && binding.MaskedStatus != "DEGRADED") {
			return "INELIGIBLE"
		}
		if binding.MaskedStatus == "DEGRADED" {
			status = "DEGRADED"
		}
	}
	return status
}

func (service *Service) GetAgentOwner(
	ctx context.Context,
	principal value.Principal,
	agentID string,
) (AgentOwnerProjection, error) {
	if err := authorize(principal, permissionRead); err != nil || value.ValidateID(agentID) != nil {
		if err != nil {
			return AgentOwnerProjection{}, err
		}
		return AgentOwnerProjection{}, errs.ErrInvalidInput
	}
	var result AgentOwnerProjection
	err := service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		agent, getErr := tx.Get(ctx, principal.OrganizationID, principal.ProjectID, agentID)
		if getErr != nil {
			return getErr
		}
		if agent.Kind != enum.KindAgent {
			return errs.ErrNotFound
		}
		result, getErr = service.agentOwnerProjectionFromTx(ctx, tx, principal, agent)
		return getErr
	})
	return result, err
}

func (service *Service) ListAgentOwners(
	ctx context.Context,
	principal value.Principal,
	states []enum.State,
	afterID string,
	limit int,
) ([]AgentOwnerProjection, string, error) {
	if err := authorize(principal, permissionList); err != nil {
		return nil, "", err
	}
	cursor, err := service.decodeOwnerListCursor(afterID, "AGENT_LIST")
	if err != nil {
		return nil, "", err
	}
	filter := query.ResourceFilter{OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
		ActorID: principal.ActorID, Kind: enum.KindAgent, States: states, AfterID: cursor.AfterID, Limit: limit}
	if filter.Validate() != nil {
		return nil, "", errs.ErrInvalidInput
	}
	var result []AgentOwnerProjection
	next := ""
	err = service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		ownerRead, ok := tx.(domainrepo.OwnerReadTransaction)
		if !ok {
			return errs.ErrInternal
		}
		snapshotSHA256, snapshotErr := ownerListSnapshot(ctx, tx, cursor.SnapshotSHA256)
		if snapshotErr != nil {
			return snapshotErr
		}
		items, listErr := ownerRead.ListOwnerResources(ctx, filter)
		if listErr != nil {
			return listErr
		}
		result = make([]AgentOwnerProjection, 0, len(items))
		for _, item := range items {
			projection, projectionErr := service.agentOwnerProjectionFromTx(ctx, tx, principal, item)
			if projectionErr != nil {
				return projectionErr
			}
			result = append(result, projection)
		}
		if len(items) == limit {
			next, listErr = service.encodeRunSnapshotCursor(runSnapshotCursor{Kind: "AGENT_LIST",
				AfterID: items[len(items)-1].ID, SnapshotSHA256: snapshotSHA256})
		}
		return listErr
	})
	return result, next, err
}

func (service *Service) ListAgentOwnerHistory(
	ctx context.Context,
	principal value.Principal,
	agentID string,
	beforeVersion uint64,
	limit int,
) ([]AgentOwnerHistoryProjection, string, error) {
	if err := authorize(principal, permissionRead); err != nil ||
		value.ValidateID(agentID) != nil || limit < 1 || limit > 100 {
		if err != nil {
			return nil, "", err
		}
		return nil, "", errs.ErrInvalidInput
	}
	var result []AgentOwnerHistoryProjection
	next := ""
	err := service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		current, getErr := tx.Get(ctx, principal.OrganizationID, principal.ProjectID, agentID)
		if getErr != nil || current.Kind != enum.KindAgent || current.OwnerActorID != principal.ActorID {
			if getErr != nil {
				return getErr
			}
			return errs.ErrNotFound
		}
		ownerRead, ok := tx.(domainrepo.OwnerReadTransaction)
		if !ok {
			return errs.ErrInternal
		}
		items, listErr := ownerRead.ListProtectedResourceHistorySnapshot(ctx, agentID, beforeVersion, limit)
		if listErr != nil {
			return listErr
		}
		result = make([]AgentOwnerHistoryProjection, 0, len(items))
		for _, item := range items {
			projection, projectionErr := service.agentOwnerProjectionFromTx(ctx, tx, principal, item.Resource)
			if projectionErr != nil {
				return projectionErr
			}
			result = append(result, AgentOwnerHistoryProjection{Projection: projection,
				Action: item.Action, SnapshotSHA256: item.SnapshotSHA256, OccurredAt: item.OccurredAt})
		}
		if len(items) == limit {
			next = fmt.Sprint(items[len(items)-1].Resource.Version)
		}
		return nil
	})
	return result, next, err
}

func (service *Service) GetOwnerConfigurationCatalog(
	ctx context.Context,
	principal value.Principal,
	afterID string,
	limit int,
) (OwnerConfigurationCatalog, error) {
	if err := authorize(principal, permissionList); err != nil {
		return OwnerConfigurationCatalog{}, err
	}
	cursor, err := service.decodeOwnerListCursor(afterID, "CATALOG_LIST")
	if err != nil {
		return OwnerConfigurationCatalog{}, err
	}
	filter := query.ResourceFilter{OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
		ActorID: principal.ActorID, Kind: enum.KindRoleDefinition, States: []enum.State{enum.StateActive},
		AfterID: cursor.AfterID, Limit: limit}
	if filter.Validate() != nil {
		return OwnerConfigurationCatalog{}, errs.ErrInvalidInput
	}
	result := OwnerConfigurationCatalog{}
	err = service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		ownerRead, ok := tx.(domainrepo.OwnerReadTransaction)
		if !ok {
			return errs.ErrInternal
		}
		snapshotSHA256, snapshotErr := ownerListSnapshot(ctx, tx, cursor.SnapshotSHA256)
		if snapshotErr != nil {
			return snapshotErr
		}
		roles, listErr := ownerRead.ListOwnerResources(ctx, filter)
		if listErr != nil {
			return listErr
		}
		for _, role := range roles {
			spec, ok := role.Spec.(entity.RoleDefinitionSpec)
			if !ok || role.OwnerActorID != principal.ActorID || spec.RoleImageRecipeID == "" {
				return errs.ErrStateConflict
			}
			digest, digestErr := entity.ProjectionSHA256(role)
			if digestErr != nil {
				return errs.ErrInternal
			}
			status := OwnerProjectionStale
			recipe, recipeErr := tx.Get(ctx, principal.OrganizationID, principal.ProjectID,
				spec.RoleImageRecipeID)
			if recipeErr != nil && !errors.Is(recipeErr, errs.ErrNotFound) {
				return recipeErr
			}
			if recipeErr == nil {
				if recipe.Kind != enum.KindRoleImageRecipe || recipe.OwnerActorID != principal.ActorID {
					return errs.ErrNotFound
				}
				if _, ok := recipe.Spec.(entity.RoleImageRecipeSpec); !ok {
					return errs.ErrStateConflict
				}
				recipeSHA, recipeSHAErr := entity.ProjectionSHA256(recipe)
				if recipeSHAErr != nil {
					return errs.ErrStateConflict
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
		if len(roles) == limit {
			result.NextPageToken, listErr = service.encodeRunSnapshotCursor(runSnapshotCursor{Kind: "CATALOG_LIST",
				AfterID: roles[len(roles)-1].ID, SnapshotSHA256: snapshotSHA256})
		}
		return listErr
	})
	if err != nil {
		return OwnerConfigurationCatalog{}, err
	}
	result.SchedulePresets, err = ownerSchedulePresets()
	if err != nil {
		return OwnerConfigurationCatalog{}, errs.ErrInternal
	}
	result.ScheduleDefaults, err = ownerScheduleDefaults()
	if err != nil {
		return OwnerConfigurationCatalog{}, errs.ErrInternal
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
	if input.Action == "create" {
		created, createErr := service.CreateScheduleFromOwnerSelections(ctx, CreateScheduleFromOwnerSelectionsInput{
			Principal: input.Principal, IdempotencyKey: input.IdempotencyKey, Name: input.Name,
			AgentStableKey: input.AgentStableKey, InstructionSetStableKey: input.InstructionSetStableKey,
			ProviderPoolStableKey: input.ProviderPoolStableKey, RoomStableKey: input.Selection.RoomStableKey,
			PromptArtifactName: input.Selection.Prompt.ArtifactName, PromptKind: input.Selection.Prompt.Kind,
			PromptInlineMarkdown: input.Selection.Prompt.InlineMarkdown, Spec: spec,
		})
		if createErr != nil {
			return OwnerScheduleProjection{}, createErr
		}
		readPrincipal := input.Principal
		readPrincipal.Permission = permissionRead
		return service.getOwnerScheduleAtVersion(ctx, readPrincipal, created.ID, created.Version)
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
		input.Selection.Prompt.Kind, input.Selection.Prompt.ArtifactName,
		hashString(input.Selection.Prompt.InlineMarkdown), input.Selection.Overrides, spec})
	if err != nil {
		return OwnerScheduleProjection{}, errs.ErrInvalidInput
	}
	prompt, err := service.prepareOwnerSchedulePrompt(ctx, ownerSchedulePromptPreparationInput{
		Principal: input.Principal, IdempotencyKey: input.IdempotencyKey, RequestSHA256: requestHash,
		Action: input.Action, ScheduleID: input.ScheduleID, ExpectedVersion: input.ExpectedVersion,
		AgentStableKey: input.AgentStableKey, InstructionSetStableKey: input.InstructionSetStableKey,
		ProviderPoolStableKey: input.ProviderPoolStableKey, RoomStableKey: input.Selection.RoomStableKey,
		SessionPolicy: spec.SessionPolicy,
	}, input.Selection.Prompt)
	if err != nil {
		return OwnerScheduleProjection{}, err
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
	readPrincipal := input.Principal
	readPrincipal.Permission = permissionRead
	return service.getOwnerScheduleAtVersion(ctx, readPrincipal, updated.ID, updated.Version)
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
	roomID, roomSHA := "", ""
	var roomVersion uint64
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
		roomVersion = room.Version
		roomSHA, roomErr = entity.ProjectionSHA256(room)
		if roomErr != nil {
			return entity.Resource{}, errs.ErrInternal
		}
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
	spec.OwnerAgentSelector, spec.OwnerInstructionSelector = input.AgentStableKey, input.InstructionSetStableKey
	spec.OwnerProviderPoolSelector = input.ProviderPoolStableKey
	spec.OwnerRoomSelector, spec.OwnerRoomVersion, spec.OwnerRoomSHA256 =
		input.Selection.RoomStableKey, roomVersion, roomSHA
	if prompt.Kind == "SELECTOR" {
		spec.OwnerPromptSelector = prompt.ArtifactName
	}
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
	if err := service.appendMutationRecords(ctx, tx, input.Principal,
		"manage_owner_schedule_update", updated); err != nil {
		return entity.Resource{}, err
	}
	if prompt.PreparationKeyHash != "" {
		preparations, ok := tx.(domainrepo.SchedulePromptPreparationTransaction)
		if !ok {
			return entity.Resource{}, errs.ErrInternal
		}
		if err := preparations.ConsumeSchedulePromptPreparation(ctx,
			prompt.PreparationKeyHash, updated.ID, prompt.PreparationGeneration,
			promptSpec.SHA256, updated.Version, now); err != nil {
			return entity.Resource{}, err
		}
	}
	return updated, nil
}

func (service *Service) GetOwnerSchedule(
	ctx context.Context,
	principal value.Principal,
	id string,
) (OwnerScheduleProjection, error) {
	return service.getOwnerScheduleAtVersion(ctx, principal, id, 0)
}

// GetOwnerScheduleAtVersion не смешивает mutation result с более новым owner snapshot.
func (service *Service) GetOwnerScheduleAtVersion(
	ctx context.Context,
	principal value.Principal,
	id string,
	expectedVersion uint64,
) (OwnerScheduleProjection, error) {
	return service.getOwnerScheduleAtVersion(ctx, principal, id, expectedVersion)
}

func (service *Service) getOwnerScheduleAtVersion(
	ctx context.Context,
	principal value.Principal,
	id string,
	expectedVersion uint64,
) (OwnerScheduleProjection, error) {
	if err := authorize(principal, permissionRead); err != nil || value.ValidateID(id) != nil {
		if err != nil {
			return OwnerScheduleProjection{}, err
		}
		return OwnerScheduleProjection{}, errs.ErrInvalidInput
	}
	var projection OwnerScheduleProjection
	err := service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		item, getErr := tx.Get(ctx, principal.OrganizationID, principal.ProjectID, id)
		if getErr != nil {
			return getErr
		}
		if expectedVersion != 0 && item.Version != expectedVersion {
			return errs.ErrVersionMismatch
		}
		projection, getErr = service.scheduleOwnerProjectionFromTx(ctx, tx, principal, item)
		return getErr
	})
	if err != nil {
		return OwnerScheduleProjection{}, err
	}
	return service.hydrateOwnerSchedulePrompt(ctx, projection)
}

func (service *Service) ListOwnerSchedules(
	ctx context.Context,
	principal value.Principal,
	states []enum.State,
	afterID string,
	limit int,
) ([]OwnerScheduleProjection, string, error) {
	if err := authorize(principal, permissionList); err != nil {
		return nil, "", err
	}
	cursor, err := service.decodeOwnerListCursor(afterID, "SCHEDULE_LIST")
	if err != nil {
		return nil, "", err
	}
	filter := query.ResourceFilter{OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
		ActorID: principal.ActorID, Kind: enum.KindSchedule, States: states, AfterID: cursor.AfterID, Limit: limit}
	if filter.Validate() != nil {
		return nil, "", errs.ErrInvalidInput
	}
	var result []OwnerScheduleProjection
	next := ""
	err = service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		ownerRead, ok := tx.(domainrepo.OwnerReadTransaction)
		if !ok {
			return errs.ErrInternal
		}
		snapshotSHA256, snapshotErr := ownerListSnapshot(ctx, tx, cursor.SnapshotSHA256)
		if snapshotErr != nil {
			return snapshotErr
		}
		resources, listErr := ownerRead.ListOwnerResources(ctx, filter)
		if listErr != nil {
			return listErr
		}
		result = make([]OwnerScheduleProjection, 0, len(resources))
		for _, item := range resources {
			projection, projectionErr := service.scheduleOwnerProjectionFromTx(ctx, tx, principal, item)
			if projectionErr != nil {
				return projectionErr
			}
			result = append(result, projection)
		}
		if len(resources) == limit {
			next, listErr = service.encodeRunSnapshotCursor(runSnapshotCursor{Kind: "SCHEDULE_LIST",
				AfterID: resources[len(resources)-1].ID, SnapshotSHA256: snapshotSHA256})
		}
		return listErr
	})
	if err != nil {
		return nil, "", err
	}
	for index := range result {
		result[index], err = service.hydrateOwnerSchedulePrompt(ctx, result[index])
		if err != nil {
			return nil, "", err
		}
	}
	return result, next, nil
}

func (service *Service) scheduleOwnerProjectionFromTx(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	item entity.Resource,
) (OwnerScheduleProjection, error) {
	projection, ok := scheduleResourceProjection(item)
	if !ok || item.Kind != enum.KindSchedule || item.OwnerActorID != principal.ActorID {
		return OwnerScheduleProjection{}, errs.ErrNotFound
	}
	spec := item.Spec.(entity.ScheduleSpec)
	var err error
	projection.AgentSelection, err = ownerScheduleSelection(ctx, tx, principal, enum.KindAgent,
		spec.AgentID, spec.OwnerAgentSelector, spec.AgentVersion, spec.AgentSHA256)
	if err != nil {
		return OwnerScheduleProjection{}, err
	}
	projection.InstructionSelection, err = ownerScheduleSelection(ctx, tx, principal, enum.KindInstructionSet,
		spec.InstructionSetID, spec.OwnerInstructionSelector, spec.InstructionSetVersion, spec.InstructionSetSHA256)
	if err != nil {
		return OwnerScheduleProjection{}, err
	}
	projection.ProviderPoolSelection, err = ownerScheduleSelection(ctx, tx, principal, enum.KindProviderPool,
		spec.ProviderPoolID, spec.OwnerProviderPoolSelector, spec.ProviderPoolVersion, spec.ProviderPoolSHA256)
	if err != nil {
		return OwnerScheduleProjection{}, err
	}
	if spec.RoomID != "" {
		projection.RoomSelection, err = ownerScheduleSelection(ctx, tx, principal, enum.KindChat,
			spec.RoomID, spec.OwnerRoomSelector, spec.OwnerRoomVersion, spec.OwnerRoomSHA256)
		if err != nil {
			return OwnerScheduleProjection{}, err
		}
	} else {
		projection.RoomSelection = OwnerSafeSelection{Status: OwnerProjectionUnavailable}
	}
	promptResource, promptErr := tx.Get(ctx, principal.OrganizationID, principal.ProjectID, spec.PromptArtifactID)
	if promptErr != nil {
		if errors.Is(promptErr, errs.ErrNotFound) {
			projection.Prompt = OwnerSchedulePromptProjection{Kind: spec.PromptIntentKind,
				ArtifactSelector: spec.OwnerPromptSelector, DisplayName: spec.PromptDisplay,
				SHA256: spec.PromptSHA256, Version: spec.PromptArtifactVersion, Status: OwnerProjectionStale}
			return projection, nil
		}
		return OwnerScheduleProjection{}, promptErr
	}
	promptSpec, promptOK := promptResource.Spec.(entity.ArtifactSpec)
	if !promptOK || promptResource.Kind != enum.KindArtifact || promptResource.OwnerActorID != principal.ActorID ||
		promptSpec.MediaType != "text/markdown" || promptSpec.Direction != "INPUT" {
		return OwnerScheduleProjection{}, errs.ErrStateConflict
	}
	promptStatus := OwnerProjectionStale
	if promptResource.State != enum.StateActive || promptSpec.ScanStatus != "CLEAN" {
		promptStatus = OwnerProjectionIneligible
	} else if promptResource.Version == spec.PromptArtifactVersion && promptSpec.SHA256 == spec.PromptSHA256 {
		promptStatus = OwnerProjectionPresent
	}
	projection.Prompt = OwnerSchedulePromptProjection{Kind: spec.PromptIntentKind,
		ArtifactSelector: spec.OwnerPromptSelector, DisplayName: promptResource.Name,
		SHA256: spec.PromptSHA256, Version: spec.PromptArtifactVersion, Status: promptStatus}
	if spec.PromptIntentKind == "INLINE" && promptStatus == OwnerProjectionPresent {
		parsed, parseErr := url.Parse(promptSpec.StorageRef)
		if parseErr != nil || parsed.Query().Get("versionId") == "" {
			return OwnerScheduleProjection{}, errs.ErrStateConflict
		}
		projection.Prompt.Object = domainobjectstore.Object{Reference: promptSpec.StorageRef,
			VersionID: parsed.Query().Get("versionId"), SHA256: promptSpec.SHA256,
			Size: promptSpec.SizeBytes, MediaType: promptSpec.MediaType}
	}
	return projection, nil
}

func ownerScheduleSelection(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	kind enum.Kind,
	id, stableSelector string,
	version uint64,
	digest string,
) (OwnerSafeSelection, error) {
	selection := OwnerSafeSelection{StableSelector: stableSelector, Version: version, SHA256: digest,
		Status: OwnerProjectionStale}
	item, err := tx.Get(ctx, principal.OrganizationID, principal.ProjectID, id)
	if errors.Is(err, errs.ErrNotFound) {
		return selection, nil
	}
	if err != nil {
		return OwnerSafeSelection{}, err
	}
	if item.Kind != kind || item.OwnerActorID != principal.ActorID {
		return OwnerSafeSelection{}, errs.ErrNotFound
	}
	itemDigest, err := entity.ProjectionSHA256(item)
	if err != nil {
		return OwnerSafeSelection{}, errs.ErrStateConflict
	}
	selection.DisplayName = item.Name
	switch spec := item.Spec.(type) {
	case entity.AgentSpec:
		if spec.StableKey != stableSelector || !spec.Enabled {
			selection.Status = OwnerProjectionIneligible
		}
	case entity.InstructionSetSpec:
		if spec.StableKey != stableSelector || spec.VersionState != "PUBLISHED" {
			selection.Status = OwnerProjectionIneligible
		}
		selection.MaskedStatus = spec.VersionState
	case entity.ProviderPoolSpec:
		if spec.StableKey != stableSelector {
			selection.Status = OwnerProjectionIneligible
		}
		selection.MaskedStatus = ownerProviderPoolStatus(spec)
	case entity.ChatSpec:
		if spec.StableKey != stableSelector {
			selection.Status = OwnerProjectionIneligible
		}
	default:
		return OwnerSafeSelection{}, errs.ErrStateConflict
	}
	if item.State != enum.StateActive {
		selection.Status = OwnerProjectionIneligible
	} else if selection.Status != OwnerProjectionIneligible && item.Version == version && itemDigest == digest {
		selection.Status = OwnerProjectionPresent
	}
	return selection, nil
}

func (service *Service) hydrateOwnerSchedulePrompt(
	ctx context.Context,
	projection OwnerScheduleProjection,
) (OwnerScheduleProjection, error) {
	if projection.Prompt.Kind != "INLINE" || projection.Prompt.Status != OwnerProjectionPresent {
		return projection, nil
	}
	if service.instructionObjects == nil {
		return OwnerScheduleProjection{}, errs.ErrUnavailable
	}
	raw, err := service.instructionObjects.Get(ctx, projection.Prompt.Object)
	if err != nil || len(raw) == 0 || len(raw) > 262144 || !utf8.Valid(raw) {
		return OwnerScheduleProjection{}, errs.ErrUnavailable
	}
	projection.Prompt.InlineMarkdown = string(raw)
	projection.Prompt.Object = domainobjectstore.Object{}
	return projection, nil
}

func (service *Service) runOwnerProjectionFromTx(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	detail RunDetailResult,
	decision runActionDecision,
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
		Initiator:     OwnerDisplayValue{Status: OwnerProjectionUnavailable},
		Agent:         OwnerDisplayValue{Status: OwnerProjectionUnavailable},
		Role:          OwnerDisplayValue{Status: OwnerProjectionUnavailable},
		Model:         OwnerDisplayValue{Status: OwnerProjectionUnavailable},
		Provider:      OwnerDisplayValue{Status: OwnerProjectionUnavailable},
		NextActions:   decision.actions()}
	if result.Attempt == 0 {
		result.Attempt = spec.RootAttempt
	}
	if !result.StartedAt.IsZero() && !result.UpdatedAt.Before(result.StartedAt) {
		result.Duration = result.UpdatedAt.Sub(result.StartedAt)
	}
	project, err := tx.Get(ctx, principal.OrganizationID, principal.ProjectID, principal.ProjectID)
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
	if spec.RootInitiatorActorID == principal.ActorID {
		result.Initiator = OwnerDisplayValue{Status: OwnerProjectionPresent, Value: "Владелец"}
	}
	if detail.Runtime != nil {
		if !validRuntimeExecutionState(detail.Runtime.State) {
			return RunOwnerProjection{}, errs.ErrStateConflict
		}
		result.RuntimeStatus = OwnerDisplayValue{Status: OwnerProjectionPresent, Value: detail.Runtime.State}
	}
	revisionSpec, revisionOK := detail.RuntimeRevision.Spec.(entity.RuntimeRevisionSpec)
	if !revisionOK || detail.RuntimeRevision.Kind != enum.KindRuntimeRevision ||
		detail.RuntimeRevision.OwnerActorID != principal.ActorID {
		return RunOwnerProjection{}, errs.ErrStateConflict
	}
	if validOwnerRunDisplay(revisionSpec.CodexModel) {
		result.Model = OwnerDisplayValue{Status: OwnerProjectionPresent, Value: revisionSpec.CodexModel}
	}
	if revisionSpec.AgentID != "" {
		result.Agent, err = ownerPinnedRunDisplay(ctx, tx, principal, enum.KindAgent,
			revisionSpec.AgentID, revisionSpec.AgentVersion, revisionSpec.AgentSHA256, false)
		if err != nil {
			return RunOwnerProjection{}, err
		}
	}
	if revisionSpec.RoleDefinitionID != "" {
		result.Role, err = ownerPinnedRunDisplay(ctx, tx, principal, enum.KindRoleDefinition,
			revisionSpec.RoleDefinitionID, revisionSpec.RoleDefinitionVersion, revisionSpec.RoleDefinitionSHA256, false)
		if err != nil {
			return RunOwnerProjection{}, err
		}
	}
	if revisionSpec.ProviderPoolID != "" {
		result.Provider, err = ownerPinnedRunDisplay(ctx, tx, principal, enum.KindProviderPool,
			revisionSpec.ProviderPoolID, revisionSpec.ProviderPoolVersion, revisionSpec.ProviderPoolSHA256, true)
		if err != nil {
			return RunOwnerProjection{}, err
		}
	}
	return result, nil
}

func validOwnerRunDisplay(value string) bool {
	return value != "" && len(value) <= 128 && value == strings.TrimSpace(value) &&
		!strings.ContainsAny(value, "\x00\r\n")
}

func ownerPinnedRunDisplay(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	kind enum.Kind,
	id string,
	version uint64,
	digest string,
	maskedProvider bool,
) (OwnerDisplayValue, error) {
	item, err := tx.Get(ctx, principal.OrganizationID, principal.ProjectID, id)
	if errors.Is(err, errs.ErrNotFound) {
		return OwnerDisplayValue{Status: OwnerProjectionStale}, nil
	}
	if err != nil {
		return OwnerDisplayValue{}, err
	}
	if item.Kind != kind || item.OwnerActorID != principal.ActorID {
		return OwnerDisplayValue{}, errs.ErrNotFound
	}
	projectionSHA, err := entity.ProjectionSHA256(item)
	if err != nil || !validOwnerRunDisplay(item.Name) {
		return OwnerDisplayValue{}, errs.ErrStateConflict
	}
	status := OwnerProjectionStale
	if item.State != enum.StateActive {
		status = OwnerProjectionIneligible
	} else if item.Version == version && projectionSHA == digest {
		status = OwnerProjectionPresent
	}
	display := item.Name
	if maskedProvider {
		pool, ok := item.Spec.(entity.ProviderPoolSpec)
		if !ok {
			return OwnerDisplayValue{}, errs.ErrStateConflict
		}
		display += " · " + ownerProviderPoolStatus(pool)
	}
	return OwnerDisplayValue{Status: status, Value: display}, nil
}

func (service *Service) RunOwnerProjection(
	ctx context.Context,
	principal value.Principal,
	detail RunDetailResult,
) (RunOwnerProjection, error) {
	if detail.Projection.RunRef != detail.ProcessRun.ID || detail.Projection.Version != detail.ProcessRun.Version ||
		detail.ProcessRun.OwnerActorID != principal.ActorID {
		return RunOwnerProjection{}, errs.ErrStateConflict
	}
	return detail.Projection, nil
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
	unavailable := OwnerDisplayValue{Status: OwnerProjectionUnavailable}
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
		parentRef := ""
		if node.ParentProcessRunID != "" {
			parentRef = ownerOpaqueRef("process", node.ParentProcessRunID)
		}
		result = append(result, RunLineageProjection{NodeRef: refs[node.ID], ParentRef: parentRef,
			Kind: "PROCESS", State: node.State, Version: node.Version, DisplayName: node.DisplayName,
			CreatedAt: node.OccurredAt, UpdatedAt: node.UpdatedAt,
			Agent: unavailable, Role: unavailable, Model: unavailable, Provider: unavailable})
	}
	for _, node := range lineage.Attempts {
		if !validRunLineageState(node.State) {
			return nil, errs.ErrStateConflict
		}
		result = append(result, RunLineageProjection{NodeRef: refs[node.ID],
			ParentRef: ownerOpaqueRef("process", node.ProcessRunID),
			Kind:      "ATTEMPT", State: node.State, Version: node.Version, Attempt: node.Attempt,
			DisplayName: node.DisplayName,
			CreatedAt:   node.OccurredAt, UpdatedAt: node.UpdatedAt,
			Agent: unavailable, Role: unavailable, Model: unavailable, Provider: unavailable})
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
	if err := authorize(principal, permissionList); err != nil {
		return nil, "", err
	}
	cursor, err := service.decodeOwnerListCursor(afterID, "RUN_LIST")
	if err != nil {
		return nil, "", err
	}
	filter := query.ResourceFilter{OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
		ActorID: principal.ActorID, Kind: enum.KindProcessRun, States: states, AfterID: cursor.AfterID, Limit: limit}
	if filter.Validate() != nil {
		return nil, "", errs.ErrInvalidInput
	}
	var result []RunOwnerProjection
	next := ""
	err = service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		ownerRead, ok := tx.(domainrepo.OwnerReadTransaction)
		if !ok {
			return errs.ErrInternal
		}
		snapshotSHA256, snapshotErr := ownerListSnapshot(ctx, tx, cursor.SnapshotSHA256)
		if snapshotErr != nil {
			return snapshotErr
		}
		resources, listErr := ownerRead.ListOwnerResources(ctx, filter)
		if listErr != nil {
			return listErr
		}
		result = make([]RunOwnerProjection, 0, len(resources))
		for _, process := range resources {
			graph, graphErr := service.lockOwnerGraphByProcess(ctx, tx, principal, process.ID)
			if graphErr != nil {
				return graphErr
			}
			if graph.Process.Version != process.Version {
				return errs.ErrVersionMismatch
			}
			processSpec, specOK := process.Spec.(entity.ProcessRunSpec)
			if !specOK {
				return errs.ErrStateConflict
			}
			tuple, tupleErr := currentExecution(processSpec)
			if tupleErr != nil {
				return tupleErr
			}
			revision, revisionErr := tx.Get(ctx, principal.OrganizationID, principal.ProjectID,
				tuple.RuntimeRevisionID)
			if revisionErr != nil || revision.Version != tuple.RuntimeRevisionVersion ||
				revision.Kind != enum.KindRuntimeRevision || revision.OwnerActorID != principal.ActorID {
				if revisionErr != nil {
					return revisionErr
				}
				return errs.ErrStateConflict
			}
			decision, decisionErr := service.decideRunActionsLocked(ctx, tx, principal, graph)
			if decisionErr != nil {
				return decisionErr
			}
			projection, projectionErr := service.runOwnerProjectionFromTx(ctx, tx, principal,
				RunDetailResult{ProcessRun: graph.Process, Session: graph.Session, Turn: graph.Turn,
					RuntimeRevision: revision, Runtime: graph.Runtime}, decision)
			if projectionErr != nil {
				return projectionErr
			}
			result = append(result, projection)
		}
		if len(resources) == limit {
			next, listErr = service.encodeRunSnapshotCursor(runSnapshotCursor{Kind: "RUN_LIST",
				AfterID: resources[len(resources)-1].ID, SnapshotSHA256: snapshotSHA256})
		}
		return listErr
	})
	return result, next, err
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
	var result RuntimeIncidentOwnerProjection
	err := service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		protected, ok := tx.(domainrepo.ProtectedTransaction)
		if !ok {
			return errs.ErrInternal
		}
		current, getErr := protected.GetRuntimeIncidentForUpdate(ctx, incident.ID)
		if getErr != nil {
			return getErr
		}
		if current.Version != incident.Version || current.ExecutionFence != incident.ExecutionFence {
			return errs.ErrVersionMismatch
		}
		var projectionErr error
		result, projectionErr = service.runtimeIncidentOwnerProjectionFromTx(ctx, tx, principal, current)
		return projectionErr
	})
	return result, err
}

func (service *Service) GetRuntimeIncidentOwner(
	ctx context.Context,
	principal value.Principal,
	incidentID string,
) (RuntimeIncidentOwnerProjection, error) {
	if err := authorize(principal, permissionRuntimeIncidentRead); err != nil || value.ValidateID(incidentID) != nil {
		if err != nil {
			return RuntimeIncidentOwnerProjection{}, err
		}
		return RuntimeIncidentOwnerProjection{}, errs.ErrInvalidInput
	}
	var result RuntimeIncidentOwnerProjection
	err := service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		protected, ok := tx.(domainrepo.ProtectedTransaction)
		if !ok {
			return errs.ErrInternal
		}
		incident, getErr := protected.GetRuntimeIncidentForUpdate(ctx, incidentID)
		if getErr != nil {
			return getErr
		}
		result, getErr = service.runtimeIncidentOwnerProjectionFromTx(ctx, tx, principal, incident)
		return getErr
	})
	return result, err
}

func (service *Service) ListRuntimeIncidentOwners(
	ctx context.Context,
	principal value.Principal,
	afterID string,
	limit int,
) ([]RuntimeIncidentOwnerProjection, string, error) {
	if err := authorize(principal, permissionRuntimeIncidentRead); err != nil {
		return nil, "", err
	}
	cursor, err := service.decodeOwnerListCursor(afterID, "INCIDENT_LIST")
	if err != nil {
		return nil, "", err
	}
	filter := query.RuntimeIncidentFilter{OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
		ActorID: principal.ActorID, AfterID: cursor.AfterID, Limit: limit}
	if filter.Validate() != nil {
		return nil, "", errs.ErrInvalidInput
	}
	var result []RuntimeIncidentOwnerProjection
	next := ""
	err = service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		ownerRead, ok := tx.(domainrepo.OwnerReadTransaction)
		if !ok {
			return errs.ErrInternal
		}
		snapshotSHA256, snapshotErr := ownerListSnapshot(ctx, tx, cursor.SnapshotSHA256)
		if snapshotErr != nil {
			return snapshotErr
		}
		incidents, listErr := ownerRead.ListRuntimeIncidentsSnapshot(ctx, filter)
		if listErr != nil {
			return listErr
		}
		result = make([]RuntimeIncidentOwnerProjection, 0, len(incidents))
		for _, incident := range incidents {
			projection, projectionErr := service.runtimeIncidentOwnerProjectionFromTx(ctx, tx, principal, incident)
			if projectionErr != nil {
				return projectionErr
			}
			result = append(result, projection)
		}
		if len(incidents) == limit {
			next, listErr = service.encodeRunSnapshotCursor(runSnapshotCursor{Kind: "INCIDENT_LIST",
				AfterID: incidents[len(incidents)-1].ID, SnapshotSHA256: snapshotSHA256})
		}
		return listErr
	})
	return result, next, err
}

func (service *Service) ListRuntimeIncidentOwnerHistory(
	ctx context.Context,
	principal value.Principal,
	incidentID string,
	beforeVersion uint64,
	limit int,
) (RuntimeIncidentOwnerHistoryPage, error) {
	if err := authorize(principal, permissionRuntimeIncidentRead); err != nil || value.ValidateID(incidentID) != nil ||
		limit < 1 || limit > 100 {
		if err != nil {
			return RuntimeIncidentOwnerHistoryPage{}, err
		}
		return RuntimeIncidentOwnerHistoryPage{}, errs.ErrInvalidInput
	}
	var result RuntimeIncidentOwnerHistoryPage
	err := service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		protected, ok := tx.(domainrepo.ProtectedTransaction)
		if !ok {
			return errs.ErrInternal
		}
		incident, getErr := protected.GetRuntimeIncidentForUpdate(ctx, incidentID)
		if getErr != nil {
			return getErr
		}
		result.Current, getErr = service.runtimeIncidentOwnerProjectionFromTx(ctx, tx, principal, incident)
		if getErr != nil {
			return getErr
		}
		ownerRead, ok := tx.(domainrepo.OwnerReadTransaction)
		if !ok {
			return errs.ErrInternal
		}
		result.Entries, getErr = ownerRead.ListRuntimeIncidentHistorySnapshot(ctx, incidentID, beforeVersion, limit)
		if getErr != nil {
			return getErr
		}
		if len(result.Entries) == limit {
			result.NextPageToken = fmt.Sprint(result.Entries[len(result.Entries)-1].Version)
		}
		return nil
	})
	return result, err
}

func (service *Service) runtimeIncidentOwnerProjectionFromTx(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	incident domainrepo.RuntimeIncident,
) (RuntimeIncidentOwnerProjection, error) {
	severity, impact, knownKind := incidentSeverity(incident.Kind)
	if !knownKind || !slices.Contains([]string{"OPEN", "ACKNOWLEDGED", "RETRYING", "RELEASED", "CLOSED"}, incident.State) {
		return RuntimeIncidentOwnerProjection{}, errs.ErrStateConflict
	}
	result := RuntimeIncidentOwnerProjection{IncidentRef: incident.ID,
		Version: incident.Version, ExecutionFence: incident.ExecutionFence, Kind: incident.Kind, State: incident.State,
		Workspace: OwnerDisplayValue{Status: OwnerProjectionUnavailable},
		Run:       OwnerDisplayValue{Status: OwnerProjectionUnavailable}, RunbookURL: runtimeIncidentRunbookURL,
		OccurredAt: incident.OccurredAt, UpdatedAt: incident.UpdatedAt,
		SafeCorrelation: hashString(incident.ID + "\x00" + fmt.Sprint(incident.Version))[:16]}
	result.Severity, result.Impact = severity, impact
	result.DiagnosticSummary = result.Impact
	execution, executionErr := tx.GetRuntimeExecutionForUpdate(ctx, incident.ExecutionID)
	if executionErr != nil {
		return RuntimeIncidentOwnerProjection{}, executionErr
	}
	process, processErr := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, execution.ProcessID)
	if processErr != nil {
		return RuntimeIncidentOwnerProjection{}, processErr
	}
	if process.Kind != enum.KindProcessRun || process.OwnerActorID != principal.ActorID {
		return RuntimeIncidentOwnerProjection{}, errs.ErrNotFound
	}
	result.Run = OwnerDisplayValue{Status: OwnerProjectionPresent, Value: process.Name}
	spec, ok := process.Spec.(entity.ProcessRunSpec)
	if !ok || !validRuntimeExecutionState(execution.State) {
		return RuntimeIncidentOwnerProjection{}, errs.ErrStateConflict
	}
	current := spec.CurrentTurnID == execution.TurnID && spec.CurrentAttempt == execution.Attempt &&
		incident.ExecutionFence == execution.Fence
	result.NextActions = runtimeIncidentNextActions(incident.State, execution.State, current)
	project, projectErr := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, principal.ProjectID)
	if projectErr == nil && project.Kind == enum.KindProject && project.OwnerActorID == principal.ActorID {
		result.Workspace = OwnerDisplayValue{Status: OwnerProjectionPresent, Value: project.Name}
	} else if projectErr != nil && !errors.Is(projectErr, errs.ErrNotFound) {
		return RuntimeIncidentOwnerProjection{}, projectErr
	}
	return result, nil
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
	var result WorkspaceRestoreOwnerProjection
	err := service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		current, getErr := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, resource.ID)
		if getErr != nil {
			return getErr
		}
		if current.Version != resource.Version {
			return errs.ErrVersionMismatch
		}
		result, getErr = service.workspaceRestoreOwnerProjectionFromTx(ctx, tx, principal, current)
		return getErr
	})
	return result, err
}

func (service *Service) workspaceRestoreOwnerProjectionFromTx(
	ctx context.Context,
	tx domainrepo.Transaction,
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
	now, timeErr := tx.CurrentTime(ctx)
	if timeErr != nil {
		return WorkspaceRestoreOwnerProjection{}, timeErr
	}
	backup, backupErr := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, spec.BackupID)
	if errors.Is(backupErr, errs.ErrNotFound) {
		result.NextActions = workspaceRestoreNextActions(resource, entity.Resource{}, now)
		return result, nil
	}
	if backupErr != nil {
		return WorkspaceRestoreOwnerProjection{}, backupErr
	}
	if backup.OwnerActorID != principal.ActorID {
		return WorkspaceRestoreOwnerProjection{}, errs.ErrNotFound
	}
	result.NextActions = workspaceRestoreNextActions(resource, backup, now)
	return result, nil
}

func (service *Service) GetWorkspaceRestoreOwner(
	ctx context.Context,
	principal value.Principal,
	restoreID string,
) (WorkspaceRestoreOwnerProjection, error) {
	if err := authorize(principal, permissionRead); err != nil || value.ValidateID(restoreID) != nil {
		if err != nil {
			return WorkspaceRestoreOwnerProjection{}, err
		}
		return WorkspaceRestoreOwnerProjection{}, errs.ErrInvalidInput
	}
	var result WorkspaceRestoreOwnerProjection
	err := service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		item, getErr := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, restoreID)
		if getErr != nil {
			return getErr
		}
		result, getErr = service.workspaceRestoreOwnerProjectionFromTx(ctx, tx, principal, item)
		return getErr
	})
	return result, err
}

func (service *Service) ListWorkspaceRestoreOwners(
	ctx context.Context,
	principal value.Principal,
	backupID string,
	states []enum.State,
	afterID string,
	limit int,
) ([]WorkspaceRestoreOwnerProjection, string, error) {
	if err := authorize(principal, permissionList); err != nil {
		return nil, "", err
	}
	if backupID != "" && value.ValidateID(backupID) != nil {
		return nil, "", errs.ErrInvalidInput
	}
	cursor, err := service.decodeOwnerListCursor(afterID, "RESTORE_LIST")
	if err != nil {
		return nil, "", err
	}
	filter := query.ResourceFilter{OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
		ActorID: principal.ActorID, Kind: enum.KindWorkspaceRestore, States: states, BackupID: backupID,
		AfterID: cursor.AfterID, Limit: limit}
	if filter.Validate() != nil {
		return nil, "", errs.ErrInvalidInput
	}
	var result []WorkspaceRestoreOwnerProjection
	next := ""
	err = service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		ownerRead, ok := tx.(domainrepo.OwnerReadTransaction)
		if !ok {
			return errs.ErrInternal
		}
		snapshotSHA256, snapshotErr := ownerListSnapshot(ctx, tx, cursor.SnapshotSHA256)
		if snapshotErr != nil {
			return snapshotErr
		}
		items, listErr := ownerRead.ListOwnerResources(ctx, filter)
		if listErr != nil {
			return listErr
		}
		result = make([]WorkspaceRestoreOwnerProjection, 0, len(items))
		for _, item := range items {
			projection, projectionErr := service.workspaceRestoreOwnerProjectionFromTx(ctx, tx, principal, item)
			if projectionErr != nil {
				return projectionErr
			}
			result = append(result, projection)
		}
		if len(items) == limit {
			next, listErr = service.encodeRunSnapshotCursor(runSnapshotCursor{Kind: "RESTORE_LIST",
				AfterID: items[len(items)-1].ID, SnapshotSHA256: snapshotSHA256})
		}
		return listErr
	})
	return result, next, err
}
