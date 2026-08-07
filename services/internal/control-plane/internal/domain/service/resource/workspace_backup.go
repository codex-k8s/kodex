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

var workspaceBackupActions = map[string]struct{}{
	"create": {}, "cancel": {}, "retry": {},
}

// ManageWorkspaceBackup фиксирует immutable membership WORKSPACE либо
// ALL_WORKSPACES и меняет только состояние полного envelope.
func (service *Service) ManageWorkspaceBackup(
	ctx context.Context,
	input ManageWorkspaceBackupInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionWorkspaceBackupManage); err != nil {
		return entity.Resource{}, err
	}
	_, actionAllowed := workspaceBackupActions[input.Action]
	if !actionAllowed || value.ValidateIdempotencyKey(input.IdempotencyKey) != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if input.Action == "create" {
		if input.BackupID != "" || input.ExpectedVersion != 0 || value.ValidateName(input.Name) != nil ||
			(input.Scope != "WORKSPACE" && input.Scope != "ALL_WORKSPACES") ||
			input.RetainUntil.IsZero() ||
			input.TerminalReasonCode != "" {
			return entity.Resource{}, errs.ErrInvalidInput
		}
	} else if value.ValidateID(input.BackupID) != nil || input.ExpectedVersion == 0 ||
		(input.Action != "retry" && value.ValidateStableKey(input.TerminalReasonCode) != nil) ||
		input.Action == "retry" && input.TerminalReasonCode != "" ||
		input.Scope != "" || input.Name != "" ||
		(input.Action != "retry" && !input.RetainUntil.IsZero()) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity                 commandIdentity
		Action, BackupID, Scope  string
		ExpectedVersion          uint64
		Name, TerminalReasonCode string
		RetainUntil              time.Time
	}{identity(input.Principal), input.Action, input.BackupID, input.Scope,
		input.ExpectedVersion, input.Name, input.TerminalReasonCode,
		input.RetainUntil.UTC()})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	apply := func(tx domainrepo.Transaction) (entity.Resource, error) {
		if input.Action == "create" {
			return service.createWorkspaceBackup(ctx, tx, input)
		}
		return service.transitionWorkspaceBackup(ctx, tx, input)
	}
	scope := "manage_workspace_backup_" + input.Action
	if input.Action == "create" {
		return service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey, scope, requestHash, apply)
	}
	return service.withOwnerLockedResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		scope, requestHash, input.BackupID, enum.KindWorkspaceBackup, input.ExpectedVersion,
		func(stored entity.Resource) error {
			if stored.ID != input.BackupID || stored.Kind != enum.KindWorkspaceBackup ||
				stored.OwnerActorID != input.Principal.ActorID || stored.Version <= input.ExpectedVersion ||
				stored.Version > input.ExpectedVersion+2 {
				return errs.ErrStateConflict
			}
			return nil
		}, apply)
}

type workspaceRecoveryTerminalReceipt struct {
	ResourceID     string
	Version        uint64
	State          enum.State
	Attempt        uint32
	Generation     uint64
	SnapshotSHA256 string
}

func (service *Service) recoveryReconcilerPrincipal(principal value.Principal) bool {
	return principal.CallerWorkload == recoveryReconcilerWorkload &&
		principal.CallerSPIFFEID == recoveryReconcilerSPIFFEID
}

// ReconcileWorkspaceBackupTerminal отделяет internal COMPLETE/FAIL/EXPIRE от
// owner-facing create/cancel/retry. Existing row сначала разрешается под lock;
// owner actor берётся только из сохранённого backup.
func (service *Service) ReconcileWorkspaceBackupTerminal(
	ctx context.Context,
	input ReconcileWorkspaceRecoveryInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionWorkspaceBackupTerminal); err != nil {
		return entity.Resource{}, err
	}
	if !service.recoveryReconcilerPrincipal(input.Principal) {
		return entity.Resource{}, errs.ErrPermissionDenied
	}
	return service.reconcileWorkspaceRecoveryTerminal(ctx, input, enum.KindWorkspaceBackup)
}

func (service *Service) createWorkspaceBackup(
	ctx context.Context,
	tx domainrepo.Transaction,
	input ManageWorkspaceBackupInput,
) (entity.Resource, error) {
	now, err := tx.CurrentTime(ctx)
	if err != nil {
		return entity.Resource{}, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	if !input.RetainUntil.After(now) || input.RetainUntil.After(now.Add(365*24*time.Hour)) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	recovery, ok := tx.(domainrepo.WorkspaceRecoveryTransaction)
	if !ok {
		return entity.Resource{}, errs.ErrInternal
	}
	coordinatorProjectID := input.Principal.ProjectID
	workspaces := []entity.Resource{}
	if input.Scope == "WORKSPACE" {
		workspace, workspaceErr := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
			coordinatorProjectID, coordinatorProjectID)
		if workspaceErr != nil || workspace.Kind != enum.KindProject ||
			workspace.State != enum.StateActive || workspace.OwnerActorID != input.Principal.ActorID {
			if workspaceErr != nil {
				return entity.Resource{}, workspaceErr
			}
			return entity.Resource{}, errs.ErrNotFound
		}
		workspaces = append(workspaces, workspace)
	} else {
		if err := recovery.SwitchWorkspaceProject(ctx, ""); err != nil {
			return entity.Resource{}, err
		}
		workspaces, err = recovery.OwnerWorkspaceProjectsForUpdate(
			ctx, input.Principal.OrganizationID, input.Principal.ActorID,
		)
		if err != nil {
			return entity.Resource{}, err
		}
	}
	if len(workspaces) == 0 {
		return entity.Resource{}, errs.ErrNotFound
	}
	members := make([]entity.WorkspaceBackupMember, 0)
	for _, workspace := range workspaces {
		if workspace.Kind != enum.KindProject || workspace.ID != workspace.ProjectID ||
			workspace.OwnerActorID != input.Principal.ActorID || workspace.State != enum.StateActive {
			return entity.Resource{}, errs.ErrNotFound
		}
		if err := recovery.SwitchWorkspaceProject(ctx, workspace.ID); err != nil {
			return entity.Resource{}, err
		}
		workspaceMembers, memberErr := service.snapshotWorkspaceBackupMembers(
			ctx, tx, input.Principal, workspace,
		)
		if memberErr != nil {
			return entity.Resource{}, memberErr
		}
		members = append(members, workspaceMembers...)
	}
	if err := recovery.SwitchWorkspaceProject(ctx, coordinatorProjectID); err != nil {
		return entity.Resource{}, err
	}
	if len(members) == 0 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	slices.SortFunc(members, func(left, right entity.WorkspaceBackupMember) int {
		if left.WorkspaceID != right.WorkspaceID {
			return compareText(left.WorkspaceID, right.WorkspaceID)
		}
		return compareText(left.SessionID, right.SessionID)
	})
	membershipSHA256, err := canonicalHash(members)
	if err != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	scopeID := ""
	if input.Scope == "WORKSPACE" {
		scopeID = coordinatorProjectID
	}
	created, err := entity.New(uuid.NewString(), input.Principal.OrganizationID, input.Principal.ProjectID,
		"", input.Principal.ActorID, enum.KindWorkspaceBackup, input.Name, entity.WorkspaceBackupSpec{
			Scope: input.Scope, ScopeID: scopeID, Members: members, MembershipSHA256: membershipSHA256,
			BackupState: "VERIFYING", Attempt: 1, Generation: 1, RetainUntil: input.RetainUntil.UTC(),
		}, now)
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if err := tx.Insert(ctx, created); err != nil {
		return entity.Resource{}, err
	}
	return created, appendOwnerStateAudit(ctx, tx, input.Principal, "manage_workspace_backup_create",
		created.OrganizationID, created.ProjectID, created.ID, string(created.Kind), created.Version, now)
}

