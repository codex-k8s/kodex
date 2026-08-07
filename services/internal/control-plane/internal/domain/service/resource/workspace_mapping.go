package resource

import (
	"context"
	"slices"
	"time"

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
	requestHash, err := canonicalHash(struct {
		Identity                            commandIdentity
		Action, MappingID                   string
		ExpectedVersion, ExpectedGeneration uint64
		ProviderReceipt                     value.ProviderEffectReceipt
		DisplayName                         string
	}{identity(input.Principal), input.Action, input.MappingID, input.ExpectedVersion,
		input.ExpectedGeneration, input.ProviderReceipt, input.Name})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
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
			return created, nil
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
		return updated, nil
	}
	scope := "manage_workspace_mapping_" + input.Action
	if input.Action == "bind" {
		return service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey, scope, requestHash, apply)
	}
	return service.withOwnerLockedResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		scope, requestHash, input.MappingID, enum.KindWorkspaceMapping, input.ExpectedVersion,
		func(stored entity.Resource) error {
			if stored.ID != input.MappingID || stored.Kind != enum.KindWorkspaceMapping ||
				stored.OwnerActorID != input.Principal.ActorID || stored.Version != input.ExpectedVersion+1 {
				return errs.ErrStateConflict
			}
			return nil
		}, apply)
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
	digest, err := canonicalHash(receipt)
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

func lockWorkspaceMappingForProviderTeam(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	providerTeamRef string,
) (entity.Resource, error) {
	if providerTeamRef == "" {
		return entity.Resource{}, errs.ErrStateConflict
	}
	resources, err := tx.ListSnapshotResources(ctx, principal.OrganizationID, principal.ProjectID)
	if err != nil {
		return entity.Resource{}, err
	}
	ids := make([]string, 0, 1)
	for _, item := range resources {
		spec, ok := item.Spec.(entity.WorkspaceMattermostMappingSpec)
		if ok && item.Kind == enum.KindWorkspaceMapping && item.State == enum.StateActive &&
			item.OwnerActorID == principal.ActorID && spec.WorkspaceID == principal.ProjectID &&
			spec.MappingState == "BOUND" && spec.ProviderTeamRef == providerTeamRef {
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
	if !ok || mapping.Kind != enum.KindWorkspaceMapping || mapping.State != enum.StateActive ||
		mapping.OwnerActorID != principal.ActorID || spec.WorkspaceID != principal.ProjectID ||
		spec.MappingState != "BOUND" || spec.ProviderTeamRef != providerTeamRef {
		return entity.Resource{}, errs.ErrStateConflict
	}
	return mapping, nil
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
