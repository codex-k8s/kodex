package resource

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	controlplanecontract "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi"
	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

var protectedConfigurationActions = map[enum.Kind]map[string]struct{}{
	enum.KindRoleDefinition:    {"create": {}, "update": {}, "reconcile_git": {}, "archive": {}, "delete": {}},
	enum.KindAgent:             {"create": {}, "update": {}, "reconcile_git": {}, "pause": {}, "resume": {}, "enable": {}, "disable": {}, "bind_bot": {}, "rebind_bot": {}, "revoke_bot": {}, "archive": {}, "delete": {}},
	enum.KindAgentAssignment:   {"assign": {}, "unassign": {}},
	enum.KindInstructionSet:    {"create": {}, "update": {}, "reconcile_git": {}, "validate": {}, "publish": {}, "rollback": {}, "detach": {}, "copy": {}, "archive": {}, "delete": {}},
	enum.KindProviderReference: {"register": {}, "refresh": {}, "archive": {}},
	enum.KindProviderPool:      {"create": {}, "update": {}, "reconcile_git": {}, "archive": {}, "delete": {}},
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

func protectedConfigurationMethod(kind enum.Kind, action string) string {
	if action == "reconcile_git" {
		return map[enum.Kind]string{
			enum.KindRoleDefinition: "/controlplane.v1.ControlPlaneService/ReconcileGitRoleDefinition",
			enum.KindAgent:          "/controlplane.v1.ControlPlaneService/ReconcileGitAgent",
			enum.KindInstructionSet: "/controlplane.v1.ControlPlaneService/ReconcileGitInstructionSet",
			enum.KindProviderPool:   "/controlplane.v1.ControlPlaneService/ReconcileGitProviderPool",
		}[kind]
	}
	if kind == enum.KindAgent && (action == "bind_bot" || action == "rebind_bot" || action == "revoke_bot") {
		return "/controlplane.v1.ControlPlaneService/ManageAgentMattermostBotIdentity"
	}
	return map[enum.Kind]string{
		enum.KindRoleDefinition:    "/controlplane.v1.ControlPlaneService/ManageRoleDefinition",
		enum.KindAgent:             "/controlplane.v1.ControlPlaneService/ManageAgent",
		enum.KindAgentAssignment:   "/controlplane.v1.ControlPlaneService/ManageAgentAssignment",
		enum.KindInstructionSet:    "/controlplane.v1.ControlPlaneService/ManageInstructionSet",
		enum.KindProviderReference: "/controlplane.v1.ControlPlaneService/ManageProviderConnectionReference",
		enum.KindProviderPool:      "/controlplane.v1.ControlPlaneService/ManageProviderPool",
	}[kind]
}

// ManageProtectedConfiguration выполняет только закрытый kind-specific registry.
func (service *Service) ManageProtectedConfiguration(
	ctx context.Context,
	input ManageProtectedConfigurationInput,
) (entity.Resource, error) {
	if input.Readiness {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	permission := protectedConfigurationPermission(input.Kind)
	if input.Action == "reconcile_git" {
		permission = permissionApplyGitConfiguration
	} else if input.Kind == enum.KindAgent &&
		(input.Action == "bind_bot" || input.Action == "rebind_bot" || input.Action == "revoke_bot") {
		permission = permissionAgentBotIdentityManage
	}
	actions, kindAllowed := protectedConfigurationActions[input.Kind]
	_, actionAllowed := actions[input.Action]
	if permission == "" || !kindAllowed || !actionAllowed || input.FullMethod == "" ||
		input.FullMethod != protectedConfigurationMethod(input.Kind, input.Action) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if err := authorize(input.Principal, permission); err != nil {
		return entity.Resource{}, err
	}
	if input.Action == "reconcile_git" {
		if err := service.validateGitReconciliationReceipt(input); err != nil {
			return entity.Resource{}, err
		}
		// Producer подписывает exact caller intent до server-owned artifact
		// materialization; deterministic artifact pins не являются частью
		// внешнего Git authority payload.
		if !validSHA256Text(input.SemanticIntentSHA256) ||
			input.GitReceipt.CommandIntentSHA256 != input.SemanticIntentSHA256 {
			return entity.Resource{}, errs.ErrPermissionDenied
		}
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || input.Principal.ProjectID == "" ||
		(value.ValidateName(input.Name) != nil && protectedCreateLike(input.Action)) ||
		((input.Action == "update" || input.Action == "reconcile_git") && value.ValidateName(input.Name) != nil) ||
		(input.ResourceID != "" && value.ValidateID(input.ResourceID) != nil) ||
		(input.TargetSHA256 != "" && !validSHA256Text(input.TargetSHA256)) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	createLike := protectedCreateLike(input.Action) || input.Action == "reconcile_git" && input.ResourceID == ""
	if createLike {
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
	if input.ProviderReceipt.ContractVersion != 0 {
		if input.Kind == enum.KindProviderReference || input.Kind == enum.KindProviderPool {
			if input.Principal.CallerWorkload != service.integrationGatewayWorkload ||
				input.Principal.CallerSPIFFEID != service.integrationGatewaySPIFFEID ||
				input.Principal.AuthoritySource != "PROVIDER_READBACK" ||
				validateProviderReceipt(input.Principal, input.ProviderReceipt,
					"AI_PROVIDER_READBACK_RECEIPT", input.Action,
					map[enum.Kind]string{enum.KindProviderReference: "provider_connection_reference", enum.KindProviderPool: "pool-observation"}[input.Kind], input.FullMethod, service.now().UTC()) != nil {
				return entity.Resource{}, errs.ErrPermissionDenied
			}
		}
		intentHash := input.SemanticIntentSHA256
		var intentErr error
		if input.Kind != enum.KindProviderReference && input.Kind != enum.KindProviderPool {
			intentHash, intentErr = protectedProviderIntentSHA256(input)
		}
		if intentErr != nil || !validSHA256Text(intentHash) ||
			input.ProviderReceipt.CommandIntentSHA256 != intentHash {
			return entity.Resource{}, errs.ErrPermissionDenied
		}
	}
	if protectedExternalSemanticReplay(input) {
		replay, replayed, replayErr := service.replayProtectedExternalCommand(ctx, input)
		if replayErr != nil || replayed {
			return replay, replayErr
		}
	}
	if input.Kind == enum.KindInstructionSet &&
		(input.Action == "create" || input.Action == "update" || input.Action == "reconcile_git") {
		var prepareErr error
		input, prepareErr = service.prepareInstructionArtifact(ctx, input)
		if prepareErr != nil {
			return entity.Resource{}, prepareErr
		}
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		FullMethod      string
		Kind            enum.Kind
		Action          string
		ResourceID      string
		ExpectedVersion uint64
		Name            string
		Spec            entity.Spec
		TargetVersion   uint64
		TargetSHA256    string
		ReferenceKeys   []string
		ProviderReceipt value.ProviderEffectReceipt
	}{
		identity(input.Principal), input.FullMethod, input.Kind, input.Action, input.ResourceID,
		input.ExpectedVersion, input.Name, input.Spec, input.TargetVersion,
		input.TargetSHA256, input.ReferenceKeys, input.ProviderReceipt,
	})
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
		if createLike {
			return service.createProtectedConfiguration(ctx, tx, protected, input)
		}
		return service.mutateProtectedConfiguration(ctx, tx, protected, input)
	}
	scope := protectedConfigurationScope(input.Kind, input.Action)
	if createLike && input.Action != "copy" {
		return service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
			scope, requestHash, apply)
	}
	result, mutationErr := service.withOwnerLockedResourceReceipt(
		ctx, input.Principal, input.IdempotencyKey,
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
	if mutationErr != nil && protectedExternalSemanticReplay(input) {
		replay, replayed, replayErr := service.replayProtectedExternalCommand(ctx, input)
		if replayErr != nil || replayed {
			return replay, replayErr
		}
	}
	return result, mutationErr
}

// CheckAgentMattermostBotIdentityManageReadiness принимает application grant
// того же generated Manage RPC, но выполняет только exact current owner и
// mapping readback. Receipt не резервируется и доменное состояние не меняется.
func (service *Service) CheckAgentMattermostBotIdentityManageReadiness(
	ctx context.Context,
	input ManageProtectedConfigurationInput,
) (entity.Resource, error) {
	if !input.Readiness || input.Kind != enum.KindAgent || input.Action != "rebind_bot" ||
		input.FullMethod != protectedConfigurationMethod(enum.KindAgent, input.Action) ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || value.ValidateID(input.ResourceID) != nil ||
		input.ExpectedVersion == 0 || input.Principal.ProjectID == "" {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if err := authorize(input.Principal, permissionAgentBotIdentityManage); err != nil {
		return entity.Resource{}, err
	}
	if !service.interactionGatewayPrincipal(input.Principal) ||
		input.Principal.AuthoritySource != "PROVIDER_READBACK" ||
		validateProviderReceipt(input.Principal, input.ProviderReceipt,
			"MATTERMOST_PROVIDER_READBACK_RECEIPT", agentBotReceiptAction(input.Action),
			"agent_bot_identity", input.FullMethod, service.now().UTC()) != nil ||
		input.ProviderReceipt.WorkspaceID != input.Principal.ProjectID ||
		!validAgentBotReceiptProfile(input.ProviderReceipt) {
		return entity.Resource{}, errs.ErrPermissionDenied
	}
	intentSHA, err := protectedProviderIntentSHA256(input)
	if err != nil || input.ProviderReceipt.CommandIntentSHA256 != intentSHA {
		return entity.Resource{}, errs.ErrPermissionDenied
	}
	var result entity.Resource
	err = service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID,
		ProjectID:      input.Principal.ProjectID,
		ActorID:        input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		current, getErr := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
			input.Principal.ProjectID, input.ResourceID)
		if getErr != nil {
			return getErr
		}
		spec, ok := current.Spec.(entity.AgentSpec)
		if !ok || current.Kind != enum.KindAgent || current.OwnerActorID != input.Principal.ActorID ||
			current.Version != input.ExpectedVersion ||
			(current.State != enum.StateActive && current.State != enum.StatePaused) ||
			spec.StableKey != input.ProviderReceipt.TargetStableKey || spec.BotIdentityRef == "" ||
			spec.BotIdentityRef != input.ProviderReceipt.ProviderObjectRef ||
			spec.BotProviderTeamRef != input.ProviderReceipt.ProviderTeamRef ||
			spec.BotUsername != input.ProviderReceipt.ProviderUsername ||
			spec.BotProviderRevision != input.ProviderReceipt.EffectVersion ||
			spec.BotProviderGeneration != input.ProviderReceipt.EffectGeneration ||
			spec.BotReceiptVersion != input.ProviderReceipt.ReceiptRevision ||
			spec.BotMaskedStatus != "AVAILABLE" || input.ProviderReceipt.MaskedStatus != "AVAILABLE" ||
			!input.ProviderReceipt.Eligible || input.ProviderReceipt.TargetResourceID != current.ID {
			return errs.ErrStateConflict
		}
		if _, mappingErr := lockWorkspaceMappingByOpaqueRef(ctx, tx, input.Principal,
			input.ProviderReceipt.ProviderTeamRef); mappingErr != nil {
			return mappingErr
		}
		result = current
		return nil
	})
	return result, err
}