func (service *Service) snapshotWorkspaceBackupMembers(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	workspace entity.Resource,
) ([]entity.WorkspaceBackupMember, error) {
	resources, err := tx.ListSnapshotResources(ctx, principal.OrganizationID, workspace.ID)
	if err != nil {
		return nil, err
	}
	workspaceSHA, err := entity.ProjectionSHA256(workspace)
	if err != nil {
		return nil, errs.ErrInternal
	}
	members := make([]entity.WorkspaceBackupMember, 0)
	for _, item := range resources {
		session, ok := item.Spec.(entity.SessionSpec)
		if !ok || item.Kind != enum.KindSession || item.OwnerActorID != principal.ActorID ||
			item.State == enum.StateDeleted {
			continue
		}
		// Backup membership — исторический owner snapshot, а не runtime
		// admission. Unassign, archive Agent либо новая Workspace version не
		// вправе молча удалить уже существовавшую Session из полного envelope.
		// Наличие pins фиксируется в Session history; для legacy Session без
		// target assignment также требуется exact terminal archive ниже.
		_ = session
		source, sourceErr := tx.LatestSessionRuntimeArchiveForRestore(
			ctx, principal.OrganizationID, workspace.ID, item.ID,
		)
		if sourceErr != nil {
			if errors.Is(sourceErr, errs.ErrNotFound) {
				return nil, errs.ErrStateConflict
			}
			return nil, sourceErr
		}
		members = append(members, entity.WorkspaceBackupMember{
			SourceExecutionID: source.ID, WorkspaceID: workspace.ID,
			WorkspaceVersion: workspace.Version, WorkspaceSHA256: workspaceSHA,
			SessionID: item.ID, SourceVersion: source.Version,
			RuntimeRevisionSHA256: source.RuntimeRevisionSHA256,
			ImmutableInputSHA256:  source.ImmutableInputSHA256,
			ArchiveSHA256:         source.ArchiveSHA256, ProvenanceSHA256: source.ArchiveProvenanceSHA256,
		})
	}
	return members, nil
}

func compareText(left, right string) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func (service *Service) transitionWorkspaceBackup(
	ctx context.Context,
	tx domainrepo.Transaction,
	input ManageWorkspaceBackupInput,
) (entity.Resource, error) {
	current, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, input.BackupID)
	if err != nil {
		return entity.Resource{}, err
	}
	spec, ok := current.Spec.(entity.WorkspaceBackupSpec)
	if !ok || current.Kind != enum.KindWorkspaceBackup || current.OwnerActorID != input.Principal.ActorID {
		return entity.Resource{}, errs.ErrNotFound
	}
	if current.Version != input.ExpectedVersion {
		return entity.Resource{}, errs.ErrVersionMismatch
	}
	now, err := tx.CurrentTime(ctx)
	if err != nil {
		return entity.Resource{}, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	if input.Action == "cancel" || input.Action == "fail" || input.Action == "expire" {
		if err := service.requireNoOpenWorkspaceRestore(ctx, tx, input.Principal, current.ID); err != nil {
			return entity.Resource{}, err
		}
	}
	var target enum.State
	switch input.Action {
	case "complete":
		if current.State != enum.StateRunning || spec.BackupState != "VERIFYING" || !spec.RetainUntil.After(now) {
			return entity.Resource{}, errs.ErrStateConflict
		}
		if err := service.requireWorkspaceBackupMembersValid(ctx, tx, input.Principal, spec.Members); err != nil {
			return entity.Resource{}, err
		}
		spec.BackupState, spec.RevokedGeneration, target = "AVAILABLE", spec.Generation, enum.StateSucceeded
	case "cancel":
		if current.State != enum.StateRunning {
			return entity.Resource{}, errs.ErrStateConflict
		}
		spec.BackupState, target = "CANCELLED", enum.StateCancelled
	case "fail":
		if current.State != enum.StateRunning {
			return entity.Resource{}, errs.ErrStateConflict
		}
		spec.BackupState, target = "FAILED", enum.StateFailed
	case "expire":
		if current.State != enum.StateRunning && current.State != enum.StateSucceeded || spec.RetainUntil.After(now) {
			return entity.Resource{}, errs.ErrStateConflict
		}
		spec.BackupState, target = "EXPIRED", enum.StateExpired
	case "retry":
		if current.State != enum.StateFailed && current.State != enum.StateCancelled && current.State != enum.StateExpired {
			return entity.Resource{}, errs.ErrStateConflict
		}
		if !input.RetainUntil.IsZero() {
			if !input.RetainUntil.After(now) || input.RetainUntil.After(now.Add(365*24*time.Hour)) {
				return entity.Resource{}, errs.ErrInvalidInput
			}
			spec.RetainUntil = input.RetainUntil.UTC()
		}
		if spec.Attempt == ^uint32(0) || spec.Generation == ^uint64(0) || !spec.RetainUntil.After(now) {
			return entity.Resource{}, errs.ErrStateConflict
		}
		spec.Attempt++
		spec.RevokedGeneration, spec.Generation = spec.Generation, spec.Generation+1
		spec.BackupState, spec.TerminalReasonCode, target = "VERIFYING", "", enum.StateRunning
	}
	if input.Action != "complete" && input.Action != "retry" {
		spec.RevokedGeneration = spec.Generation
		spec.TerminalReasonCode = input.TerminalReasonCode
	}
	updated, err := current.ReplaceAndTransition(spec, target, now)
	if err != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, updated, current.Version); err != nil {
		return entity.Resource{}, err
	}
	return updated, appendOwnerStateAudit(ctx, tx, input.Principal, "manage_workspace_backup_"+input.Action,
		updated.OrganizationID, updated.ProjectID, updated.ID, string(updated.Kind), updated.Version, now)
}

