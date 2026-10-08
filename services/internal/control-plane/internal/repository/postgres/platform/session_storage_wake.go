package platform

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// emitSessionStorageRunChanged не создаёт run delta или fanout по всем roots.
// Anchor и tenant принадлежат owner rows; потребитель заново разрешает подписки.
func (repository *Repository) emitSessionStorageRunChanged(ctx context.Context, tx pgx.Tx, current scope, sessionID string) error {
	var anchor string
	var version int64
	err := tx.QueryRow(ctx, querySessionArchiveRunWake, pgx.StrictNamedArgs{"session_identity": sessionID}).
		Scan(&current.organizationID, &current.organizationRef, &anchor, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // Session без запуска не имеет run projection.
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	return repository.emitPlatformEventSnapshot(ctx, tx, current, "RUN_CHANGED", "", anchor, "i18n:RUN_METADATA_UPDATED", version, "")
}
