package platform

import (
	"context"
	_ "embed"

	"github.com/jackc/pgx/v5"
)

//go:embed sql/assistant_purge_due.sql
var queryAssistantPurgeDue string

func (repository *Repository) PurgeDueAssistantConversations(ctx context.Context, batchSize int32) error {
	if batchSize < 1 || batchSize > 100 {
		return pgx.ErrNoRows
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, queryAssistantPurgeDue, pgx.StrictNamedArgs{
		"batch_size": batchSize,
	})
	if err != nil {
		return err
	}
	type purgedConversation struct {
		organizationID, organizationRef, projectRef, ref string
		version                                          int64
	}
	purged := make([]purgedConversation, 0, batchSize)
	for rows.Next() {
		var item purgedConversation
		var deleted bool
		if err := rows.Scan(&item.organizationID, &item.organizationRef, &item.projectRef,
			&item.ref, &item.version, &deleted); err != nil {
			rows.Close()
			return err
		}
		if item.organizationID == "" || item.organizationRef == "" || item.ref == "" || item.version < 1 || !deleted {
			rows.Close()
			return pgx.ErrNoRows
		}
		purged = append(purged, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range purged {
		current := scope{
			organizationID: item.organizationID, organizationRef: item.organizationRef,
			correlationRef: item.ref,
		}
		if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "SYSTEM_ASSISTANT_CHANGED",
			item.projectRef, item.ref, "i18n:ASSISTANT_CONVERSATION_PURGED", item.version, "PURGED"); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