func (service *Service) requireWorkspaceBackupMembersValid(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	members []entity.WorkspaceBackupMember,
) error {
	recovery, ok := tx.(domainrepo.WorkspaceRecoveryTransaction)
	if !ok {
		return errs.ErrInternal
	}
	coordinatorProjectID := principal.ProjectID
	ordered := slices.Clone(members)
	slices.SortFunc(ordered, func(left, right entity.WorkspaceBackupMember) int {
		if left.WorkspaceID != right.WorkspaceID {
			return compareText(left.WorkspaceID, right.WorkspaceID)
		}
		return compareText(left.SourceExecutionID, right.SourceExecutionID)
	})
	for _, member := range ordered {
		if err := recovery.SwitchWorkspaceProject(ctx, member.WorkspaceID); err != nil {
			return err
		}
		workspace, err := tx.GetForUpdate(ctx, principal.OrganizationID, member.WorkspaceID, member.WorkspaceID)
		if err != nil {
			return err
		}
		workspaceSHA, err := entity.ProjectionSHA256(workspace)
		if err != nil || workspace.Kind != enum.KindProject || workspace.State != enum.StateActive ||
			workspace.OwnerActorID != principal.ActorID || workspace.Version != member.WorkspaceVersion ||
			workspaceSHA != member.WorkspaceSHA256 {
			return errs.ErrStateConflict
		}
		execution, err := tx.GetRuntimeExecutionForUpdate(ctx, member.SourceExecutionID)
		if err != nil {
			return err
		}
		if execution.ProjectID != member.WorkspaceID || execution.SessionID != member.SessionID ||
			execution.Version != member.SourceVersion ||
			execution.RuntimeRevisionSHA256 != member.RuntimeRevisionSHA256 ||
			execution.ImmutableInputSHA256 != member.ImmutableInputSHA256 ||
			execution.ArchiveSHA256 != member.ArchiveSHA256 ||
			execution.ArchiveProvenanceSHA256 != member.ProvenanceSHA256 {
			return errs.ErrStateConflict
		}
	}
	return recovery.SwitchWorkspaceProject(ctx, coordinatorProjectID)
}

func (service *Service) requireNoOpenWorkspaceRestore(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	backupID string,
) error {
	resources, err := tx.ListSnapshotResources(ctx, principal.OrganizationID, principal.ProjectID)
	if err != nil {
		return err
	}
	ids := make([]string, 0)
	for _, item := range resources {
		spec, ok := item.Spec.(entity.WorkspaceRestoreSpec)
		if ok && item.Kind == enum.KindWorkspaceRestore && item.OwnerActorID == principal.ActorID &&
			spec.BackupID == backupID && !item.State.Terminal() {
			ids = append(ids, item.ID)
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		current, lockErr := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, id)
		if lockErr != nil {
			return lockErr
		}
		spec, ok := current.Spec.(entity.WorkspaceRestoreSpec)
		if ok && current.Kind == enum.KindWorkspaceRestore && spec.BackupID == backupID &&
			!current.State.Terminal() {
			return errs.ErrStateConflict
		}
	}
	return nil
}

var workspaceRestoreActions = map[string]struct{}{
	"create": {}, "cancel": {}, "retry": {},
}

// ManageWorkspaceRestore создаёт и завершает только полный materialized graph.
func (service *Service) ManageWorkspaceRestore(
	ctx context.Context,
	input ManageWorkspaceRestoreInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionWorkspaceRestoreManage); err != nil {
		return entity.Resource{}, err
	}
	_, actionAllowed := workspaceRestoreActions[input.Action]
	if !actionAllowed || value.ValidateIdempotencyKey(input.IdempotencyKey) != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if input.Action == "create" {
		if input.RestoreID != "" || input.ExpectedVersion != 0 || value.ValidateID(input.BackupID) != nil ||
			input.ExpectedBackupVersion == 0 || !validSHA256Text(input.MembershipSHA256) ||
			value.ValidateName(input.Name) != nil || input.TerminalReasonCode != "" {
			return entity.Resource{}, errs.ErrInvalidInput
		}
	} else if value.ValidateID(input.RestoreID) != nil || input.ExpectedVersion == 0 ||
		(input.Action != "retry" && value.ValidateStableKey(input.TerminalReasonCode) != nil) ||
		input.Action == "retry" && input.TerminalReasonCode != "" ||
		input.BackupID != "" || input.ExpectedBackupVersion != 0 || input.MembershipSHA256 != "" || input.Name != "" {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity                               commandIdentity
		Action, RestoreID, BackupID            string
		ExpectedVersion, ExpectedBackupVersion uint64
		MembershipSHA256, Name, TerminalReason string
	}{identity(input.Principal), input.Action, input.RestoreID, input.BackupID,
		input.ExpectedVersion, input.ExpectedBackupVersion, input.MembershipSHA256,
		input.Name, input.TerminalReasonCode})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	apply := func(tx domainrepo.Transaction) (entity.Resource, error) {
		if input.Action == "create" {
			return service.createWorkspaceRestore(ctx, tx, input)
		}
		return service.transitionWorkspaceRestore(ctx, tx, input)
	}
	scope := "manage_workspace_restore_" + input.Action
	if input.Action == "create" {
		return service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey, scope, requestHash, apply)
	}
	return service.withOwnerLockedResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		scope, requestHash, input.RestoreID, enum.KindWorkspaceRestore, input.ExpectedVersion,
		func(stored entity.Resource) error {
			if stored.ID != input.RestoreID || stored.Kind != enum.KindWorkspaceRestore ||
				stored.OwnerActorID != input.Principal.ActorID || stored.Version <= input.ExpectedVersion ||
				stored.Version > input.ExpectedVersion+2 {
				return errs.ErrStateConflict
			}
			return nil
		}, apply)
}

// ReconcileWorkspaceRestoreTerminal завершает envelope только после exact
// readback всего материализованного member graph.
func (service *Service) ReconcileWorkspaceRestoreTerminal(
	ctx context.Context,
	input ReconcileWorkspaceRecoveryInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionWorkspaceRestoreTerminal); err != nil {
		return entity.Resource{}, err
	}
	if !service.recoveryReconcilerPrincipal(input.Principal) {
		return entity.Resource{}, errs.ErrPermissionDenied
	}
	return service.reconcileWorkspaceRecoveryTerminal(ctx, input, enum.KindWorkspaceRestore)
}

