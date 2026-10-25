package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// collectTypedRows preserves row order and limit+1 capacity while decoding each row once.
// A decoder failure discards partial items as Unavailable. It deliberately leaves rows.Err,
// row Close and transaction completion to callers, preserving their distinct error ordering.
// collectTypedRows сохраняет порядок строк и capacity limit+1, декодируя каждую строку один раз.
// Ошибка decoder отбрасывает partial items с Unavailable. rows.Err, Close и завершение transaction
// намеренно оставляет callers, сохраняя их отдельный порядок проверки ошибок.
func collectTypedRows[T any](rows pgx.Rows, limit int32, scan func(rowScanner) (T, error)) ([]T, error) {
	items := make([]T, 0, limit+1)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		items = append(items, item)
	}
	return items, nil
}

// readRevisionRowsPage reads the already authorized bounded query, trims lookahead and computes
// the caller's exact revision cursor before checking iteration failure and committing once.
// Scan, iteration or commit failures discard items/cursor as Unavailable; iteration failure
// short-circuits commit. The caller owns SQL, eligibility, cursor validation, Close and rollback.
// readRevisionRowsPage читает уже авторизованный bounded query, убирает lookahead и вычисляет
// точный revision cursor caller до проверки iteration failure и однократного commit. Ошибки
// scan/iteration/commit отбрасывают items/cursor с Unavailable; iteration failure исключает commit.
// SQL, eligibility, validation cursor, Close и rollback принадлежат вызывающему.
func readRevisionRowsPage[T any](ctx context.Context, tx pgx.Tx, rows pgx.Rows, limit int32, scan func(rowScanner) (T, error), revision func(T) string) ([]T, string, error) {
	items, err := collectTypedRows(rows, limit, scan)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = revision(items[len(items)-1])
	}
	if rows.Err() != nil || tx.Commit(ctx) != nil {
		return nil, "", errs.ErrUnavailable
	}
	return items, next, nil
}
