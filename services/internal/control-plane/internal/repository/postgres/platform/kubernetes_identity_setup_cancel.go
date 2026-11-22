package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// kubernetesSetupCancelOperation fixes receipt and audit scope independently from caller payload.
// kubernetesSetupCancelOperation фиксирует receipt и audit scope независимо от caller payload.
const kubernetesSetupCancelOperation = "platform.integrations.kubernetes.setup.cancel"

// queryKubernetesSetupLockOwned locks only the original active actor's addressed setup.
// queryKubernetesSetupLockOwned блокирует только addressed setup исходного active actor.
//
//go:embed sql/kubernetes_setup__lock_owned.sql
var queryKubernetesSetupLockOwned string

// queryKubernetesSetupCancel closes every active attempt and its exact setup in one owner transaction.
// queryKubernetesSetupCancel закрывает все active attempts и exact setup в одной owner transaction.
//
//go:embed sql/kubernetes_setup__cancel.sql
var queryKubernetesSetupCancel string

// CancelKubernetesIdentitySetup revokes one setup's grants while retaining uncertain material history.
// Current visibility and original actor ownership precede receipt replay and OCC. No external request
// is cancelled by this transaction; late CREATE evidence must still be recorded without rebinding.
// CancelKubernetesIdentitySetup отзывает grants одной setup, сохраняя uncertain material history.
// Current visibility и original actor ownership предшествуют receipt replay и OCC. Transaction
// не отменяет external request; late CREATE evidence сохраняется отдельно без повторной binding.
func (repository *Repository) CancelKubernetesIdentitySetup(ctx context.Context, principal value.Principal, mutation value.Mutation, input command.KubernetesIdentitySetupCancel) (result entity.KubernetesIdentitySetupView, resultErr error) {
	if err := kubernetesSetupAPICaller(principal, kubernetesSetupCancelOperation); err != nil {
		return result, err
	}
	if len(input.ConnectionRef) > 96 || !managedkubernetes.ValidLocator(input.ConnectionRef) ||
		len(input.SetupRef) > 96 || !strings.HasPrefix(input.SetupRef, "ksu_") || !managedkubernetes.ValidLocator(input.SetupRef) ||
		mutation.ExpectedVersion == nil || *mutation.ExpectedVersion < 1 ||
		len(mutation.IdempotencyKey) < 8 || len(mutation.IdempotencyKey) > 128 || !utf8.ValidString(mutation.IdempotencyKey) ||
		strings.ContainsAny(mutation.IdempotencyKey, "\r\n\x00") {
		return result, errs.ErrInvalid
	}
	intent, err := json.Marshal(struct {
		Connection, Setup string
		Version           int64
	}{input.ConnectionRef, input.SetupRef, *mutation.ExpectedVersion})
	if err != nil {
		return result, errs.ErrInvalid
	}
	digest := sha256.Sum256(intent)
	intentDigest := hex.EncodeToString(digest[:])
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	if err := repository.requireAccess(ctx, tx, current, "integration.view", entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: input.ConnectionRef,
	}); err != nil {
		return result, concealAccessDenial(err, errs.ErrNotFound)
	}
	if _, err := repository.lockIntegrationConnection(ctx, tx, current.organizationID, input.ConnectionRef); err != nil {
		return result, err
	}
	var setupID string
	var version int64
	var state enum.KubernetesIdentitySetupState
	err = tx.QueryRow(ctx, queryKubernetesSetupLockOwned, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"connection_ref": input.ConnectionRef, "setup_ref": input.SetupRef,
	}).Scan(&setupID, &version, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil || !state.Valid() {
		return result, errs.ErrUnavailable
	}
	var storedDigest string
	var payload []byte
	err = tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation,
		current.organizationID, current.actorID, kubernetesSetupCancelOperation, mutation.IdempotencyKey).Scan(&storedDigest, &payload)
	if err == nil {
		if storedDigest != intentDigest {
			return result, errs.ErrIdempotencyReuse
		}
		if json.Unmarshal(payload, &result) != nil || result.Ref != input.SetupRef || result.ConnectionRef != input.ConnectionRef ||
			(result.State != enum.KubernetesIdentitySetupCancelled && result.State != enum.KubernetesIdentitySetupExpired) {
			return entity.KubernetesIdentitySetupView{}, errs.ErrUnavailable
		}
		if tx.Commit(ctx) != nil {
			return entity.KubernetesIdentitySetupView{}, errs.ErrUnavailable
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrUnavailable
	}
	if version != *mutation.ExpectedVersion {
		return result, errs.ErrVersionMismatch
	}
	if state == enum.KubernetesIdentitySetupCompleted || state == enum.KubernetesIdentitySetupAcknowledgementRejected {
		return result, errs.ErrConflict
	}
	if !state.Terminal() {
		tag, err := tx.Exec(ctx, queryKubernetesSetupCancel, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "setup_id": setupID, "expected_version": version,
		})
		if err != nil || tag.RowsAffected() != 1 {
			return result, errs.ErrUnavailable
		}
	}
	result, err = readKubernetesSetupView(tx.QueryRow(ctx, queryKubernetesSetupReadOwned, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"connection_ref": input.ConnectionRef, "setup_ref": input.SetupRef, "begin_key": "",
	}))
	if err != nil {
		return entity.KubernetesIdentitySetupView{}, err
	}
	payload, err = json.Marshal(result)
	if err != nil {
		return entity.KubernetesIdentitySetupView{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest,
		current.organizationID, current.actorID, kubernetesSetupCancelOperation, mutation.IdempotencyKey,
		intentDigest, "KUBERNETES_IDENTITY_SETUP", payload); err != nil {
		return entity.KubernetesIdentitySetupView{}, errs.ErrUnavailable
	}
	auditRef, err := newRef("aud")
	if err != nil {
		return entity.KubernetesIdentitySetupView{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef,
		current.organizationID, nil, current.actorID, kubernetesSetupCancelOperation, "INTEGRATION_CONNECTION", input.ConnectionRef,
		"i18n:KUBERNETES_IDENTITY_SETUP_CANCELLED", current.correlationRef); err != nil {
		return entity.KubernetesIdentitySetupView{}, errs.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return entity.KubernetesIdentitySetupView{}, errs.ErrUnavailable
	}
	return result, nil
}