func (service *Service) reconcileWorkspaceRecoveryTerminal(
	ctx context.Context,
	input ReconcileWorkspaceRecoveryInput,
	kind enum.Kind,
) (entity.Resource, error) {
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ResourceID) != nil || input.ExpectedVersion == 0 ||
		input.ExpectedAttempt == 0 || input.ExpectedGeneration == 0 ||
		!slices.Contains([]string{"complete", "fail", "expire"}, input.Outcome) ||
		(input.Outcome == "complete" && input.TerminalReasonCode != "") ||
		(input.Outcome != "complete" && value.ValidateStableKey(input.TerminalReasonCode) != nil) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity                    commandIdentity
		ResourceID, Kind, Outcome   string
		ExpectedVersion, Generation uint64
		Attempt                     uint32
		TerminalReasonCode          string
	}{identity(input.Principal), input.ResourceID, string(kind), input.Outcome,
		input.ExpectedVersion, input.ExpectedGeneration, input.ExpectedAttempt,
		input.TerminalReasonCode})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	var locked entity.Resource
	var receipt workspaceRecoveryTerminalReceipt
	var ownerActorID string
	scope := "reconcile_" + strings.ToLower(string(kind)) + "_" + input.Outcome
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, scope, requestHash, &receipt,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			current, lockErr := tx.GetForUpdateIncludingDeleted(
				ctx, input.Principal.OrganizationID, input.Principal.ProjectID, input.ResourceID,
			)
			if lockErr != nil {
				return 0, lockErr
			}
			if current.Kind != kind || current.OwnerActorID == "" {
				return 0, errs.ErrNotFound
			}
			attempt, generation, specErr := workspaceRecoveryAttemptGeneration(current)
			if specErr != nil || attempt != input.ExpectedAttempt || generation != input.ExpectedGeneration {
				return 0, errs.ErrStateConflict
			}
			locked, ownerActorID = current, current.OwnerActorID
			if current.Version == input.ExpectedVersion {
				return lifecycleReceiptApply, nil
			}
			if current.Version >= input.ExpectedVersion+1 && current.Version <= input.ExpectedVersion+2 &&
				workspaceRecoveryTerminalMatches(current, input.Outcome, input.TerminalReasonCode) {
				return lifecycleReceiptReplay, nil
			}
			return 0, errs.ErrVersionMismatch
		},
		func() error {
			if receipt.ResourceID != locked.ID || receipt.Version != locked.Version ||
				receipt.State != locked.State || receipt.Attempt != input.ExpectedAttempt ||
				receipt.Generation != input.ExpectedGeneration {
				return errs.ErrStateConflict
			}
			digest, digestErr := entity.ProjectionSHA256(locked)
			if digestErr != nil || digest != receipt.SnapshotSHA256 {
				return errs.ErrStateConflict
			}
			return nil
		},
		func(tx domainrepo.Transaction) error {
			ownerPrincipal := input.Principal
			ownerPrincipal.ActorID = ownerActorID
			var updated entity.Resource
			var transitionErr error
			if kind == enum.KindWorkspaceBackup {
				updated, transitionErr = service.transitionWorkspaceBackup(ctx, tx, ManageWorkspaceBackupInput{
					Principal: ownerPrincipal, Action: input.Outcome, BackupID: input.ResourceID,
					ExpectedVersion: input.ExpectedVersion, TerminalReasonCode: input.TerminalReasonCode,
				})
			} else {
				updated, transitionErr = service.transitionWorkspaceRestore(ctx, tx, ManageWorkspaceRestoreInput{
					Principal: ownerPrincipal, Action: input.Outcome, RestoreID: input.ResourceID,
					ExpectedVersion: input.ExpectedVersion, TerminalReasonCode: input.TerminalReasonCode,
				})
			}
			if transitionErr != nil {
				return transitionErr
			}
			digest, digestErr := entity.ProjectionSHA256(updated)
			if digestErr != nil {
				return errs.ErrInternal
			}
			attempt, generation, specErr := workspaceRecoveryAttemptGeneration(updated)
			if specErr != nil {
				return specErr
			}
			receipt = workspaceRecoveryTerminalReceipt{ResourceID: updated.ID, Version: updated.Version,
				State: updated.State, Attempt: attempt, Generation: generation, SnapshotSHA256: digest}
			locked = updated
			return appendOwnerStateAudit(ctx, tx, input.Principal, scope,
				updated.OrganizationID, updated.ProjectID, updated.ID, string(updated.Kind), updated.Version, updated.UpdatedAt)
		},
	)
	if err != nil {
		return entity.Resource{}, err
	}
	if locked.Version != receipt.Version {
		locked, err = service.repository.GetIncludingDeleted(
			ctx, input.Principal.OrganizationID, input.Principal.ProjectID, input.ResourceID, kind,
		)
		if err != nil {
			return entity.Resource{}, err
		}
	}
	return locked, nil
}

func workspaceRecoveryAttemptGeneration(resource entity.Resource) (uint32, uint64, error) {
	switch spec := resource.Spec.(type) {
	case entity.WorkspaceBackupSpec:
		return spec.Attempt, spec.Generation, nil
	case entity.WorkspaceRestoreSpec:
		return spec.Attempt, spec.Generation, nil
	default:
		return 0, 0, errs.ErrStateConflict
	}
}

func workspaceRecoveryTerminalMatches(resource entity.Resource, outcome, reason string) bool {
	state := map[string]enum.State{"complete": enum.StateSucceeded, "fail": enum.StateFailed, "expire": enum.StateExpired}[outcome]
	if resource.State != state {
		return false
	}
	switch spec := resource.Spec.(type) {
	case entity.WorkspaceBackupSpec:
		return spec.TerminalReasonCode == reason
	case entity.WorkspaceRestoreSpec:
		return spec.TerminalReasonCode == reason
	default:
		return false
	}
}

