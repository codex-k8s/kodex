package platform

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// scanOwnerProjection scans caller-selected private IDs and decodes the typed JSON into target.
// It preserves partially scanned fields and maps missing, driver and JSON errors as before; owner
// authorization remains separate.
// scanOwnerProjection читает выбранные caller внутренние ID и декодирует typed JSON в target.
// Сохраняет частичные поля и прежнее отображение ошибок; проверка владельца выполняется отдельно.
func scanOwnerProjection(row rowScanner, target any, ids ...any) error {
	var raw []byte
	if err := row.Scan(append(ids, &raw)...); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	} else if err != nil {
		return errs.ErrUnavailable
	}
	if json.Unmarshal(raw, target) != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// lockOwnerVersionState reads a caller-selected tenant-scoped lock tuple; its result does not
// authorize mutation or choose lifecycle rules for the caller. It checks OCC only after the lock.
// lockOwnerVersionState читает выбранный caller lock tuple в границе tenant; результат не выдаёт
// право изменения и не выбирает lifecycle. Проверяет ожидаемую версию только после lock.
func lockOwnerVersionState(ctx context.Context, tx pgx.Tx, query, organizationID, ref string, expected *int64) (string, int64, string, error) {
	var id, state string
	var version int64
	err := tx.QueryRow(ctx, query, organizationID, ref).Scan(&id, &version, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return id, version, state, errs.ErrNotFound
	}
	if err != nil {
		return id, version, state, errs.ErrUnavailable
	}
	if expected == nil || version != *expected {
		return id, version, state, errs.ErrVersionMismatch
	}
	return id, version, state, nil
}
