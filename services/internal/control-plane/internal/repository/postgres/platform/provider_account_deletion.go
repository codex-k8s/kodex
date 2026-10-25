package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryProviderAccountDeletionStart embeds creation of a PENDING_BLOCKERS deletion intent for the
// supplied organization/account/requesting actor. Account locking, access, OCC and transaction
// commit belong to the command path.
// queryProviderAccountDeletionStart встраивает создание PENDING_BLOCKERS deletion intent для
// переданных organization/account/requesting actor. Account lock, access, OCC и commit принадлежат
// command path.
//
//go:embed sql/provider_account_deletion_start.sql
var queryProviderAccountDeletionStart string

// queryProviderAccountDeletionScheduleAuthorizations embeds cleanup metadata tasks for account
// authorization attempts that have no cleanup task. The materializer ref is deterministic from
// attempt/account; scheduling does not prove external absence.
// queryProviderAccountDeletionScheduleAuthorizations встраивает cleanup metadata tasks для account
// authorization attempts без cleanup task. Materializer ref детерминирован по attempt/account;
// scheduling не доказывает внешнее отсутствие.
//
//go:embed sql/provider_account_deletion_schedule_authorizations.sql
var queryProviderAccountDeletionScheduleAuthorizations string

// queryProviderAccountDeletionLockPending embeds a bounded ordered SKIP LOCKED selection of
// deleting accounts with unfinished intents. Locks belong to the caller transaction and do not
// themselves advance deletion.
// queryProviderAccountDeletionLockPending встраивает ограниченную упорядоченную SKIP LOCKED
// выборку deleting accounts с незавершёнными intents. Locks принадлежат caller transaction и сами
// не продвигают deletion.
//
//go:embed sql/provider_account_deletion_lock_pending.sql
var queryProviderAccountDeletionLockPending string

// queryProviderAccountDeletionAdvance embeds dependency/cleanup-driven deletion advancement with
// blockers taking precedence over failed, claimed and pending cleanup. It clears the current
// credential only after blockers vanish and marks DELETED only after all cleanup completes;
// changed rows carry realtime snapshot pins.
// queryProviderAccountDeletionAdvance встраивает продвижение deletion по dependencies/cleanup с
// приоритетом blockers над failed, claimed и pending cleanup. Очищает current credential только
// после исчезновения blockers и ставит DELETED лишь после всего cleanup; изменённые строки несут
// realtime snapshot pins.
//
//go:embed sql/provider_account_deletion_advance.sql
var queryProviderAccountDeletionAdvance string

// queryProviderAccountDeletionRetry embeds retry by creating successors only for supported
// DEAD_LETTER tasks with recovery identity and no successor under a FAILED intent. Superseded
// predecessors become COMPLETED with a prefixed receipt; this is not proof of credential deletion.
// queryProviderAccountDeletionRetry встраивает retry через successors только для поддержанных
// DEAD_LETTER tasks с recovery identity и без successor под FAILED intent. Superseded predecessors
// становятся COMPLETED с prefixed receipt; это не доказательство удаления credential.
//
//go:embed sql/provider_account_deletion_retry.sql
var queryProviderAccountDeletionRetry string

// startProviderAccountDeletion retries eligible failed cleanup for DELETING accounts, rejects
// DELETED, or creates a fresh intent and disables the account before failing pending
// authorizations and scheduling cleanup. Existing access/OCC checks and atomic commit remain with
// the caller; no external credential deletion occurs here.
// startProviderAccountDeletion повторяет допустимый failed cleanup для DELETING accounts,
// отклоняет DELETED либо создаёт intent и отключает account до отказа pending authorizations и
// scheduling cleanup. Access/OCC и атомарный commit остаются у caller; внешнего удаления
// credential здесь нет.
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
		return repository.scheduleProviderAccountDeletion(ctx, tx, current, accountID, false)
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
	return repository.scheduleProviderAccountDeletion(ctx, tx, current, accountID, false)
}

// scheduleProviderAccountDeletion schedules credential and authorization cleanup, advances the
// intent from current dependencies, and optionally emits a snapshot only when SQL returns a
// changed intent. No rows is a successful no-op; all writes/events use the supplied transaction
// without committing.
// scheduleProviderAccountDeletion назначает credential и authorization cleanup, продвигает intent
// по текущим dependencies и при необходимости выпускает snapshot только при изменённом SQL intent.
// No rows — успешный no-op; все writes/events используют переданную transaction без commit.
func (repository *Repository) scheduleProviderAccountDeletion(ctx context.Context, tx pgx.Tx, current scope, accountID string, emitRealtime bool) error {
	if _, err := tx.Exec(ctx, queryProviderCredentialCleanupScheduleAccount, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "account_id": accountID,
		"eligible_at": time.Now().UTC(), "maximum_attempts": providerCredentialCleanupMaxAttempts,
	}); err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryProviderAccountDeletionScheduleAuthorizations, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "account_id": accountID, "maximum_attempts": providerCredentialCleanupMaxAttempts,
	}); err != nil {
		return errs.ErrUnavailable
	}
	var organizationRef, accountRef, deletionRef, state string
	var accountVersion, deletionVersion int64
	err := tx.QueryRow(ctx, queryProviderAccountDeletionAdvance, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "account_id": accountID,
	}).Scan(&organizationRef, &accountRef, &accountVersion, &deletionRef, &deletionVersion, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if emitRealtime {
		current.organizationRef = organizationRef
		current.correlationRef = deletionRef
		return repository.emitPlatformEventSnapshot(ctx, tx, current, "PROVIDER_ACCOUNT_CHANGED", "",
			accountRef, "i18n:PROVIDER_ACCOUNT_UPDATED", accountVersion, state)
	}
	return nil
}

// advanceProviderAccountDeletions collects a bounded locked batch, closes and checks its rows
// before scheduling each account with realtime emission. It stops at the first failure; earlier
// writes remain in the caller transaction and require its rollback or commit.
// advanceProviderAccountDeletions собирает ограниченный locked batch, закрывает и проверяет rows
// до scheduling каждого account с realtime emission. Останавливается на первой ошибке; прежние
// writes остаются в caller transaction и требуют её rollback либо commit.
func (repository *Repository) advanceProviderAccountDeletions(ctx context.Context, tx pgx.Tx, limit int32) error {
	rows, err := tx.Query(ctx, queryProviderAccountDeletionLockPending, pgx.StrictNamedArgs{"limit": limit})
	if err != nil {
		return errs.ErrUnavailable
	}
	// pending holds one locked deletion candidate with organization identity and account UUID while
	// the result set is closed before further SQL. It is transaction-local routing data, not
	// independent authority.
	// pending хранит одного locked deletion candidate с organization identity и account UUID, пока
	// result set закрывается перед дальнейшим SQL. Это transaction-local routing data, а не
	// самостоятельные полномочия.
	type pending struct{ organizationID, organizationRef, accountID string }
	items := make([]pending, 0, limit)
	for rows.Next() {
		var item pending
		if rows.Scan(&item.organizationID, &item.organizationRef, &item.accountID) != nil {
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
		if err := repository.scheduleProviderAccountDeletion(ctx, tx, scope{
			organizationID: item.organizationID, organizationRef: item.organizationRef,
		}, item.accountID, true); err != nil {
			return err
		}
	}
	return nil
}
