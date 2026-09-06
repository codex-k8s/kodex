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
	if _, err := tx.Exec(ctx, queryProviderVerificationExpire, pgx.StrictNamedArgs{"account_id": accountID}); err != nil {
		return errs.ErrUnavailable
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
