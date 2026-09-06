package platform

import (
	"context"
	_ "embed"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/provider_account_deletion_start.sql
var queryProviderAccountDeletionStart string

//go:embed sql/provider_account_deletion_schedule_authorizations.sql
var queryProviderAccountDeletionScheduleAuthorizations string

//go:embed sql/provider_account_deletion_lock_pending.sql
var queryProviderAccountDeletionLockPending string

//go:embed sql/provider_account_deletion_advance.sql
var queryProviderAccountDeletionAdvance string

//go:embed sql/provider_account_deletion_retry.sql
var queryProviderAccountDeletionRetry string

func (repository *Repository) startProviderAccountDeletion(ctx context.Context, tx pgx.Tx, current scope, accountID, state string) error {
	if state == "DELETING" {
		tag, err := tx.Exec(ctx, queryProviderAccountDeletionRetry, pgx.StrictNamedArgs{"organization_id": current.organizationID, "account_id": accountID})
		if err != nil {
			return errs.ErrUnavailable
		}
		if tag.RowsAffected() == 0 {
			return errs.ErrConflict
		}
		if _, err = tx.Exec(ctx, queryProviderAccountsUpdateLifecycle, pgx.StrictNamedArgs{"account_id": accountID, "state": "DELETING", "enabled": false, "clear_credential": false}); err != nil {
			return errs.ErrUnavailable
		}
		return repository.scheduleProviderAccountDeletion(ctx, tx, current.organizationID, accountID)
	}
	if state == "DELETED" {
		return errs.ErrConflict
	}
	intentRef, err := newRef("pdel")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, queryProviderAccountDeletionStart, pgx.StrictNamedArgs{
		"intent_ref": intentRef, "organization_id": current.organizationID,
		"account_id": accountID, "actor_id": current.actorID,
	}); err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryProviderAccountsUpdateLifecycle, pgx.StrictNamedArgs{
		"account_id": accountID, "state": "DELETING", "enabled": false, "clear_credential": false,
	}); err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryProviderAccountsFailPendingAuthorizations, pgx.StrictNamedArgs{
		"account_id": accountID, "safe_failure_code": "REVOKED",
	}); err != nil {
		return errs.ErrUnavailable
	}
	return repository.scheduleProviderAccountDeletion(ctx, tx, current.organizationID, accountID)
}

func (repository *Repository) scheduleProviderAccountDeletion(ctx context.Context, tx pgx.Tx, organizationID, accountID string) error {
	if _, err := tx.Exec(ctx, queryProviderCredentialCleanupScheduleAccount, pgx.StrictNamedArgs{
		"organization_id": organizationID, "account_id": accountID,
		"eligible_at": time.Now().UTC(), "maximum_attempts": providerCredentialCleanupMaxAttempts,
	}); err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryProviderAccountDeletionScheduleAuthorizations, pgx.StrictNamedArgs{
		"organization_id": organizationID, "account_id": accountID, "maximum_attempts": providerCredentialCleanupMaxAttempts,
	}); err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryProviderAccountDeletionAdvance, pgx.StrictNamedArgs{
		"organization_id": organizationID, "account_id": accountID,
	}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

func (repository *Repository) advanceProviderAccountDeletions(ctx context.Context, tx pgx.Tx, limit int32) error {
	rows, err := tx.Query(ctx, queryProviderAccountDeletionLockPending, pgx.StrictNamedArgs{"limit": limit})
	if err != nil {
		return errs.ErrUnavailable
	}
	type pending struct{ organizationID, accountID string }
	items := make([]pending, 0, limit)
	for rows.Next() {
		var item pending
		if rows.Scan(&item.organizationID, &item.accountID) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		items = append(items, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	for _, item := range items {
		if err := repository.scheduleProviderAccountDeletion(ctx, tx, item.organizationID, item.accountID); err != nil {
			return err
		}
	}
	return nil
}
