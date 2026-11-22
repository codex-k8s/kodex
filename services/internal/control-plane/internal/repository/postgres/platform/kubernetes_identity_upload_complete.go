package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// recordKubernetesUploadFactTx appends evidence under the original immutable storage boundary.
// A different known UID/RV/content is a conflict, not an observation-based replacement. This helper
// grants neither binding nor another external request and remains usable after owner cancellation.
// Its flag requires quarantine only for the first negative witness conflicting with retained KNOWN.
// recordKubernetesUploadFactTx добавляет evidence в original immutable storage boundary.
// Иные known UID/RV/content — conflict, не replacement через observation. Helper не разрешает
// binding либо новый external request и остаётся доступным после owner cancellation.
// Flag требует quarantine лишь для первого negative witness, конфликтующего с retained KNOWN.
func recordKubernetesUploadFactTx(ctx context.Context, tx pgx.Tx, row kubernetesUploadRow, attemptID string, input command.KubernetesIdentityUploadComplete) (bool, error) {
	var rejected, known bool
	if tx.QueryRow(ctx, queryKubernetesSetupFactClassification, pgx.StrictNamedArgs{
		"organization_id": row.input.OrganizationID, "setup_id": row.id,
	}).Scan(&rejected, &known) != nil {
		return false, errs.ErrUnavailable
	}
	if input.RejectedAcknowledgement == nil && rejected {
		return false, errs.ErrConflict
	}
	if input.RejectedAcknowledgement != nil {
		witness := *input.RejectedAcknowledgement
		if _, err := tx.Exec(ctx, queryKubernetesSetupRejectionInsert, pgx.StrictNamedArgs{
			"attempt_id": attemptID, "response_digest": witness.ResponseSHA256, "truncated": witness.Truncated, "observed_uid": witness.ObservedUID,
		}); err != nil {
			return false, mapWriteError(err)
		}
		var stored value.ManagedKubernetesAcknowledgementRejection
		err := tx.QueryRow(ctx, queryKubernetesSetupRejectionRead, pgx.StrictNamedArgs{"attempt_id": attemptID}).Scan(
			&stored.ResponseSHA256, &stored.Truncated, &stored.ObservedUID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, errs.ErrUnavailable
		}
		if errors.Is(err, pgx.ErrNoRows) || stored != witness || !stored.Valid() {
			return false, errs.ErrConflict
		}
		return known && !rejected, nil
	}
	outcome, identityID := "UNKNOWN", ""
	if input.Identity != nil {
		var namespace string
		if tx.QueryRow(ctx, queryKubernetesSetupOriginalNamespace, pgx.StrictNamedArgs{
			"registry_revision": row.input.RegistryRevision,
		}).Scan(&namespace) != nil {
			return false, errs.ErrUnavailable
		}
		locator := *input.Identity
		if locator.Validate(namespace) != nil || locator.IdentityRef != row.input.IdentityRef || locator.TargetRef != row.input.TargetRef ||
			locator.Revision != row.input.IdentityRevision || locator.ContentSHA256 != row.input.ContentDigest {
			return false, errs.ErrConflict
		}
		if _, err := tx.Exec(ctx, queryKubernetesSetupMaterialInsert, pgx.StrictNamedArgs{
			"organization_id": row.input.OrganizationID, "identity_id": row.input.IdentityID,
			"namespace": locator.Namespace, "name": locator.Name, "uid": locator.UID,
			"resource_version": locator.ResourceVersion, "content_digest": locator.ContentSHA256,
		}); err != nil {
			return false, mapWriteError(err)
		}
		stored, err := kubernetesKnownMaterialTx(ctx, tx, row, namespace)
		if err != nil {
			return false, err
		}
		if stored == nil || *stored != locator {
			return false, errs.ErrConflict
		}
		outcome, identityID = "KNOWN", row.input.IdentityID
	}
	if _, err := tx.Exec(ctx, queryKubernetesSetupObservationInsert, pgx.StrictNamedArgs{
		"attempt_id": attemptID, "outcome": outcome, "identity_id": identityID,
	}); err != nil {
		return false, mapWriteError(err)
	}
	return false, nil
}

