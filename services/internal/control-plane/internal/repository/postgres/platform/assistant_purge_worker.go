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
	rows, err := repository.pool.Query(ctx, queryAssistantPurgeDue, pgx.StrictNamedArgs{
		"batch_size": batchSize,
	})
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		var purged bool
		if err := rows.Scan(&ref, &purged); err != nil {
			return err
		}
		if ref == "" || !purged {
			return pgx.ErrNoRows
		}
	}
	return rows.Err()
}
