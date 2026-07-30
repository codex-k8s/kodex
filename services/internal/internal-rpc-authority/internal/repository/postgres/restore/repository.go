package restore

import (
	"context"
	"errors"
	"time"

	domainrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository применяет и читает durable restore fence.
type Repository struct {
	pool *pgxpool.Pool
}

// New создаёт адаптер только с заданным PostgreSQL pool.
func New(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("restore fence PostgreSQL pool is required")
	}
	return &Repository{pool: pool}, nil
}

// ApplyRestoreFence атомарно применяет следующий допустимый fence.
func (repository *Repository) ApplyRestoreFence(
	ctx context.Context,
	state model.RestoreState,
) error {
	var safeWindow *time.Time
	if state.SafeWindowNotBefore != 0 {
		value := time.Unix(state.SafeWindowNotBefore, 0).UTC()
		safeWindow = &value
	}
	var applied bool
	err := repository.pool.QueryRow(
		ctx,
		applyFenceSQL,
		pgx.StrictNamedArgs{
			"database_cluster_id":    state.DatabaseClusterID,
			"restore_epoch":          state.RestoreEpoch,
			"phase":                  state.Phase,
			"evidence_digest_sha256": state.EvidenceDigest,
			"safe_window_not_before": safeWindow,
		},
	).Scan(&applied)
	if err != nil {
		return err
	}
	if !applied {
		return domainrepository.ErrIdempotencyConflict
	}
	return nil
}

// RestoreFenceReady сверяет фактически сохранённый fence с ожидаемым.
func (repository *Repository) RestoreFenceReady(
	ctx context.Context,
	state model.RestoreState,
) error {
	var safeWindow *time.Time
	if state.SafeWindowNotBefore != 0 {
		value := time.Unix(state.SafeWindowNotBefore, 0).UTC()
		safeWindow = &value
	}
	var ready bool
	err := repository.pool.QueryRow(
		ctx,
		fenceReadinessSQL,
		pgx.StrictNamedArgs{
			"database_cluster_id":    state.DatabaseClusterID,
			"restore_epoch":          state.RestoreEpoch,
			"phase":                  state.Phase,
			"evidence_digest_sha256": state.EvidenceDigest,
			"safe_window_not_before": safeWindow,
		},
	).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return errors.New("restore fence readback mismatch")
	}
	return nil
}

var _ domainrepository.RestoreFenceStore = (*Repository)(nil)
