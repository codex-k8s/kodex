package platform

import (
	"context"
	_ "embed"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/model_catalog__claim_accounts.sql
var queryModelCatalogClaimAccounts string

//go:embed sql/model_catalog__expire_tasks.sql
var queryModelCatalogExpireTasks string

//go:embed sql/model_catalog__create_task.sql
var queryModelCatalogCreateTask string

//go:embed sql/model_catalog__claim_task.sql
var queryModelCatalogClaimTask string

func (repository *Repository) ClaimProviderModelCatalogTasks(ctx context.Context, claimant string, limit int32, encoder platformrepo.ProviderModelCatalogEncoder) ([]platformrepo.ProviderModelCatalogTask, error) {
	if claimant == "" || len(claimant) > 128 || limit < 1 || limit > 16 || encoder == nil {
		return nil, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, queryModelCatalogExpireTasks); err != nil {
		return nil, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryProviderVerificationExpire, pgx.StrictNamedArgs{"account_id": ""}); err != nil {
		return nil, errs.ErrUnavailable
	}
	rows, err := tx.Query(ctx, queryModelCatalogClaimAccounts, limit)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	type claimed struct {
		task                    platformrepo.ProviderModelCatalogTask
		accountID, credentialID string
	}
	var claims []claimed
	for rows.Next() {
		var item claimed
		if err := rows.Scan(&item.accountID, &item.task.AccountRef, &item.task.OrganizationID, &item.task.AccountVersion,
			&item.task.ProviderDefinitionKey, &item.credentialID, &item.task.CredentialRef, &item.task.CredentialRevision,
			&item.task.Credential.SecretName, &item.task.Credential.SecretUID, &item.task.Credential.SecretResourceVersion,
			&item.task.Credential.ContentSHA256, &item.task.AuthorizationMethod); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		claims = append(claims, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	result := make([]platformrepo.ProviderModelCatalogTask, 0, len(claims))
	for _, item := range claims {
		item.task.Ref, err = newRef("mcattsk")
		if err != nil {
			return nil, err
		}
		item.task.Fence, err = newRef("mcf")
		if err != nil {
			return nil, err
		}
		item.task.ClaimantID, item.task.ClaimGeneration = claimant, 1
		var taskID string
		if err := tx.QueryRow(ctx, queryModelCatalogCreateTask, item.task.Ref, item.task.OrganizationID, item.accountID, item.task.AccountVersion, item.credentialID, item.task.AuthorizationMethod).Scan(&taskID, &item.task.ExpiresAt); err != nil {
			return nil, errs.ErrUnavailable
		}
		item.task.RequestDigest, err = encoder.ModelCatalogRequestDigest(item.task)
		if _, linkErr := tx.Exec(ctx, queryProviderVerificationLinkTask, pgx.StrictNamedArgs{"task_id": taskID}); linkErr != nil {
			return nil, errs.ErrUnavailable
		}
		if err != nil || !validModelCatalogDigest(item.task.RequestDigest) {
			return nil, errs.ErrInvalid
		}
		if _, err := tx.Exec(ctx, queryModelCatalogClaimTask, taskID, claimant, item.task.ClaimGeneration, item.task.Fence, item.task.RequestDigest, item.task.ExpiresAt); err != nil {
			return nil, errs.ErrUnavailable
		}
		result = append(result, item.task)
	}
	if tx.Commit(ctx) != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}
