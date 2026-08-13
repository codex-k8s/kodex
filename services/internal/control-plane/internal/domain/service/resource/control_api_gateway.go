package resource

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

// CreateProject — закрытая organization-scoped команда. Она единственная
// может пройти protected create registry для PROJECT; ID, owner, project scope,
// начальное состояние и OCC-версия назначаются общей owner transaction.
func (service *Service) CreateProject(
	ctx context.Context,
	input CreateProjectInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionProjectCreate); err != nil {
		return entity.Resource{}, err
	}
	if !service.controlAPIGatewayPrincipal(input.Principal) ||
		input.Principal.ProjectID != "" {
		return entity.Resource{}, errs.ErrPermissionDenied
	}
	return service.create(ctx, CreateInput{
		Principal: input.Principal, IdempotencyKey: input.IdempotencyKey,
		Kind: enum.KindProject, Name: input.Name, Spec: input.Spec,
		TenantProject: true,
	}, true)
}

func (service *Service) trustedOwnedProject(
	ctx context.Context,
	principal value.Principal,
	projectID string,
) (value.Principal, entity.Resource, error) {
	if principal.ProjectID != "" || value.ValidateID(projectID) != nil {
		return value.Principal{}, entity.Resource{}, errs.ErrInvalidInput
	}
	project, err := service.repository.GetIncludingDeleted(
		ctx, principal.OrganizationID, projectID, projectID, enum.KindProject,
	)
	if err != nil {
		return value.Principal{}, entity.Resource{}, err
	}
	if project.ID != projectID || project.ProjectID != projectID ||
		project.Kind != enum.KindProject || project.OwnerActorID != principal.ActorID {
		return value.Principal{}, entity.Resource{}, errs.ErrNotFound
	}
	principal.ProjectID = projectID
	return principal, project, nil
}

// UpdateProject сначала разрешает locator в tenant owner boundary, затем
// повторно блокирует exact project и только после owner/OCC читает receipt.
func (service *Service) UpdateProject(
	ctx context.Context,
	input UpdateProjectInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionProjectUpdate); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ProjectID) != nil || input.ExpectedVersion == 0 ||
		value.ValidateName(input.Name) != nil || input.Spec.Validate() != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	trusted, _, err := service.trustedOwnedProject(ctx, input.Principal, input.ProjectID)
	if err != nil {
		return entity.Resource{}, err
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		ProjectID       string
		ExpectedVersion uint64
		Name            string
		Spec            entity.ProjectSpec
	}{identity(trusted), input.ProjectID, input.ExpectedVersion, input.Name, input.Spec})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	var locked entity.Resource
	var result entity.Resource
	err = service.withLifecycleReceipt(
		ctx, trusted, input.IdempotencyKey, "update_project", requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			current, lockErr := tx.GetForUpdateIncludingDeleted(
				ctx, trusted.OrganizationID, trusted.ProjectID, input.ProjectID,
			)
			if lockErr != nil {
				return 0, lockErr
			}
			if current.Kind != enum.KindProject || current.ProjectID != current.ID ||
				requireLifecycleOwner(trusted, current) != nil {
				return 0, errs.ErrNotFound
			}
			if current.Version == input.ExpectedVersion && !current.State.Terminal() &&
				current.State != enum.StateDeletionPending {
				locked = current
				return lifecycleReceiptApply, nil
			}
			if current.Version == input.ExpectedVersion+1 && !current.State.Terminal() &&
				current.State != enum.StateDeletionPending {
				locked = current
				return lifecycleReceiptReplay, nil
			}
			return 0, errs.ErrVersionMismatch
		},
		func() error {
			if result.ID != locked.ID || result.Version != locked.Version ||
				result.State != locked.State || result.OwnerActorID != trusted.ActorID {
				return errs.ErrStateConflict
			}
			return nil
		},
		func(tx domainrepo.Transaction) error {
			current := locked
			if err := authorizeGitManagedMutation(ctx, tx, trusted, current.Spec); err != nil {
				return err
			}
			currentSpec, ok := current.Spec.(entity.ProjectSpec)
			if !ok {
				return errs.ErrStateConflict
			}
			input.Spec.Ownership = currentSpec.Ownership
			now, timeErr := tx.CurrentTime(ctx)
			if timeErr != nil {
				return timeErr
			}
			updated, updateErr := current.Update(input.Name, input.Spec, now)
			if updateErr != nil {
				return errs.ErrStateConflict
			}
			if updateErr = tx.Update(ctx, updated, current.Version); updateErr != nil {
				return updateErr
			}
			if updateErr = service.appendMutationRecords(
				ctx, tx, trusted, "update_project", updated,
			); updateErr != nil {
				return updateErr
			}
			result = updated
			return nil
		},
	)
	return result, err
}

