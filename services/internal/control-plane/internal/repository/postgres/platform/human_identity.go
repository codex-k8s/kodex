package platform

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/jackc/pgx/v5"
)

// resolveHumanIdentityTx preserves the locked installation-owner claim, subject creation/profile and group synchronization inside its caller transaction.
// The caller owns admitted workload checks, eligibility completion, commit and rollback; this helper neither commits nor allocates session revisions.
// resolveHumanIdentityTx сохраняет locked installation-owner claim, создание/profile subject и group synchronization в транзакции caller.
// Caller владеет workload checks, eligibility completion, commit и rollback; helper не коммитит и не выделяет session revisions.
func (repository *Repository) resolveHumanIdentityTx(ctx context.Context, tx pgx.Tx, input platformrepo.ProofPrincipalInput, actorDigest, displayName string) (platformrepo.ProofAuthority, error) {
	authority := platformrepo.ProofAuthority{}
	var authorityTenant, claimState string
	if err := tx.QueryRow(ctx, queryLockInstallationOwnerClaim).Scan(
		&authority.OrganizationID, &authority.OrganizationVersion, &authorityTenant, &claimState,
	); err != nil {
		return platformrepo.ProofAuthority{}, humanIdentityDatabaseError(input, err)
	}
	if authorityTenant != "" && authorityTenant != input.ExternalTenantID {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	if claimState == "PENDING_CLAIM" && !input.OwnerClaim {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	err := tx.QueryRow(ctx, queryFindInstallationOwnerSubject, authority.OrganizationID, actorDigest).Scan(&authority.ActorID)
	if errors.Is(err, pgx.ErrNoRows) {
		actorRef, refErr := newRef("usr")
		if refErr != nil {
			return platformrepo.ProofAuthority{}, refErr
		}
		if err := tx.QueryRow(ctx, queryCreateInstallationOwnerSubject,
			actorRef, authority.OrganizationID, actorDigest, displayName, input.ExternalEmailHint).Scan(&authority.ActorID); err != nil {
			return platformrepo.ProofAuthority{}, humanIdentityDatabaseError(input, err)
		}
		if claimState == "PENDING_CLAIM" {
			if _, err := tx.Exec(ctx, queryClaimInstallationOwnership,
				authority.OrganizationID, authority.ActorID, input.ExternalTenantID); err != nil {
				return platformrepo.ProofAuthority{}, humanIdentityDatabaseError(input, err)
			}
			bindingRef, refErr := newRef("abnd")
			if refErr != nil {
				return platformrepo.ProofAuthority{}, refErr
			}
			if _, err := tx.Exec(ctx, queryAccessInsertOwnerBinding, pgx.NamedArgs{
				"ref": bindingRef, "organization_id": authority.OrganizationID, "subject_id": authority.ActorID,
			}); err != nil {
				return platformrepo.ProofAuthority{}, humanIdentityDatabaseError(input, err)
			}
			authority.OrganizationVersion++
		}
	} else if err != nil {
		return platformrepo.ProofAuthority{}, humanIdentityDatabaseError(input, err)
	}
	if _, err := tx.Exec(ctx, queryUpdateOIDCSubjectProfile, authority.OrganizationID, authority.ActorID, displayName, input.ExternalEmailHint); err != nil {
		return platformrepo.ProofAuthority{}, humanIdentityDatabaseError(input, err)
	}
	if err := repository.syncOIDCGroups(ctx, tx, authority.OrganizationID, authority.ActorID, input); err != nil {
		return platformrepo.ProofAuthority{}, err
	}
	return authority, nil
}

// humanIdentityDatabaseError retains the existing safe legacy failure and marks only native standard-mode transaction conflicts for bounded replay after rollback.
// Driver payload never appears in the returned diagnostic, and the marker alone does not prove transaction cleanup.
// humanIdentityDatabaseError сохраняет safe legacy failure и помечает лишь native standard-mode transaction conflicts для bounded replay после rollback.
// Driver payload не попадает в diagnostic; marker сам не доказывает завершение transaction cleanup.
func humanIdentityDatabaseError(input platformrepo.ProofPrincipalInput, err error) error {
	if input.PlatformManagedOIDC {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	return errs.ErrUnavailable
}
