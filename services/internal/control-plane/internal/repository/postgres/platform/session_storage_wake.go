package platform

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// emitSessionStorageRunChanged resolves one server-owned root anchor and tenant for a session
// identity and emits a RUN_CHANGED metadata snapshot in the caller transaction. No run means no
// event; it creates neither a run delta nor fanout to all roots, and consumers must reauthorize
// subscriptions.
// emitSessionStorageRunChanged разрешает один серверный root anchor и tenant по идентичности
// сессии и записывает RUN_CHANGED metadata snapshot в переданной транзакции. Без запуска события
// нет; не создаёт run delta и fanout на все roots, потребители повторно проверяют права подписок.
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
