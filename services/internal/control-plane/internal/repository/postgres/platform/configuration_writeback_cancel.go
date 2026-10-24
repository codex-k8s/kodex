package platform

import (
	"context"
	_ "embed"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryWriteBackActive embeds ordered selection of active proposals for the exact tenant and
// matching configuration or connection; unknown outcomes remain included for authority withdrawal.
// queryWriteBackActive встраивает упорядоченный выбор активных proposals точного tenant по
// configuration либо connection; неизвестные исходы включаются для отзыва полномочий.
//
//go:embed sql/configuration_writeback__active.sql
var queryWriteBackActive string

// cancelConfigurationWriteBacks locks active proposals in the caller transaction and clears each
// old claim. Unstarted work cancels with SOURCE_CHANGED; started/unknown work conflicts for
// configuration-only cancellation but preserves UNKNOWN_OUTCOME/AUTHORITY_CHANGED for connection
// withdrawal, allowing later read-only recovery. Each changed row saves audit/event without
// blocking credential revocation on an uncertain provider result.
// cancelConfigurationWriteBacks блокирует активные proposals в транзакции caller и очищает каждый
// прежний claim. Неначатую работу отменяет с SOURCE_CHANGED; started/unknown work даёт Conflict
// при отмене только по configuration, но сохраняет UNKNOWN_OUTCOME/AUTHORITY_CHANGED при отзыве
// connection для последующего read-only recovery. Каждая изменённая строка сохраняет audit/event,
// не блокируя отзыв credential неизвестным результатом провайдера.
func (repository *Repository) cancelConfigurationWriteBacks(ctx context.Context, tx pgx.Tx, current scope, configurationRef, connectionRef string) error {
	rows, err := tx.Query(ctx, queryWriteBackActive, current.organizationID, configurationRef, connectionRef)
	if err != nil {
		return errs.ErrUnavailable
	}
	refs := []string{}
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		refs = append(refs, ref)
	}
	rows.Close()
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	if len(refs) == 0 {
		return nil
	}
	var now time.Time
	if tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&now) != nil {
		return errs.ErrUnavailable
	}
	for _, ref := range refs {
		row, err := lockWriteBack(ctx, tx, current, ref)
		if err != nil {
			return err
		}
		if writeBackTerminal(row.proposal.State) {
			continue
		}
		// Отзыв connection/credential нельзя блокировать неизвестным внешним эффектом.
		// Сохраняем intent для readonly recovery и закрываем прежний claim атомарно.
		if row.started != nil || row.proposal.State == entity.WriteBackUnknown {
			if connectionRef == "" {
				return errs.ErrConflict
			}
			row.proposal.State, row.proposal.FailureCode = entity.WriteBackUnknown, "AUTHORITY_CHANGED"
		} else {
			row.proposal.State, row.proposal.FailureCode, row.proposal.CompletedAt = entity.WriteBackCancelled, "SOURCE_CHANGED", &now
		}
		row.lease.Fence, row.lease.Claimant, row.lease.ExpiresAt = "", "", time.Time{}
		if err := repository.saveWriteBack(ctx, tx, current, &row, true); err != nil {
			return err
		}
	}
	return nil
}
