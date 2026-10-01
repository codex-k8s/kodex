package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/provider_verification_start.sql
var queryProviderVerificationStart string

//go:embed sql/provider_verification_cancel_previous_catalog.sql
var queryProviderVerificationCancelPreviousCatalog string

//go:embed sql/provider_verification_expire.sql
var queryProviderVerificationExpire string

//go:embed sql/provider_verification_link_task.sql
var queryProviderVerificationLinkTask string

//go:embed sql/provider_verification_complete.sql
var queryProviderVerificationComplete string

//go:embed sql/provider_verifications_read.sql
var queryProviderVerificationsRead string

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

func (repository *Repository) expireProviderVerifications(ctx context.Context, tx pgx.Tx, accountID string, emitRealtime bool) error {
	rows, err := tx.Query(ctx, queryProviderVerificationExpire, pgx.StrictNamedArgs{"account_id": accountID})
	if err != nil {
		return errs.ErrUnavailable
	}
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