func protectedCreateLike(action string) bool {
	return action == "create" || action == "assign" || action == "register" || action == "copy"
}

func protectedProviderIntentSHA256(input ManageProtectedConfigurationInput) (string, error) {
	if input.Kind == enum.KindAgent &&
		(input.Action == "bind_bot" || input.Action == "rebind_bot" || input.Action == "revoke_bot") {
		action := controlplanev1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_BIND
		switch input.Action {
		case "rebind_bot":
			action = controlplanev1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_REBIND
		case "revoke_bot":
			action = controlplanev1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_REVOKE
		}
		return controlplanecontract.AgentMattermostBotIdentityIntentSHA256(
			controlplanecontract.VerifiedCommandAuthority{
				ActorID: input.Principal.ActorID, OrganizationID: input.Principal.OrganizationID,
				ProjectID: input.Principal.ProjectID, WorkloadID: input.Principal.CallerWorkload,
				FullMethod: input.FullMethod,
			},
			&controlplanev1.ManageAgentMattermostBotIdentityRequest{
				Action: action, AgentId: input.ResourceID, ExpectedVersion: input.ExpectedVersion,
				Readiness: input.Readiness,
			},
			input.ProviderReceipt.TargetStableKey,
		)
	}
	return canonicalHash(struct {
		Identity        commandIdentity
		FullMethod      string
		Kind            enum.Kind
		Action          string
		ResourceID      string
		ExpectedVersion uint64
		Name            string
		Spec            entity.Spec
		ReferenceKeys   []string
	}{
		identity(input.Principal), input.FullMethod, input.Kind, input.Action, input.ResourceID,
		input.ExpectedVersion, input.Name, input.Spec, input.ReferenceKeys,
	})
}

