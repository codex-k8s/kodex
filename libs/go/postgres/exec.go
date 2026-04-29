package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Execer is the minimal pgx-compatible Exec contract used by repositories.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Exec runs Exec on a pool, connection or transaction.
func Exec(ctx context.Context, db Execer, sql string, args ...any) error {
	_, err := db.Exec(ctx, sql, args...)
	return err
}

// ExecRequireRow returns pgx.ErrNoRows when a mutation affects no rows.
func ExecRequireRow(ctx context.Context, db Execer, sql string, args ...any) error {
	tag, err := db.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
