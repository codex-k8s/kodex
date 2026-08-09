package resource

import (
	"context"
	"slices"
	"strings"
	"time"

	controlplanecontract "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

// ManageWorkspaceMapping сохраняет только server-resolved workspace и
// metadata signed provider readback, принадлежащего interaction-gateway.
func (service *Service) ManageWorkspaceMapping(
	ctx context.Context,
	input ManageWorkspaceMappingInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionWorkspaceMappingManage); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		(input.Action != "bind" && input.Action != "relink" && input.Action != "unlink") ||
		!service.interactionGatewayPrincipal(input.Principal) ||
		input.Principal.AuthoritySource != "PROVIDER_READBACK" ||
		input.Principal.AuthorityReference != input.ProviderReceipt.ReceiptID ||
		input.Principal.AuthorityRevision != input.ProviderReceipt.ReceiptRevision ||
		!validSHA256Text(input.Principal.AuthorityDigest) {
		return entity.Resource{}, errs.ErrPermissionDenied
	}
	if err := validateProviderReceipt(input.Principal, input.ProviderReceipt,
		"MATTERMOST_PROVIDER_READBACK_RECEIPT", input.Action,
		"workspace_mattermost_mapping", "/controlplane.v1.ControlPlaneService/ManageWorkspaceMattermostMapping", service.now().UTC()); err != nil {
		return entity.Resource{}, err
	}
	if input.Action == "bind" {
		if input.MappingID != "" || input.ExpectedVersion != 0 || input.ExpectedGeneration != 0 ||
			value.ValidateName(input.Name) != nil {
			return entity.Resource{}, errs.ErrInvalidInput
		}
	} else if value.ValidateID(input.MappingID) != nil || input.ExpectedVersion == 0 || input.ExpectedGeneration == 0 ||
		input.Name != "" {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	intentHash, err := controlplanecontract.WorkspaceMattermostMappingIntentSHA256(
		controlplanecontract.WorkspaceMattermostMappingIntent{
			ActorID: input.Principal.ActorID, OrganizationID: input.Principal.OrganizationID,
			ProjectID: input.Principal.ProjectID, WorkspaceID: input.ProviderReceipt.WorkspaceID,
			Action: input.Action, MappingID: input.MappingID, DisplayName: input.Name,
			ExpectedVersion: input.ExpectedVersion, ExpectedGeneration: input.ExpectedGeneration,
			ProviderTeamRef:   input.ProviderReceipt.ProviderTeamRef,
			ProviderObjectRef: input.ProviderReceipt.ProviderObjectRef,
			EffectGeneration:  input.ProviderReceipt.EffectGeneration,
			EffectSHA256:      input.ProviderReceipt.EffectSHA256,
		},
	)
	if err != nil || input.ProviderReceipt.CommandIntentSHA256 != intentHash {
		return entity.Resource{}, errs.ErrPermissionDenied
	}
	// One-use receipt/JTI, policy revision и provider generation относятся к
	// transport proof конкретной попытки. Durable idempotency связывается только
	// с проверенной workload boundary и неизменным business intent, чтобы после
	// ambiguous outcome producer мог предъявить свежий receipt той же команды.
	requestHash, err := workspaceMappingRequestHash(input)
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if input.Action == "relink" || input.Action == "unlink" {
		replay, replayed, replayErr := service.replayWorkspaceMappingExternalCommand(ctx, input)
		if replayErr != nil || replayed {
			return replay, replayErr
		}
	}
	apply := func(tx domainrepo.Transaction) (entity.Resource, error) {
		protected, ok := tx.(domainrepo.ProtectedTransaction)
		if !ok {
			return entity.Resource{}, errs.ErrInternal
		}
		now := service.now().UTC().Truncate(time.Microsecond)
		workspace, workspaceSHA, err := lockActiveWorkspace(ctx, tx, input.Principal)
		if err != nil || input.ProviderReceipt.WorkspaceID != workspace.ID {
			if err != nil {
				return entity.Resource{}, err
			}
			return entity.Resource{}, errs.ErrNotFound
		}
		stableTarget := "workspace-" + strings.ReplaceAll(workspace.ID, "-", "")
		consumption, replay, replayed, err := reserveProviderCommandReceipt(
			ctx, tx, protected, input.Principal, input.ProviderReceipt,
			"workspace_mattermost_mapping", workspace.ID, stableTarget, now,
		)
		if err != nil || replayed {
			return replay, err
		}
		finish := func(result entity.Resource) (entity.Resource, error) {
			if err := finalizeExternalCommandReceipt(ctx, protected, consumption, result); err != nil {
				return entity.Resource{}, err
			}
			return result, nil
		}
		if input.Action == "bind" {
			existing, listErr := tx.ListSnapshotResources(ctx,
				input.Principal.OrganizationID, input.Principal.ProjectID)
			if listErr != nil {
				return entity.Resource{}, listErr
			}
			for _, candidate := range existing {
				spec, ok := candidate.Spec.(entity.WorkspaceMattermostMappingSpec)
				if ok && candidate.Kind == enum.KindWorkspaceMapping && spec.WorkspaceID == workspace.ID {
					if _, lockErr := tx.GetForUpdateIncludingDeleted(ctx, input.Principal.OrganizationID,
						input.Principal.ProjectID, candidate.ID); lockErr != nil {
						return entity.Resource{}, lockErr
					}
					return entity.Resource{}, errs.ErrStateConflict
				}
			}
			created, err := entity.New(uuid.NewString(), input.Principal.OrganizationID,
				input.Principal.ProjectID, workspace.ID, input.Principal.ActorID,
				enum.KindWorkspaceMapping, input.Name, entity.WorkspaceMattermostMappingSpec{
					WorkspaceID: workspace.ID, WorkspaceVersion: workspace.Version, WorkspaceSHA256: workspaceSHA,
					ProviderTeamRef: input.ProviderReceipt.ProviderTeamRef, ProviderReceiptID: input.Principal.AuthorityReference,
					ProviderReceiptVersion:   input.Principal.AuthorityRevision,
					ProviderReceiptSHA256:    input.Principal.AuthorityDigest,
					ProviderEffectVersion:    input.ProviderReceipt.EffectVersion,
					ProviderEffectGeneration: input.ProviderReceipt.EffectGeneration,
					MappingGeneration:        1, MappingState: "BOUND", ProviderObservedAt: now,
				}, now)
			if err != nil {
				return entity.Resource{}, errs.ErrInvalidInput
			}
			if err := tx.Insert(ctx, created); err != nil {
				return entity.Resource{}, err
			}
			if err := service.appendWorkspaceMappingRecords(ctx, tx, protected, input.Principal, input.Action, created); err != nil {
				return entity.Resource{}, err
			}
			return finish(created)
		}
		current, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
			input.Principal.ProjectID, input.MappingID)
		if err != nil {
			return entity.Resource{}, err
		}
		spec, ok := current.Spec.(entity.WorkspaceMattermostMappingSpec)
		if !ok || current.Kind != enum.KindWorkspaceMapping || current.OwnerActorID != input.Principal.ActorID {
			return entity.Resource{}, errs.ErrNotFound
		}
		if current.Version != input.ExpectedVersion || spec.MappingGeneration != input.ExpectedGeneration {
			return entity.Resource{}, errs.ErrVersionMismatch
		}
		if input.Principal.AuthorityRevision <= spec.ProviderReceiptVersion ||
			input.ProviderReceipt.EffectVersion <= spec.ProviderEffectVersion ||
			input.ProviderReceipt.EffectGeneration <= spec.ProviderEffectGeneration ||
			input.ProviderReceipt.WorkspaceID != spec.WorkspaceID || spec.WorkspaceID != workspace.ID ||
			(input.Action == "unlink" && input.ProviderReceipt.ProviderTeamRef != spec.ProviderTeamRef) {
			return entity.Resource{}, errs.ErrStateConflict
		}
		if err := lockAndRejectOpenWorkspaceGraph(ctx, tx, input.Principal, spec.WorkspaceID); err != nil {
			return entity.Resource{}, err
		}
		spec.MappingGeneration++
		spec.WorkspaceVersion, spec.WorkspaceSHA256 = workspace.Version, workspaceSHA
		spec.ProviderReceiptID, spec.ProviderReceiptVersion, spec.ProviderReceiptSHA256 =
			input.Principal.AuthorityReference, input.Principal.AuthorityRevision, input.Principal.AuthorityDigest
		spec.ProviderEffectVersion, spec.ProviderEffectGeneration =
			input.ProviderReceipt.EffectVersion, input.ProviderReceipt.EffectGeneration
		spec.ProviderObservedAt = now
		var updated entity.Resource
		if input.Action == "relink" {
			if current.State != enum.StateActive || spec.MappingState != "BOUND" {
				return entity.Resource{}, errs.ErrStateConflict
			}
			spec.ProviderTeamRef = input.ProviderReceipt.ProviderTeamRef
			updated, err = current.Update(current.Name, spec, now)
		} else {
			if current.State != enum.StateActive || spec.MappingState != "BOUND" {
				return entity.Resource{}, errs.ErrStateConflict
			}
			spec.MappingState = "UNLINKED"
			updated, err = current.ReplaceAndTransition(spec, enum.StateArchived, now)
		}
		if err != nil {
			return entity.Resource{}, errs.ErrStateConflict
		}
		if err := tx.Update(ctx, updated, current.Version); err != nil {
			return entity.Resource{}, err
		}
		if err := service.appendWorkspaceMappingRecords(ctx, tx, protected, input.Principal, input.Action, updated); err != nil {
			return entity.Resource{}, err
		}
		return finish(updated)
	}
	scope := "manage_workspace_mapping_" + input.Action
	if input.Action == "bind" {
		return service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey, scope, requestHash, apply)
	}
	result, mutationErr := service.withOwnerLockedResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		scope, requestHash, input.MappingID, enum.KindWorkspaceMapping, input.ExpectedVersion,
		func(stored entity.Resource) error {
			if stored.ID != input.MappingID || stored.Kind != enum.KindWorkspaceMapping ||
				stored.OwnerActorID != input.Principal.ActorID || stored.Version != input.ExpectedVersion+1 {
				return errs.ErrStateConflict
			}
			return nil
		}, apply)
	if mutationErr != nil {
		replay, replayed, replayErr := service.replayWorkspaceMappingExternalCommand(ctx, input)
		if replayErr != nil || replayed {
			return replay, replayErr
		}
	}
	return result, mutationErr
}

