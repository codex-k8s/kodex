package platform

import (
	"context"
	_ "embed"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/configuration_writeback__active.sql
var queryWriteBackActive string

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
		if err := saveWriteBack(ctx, tx, current, &row); err != nil {
			return err
		}
	}
	return nil
}
