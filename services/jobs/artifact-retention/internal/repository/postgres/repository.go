// Package postgres хранит authoritative claim и tombstone retention lifecycle.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/codex-k8s/kodex/services/jobs/artifact-retention/internal/retention"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (repository *Repository) Claim(
	ctx context.Context,
	owner string,
	batchSize int,
	leaseSeconds int64,
) ([]retention.Claim, error) {
	rows, err := repository.pool.Query(ctx, queryClaimDue, pgx.StrictNamedArgs{
		"claim_owner":   owner,
		"batch_size":    batchSize,
		"lease_seconds": leaseSeconds,
	})
	if err != nil {
		return nil, fmt.Errorf("claim due artifacts: %w", err)
	}
	defer rows.Close()
	claims := make([]retention.Claim, 0, batchSize)
	for rows.Next() {
		var claim retention.Claim
		if err := rows.Scan(&claim.ArtifactID, &claim.ArtifactRef, &claim.ObjectKey, &claim.ObjectVersion, &claim.Generation); err != nil {
			return nil, fmt.Errorf("scan artifact retention claim: %w", err)
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read artifact retention claims: %w", err)
	}
	return claims, nil
}

func (repository *Repository) Finalize(
	ctx context.Context,
	claim retention.Claim,
	owner string,
) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin artifact retention finalization: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var organizationID, projectID, artifactRef string
	err = tx.QueryRow(ctx, queryLockClaim, pgx.StrictNamedArgs{
		"artifact_id":      claim.ArtifactID,
		"claim_owner":      owner,
		"claim_generation": claim.Generation,
	}).Scan(&organizationID, &projectID, &artifactRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return retention.ErrLostClaim
	}
	if err != nil {
		return fmt.Errorf("lock artifact retention claim: %w", err)
	}
	arguments := pgx.StrictNamedArgs{"artifact_id": claim.ArtifactID}
	for _, query := range []string{queryDeleteBindings, queryDeleteDownloadGrants, queryDeleteContent} {
		if _, err := tx.Exec(ctx, query, arguments); err != nil {
			return fmt.Errorf("delete artifact retention dependency: %w", err)
		}
	}
	var actorID string
	if err := tx.QueryRow(ctx, queryUpsertServiceSubject, pgx.StrictNamedArgs{"organization_id": organizationID}).Scan(&actorID); err != nil {
		return fmt.Errorf("resolve artifact retention actor: %w", err)
	}
	result, err := tx.Exec(ctx, queryFinalizeTombstone, pgx.StrictNamedArgs{
		"artifact_id":      claim.ArtifactID,
		"claim_owner":      owner,
		"claim_generation": claim.Generation,
	})
	if err != nil {
		return fmt.Errorf("finalize artifact retention tombstone: %w", err)
	}
	if result.RowsAffected() != 1 {
		return retention.ErrLostClaim
	}
	auditRef := "aud_" + uuid.NewString()
	correlationRef := fmt.Sprintf("retention_%s_%d", claim.ArtifactRef, claim.Generation)
	if _, err := tx.Exec(ctx, queryInsertAuditEvent, pgx.StrictNamedArgs{
		"audit_ref":       auditRef,
		"organization_id": organizationID,
		"project_id":      projectID,
		"actor_id":        actorID,
		"artifact_ref":    artifactRef,
		"correlation_ref": correlationRef,
	}); err != nil {
		return fmt.Errorf("record artifact retention audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit artifact retention finalization: %w", err)
	}
	return nil
}
