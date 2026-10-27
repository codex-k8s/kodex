package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
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

// collectClosedRows appends decoded rows to the caller's chosen slice, closes the rows before
// checking iteration failure and discards results on either scan or iteration error as Unavailable.
// A scan error skips Err after Close. It owns no query, transaction or authorization.
// collectClosedRows добавляет декодированные строки к выбранному caller срезу, закрывает rows
// до проверки iteration error и отбрасывает результат при scan или iteration error с Unavailable.
// При scan error не вызывает Err после Close. Не владеет query, transaction или authorization.
func collectClosedRows[T any](rows pgx.Rows, initial []T, scan func(rowScanner) (T, error)) ([]T, error) {
	items, err := collectRowsInto(rows, initial, scan)
	rows.Close()
	if err != nil || rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return items, nil
}

// readPlatformCatalog resolves the caller before issuing the server-selected embedded catalog
// query, then owns its deferred row Close. The two enabled global catalogs retain their own SQL
// and typed column mappings; this helper is not a tenant-resource or mutation authorization gate.
// Query failures become Unavailable, while readCatalogRows preserves the legacy iteration policy.
// readPlatformCatalog разрешает вызывающего до выполнения выбранного сервером встроенного запроса
// каталога, затем владеет отложенным Close строк. Два глобальных enabled каталога сохраняют свои
// SQL и типизированные колонки; helper не авторизует ресурсы tenant или изменения состояния.
// Ошибка Query даёт Unavailable, а readCatalogRows сохраняет прежнюю политику iteration errors.
func readPlatformCatalog[T any](ctx context.Context, repository *Repository, principal value.Principal, queryText string, scan func(rowScanner) (T, error)) ([]T, error) {
	if _, err := repository.resolveScope(ctx, principal); err != nil {
		return nil, err
	}
	rows, err := repository.pool.Query(ctx, queryText)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	return readCatalogRows(rows, scan)
}

// readCatalogRows starts with a nil slice and delegates decoding/appends to collectRowsInto.
// Scan failures discard all items as Unavailable; an iteration failure instead returns the same
// raw error with accumulated items. Err is read only after exhaustion, and this helper neither
// closes rows nor completes a transaction; driver auto-close behavior remains unchanged.
// readCatalogRows начинает с nil среза и передаёт decoding/append в collectRowsInto. Ошибка Scan
// отбрасывает все элементы с Unavailable; iteration failure возвращает ту же исходную ошибку и
// накопленные элементы. Err читается после завершения Next; helper не закрывает строки и не
// завершает транзакцию, автоматическое закрытие драйвером остаётся прежним.
func readCatalogRows[T any](rows pgx.Rows, scan func(rowScanner) (T, error)) ([]T, error) {
	items, err := collectRowsInto(rows, []T(nil), scan)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	return items, rows.Err()
}