func (service *Service) createWorkspaceRestore(
	ctx context.Context,
	tx domainrepo.Transaction,
	input ManageWorkspaceRestoreInput,
) (entity.Resource, error) {
	recovery, ok := tx.(domainrepo.WorkspaceRecoveryTransaction)
	if !ok {
		return entity.Resource{}, errs.ErrInternal
	}
	coordinatorProjectID := input.Principal.ProjectID
	backup, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, input.BackupID)
	if err != nil {
		return entity.Resource{}, err
	}
	backupSpec, ok := backup.Spec.(entity.WorkspaceBackupSpec)
	if !ok || backup.Kind != enum.KindWorkspaceBackup || backup.OwnerActorID != input.Principal.ActorID {
		return entity.Resource{}, errs.ErrNotFound
	}
	now, err := tx.CurrentTime(ctx)
	if err != nil {
		return entity.Resource{}, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	if backup.Version != input.ExpectedBackupVersion || backup.State != enum.StateSucceeded ||
		backupSpec.BackupState != "AVAILABLE" || backupSpec.MembershipSHA256 != input.MembershipSHA256 ||
		!backupSpec.RetainUntil.After(now) {
		return entity.Resource{}, errs.ErrStateConflict
	}
	sources := slices.Clone(backupSpec.Members)
	slices.SortFunc(sources, func(left, right entity.WorkspaceBackupMember) int {
		if left.WorkspaceID != right.WorkspaceID {
			return compareText(left.WorkspaceID, right.WorkspaceID)
		}
		return compareText(left.SourceExecutionID, right.SourceExecutionID)
	})
	members := make([]entity.WorkspaceRestoreMember, 0, len(sources))
	for _, source := range sources {
		if err := recovery.SwitchWorkspaceProject(ctx, source.WorkspaceID); err != nil {
			return entity.Resource{}, err
		}
		memberPrincipal := input.Principal
		memberPrincipal.ProjectID = source.WorkspaceID
		workspace, workspaceErr := tx.GetForUpdate(
			ctx, input.Principal.OrganizationID, source.WorkspaceID, source.WorkspaceID,
		)
		if workspaceErr != nil {
			return entity.Resource{}, workspaceErr
		}
		workspaceSHA, digestErr := entity.ProjectionSHA256(workspace)
		if digestErr != nil || workspace.Kind != enum.KindProject ||
			workspace.OwnerActorID != input.Principal.ActorID || workspace.State != enum.StateActive ||
			workspace.Version != source.WorkspaceVersion || workspaceSHA != source.WorkspaceSHA256 {
			return entity.Resource{}, errs.ErrStateConflict
		}
		execution, err := tx.GetRuntimeExecutionForUpdate(ctx, source.SourceExecutionID)
		if err != nil {
			return entity.Resource{}, err
		}
		if execution.SessionID != source.SessionID || execution.Version != source.SourceVersion ||
			execution.RuntimeRevisionSHA256 != source.RuntimeRevisionSHA256 ||
			execution.ImmutableInputSHA256 != source.ImmutableInputSHA256 ||
			execution.ArchiveSHA256 != source.ArchiveSHA256 ||
			execution.ArchiveProvenanceSHA256 != source.ProvenanceSHA256 {
			return entity.Resource{}, errs.ErrStateConflict
		}
		result, err := service.retryWorkspaceExecution(ctx, tx, memberPrincipal, execution,
			&restoreRuntimeIntent{BackupID: execution.ID, SourceVersion: execution.Version,
				SourceFence: execution.Fence, ExpectedBackupVersion: execution.Version,
				ArchiveSHA256: execution.ArchiveSHA256, ProvenanceSHA256: execution.ArchiveProvenanceSHA256,
				SessionID: execution.SessionID}, now)
		if err != nil || result.Restore == nil {
			if err != nil {
				return entity.Resource{}, err
			}
			return entity.Resource{}, errs.ErrInternal
		}
		turnSpec, ok := result.Turn.Spec.(entity.TurnSpec)
		if !ok {
			return entity.Resource{}, errs.ErrInternal
		}
		grantSHA256, err := canonicalHash(struct {
			TurnID, InputSHA256, RestoreOperationID string
			Attempt                                 uint32
			Generation                              uint64
		}{result.Turn.ID, turnSpec.EffectiveInputSHA256, result.Restore.ID,
			turnSpec.Attempt, result.Restore.Generation})
		if err != nil {
			return entity.Resource{}, errs.ErrInternal
		}
		members = append(members, entity.WorkspaceRestoreMember{
			SourceExecutionID: source.SourceExecutionID, WorkspaceID: source.WorkspaceID,
			SourceSessionID: source.SessionID,
			TargetTurnID:    result.Turn.ID, TargetTurnVersion: result.Turn.Version,
			TargetAttempt: turnSpec.Attempt, RuntimeRevisionID: turnSpec.RuntimeRevisionID,
			RuntimeRevisionVersion: 1,
			ImmutableInputSHA256:   turnSpec.EffectiveInputSHA256, GrantSHA256: grantSHA256, State: "QUEUED",
		})
	}
	if err := recovery.SwitchWorkspaceProject(ctx, coordinatorProjectID); err != nil {
		return entity.Resource{}, err
	}
	created, err := entity.New(uuid.NewString(), input.Principal.OrganizationID, input.Principal.ProjectID,
		backup.ID, input.Principal.ActorID, enum.KindWorkspaceRestore, input.Name, entity.WorkspaceRestoreSpec{
			BackupID: backup.ID, BackupVersion: backup.Version, MembershipSHA256: backupSpec.MembershipSHA256,
			Members: members, RestoreState: "QUEUED", Attempt: 1, Generation: 1,
		}, now)
	if err != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	if err := tx.Insert(ctx, created); err != nil {
		return entity.Resource{}, err
	}
	return created, appendOwnerStateAudit(ctx, tx, input.Principal, "manage_workspace_restore_create",
		created.OrganizationID, created.ProjectID, created.ID, string(created.Kind), created.Version, now)
}

