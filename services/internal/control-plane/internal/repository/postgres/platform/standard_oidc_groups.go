package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/jackc/pgx/v5"
)

// syncStandardOIDCGroups serializes all sessions of a subject against a durable signed-issuance high-watermark.
// Older tokens preserve newer observations; equal issuance with conflicting groups is denied rather than arbitrarily winning.
// syncStandardOIDCGroups сериализует все сессии subject относительно durable high-watermark подписанного времени выпуска.
// Старые tokens сохраняют новые observations; одинаковое время выпуска с иными groups отклоняется без произвольного выбора.
func (repository *Repository) syncStandardOIDCGroups(ctx context.Context, tx pgx.Tx, organizationID, subjectID string, input platformrepo.ProofPrincipalInput, groups []string) error {
	if repository.standardOIDCPolicy == nil || input.ExternalIssuer != repository.standardOIDCPolicy.Issuer || input.ExternalIssuedAt.IsZero() {
		return errs.ErrForbidden
	}
	var locked int
	if err := tx.QueryRow(ctx, queryAccessSyncLockSubject, pgx.StrictNamedArgs{
		"organization_id": organizationID, "subject_id": subjectID,
	}).Scan(&locked); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	} else if err != nil || locked != 1 {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	raw, err := json.Marshal(groups)
	if err != nil {
		return errs.ErrInvalid
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	var observed time.Time
	var previousDigest string
	var previousRevision uint64
	err = tx.QueryRow(ctx, queryStandardOIDCObservationRead, pgx.StrictNamedArgs{
		"organization_id": organizationID, "subject_id": subjectID,
	}).Scan(&observed, &previousDigest, &previousRevision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	if err == nil {
		if input.ExternalIssuedAt.Before(observed) {
			return nil
		}
		if input.ExternalIssuedAt.Equal(observed) {
			if digest != previousDigest {
				return errs.ErrForbidden
			}
			return nil
		}
	}
	if err := repository.replaceOIDCGroupsTx(ctx, tx, organizationID, subjectID, input, groups); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, queryStandardOIDCObservationAdvance, pgx.StrictNamedArgs{
		"organization_id": organizationID, "subject_id": subjectID, "issued_at": input.ExternalIssuedAt,
		"groups_digest": digest, "revision": input.ExternalSessionRevision,
	})
	if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	if result.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}