// DeleteProject терминально закрывает только пустой owner project. Оба
// перехода и события фиксируются одной транзакцией; повтор читает tombstone.
func (service *Service) DeleteProject(
	ctx context.Context,
	input DeleteProjectInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionProjectDelete); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ProjectID) != nil || input.ExpectedVersion == 0 {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	trusted, _, err := service.trustedOwnedProject(ctx, input.Principal, input.ProjectID)
	if err != nil {
		return entity.Resource{}, err
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		ProjectID       string
		ExpectedVersion uint64
	}{identity(trusted), input.ProjectID, input.ExpectedVersion})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	var locked entity.Resource
	var result entity.Resource
	err = service.withLifecycleReceipt(
		ctx, trusted, input.IdempotencyKey, "delete_project", requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			current, lockErr := tx.GetForUpdateIncludingDeleted(
				ctx, trusted.OrganizationID, trusted.ProjectID, input.ProjectID,
			)
			if lockErr != nil {
				return 0, lockErr
			}
			if current.Kind != enum.KindProject || current.ProjectID != current.ID ||
				requireLifecycleOwner(trusted, current) != nil {
				return 0, errs.ErrNotFound
			}
			locked = current
			if current.Version == input.ExpectedVersion &&
				(current.State == enum.StateActive || current.State == enum.StatePaused ||
					current.State == enum.StateArchived) {
				live, liveErr := tx.ProjectHasLiveResources(ctx, current.OrganizationID, current.ID)
				if liveErr != nil {
					return 0, liveErr
				}
				if live {
					return 0, errs.ErrStateConflict
				}
				return lifecycleReceiptApply, nil
			}
			if current.Version == input.ExpectedVersion+2 && current.State == enum.StateDeleted {
				return lifecycleReceiptReplay, nil
			}
			return 0, errs.ErrVersionMismatch
		},
		func() error {
			if result.ID != locked.ID || result.Version != locked.Version ||
				result.State != enum.StateDeleted || result.OwnerActorID != trusted.ActorID {
				return errs.ErrStateConflict
			}
			return nil
		},
		func(tx domainrepo.Transaction) error {
			if err := authorizeGitManagedMutation(ctx, tx, trusted, locked.Spec); err != nil {
				return err
			}
			now, timeErr := tx.CurrentTime(ctx)
			if timeErr != nil {
				return timeErr
			}
			pending, transitionErr := locked.Transition(enum.StateDeletionPending, now)
			if transitionErr != nil {
				return errs.ErrStateConflict
			}
			if transitionErr = tx.Update(ctx, pending, locked.Version); transitionErr != nil {
				return transitionErr
			}
			if transitionErr = service.appendMutationRecords(
				ctx, tx, trusted, "delete_project_pending", pending,
			); transitionErr != nil {
				return transitionErr
			}
			deleted, transitionErr := pending.Transition(enum.StateDeleted, now)
			if transitionErr != nil {
				return errs.ErrStateConflict
			}
			if transitionErr = tx.Update(ctx, deleted, pending.Version); transitionErr != nil {
				return transitionErr
			}
			if transitionErr = service.appendMutationRecords(
				ctx, tx, trusted, "delete_project", deleted,
			); transitionErr != nil {
				return transitionErr
			}
			result = deleted
			return nil
		},
	)
	return result, err
}

func accessConfigurationKind(kind enum.Kind) bool {
	// ROLE и PROMPT_PROFILE после target cutover доступны только для
	// immutable legacy/derived read. Их detach/copy также не должны возвращать
	// прежнему универсальному access path mutation authority.
	return kind == enum.KindTeam
}

