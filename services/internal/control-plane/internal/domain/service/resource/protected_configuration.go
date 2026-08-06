package resource

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

var protectedConfigurationActions = map[enum.Kind]map[string]struct{}{
	enum.KindRoleDefinition:    {"create": {}, "update": {}, "archive": {}, "delete": {}},
	enum.KindAgent:             {"create": {}, "update": {}, "archive": {}, "delete": {}},
	enum.KindAgentAssignment:   {"assign": {}, "unassign": {}},
	enum.KindInstructionSet:    {"create": {}, "update": {}, "validate": {}, "publish": {}, "rollback": {}, "detach": {}, "copy": {}, "archive": {}, "delete": {}},
	enum.KindProviderReference: {"register": {}, "refresh": {}, "archive": {}},
	enum.KindProviderPool:      {"create": {}, "update": {}, "archive": {}, "delete": {}},
}

func protectedConfigurationPermission(kind enum.Kind) string {
	switch kind {
	case enum.KindRoleDefinition:
		return permissionRoleDefinitionManage
	case enum.KindAgent:
		return permissionAgentManage
	case enum.KindAgentAssignment:
		return permissionAgentAssignmentManage
	case enum.KindInstructionSet:
		return permissionInstructionSetManage
	case enum.KindProviderReference:
		return permissionProviderReferenceManage
	case enum.KindProviderPool:
		return permissionProviderPoolManage
	default:
		return ""
	}
}

func protectedConfigurationScope(kind enum.Kind, action string) string {
	prefix := map[enum.Kind]string{
		enum.KindRoleDefinition:    "role_definition",
		enum.KindAgent:             "agent",
		enum.KindAgentAssignment:   "agent_assignment",
		enum.KindInstructionSet:    "instruction_set",
		enum.KindProviderReference: "provider_reference",
		enum.KindProviderPool:      "provider_pool",
	}[kind]
	return prefix + "_" + action
}

// ManageProtectedConfiguration выполняет только закрытый kind-specific registry.
func (service *Service) ManageProtectedConfiguration(
	ctx context.Context,
	input ManageProtectedConfigurationInput,
) (entity.Resource, error) {
	permission := protectedConfigurationPermission(input.Kind)
	actions, kindAllowed := protectedConfigurationActions[input.Kind]
	_, actionAllowed := actions[input.Action]
	if permission == "" || !kindAllowed || !actionAllowed {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if err := authorize(input.Principal, permission); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || input.Principal.ProjectID == "" ||
		(value.ValidateName(input.Name) != nil && protectedCreateLike(input.Action)) ||
		(input.Action == "update" && value.ValidateName(input.Name) != nil) ||
		(input.ResourceID != "" && value.ValidateID(input.ResourceID) != nil) ||
		(input.TargetSHA256 != "" && !validSHA256Text(input.TargetSHA256)) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if protectedCreateLike(input.Action) {
		if input.Action == "copy" &&
			(value.ValidateID(input.ResourceID) != nil || input.ExpectedVersion == 0) {
			return entity.Resource{}, errs.ErrInvalidInput
		}
		if input.Action != "copy" && (input.ResourceID != "" || input.ExpectedVersion != 0) {
			return entity.Resource{}, errs.ErrInvalidInput
		}
	} else if value.ValidateID(input.ResourceID) != nil || input.ExpectedVersion == 0 {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		Kind            enum.Kind
		Action          string
		ResourceID      string
		ExpectedVersion uint64
		Name            string
		Spec            entity.Spec
		TargetVersion   uint64
		TargetSHA256    string
		ReferenceKeys   []string
	}{identity(input.Principal), input.Kind, input.Action, input.ResourceID,
		input.ExpectedVersion, input.Name, input.Spec, input.TargetVersion,
		input.TargetSHA256, input.ReferenceKeys})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	apply := func(tx domainrepo.Transaction) (entity.Resource, error) {
		protected, ok := tx.(domainrepo.ProtectedTransaction)
		if !ok {
			return entity.Resource{}, errs.ErrInternal
		}
		if input.Action == "copy" {
			return service.copyInstructionSet(ctx, tx, protected, input)
		}
		if protectedCreateLike(input.Action) {
			return service.createProtectedConfiguration(ctx, tx, protected, input)
		}
		return service.mutateProtectedConfiguration(ctx, tx, protected, input)
	}
	scope := protectedConfigurationScope(input.Kind, input.Action)
	if protectedCreateLike(input.Action) && input.Action != "copy" {
		return service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
			scope, requestHash, apply)
	}
	return service.withOwnerLockedResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		scope, requestHash, input.ResourceID, input.Kind, input.ExpectedVersion,
		func(stored entity.Resource) error {
			if stored.Kind != input.Kind || stored.OwnerActorID != input.Principal.ActorID ||
				(input.Action != "copy" && stored.ID != input.ResourceID) {
				return errs.ErrStateConflict
			}
			if input.Action == "copy" {
				if stored.Version != 1 {
					return errs.ErrStateConflict
				}
				return nil
			}
			expectedResultVersion := input.ExpectedVersion + 1
			if input.Action == "delete" {
				expectedResultVersion++
			}
			if stored.Version != expectedResultVersion {
				return errs.ErrStateConflict
			}
			return nil
		},
		apply,
	)
}

