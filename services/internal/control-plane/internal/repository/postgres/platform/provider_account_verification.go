package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryProviderVerificationStart embeds verification creation only for an enabled authorized
// account with current credential, latest authorized DEVICE_CODE attempt and no pending
// verification. It pins account version and credential revision; reachability is not established
// by insertion.
// queryProviderVerificationStart встраивает создание verification только для enabled authorized
// account с current credential, latest authorized DEVICE_CODE attempt и без pending verification.
// Фиксирует account version и credential revision; вставка не доказывает reachability.
//
//go:embed sql/provider_verification_start.sql
var queryProviderVerificationStart string

// queryProviderVerificationCancelPreviousCatalog embeds cancellation of pending/claimed catalog
// tasks for one organization/account and clears their claimant/fence/digest/expiry. It is part of
// the caller transaction, not an external worker cancellation acknowledgement.
// queryProviderVerificationCancelPreviousCatalog встраивает отмену pending/claimed catalog tasks
// одного organization/account и очищает claimant/fence/digest/expiry. Это часть caller
// transaction, а не подтверждение остановки внешнего worker.
//
//go:embed sql/provider_verification_cancel_previous_catalog.sql
var queryProviderVerificationCancelPreviousCatalog string

// queryProviderVerificationExpire embeds a bounded SKIP LOCKED closure of pending verifications
// whose account/credential changed, deadline elapsed or catalog task was cancelled. Changed source
// maps to STALE, other eligible closures to FAILED; returned rows drive optional snapshots.
// queryProviderVerificationExpire встраивает ограниченное SKIP LOCKED закрытие pending
// verifications при изменении account/credential, истечении deadline либо отмене catalog task.
// Changed source даёт STALE, прочие допустимые закрытия — FAILED; returned rows используются для
// optional snapshots.
//
//go:embed sql/provider_verification_expire.sql
var queryProviderVerificationExpire string

// queryProviderVerificationLinkTask embeds attachment of an exact account/version/credential
// catalog task to an unlinked, unexpired pending verification. It does not create a task or mark
// verification successful.
// queryProviderVerificationLinkTask встраивает привязку exact account/version/credential catalog
// task к непривязанной unexpired pending verification. Не создаёт task и не отмечает verification
// успешной.
//
//go:embed sql/provider_verification_link_task.sql
var queryProviderVerificationLinkTask string

// queryProviderVerificationComplete embeds terminal verification from the linked observation with
// exact account-version/credential pins before deadline. Only a fresh NONE-failure
// REMOTE_API/REMOTE_CODEX observation is VERIFIED; other linked observations fail.
// queryProviderVerificationComplete встраивает terminal verification по linked observation с exact
// account-version/credential pins до deadline. Только свежая NONE-failure REMOTE_API/REMOTE_CODEX
// observation даёт VERIFIED; прочие linked observations дают отказ.
//
//go:embed sql/provider_verification_complete.sql
var queryProviderVerificationComplete string

// queryProviderVerificationsRead embeds latest verification per requested tenant account,
// projecting changed sources as STALE and overdue pending attempts as FAILED without writing.
// Historical credential revision and effective completion time remain in the result.
// queryProviderVerificationsRead встраивает latest verification каждого requested tenant account,
// проецируя changed sources в STALE и overdue pending attempts в FAILED без записи. Historical
// credential revision и effective completion time остаются в результате.
//
//go:embed sql/provider_verifications_read.sql
var queryProviderVerificationsRead string

