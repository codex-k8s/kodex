package postgres

import (
	"context"
	_ "embed"
	"errors"
)

// queryManualComplete stores safe terminal readback under the exact owner custody.
// queryManualComplete сохраняет safe terminal readback под exact owner custody.
//
//go:embed sql/manual_complete.sql
var queryManualComplete string

// queryManualCheck attests the actual typed claim consumer and durable intent foreign key.
// queryManualCheck проверяет actual typed claim consumer и durable intent foreign key.
//
//go:embed sql/manual_check.sql
var queryManualCheck string

// CheckManual rejects startup when manual intent custody cannot be consumed safely.
// CheckManual отклоняет startup, если manual intent custody нельзя безопасно обработать.
func (repository *Repository) CheckManual(ctx context.Context) error {
	var ready bool
	if err := repository.pool.QueryRow(ctx, queryManualCheck).Scan(&ready); err != nil || !ready {
		return errors.New("artifact manual purge custody is unavailable")
	}
	return nil
}