func protectedCreateLike(action string) bool {
	return action == "create" || action == "assign" || action == "register" || action == "copy"
}

func (service *Service) createProtectedConfiguration(
	ctx context.Context,
	tx domainrepo.Transaction,
	protected domainrepo.ProtectedTransaction,
	input ManageProtectedConfigurationInput,
) (entity.Resource, error) {
	spec, err := service.resolveProtectedSpec(ctx, tx, protected, input, entity.Resource{})
	if err != nil {
		return entity.Resource{}, err
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	created, err := entity.New(
		uuid.NewString(), input.Principal.OrganizationID, input.Principal.ProjectID, "",
		input.Principal.ActorID, input.Kind, input.Name, spec, now,
	)
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if err := tx.Insert(ctx, created); err != nil {
		return entity.Resource{}, err
	}
	if err := service.appendProtectedRecords(ctx, tx, protected, input.Principal, input.Action, created); err != nil {
		return entity.Resource{}, err
	}
	return created, nil
}

func (service *Service) mutateProtectedConfiguration(
	ctx context.Context,
	tx domainrepo.Transaction,
	protected domainrepo.ProtectedTransaction,
	input ManageProtectedConfigurationInput,
) (entity.Resource, error) {
	current, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, input.ResourceID)
	if err != nil {
		return entity.Resource{}, err
	}
	if current.Kind != input.Kind || current.OwnerActorID != input.Principal.ActorID {
		return entity.Resource{}, errs.ErrNotFound
	}
	if current.Version != input.ExpectedVersion {
		return entity.Resource{}, errs.ErrVersionMismatch
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	var updated entity.Resource
	switch input.Action {
	case "update", "refresh":
		spec, resolveErr := service.resolveProtectedSpec(ctx, tx, protected, input, current)
		if resolveErr != nil {
			return entity.Resource{}, resolveErr
		}
		currentStableKey, currentHasStableKey := protectedConfigurationStableKey(current.Spec)
		nextStableKey, nextHasStableKey := protectedConfigurationStableKey(spec)
		if currentHasStableKey != nextHasStableKey ||
			(currentHasStableKey && currentStableKey != nextStableKey) {
			return entity.Resource{}, errs.ErrStateConflict
		}
		if input.Action == "update" {
			spec, resolveErr = configurationUpdateSpec(ctx, tx, input.Principal, current.Spec, spec, false)
			if resolveErr != nil {
				return entity.Resource{}, resolveErr
			}
		}
		name := current.Name
		if input.Action == "update" {
			name = input.Name
		}
		updated, err = current.Update(name, spec, now)
	case "validate", "publish", "rollback", "detach":
		updated, err = service.transitionInstructionSet(ctx, tx, protected, input, current, now)
	case "archive":
		if err := service.requireNoLiveProtectedReferences(ctx, tx, input.Principal, current.ID); err != nil {
			return entity.Resource{}, err
		}
		updated, err = archiveProtectedResource(current, now)
	case "delete":
		if err := service.requireNoLiveProtectedReferences(ctx, tx, input.Principal, current.ID); err != nil {
			return entity.Resource{}, err
		}
		updated, err = deleteProtectedResource(current, now)
	case "unassign":
		assignment, ok := current.Spec.(entity.AgentAssignmentSpec)
		if !ok || current.State != enum.StateActive {
			return entity.Resource{}, errs.ErrStateConflict
		}
		assignment.AssignmentGeneration++
		updated, err = current.ReplaceAndTransition(assignment, enum.StateArchived, now)
	default:
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if err != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, updated, current.Version); err != nil {
		return entity.Resource{}, err
	}
	if err := service.appendProtectedRecords(ctx, tx, protected, input.Principal, input.Action, updated); err != nil {
		return entity.Resource{}, err
	}
	return updated, nil
}

func protectedConfigurationStableKey(spec entity.Spec) (string, bool) {
	switch typed := spec.(type) {
	case entity.RoleDefinitionSpec:
		return typed.StableKey, true
	case entity.AgentSpec:
		return typed.StableKey, true
	case entity.InstructionSetSpec:
		return typed.StableKey, true
	case entity.ProviderConnectionReferenceSpec:
		return typed.StableKey, true
	case entity.ProviderPoolSpec:
		return typed.StableKey, true
	default:
		return "", false
	}
}

func archiveProtectedResource(current entity.Resource, now time.Time) (entity.Resource, error) {
	if current.Kind == enum.KindInstructionSet {
		spec, ok := current.Spec.(entity.InstructionSetSpec)
		if !ok || current.State != enum.StateActive {
			return entity.Resource{}, errs.ErrStateConflict
		}
		spec.VersionState = "ARCHIVED"
		return current.ReplaceAndTransition(spec, enum.StateArchived, now)
	}
	if current.Kind == enum.KindProviderReference {
		spec, ok := current.Spec.(entity.ProviderConnectionReferenceSpec)
		if !ok || (current.State != enum.StateActive && current.State != enum.StatePaused) {
			return entity.Resource{}, errs.ErrStateConflict
		}
		spec.MaskedStatus = "ARCHIVED"
		spec.Eligible = false
		return current.ReplaceAndTransition(spec, enum.StateArchived, now)
	}
	return current.Transition(enum.StateArchived, now)
}

func deleteProtectedResource(current entity.Resource, now time.Time) (entity.Resource, error) {
	if current.State != enum.StateArchived {
		return entity.Resource{}, errs.ErrStateConflict
	}
	pending, err := current.Transition(enum.StateDeletionPending, now)
	if err != nil {
		return entity.Resource{}, err
	}
	return pending.Transition(enum.StateDeleted, now)
}

func (service *Service) resolveProtectedSpec(
	ctx context.Context,
	tx domainrepo.Transaction,
	protected domainrepo.ProtectedTransaction,
	input ManageProtectedConfigurationInput,
	current entity.Resource,
) (entity.Spec, error) {
	switch input.Kind {
	case enum.KindRoleDefinition:
		spec, ok := input.Spec.(entity.RoleDefinitionSpec)
		if !ok || spec.Validate() != nil {
			return nil, errs.ErrInvalidInput
		}
		permissions, err := tx.ActorPermissions(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, input.Principal.ActorID)
		if err != nil {
			return nil, err
		}
		if err := ensureAssignable(permissions, spec.Capabilities); err != nil {
			return nil, err
		}
		ids := slices.Clone(spec.AllowedTargetRoleDefinitionIDs)
		slices.Sort(ids)
		for _, targetID := range ids {
			target, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, targetID)
			if err != nil {
				return nil, err
			}
			targetSpec, ok := target.Spec.(entity.RoleDefinitionSpec)
			if !ok || target.State != enum.StateActive || ensureAssignable(permissions, targetSpec.Capabilities) != nil {
				return nil, errs.ErrPermissionDenied
			}
		}
		if spec.RoleImageRecipeID != "" {
			recipe, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, spec.RoleImageRecipeID)
			if err != nil {
				return nil, err
			}
			recipeSHA, digestErr := entity.ProjectionSHA256(recipe)
			if digestErr != nil || recipe.Kind != enum.KindRoleImageRecipe || recipe.State != enum.StateActive ||
				recipe.Version != spec.RoleImageRecipeVersion || recipeSHA != spec.RoleImageRecipeSHA256 {
				return nil, errs.ErrStateConflict
			}
		}
		return spec, nil
	case enum.KindAgent:
		spec, ok := input.Spec.(entity.AgentSpec)
		if !ok || len(input.ReferenceKeys) != 3 {
			return nil, errs.ErrInvalidInput
		}
		role, err := requireProtectedStable(ctx, protected, input.Principal, enum.KindRoleDefinition, input.ReferenceKeys[0])
		if err != nil {
			return nil, err
		}
		instruction, err := requireProtectedStable(ctx, protected, input.Principal, enum.KindInstructionSet, input.ReferenceKeys[1])
		if err != nil {
			return nil, err
		}
		pool, err := requireProtectedStable(ctx, protected, input.Principal, enum.KindProviderPool, input.ReferenceKeys[2])
		if err != nil {
			return nil, err
		}
		instructionSpec, ok := instruction.Spec.(entity.InstructionSetSpec)
		if !ok || instructionSpec.VersionState != "PUBLISHED" || instructionSpec.PublishedVersion != instructionSpec.CurrentVersion {
			return nil, errs.ErrStateConflict
		}
		permissions, err := tx.ActorPermissions(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, input.Principal.ActorID)
		if err != nil {
			return nil, err
		}
		roleSpec, ok := role.Spec.(entity.RoleDefinitionSpec)
		if !ok || ensureAssignable(roleSpec.Capabilities, spec.Capabilities) != nil {
			return nil, errs.ErrPermissionDenied
		}
		if err := ensureAssignable(permissions, spec.Capabilities); err != nil {
			return nil, err
		}
		if spec.RoleDefinitionID, spec.RoleDefinitionVersion, spec.RoleDefinitionSHA256, err = protectedTuple(role); err != nil {
			return nil, err
		}
		if spec.InstructionSetID, spec.InstructionSetVersion, spec.InstructionSetSHA256, err = protectedTuple(instruction); err != nil {
			return nil, err
		}
		if spec.ProviderPoolID, spec.ProviderPoolVersion, spec.ProviderPoolSHA256, err = protectedTuple(pool); err != nil {
			return nil, err
		}
		if spec.Validate() != nil {
			return nil, errs.ErrInvalidInput
		}
		return spec, nil
	case enum.KindAgentAssignment:
		if len(input.ReferenceKeys) != 3 {
			return nil, errs.ErrInvalidInput
		}
		agent, err := requireProtectedStable(ctx, protected, input.Principal, enum.KindAgent, input.ReferenceKeys[0])
		if err != nil {
			return nil, err
		}
		workspace, err := protected.GetByNameForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
			enum.KindRepositoryWorkspace, input.ReferenceKeys[1])
		if err != nil || workspace.State != enum.StateActive {
			return nil, errs.ErrNotFound
		}
		roomID := ""
		if input.ReferenceKeys[2] != "" {
			room, err := requireProtectedStable(ctx, protected, input.Principal, enum.KindChat, input.ReferenceKeys[2])
			if err != nil {
				return nil, err
			}
			roomID = room.ID
		}
		agentID, agentVersion, agentSHA, err := protectedTuple(agent)
		if err != nil {
			return nil, err
		}
		workspaceID, workspaceVersion, workspaceSHA, err := protectedTuple(workspace)
		if err != nil {
			return nil, err
		}
		return entity.AgentAssignmentSpec{
			AgentID: agentID, AgentVersion: agentVersion, AgentSHA256: agentSHA,
			WorkspaceID: workspaceID, WorkspaceVersion: workspaceVersion, WorkspaceSHA256: workspaceSHA,
			RoomID: roomID, RootActorID: input.Principal.ActorID, AssignmentGeneration: 1,
		}, nil
	case enum.KindInstructionSet:
		spec, ok := input.Spec.(entity.InstructionSetSpec)
		if !ok || input.Action != "create" && input.Action != "update" {
			return nil, errs.ErrInvalidInput
		}
		if input.Action == "create" {
			spec.CurrentVersion, spec.PublishedVersion, spec.VersionState = 1, 0, "DRAFT"
			spec.ValidationSHA256, spec.RollbackOfVersion = "", 0
		} else {
			currentSpec, ok := current.Spec.(entity.InstructionSetSpec)
			if !ok || currentSpec.VersionState == "ARCHIVED" {
				return nil, errs.ErrStateConflict
			}
			if currentSpec.Ownership.ManagedBy == "GIT" {
				return nil, errs.ErrPermissionDenied
			}
			spec.CurrentVersion = currentSpec.CurrentVersion + 1
			spec.PublishedVersion = currentSpec.PublishedVersion
			spec.VersionState, spec.ValidationSHA256, spec.RollbackOfVersion = "DRAFT", "", 0
		}
		if spec.Validate() != nil {
			return nil, errs.ErrInvalidInput
		}
		return spec, validateConfigurationCreate(ctx, tx, input.Principal, spec)
	case enum.KindProviderReference:
		spec, ok := input.Spec.(entity.ProviderConnectionReferenceSpec)
		if !ok || input.Principal.CallerWorkload != service.integrationGatewayWorkload ||
			input.Principal.CallerSPIFFEID != service.integrationGatewaySPIFFEID ||
			input.Principal.AuthoritySource != "DOMAIN_STATE" ||
			input.Principal.AuthorityReference == "" || input.Principal.AuthorityRevision == 0 ||
			!validSHA256Text(input.Principal.AuthorityDigest) {
			return nil, errs.ErrPermissionDenied
		}
		if spec.ReceiptID != "" && spec.ReceiptID != input.Principal.AuthorityReference ||
			spec.ReceiptVersion != 0 && spec.ReceiptVersion != input.Principal.AuthorityRevision ||
			spec.ReceiptSHA256 != "" && spec.ReceiptSHA256 != input.Principal.AuthorityDigest {
			return nil, errs.ErrStateConflict
		}
		spec.ReceiptID, spec.ReceiptVersion, spec.ReceiptSHA256 = input.Principal.AuthorityReference,
			input.Principal.AuthorityRevision, input.Principal.AuthorityDigest
		if current.ID != "" {
			currentSpec, ok := current.Spec.(entity.ProviderConnectionReferenceSpec)
			if !ok || spec.ReferenceVersion <= currentSpec.ReferenceVersion ||
				spec.ReceiptVersion <= currentSpec.ReceiptVersion || spec.ServerReference != currentSpec.ServerReference {
				return nil, errs.ErrStateConflict
			}
		}
		if spec.Validate() != nil {
			return nil, errs.ErrInvalidInput
		}
		return spec, nil
	case enum.KindProviderPool:
		spec, ok := input.Spec.(entity.ProviderPoolSpec)
		if !ok || len(spec.Bindings) != len(input.ReferenceKeys) || len(input.ReferenceKeys) == 0 {
			return nil, errs.ErrInvalidInput
		}
		weights := make(map[string]uint32, len(input.ReferenceKeys))
		for index, key := range input.ReferenceKeys {
			if value.ValidateStableKey(key) != nil || spec.Bindings[index].Weight == 0 {
				return nil, errs.ErrInvalidInput
			}
			weights[key] = spec.Bindings[index].Weight
		}
		keys := slices.Clone(input.ReferenceKeys)
		slices.Sort(keys)
		spec.Bindings = make([]entity.ProviderPoolBinding, 0, len(keys))
		now := service.now().UTC()
		for _, key := range keys {
			reference, err := requireProtectedStable(ctx, protected, input.Principal, enum.KindProviderReference, key)
			if err != nil {
				return nil, err
			}
			referenceSpec, ok := reference.Spec.(entity.ProviderConnectionReferenceSpec)
			if !ok || !referenceSpec.Eligible || now.Sub(referenceSpec.ObservedAt) > spec.ObservationMaxAge {
				return nil, errs.ErrStateConflict
			}
			referenceID, referenceVersion, referenceSHA, err := protectedTuple(reference)
			if err != nil {
				return nil, err
			}
			spec.Bindings = append(spec.Bindings, entity.ProviderPoolBinding{
				ProviderConnectionReferenceID: referenceID, ReferenceVersion: referenceVersion,
				ProviderConnectionStableKey: key,
				ReferenceSHA256:             referenceSHA, Weight: weights[key], Eligible: true,
				MaskedStatus: referenceSpec.MaskedStatus,
			})
		}
		snapshot := spec
		snapshot.EligibilitySnapshotSHA256 = strings.Repeat("0", 64)
		digest, err := canonicalHash(snapshot)
		if err != nil {
			return nil, errs.ErrInternal
		}
		spec.EligibilitySnapshotSHA256 = digest
		if current.ID != "" {
			currentSpec, ok := current.Spec.(entity.ProviderPoolSpec)
			if !ok || spec.PolicyRevision <= currentSpec.PolicyRevision {
				return nil, errs.ErrStateConflict
			}
		}
		if spec.Validate() != nil {
			return nil, errs.ErrInvalidInput
		}
		return spec, nil
	default:
		return nil, errs.ErrInvalidInput
	}
}