// CompleteKubernetesIdentityUpload records late evidence without recovering expired execution credit.
// The private fence permits only reporting the original attempt. Binding additionally requires the
// fresh original actor/package/target/generation and an unexpired active attempt in this transaction.
// CompleteKubernetesIdentityUpload сохраняет late evidence без восстановления expired execution credit.
// Private fence разрешает лишь отчёт original attempt. Binding дополнительно требует fresh original
// actor/package/target/generation и unexpired active attempt в этой transaction.
func (repository *Repository) CompleteKubernetesIdentityUpload(ctx context.Context, principal value.Principal, input command.KubernetesIdentityUploadComplete) (result entity.KubernetesIdentitySetupView, resultErr error) {
	if err := kubernetesSetupBrokerCaller(principal, "platform.runtime.kubernetes.setup.complete"); err != nil {
		return result, err
	}
	if len(input.SetupRef) > 96 || !strings.HasPrefix(input.SetupRef, "ksu_") || !managedkubernetes.ValidLocator(input.SetupRef) ||
		len(input.AttemptRef) > 96 || !strings.HasPrefix(input.AttemptRef, "ksa_") || !managedkubernetes.ValidLocator(input.AttemptRef) ||
		input.Generation < 1 || input.Generation > 8 || len(input.Fence) > 96 || !managedkubernetes.ValidLocator(input.Fence) ||
		len(input.WorkloadInstance) > 96 || !managedkubernetes.ValidLocator(input.WorkloadInstance) ||
		!input.ValidOutcome() {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := repository.resolveScope(ctx, principal); err != nil {
		return result, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	source, err := scanKubernetesOperator(tx.QueryRow(ctx, queryKubernetesSetupSourceShare))
	if err != nil {
		return result, err
	}
	fenceHash := sha256.Sum256([]byte(input.Fence))
	fenceDigest := hex.EncodeToString(fenceHash[:])
	var permitDigest, attemptID string
	err = tx.QueryRow(ctx, queryKubernetesSetupAttemptLookup, pgx.StrictNamedArgs{
		"setup_ref": input.SetupRef, "attempt_ref": input.AttemptRef, "generation": input.Generation,
		"instance": input.WorkloadInstance, "fence_digest": fenceDigest,
	}).Scan(&permitDigest, &attemptID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	readArgs := pgx.StrictNamedArgs{"setup_ref": input.SetupRef, "permit_digest": permitDigest}
	row, err := scanKubernetesUploadRow(tx.QueryRow(ctx, queryKubernetesSetupPrivatePermit, readArgs))
	if err != nil {
		return result, err
	}
	var connectionID string
	if tx.QueryRow(ctx, queryKubernetesSetupConnectionLockAny, pgx.StrictNamedArgs{
		"organization_id": row.input.OrganizationID, "connection_id": row.input.ConnectionID,
	}).Scan(&connectionID) != nil || connectionID != row.input.ConnectionID {
		return result, errs.ErrUnavailable
	}
	row, err = scanKubernetesUploadRow(tx.QueryRow(ctx, queryKubernetesSetupPrivatePermit, readArgs))
	if err != nil {
		return result, err
	}
	var quarantineBindingRef string
	if input.RejectedAcknowledgement != nil {
		quarantineBindingRef, err = lockKubernetesQuarantineBindingTx(ctx, tx, row)
		if err != nil {
			return result, err
		}
	}
	var current scope
	var ownerErr error
	var accessUntil time.Time
	if !row.state.Terminal() && row.now.Before(row.input.ExpiresAt) && input.RejectedAcknowledgement == nil {
		current, _, accessUntil, ownerErr = repository.validateKubernetesUploadOwnerTx(ctx, tx, row, source, principal.CorrelationRef)
		if ownerErr != nil && !brokerIdentitySetupAuthorityLost(ownerErr) {
			return result, ownerErr
		}
	}
	var version int64
	var state enum.KubernetesIdentitySetupState
	err = tx.QueryRow(ctx, queryKubernetesSetupPrivateLock, pgx.StrictNamedArgs{
		"organization_id": row.input.OrganizationID, "setup_id": row.id,
	}).Scan(&version, &state)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrUnavailable
	}
	if errors.Is(err, pgx.ErrNoRows) || version != row.version || state != row.state {
		return result, errs.ErrConflict
	}
	var attemptState string
	var expiresAt, now time.Time
	if tx.QueryRow(ctx, queryKubernetesSetupAttemptLock, pgx.StrictNamedArgs{
		"attempt_id": attemptID, "organization_id": row.input.OrganizationID, "setup_id": row.id,
		"attempt_ref": input.AttemptRef, "generation": input.Generation, "instance": input.WorkloadInstance, "fence_digest": fenceDigest,
	}).Scan(&attemptState, &expiresAt, &now) != nil || (attemptState != "ACTIVE" && attemptState != "COMPLETED" && attemptState != "UNKNOWN" && attemptState != "CANCELLED" && attemptState != "EXPIRED" && attemptState != "ACK_REJECTED") {
		return result, errs.ErrUnavailable
	}
	quarantine, err := recordKubernetesUploadFactTx(ctx, tx, row, attemptID, input)
	if err != nil {
		return result, err
	}
	if quarantine {
		if err := repository.quarantineKubernetesIdentityTx(ctx, tx, row, quarantineBindingRef, principal.CorrelationRef); err != nil {
			return result, err
		}
	}
	closeState := "UNKNOWN"
	if input.Identity != nil {
		closeState = "COMPLETED"
	}
	if input.RejectedAcknowledgement != nil {
		closeState = "ACK_REJECTED"
	}
	if _, err := tx.Exec(ctx, queryKubernetesSetupAttemptComplete, pgx.StrictNamedArgs{
		"attempt_id": attemptID, "organization_id": row.input.OrganizationID, "setup_id": row.id, "state": closeState,
	}); err != nil {
		return result, errs.ErrUnavailable
	}
	if !row.state.Terminal() {
		authorityClose := ""
		switch {
		case input.RejectedAcknowledgement != nil:
			authorityClose = "ACK_REJECTED"
		case !now.Before(row.input.ExpiresAt):
			authorityClose = "EXPIRED"
		case ownerErr != nil || accessDeadlineExpired(accessUntil):
			authorityClose = "CANCELLED"
		case input.Identity != nil && attemptState == "ACTIVE" && now.Before(expiresAt):
			attachErr := repository.attachKubernetesUploadIdentityTx(ctx, tx, current, row, accessUntil)
			if attachErr != nil {
				if !brokerIdentitySetupAuthorityLost(attachErr) {
					return result, attachErr
				}
				authorityClose = "CANCELLED"
			}
		case row.state == enum.KubernetesIdentitySetupAttempting && attemptState == "ACTIVE":
			if _, err := tx.Exec(ctx, queryKubernetesSetupTransition, pgx.StrictNamedArgs{
				"organization_id": row.input.OrganizationID, "setup_id": row.id, "state": "UNKNOWN",
			}); err != nil {
				return result, errs.ErrUnavailable
			}
		}
		if authorityClose != "" {
			if _, err := tx.Exec(ctx, queryKubernetesSetupCloseAuthority, pgx.StrictNamedArgs{
				"organization_id": row.input.OrganizationID, "setup_id": row.id, "state": authorityClose,
			}); err != nil {
				return result, errs.ErrUnavailable
			}
		}
	}
	result, err = readKubernetesSetupView(tx.QueryRow(ctx, queryKubernetesSetupPrivateView, pgx.StrictNamedArgs{
		"organization_id": row.input.OrganizationID, "setup_id": row.id,
	}))
	if err != nil {
		return entity.KubernetesIdentitySetupView{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.KubernetesIdentitySetupView{}, errs.ErrUnavailable
	}
	return result, nil
}

// attachKubernetesUploadIdentityTx lets definitive authority denial roll back activation without losing
// the authenticated material observation and attempt receipt recorded in the parent transaction.
// attachKubernetesUploadIdentityTx при окончательном отказе откатывает подключение credential,
// сохраняя подтверждённое наблюдение материала и receipt попытки в родительской транзакции.
func (repository *Repository) attachKubernetesUploadIdentityTx(ctx context.Context, tx pgx.Tx, current scope, row kubernetesUploadRow, accessUntil time.Time) (resultErr error) {
	save, err := tx.Begin(ctx)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, save, &resultErr)
	tag, err := save.Exec(ctx, queryKubernetesSetupTransition, pgx.StrictNamedArgs{
		"organization_id": row.input.OrganizationID, "setup_id": row.id, "state": "COMPLETED",
	})
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrUnavailable
	}
	if _, err := repository.attachIntegrationBrokerIdentityRevisionTx(ctx, save, current, row.owner, row.input.IdentityID, accessUntil); err != nil {
		return err
	}
	if accessDeadlineExpired(accessUntil) {
		return errs.ErrForbidden
	}
	if save.Commit(ctx) != nil {
		return errs.ErrUnavailable
	}
	return nil
}
