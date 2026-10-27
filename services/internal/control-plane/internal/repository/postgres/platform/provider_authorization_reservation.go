package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryProviderAuthorizationReservationRead embeds replay lookup by
// organization/account/actor/method/request key with original/reserved versions and deadline
// freshness. It returns metadata only; access and semantic digest equality are checked by the
// caller.
// queryProviderAuthorizationReservationRead встраивает replay lookup по
// organization/account/actor/method/request key с original/reserved versions и deadline freshness.
// Возвращает только metadata; access и semantic digest equality проверяет caller.
//
//go:embed sql/provider_authorization_reservation_read.sql
var queryProviderAuthorizationReservationRead string

// queryProviderAuthorizationReservationInsert embeds a RESERVED authorization attempt with a
// fifteen-minute deadline, pinned versions and request digest. Initial FAILED authorization state
// does not mean preparation failed; external materialization has not happened here.
// queryProviderAuthorizationReservationInsert встраивает RESERVED authorization attempt с
// пятнадцатиминутным deadline, pinned versions и request digest. Начальное FAILED authorization
// state не означает отказ preparation; внешняя materialization здесь ещё не выполнена.
//
//go:embed sql/provider_authorization_reservation_insert.sql
var queryProviderAuthorizationReservationInsert string

// queryProviderAuthorizationReservationAbandon embeds supersession of reserved or pending attempts
// for the locked account, clearing interactive codes and incrementing versions. Tenant ownership
// and account locking must already be established by the caller.
// queryProviderAuthorizationReservationAbandon встраивает supersession reserved либо pending
// attempts заблокированного account, очищая interactive codes и увеличивая versions. Tenant
// ownership и account lock должны быть установлены caller заранее.
//
//go:embed sql/provider_authorization_reservation_abandon.sql
var queryProviderAuthorizationReservationAbandon string

// queryProviderAuthorizationAbandonedCleanup embeds deduplicated metadata cleanup tasks for
// abandoned attempts using deterministic materializer refs. It does not itself revoke external
// credentials or confirm cleanup completion.
// queryProviderAuthorizationAbandonedCleanup встраивает deduplicated metadata cleanup tasks для
// abandoned attempts с детерминированными materializer refs. Сам не отзывает внешние credentials и
// не подтверждает завершение cleanup.
//
//go:embed sql/provider_authorization_abandoned_cleanup.sql
var queryProviderAuthorizationAbandonedCleanup string

// queryProviderAuthorizationExpiredAccounts embeds a bounded account-ordered SKIP LOCKED selection
// with overdue reserved attempts. Selection holds account locks for the caller transaction; expiry
// is applied by a separate statement.
// queryProviderAuthorizationExpiredAccounts встраивает ограниченную account-ordered SKIP LOCKED
// выборку с просроченными reserved attempts. Выборка держит account locks caller transaction;
// expiry применяется отдельным statement.
//
//go:embed sql/provider_authorization_expired_accounts.sql
var queryProviderAuthorizationExpiredAccounts string

// queryProviderAuthorizationExpire embeds abandonment of overdue reservations and
// disables/transitions the account only when its current pending version matches an expired
// reserved version. It preserves later account changes and leaves cleanup scheduling to the
// caller.
// queryProviderAuthorizationExpire встраивает abandonment просроченных reservations и
// отключает/переводит account только при совпадении current pending version с expired reserved
// version. Сохраняет более поздние account changes; cleanup назначает caller.
//
//go:embed sql/provider_authorization_expire.sql
var queryProviderAuthorizationExpire string

// expireProviderAuthorizationReservations collects and closes a bounded locked account batch
// before expiring reservations and scheduling abandoned metadata cleanup for each. It stops at the
// first error without commit; all earlier writes remain the caller transaction responsibility.
// expireProviderAuthorizationReservations собирает и закрывает ограниченный locked account batch
// до expiry reservations и назначения abandoned metadata cleanup каждому account. Останавливается
// на первой ошибке без commit; прежние writes остаются ответственностью caller transaction.
func (repository *Repository) expireProviderAuthorizationReservations(ctx context.Context, tx pgx.Tx, limit int32) error {
	rows, err := tx.Query(ctx, queryProviderAuthorizationExpiredAccounts, pgx.StrictNamedArgs{"limit": limit})
	if err != nil {
		return errs.ErrUnavailable
	}
	var accounts []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		accounts = append(accounts, id)
	}
	rows.Close()
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	for _, id := range accounts {
		if _, err := tx.Exec(ctx, queryProviderAuthorizationExpire, pgx.StrictNamedArgs{"account_id": id}); err != nil {
			return errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryProviderAuthorizationAbandonedCleanup, pgx.StrictNamedArgs{"account_id": id, "maximum_attempts": providerCredentialCleanupMaxAttempts}); err != nil {
			return errs.ErrUnavailable
		}
	}
	return nil
}