func requireProtectedStable(
	ctx context.Context,
	protected domainrepo.ProtectedTransaction,
	principal value.Principal,
	kind enum.Kind,
	stableKey string,
) (entity.Resource, error) {
	if value.ValidateStableKey(stableKey) != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	resource, err := protected.GetByStableKeyForUpdate(ctx, principal.OrganizationID, principal.ProjectID, kind, stableKey)
	if err != nil {
		return entity.Resource{}, err
	}
	if resource.Kind != kind || resource.State != enum.StateActive {
		return entity.Resource{}, errs.ErrStateConflict
	}
	return resource, nil
}

func protectedTuple(resource entity.Resource) (string, uint64, string, error) {
	digest, err := entity.ProjectionSHA256(resource)
	if err != nil {
		return "", 0, "", errs.ErrInternal
	}
	return resource.ID, resource.Version, digest, nil
}

func (service *Service) transitionInstructionSet(
	ctx context.Context,
	tx domainrepo.Transaction,
	protected domainrepo.ProtectedTransaction,
	input ManageProtectedConfigurationInput,
	current entity.Resource,
	now time.Time,
) (entity.Resource, error) {
	spec, ok := current.Spec.(entity.InstructionSetSpec)
	if !ok {
		return entity.Resource{}, errs.ErrStateConflict
	}
	switch input.Action {
	case "validate":
		if spec.VersionState != "DRAFT" || input.TargetVersion != spec.CurrentVersion || !validSHA256Text(input.TargetSHA256) {
			return entity.Resource{}, errs.ErrStateConflict
		}
		spec.VersionState, spec.ValidationSHA256 = "VALIDATED", input.TargetSHA256
		return current.Update(current.Name, spec, now)
	case "publish":
		if spec.VersionState != "VALIDATED" || input.TargetVersion != spec.CurrentVersion ||
			input.TargetSHA256 != spec.ContentSHA256 {
			return entity.Resource{}, errs.ErrStateConflict
		}
		spec.VersionState, spec.PublishedVersion = "PUBLISHED", spec.CurrentVersion
		return current.Update(current.Name, spec, now)
	case "rollback":
		if spec.Ownership.ManagedBy == "GIT" || input.TargetVersion == 0 || !validSHA256Text(input.TargetSHA256) {
			return entity.Resource{}, errs.ErrPermissionDenied
		}
		target, err := protected.GetInstructionHistoryContentVersion(ctx, current.ID, input.TargetVersion)
		if err != nil {
			return entity.Resource{}, err
		}
		targetSpec, ok := target.Resource.Spec.(entity.InstructionSetSpec)
		if !ok || targetSpec.ContentSHA256 != input.TargetSHA256 || targetSpec.VersionState != "PUBLISHED" {
			return entity.Resource{}, errs.ErrStateConflict
		}
		spec.CurrentVersion++
		spec.PublishedVersion = spec.CurrentVersion
		spec.Content, spec.ContentSHA256 = targetSpec.Content, targetSpec.ContentSHA256
		spec.VersionState, spec.ValidationSHA256 = "PUBLISHED", targetSpec.ValidationSHA256
		spec.RollbackOfVersion = targetSpec.CurrentVersion
		return current.Update(current.Name, spec, now)
	case "detach":
		if spec.Ownership.ManagedBy != "GIT" {
			return entity.Resource{}, errs.ErrStateConflict
		}
		if err := requireDurablePermission(ctx, tx, input.Principal, permissionDetachConfiguration); err != nil {
			return entity.Resource{}, err
		}
		previousVersion := spec.CurrentVersion
		spec.CurrentVersion++
		if spec.VersionState == "PUBLISHED" {
			spec.PublishedVersion = spec.CurrentVersion
		}
		spec.RollbackOfVersion = previousVersion
		spec.Ownership = entity.ConfigurationOwnership{
			ManagedBy: "UI", SourceRef: "control-plane://instruction-set/" + current.ID,
			SourceRevision: previousVersion,
		}
		return current.Update(current.Name, spec, now)
	default:
		return entity.Resource{}, errs.ErrInvalidInput
	}
}