func workspaceMappingRequestHash(input ManageWorkspaceMappingInput) (string, error) {
	return canonicalHash(struct {
		ActorID, OrganizationID, ProjectID string
		Permission, CallerWorkload         string
		CallerSPIFFEID, AuthoritySource    string
		Action, MappingID, DisplayName     string
		ExpectedVersion                    uint64
		ExpectedGeneration                 uint64
		WorkspaceID, ProviderTeamRef       string
		ProviderObjectRef, EffectSHA256    string
	}{
		ActorID: input.Principal.ActorID, OrganizationID: input.Principal.OrganizationID,
		ProjectID: input.Principal.ProjectID, Permission: input.Principal.Permission,
		CallerWorkload: input.Principal.CallerWorkload, CallerSPIFFEID: input.Principal.CallerSPIFFEID,
		AuthoritySource: input.Principal.AuthoritySource, Action: input.Action, MappingID: input.MappingID,
		DisplayName: input.Name, ExpectedVersion: input.ExpectedVersion,
		ExpectedGeneration: input.ExpectedGeneration, WorkspaceID: input.ProviderReceipt.WorkspaceID,
		ProviderTeamRef:   input.ProviderReceipt.ProviderTeamRef,
		ProviderObjectRef: input.ProviderReceipt.ProviderObjectRef,
		EffectSHA256:      input.ProviderReceipt.EffectSHA256,
	})
}

