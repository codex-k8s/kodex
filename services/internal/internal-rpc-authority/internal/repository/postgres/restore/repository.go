package restore

import (
	"context"
	"errors"
	"time"

	domainrepository "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository applies and reads durable restore fences through the injected pool and fixed SQL; it does not own the external coordination graph or execute a physical database restore.
// Repository применяет и читает устойчивые ограждения восстановления через внедрённый пул и фиксированный SQL; внешним графом координации не владеет и физическое восстановление БД не выполняет.
type Repository struct {
	pool *pgxpool.Pool
}

// New requires a nonnil PostgreSQL pool and stores it without connecting, probing readiness or applying any fence.
// New требует ненулевой пул PostgreSQL и сохраняет его без подключения, проверки готовности или применения ограждения.
func New(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("restore fence PostgreSQL pool is required")
	}
	return &Repository{pool: pool}, nil
}

// ApplyRestoreFence converts a zero safe-window timestamp to SQL NULL and passes exact cluster/epoch/phase/evidence fields to the database function. Driver errors propagate; a false application result becomes an idempotency conflict.
// ApplyRestoreFence превращает нулевое время безопасного окна в SQL NULL и передаёт точные cluster, epoch, phase и evidence функции БД. Ошибки драйвера передаются выше; false применения становится конфликтом идемпотентности.
func (repository *Repository) ApplyRestoreFence(
	ctx context.Context,
	state model.RestoreState,
) error {
	var applied bool
	err := repository.pool.QueryRow(
		ctx,
		applyFenceSQL,
		restoreFenceArgs(state),
	).Scan(&applied)
	if err != nil {
		return err
	}
	if !applied {
		return domainrepository.ErrIdempotencyConflict
	}
	return nil
}

// RestoreFenceReady reads back that exact fence and nullable safe-window binding, returning a fixed mismatch error for false. It does not advance the fence or assert that all runtime work is allowed.
// RestoreFenceReady читает точное ограждение и nullable-привязку безопасного окна, возвращая фиксированную ошибку при false. Ограждение не продвигается и разрешение всей runtime-работы не утверждается.
func (repository *Repository) RestoreFenceReady(
	ctx context.Context,
	state model.RestoreState,
) error {
	var ready bool
	err := repository.pool.QueryRow(
		ctx,
		fenceReadinessSQL,
		restoreFenceArgs(state),
	).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return errors.New("restore fence readback mismatch")
	}
	return nil
}

// restoreFenceArgs preserves the five exact fence bindings for apply/readback,
// mapping zero safe-window time to NULL and all other Unix seconds to UTC.
// It allocates a fresh map and performs no validation or database operation.
// restoreFenceArgs сохраняет пять точных привязок ограждения для apply и readback,
// превращая нулевое время окна в NULL, остальные секунды Unix — во время UTC.
// Создаётся новая карта без проверки допустимости и операций с БД.
func restoreFenceArgs(state model.RestoreState) pgx.StrictNamedArgs {
	var safeWindow *time.Time
	if state.SafeWindowNotBefore != 0 {
		value := time.Unix(state.SafeWindowNotBefore, 0).UTC()
		safeWindow = &value
	}
	return pgx.StrictNamedArgs{
		"database_cluster_id":    state.DatabaseClusterID,
		"restore_epoch":          state.RestoreEpoch,
		"phase":                  state.Phase,
		"evidence_digest_sha256": state.EvidenceDigest,
		"safe_window_not_before": safeWindow,
	}
}

var _ domainrepository.RestoreFenceStore = (*Repository)(nil)
