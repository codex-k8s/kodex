package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

// queryPersonalInvocationCompletionContext reads immutable personal lineage without locking i first.
// queryPersonalInvocationCompletionContext читает immutable personal lineage, не блокируя i первым.
//
//go:embed sql/personal_invocation__completion_context.sql
var queryPersonalInvocationCompletionContext string

// queryPersonalInvocationCompletionLockConnection retains completion after disable/revoke while
// serializing its binding update with Begin and key rotation.
// queryPersonalInvocationCompletionLockConnection сохраняет completion после disable/revoke,
// сериализуя изменение binding с Begin и ротацией ключа.
//
//go:embed sql/personal_invocation__completion_lock_connection.sql
var queryPersonalInvocationCompletionLockConnection string

// queryPersonalInvocationCompletionAdmitted requires a durable admission of this exact lease.
// queryPersonalInvocationCompletionAdmitted требует durable admission именно этого lease.
//
//go:embed sql/personal_invocation__completion_admitted.sql
var queryPersonalInvocationCompletionAdmitted string

// queryPersonalInvocationRequireVerification resets only the still-current rejected descriptor.
// No generic 401/403 classification may poison a subsequently rotated credential.
// queryPersonalInvocationRequireVerification сбрасывает лишь текущий отклонённый descriptor.
// Общая классификация 401/403 не затрагивает позднее ротированную credential.
//
//go:embed sql/personal_invocation__require_verification.sql
var queryPersonalInvocationRequireVerification string

// personalInvocationCompletionPin holds immutable coordinates for an admitted effect's history.
// Current binding readiness is not a condition for recording its already started external result.
// personalInvocationCompletionPin хранит immutable coordinates истории допущенного effect.
// Текущая готовность binding не является условием записи результата уже начатой внешней работы.
type personalInvocationCompletionPin struct {
	bindingID, credentialRef string
	generation               int64
}

// lockPersonalInvocationCompletion locks Connection→binding before the caller locks invocation.
// Shared completion is unchanged; personal completion requires the exact previous Begin receipt
// even after rotation/revoke. It neither reissues credentials nor cancels admitted work.
// lockPersonalInvocationCompletion блокирует Connection→binding до блокировки invocation caller.
// Shared completion не меняется; personal completion требует exact прежний Begin receipt даже
// после rotation/revoke. Credential не выдаётся заново, допущенная работа не отменяется.
func (repository *Repository) lockPersonalInvocationCompletion(ctx context.Context, tx pgx.Tx, current scope, caller string, input command.IntegrationInvocationInput) (*personalInvocationCompletionPin, error) {
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": input.InvocationRef}
	var connectionRef string
	var pin personalInvocationCompletionPin
	err := tx.QueryRow(ctx, queryPersonalInvocationCompletionContext, args).Scan(&connectionRef, &pin.bindingID, &pin.generation, &pin.credentialRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errs.ErrNotFound
	}
	if err != nil {
		return nil, serializableTransactionError(err, errs.ErrUnavailable)
	}
	// The owner creates every personal intent and its pin atomically. An absent pin therefore
	// retains historical shared completion even after the Connection policy is changed later.
	// Owner атомарно создаёт personal intent вместе с pin. Отсутствие pin сохраняет historical
	// shared completion даже после поздней смены policy Connection.
	if pin.bindingID == "" {
		return nil, nil
	}
	if caller != "integration-gateway" || pin.bindingID == "" || pin.generation < 1 || pin.credentialRef == "" {
		return nil, errs.ErrForbidden
	}
	var locked string
	if err := tx.QueryRow(ctx, queryPersonalInvocationCompletionLockConnection, pgx.StrictNamedArgs{"organization_id": current.organizationID, "connection_ref": connectionRef}).Scan(&locked); err != nil {
		return nil, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if err := tx.QueryRow(ctx, queryPersonalCredentialTestLockBinding, pgx.StrictNamedArgs{"organization_id": current.organizationID, "binding_id": pin.bindingID}).Scan(&locked); err != nil {
		return nil, serializableTransactionError(err, errs.ErrUnavailable)
	}
	fence := sha256.Sum256([]byte(input.Fence))
	var admitted bool
	if err := tx.QueryRow(ctx, queryPersonalInvocationCompletionAdmitted, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": input.InvocationRef,
		"generation": input.Generation, "lease_ref": input.LeaseRef, "fence_digest": hex.EncodeToString(fence[:])}).Scan(&admitted); err != nil {
		return nil, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if !admitted {
		return nil, errs.ErrForbidden
	}
	return &pin, nil
}

// requirePersonalVerificationAfterAuthFailure clears stale usability only for an exact old pin.
// Existing adapters merge 401/403 into AUTH_REJECTED, so this requests re-verification instead
// of inventing token expiry, invalid authentication or a verified missing permission. Pending
// verification jobs are closed atomically so a late probe cannot restore the rejected proof.
// requirePersonalVerificationAfterAuthFailure сбрасывает устаревшую usability лишь exact старого pin.
// Адаптеры объединяют 401/403 в AUTH_REJECTED, поэтому нужна повторная проверка, а не выдуманный
// срок токена, неверная аутентификация либо доказанное отсутствующее право. Pending verification
// jobs закрываются атомарно: поздний probe не восстанавливает отклонённое proof.
func requirePersonalVerificationAfterAuthFailure(ctx context.Context, tx pgx.Tx, current scope, pin *personalInvocationCompletionPin, input command.IntegrationInvocationInput) error {
	if pin == nil || input.Success || input.SafeErrorCode != "INTEGRATION_AUTH_REJECTED" {
		return nil
	}
	tag, err := tx.Exec(ctx, queryPersonalInvocationRequireVerification, pgx.StrictNamedArgs{"organization_id": current.organizationID, "binding_id": pin.bindingID,
		"binding_generation": pin.generation, "credential_ref": pin.credentialRef})
	if err != nil {
		return serializableTransactionError(err, mapWriteError(err))
	}
	if tag.RowsAffected() == 1 {
		return cancelPersonalCredentialTests(ctx, tx, current.organizationID, pin.bindingID)
	}
	return nil
}