// DetachAccessResource разрешает source в trusted owner boundary до OCC и
// меняет только server-owned ownership metadata в той же транзакции.
func (service *Service) DetachAccessResource(
	ctx context.Context,
	input DetachAccessResourceInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionAccessDetach); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ResourceID) != nil || input.ExpectedVersion == 0 ||
		!accessConfigurationKind(input.ExpectedKind) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		ResourceID      string
		ExpectedVersion uint64
		ExpectedKind    enum.Kind
	}{identity(input.Principal), input.ResourceID, input.ExpectedVersion, input.ExpectedKind})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		"detach_access_configuration", requestHash, func(tx domainrepo.Transaction) (entity.Resource, error) {
			current, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, input.ResourceID)
			if err != nil {
				return entity.Resource{}, err
			}
			if current.Kind != input.ExpectedKind {
				return entity.Resource{}, errs.ErrNotFound
			}
			if current.Version != input.ExpectedVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			configured, ok := current.Spec.(entity.ConfiguredSpec)
			if !ok || configured.ConfigurationOwnership().ManagedBy != "GIT" {
				return entity.Resource{}, errs.ErrStateConflict
			}
			nextSpec, err := entity.WithConfigurationOwnership(
				current.Spec, entity.ConfigurationOwnership{ManagedBy: "UI"},
			)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			updated, err := current.Update(current.Name, nextSpec, service.now().UTC().Truncate(time.Microsecond))
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updated, current.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(ctx, tx, input.Principal,
				"detach_access_configuration", updated)
		})
}

// CopyAccessResource создаёт новую UI-owned сущность только из locked source.
func (service *Service) CopyAccessResource(
	ctx context.Context,
	input CopyAccessResourceInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionAccessCopy); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.SourceResourceID) != nil || input.ExpectedSourceVersion == 0 ||
		!accessConfigurationKind(input.ExpectedKind) || value.ValidateName(input.Name) != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity              commandIdentity
		SourceResourceID      string
		ExpectedSourceVersion uint64
		ExpectedKind          enum.Kind
		Name                  string
	}{
		identity(input.Principal), input.SourceResourceID, input.ExpectedSourceVersion,
		input.ExpectedKind, input.Name,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		"copy_access_configuration", requestHash, func(tx domainrepo.Transaction) (entity.Resource, error) {
			source, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, input.SourceResourceID)
			if err != nil {
				return entity.Resource{}, err
			}
			if source.Kind != input.ExpectedKind {
				return entity.Resource{}, errs.ErrNotFound
			}
			if source.Version != input.ExpectedSourceVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			configured, ok := source.Spec.(entity.ConfiguredSpec)
			if !ok || configured.ConfigurationOwnership().ManagedBy != "GIT" {
				return entity.Resource{}, errs.ErrStateConflict
			}
			created, err := copyAccessResource(source, input.Principal.ActorID, input.Name,
				service.now().UTC().Truncate(time.Microsecond))
			if err != nil {
				return entity.Resource{}, errs.ErrInvalidInput
			}
			if err := service.validateAccessMutation(ctx, tx, input.Principal, created.Kind, created.Spec); err != nil {
				return entity.Resource{}, err
			}
			if err := service.validateReferences(ctx, tx, created); err != nil {
				return entity.Resource{}, err
			}
			if err := tx.Insert(ctx, created); err != nil {
				return entity.Resource{}, err
			}
			return created, service.appendMutationRecords(ctx, tx, input.Principal,
				"copy_access_configuration", created)
		})
}