// replayWorkspaceMappingExternalCommand разрешает source внутри owner boundary
// и читает только завершённый immutable semantic result до generic command key.
func (service *Service) replayWorkspaceMappingExternalCommand(
	ctx context.Context,
	input ManageWorkspaceMappingInput,
) (entity.Resource, bool, error) {
	var result entity.Resource
	replayed := false
	err := service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID,
		ProjectID:      input.Principal.ProjectID,
		ActorID:        input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		current, err := tx.GetForUpdateIncludingDeleted(ctx, input.Principal.OrganizationID,
			input.Principal.ProjectID, input.MappingID)
		if err != nil {
			return err
		}
		spec, ok := current.Spec.(entity.WorkspaceMattermostMappingSpec)
		if !ok || current.Kind != enum.KindWorkspaceMapping ||
			current.OwnerActorID != input.Principal.ActorID {
			return errs.ErrNotFound
		}
		if current.Version < input.ExpectedVersion {
			return errs.ErrVersionMismatch
		}
		protected, ok := tx.(domainrepo.ProtectedTransaction)
		if !ok {
			return errs.ErrInternal
		}
		stableTarget := "workspace-" + strings.ReplaceAll(spec.WorkspaceID, "-", "")
		result, replayed, err = replayProviderCommandReceipt(ctx, protected, input.Principal,
			input.ProviderReceipt, "workspace_mattermost_mapping", spec.WorkspaceID,
			stableTarget, service.now().UTC())
		if err != nil || !replayed {
			return err
		}
		if result.ID != current.ID || result.Kind != enum.KindWorkspaceMapping ||
			result.Version != input.ExpectedVersion+1 ||
			result.OwnerActorID != input.Principal.ActorID {
			return errs.ErrStateConflict
		}
		return nil
	})
	return result, replayed && err == nil, err
}

