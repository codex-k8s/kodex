package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	//go:embed sql/provider_renewal__claim_accounts.sql
	queryProviderRenewalClaimAccounts string
	//go:embed sql/provider_renewal__input_eligible.sql
	queryProviderRenewalInputEligible string
	//go:embed sql/provider_renewal__create_task.sql
	queryProviderRenewalCreateTask string
	//go:embed sql/provider_renewal__expire_tasks.sql
	queryProviderRenewalExpireTasks string
	//go:embed sql/provider_renewal__lock_task.sql
	queryProviderRenewalLockTask string
	//go:embed sql/provider_renewal__authorize_task.sql
	queryProviderRenewalAuthorizeTask string
	//go:embed sql/provider_renewal__runtime_candidate.sql
	queryProviderRenewalRuntimeCandidate string
)

func (repository *Repository) ClaimProviderCredentialRenewalTasks(ctx context.Context, claimant string, encoder port.ProviderCredentialRenewalEncoder) (result []port.ProviderCredentialRenewalTask, resultErr error) {
	if claimant == "" || len(claimant) > 128 || strings.ContainsAny(claimant, "\r\n\x00") || encoder == nil {
		return nil, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	actor, err := repository.resolveProofIdentity(ctx, port.ProofPrincipalInput{CallerWorkload: "control-plane", Operation: port.ProviderCredentialRenewalOperation,
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation"})
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	if _, err := tx.Exec(ctx, queryProviderRenewalExpireTasks, actor.OrganizationID); err != nil {
		return nil, errs.ErrUnavailable
	}
	rows, err := tx.Query(ctx, queryProviderRenewalClaimAccounts, actor.OrganizationID)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	type candidate struct {
		accountID, credentialID string
		task                    port.ProviderCredentialRenewalTask
	}
	var candidates []candidate
	for rows.Next() {
		var item candidate
		item.task.OrganizationID, item.task.ClaimantID, item.task.State = actor.OrganizationID, claimant, "CLAIMED"
		if rows.Scan(&item.accountID, &item.task.AccountRef, &item.task.AccountVersion, &item.credentialID, &item.task.CredentialRef, &item.task.CredentialRevision,
			&item.task.Previous.SecretName, &item.task.Previous.SecretUID, &item.task.Previous.SecretResourceVersion, &item.task.Previous.ContentSHA256,
			&item.task.ExpiresAt, &item.task.Generation) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		candidates = append(candidates, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	for _, item := range candidates {
		// Свежий statement ПОСЛЕ account lock; initial snapshot не закрывает TOCTOU.
		var eligible bool
		if err := tx.QueryRow(ctx, queryProviderRenewalInputEligible, actor.OrganizationID, item.accountID, item.task.AccountVersion, item.credentialID).Scan(&eligible); err != nil {
			return nil, errs.ErrUnavailable
		}
		if !eligible {
			continue
		}
		item.task.Ref, err = newRef("pcrt")
		if err != nil {
			return nil, err
		}
		item.task.Fence, err = newRef("prf")
		if err != nil {
			return nil, err
		}
		item.task.RequestDigest, item.task.OwnerRequestDigest, err = encoder.CredentialRenewalRequestDigests(item.task)
		if err != nil || !validModelCatalogDigest(item.task.RequestDigest) || !validModelCatalogDigest(item.task.OwnerRequestDigest) {
			return nil, errs.ErrInvalid
		}
		if _, err := tx.Exec(ctx, queryProviderRenewalCreateTask, pgx.StrictNamedArgs{
			"ref": item.task.Ref, "organization_id": actor.OrganizationID, "account_id": item.accountID, "account_version": item.task.AccountVersion,
			"credential_id": item.credentialID, "attempt": item.task.Generation, "claimant": claimant, "generation": item.task.Generation,
			"fence": item.task.Fence, "request_digest": item.task.RequestDigest, "owner_request_digest": item.task.OwnerRequestDigest, "expires_at": item.task.ExpiresAt,
		}); err != nil {
			return nil, errs.ErrConflict
		}
		result = append(result, item.task)
	}
	if tx.Commit(ctx) != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

type lockedProviderRenewal struct {
	id, accountID, credentialID      string
	task                             port.ProviderCredentialRenewalTask
	eligible                         bool
	now                              time.Time
	resultDigest, outputCredentialID string
}

func lockProviderRenewal(ctx context.Context, tx pgx.Tx, organizationID, ref string) (lockedProviderRenewal, error) {
	var locked lockedProviderRenewal
	err := tx.QueryRow(ctx, queryProviderRenewalLockTask, organizationID, ref).Scan(&locked.id, &locked.task.State, &locked.task.OrganizationID, &locked.accountID,
		&locked.task.Ref, &locked.task.AccountRef, &locked.task.AccountVersion, &locked.credentialID, &locked.task.CredentialRef, &locked.task.CredentialRevision,
		&locked.task.Previous.SecretName, &locked.task.Previous.SecretUID, &locked.task.Previous.SecretResourceVersion, &locked.task.Previous.ContentSHA256,
		&locked.task.ClaimantID, &locked.task.Generation, &locked.task.Fence, &locked.task.RequestDigest, &locked.task.OwnerRequestDigest, &locked.task.ExpiresAt,
		&locked.eligible, &locked.now, &locked.resultDigest, &locked.outputCredentialID)
	if errors.Is(err, pgx.ErrNoRows) {
		return locked, errs.ErrForbidden
	}
	if err != nil {
		return locked, errs.ErrUnavailable
	}
	return locked, nil
}

func sameProviderRenewalBinding(a, b port.ProviderCredentialRenewalTask) bool {
	return a.Ref == b.Ref && a.AccountRef == b.AccountRef && a.AccountVersion == b.AccountVersion && a.CredentialRef == b.CredentialRef &&
		a.CredentialRevision == b.CredentialRevision && a.Previous == b.Previous && a.ClaimantID == b.ClaimantID && a.Generation == b.Generation &&
		a.Fence == b.Fence && a.RequestDigest == b.RequestDigest && a.ExpiresAt.Equal(b.ExpiresAt)
}

func validProviderRenewalBinding(task port.ProviderCredentialRenewalTask) bool {
	if !strings.HasPrefix(task.Ref, "pcrt_") || !validModelCatalogDigest(task.RequestDigest) || task.ExpiresAt.IsZero() ||
		task.AccountVersion < 1 || task.CredentialRevision < 1 || task.Generation < 1 || task.AccountVersion > 9007199254740991 || task.CredentialRevision > 9007199254740991 || task.Generation > 9007199254740991 {
		return false
	}
	for _, ref := range []string{task.Ref, task.AccountRef, task.CredentialRef, task.Fence, task.ClaimantID} {
		if len(ref) < 1 || len(ref) > 128 || strings.ContainsAny(ref, "\r\n\x00") {
			return false
		}
	}
	uid, err := uuid.Parse(task.Previous.SecretUID)
	return err == nil && uid.String() == task.Previous.SecretUID && validModelCatalogDigest(task.Previous.ContentSHA256) && validDNSLabel(task.Previous.SecretName) &&
		len(task.Previous.SecretResourceVersion) > 0 && len(task.Previous.SecretResourceVersion) <= 128
}

func (repository *Repository) AuthorizeProviderCredentialRenewal(ctx context.Context, principal value.Principal, task port.ProviderCredentialRenewalTask) (resultErr error) {
	return repository.checkProviderRenewalOwner(ctx, principal, task, true)
}

func (repository *Repository) ValidateProviderCredentialRenewalObservation(ctx context.Context, principal value.Principal, task port.ProviderCredentialRenewalTask) (resultErr error) {
	return repository.checkProviderRenewalOwner(ctx, principal, task, false)
}

func (repository *Repository) checkProviderRenewalOwner(ctx context.Context, principal value.Principal, task port.ProviderCredentialRenewalTask, authorize bool) (resultErr error) {
	operation := port.ProviderCredentialRenewalValidateOperation
	if authorize {
		operation = port.ProviderCredentialRenewalAuthorizeOperation
	}
	if principal.CallerWorkload != "secret-broker" || principal.Permission != operation || principal.ProjectRef != "" || !validProviderRenewalBinding(task) {
		return errs.ErrForbidden
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	locked, err := lockProviderRenewal(ctx, tx, current.organizationID, task.Ref)
	if err != nil {
		return err
	}
	if !sameProviderRenewalBinding(task, locked.task) || !locked.eligible {
		return errs.ErrForbidden
	}
	if authorize {
		if !locked.task.ExpiresAt.After(locked.now) || locked.task.State != "CLAIMED" && locked.task.State != "AUTHORIZED" {
			return errs.ErrForbidden
		}
		var eligible bool
		if err := tx.QueryRow(ctx, queryProviderRenewalInputEligible, current.organizationID, locked.accountID, task.AccountVersion, locked.credentialID).Scan(&eligible); err != nil {
			return errs.ErrUnavailable
		}
		if !eligible {
			return errs.ErrForbidden
		}
		if locked.task.State == "CLAIMED" {
			tag, err := tx.Exec(ctx, queryProviderRenewalAuthorizeTask, locked.id)
			if err != nil || tag.RowsAffected() != 1 {
				return errs.ErrConflict
			}
		}
	} else if locked.task.State != "AUTHORIZED" && locked.task.State != "UNKNOWN_OUTCOME" {
		return errs.ErrForbidden
	}
	if tx.Commit(ctx) != nil {
		return errs.ErrUnavailable
	}
	return nil
}