func (service *Service) retryWorkspaceExecution(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	execution RuntimeExecution,
	restore *restoreRuntimeIntent,
	now time.Time,
) (RetryRuntimeExecutionResult, error) {
	graph, err := service.lockOwnerGraphByTurn(ctx, tx, principal, execution.TurnID)
	if err != nil || graph.Runtime == nil || graph.Runtime.ID != execution.ID {
		if err != nil {
			return RetryRuntimeExecutionResult{}, err
		}
		return RetryRuntimeExecutionResult{}, errs.ErrStateConflict
	}
	if err := service.requireRuntimeOwner(ctx, tx, principal, execution); err != nil {
		return RetryRuntimeExecutionResult{}, err
	}
	if execution.Version != graph.Runtime.Version || execution.Fence != graph.Runtime.Fence {
		return RetryRuntimeExecutionResult{}, errs.ErrStateConflict
	}
	var operation *domainrepo.RuntimeRestoreOperation
	var previousGeneration uint64
	if restore != nil {
		latest, latestErr := tx.LatestSessionRuntimeArchiveForRestore(ctx, principal.OrganizationID,
			principal.ProjectID, execution.SessionID)
		if latestErr != nil || validateRestoreRuntimeSource(execution, latest, *restore, now) != nil {
			if latestErr != nil && !errors.Is(latestErr, errs.ErrNotFound) {
				return RetryRuntimeExecutionResult{}, latestErr
			}
			return RetryRuntimeExecutionResult{}, errs.ErrStateConflict
		}
		sourceAuthoritySHA256, err := runtimeRestoreSourceAuthoritySHA256(execution)
		if err != nil {
			return RetryRuntimeExecutionResult{}, err
		}
		operation = &domainrepo.RuntimeRestoreOperation{
			ID: uuid.NewString(), OrganizationID: execution.OrganizationID, ProjectID: execution.ProjectID,
			OwnerActorID: principal.ActorID, BackupID: execution.ID, SourceVersion: execution.Version,
			SourceFence: execution.Fence, ArchiveSHA256: execution.ArchiveSHA256,
			ProvenanceSHA256: execution.ArchiveProvenanceSHA256, SourceAuthoritySHA256: sourceAuthoritySHA256,
			Generation: 1, SessionID: execution.SessionID, CreatedAt: now, UpdatedAt: now,
		}
	} else {
		turnSpec, ok := graph.Turn.Spec.(entity.TurnSpec)
		if !ok {
			return RetryRuntimeExecutionResult{}, errs.ErrStateConflict
		}
		if turnSpec.RestoreOperationID != "" {
			stored, operationErr := tx.GetRuntimeRestoreOperation(ctx, turnSpec.RestoreOperationID)
			if operationErr != nil || stored.TargetTurnID != graph.Turn.ID || stored.TargetAttempt != turnSpec.Attempt ||
				stored.Generation != turnSpec.RestoreOperationGeneration || stored.RevokedGeneration < stored.Generation {
				if operationErr != nil {
					return RetryRuntimeExecutionResult{}, operationErr
				}
				return RetryRuntimeExecutionResult{}, errs.ErrStateConflict
			}
			previousGeneration = stored.Generation
			stored.Generation++
			stored.UpdatedAt = now
			operation = &stored
		}
	}
	turn := graph.Turn
	turnSpec, ok := turn.Spec.(entity.TurnSpec)
	if !ok || turnSpec.SessionID != execution.SessionID || turnSpec.Attempt != execution.Attempt {
		return RetryRuntimeExecutionResult{}, errs.ErrStateConflict
	}
	switch execution.State {
	case "FAILED", "CANCELLED", "EXPIRED":
		turn, err = service.requireRetryableClosedRuntimeGraph(ctx, tx, principal, execution)
	default:
		return RetryRuntimeExecutionResult{}, errs.ErrStateConflict
	}
	if err != nil {
		return RetryRuntimeExecutionResult{}, err
	}
	open, err := tx.ProcessHasOpenWork(ctx, principal.OrganizationID, principal.ProjectID,
		execution.ProcessID, execution.TurnID, "")
	if err != nil || open {
		if err != nil {
			return RetryRuntimeExecutionResult{}, err
		}
		return RetryRuntimeExecutionResult{}, errs.ErrStateConflict
	}
	turnSpec, ok = turn.Spec.(entity.TurnSpec)
	if !ok {
		return RetryRuntimeExecutionResult{}, errs.ErrInternal
	}
	graph.Turn = turn
	retried, retriedSpec, err := service.prepareRetriedExecution(ctx, tx, principal, graph, turnSpec, operation, now)
	if err != nil {
		return RetryRuntimeExecutionResult{}, err
	}
	if err := tx.Update(ctx, retried, turn.Version); err != nil {
		return RetryRuntimeExecutionResult{}, err
	}
	authorityGeneration := principal.AuthorityGeneration
	if operation != nil {
		authorityGeneration = operation.Generation
	}
	if err := tx.SaveTurnAttempt(ctx, domainrepo.TurnAttempt{
		TurnID: retried.ID, Attempt: retriedSpec.Attempt, WorkloadID: "unassigned",
		AuthorityGeneration: authorityGeneration, State: "QUEUED",
		InputSHA256: retriedSpec.EffectiveInputSHA256, LeaseFence: retried.Version,
		StartedAt: now, Outcome: "workspace_restore",
	}); err != nil {
		return RetryRuntimeExecutionResult{}, err
	}
	if err := service.appendMutationRecords(ctx, tx, principal, "workspace_restore_turn", retried); err != nil {
		return RetryRuntimeExecutionResult{}, err
	}
	if operation != nil {
		operation.TargetTurnID, operation.TargetAttempt = retried.ID, retriedSpec.Attempt
		operation.TargetExecutionID = runtimeExecutionID(retried.ID, retriedSpec.Attempt)
		if previousGeneration == 0 {
			if err := tx.InsertRuntimeRestoreOperation(ctx, *operation); err != nil {
				return RetryRuntimeExecutionResult{}, err
			}
		} else if err := tx.AdvanceRuntimeRestoreOperation(ctx, *operation, previousGeneration); err != nil {
			return RetryRuntimeExecutionResult{}, err
		}
	}
	if err := service.rebindIntegrationContinuationRetry(ctx, tx, principal, execution, retried, retriedSpec, now); err != nil {
		return RetryRuntimeExecutionResult{}, err
	}
	expectedVersion, expectedFence := execution.Version, execution.Fence
	execution.Version++
	execution.Fence++
	execution.State = "RETRIED"
	if err := pinRuntimeRetention(&execution, now); err != nil {
		return RetryRuntimeExecutionResult{}, err
	}
	execution.LeaseID, execution.LeaseTokenSHA256, execution.LeaseExpiresAt = "", "", time.Time{}
	execution.UpdatedAt = now
	if err := tx.UpdateRuntimeExecution(ctx, execution, expectedVersion, expectedFence); err != nil {
		return RetryRuntimeExecutionResult{}, err
	}
	if err := service.appendLifecycleAudit(ctx, tx, principal, "workspace_restore_source_retried",
		execution.ID, "RUNTIME_EXECUTION", execution.Version, now); err != nil {
		return RetryRuntimeExecutionResult{}, err
	}
	return RetryRuntimeExecutionResult{Previous: execution, Turn: retried, Restore: operation}, nil
}