func copyAccessResource(source entity.Resource, ownerActorID, name string, now time.Time) (entity.Resource, error) {
	copiedSpec, err := entity.WithConfigurationOwnership(source.Spec, entity.ConfigurationOwnership{
		ManagedBy: "UI", SourceRef: source.ID, SourceRevision: source.Version,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	created, err := entity.NewPausedAccessConfiguration(uuid.NewString(), source.OrganizationID, source.ProjectID,
		source.ParentID, ownerActorID, source.Kind, name, copiedSpec, now)
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return created, nil
}

func (service *Service) ListRuntimeIncidents(
	ctx context.Context,
	input ListRuntimeIncidentsInput,
) ([]domainrepo.RuntimeIncident, error) {
	if err := authorize(input.Principal, permissionRuntimeIncidentRead); err != nil {
		return nil, err
	}
	input.Filter.OrganizationID = input.Principal.OrganizationID
	input.Filter.ProjectID = input.Principal.ProjectID
	input.Filter.ActorID = input.Principal.ActorID
	if input.Principal.ProjectID == "" || input.Filter.Validate() != nil {
		return nil, errs.ErrInvalidInput
	}
	return service.repository.ListRuntimeIncidents(ctx, input.Filter)
}

func (service *Service) controlAPIGatewayPrincipal(principal value.Principal) bool {
	return principal.CallerWorkload == controlAPIGatewayWorkload &&
		principal.CallerSPIFFEID == controlAPIGatewaySPIFFEID
}

func (service *Service) ListBackups(
	ctx context.Context,
	input ListBackupsInput,
) ([]domainrepo.Backup, error) {
	if err := authorize(input.Principal, permissionBackupRead); err != nil {
		return nil, err
	}
	if !service.controlAPIGatewayPrincipal(input.Principal) ||
		input.Principal.ProjectID == "" || input.Limit < 1 || input.Limit > 100 ||
		(input.AfterID != "" && value.ValidateID(input.AfterID) != nil) {
		return nil, errs.ErrInvalidInput
	}
	return service.repository.ListBackups(
		ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		input.Principal.ActorID, input.AfterID, input.Limit,
	)
}

func (service *Service) GetBackup(
	ctx context.Context,
	input GetBackupInput,
) (domainrepo.Backup, error) {
	if err := authorize(input.Principal, permissionBackupRead); err != nil {
		return domainrepo.Backup{}, err
	}
	if !service.controlAPIGatewayPrincipal(input.Principal) ||
		input.Principal.ProjectID == "" || value.ValidateID(input.BackupID) != nil {
		return domainrepo.Backup{}, errs.ErrInvalidInput
	}
	return service.repository.GetBackup(
		ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		input.Principal.ActorID, input.BackupID,
	)
}

func (service *Service) RestoreBackup(
	ctx context.Context,
	input RestoreBackupInput,
) (domainrepo.RuntimeRestoreOperation, error) {
	if err := authorize(input.Principal, permissionBackupRestore); err != nil {
		return domainrepo.RuntimeRestoreOperation{}, err
	}
	if !service.controlAPIGatewayPrincipal(input.Principal) ||
		input.Principal.ProjectID == "" ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.BackupID) != nil || input.ExpectedBackupVersion == 0 ||
		input.ExpectedSourceVersion == 0 ||
		!validSHA256Text(input.ArchiveSHA256) ||
		!validSHA256Text(input.ProvenanceSHA256) || input.Scope != "SESSION" ||
		value.ValidateID(input.ScopeID) != nil {
		return domainrepo.RuntimeRestoreOperation{}, errs.ErrInvalidInput
	}
	backup, err := service.repository.GetBackup(
		ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		input.Principal.ActorID, input.BackupID,
	)
	if err != nil {
		return domainrepo.RuntimeRestoreOperation{}, err
	}
	// Dynamic eligibility проверяется внутри той же lifecycle transaction после
	// lookup receipt. Поэтому terminal replay с исходным ETag не блокируется
	// изменившимся readback state/version, а новый command всё равно fail-closed.
	if backup.SourceVersion != input.ExpectedSourceVersion ||
		backup.ArchiveSHA256 != input.ArchiveSHA256 ||
		backup.ProvenanceSHA256 != input.ProvenanceSHA256 ||
		backup.SessionID != input.ScopeID || backup.SourceFence == 0 {
		return domainrepo.RuntimeRestoreOperation{}, errs.ErrStateConflict
	}
	retried, err := service.retryRuntimeExecution(ctx, RuntimeExecutionInput{
		Principal: input.Principal, IdempotencyKey: input.IdempotencyKey,
		ExecutionID: backup.ID, ExpectedVersion: backup.SourceVersion,
		ExpectedFence: backup.SourceFence,
	}, &restoreRuntimeIntent{
		BackupID: backup.ID, SourceVersion: backup.SourceVersion,
		SourceFence: backup.SourceFence, ArchiveSHA256: backup.ArchiveSHA256,
		ProvenanceSHA256: backup.ProvenanceSHA256, SessionID: backup.SessionID,
		ExpectedBackupVersion: input.ExpectedBackupVersion,
	})
	if err != nil {
		return domainrepo.RuntimeRestoreOperation{}, err
	}
	if retried.Restore == nil {
		return domainrepo.RuntimeRestoreOperation{}, errs.ErrInternal
	}
	return service.repository.GetRuntimeRestoreOperation(
		ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		input.Principal.ActorID, retried.Restore.ID,
	)
}

func (service *Service) GetRestoreOperation(
	ctx context.Context,
	input GetRestoreOperationInput,
) (domainrepo.RuntimeRestoreOperation, error) {
	if err := authorize(input.Principal, permissionBackupRead); err != nil {
		return domainrepo.RuntimeRestoreOperation{}, err
	}
	if !service.controlAPIGatewayPrincipal(input.Principal) ||
		input.Principal.ProjectID == "" || value.ValidateID(input.OperationID) != nil {
		return domainrepo.RuntimeRestoreOperation{}, errs.ErrInvalidInput
	}
	return service.repository.GetRuntimeRestoreOperation(
		ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		input.Principal.ActorID, input.OperationID,
	)
}

func (service *Service) ListRestoreOperations(
	ctx context.Context,
	input ListRestoreOperationsInput,
) ([]domainrepo.RuntimeRestoreOperation, error) {
	if err := authorize(input.Principal, permissionBackupRead); err != nil {
		return nil, err
	}
	if !service.controlAPIGatewayPrincipal(input.Principal) || input.Principal.ProjectID == "" ||
		(input.BackupID != "" && value.ValidateID(input.BackupID) != nil) ||
		(input.AfterID != "" && value.ValidateID(input.AfterID) != nil) ||
		input.Limit < 1 || input.Limit > 100 {
		return nil, errs.ErrInvalidInput
	}
	return service.repository.ListRuntimeRestoreOperations(
		ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		input.Principal.ActorID, input.BackupID, input.AfterID, input.Limit,
	)
}

func (service *Service) AdmitOwnerSession(
	ctx context.Context,
	input OwnerSessionInput,
) (domainrepo.OwnerSessionState, error) {
	if err := authorize(input.Principal, permissionOwnerSessionAdmit); err != nil {
		return domainrepo.OwnerSessionState{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.Principal.AuthorityReference) != nil ||
		input.Principal.AuthorityRevision == 0 || !validSHA256Text(input.Principal.AuthorityDigest) {
		return domainrepo.OwnerSessionState{}, errs.ErrInvalidInput
	}
	state := domainrepo.OwnerSessionState{
		OrganizationID: input.Principal.OrganizationID, ActorID: input.Principal.ActorID,
		SessionID:              input.Principal.AuthorityReference,
		CredentialDigestSHA256: input.Principal.AuthorityDigest,
		CurrentRevision:        input.Principal.AuthorityRevision,
	}
	return service.ownerSessionCommand(ctx, input.Principal, input.IdempotencyKey,
		"admit_owner_session", state, func(tx domainrepo.Transaction) (domainrepo.OwnerSessionState, error) {
			state.UpdatedAt = service.now().UTC().Truncate(time.Microsecond)
			return tx.AdmitOwnerSession(ctx, state)
		})
}

func (service *Service) RevokeOwnerSession(
	ctx context.Context,
	input OwnerSessionInput,
) (domainrepo.OwnerSessionState, error) {
	if err := authorize(input.Principal, permissionOwnerSessionRevoke); err != nil {
		return domainrepo.OwnerSessionState{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		input.ExpectedRevision == 0 || input.ExpectedRevision != input.Principal.AuthorityRevision ||
		value.ValidateID(input.Principal.AuthorityReference) != nil ||
		!validSHA256Text(input.Principal.AuthorityDigest) {
		return domainrepo.OwnerSessionState{}, errs.ErrInvalidInput
	}
	state := domainrepo.OwnerSessionState{
		OrganizationID: input.Principal.OrganizationID, ActorID: input.Principal.ActorID,
		SessionID:              input.Principal.AuthorityReference,
		CredentialDigestSHA256: input.Principal.AuthorityDigest,
		CurrentRevision:        input.ExpectedRevision,
	}
	return service.ownerSessionCommand(ctx, input.Principal, input.IdempotencyKey,
		"revoke_owner_session", state, func(tx domainrepo.Transaction) (domainrepo.OwnerSessionState, error) {
			state.UpdatedAt = service.now().UTC().Truncate(time.Microsecond)
			return tx.RevokeOwnerSession(ctx, state)
		})
}

type ownerSessionMutation func(domainrepo.Transaction) (domainrepo.OwnerSessionState, error)

func (service *Service) ownerSessionCommand(ctx context.Context, principal value.Principal,
	idempotencyKey, scope string, requested domainrepo.OwnerSessionState,
	apply ownerSessionMutation,
) (domainrepo.OwnerSessionState, error) {
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		State    domainrepo.OwnerSessionState
	}{identity(principal), requested})
	if err != nil {
		return domainrepo.OwnerSessionState{}, errs.ErrInvalidInput
	}
	keyHash := hashString(idempotencyKey)
	var result domainrepo.OwnerSessionState
	err = service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID, ActorID: principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		receipt, receiptErr := tx.GetReceipt(ctx, principal.OrganizationID, scope, keyHash)
		if receiptErr == nil {
			if receipt.RequestHash != requestHash || json.Unmarshal(receipt.Payload, &result) != nil {
				return errs.ErrIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(receiptErr, errs.ErrNotFound) {
			return receiptErr
		}
		result, receiptErr = apply(tx)
		if receiptErr != nil {
			return receiptErr
		}
		payload, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return errs.ErrInternal
		}
		return tx.SaveReceipt(ctx, domainrepo.Receipt{
			OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
			Scope: scope, KeyHash: keyHash, RequestHash: requestHash, Payload: payload,
			CreatedAt: service.now().UTC().Truncate(time.Microsecond),
		})
	})
	return result, err
}

const (
	gatewayPublicTLSOverlap         = 15 * time.Minute
	gatewayPublicTLSMaximumLifetime = 120 * 24 * time.Hour
	// Public TLS принадлежит deployment control API, а не пользовательскому
	// Project. Внутренний UUID дает PostgreSQL/RLS устойчивый scope, не
	// расширяя project authority внешнего readiness grant.
	gatewayPublicTLSSystemProjectID = "1b2b8575-0cef-5f6f-8e4d-ed3960a28131"
)

func (service *Service) PrepareGatewayPublicTLS(
	ctx context.Context,
	input PrepareGatewayPublicTLSInput,
) (domainrepo.GatewayPublicTLSState, error) {
	if err := authorize(input.Principal, permissionGatewayPublicTLSPrepare); err != nil {
		return domainrepo.GatewayPublicTLSState{}, err
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	if input.Principal.CallerWorkload != controlAPIGatewayWorkload ||
		input.Principal.CallerSPIFFEID != controlAPIGatewaySPIFFEID ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || input.Generation == 0 ||
		!validSHA256Text(input.CertificateSHA256) ||
		(input.PredecessorGeneration == 0) != (input.PredecessorCertificateSHA256 == "") ||
		(input.Generation == 1 && input.PredecessorGeneration != 0) ||
		(input.Generation > 1 && input.PredecessorGeneration+1 != input.Generation) ||
		(input.PredecessorCertificateSHA256 != "" && !validSHA256Text(input.PredecessorCertificateSHA256)) ||
		input.NotBefore.IsZero() || !input.NotAfter.After(input.NotBefore) ||
		input.NotAfter.Sub(input.NotBefore) > gatewayPublicTLSMaximumLifetime || input.NotBefore.After(now) ||
		!input.NotAfter.After(now.Add(5*time.Minute)) {
		return domainrepo.GatewayPublicTLSState{}, errs.ErrInvalidInput
	}
	scope := domainrepo.GatewayPublicTLSState{
		OrganizationID: input.Principal.OrganizationID, ProjectID: gatewayPublicTLSSystemProjectID,
		WorkloadID: controlAPIGatewayWorkload,
	}
	candidate := domainrepo.GatewayPublicTLSMaterial{
		Generation: input.Generation, CertificateSHA256: input.CertificateSHA256,
		NotBefore: input.NotBefore.UTC(), NotAfter: input.NotAfter.UTC(),
	}
	requestHash, err := canonicalHash(struct {
		Identity                     commandIdentity
		Candidate                    domainrepo.GatewayPublicTLSMaterial
		PredecessorGeneration        uint64
		PredecessorCertificateSHA256 string
	}{
		identity(input.Principal), candidate, input.PredecessorGeneration,
		input.PredecessorCertificateSHA256,
	})
	if err != nil {
		return domainrepo.GatewayPublicTLSState{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	var result domainrepo.GatewayPublicTLSState
	err = service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID, ProjectID: gatewayPublicTLSSystemProjectID,
		ActorID: input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		receipt, receiptErr := tx.GetReceipt(ctx, input.Principal.OrganizationID,
			"prepare_gateway_public_tls", keyHash)
		if receiptErr == nil {
			if receipt.RequestHash != requestHash || json.Unmarshal(receipt.Payload, &result) != nil {
				return errs.ErrIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(receiptErr, errs.ErrNotFound) {
			return receiptErr
		}
		result, receiptErr = tx.PrepareGatewayPublicTLS(ctx, scope, candidate,
			input.PredecessorGeneration, input.PredecessorCertificateSHA256, now)
		if receiptErr != nil {
			return receiptErr
		}
		payload, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return errs.ErrInternal
		}
		return tx.SaveReceipt(ctx, domainrepo.Receipt{
			OrganizationID: input.Principal.OrganizationID, ProjectID: gatewayPublicTLSSystemProjectID,
			Scope: "prepare_gateway_public_tls", KeyHash: keyHash, RequestHash: requestHash,
			Payload: payload, CreatedAt: now,
		})
	})
	return result, err
}

func (service *Service) ConfirmGatewayPublicTLS(
	ctx context.Context,
	input ConfirmGatewayPublicTLSInput,
) (domainrepo.GatewayPublicTLSState, error) {
	if err := authorize(input.Principal, permissionGatewayPublicTLSConfirm); err != nil {
		return domainrepo.GatewayPublicTLSState{}, err
	}
	if input.Principal.CallerWorkload != controlAPIGatewayWorkload ||
		input.Principal.CallerSPIFFEID != controlAPIGatewaySPIFFEID ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || input.Generation == 0 ||
		!validSHA256Text(input.CertificateSHA256) {
		return domainrepo.GatewayPublicTLSState{}, errs.ErrInvalidInput
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	scope := domainrepo.GatewayPublicTLSState{
		OrganizationID: input.Principal.OrganizationID, ProjectID: gatewayPublicTLSSystemProjectID,
		WorkloadID: controlAPIGatewayWorkload,
	}
	requestHash, err := canonicalHash(struct {
		Identity          commandIdentity
		Generation        uint64
		CertificateSHA256 string
	}{identity(input.Principal), input.Generation, input.CertificateSHA256})
	if err != nil {
		return domainrepo.GatewayPublicTLSState{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	var result domainrepo.GatewayPublicTLSState
	err = service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID, ProjectID: gatewayPublicTLSSystemProjectID,
		ActorID: input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		receipt, receiptErr := tx.GetReceipt(ctx, input.Principal.OrganizationID,
			"confirm_gateway_public_tls", keyHash)
		if receiptErr == nil {
			if receipt.RequestHash != requestHash || json.Unmarshal(receipt.Payload, &result) != nil {
				return errs.ErrIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(receiptErr, errs.ErrNotFound) {
			return receiptErr
		}
		result, receiptErr = tx.ConfirmGatewayPublicTLS(ctx, scope, input.Generation,
			input.CertificateSHA256, now, now.Add(gatewayPublicTLSOverlap))
		if receiptErr != nil {
			return receiptErr
		}
		payload, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return errs.ErrInternal
		}
		return tx.SaveReceipt(ctx, domainrepo.Receipt{
			OrganizationID: input.Principal.OrganizationID, ProjectID: gatewayPublicTLSSystemProjectID,
			Scope: "confirm_gateway_public_tls", KeyHash: keyHash, RequestHash: requestHash,
			Payload: payload, CreatedAt: now,
		})
	})
	return result, err
}

func (service *Service) CheckGatewayPublicTLS(
	ctx context.Context,
	input CheckGatewayPublicTLSInput,
) (domainrepo.GatewayPublicTLSState, error) {
	if err := authorize(input.Principal, permissionGatewayPublicTLSCheck); err != nil {
		return domainrepo.GatewayPublicTLSState{}, err
	}
	if input.Principal.CallerWorkload != controlAPIGatewayWorkload ||
		input.Principal.CallerSPIFFEID != controlAPIGatewaySPIFFEID || input.Generation == 0 ||
		!validSHA256Text(input.CertificateSHA256) {
		return domainrepo.GatewayPublicTLSState{}, errs.ErrInvalidInput
	}
	scope := domainrepo.GatewayPublicTLSState{
		OrganizationID: input.Principal.OrganizationID, ProjectID: gatewayPublicTLSSystemProjectID,
		WorkloadID: controlAPIGatewayWorkload,
	}
	var result domainrepo.GatewayPublicTLSState
	err := service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID, ProjectID: gatewayPublicTLSSystemProjectID,
		ActorID: input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		var readErr error
		result, readErr = tx.CheckGatewayPublicTLS(ctx, scope, input.Generation,
			input.CertificateSHA256, service.now().UTC().Truncate(time.Microsecond))
		return readErr
	})
	return result, err
}
