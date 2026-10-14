package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	//go:embed sql/provider_renewal__complete_task.sql
	queryProviderRenewalCompleteTask string
	//go:embed sql/provider_renewal__unknown_task.sql
	queryProviderRenewalUnknownTask string
	//go:embed sql/provider_renewal__recovery_tasks.sql
	queryProviderRenewalRecoveryTasks string
	//go:embed sql/provider_renewal__readiness.sql
	queryProviderRenewalReadiness string
)

func (repository *Repository) CheckProviderCredentialRenewalReadiness(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var ready bool
	if repository.pool.QueryRow(ctx, queryProviderRenewalReadiness).Scan(&ready) != nil || !ready {
		return errs.ErrUnavailable
	}
	return nil
}

func (repository *Repository) ListProviderCredentialRenewalRecoveryTasks(ctx context.Context) ([]port.ProviderCredentialRenewalTask, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	actor, err := repository.resolveProofIdentity(ctx, port.ProofPrincipalInput{CallerWorkload: "control-plane", Operation: port.ProviderCredentialRenewalObserveOperation,
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation"})
	if err != nil {
		return nil, err
	}
	rows, err := repository.pool.Query(ctx, queryProviderRenewalRecoveryTasks, actor.OrganizationID)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []port.ProviderCredentialRenewalTask
	for rows.Next() {
		var task port.ProviderCredentialRenewalTask
		if rows.Scan(&task.Ref, &task.OrganizationID, &task.AccountRef, &task.AccountVersion, &task.CredentialRef, &task.CredentialRevision,
			&task.Previous.SecretName, &task.Previous.SecretUID, &task.Previous.SecretResourceVersion, &task.Previous.ContentSHA256,
			&task.ClaimantID, &task.Generation, &task.Fence, &task.RequestDigest, &task.OwnerRequestDigest, &task.ExpiresAt, &task.State) != nil {
			return nil, errs.ErrUnavailable
		}
		result = append(result, task)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

func validProviderRenewalResult(result port.ProviderCredentialRenewalResult) bool {
	if result.State == "UNKNOWN_OUTCOME" {
		return result.Credential == nil
	}
	if result.State != "SUCCEEDED" || result.Credential == nil {
		return false
	}
	uid, err := uuid.Parse(result.ReceiptUID)
	credentialUID, credentialErr := uuid.Parse(result.Credential.SecretUID)
	return err == nil && uid.String() == result.ReceiptUID && credentialErr == nil && credentialUID.String() == result.Credential.SecretUID &&
		len(result.ReceiptVersion) > 0 && len(result.ReceiptVersion) <= 128 && !strings.ContainsAny(result.ReceiptVersion, "\r\n\x00") &&
		validDNSLabel(result.Credential.SecretName) && len(result.Credential.SecretResourceVersion) > 0 && len(result.Credential.SecretResourceVersion) <= 128 &&
		!strings.ContainsAny(result.Credential.SecretResourceVersion, "\r\n\x00") && validModelCatalogDigest(result.Credential.ContentSHA256)
}

func (repository *Repository) CompleteProviderCredentialRenewalTask(ctx context.Context, task port.ProviderCredentialRenewalTask, result port.ProviderCredentialRenewalResult) (resultErr error) {
	if !validProviderRenewalBinding(task) || uuid.Validate(task.OrganizationID) != nil || !validProviderRenewalResult(result) {
		return errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	actor, err := repository.resolveProofIdentity(ctx, port.ProofPrincipalInput{CallerWorkload: "control-plane", Operation: port.ProviderCredentialRenewalOperation,
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation"})
	if err != nil || actor.OrganizationID != task.OrganizationID {
		return errs.ErrForbidden
	}
	resolved, err := repository.ResolvePrincipal(ctx, value.Principal{ActorID: actor.ActorID, AuthorityTenant: actor.OrganizationID, CorrelationRef: task.Ref})
	if err != nil {
		return err
	}
	current, err := repository.resolveScope(ctx, resolved)
	if err != nil {
		return err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	locked, err := lockProviderRenewal(ctx, tx, task.OrganizationID, task.Ref)
	if err != nil {
		return err
	}
	if !sameProviderRenewalBinding(task, locked.task) || task.OwnerRequestDigest != locked.task.OwnerRequestDigest {
		return errs.ErrConflict
	}
	// RV mutable receipt — evidence READ, а не identity immutable результата.
	semanticResult := result
	semanticResult.ReceiptVersion = ""
	raw, err := json.Marshal(semanticResult)
	if err != nil {
		return errs.ErrInvalid
	}
	resultDigest := digestBytes(raw)
	if locked.task.State == "SUCCEEDED" {
		if locked.resultDigest == resultDigest {
			return nil
		}
		return errs.ErrConflict
	}
	if result.State == "UNKNOWN_OUTCOME" {
		if locked.task.State == "UNKNOWN_OUTCOME" || locked.task.State == "FAILED" {
			return nil
		}
		if _, err := tx.Exec(ctx, queryProviderRenewalUnknownTask, locked.id); err != nil {
			return errs.ErrUnavailable
		}
		if tx.Commit(ctx) != nil {
			return errs.ErrUnavailable
		}
		return nil
	}
	if !locked.eligible || locked.task.State != "AUTHORIZED" && locked.task.State != "UNKNOWN_OUTCOME" {
		return errs.ErrConflict
	}
	if locked.task.State == "AUTHORIZED" && !locked.task.ExpiresAt.After(locked.now) ||
		locked.task.State == "UNKNOWN_OUTCOME" && !result.Recovered {
		return errs.ErrForbidden
	}
	var eligible bool
	if err := tx.QueryRow(ctx, queryProviderRenewalInputEligible, task.OrganizationID, locked.accountID, task.AccountVersion, locked.credentialID).Scan(&eligible); err != nil {
		return errs.ErrUnavailable
	}
	if !eligible || result.Credential.ContentSHA256 == task.Previous.ContentSHA256 {
		return errs.ErrConflict
	}
	credentialRef, err := newRef("pcr")
	if err != nil {
		return err
	}
	var credentialID string
	var revision int64
	if err := tx.QueryRow(ctx, queryRuntimeCommitprovidercredentialrefreshInsertRevision, pgx.StrictNamedArgs{
		"ref": credentialRef, "organization_id": task.OrganizationID, "provider_account_id": locked.accountID,
		"secret_name": result.Credential.SecretName, "secret_uid": result.Credential.SecretUID,
		"secret_resource_version": result.Credential.SecretResourceVersion, "content_sha256": result.Credential.ContentSHA256,
	}).Scan(&credentialID, &revision); err != nil {
		return errs.ErrConflict
	}
	// Сначала закрываем свою task, чтобы account trigger не отменил успешный intent.
	tag, err := tx.Exec(ctx, queryProviderRenewalCompleteTask, pgx.StrictNamedArgs{"task_id": locked.id, "credential_id": credentialID,
		"receipt_uid": result.ReceiptUID, "receipt_version": result.ReceiptVersion, "result_digest": resultDigest})
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	tag, err = tx.Exec(ctx, queryRuntimeCommitprovidercredentialrefreshActivateRevision, locked.accountID, locked.credentialID, credentialID)
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	if err := repository.scheduleProviderCredentialCleanup(ctx, tx, task.OrganizationID, locked.accountID, locked.credentialID,
		locked.now.Add(providerCredentialCleanupRetention)); err != nil {
		return err
	}
	if err := repository.auditRoleImage(ctx, tx, current, "", "PROVIDER_CREDENTIAL_RENEWED", "PROVIDER_ACCOUNT", task.AccountRef, "i18n:PROVIDER_CREDENTIAL_REFRESH_COMMITTED"); err != nil {
		return err
	}
	if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "PROVIDER_ACCOUNT_CHANGED", "", task.AccountRef, "i18n:PROVIDER_ACCOUNT_UPDATED", task.AccountVersion+1, ""); err != nil {
		return err
	}
	if tx.Commit(ctx) != nil {
		return errs.ErrUnavailable
	}
	return nil
}
