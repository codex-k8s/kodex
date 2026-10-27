package platform

import (
	"context"
	_ "embed"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/jackc/pgx/v5"
)

// queryModelCatalogClaimAccounts embeds bounded SKIP LOCKED enabled authorized accounts with exact
// current credentials and authorization method. Pending verification or absence of a recent
// matching completion makes an account due; existing pending/claimed tasks exclude it. It returns
// metadata, not credential values.
// queryModelCatalogClaimAccounts встраивает bounded SKIP LOCKED enabled authorized accounts с
// точными current credentials и authorization method. Pending verification либо отсутствие свежего
// matching completion делает account кандидатом; прежняя pending/claimed task его исключает.
// Возвращает metadata, а не значения credentials.
//
//go:embed sql/model_catalog__claim_accounts.sql
var queryModelCatalogClaimAccounts string

// queryModelCatalogExpireTasks embeds bounded128 SKIP LOCKED cancellation of expired or
// account/credential-invalid pending/claimed tasks, clearing claim authority fields. It is
// maintenance inside the claim transaction and does not itself probe or create a replacement
// attempt.
// queryModelCatalogExpireTasks встраивает bounded128 SKIP LOCKED отмену expired либо
// account/credential-invalid pending/claimed tasks с очисткой claim authority fields. Это
// обслуживание внутри claim transaction без probe и самостоятельного создания новой попытки.
//
//go:embed sql/model_catalog__expire_tasks.sql
var queryModelCatalogExpireTasks string

// queryModelCatalogCreateTask embeds owner-pinned PENDING catalog task creation and returns
// internal identity with a database-derived15-second expiry. Claimant/fence/request digest are
// assigned separately in the same owner transaction.
// queryModelCatalogCreateTask встраивает создание owner-pinned PENDING catalog task и возвращает
// внутреннюю идентичность с15-second expiry от БД. Claimant/fence/request digest назначаются
// отдельно в той же owner transaction.
//
//go:embed sql/model_catalog__create_task.sql
var queryModelCatalogCreateTask string

// queryModelCatalogClaimTask embeds transition of an exact PENDING task to CLAIMED with claimant,
// generation, fence, request digest and expiry. The caller owns prior account locks and
// transaction; this statement does not validate arbitrary client authority or require its own row
// count.
// queryModelCatalogClaimTask встраивает переход точной PENDING task в CLAIMED с claimant,
// generation, fence, request digest и expiry. Caller владеет прежними account locks и транзакцией;
// statement не проверяет произвольные полномочия клиента и сам не требует число строк.
//
//go:embed sql/model_catalog__claim_task.sql
var queryModelCatalogClaimTask string

// ClaimProviderModelCatalogTasks requires a nonempty bounded claimant, batch1..16 and encoder,
// then expires stale tasks/verifications in a five-second transaction. It locks due accounts,
// closes rows before writes and creates fresh task/fence with generation1 and encoded request
// digest, linking verification before commit. Any error returns no claims and rolls back the
// batch; no remote call or secret-value read occurs here.
// ClaimProviderModelCatalogTasks требует непустой ограниченный claimant, batch1..16 и encoder,
// затем за пятисекундную транзакцию завершает stale tasks/verifications. Блокирует due accounts,
// закрывает rows до записей и создаёт fresh task/fence с generation1 и encoded request digest,
// связывая verification до Commit. Любая ошибка возвращает отсутствие claims и откатывает batch;
// remote call и чтения secret values здесь нет.
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
	if err := repository.expireProviderVerifications(ctx, tx, "", true); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, queryModelCatalogClaimAccounts, limit)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	// claimed holds a scanned owner task and internal account/credential IDs while the batch cursor is
	// closed before task creation. It contains only credential metadata and carries no independent
	// execution permission.
	// claimed хранит считанную owner task и внутренние account/credential IDs до закрытия batch cursor
	// и последующего создания task. Содержит только credential metadata и не несёт самостоятельного
	// права исполнения.
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