func (service *Service) validateGitReconciliationReceipt(input ManageProtectedConfigurationInput) error {
	receipt := input.GitReceipt
	if receipt.Validate(service.now().UTC()) != nil ||
		input.Principal.CallerWorkload != service.integrationGatewayWorkload ||
		input.Principal.CallerSPIFFEID != service.integrationGatewaySPIFFEID ||
		input.Principal.AuthoritySource != "GIT_RECONCILIATION" ||
		input.Principal.AuthorityReference != receipt.ReceiptID ||
		input.Principal.AuthorityRevision != receipt.ReceiptRevision ||
		receipt.WorkloadID != input.Principal.CallerWorkload || receipt.CallerSPIFFEID != input.Principal.CallerSPIFFEID ||
		receipt.FullMethod != input.FullMethod || receipt.ActorID != input.Principal.ActorID ||
		receipt.OrganizationID != input.Principal.OrganizationID || receipt.ProjectID != input.Principal.ProjectID ||
		receipt.TargetKind != strings.ToLower(string(input.Kind)) || receipt.TargetResourceID != input.ResourceID {
		return errs.ErrPermissionDenied
	}
	configured, ok := input.Spec.(entity.ConfiguredSpec)
	if !ok {
		return errs.ErrInvalidInput
	}
	ownership := configured.ConfigurationOwnership()
	stableKey, ok := protectedConfigurationStableKey(input.Spec)
	if !ok || receipt.TargetStableKey != stableKey || ownership.ManagedBy != "GIT" ||
		ownership.SourceRef != receipt.SourceRef || ownership.SourceRevision != receipt.SourceRevision ||
		ownership.SourceSHA256 != receipt.SourceSHA256 {
		return errs.ErrPermissionDenied
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(receipt)
	if err != nil || digest != input.Principal.AuthorityDigest {
		return errs.ErrPermissionDenied
	}
	return nil
}

func (service *Service) createProtectedConfiguration(
	ctx context.Context,
	tx domainrepo.Transaction,
	protected domainrepo.ProtectedTransaction,
	input ManageProtectedConfigurationInput,
) (entity.Resource, error) {
	stableKey, hasStableKey := protectedConfigurationStableKey(input.Spec)
	var consumption domainrepo.ExternalCommandReceipt
	if input.ProviderReceipt.ContractVersion != 0 {
		if !hasStableKey {
			return entity.Resource{}, errs.ErrStateConflict
		}
		var replay entity.Resource
		var replayed bool
		var reserveErr error
		consumption, replay, replayed, reserveErr = reserveProviderCommandReceipt(
			ctx, tx, protected, input.Principal, input.ProviderReceipt,
			strings.ToLower(string(input.Kind)), "", stableKey, service.now().UTC(),
		)
		if reserveErr != nil || replayed {
			return replay, reserveErr
		}
	} else if input.Action == "reconcile_git" {
		var replay entity.Resource
		var replayed bool
		var reserveErr error
		consumption, replay, replayed, reserveErr = reserveGitCommandReceipt(
			ctx, tx, protected, input.Principal, input.GitReceipt,
			strings.ToLower(string(input.Kind)), "", stableKey, service.now().UTC(),
		)
		if reserveErr != nil || replayed {
			return replay, reserveErr
		}
	}
	spec, err := service.resolveProtectedSpec(ctx, tx, protected, input, entity.Resource{})
	if err != nil {
		return entity.Resource{}, err
	}
	if err := service.ensureInstructionArtifact(ctx, tx, input, spec); err != nil {
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
	if consumption.ReceiptID != "" {
		if err := finalizeExternalCommandReceipt(ctx, protected, consumption, created); err != nil {
			return entity.Resource{}, err
		}
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
	var consumption domainrepo.ExternalCommandReceipt
	if input.ProviderReceipt.ContractVersion != 0 {
		stableKey, ok := protectedConfigurationStableKey(current.Spec)
		if !ok {
			return entity.Resource{}, errs.ErrStateConflict
		}
		var replay entity.Resource
		var replayed bool
		consumption, replay, replayed, err = reserveProviderCommandReceipt(
			ctx, tx, protected, input.Principal, input.ProviderReceipt,
			strings.ToLower(string(input.Kind)), current.ID, stableKey, service.now().UTC(),
		)
		if err != nil || replayed {
			return replay, err
		}
	} else if input.Action == "reconcile_git" {
		stableKey, ok := protectedConfigurationStableKey(current.Spec)
		if !ok {
			return entity.Resource{}, errs.ErrStateConflict
		}
		var replay entity.Resource
		var replayed bool
		consumption, replay, replayed, err = reserveGitCommandReceipt(
			ctx, tx, protected, input.Principal, input.GitReceipt,
			strings.ToLower(string(input.Kind)), current.ID, stableKey, service.now().UTC(),
		)
		if err != nil || replayed {
			return replay, err
		}
	}
	if current.Version != input.ExpectedVersion {
		if input.Action == "reconcile_git" {
			return service.persistGitReconciliationDrift(ctx, tx, protected, input, current, consumption)
		}
		return entity.Resource{}, errs.ErrVersionMismatch
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	var updated entity.Resource
	switch input.Action {
	case "update", "refresh", "reconcile_git":
		spec, resolveErr := service.resolveProtectedSpec(ctx, tx, protected, input, current)
		if resolveErr != nil {
			if input.Action == "reconcile_git" {
				return service.persistGitReconciliationDrift(ctx, tx, protected, input, current, consumption)
			}
			return entity.Resource{}, resolveErr
		}
		if ensureErr := service.ensureInstructionArtifact(ctx, tx, input, spec); ensureErr != nil {
			if input.Action == "reconcile_git" {
				return service.persistGitReconciliationDrift(ctx, tx, protected, input, current, consumption)
			}
			return entity.Resource{}, ensureErr
		}
		currentStableKey, currentHasStableKey := protectedConfigurationStableKey(current.Spec)
		nextStableKey, nextHasStableKey := protectedConfigurationStableKey(spec)
		if currentHasStableKey != nextHasStableKey ||
			(currentHasStableKey && currentStableKey != nextStableKey) {
			if input.Action == "reconcile_git" {
				return service.persistGitReconciliationDrift(ctx, tx, protected, input, current, consumption)
			}
			return entity.Resource{}, errs.ErrStateConflict
		}
		if input.Action == "update" || input.Action == "reconcile_git" {
			spec, resolveErr = configurationUpdateSpec(ctx, tx, input.Principal, current.Spec, spec, false)
			if resolveErr != nil {
				if input.Action == "reconcile_git" {
					return service.persistGitReconciliationDrift(ctx, tx, protected, input, current, consumption)
				}
				return entity.Resource{}, resolveErr
			}
		}
		name := current.Name
		if input.Action == "update" || input.Action == "reconcile_git" {
			name = input.Name
		}
		updated, err = current.Update(name, spec, now)
	case "validate", "publish", "rollback", "detach":
		updated, err = service.transitionInstructionSet(ctx, tx, protected, input, current, now)
	case "pause", "resume", "enable", "disable", "bind_bot", "rebind_bot", "revoke_bot":
		updated, err = service.transitionAgent(ctx, tx, input, current, now)
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
	if consumption.ReceiptID != "" {
		if err := finalizeExternalCommandReceipt(ctx, protected, consumption, updated); err != nil {
			return entity.Resource{}, err
		}
	}
	return updated, nil
}

// persistGitReconciliationDrift фиксирует отрицательный результат сравнения в
// той же owner transaction, которая locked exact protected resource. Gateway
// получает только сохранённую проекцию и не вычисляет drift из transport error.
func (service *Service) persistGitReconciliationDrift(
	ctx context.Context,
	tx domainrepo.Transaction,
	protected domainrepo.ProtectedTransaction,
	input ManageProtectedConfigurationInput,
	current entity.Resource,
	consumption domainrepo.ExternalCommandReceipt,
) (entity.Resource, error) {
	drifted, err := markGitConfigurationDrift(current, service.now().UTC().Truncate(time.Microsecond))
	if err != nil {
		return entity.Resource{}, err
	}
	if err := tx.Update(ctx, drifted, current.Version); err != nil {
		return entity.Resource{}, err
	}
	if err := service.appendProtectedRecords(ctx, tx, protected, input.Principal, "reconcile_git", drifted); err != nil {
		return entity.Resource{}, err
	}
	if consumption.ReceiptID != "" {
		if err := finalizeExternalCommandReceipt(ctx, protected, consumption, drifted); err != nil {
			return entity.Resource{}, err
		}
	}
	return drifted, nil
}

func markGitConfigurationDrift(current entity.Resource, now time.Time) (entity.Resource, error) {
	configured, ok := current.Spec.(entity.ConfiguredSpec)
	if !ok {
		return entity.Resource{}, errs.ErrStateConflict
	}
	ownership := configured.ConfigurationOwnership()
	if ownership.ManagedBy != "GIT" || ownership.AuthoritativeDrift() != "IN_SYNC" {
		return entity.Resource{}, errs.ErrStateConflict
	}
	ownership.Drift = "DRIFTED"
	spec, err := entity.WithConfigurationOwnership(current.Spec, ownership)
	if err != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	drifted, err := current.Update(current.Name, spec, now)
	if err != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	return drifted, nil
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

func (service *Service) prepareInstructionArtifact(
	ctx context.Context,
	input ManageProtectedConfigurationInput,
) (ManageProtectedConfigurationInput, error) {
	spec, ok := input.Spec.(entity.InstructionSetSpec)
	if !ok || spec.ContentArtifactID != "" || spec.ContentArtifactVersion != 0 ||
		value.ValidateStableKey(spec.StableKey) != nil || hashString(spec.Content) != spec.ContentSHA256 {
		return ManageProtectedConfigurationInput{}, errs.ErrInvalidInput
	}
	if service.instructionObjects == nil {
		return ManageProtectedConfigurationInput{}, errs.ErrUnavailable
	}
	artifactID := instructionArtifactID(input.Principal.ProjectID, spec.StableKey, spec.ContentSHA256)
	object, err := service.instructionObjects.Put(ctx, input.Principal.ProjectID,
		"instruction-sets/"+spec.StableKey+"/"+spec.ContentSHA256+".md", []byte(spec.Content),
		"text/markdown", spec.ContentSHA256)
	if err != nil || object.Reference == "" || object.VersionID == "" || object.SHA256 != spec.ContentSHA256 ||
		object.Size != uint64(len([]byte(spec.Content))) || object.MediaType != "text/markdown" {
		return ManageProtectedConfigurationInput{}, errs.ErrUnavailable
	}
	spec.ContentArtifactID, spec.ContentArtifactVersion = artifactID, 1
	input.Spec, input.InstructionObject = spec, object
	return input, nil
}

// instructionArtifactID использует тот же dedup domain, что и object key:
// project + InstructionSet stable key + immutable content digest. Поэтому два
// независимых InstructionSet с одинаковым Markdown не делят metadata identity.
func instructionArtifactID(projectID, instructionStableKey, contentSHA256 string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("mattercodex:instruction-artifact:"+
		projectID+":"+instructionStableKey+":"+contentSHA256)).String()
}

func (service *Service) ensureInstructionArtifact(
	ctx context.Context,
	tx domainrepo.Transaction,
	input ManageProtectedConfigurationInput,
	spec entity.Spec,
) error {
	instruction, ok := spec.(entity.InstructionSetSpec)
	if !ok || input.InstructionObject.Reference == "" {
		return nil
	}
	if instruction.ContentArtifactID == "" || instruction.ContentArtifactVersion != 1 ||
		instruction.ContentSHA256 != input.InstructionObject.SHA256 {
		return errs.ErrStateConflict
	}
	existing, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		instruction.ContentArtifactID)
	if err == nil {
		artifact, cast := existing.Spec.(entity.ArtifactSpec)
		if !cast || existing.Kind != enum.KindArtifact || existing.OwnerActorID != input.Principal.ActorID ||
			existing.Version != instruction.ContentArtifactVersion || existing.State != enum.StateActive ||
			artifact.Direction != "INPUT" || artifact.MediaType != input.InstructionObject.MediaType ||
			artifact.StorageRef != input.InstructionObject.Reference || artifact.SHA256 != input.InstructionObject.SHA256 ||
			artifact.SizeBytes != input.InstructionObject.Size || artifact.ScanStatus != "CLEAN" {
			return errs.ErrStateConflict
		}
		return nil
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return err
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	evidence, err := canonicalHash(struct {
		Reference, VersionID, SHA256, Validator string
		Size                                    uint64
	}{
		input.InstructionObject.Reference, input.InstructionObject.VersionID,
		input.InstructionObject.SHA256, "control-plane-instruction-validator-v1", input.InstructionObject.Size,
	})
	if err != nil {
		return errs.ErrInternal
	}
	artifact, err := entity.New(instruction.ContentArtifactID, input.Principal.OrganizationID,
		input.Principal.ProjectID, input.Principal.ProjectID, input.Principal.ActorID, enum.KindArtifact,
		"Instruction content "+strconv.FormatUint(instruction.CurrentVersion, 10), entity.ArtifactSpec{
			ArtifactKind: "instruction-content", Direction: "INPUT", StorageRef: input.InstructionObject.Reference,
			SizeBytes: input.InstructionObject.Size, MediaType: input.InstructionObject.MediaType,
			SHA256: input.InstructionObject.SHA256, ScanStatus: "CLEAN",
			RetentionPolicyRef: "control-plane://retention/instruction-content", ScanPolicyRevision: 1,
			ScanEvidenceSHA256: evidence, ScannerWorkloadID: "control-plane-instruction-validator", ScannedAt: now,
		}, now)
	if err != nil {
		return errs.ErrInternal
	}
	if err := tx.Insert(ctx, artifact); err != nil {
		return err
	}
	return service.appendMutationRecords(ctx, tx, input.Principal, "materialize_instruction_content", artifact)
}

func reserveProviderCommandReceipt(
	ctx context.Context,
	_ domainrepo.Transaction,
	protected domainrepo.ProtectedTransaction,
	principal value.Principal,
	receipt value.ProviderEffectReceipt,
	targetKind, targetResourceID, targetStableKey string,
	now time.Time,
) (domainrepo.ExternalCommandReceipt, entity.Resource, bool, error) {
	if receipt.TargetKind != targetKind || receipt.TargetResourceID != targetResourceID ||
		receipt.TargetStableKey != targetStableKey {
		return domainrepo.ExternalCommandReceipt{}, entity.Resource{}, false, errs.ErrPermissionDenied
	}
	consumption := providerCommandReceiptConsumption(principal, receipt, now)
	stored, reserved, err := protected.ReserveExternalCommandReceipt(ctx, consumption)
	if err != nil {
		return domainrepo.ExternalCommandReceipt{}, entity.Resource{}, false, err
	}
	if reserved {
		return consumption, entity.Resource{}, false, nil
	}
	result, err := externalCommandReceiptReplay(stored, consumption)
	if err != nil {
		return domainrepo.ExternalCommandReceipt{}, entity.Resource{}, false, err
	}
	return stored, result, true, nil
}

func providerCommandReceiptConsumption(
	principal value.Principal,
	receipt value.ProviderEffectReceipt,
	now time.Time,
) domainrepo.ExternalCommandReceipt {
	return domainrepo.ExternalCommandReceipt{
		Issuer: receipt.Issuer, Purpose: receipt.Purpose, ReceiptID: receipt.ReceiptID,
		OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID, OwnerActorID: principal.ActorID,
		TargetKind: receipt.TargetKind, TargetResourceID: receipt.TargetResourceID,
		TargetStableKey: receipt.TargetStableKey, Action: receipt.Action, Effect: receipt.Effect,
		EffectGeneration: receipt.EffectGeneration, EffectSHA256: receipt.EffectSHA256,
		CommandIntentSHA256: receipt.CommandIntentSHA256, AuthoritySHA256: principal.AuthorityDigest,
		ConsumedAt: now.UTC().Truncate(time.Microsecond),
	}
}

func externalCommandReceiptReplay(
	stored domainrepo.ExternalCommandReceipt,
	expected domainrepo.ExternalCommandReceipt,
) (entity.Resource, error) {
	if stored.Issuer != expected.Issuer || stored.Purpose != expected.Purpose ||
		stored.ReceiptID != expected.ReceiptID ||
		stored.OrganizationID != expected.OrganizationID || stored.ProjectID != expected.ProjectID ||
		stored.OwnerActorID != expected.OwnerActorID || stored.TargetKind != expected.TargetKind ||
		stored.TargetResourceID != expected.TargetResourceID || stored.TargetStableKey != expected.TargetStableKey ||
		stored.Action != expected.Action || stored.Effect != expected.Effect ||
		stored.EffectGeneration != expected.EffectGeneration || stored.EffectSHA256 != expected.EffectSHA256 ||
		stored.CommandIntentSHA256 != expected.CommandIntentSHA256 || stored.AuthoritySHA256 != expected.AuthoritySHA256 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if stored.ResultResourceID == "" || stored.ResultVersion == 0 || !validSHA256Text(stored.ResultSHA256) {
		return entity.Resource{}, errs.ErrStateConflict
	}
	result := stored.Result
	digest, err := entity.ProjectionSHA256(result)
	if err != nil || result.ID != stored.ResultResourceID || result.Version != stored.ResultVersion ||
		result.OrganizationID != expected.OrganizationID || result.ProjectID != expected.ProjectID ||
		result.OwnerActorID != expected.OwnerActorID || digest != stored.ResultSHA256 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	return result, nil
}

func replayProviderCommandReceipt(
	ctx context.Context,
	protected domainrepo.ProtectedTransaction,
	principal value.Principal,
	receipt value.ProviderEffectReceipt,
	targetKind, targetResourceID, targetStableKey string,
	now time.Time,
) (entity.Resource, bool, error) {
	if receipt.TargetKind != targetKind || receipt.TargetResourceID != targetResourceID ||
		receipt.TargetStableKey != targetStableKey {
		return entity.Resource{}, false, errs.ErrPermissionDenied
	}
	expected := providerCommandReceiptConsumption(principal, receipt, now)
	stored, err := protected.GetExternalCommandReceipt(ctx, receipt.Issuer, receipt.Purpose, receipt.ReceiptID)
	if errors.Is(err, errs.ErrNotFound) {
		return entity.Resource{}, false, nil
	}
	if err != nil {
		return entity.Resource{}, false, err
	}
	result, err := externalCommandReceiptReplay(stored, expected)
	return result, err == nil, err
}

func reserveGitCommandReceipt(
	ctx context.Context,
	tx domainrepo.Transaction,
	protected domainrepo.ProtectedTransaction,
	principal value.Principal,
	receipt value.GitReconciliationReceipt,
	targetKind, targetResourceID, targetStableKey string,
	now time.Time,
) (domainrepo.ExternalCommandReceipt, entity.Resource, bool, error) {
	return reserveProviderCommandReceipt(ctx, tx, protected, principal, value.ProviderEffectReceipt{
		Issuer: receipt.Issuer, Purpose: receipt.Purpose, ReceiptID: receipt.ReceiptID,
		TargetKind: receipt.TargetKind, TargetResourceID: receipt.TargetResourceID,
		TargetStableKey: receipt.TargetStableKey, Action: "reconcile_git", Effect: "git_configuration",
		EffectGeneration: receipt.SourceRevision, EffectSHA256: receipt.SourceSHA256,
		CommandIntentSHA256: receipt.CommandIntentSHA256,
	}, targetKind, targetResourceID, targetStableKey, now)
}

func replayGitCommandReceipt(
	ctx context.Context,
	protected domainrepo.ProtectedTransaction,
	principal value.Principal,
	receipt value.GitReconciliationReceipt,
	targetKind, targetResourceID, targetStableKey string,
	now time.Time,
) (entity.Resource, bool, error) {
	return replayProviderCommandReceipt(ctx, protected, principal, value.ProviderEffectReceipt{
		Issuer: receipt.Issuer, Purpose: receipt.Purpose, ReceiptID: receipt.ReceiptID,
		TargetKind: receipt.TargetKind, TargetResourceID: receipt.TargetResourceID,
		TargetStableKey: receipt.TargetStableKey, Action: "reconcile_git", Effect: "git_configuration",
		EffectGeneration: receipt.SourceRevision, EffectSHA256: receipt.SourceSHA256,
		CommandIntentSHA256: receipt.CommandIntentSHA256,
	}, targetKind, targetResourceID, targetStableKey, now)
}

func protectedExternalSemanticReplay(input ManageProtectedConfigurationInput) bool {
	if input.ResourceID == "" {
		return false
	}
	if input.Action == "reconcile_git" {
		return input.Kind == enum.KindRoleDefinition || input.Kind == enum.KindAgent ||
			input.Kind == enum.KindInstructionSet || input.Kind == enum.KindProviderPool
	}
	if input.Kind == enum.KindAgent {
		return input.Action == "bind_bot" || input.Action == "rebind_bot" || input.Action == "revoke_bot"
	}
	return input.Kind == enum.KindProviderReference && input.Action == "refresh" ||
		input.Kind == enum.KindProviderPool && (input.Action == "update" || input.Action == "archive" || input.Action == "delete")
}

// replayProtectedExternalCommand выполняется до generic idempotency и любых
// InstructionSet object effects. Транзакция только читает owner-locked source
// и immutable one-use consumption; первая execution по-прежнему фиксирует все
// записи одной последующей owner transaction.
func (service *Service) replayProtectedExternalCommand(
	ctx context.Context,
	input ManageProtectedConfigurationInput,
) (entity.Resource, bool, error) {
	if !protectedExternalSemanticReplay(input) {
		return entity.Resource{}, false, nil
	}
	var result entity.Resource
	replayed := false
	err := service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID,
		ProjectID:      input.Principal.ProjectID,
		ActorID:        input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		current, err := tx.GetForUpdateIncludingDeleted(ctx, input.Principal.OrganizationID,
			input.Principal.ProjectID, input.ResourceID)
		if err != nil {
			return err
		}
		if current.Kind != input.Kind || current.OwnerActorID != input.Principal.ActorID {
			return errs.ErrNotFound
		}
		if current.Version < input.ExpectedVersion {
			return errs.ErrVersionMismatch
		}
		stableKey, ok := protectedConfigurationStableKey(current.Spec)
		if !ok {
			return errs.ErrStateConflict
		}
		protected, ok := tx.(domainrepo.ProtectedTransaction)
		if !ok {
			return errs.ErrInternal
		}
		now := service.now().UTC()
		if input.Action == "reconcile_git" {
			result, replayed, err = replayGitCommandReceipt(ctx, protected, input.Principal,
				input.GitReceipt, strings.ToLower(string(input.Kind)), current.ID, stableKey, now)
		} else {
			switch input.Kind {
			case enum.KindAgent:
				if !service.interactionGatewayPrincipal(input.Principal) ||
					input.Principal.AuthoritySource != "PROVIDER_READBACK" {
					return errs.ErrPermissionDenied
				}
				err = validateProviderReceipt(input.Principal, input.ProviderReceipt,
					"MATTERMOST_PROVIDER_READBACK_RECEIPT", input.Action,
					"agent_bot_identity", input.FullMethod, now)
			case enum.KindProviderReference:
				if input.Principal.CallerWorkload != service.integrationGatewayWorkload ||
					input.Principal.CallerSPIFFEID != service.integrationGatewaySPIFFEID ||
					input.Principal.AuthoritySource != "PROVIDER_READBACK" {
					return errs.ErrPermissionDenied
				}
				err = validateProviderReceipt(input.Principal, input.ProviderReceipt,
					"AI_PROVIDER_READBACK_RECEIPT", input.Action,
					"provider_connection_reference", input.FullMethod, now)
			case enum.KindProviderPool:
				if input.Principal.CallerWorkload != service.integrationGatewayWorkload ||
					input.Principal.CallerSPIFFEID != service.integrationGatewaySPIFFEID ||
					input.Principal.AuthoritySource != "PROVIDER_READBACK" {
					return errs.ErrPermissionDenied
				}
				err = validateProviderReceipt(input.Principal, input.ProviderReceipt,
					"AI_PROVIDER_READBACK_RECEIPT", input.Action, "pool-observation", input.FullMethod, now)
			default:
				return errs.ErrStateConflict
			}
			if err == nil {
				result, replayed, err = replayProviderCommandReceipt(ctx, protected, input.Principal,
					input.ProviderReceipt, strings.ToLower(string(input.Kind)), current.ID, stableKey, now)
			}
		}
		if err != nil || !replayed {
			return err
		}
		if result.ID != current.ID || result.Kind != input.Kind ||
			result.Version != input.ExpectedVersion+1 || result.OwnerActorID != input.Principal.ActorID {
			return errs.ErrStateConflict
		}
		return nil
	})
	return result, replayed && err == nil, err
}

func finalizeExternalCommandReceipt(
	ctx context.Context,
	protected domainrepo.ProtectedTransaction,
	consumption domainrepo.ExternalCommandReceipt,
	result entity.Resource,
) error {
	digest, err := entity.ProjectionSHA256(result)
	if err != nil {
		return errs.ErrInternal
	}
	consumption.ResultResourceID, consumption.ResultVersion, consumption.ResultSHA256 = result.ID, result.Version, digest
	consumption.Result = result
	return protected.FinalizeExternalCommandReceipt(ctx, consumption)
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
		if !ok {
			return nil, errs.ErrInvalidInput
		}
		var ownershipErr error
		spec.Ownership, ownershipErr = normalizedProtectedOwnership(spec.Ownership, input.Action, current.ID == "")
		if ownershipErr != nil || spec.Validate() != nil {
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
				recipe.OwnerActorID != input.Principal.ActorID ||
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
		spec.OwnerRoleSelector, spec.OwnerInstructionSelector, spec.OwnerProviderPoolSelector =
			input.ReferenceKeys[0], input.ReferenceKeys[1], input.ReferenceKeys[2]
		if roleSpec.RoleImageRecipeID == "" {
			return nil, errs.ErrStateConflict
		}
		runtimeProfile, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, roleSpec.RoleImageRecipeID)
		if err != nil {
			return nil, err
		}
		profileID, profileVersion, profileSHA, err := protectedTuple(runtimeProfile)
		if err != nil || runtimeProfile.Kind != enum.KindRoleImageRecipe || runtimeProfile.State != enum.StateActive ||
			runtimeProfile.OwnerActorID != input.Principal.ActorID ||
			profileVersion != roleSpec.RoleImageRecipeVersion || profileSHA != roleSpec.RoleImageRecipeSHA256 {
			return nil, errs.ErrStateConflict
		}
		spec.RuntimeProfileRef = "control-plane://runtime-profile/" + profileID
		spec.RuntimeProfileVersion, spec.RuntimeProfileSHA256 = profileVersion, profileSHA
		if current.ID == "" && (spec.BotIdentityRef != "" || spec.BotUsername != "" || spec.BotProviderRevision != 0 ||
			spec.BotProviderGeneration != 0 || spec.BotProviderTeamRef != "" ||
			spec.BotMaskedStatus != "" || spec.BotReceiptID != "" || spec.BotReceiptVersion != 0 || spec.BotReceiptSHA256 != "") {
			return nil, errs.ErrInvalidInput
		}
		if current.ID == "" {
			spec.Enabled = true
		}
		if current.ID != "" {
			currentSpec, currentOK := current.Spec.(entity.AgentSpec)
			if !currentOK {
				return nil, errs.ErrStateConflict
			}
			spec.BotIdentityRef, spec.BotUsername, spec.BotProviderRevision, spec.BotProviderGeneration,
				spec.BotProviderTeamRef, spec.BotMaskedStatus =
				currentSpec.BotIdentityRef, currentSpec.BotUsername, currentSpec.BotProviderRevision,
				currentSpec.BotProviderGeneration, currentSpec.BotProviderTeamRef, currentSpec.BotMaskedStatus
			spec.BotReceiptID, spec.BotReceiptVersion, spec.BotReceiptSHA256 =
				currentSpec.BotReceiptID, currentSpec.BotReceiptVersion, currentSpec.BotReceiptSHA256
			spec.Enabled = currentSpec.Enabled
		}
		var ownershipErr error
		spec.Ownership, ownershipErr = normalizedProtectedOwnership(spec.Ownership, input.Action, current.ID == "")
		if ownershipErr != nil {
			return nil, errs.ErrInvalidInput
		}
		if spec.Validate() != nil {
			return nil, errs.ErrInvalidInput
		}
		return spec, nil
	case enum.KindAgentAssignment:
		if len(input.ReferenceKeys) != 2 {
			return nil, errs.ErrInvalidInput
		}
		agent, err := requireProtectedStable(ctx, protected, input.Principal, enum.KindAgent, input.ReferenceKeys[0])
		if err != nil {
			return nil, err
		}
		workspace, workspaceSHA, err := lockActiveWorkspace(ctx, tx, input.Principal)
		if err != nil {
			return nil, err
		}
		roomID := ""
		if input.ReferenceKeys[1] != "" {
			room, err := requireProtectedStable(ctx, protected, input.Principal, enum.KindChat, input.ReferenceKeys[1])
			if err != nil {
				return nil, err
			}
			roomID = room.ID
		}
		agentID, agentVersion, agentSHA, err := protectedTuple(agent)
		if err != nil {
			return nil, err
		}
		return entity.AgentAssignmentSpec{
			AgentID: agentID, AgentVersion: agentVersion, AgentSHA256: agentSHA,
			WorkspaceID: workspace.ID, WorkspaceVersion: workspace.Version, WorkspaceSHA256: workspaceSHA,
			RoomID: roomID, RootActorID: input.Principal.ActorID, AssignmentGeneration: 1,
		}, nil
	case enum.KindInstructionSet:
		spec, ok := input.Spec.(entity.InstructionSetSpec)
		if !ok || input.Action != "create" && input.Action != "update" && input.Action != "reconcile_git" {
			return nil, errs.ErrInvalidInput
		}
		if spec.ContentArtifactID == "" || spec.ContentArtifactVersion != 1 {
			return nil, errs.ErrInvalidInput
		}
		if current.ID == "" {
			spec.CurrentVersion, spec.PublishedVersion, spec.VersionState = 1, 0, "DRAFT"
			spec.ValidationSHA256, spec.RollbackOfVersion = "", 0
			spec.ValidationSucceeded, spec.ValidatedContentVersion, spec.ValidatedContentSHA256, spec.ValidationErrors = false, 0, "", nil
		} else {
			currentSpec, ok := current.Spec.(entity.InstructionSetSpec)
			if !ok || currentSpec.VersionState == "ARCHIVED" {
				return nil, errs.ErrStateConflict
			}
			if currentSpec.Ownership.ManagedBy == "GIT" && input.Action != "reconcile_git" {
				return nil, errs.ErrPermissionDenied
			}
			spec.CurrentVersion = currentSpec.CurrentVersion + 1
			spec.PublishedVersion = currentSpec.PublishedVersion
			spec.VersionState, spec.ValidationSHA256, spec.RollbackOfVersion = "DRAFT", "", 0
			spec.ValidationSucceeded, spec.ValidatedContentVersion, spec.ValidatedContentSHA256, spec.ValidationErrors = false, 0, "", nil
		}
		var ownershipErr error
		spec.Ownership, ownershipErr = normalizedProtectedOwnership(spec.Ownership, input.Action, current.ID == "")
		if ownershipErr != nil {
			return nil, errs.ErrInvalidInput
		}
		if spec.Validate() != nil {
			return nil, errs.ErrInvalidInput
		}
		return spec, validateConfigurationCreate(ctx, tx, input.Principal, spec)
	case enum.KindProviderReference:
		spec, ok := input.Spec.(entity.ProviderConnectionReferenceSpec)
		if !ok || input.Principal.CallerWorkload != service.integrationGatewayWorkload ||
			input.Principal.CallerSPIFFEID != service.integrationGatewaySPIFFEID ||
			input.Principal.AuthoritySource != "PROVIDER_READBACK" ||
			input.Principal.AuthorityReference == "" || input.Principal.AuthorityRevision == 0 ||
			!validSHA256Text(input.Principal.AuthorityDigest) {
			return nil, errs.ErrPermissionDenied
		}
		if err := validateProviderReceipt(input.Principal, input.ProviderReceipt,
			"AI_PROVIDER_READBACK_RECEIPT", input.Action, "provider_connection_reference",
			input.FullMethod, service.now().UTC()); err != nil {
			return nil, err
		}
		if spec.ReceiptID != "" && spec.ReceiptID != input.Principal.AuthorityReference ||
			spec.ReceiptVersion != 0 && spec.ReceiptVersion != input.Principal.AuthorityRevision ||
			spec.ReceiptSHA256 != "" && spec.ReceiptSHA256 != input.Principal.AuthorityDigest {
			return nil, errs.ErrStateConflict
		}
		spec.ReceiptID, spec.ReceiptVersion, spec.ReceiptSHA256 = input.Principal.AuthorityReference,
			input.Principal.AuthorityRevision, input.Principal.AuthorityDigest
		spec.ServerReference, spec.ReferenceVersion, spec.ReferenceGeneration, spec.ReferenceSHA256 =
			input.ProviderReceipt.ProviderObjectRef, input.ProviderReceipt.EffectVersion,
			input.ProviderReceipt.EffectGeneration, input.ProviderReceipt.EffectSHA256
		if input.ProviderReceipt.Provider == "" || input.ProviderReceipt.MaskedLabel == "" ||
			(input.ProviderReceipt.MaskedStatus != "AVAILABLE" && input.ProviderReceipt.MaskedStatus != "DEGRADED" &&
				input.ProviderReceipt.MaskedStatus != "INELIGIBLE" && input.ProviderReceipt.MaskedStatus != "ARCHIVED") {
			return nil, errs.ErrStateConflict
		}
		spec.Provider, spec.MaskedLabel, spec.MaskedStatus = input.ProviderReceipt.Provider,
			input.ProviderReceipt.MaskedLabel, input.ProviderReceipt.MaskedStatus
		spec.Capabilities, spec.Eligible, spec.ObservedAt = slices.Clone(input.ProviderReceipt.Capabilities),
			input.ProviderReceipt.Eligible, input.ProviderReceipt.IssuedAt
		spec.ObservedUsage, spec.ObservedLimit = input.ProviderReceipt.ObservedUsage, input.ProviderReceipt.ObservedLimit
		spec.ObservationRevision, spec.ObservationExpiresAt = input.ProviderReceipt.ObservationRevision, input.ProviderReceipt.ObservationExpiresAt
		spec.WindowDurationSeconds, spec.ResetsAt = input.ProviderReceipt.WindowDurationSeconds, input.ProviderReceipt.ResetsAt
		spec.ObservationSHA256 = input.ProviderReceipt.ObservationSHA256
		if input.Action != "archive" {
			binding, bindingErr := service.materializeProviderCredential(ctx, tx, protected, input)
			if bindingErr != nil {
				return nil, bindingErr
			}
			bindingSHA, digestErr := entity.ProjectionSHA256(binding)
			if digestErr != nil {
				return nil, errs.ErrInternal
			}
			spec.CredentialBindingID, spec.CredentialBindingVersion, spec.CredentialBindingSHA256 =
				binding.ID, binding.Version, bindingSHA
		}
		if current.ID != "" {
			currentSpec, ok := current.Spec.(entity.ProviderConnectionReferenceSpec)
			if !ok || spec.ReferenceVersion <= currentSpec.ReferenceVersion ||
				spec.ReferenceGeneration <= currentSpec.ReferenceGeneration ||
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
		requested := make(map[string]entity.ProviderPoolBinding, len(input.ReferenceKeys))
		for index, key := range input.ReferenceKeys {
			if value.ValidateStableKey(key) != nil || spec.Bindings[index].Weight == 0 {
				return nil, errs.ErrInvalidInput
			}
			requested[key] = spec.Bindings[index]
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
			candidate := requested[key]
			if !ok || !referenceSpec.Eligible || now.Sub(referenceSpec.ObservedAt) > spec.ObservationMaxAge ||
				!referenceSpec.ObservationExpiresAt.After(now) || candidate.ObservedUsage != referenceSpec.ObservedUsage ||
				candidate.ObservedLimit != referenceSpec.ObservedLimit || candidate.ObservationRevision != referenceSpec.ObservationRevision ||
				!candidate.ObservedAt.Equal(referenceSpec.ObservedAt) || !candidate.ObservationExpiresAt.Equal(referenceSpec.ObservationExpiresAt) ||
				candidate.ObservationSHA256 != referenceSpec.ObservationSHA256 || candidate.WindowDurationSeconds != referenceSpec.WindowDurationSeconds ||
				!candidate.ResetsAt.Equal(referenceSpec.ResetsAt) {
				return nil, errs.ErrStateConflict
			}
			referenceID, referenceVersion, referenceSHA, err := protectedTuple(reference)
			if err != nil {
				return nil, err
			}
			spec.Bindings = append(spec.Bindings, entity.ProviderPoolBinding{
				ProviderConnectionReferenceID: referenceID, ReferenceVersion: referenceVersion,
				ProviderConnectionStableKey: key,
				ReferenceSHA256:             referenceSHA, Weight: candidate.Weight, Eligible: true,
				MaskedStatus:  referenceSpec.MaskedStatus,
				ObservedUsage: referenceSpec.ObservedUsage, ObservedLimit: referenceSpec.ObservedLimit,
				ObservationRevision: referenceSpec.ObservationRevision, ObservedAt: referenceSpec.ObservedAt,
				ObservationExpiresAt: referenceSpec.ObservationExpiresAt, ObservationSHA256: referenceSpec.ObservationSHA256,
				WindowDurationSeconds: referenceSpec.WindowDurationSeconds, ResetsAt: referenceSpec.ResetsAt,
			})
		}
		snapshot := spec
		snapshot.EligibilitySnapshotSHA256 = strings.Repeat("0", 64)
		digest, err := canonicalHash(snapshot)
		if err != nil {
			return nil, errs.ErrInternal
		}
		spec.EligibilitySnapshotSHA256 = digest
		var ownershipErr error
		spec.Ownership, ownershipErr = normalizedProtectedOwnership(spec.Ownership, input.Action, current.ID == "")
		if ownershipErr != nil {
			return nil, errs.ErrInvalidInput
		}
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

func normalizedProtectedOwnership(ownership entity.ConfigurationOwnership, action string, creating bool) (entity.ConfigurationOwnership, error) {
	if action == "reconcile_git" {
		if ownership.ManagedBy != "GIT" || ownership.Validate() != nil || !validSHA256Text(ownership.SourceSHA256) {
			return entity.ConfigurationOwnership{}, errs.ErrInvalidInput
		}
		// Успешная owner transaction применяет exact signed Git revision;
		// только этот authoritative path может объявить её синхронной.
		ownership.Drift = "IN_SYNC"
		return ownership, nil
	}
	if creating {
		if ownership.ManagedBy != "UI" || ownership.SourceRef != "" || ownership.SourceRevision != 0 || ownership.SourceSHA256 != "" {
			return entity.ConfigurationOwnership{}, errs.ErrInvalidInput
		}
		return entity.ConfigurationOwnership{ManagedBy: "UI", Drift: "NOT_APPLICABLE"}, nil
	}
	if ownership.ManagedBy != "UI" {
		return entity.ConfigurationOwnership{}, errs.ErrInvalidInput
	}
	ownership.Drift = "NOT_APPLICABLE"
	return ownership, nil
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
		if spec.VersionState != "DRAFT" || input.TargetVersion != spec.CurrentVersion || input.TargetSHA256 != "" {
			return entity.Resource{}, errs.ErrStateConflict
		}
		spec.ValidationErrors = validateInstructionContent(spec.Content)
		spec.ValidationSucceeded = len(spec.ValidationErrors) == 0
		spec.ValidatedContentVersion, spec.ValidatedContentSHA256 = spec.CurrentVersion, spec.ContentSHA256
		validationDigest, digestErr := canonicalHash(struct {
			ContentVersion uint64
			ContentSHA256  string
			Succeeded      bool
			Errors         []entity.InstructionValidationError
		}{spec.CurrentVersion, spec.ContentSHA256, spec.ValidationSucceeded, spec.ValidationErrors})
		if digestErr != nil {
			return entity.Resource{}, errs.ErrInternal
		}
		spec.ValidationSHA256 = validationDigest
		if spec.ValidationSucceeded {
			spec.VersionState = "VALIDATED"
		} else {
			spec.VersionState = "REJECTED"
		}
		return current.Update(current.Name, spec, now)
	case "publish":
		if spec.VersionState != "VALIDATED" || input.TargetVersion != spec.CurrentVersion ||
			input.TargetSHA256 != "" || !spec.ValidationSucceeded || spec.ValidatedContentVersion != spec.CurrentVersion ||
			spec.ValidatedContentSHA256 != spec.ContentSHA256 || len(spec.ValidationErrors) != 0 {
			return entity.Resource{}, errs.ErrStateConflict
		}
		spec.VersionState, spec.PublishedVersion = "PUBLISHED", spec.CurrentVersion
		return current.Update(current.Name, spec, now)
	case "rollback":
		if spec.Ownership.ManagedBy == "GIT" || input.TargetVersion == 0 || input.TargetSHA256 != "" {
			return entity.Resource{}, errs.ErrPermissionDenied
		}
		target, err := protected.GetInstructionHistoryContentVersion(ctx, current.ID, input.TargetVersion)
		if err != nil {
			return entity.Resource{}, err
		}
		targetSpec, ok := target.Resource.Spec.(entity.InstructionSetSpec)
		if !ok || targetSpec.VersionState != "PUBLISHED" || !targetSpec.ValidationSucceeded ||
			targetSpec.ValidatedContentVersion != targetSpec.CurrentVersion || targetSpec.ValidatedContentSHA256 != targetSpec.ContentSHA256 {
			return entity.Resource{}, errs.ErrStateConflict
		}
		spec.CurrentVersion++
		spec.PublishedVersion = spec.CurrentVersion
		spec.Content, spec.ContentSHA256 = targetSpec.Content, targetSpec.ContentSHA256
		spec.ContentArtifactID, spec.ContentArtifactVersion = targetSpec.ContentArtifactID, targetSpec.ContentArtifactVersion
		spec.VersionState, spec.ValidationSHA256 = "PUBLISHED", targetSpec.ValidationSHA256
		spec.ValidationSucceeded, spec.ValidatedContentVersion, spec.ValidatedContentSHA256 = true, spec.CurrentVersion, targetSpec.ContentSHA256
		spec.ValidationErrors = nil
		spec.RollbackOfVersion = targetSpec.CurrentVersion
		return current.Update(current.Name, spec, now)
	case "detach":
		if spec.Ownership.ManagedBy != "GIT" {
			return entity.Resource{}, errs.ErrStateConflict
		}
		if err := requireDurablePermission(ctx, tx, input.Principal, permissionDetachConfiguration); err != nil {
			return entity.Resource{}, err
		}
		spec.Ownership = entity.ConfigurationOwnership{ManagedBy: "UI"}
		return current.Update(current.Name, spec, now)
	default:
		return entity.Resource{}, errs.ErrInvalidInput
	}
}

func (service *Service) transitionAgent(
	ctx context.Context,
	tx domainrepo.Transaction,
	input ManageProtectedConfigurationInput,
	current entity.Resource,
	now time.Time,
) (entity.Resource, error) {
	spec, ok := current.Spec.(entity.AgentSpec)
	if !ok || current.Kind != enum.KindAgent {
		return entity.Resource{}, errs.ErrStateConflict
	}
	switch input.Action {
	case "pause":
		if current.State != enum.StateActive || !spec.Enabled {
			return entity.Resource{}, errs.ErrStateConflict
		}
		return current.ReplaceAndTransition(spec, enum.StatePaused, now)
	case "resume":
		if current.State != enum.StatePaused || !spec.Enabled {
			return entity.Resource{}, errs.ErrStateConflict
		}
		return current.ReplaceAndTransition(spec, enum.StateActive, now)
	case "disable":
		if current.State != enum.StateActive && current.State != enum.StatePaused || !spec.Enabled {
			return entity.Resource{}, errs.ErrStateConflict
		}
		spec.Enabled = false
		if current.State == enum.StatePaused {
			return current.Update(current.Name, spec, now)
		}
		return current.ReplaceAndTransition(spec, enum.StatePaused, now)
	case "enable":
		if current.State != enum.StatePaused || spec.Enabled {
			return entity.Resource{}, errs.ErrStateConflict
		}
		spec.Enabled = true
		return current.ReplaceAndTransition(spec, enum.StateActive, now)
	case "bind_bot", "rebind_bot", "revoke_bot":
		if current.State != enum.StateActive && current.State != enum.StatePaused ||
			!service.interactionGatewayPrincipal(input.Principal) || input.Principal.AuthoritySource != "PROVIDER_READBACK" {
			return entity.Resource{}, errs.ErrPermissionDenied
		}
		if err := validateProviderReceipt(input.Principal, input.ProviderReceipt,
			"MATTERMOST_PROVIDER_READBACK_RECEIPT", agentBotReceiptAction(input.Action), "agent_bot_identity",
			input.FullMethod, now); err != nil {
			return entity.Resource{}, err
		}
		if input.ProviderReceipt.WorkspaceID != input.Principal.ProjectID ||
			input.ProviderReceipt.ReceiptRevision <= spec.BotReceiptVersion ||
			!validAgentBotReceiptProfile(input.ProviderReceipt) {
			return entity.Resource{}, errs.ErrStateConflict
		}
		if _, err := lockWorkspaceMappingByOpaqueRef(ctx, tx, input.Principal,
			input.ProviderReceipt.ProviderTeamRef); err != nil {
			return entity.Resource{}, err
		}
		switch input.Action {
		case "bind_bot":
			if spec.BotIdentityRef != "" || input.ProviderReceipt.MaskedStatus != "AVAILABLE" {
				return entity.Resource{}, errs.ErrStateConflict
			}
		case "rebind_bot":
			if spec.BotIdentityRef == "" || input.ProviderReceipt.MaskedStatus != "AVAILABLE" {
				return entity.Resource{}, errs.ErrStateConflict
			}
		case "revoke_bot":
			if spec.BotIdentityRef == "" || input.ProviderReceipt.ProviderObjectRef != spec.BotIdentityRef ||
				input.ProviderReceipt.ProviderTeamRef != spec.BotProviderTeamRef ||
				input.ProviderReceipt.MaskedStatus != "REVOKED" {
				return entity.Resource{}, errs.ErrStateConflict
			}
		}
		if spec.BotProviderGeneration != 0 &&
			(input.ProviderReceipt.EffectGeneration <= spec.BotProviderGeneration ||
				input.ProviderReceipt.EffectVersion <= spec.BotProviderRevision) {
			return entity.Resource{}, errs.ErrStateConflict
		}
		spec.BotIdentityRef, spec.BotUsername = input.ProviderReceipt.ProviderObjectRef, input.ProviderReceipt.ProviderUsername
		spec.BotProviderRevision, spec.BotProviderGeneration, spec.BotMaskedStatus = input.ProviderReceipt.EffectVersion,
			input.ProviderReceipt.EffectGeneration, input.ProviderReceipt.MaskedStatus
		spec.BotProviderTeamRef = input.ProviderReceipt.ProviderTeamRef
		spec.BotReceiptID, spec.BotReceiptVersion, spec.BotReceiptSHA256 = input.Principal.AuthorityReference,
			input.Principal.AuthorityRevision, input.Principal.AuthorityDigest
		if spec.Validate() != nil {
			return entity.Resource{}, errs.ErrInvalidInput
		}
		return current.Update(current.Name, spec, now)
	default:
		return entity.Resource{}, errs.ErrInvalidInput
	}
}

func validAgentBotReceiptBoundary(receipt value.ProviderEffectReceipt) bool {
	return uuid.Validate(receipt.ProviderTeamRef) == nil && uuid.Validate(receipt.ProviderObjectRef) == nil &&
		receipt.CredentialBindingID == "" && receipt.CredentialBindingVersion == 0 &&
		receipt.CredentialBindingSHA256 == "" && receipt.SecretRef == "" && receipt.SecretVersion == 0 &&
		receipt.SecretContentSHA256 == ""
}

func validAgentBotReceiptProfile(receipt value.ProviderEffectReceipt) bool {
	return validAgentBotReceiptBoundary(receipt) && receipt.Provider == "mattermost" &&
		receipt.TargetKind == "agent_bot_identity" && len(receipt.Capabilities) == 2 &&
		receipt.Capabilities[0] == "mattermost_post" && receipt.Capabilities[1] == "mattermost_readback"
}

func agentBotReceiptAction(action string) string {
	return strings.TrimSuffix(action, "_bot")
}

func validateInstructionContent(content string) []entity.InstructionValidationError {
	errorsFound := make([]entity.InstructionValidationError, 0)
	line, column := uint32(1), uint32(0)
	for _, symbol := range content {
		column++
		if symbol == '\n' {
			line, column = line+1, 0
			continue
		}
		if symbol < 0x20 && symbol != '\t' || symbol == 0x7f {
			errorsFound = append(errorsFound, entity.InstructionValidationError{
				Code: "unsupported_control_character", Field: "content", Line: line, Column: column,
				Message: "Instruction content contains an unsupported control character.",
			})
			if len(errorsFound) == 64 {
				break
			}
		}
	}
	return errorsFound
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
	copySpec.ValidationSucceeded, copySpec.ValidatedContentVersion, copySpec.ValidatedContentSHA256, copySpec.ValidationErrors = false, 0, "", nil
	copySpec.Ownership = entity.ConfigurationOwnership{ManagedBy: "UI"}
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

// materializeProviderCredential фиксирует opaque CredentialBinding в той же
// owner transaction, что и ProviderConnectionReference. Повторный receipt
// принимает только полностью совпавшее immutable поколение.
func (service *Service) materializeProviderCredential(
	ctx context.Context,
	tx domainrepo.Transaction,
	protected domainrepo.ProtectedTransaction,
	input ManageProtectedConfigurationInput,
) (entity.Resource, error) {
	receipt := input.ProviderReceipt
	materialization := controlplanecontract.ProviderCredentialMaterialization{
		CredentialBindingID: receipt.CredentialBindingID, BindingVersion: receipt.CredentialBindingVersion,
		CredentialGeneration: receipt.EffectGeneration,
		Provider:             receipt.Provider, ProviderObjectRef: receipt.ProviderObjectRef,
		SecretRef: receipt.SecretRef, SecretVersion: receipt.SecretVersion,
		SecretContentSHA256: receipt.SecretContentSHA256, MaskedAccount: receipt.MaskedAccount,
		MaskedLabel: receipt.MaskedLabel, Capabilities: slices.Clone(receipt.Capabilities),
		ObservedUsage: receipt.ObservedUsage, ObservedLimit: receipt.ObservedLimit,
		ObservationRevision: receipt.ObservationRevision, ObservedAt: receipt.ObservedAt,
		WindowSeconds: receipt.WindowDurationSeconds, ResetsAt: receipt.ResetsAt,
		ObservationExpiresAt: receipt.ObservationExpiresAt, ObservationSHA256: receipt.ObservationSHA256,
	}
	digest, err := controlplanecontract.ProviderCredentialMaterializationSHA256(materialization)
	if err != nil || digest != receipt.CredentialBindingSHA256 || receipt.CredentialBindingVersion != 1 {
		return entity.Resource{}, errs.ErrPermissionDenied
	}
	escapedRef := url.PathEscape(receipt.SecretRef)
	spec := entity.CredentialBindingSpec{
		Purpose: "provider-account", SecretRef: "vault://integration-gateway/" + escapedRef,
		PrincipalRef: "provider-account:" + receipt.Provider + ":" + receipt.ProviderObjectRef,
		Revision:     receipt.EffectGeneration, ProviderEligible: receipt.Eligible,
		ProviderCapabilities: slices.Clone(receipt.Capabilities), ProviderObservedUsage: receipt.ObservedUsage,
		ProviderObservedLimit: receipt.ObservedLimit, ProviderObservationRevision: receipt.ObservationRevision,
		ProviderObservedAt:            receipt.ObservedAt,
		ImmutableSecretRef:            "vault-versioned://integration-gateway/" + escapedRef + "/" + strconv.FormatUint(receipt.SecretVersion, 10),
		ProviderContentVersion:        "vault-version:" + strconv.FormatUint(receipt.SecretVersion, 10),
		ContentSHA256:                 receipt.SecretContentSHA256,
		ProviderWindowDurationSeconds: receipt.WindowDurationSeconds, ProviderResetsAt: receipt.ResetsAt,
		ProviderObservationExpiresAt: receipt.ObservationExpiresAt,
		Ownership: entity.ConfigurationOwnership{
			ManagedBy: "UI", SourceRef: "provider-receipt:" + receipt.ReceiptID,
			SourceRevision: receipt.ReceiptRevision, SourceSHA256: input.Principal.AuthorityDigest,
		},
	}
	if spec.Validate() != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	existing, readErr := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, receipt.CredentialBindingID)
	if readErr == nil {
		stored, ok := existing.Spec.(entity.CredentialBindingSpec)
		storedDigest, storedErr := canonicalHash(stored)
		expectedDigest, expectedErr := canonicalHash(spec)
		if !ok || existing.Kind != enum.KindCredentialBinding || existing.OwnerActorID != input.Principal.ActorID ||
			existing.State != enum.StateActive || existing.Version != 1 || storedErr != nil || expectedErr != nil || storedDigest != expectedDigest {
			return entity.Resource{}, errs.ErrStateConflict
		}
		return existing, nil
	}
	if !errors.Is(readErr, errs.ErrNotFound) {
		return entity.Resource{}, readErr
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	created, err := entity.New(receipt.CredentialBindingID, input.Principal.OrganizationID,
		input.Principal.ProjectID, input.Principal.ProjectID, input.Principal.ActorID,
		enum.KindCredentialBinding, "Provider account "+receipt.MaskedLabel, spec, now)
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if err = tx.Insert(ctx, created); err != nil {
		return entity.Resource{}, err
	}
	if err = service.appendProtectedRecords(ctx, tx, protected, input.Principal, "materialize_provider_credential", created); err != nil {
		return entity.Resource{}, err
	}
	return created, nil
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
	case entity.SessionSpec:
		return typed.AgentID == resourceID
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

// GetAgentMattermostBotIdentityReadback материализует внутренний exact Agent
// owner/receipt только для подписанного interaction-gateway source readback.
// Owner HTTP path продолжает получать safe AgentOwnerProjection из Issue #263.
func (service *Service) GetAgentMattermostBotIdentityReadback(
	ctx context.Context,
	principal value.Principal,
	agentID string,
) (entity.Resource, bool, error) {
	if !service.interactionGatewayPrincipal(principal) || principal.AuthoritySource != "PROVIDER_READBACK" {
		return entity.Resource{}, false, nil
	}
	if err := authorize(principal, permissionRead); err != nil {
		return entity.Resource{}, true, err
	}
	if value.ValidateID(agentID) != nil {
		return entity.Resource{}, true, errs.ErrInvalidInput
	}
	agent, err := service.GetProtectedConfiguration(ctx, principal, agentID, enum.KindAgent)
	if err != nil {
		return entity.Resource{}, true, err
	}
	if agent.OwnerActorID != principal.ActorID ||
		(agent.State != enum.StateActive && agent.State != enum.StatePaused) {
		return entity.Resource{}, true, errs.ErrNotFound
	}
	if _, ok := agent.Spec.(entity.AgentSpec); !ok {
		return entity.Resource{}, true, errs.ErrInternal
	}
	return agent, true, nil
}

// GetMaterializedProviderCredential возвращает только exact binding,
// материализованный специализированным provider receipt path. Generic
// credential CRUD для integration-gateway не открывается.
func (service *Service) GetMaterializedProviderCredential(
	ctx context.Context,
	principal value.Principal,
	credentialBindingID string,
) (entity.Resource, error) {
	if err := authorize(principal, permissionProviderReferenceManage); err != nil ||
		principal.CallerWorkload != service.integrationGatewayWorkload ||
		principal.CallerSPIFFEID != service.integrationGatewaySPIFFEID ||
		principal.AuthoritySource != "PROVIDER_READBACK" || value.ValidateID(credentialBindingID) != nil {
		return entity.Resource{}, errs.ErrPermissionDenied
	}
	resource, err := service.repository.Get(ctx, principal.OrganizationID, principal.ProjectID,
		credentialBindingID, enum.KindCredentialBinding)
	if err != nil {
		return entity.Resource{}, err
	}
	if resource.OwnerActorID != principal.ActorID || resource.State != enum.StateActive ||
		resource.Version != 1 {
		return entity.Resource{}, errs.ErrNotFound
	}
	if _, ok := resource.Spec.(entity.CredentialBindingSpec); !ok {
		return entity.Resource{}, errs.ErrInternal
	}
	return resource, nil
}

func (service *Service) ListProtectedConfigurations(
	ctx context.Context,
	principal value.Principal,
	kind enum.Kind,
	states []enum.State,
	afterID string,
	limit int,
) ([]entity.Resource, error) {
	if err := authorize(principal, permissionRead); err != nil {
		return nil, err
	}
	if _, kindAllowed := protectedConfigurationActions[kind]; !kindAllowed {
		return nil, errs.ErrInvalidInput
	}
	filter := query.ResourceFilter{
		OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
		ActorID: principal.ActorID, Kind: kind,
		States: states, AfterID: afterID, Limit: limit,
	}
	if principal.ProjectID == "" || filter.Validate() != nil {
		return nil, errs.ErrInvalidInput
	}
	resources, err := service.repository.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	return filterOwnerBoundResources(resources, principal.ActorID), nil
}

// ListWorkspaceMattermostMappings обслуживает принятый specialized read
// contract с permission controlplane.resource.read, не расширяя generic list.
func (service *Service) ListWorkspaceMattermostMappings(
	ctx context.Context,
	principal value.Principal,
	states []enum.State,
	afterID string,
	limit int,
) ([]entity.Resource, error) {
	if err := authorize(principal, permissionRead); err != nil {
		return nil, err
	}
	filter := query.ResourceFilter{
		OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
		ActorID: principal.ActorID, Kind: enum.KindWorkspaceMapping,
		States: states, AfterID: afterID, Limit: limit,
	}
	if principal.ProjectID == "" || filter.Validate() != nil {
		return nil, errs.ErrInvalidInput
	}
	resources, err := service.repository.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	return filterOwnerBoundResources(resources, principal.ActorID), nil
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
		input.RightVersion == 0 || input.LeftVersion == input.RightVersion ||
		input.PageSize < 1 || input.PageSize > 100 {
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
		left, leftOK := result.Left.Resource.Spec.(entity.InstructionSetSpec)
		right, rightOK := result.Right.Resource.Spec.(entity.InstructionSetSpec)
		if !leftOK || !rightOK {
			return errs.ErrStateConflict
		}
		result.ContentEqual = left.ContentSHA256 == right.ContentSHA256
		result.ComparisonSHA256, err = canonicalHash(struct {
			InstructionSetID    string
			LeftVersion         uint64
			LeftSHA256          string
			LeftSnapshotSHA256  string
			RightVersion        uint64
			RightSHA256         string
			RightSnapshotSHA256 string
		}{current.ID, input.LeftVersion, left.ContentSHA256, result.Left.SnapshotSHA256,
			input.RightVersion, right.ContentSHA256, result.Right.SnapshotSHA256})
		return err
	})
	if err != nil {
		return CompareInstructionVersionsResult{}, err
	}
	left, leftOK := result.Left.Resource.Spec.(entity.InstructionSetSpec)
	right, rightOK := result.Right.Resource.Spec.(entity.InstructionSetSpec)
	if !leftOK || !rightOK {
		return CompareInstructionVersionsResult{}, errs.ErrStateConflict
	}
	result.Page, err = service.buildConfigurationDiffPage(input.LeftVersion, left.ContentSHA256,
		result.Left.SnapshotSHA256, left.Content, input.RightVersion, right.ContentSHA256,
		result.Right.SnapshotSHA256, right.Content, result.ComparisonSHA256, input.PageToken, input.PageSize)
	return result, err
}