func (service *Service) copyInstructionSet(
	ctx context.Context,
	tx domainrepo.Transaction,
	protected domainrepo.ProtectedTransaction,
	input ManageProtectedConfigurationInput,
) (entity.Resource, error) {
	source, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, input.ResourceID)
	if err != nil {
		return entity.Resource{}, err
	}
	sourceSpec, ok := source.Spec.(entity.InstructionSetSpec)
	if !ok || source.Kind != enum.KindInstructionSet || source.OwnerActorID != input.Principal.ActorID ||
		source.Version != input.ExpectedVersion || sourceSpec.Ownership.ManagedBy != "GIT" {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if err := requireDurablePermission(ctx, tx, input.Principal, permissionDetachConfiguration); err != nil {
		return entity.Resource{}, err
	}
	newID := uuid.NewString()
	copySpec := sourceSpec
	suffix := "-copy-" + strings.ReplaceAll(newID[:8], "-", "")
	baseKey := sourceSpec.StableKey
	if len(baseKey) > 96-len(suffix) {
		baseKey = strings.TrimRight(baseKey[:96-len(suffix)], "-")
	}
	copySpec.StableKey = baseKey + suffix
	copySpec.CurrentVersion, copySpec.PublishedVersion = 1, 0
	copySpec.VersionState, copySpec.ValidationSHA256, copySpec.RollbackOfVersion = "DRAFT", "", 0
	copySpec.Ownership = entity.ConfigurationOwnership{
		ManagedBy: "UI", SourceRef: "control-plane://instruction-set/" + source.ID,
		SourceRevision: sourceSpec.CurrentVersion,
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	created, err := entity.New(newID, input.Principal.OrganizationID, input.Principal.ProjectID, "",
		input.Principal.ActorID, enum.KindInstructionSet, input.Name, copySpec, now)
	if err != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if err := tx.Insert(ctx, created); err != nil {
		return entity.Resource{}, err
	}
	if err := service.appendProtectedRecords(ctx, tx, protected, input.Principal, input.Action, created); err != nil {
		return entity.Resource{}, err
	}
	return created, nil
}

func (service *Service) appendProtectedRecords(
	ctx context.Context,
	tx domainrepo.Transaction,
	protected domainrepo.ProtectedTransaction,
	principal value.Principal,
	action string,
	resource entity.Resource,
) error {
	digest, err := entity.ProjectionSHA256(resource)
	if err != nil {
		return errs.ErrInternal
	}
	if err := protected.AppendProtectedResourceHistory(ctx, domainrepo.ProtectedResourceHistory{
		Resource: resource, Action: action, SnapshotSHA256: digest, OccurredAt: resource.UpdatedAt,
	}); err != nil {
		return err
	}
	return appendOwnerStateAudit(ctx, tx, principal, protectedConfigurationScope(resource.Kind, action),
		resource.OrganizationID, resource.ProjectID, resource.ID, string(resource.Kind), resource.Version, resource.UpdatedAt)
}

func (service *Service) requireNoLiveProtectedReferences(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	resourceID string,
) error {
	resources, err := tx.ListSnapshotResources(ctx, principal.OrganizationID, principal.ProjectID)
	if err != nil {
		return err
	}
	for _, candidate := range resources {
		if candidate.ID == resourceID || candidate.State == enum.StateDeleted || candidate.State == enum.StateArchived {
			continue
		}
		if protectedSpecReferences(candidate.Spec, resourceID) {
			return errs.ErrStateConflict
		}
	}
	return nil
}

func protectedSpecReferences(spec entity.Spec, resourceID string) bool {
	switch typed := spec.(type) {
	case entity.AgentSpec:
		return typed.RoleDefinitionID == resourceID || typed.InstructionSetID == resourceID || typed.ProviderPoolID == resourceID
	case entity.AgentAssignmentSpec:
		return typed.AgentID == resourceID || typed.WorkspaceID == resourceID || typed.RoomID == resourceID
	case entity.ProviderPoolSpec:
		return slices.ContainsFunc(typed.Bindings, func(binding entity.ProviderPoolBinding) bool {
			return binding.ProviderConnectionReferenceID == resourceID
		})
	case entity.ScheduleSpec:
		return typed.TargetResourceID == resourceID || typed.AgentID == resourceID ||
			typed.InstructionSetID == resourceID || typed.ProviderPoolID == resourceID
	case entity.RuntimeRevisionSpec:
		return typed.AgentID == resourceID || typed.RoleDefinitionID == resourceID ||
			typed.InstructionSetID == resourceID || typed.ProviderPoolID == resourceID ||
			slices.ContainsFunc(typed.Components, func(component entity.EffectiveResourceRef) bool {
				return component.ResourceID == resourceID
			})
	case entity.WorkspaceRestoreSpec:
		return typed.BackupID == resourceID
	case entity.WorkspaceMattermostMappingSpec:
		return typed.WorkspaceID == resourceID
	default:
		return false
	}
}

func (service *Service) GetProtectedConfiguration(
	ctx context.Context,
	principal value.Principal,
	resourceID string,
	kind enum.Kind,
) (entity.Resource, error) {
	return service.Get(ctx, GetInput{Principal: principal, ResourceID: resourceID, Kind: kind})
}

func (service *Service) ListProtectedConfigurations(
	ctx context.Context,
	principal value.Principal,
	kind enum.Kind,
	states []enum.State,
	afterID string,
	limit int,
) ([]entity.Resource, error) {
	return service.List(ctx, ListInput{Principal: principal, Filter: query.ResourceFilter{
		Kind: kind, States: states, AfterID: afterID, Limit: limit,
	}})
}

func (service *Service) ListProtectedResourceHistory(
	ctx context.Context,
	input ProtectedResourceHistoryInput,
) ([]domainrepo.ProtectedResourceHistory, error) {
	if err := authorize(input.Principal, permissionRead); err != nil {
		return nil, err
	}
	_, kindAllowed := protectedConfigurationActions[input.Kind]
	if value.ValidateID(input.ResourceID) != nil || input.Limit < 1 || input.Limit > 100 || !kindAllowed {
		return nil, errs.ErrInvalidInput
	}
	current, err := service.repository.Get(ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		input.ResourceID, input.Kind)
	if err != nil || current.OwnerActorID != input.Principal.ActorID {
		return nil, errs.ErrNotFound
	}
	repository, ok := service.repository.(domainrepo.ProtectedRepository)
	if !ok {
		return nil, errs.ErrInternal
	}
	return repository.ListProtectedResourceHistory(ctx, input.Principal.OrganizationID,
		input.Principal.ProjectID, input.Principal.ActorID, input.ResourceID, input.BeforeVersion, input.Limit)
}

func (service *Service) CompareInstructionVersions(
	ctx context.Context,
	input CompareInstructionVersionsInput,
) (CompareInstructionVersionsResult, error) {
	if err := authorize(input.Principal, permissionRead); err != nil {
		return CompareInstructionVersionsResult{}, err
	}
	if value.ValidateID(input.InstructionSetID) != nil || input.LeftVersion == 0 ||
		input.RightVersion == 0 || input.LeftVersion == input.RightVersion {
		return CompareInstructionVersionsResult{}, errs.ErrInvalidInput
	}
	var result CompareInstructionVersionsResult
	err := service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID, ProjectID: input.Principal.ProjectID,
		ActorID: input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		current, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, input.InstructionSetID)
		if err != nil {
			return err
		}
		if current.Kind != enum.KindInstructionSet || current.OwnerActorID != input.Principal.ActorID {
			return errs.ErrNotFound
		}
		protected, ok := tx.(domainrepo.ProtectedTransaction)
		if !ok {
			return errs.ErrInternal
		}
		result.Left, err = protected.GetInstructionHistoryContentVersion(ctx, current.ID, input.LeftVersion)
		if err != nil {
			return err
		}
		result.Right, err = protected.GetInstructionHistoryContentVersion(ctx, current.ID, input.RightVersion)
		if err != nil {
			return err
		}
		left := result.Left.Resource.Spec.(entity.InstructionSetSpec)
		right := result.Right.Resource.Spec.(entity.InstructionSetSpec)
		result.ContentEqual = left.ContentSHA256 == right.ContentSHA256
		result.ComparisonSHA256, err = canonicalHash(struct {
			InstructionSetID string
			LeftVersion      uint64
			LeftSHA256       string
			RightVersion     uint64
			RightSHA256      string
		}{current.ID, input.LeftVersion, left.ContentSHA256, input.RightVersion, right.ContentSHA256})
		return err
	})
	return result, err
}
