package resource

import (
	"context"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

// ManageWorkspaceMapping сохраняет только server-resolved workspace и
// metadata signed provider readback, принадлежащего integration-gateway.
func (service *Service) ManageWorkspaceMapping(
	ctx context.Context,
	input ManageWorkspaceMappingInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionWorkspaceMappingManage); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		(input.Action != "bind" && input.Action != "relink" && input.Action != "unlink") ||
		input.Principal.CallerWorkload != service.integrationGatewayWorkload ||
		input.Principal.CallerSPIFFEID != service.integrationGatewaySPIFFEID ||
		input.Principal.AuthoritySource != "DOMAIN_STATE" ||
		input.Principal.AuthorityReference != input.ProviderReadbackReceipt ||
		input.Principal.AuthorityRevision == 0 || !validSHA256Text(input.Principal.AuthorityDigest) ||
		!validExternalRefText(input.ProviderTeamRef) {
		return entity.Resource{}, errs.ErrPermissionDenied
	}
	if input.Action == "bind" {
		if input.MappingID != "" || input.ExpectedVersion != 0 || input.ExpectedGeneration != 0 ||
			value.ValidateName(input.Name) != nil || value.ValidateStableKey(input.WorkspaceStableKey) != nil {
			return entity.Resource{}, errs.ErrInvalidInput
		}
	} else if value.ValidateID(input.MappingID) != nil || input.ExpectedVersion == 0 || input.ExpectedGeneration == 0 ||
		input.WorkspaceStableKey != "" || input.Name != "" {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity                             commandIdentity
		Action, MappingID                    string
		ExpectedVersion, ExpectedGeneration  uint64
		WorkspaceStableKey, ProviderTeamRef  string
		ProviderReadbackReceipt, DisplayName string
	}{identity(input.Principal), input.Action, input.MappingID, input.ExpectedVersion,
		input.ExpectedGeneration, input.WorkspaceStableKey, input.ProviderTeamRef,
		input.ProviderReadbackReceipt, input.Name})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	apply := func(tx domainrepo.Transaction) (entity.Resource, error) {
		protected, ok := tx.(domainrepo.ProtectedTransaction)
		if !ok {
			return entity.Resource{}, errs.ErrInternal
		}
		now := service.now().UTC().Truncate(time.Microsecond)
		if input.Action == "bind" {
			workspace, err := protected.GetByNameForUpdate(ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, enum.KindRepositoryWorkspace, input.WorkspaceStableKey)
			if err != nil || workspace.State != enum.StateActive {
				return entity.Resource{}, errs.ErrNotFound
			}
			workspaceID, workspaceVersion, workspaceSHA, err := protectedTuple(workspace)
			if err != nil {
				return entity.Resource{}, err
			}
			created, err := entity.New(uuid.NewString(), input.Principal.OrganizationID,
				input.Principal.ProjectID, workspace.ID, input.Principal.ActorID,
				enum.KindWorkspaceMapping, input.Name, entity.WorkspaceMattermostMappingSpec{
					WorkspaceID: workspaceID, WorkspaceVersion: workspaceVersion, WorkspaceSHA256: workspaceSHA,
					ProviderTeamRef: input.ProviderTeamRef, ProviderReceiptID: input.Principal.AuthorityReference,
					ProviderReceiptVersion: input.Principal.AuthorityRevision,
					ProviderReceiptSHA256:  input.Principal.AuthorityDigest,
					MappingGeneration:      1, MappingState: "BOUND", ProviderObservedAt: now,
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
			(input.Action == "unlink" && input.ProviderTeamRef != spec.ProviderTeamRef) {
			return entity.Resource{}, errs.ErrStateConflict
		}
		spec.MappingGeneration++
		spec.ProviderReceiptID, spec.ProviderReceiptVersion, spec.ProviderReceiptSHA256 =
			input.Principal.AuthorityReference, input.Principal.AuthorityRevision, input.Principal.AuthorityDigest
		spec.ProviderObservedAt = now
		var updated entity.Resource
		if input.Action == "relink" {
			if current.State != enum.StateActive || spec.MappingState != "BOUND" {
				return entity.Resource{}, errs.ErrStateConflict
			}
			spec.ProviderTeamRef = input.ProviderTeamRef
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
