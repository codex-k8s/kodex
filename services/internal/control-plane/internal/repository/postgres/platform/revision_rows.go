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
	items, err := collectRowsInto(rows, make([]T, 0, limit+1), scan)
	if err != nil {
		return nil, errs.ErrUnavailable
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

// collectRowsInto appends decoded rows in Next order to the caller's chosen initial slice.
// It preserves nil/capacity policy and returns the original scan error with no partial result.
// Iteration errors, explicit Close and transaction completion remain outside; pgx may still
// auto-close its own rows on exhaustion or scan failure.
// collectRowsInto добавляет декодированные строки в порядке Next к выбранному caller срезу.
// Сохраняет nil/capacity policy и возвращает исходную scan error без частичного результата.
// Iteration errors, явный Close и завершение транзакции остаются снаружи; pgx по-прежнему может
// автоматически закрыть собственные rows при завершении либо ошибке сканирования.
func collectRowsInto[T any](rows pgx.Rows, items []T, scan func(rowScanner) (T, error)) ([]T, error) {
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
