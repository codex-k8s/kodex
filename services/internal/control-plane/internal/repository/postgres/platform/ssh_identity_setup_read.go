package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// querySSHSetupReadOwned selects only the original current actor's safe metadata and never recovers a permit or private material locator from a historical setup.
// querySSHSetupReadOwned выбирает лишь safe metadata original current actor и не восстанавливает permit либо private material locator исторического setup.
//
//go:embed sql/ssh_setup__read_owned.sql
var querySSHSetupReadOwned string

// sshSetupAPICaller confines public setup persistence to the authenticated API technical operation before resolving the current user's tenant and resource eligibility.
// sshSetupAPICaller ограничивает public setup persistence authenticated API technical operation до разрешения current user tenant и resource eligibility.
func sshSetupAPICaller(principal value.Principal, permission string) error {
	if principal.Validate() != nil || principal.CallerWorkload != "control-api-gateway" || principal.Permission != permission {
		return errs.ErrForbidden
	}
	return nil
}

// readSSHSetupView rejects corrupt or mixed public metadata; EXPIRED describes closed authority, never remote absence or permission to recreate material.
// readSSHSetupView отклоняет corrupt либо mixed public metadata; EXPIRED обозначает closed authority, а не remote absence либо permission на recreate material.
func readSSHSetupView(row pgx.Row) (entity.SSHIdentitySetupView, error) {
	var view entity.SSHIdentitySetupView
	err := row.Scan(&view.Ref, &view.ConnectionRef, &view.IdentityRef, &view.TargetRef, &view.ReasonCode, &view.Version, &view.ConnectionVersion,
		&view.BindingVersion, &view.BindingGeneration, &view.IdentityRevision, &view.Policy, &view.State, &view.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return view, errs.ErrNotFound
	}
	if err != nil {
		return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
	}
	return canonicalSSHSetupView(view)
}

// canonicalSSHSetupView validates the same finite metadata shape for authoritative rows and retained command receipts; only UTC normalization is permitted.
// canonicalSSHSetupView проверяет одну finite metadata shape authoritative rows и retained command receipts; допускается лишь UTC normalization.
func canonicalSSHSetupView(view entity.SSHIdentitySetupView) (entity.SSHIdentitySetupView, error) {
	view.ExpiresAt = view.ExpiresAt.UTC()
	if !managedssh.OwnerReference(view.Ref) || !strings.HasPrefix(view.Ref, "ssu_") || !managedssh.OwnerReference(view.ConnectionRef) || !managedssh.OwnerReference(view.IdentityRef) || !managedssh.OwnerReference(view.TargetRef) ||
		view.Version < 1 || view.ConnectionVersion < 1 || view.IdentityRevision < 1 || view.ExpiresAt.IsZero() || !view.State.Valid() || !sshSetupReason(view.ReasonCode) {
		return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
	}
	switch view.Policy {
	case entity.IntegrationCredentialInitiatorRequired:
		if view.BindingVersion < 1 || view.BindingGeneration < 1 {
			return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
		}
	case entity.IntegrationCredentialProjectShared:
		if view.BindingVersion != 0 || view.BindingGeneration != 0 {
			return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
		}
	default:
		return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
	}
	return view, nil
}

// sshSetupReason permits only the owner's closed metadata codes and never a transport error, key parser diagnostic or caller-provided text.
// sshSetupReason допускает лишь owner closed metadata codes без transport error, key parser diagnostic либо caller-provided text.
func sshSetupReason(reason string) bool {
	switch reason {
	case "PREPARED", "ATTEMPTING", "MATERIAL_UNKNOWN", "MISSING_COMPLETION", "ACK_REJECTED", "COMPLETED", "CANCELLED", "EXPIRED", "STALE_OWNER", "MATERIAL_RECORDED_NO_ATTACHMENT", "QUARANTINED", "SETUP_EXPIRED", "SETUP_EXPIRED_WITH_MATERIAL_UNRESOLVED", "OWNER_REVOKED", "OWNER_REPLACED":
		return true
	default:
		return false
	}
}

// GetSSHIdentitySetup rereads current membership and Connection eligibility before either an addressed setup or a Begin receipt; neither route returns a raw permit or claims attachment from a storage receipt.
// GetSSHIdentitySetup заново проверяет current membership и Connection eligibility до addressed setup либо Begin receipt; оба route не возвращают raw permit и не выводят attachment из storage receipt.
func (repository *Repository) GetSSHIdentitySetup(ctx context.Context, principal value.Principal, input command.SSHIdentitySetupRead) (entity.SSHIdentitySetupView, error) {
	if err := sshSetupAPICaller(principal, "platform.integrations.ssh.setup.get"); err != nil {
		return entity.SSHIdentitySetupView{}, err
	}
	if !managedssh.OwnerReference(input.ConnectionRef) || (input.SetupRef == "") == (input.BeginIdempotencyKey == "") || input.SetupRef != "" && (!managedssh.OwnerReference(input.SetupRef) || !strings.HasPrefix(input.SetupRef, "ssu_")) || len(input.BeginIdempotencyKey) > 128 {
		return entity.SSHIdentitySetupView{}, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.SSHIdentitySetupView{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.requireAccess(ctx, tx, current, "integration.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: input.ConnectionRef}); err != nil {
		return entity.SSHIdentitySetupView{}, sshSetupAccessError(err)
	}
	view, err := readSSHSetupView(tx.QueryRow(ctx, querySSHSetupReadOwned, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "connection_ref": input.ConnectionRef, "setup_ref": input.SetupRef, "begin_key": input.BeginIdempotencyKey}))
	if err != nil {
		return entity.SSHIdentitySetupView{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
	}
	return view, nil
}