func validateProviderReceipt(principal value.Principal, receipt value.ProviderEffectReceipt,
	purpose, action, effect, fullMethod string, now time.Time,
) error {
	if receipt.Validate(now) != nil || receipt.Purpose != purpose || receipt.WorkloadID != principal.CallerWorkload ||
		receipt.CallerSPIFFEID != principal.CallerSPIFFEID || receipt.FullMethod != fullMethod ||
		receipt.ActorID != principal.ActorID || receipt.OrganizationID != principal.OrganizationID ||
		receipt.ProjectID != principal.ProjectID || receipt.Action != action || receipt.Effect != effect ||
		receipt.ReceiptID != principal.AuthorityReference || receipt.ReceiptRevision != principal.AuthorityRevision {
		return errs.ErrPermissionDenied
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(receipt)
	if err != nil || digest != principal.AuthorityDigest {
		return errs.ErrPermissionDenied
	}
	return nil
}

func lockAndRejectOpenWorkspaceGraph(ctx context.Context, tx domainrepo.Transaction,
	principal value.Principal, workspaceID string,
) error {
	// Query первым берёт общий workspace advisory lock. Resource insert и
	// delivery enqueue используют тот же ключ, поэтому после snapshot новый
	// materialized graph уже не может появиться до конца transaction.
	openDeliveries, err := tx.WorkspaceHasOpenInteractionDeliveries(
		ctx, principal.OrganizationID, principal.ProjectID,
	)
	if err != nil {
		return err
	}
	if openDeliveries {
		return errs.ErrStateConflict
	}
	resources, err := tx.ListSnapshotResources(ctx, principal.OrganizationID, principal.ProjectID)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(resources))
	for _, item := range resources {
		if item.ID != workspaceID && (item.Kind == enum.KindChat || item.Kind == enum.KindSession ||
			item.Kind == enum.KindTurn || item.Kind == enum.KindAgent) {
			ids = append(ids, item.ID)
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		item, lockErr := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, id)
		if lockErr != nil {
			return lockErr
		}
		if agent, ok := item.Spec.(entity.AgentSpec); ok && item.Kind == enum.KindAgent {
			if agent.BotMaskedStatus == "AVAILABLE" {
				return errs.ErrStateConflict
			}
			continue
		}
		if item.State != enum.StateArchived && !item.State.Terminal() {
			return errs.ErrStateConflict
		}
	}
	return nil
}

// lockWorkspaceMappingByOpaqueRef принимает только proof exact current mapping
// tuple, вычисленный interaction-gateway. Raw provider Team ID через Agent bot
// receipt в control-plane не пересекает trusted gateway boundary.
func lockWorkspaceMappingByOpaqueRef(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	mappingRef string,
) (entity.Resource, error) {
	if uuid.Validate(mappingRef) != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	resources, err := tx.ListSnapshotResources(ctx, principal.OrganizationID, principal.ProjectID)
	if err != nil {
		return entity.Resource{}, err
	}
	ids := make([]string, 0, 1)
	for _, item := range resources {
		spec, ok := item.Spec.(entity.WorkspaceMattermostMappingSpec)
		proof, proofErr := agentBotMappingProofRef(item, spec)
		if ok && proofErr == nil && item.Kind == enum.KindWorkspaceMapping && item.State == enum.StateActive &&
			item.OwnerActorID == principal.ActorID && spec.WorkspaceID == principal.ProjectID &&
			spec.MappingState == "BOUND" && proof == mappingRef {
			ids = append(ids, item.ID)
		}
	}
	slices.Sort(ids)
	if len(ids) != 1 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	mapping, err := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, ids[0])
	if err != nil {
		return entity.Resource{}, err
	}
	spec, ok := mapping.Spec.(entity.WorkspaceMattermostMappingSpec)
	proof, proofErr := agentBotMappingProofRef(mapping, spec)
	if !ok || mapping.Kind != enum.KindWorkspaceMapping || mapping.State != enum.StateActive ||
		mapping.OwnerActorID != principal.ActorID || spec.WorkspaceID != principal.ProjectID ||
		spec.MappingState != "BOUND" || proofErr != nil || proof != mappingRef {
		return entity.Resource{}, errs.ErrStateConflict
	}
	return mapping, nil
}

func agentBotMappingProofRef(mapping entity.Resource,
	spec entity.WorkspaceMattermostMappingSpec,
) (string, error) {
	return controlplanecontract.AgentBotMappingProofRef(mapping.ID, mapping.Version, spec.MappingGeneration,
		spec.ProviderEffectVersion, spec.ProviderEffectGeneration)
}

func (service *Service) appendWorkspaceMappingRecords(
	ctx context.Context,
	tx domainrepo.Transaction,
	protected domainrepo.ProtectedTransaction,
	principal value.Principal,
	action string,
	mapping entity.Resource,
) error {
	digest, err := entity.ProjectionSHA256(mapping)
	if err != nil {
		return errs.ErrInternal
	}
	if err := protected.AppendProtectedResourceHistory(ctx, domainrepo.ProtectedResourceHistory{
		Resource: mapping, Action: action, SnapshotSHA256: digest, OccurredAt: mapping.UpdatedAt,
	}); err != nil {
		return err
	}
	return appendOwnerStateAudit(ctx, tx, principal, "manage_workspace_mapping_"+action,
		mapping.OrganizationID, mapping.ProjectID, mapping.ID, string(mapping.Kind), mapping.Version, mapping.UpdatedAt)
}