// hydrateProviderVerifications scans latest verification projections into existing account
// pointers with fixed CREDENTIALED_CATALOG_REACHABILITY scope. Unknown account refs, scan or
// iterator errors are Unavailable; prior pointer updates may remain on failure and no permissions
// are added.
// hydrateProviderVerifications читает latest verification projections в существующие account
// pointers с фиксированным CREDENTIALED_CATALOG_REACHABILITY scope. Unknown account refs, scan
// либо iterator errors дают Unavailable; прежние pointer updates могут сохраниться при ошибке,
// permissions не добавляются.
func (repository *Repository) hydrateProviderVerifications(ctx context.Context, tx pgx.Tx, current scope, refs []string, items map[string]*entity.ProviderAccount) error {
	rows, err := tx.Query(ctx, queryProviderVerificationsRead, pgx.StrictNamedArgs{"organization_id": current.organizationID, "account_refs": refs})
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		verification := &entity.ProviderAccountVerification{Scope: "CREDENTIALED_CATALOG_REACHABILITY"}
		if rows.Scan(&ref, &verification.Ref, &verification.State, &verification.SafeReason, &verification.AccountVersion,
			&verification.CredentialRevision, &verification.RequestedAt, &verification.CompletedAt) != nil || items[ref] == nil {
			return errs.ErrUnavailable
		}
		items[ref].Verification = verification
	}
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// startProviderVerification expires eligible old verifications first, allocates a ref and inserts
// the current pinned attempt before cancelling prior catalog work. An ineligible insert is
// Conflict. It neither runs the provider probe nor commits the caller transaction.
// startProviderVerification сначала закрывает допустимые старые verifications, выделяет ref и
// добавляет current pinned attempt до отмены прежней catalog work. Недопустимая вставка даёт
// Conflict. Не запускает provider probe и не фиксирует caller transaction.
func (repository *Repository) startProviderVerification(ctx context.Context, tx pgx.Tx, current scope, accountID string) error {
	if err := repository.expireProviderVerifications(ctx, tx, accountID, false); err != nil {
		return err
	}
	ref, err := newRef("pverify")
	if err != nil {
		return err
	}
	var id string
	err = tx.QueryRow(ctx, queryProviderVerificationStart, pgx.StrictNamedArgs{
		"verification_ref": ref, "organization_id": current.organizationID, "account_id": accountID, "actor_id": current.actorID,
	}).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryProviderVerificationCancelPreviousCatalog, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "account_id": accountID,
	}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// expireProviderVerifications collects changed verification rows and validates iteration before
// optionally emitting one account snapshot per row with verification correlation. An empty account
// ID permits the SQL-wide bounded sweep; updates and events remain atomic only through the caller
// transaction.
// expireProviderVerifications собирает changed verification rows и проверяет iteration до optional
// account snapshot для каждой строки с verification correlation. Пустой account ID разрешает
// ограниченный SQL-wide sweep; updates/events атомарны только через caller transaction.
func (repository *Repository) expireProviderVerifications(ctx context.Context, tx pgx.Tx, accountID string, emitRealtime bool) error {
	rows, err := tx.Query(ctx, queryProviderVerificationExpire, pgx.StrictNamedArgs{"account_id": accountID})
	if err != nil {
		return errs.ErrUnavailable
	}
	// expiredVerification holds returned organization/account/verification refs and
	// account-version/state pins for snapshots after rows are closed. It reflects a database
	// transition, without granting access or completing external catalog work.
	// expiredVerification хранит returned organization/account/verification refs и
	// account-version/state pins для snapshots после закрытия rows. Отражает DB transition, не выдаёт
	// access и не завершает внешнюю catalog work.
	type expiredVerification struct {
		organizationID, organizationRef, accountRef, verificationRef, state string
		accountVersion                                                      int64
	}
	items := make([]expiredVerification, 0, 8)
	for rows.Next() {
		var item expiredVerification
		if err := rows.Scan(&item.organizationID, &item.organizationRef, &item.accountRef,
			&item.verificationRef, &item.accountVersion, &item.state); err != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		items = append(items, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	if !emitRealtime {
		return nil
	}
	for _, item := range items {
		if err := repository.emitPlatformEventSnapshot(ctx, tx, scope{
			organizationID: item.organizationID, organizationRef: item.organizationRef, correlationRef: item.verificationRef,
		}, "PROVIDER_ACCOUNT_CHANGED", "", item.accountRef, "i18n:PROVIDER_ACCOUNT_UPDATED", item.accountVersion, item.state); err != nil {
			return err
		}
	}
	return nil
}