func (service *Service) transitionWorkspaceRestore(
	ctx context.Context,
	tx domainrepo.Transaction,
	input ManageWorkspaceRestoreInput,
) (entity.Resource, error) {
	recovery, ok := tx.(domainrepo.WorkspaceRecoveryTransaction)
	if !ok {
		return entity.Resource{}, errs.ErrInternal
	}
	coordinatorProjectID := input.Principal.ProjectID
	current, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, input.RestoreID)
	if err != nil {
		return entity.Resource{}, err
	}
	spec, ok := current.Spec.(entity.WorkspaceRestoreSpec)
	if !ok || current.Kind != enum.KindWorkspaceRestore || current.OwnerActorID != input.Principal.ActorID {
		return entity.Resource{}, errs.ErrNotFound
	}
	if current.Version != input.ExpectedVersion {
		return entity.Resource{}, errs.ErrVersionMismatch
	}
	now, err := tx.CurrentTime(ctx)
	if err != nil {
		return entity.Resource{}, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	if input.Action == "retry" {
		if current.State != enum.StateFailed && current.State != enum.StateCancelled && current.State != enum.StateExpired ||
			spec.Attempt == ^uint32(0) || spec.Generation == ^uint64(0) {
			return entity.Resource{}, errs.ErrStateConflict
		}
		backup, backupErr := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
			input.Principal.ProjectID, spec.BackupID)
		backupSpec, backupOK := backup.Spec.(entity.WorkspaceBackupSpec)
		if backupErr != nil || !backupOK || backup.Kind != enum.KindWorkspaceBackup ||
			backup.OwnerActorID != input.Principal.ActorID || backup.State != enum.StateSucceeded ||
			backupSpec.BackupState != "AVAILABLE" ||
			backupSpec.MembershipSHA256 != spec.MembershipSHA256 || !backupSpec.RetainUntil.After(now) {
			if backupErr != nil {
				return entity.Resource{}, backupErr
			}
			return entity.Resource{}, errs.ErrStateConflict
		}
		members := slices.Clone(spec.Members)
		slices.SortFunc(members, func(left, right entity.WorkspaceRestoreMember) int {
			if left.WorkspaceID != right.WorkspaceID {
				return compareText(left.WorkspaceID, right.WorkspaceID)
			}
			return compareText(left.TargetTurnID, right.TargetTurnID)
		})
		for index := range members {
			if err := recovery.SwitchWorkspaceProject(ctx, members[index].WorkspaceID); err != nil {
				return entity.Resource{}, err
			}
			memberPrincipal := input.Principal
			memberPrincipal.ProjectID = members[index].WorkspaceID
			member, retryErr := service.retryWorkspaceRestoreMember(ctx, tx, memberPrincipal, members[index], now)
			if retryErr != nil {
				return entity.Resource{}, retryErr
			}
			members[index] = member
		}
		if err := recovery.SwitchWorkspaceProject(ctx, coordinatorProjectID); err != nil {
			return entity.Resource{}, err
		}
		slices.SortFunc(members, func(left, right entity.WorkspaceRestoreMember) int {
			return compareText(left.SourceExecutionID, right.SourceExecutionID)
		})
		spec.Members = members
		spec.BackupVersion = backup.Version
		spec.Attempt++
		spec.RevokedGeneration, spec.Generation = spec.Generation, spec.Generation+1
		spec.RestoreState, spec.TerminalReasonCode = "QUEUED", ""
		updated, transitionErr := current.ReplaceAndTransition(spec, enum.StateQueued, now)
		if transitionErr != nil {
			return entity.Resource{}, errs.ErrStateConflict
		}
		if err := tx.Update(ctx, updated, current.Version); err != nil {
			return entity.Resource{}, err
		}
		return updated, appendOwnerStateAudit(ctx, tx, input.Principal, "manage_workspace_restore_retry",
			updated.OrganizationID, updated.ProjectID, updated.ID, string(updated.Kind), updated.Version, now)
	}
	if current.State != enum.StateQueued && current.State != enum.StateRunning {
		return entity.Resource{}, errs.ErrStateConflict
	}
	targetState, memberState := enum.StateCancelled, "CANCELLED"
	switch input.Action {
	case "complete":
		targetState, memberState = enum.StateSucceeded, "SUCCEEDED"
	case "fail":
		targetState, memberState = enum.StateFailed, "FAILED"
	case "expire":
		backup, backupErr := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
			input.Principal.ProjectID, spec.BackupID)
		backupSpec, backupOK := backup.Spec.(entity.WorkspaceBackupSpec)
		if backupErr != nil || !backupOK || backup.Kind != enum.KindWorkspaceBackup ||
			backup.OwnerActorID != input.Principal.ActorID ||
			backupSpec.MembershipSHA256 != spec.MembershipSHA256 || backupSpec.RetainUntil.After(now) {
			if backupErr != nil {
				return entity.Resource{}, backupErr
			}
			return entity.Resource{}, errs.ErrStateConflict
		}
		targetState, memberState = enum.StateExpired, "EXPIRED"
	case "cancel":
	default:
		return entity.Resource{}, errs.ErrInvalidInput
	}
	members := slices.Clone(spec.Members)
	slices.SortFunc(members, func(left, right entity.WorkspaceRestoreMember) int {
		if left.WorkspaceID != right.WorkspaceID {
			return compareText(left.WorkspaceID, right.WorkspaceID)
		}
		return compareText(left.TargetTurnID, right.TargetTurnID)
	})
	for index := range members {
		if err := recovery.SwitchWorkspaceProject(ctx, members[index].WorkspaceID); err != nil {
			return entity.Resource{}, err
		}
		memberPrincipal := input.Principal
		memberPrincipal.ProjectID = members[index].WorkspaceID
		if input.Action == "complete" {
			if err := service.requireWorkspaceRestoreMemberSucceeded(ctx, tx, memberPrincipal, members[index]); err != nil {
				return entity.Resource{}, err
			}
		} else if err := service.closeWorkspaceRestoreMember(ctx, tx, memberPrincipal, members[index], input.TerminalReasonCode, now); err != nil {
			return entity.Resource{}, err
		}
		members[index].State = memberState
	}
	if err := recovery.SwitchWorkspaceProject(ctx, coordinatorProjectID); err != nil {
		return entity.Resource{}, err
	}
	slices.SortFunc(members, func(left, right entity.WorkspaceRestoreMember) int {
		return compareText(left.SourceExecutionID, right.SourceExecutionID)
	})
	spec.Members = members
	spec.RestoreState = memberState
	spec.RevokedGeneration = spec.Generation
	if input.Action == "complete" {
		spec.TerminalReasonCode = ""
	} else {
		spec.TerminalReasonCode = input.TerminalReasonCode
	}
	base := current
	if current.State == enum.StateQueued && (targetState == enum.StateSucceeded || targetState == enum.StateFailed) {
		spec.RestoreState = "RUNNING"
		spec.RevokedGeneration = 0
		running, transitionErr := current.ReplaceAndTransition(spec, enum.StateRunning, now)
		if transitionErr != nil {
			return entity.Resource{}, errs.ErrStateConflict
		}
		if err := tx.Update(ctx, running, current.Version); err != nil {
			return entity.Resource{}, err
		}
		if err := appendOwnerStateAudit(ctx, tx, input.Principal, "manage_workspace_restore_running",
			running.OrganizationID, running.ProjectID, running.ID, string(running.Kind), running.Version, now); err != nil {
			return entity.Resource{}, err
		}
		base = running
		spec.RestoreState = memberState
		spec.RevokedGeneration = spec.Generation
	}
	updated, err := base.ReplaceAndTransition(spec, targetState, now)
	if err != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, updated, base.Version); err != nil {
		return entity.Resource{}, err
	}
	return updated, appendOwnerStateAudit(ctx, tx, input.Principal, "manage_workspace_restore_"+input.Action,
		updated.OrganizationID, updated.ProjectID, updated.ID, string(updated.Kind), updated.Version, now)
}

func (service *Service) requireWorkspaceRestoreMemberSucceeded(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	member entity.WorkspaceRestoreMember,
) error {
	turn, err := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, member.TargetTurnID)
	if err != nil {
		return err
	}
	turnSpec, ok := turn.Spec.(entity.TurnSpec)
	if !ok || turn.OwnerActorID != principal.ActorID || turn.State != enum.StateSucceeded ||
		turnSpec.Attempt != member.TargetAttempt || turnSpec.RuntimeRevisionID != member.RuntimeRevisionID ||
		turnSpec.EffectiveInputSHA256 != member.ImmutableInputSHA256 {
		return errs.ErrStateConflict
	}
	execution, err := tx.GetRuntimeExecutionByTurnForUpdate(ctx, turn.ID, turnSpec.Attempt)
	if err != nil || execution.State != "SUCCEEDED" || execution.TurnID != turn.ID {
		if err != nil {
			return err
		}
		return errs.ErrStateConflict
	}
	return nil
}