// ReserveProviderAuthorization checks shape, resolves scope and requires both account
// view/authorize before locking the account. Exact digest/original-version replay returns the old
// identity without commit; fresh unapplied replay also requires deadline/version/state pins. A new
// reservation checks OCC and active/warm guards, abandons older attempts, schedules cleanup,
// inserts metadata and disables the account atomically. Digest validation here checks length only;
// no external authorization or secret transfer occurs.
// ReserveProviderAuthorization проверяет shape, разрешает scope и требует account view/authorize
// до account lock. Exact digest/original-version replay возвращает прежнюю identity без commit;
// свежий unapplied replay также требует deadline/version/state pins. Новое reservation проверяет
// OCC и active/warm guards, закрывает старые attempts, назначает cleanup, добавляет metadata и
// атомарно отключает account. Digest здесь проверяется только по длине; внешней authorization и
// переноса секретов нет.
func (repository *Repository) ReserveProviderAuthorization(ctx context.Context, principal value.Principal, mutation value.Mutation, accountRef, method, digest string) (entity.ProviderAuthorizationReservation, error) {
	var result entity.ProviderAuthorizationReservation
	if mutation.ExpectedVersion == nil || *mutation.ExpectedVersion < 1 || len(mutation.IdempotencyKey) < 8 || len(mutation.IdempotencyKey) > 128 ||
		len(digest) != 64 || !strings.HasPrefix(accountRef, "pacc_") || !contains([]string{"API_KEY", "DEVICE_CODE"}, method) {
		return result, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	target := entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROVIDER_ACCOUNT", ResourceRef: accountRef}
	for _, permission := range []string{"provider.account.view", "provider.account.authorize"} {
		if err := repository.requireAccess(ctx, tx, current, permission, target); err != nil {
			return result, err
		}
	}
	var accountID, state string
	var version int64
	var enabled bool
	var credentialID *string
	if err := tx.QueryRow(ctx, queryProviderAccountsLock, pgx.StrictNamedArgs{"organization_id": current.organizationID, "account_ref": accountRef}).Scan(&accountID, &version, &state, &enabled, &credentialID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, errs.ErrNotFound
		}
		return result, errs.ErrUnavailable
	}
	var storedDigest, preparation string
	var originalVersion int64
	var fresh bool
	err = tx.QueryRow(ctx, queryProviderAuthorizationReservationRead, pgx.StrictNamedArgs{"organization_id": current.organizationID, "account_id": accountID,
		"actor_id": current.actorID, "method": method, "request_key": mutation.IdempotencyKey}).Scan(&result.AttemptRef, &storedDigest, &originalVersion, &result.ReservedVersion, &preparation, &fresh)
	if err == nil {
		if storedDigest != digest || originalVersion != *mutation.ExpectedVersion {
			return result, errs.ErrIdempotencyReuse
		}
		result.Applied = preparation == "APPLIED"
		if !result.Applied && (preparation != "RESERVED" || !fresh || version != result.ReservedVersion || state != "PENDING_AUTHORIZATION") {
			return result, errs.ErrConflict
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrUnavailable
	}
	if version != *mutation.ExpectedVersion {
		return result, errs.ErrVersionMismatch
	}
	if !contains([]string{"PENDING_AUTHORIZATION", "REAUTHORIZATION_REQUIRED", "AUTHORIZED", "DISABLED"}, state) {
		return result, errs.ErrConflict
	}
	var active, warm bool
	if err := tx.QueryRow(ctx, queryProviderAccountsCleanupGuard, pgx.StrictNamedArgs{"organization_id": current.organizationID, "account_id": accountID}).Scan(&active, &warm); err != nil {
		return result, errs.ErrUnavailable
	}
	if active || warm {
		return result, errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryProviderAuthorizationReservationAbandon, pgx.StrictNamedArgs{"account_id": accountID}); err != nil {
		return result, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryProviderAuthorizationAbandonedCleanup, pgx.StrictNamedArgs{"account_id": accountID, "maximum_attempts": providerCredentialCleanupMaxAttempts}); err != nil {
		return result, errs.ErrUnavailable
	}
	result.AttemptRef, err = newRef("pauth")
	if err != nil {
		return result, err
	}
	result.ReservedVersion = version + 1
	if _, err := tx.Exec(ctx, queryProviderAuthorizationReservationInsert, pgx.StrictNamedArgs{"attempt_ref": result.AttemptRef, "organization_id": current.organizationID,
		"account_id": accountID, "actor_id": current.actorID, "method": method, "request_key": mutation.IdempotencyKey, "request_digest": digest,
		"original_version": version, "reserved_version": result.ReservedVersion}); err != nil {
		return result, mapWriteError(err)
	}
	if _, err := tx.Exec(ctx, queryProviderAccountsUpdateLifecycle, pgx.StrictNamedArgs{"account_id": accountID, "state": "PENDING_AUTHORIZATION", "enabled": false, "clear_credential": false}); err != nil {
		return result, errs.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return result, errs.ErrUnavailable
	}
	return result, nil
}
