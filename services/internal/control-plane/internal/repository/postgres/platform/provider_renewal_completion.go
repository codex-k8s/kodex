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
	// queryProviderRenewalCompleteTask embeds success recording for an authorized/unknown renewal task
	// with output credential, next account version, materializer receipt and semantic result digest.
	// Account activation and atomic commit are separate statements in the same owner transaction.
	// queryProviderRenewalCompleteTask встраивает запись success authorized/unknown renewal task с
	// output credential, next account version, materializer receipt и semantic result digest. Account
	// activation и атомарный commit выполняются отдельными statements той же owner transaction.
	//go:embed sql/provider_renewal__complete_task.sql
	queryProviderRenewalCompleteTask string
	// queryProviderRenewalUnknownTask embeds closure of claimed/authorized renewal into FAILED before
	// authorization or UNKNOWN_OUTCOME after it. This records uncertainty without creating another
	// attempt or repeating external materialization.
	// queryProviderRenewalUnknownTask встраивает закрытие claimed/authorized renewal в FAILED до
	// authorization либо UNKNOWN_OUTCOME после неё. Фиксирует неопределённость без создания новой
	// attempt или повторной внешней materialization.
	//go:embed sql/provider_renewal__unknown_task.sql
	queryProviderRenewalUnknownTask string
	// queryProviderRenewalRecoveryTasks embeds at most one ordered UNKNOWN_OUTCOME task in the system
	// organization whose account version/current input credential still match an enabled authorized
	// account. Returned binding and descriptor metadata enable observation, not a new execution grant.
	// queryProviderRenewalRecoveryTasks встраивает не более одной упорядоченной UNKNOWN_OUTCOME task
	// system organization, чьи account version/current input credential ещё совпадают с enabled
	// authorized account. Returned binding и descriptor metadata нужны для observation, а не нового
	// execution grant.
	//go:embed sql/provider_renewal__recovery_tasks.sql
	queryProviderRenewalRecoveryTasks string
	// queryProviderRenewalReadiness embeds exact catalog checks for renewal columns, two unique
	// partial indexes and enabled task/account protection triggers. A true result verifies this
	// database profile only, not broker availability, key delivery or the complete renewal path.
	// queryProviderRenewalReadiness встраивает exact catalog checks renewal columns, двух unique
	// partial indexes и enabled task/account protection triggers. True проверяет только этот DB
	// profile, а не broker availability, key delivery или полный renewal path.
	//go:embed sql/provider_renewal__readiness.sql
	queryProviderRenewalReadiness string
)

// CheckProviderCredentialRenewalReadiness checks the stored renewal schema/index/trigger profile
// under a five-second child context. Query/scan failure or a false result is Unavailable; it does
// not run renewal or verify external services.
// CheckProviderCredentialRenewalReadiness проверяет stored renewal schema/index/trigger profile в
// пятисекундном child context. Query/scan failure либо false дают Unavailable; renewal не
// запускается, внешние services не проверяются.
func (repository *Repository) CheckProviderCredentialRenewalReadiness(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var ready bool
	if repository.pool.QueryRow(ctx, queryProviderRenewalReadiness).Scan(&ready) != nil || !ready {
		return errs.ErrUnavailable
	}
	return nil
}

// ListProviderCredentialRenewalRecoveryTasks resolves the fixed control-plane observation identity
// and lists the SQL-bounded current recovery binding within five seconds. It closes/checks rows
// and returns nil on failure, carrying only credential descriptors; it neither claims a task nor
// retries an external effect.
// ListProviderCredentialRenewalRecoveryTasks разрешает fixed control-plane observation identity и
// читает SQL-bounded current recovery binding за пять секунд. Закрывает/проверяет rows и
// возвращает nil при ошибке, перенося только credential descriptors; task не захватывает и
// external effect не повторяет.
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

// validProviderRenewalResult accepts UNKNOWN_OUTCOME only without a credential; SUCCEEDED requires
// canonical receipt/Secret UUIDs, bounded clean resource versions, DNS Secret name and digest
// syntax. It validates shape only, without checking task binding, receipt authenticity or external
// contents.
// validProviderRenewalResult принимает UNKNOWN_OUTCOME только без credential; SUCCEEDED требует
// canonical receipt/Secret UUIDs, bounded clean resource versions, DNS Secret name и digest
// syntax. Проверяет только shape, без task binding, receipt authenticity либо внешнего contents.
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

// CompleteProviderCredentialRenewalTask validates input, resolves the fixed system identity and
// exact tenant, locks and compares task bindings under a five-second transaction. Semantic replay
// ignores mutable receipt version; unknown completion closes without resend. Success requires
// current eligible inputs, changed digest, authorized unexpired task or recovered unknown task,
// then records revision/task success before activation, retained cleanup, audit and event in one
// commit. Locked-read rollback errors remain observable; no secret bytes or external refresh are
// handled here.
// CompleteProviderCredentialRenewalTask проверяет input, разрешает fixed system identity и exact
// tenant, блокирует и сравнивает task bindings в пятисекундной transaction. Semantic replay
// исключает mutable receipt version; unknown completion закрывается без resend. Success требует
// current eligible inputs, новый digest, authorized unexpired task либо recovered unknown task,
// затем записывает revision/task success до activation, retained cleanup, audit и event одним
// commit. Locked-read rollback errors остаются видимыми; secret bytes и external refresh здесь не
// обрабатываются.
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