func (service *Service) closeWorkspaceRestoreMember(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	member entity.WorkspaceRestoreMember,
	reason string,
	now time.Time,
) error {
	graph, err := service.lockOwnerGraphByTurn(ctx, tx, principal, member.TargetTurnID)
	if err != nil {
		return err
	}
	turnSpec, ok := graph.Turn.Spec.(entity.TurnSpec)
	if !ok || turnSpec.Attempt != member.TargetAttempt || turnSpec.RuntimeRevisionID != member.RuntimeRevisionID ||
		turnSpec.EffectiveInputSHA256 != member.ImmutableInputSHA256 {
		return errs.ErrStateConflict
	}
	if graph.Runtime != nil && !runtimeTerminal(graph.Runtime.State) {
		if graph.Runtime.State == "ADMITTED" || graph.Runtime.State == "RUNNING" {
			_, err := service.terminateRuntimeGraphForOwner(
				ctx, tx, principal, principal.ActorID, *graph.Runtime, reason,
				"workspace_restore_cancel_runtime", now,
			)
			return err
		}
		if graph.Runtime.State != "PENDING" {
			return errs.ErrStateConflict
		}
		closedTurn, err := service.cancelTurnExecution(ctx, tx, principal, graph.Turn, reason, now)
		if err != nil {
			return err
		}
		if err := service.completeRuntimeProcessFromTurn(ctx, tx, principal, closedTurn); err != nil {
			return err
		}
		execution := *graph.Runtime
		expectedVersion, expectedFence := execution.Version, execution.Fence
		execution.Version++
		execution.Fence++
		execution.State = "CANCELLED"
		if err := pinRuntimeRetention(&execution, now); err != nil {
			return err
		}
		execution.TerminalOutcome, execution.TerminalReference = "CANCELLED", reason
		execution.TerminalSHA256 = hashString(reason)
		execution.LeaseID, execution.LeaseTokenSHA256, execution.LeaseExpiresAt = "", "", time.Time{}
		execution.UpdatedAt = now
		if err := tx.UpdateRuntimeExecution(ctx, execution, expectedVersion, expectedFence); err != nil {
			return err
		}
		return service.appendLifecycleAudit(ctx, tx, principal, "workspace_restore_cancel_runtime",
			execution.ID, "RUNTIME_EXECUTION", execution.Version, now)
	}
	if !graph.Turn.State.Terminal() {
		if _, err := service.cancelTurnExecution(ctx, tx, principal, graph.Turn, reason, now); err != nil {
			return err
		}
	}
	if graph.Process.ID != "" && !graph.Process.State.Terminal() {
		if active, err := tx.HasActiveChildProcesses(ctx, principal.OrganizationID, principal.ProjectID, graph.Process.ID); err != nil {
			return err
		} else if active {
			return errs.ErrStateConflict
		}
		cancelled, err := graph.Process.Transition(enum.StateCancelled, now)
		if err != nil {
			return errs.ErrStateConflict
		}
		if err := tx.Update(ctx, cancelled, graph.Process.Version); err != nil {
			return err
		}
		if err := service.appendMutationRecords(ctx, tx, principal, "workspace_restore_cancel_process", cancelled); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) retryWorkspaceRestoreMember(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	member entity.WorkspaceRestoreMember,
	now time.Time,
) (entity.WorkspaceRestoreMember, error) {
	execution, err := tx.GetRuntimeExecutionByTurnForUpdate(ctx, member.TargetTurnID, member.TargetAttempt)
	if err == nil {
		result, retryErr := service.retryWorkspaceExecution(ctx, tx, principal, execution, nil, now)
		if retryErr != nil {
			return entity.WorkspaceRestoreMember{}, retryErr
		}
		return workspaceRestoreMemberFromRetry(member.SourceExecutionID, member.WorkspaceID, result)
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return entity.WorkspaceRestoreMember{}, err
	}
	graph, err := service.lockOwnerGraphByTurn(ctx, tx, principal, member.TargetTurnID)
	if err != nil {
		return entity.WorkspaceRestoreMember{}, err
	}
	turnSpec, ok := graph.Turn.Spec.(entity.TurnSpec)
	if !ok || graph.Turn.OwnerActorID != principal.ActorID || !graph.Turn.State.Terminal() ||
		turnSpec.Attempt != member.TargetAttempt || turnSpec.RestoreOperationID == "" {
		return entity.WorkspaceRestoreMember{}, errs.ErrStateConflict
	}
	operation, err := tx.GetRuntimeRestoreOperation(ctx, turnSpec.RestoreOperationID)
	if err != nil || operation.TargetTurnID != graph.Turn.ID || operation.Generation != turnSpec.RestoreOperationGeneration ||
		operation.RevokedGeneration < operation.Generation {
		if err != nil {
			return entity.WorkspaceRestoreMember{}, err
		}
		return entity.WorkspaceRestoreMember{}, errs.ErrStateConflict
	}
	previousGeneration := operation.Generation
	operation.Generation++
	operation.UpdatedAt = now
	retried, retriedSpec, err := service.prepareRetriedExecution(ctx, tx, principal, graph, turnSpec, &operation, now)
	if err != nil {
		return entity.WorkspaceRestoreMember{}, err
	}
	if err := tx.Update(ctx, retried, graph.Turn.Version); err != nil {
		return entity.WorkspaceRestoreMember{}, err
	}
	if err := tx.SaveTurnAttempt(ctx, domainrepo.TurnAttempt{TurnID: retried.ID, Attempt: retriedSpec.Attempt,
		WorkloadID: "unassigned", AuthorityGeneration: operation.Generation, State: "QUEUED",
		InputSHA256: retriedSpec.EffectiveInputSHA256, LeaseFence: retried.Version, StartedAt: now,
		Outcome: "workspace_restore_retry"}); err != nil {
		return entity.WorkspaceRestoreMember{}, err
	}
	operation.TargetAttempt = retriedSpec.Attempt
	operation.TargetExecutionID = runtimeExecutionID(retried.ID, retriedSpec.Attempt)
	if err := tx.AdvanceRuntimeRestoreOperation(ctx, operation, previousGeneration); err != nil {
		return entity.WorkspaceRestoreMember{}, err
	}
	if err := service.appendMutationRecords(ctx, tx, principal, "workspace_restore_retry_turn", retried); err != nil {
		return entity.WorkspaceRestoreMember{}, err
	}
	result := RetryRuntimeExecutionResult{Turn: retried, Restore: &operation}
	return workspaceRestoreMemberFromRetry(member.SourceExecutionID, member.WorkspaceID, result)
}

func workspaceRestoreMemberFromRetry(
	sourceExecutionID, workspaceID string,
	result RetryRuntimeExecutionResult,
) (entity.WorkspaceRestoreMember, error) {
	if result.Restore == nil {
		return entity.WorkspaceRestoreMember{}, errs.ErrInternal
	}
	spec, ok := result.Turn.Spec.(entity.TurnSpec)
	if !ok {
		return entity.WorkspaceRestoreMember{}, errs.ErrInternal
	}
	grantSHA256, err := canonicalHash(struct {
		TurnID, InputSHA256, RestoreOperationID string
		Attempt                                 uint32
		Generation                              uint64
	}{result.Turn.ID, spec.EffectiveInputSHA256, result.Restore.ID, spec.Attempt, result.Restore.Generation})
	if err != nil {
		return entity.WorkspaceRestoreMember{}, errs.ErrInternal
	}
	return entity.WorkspaceRestoreMember{SourceExecutionID: sourceExecutionID,
		WorkspaceID:     workspaceID,
		SourceSessionID: result.Restore.SessionID, TargetTurnID: result.Turn.ID,
		TargetTurnVersion: result.Turn.Version, TargetAttempt: spec.Attempt,
		RuntimeRevisionID: spec.RuntimeRevisionID, RuntimeRevisionVersion: 1,
		ImmutableInputSHA256: spec.EffectiveInputSHA256, GrantSHA256: grantSHA256, State: "QUEUED"}, nil
}
