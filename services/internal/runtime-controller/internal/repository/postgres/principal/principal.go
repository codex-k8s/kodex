// Package principal проверяет неизменяемую PostgreSQL runtime identity.
package principal

import (
	"context"
	_ "embed"
	"errors"

	"github.com/jackc/pgx/v5"
)

//go:embed session_user.sql
var sessionUserQuery string

type queryRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func Check(ctx context.Context, database queryRow, expected string) error {
	if database == nil || expected == "" {
		return errors.New("runtime-controller PostgreSQL principal configuration is invalid")
	}
	var actual string
	if err := database.QueryRow(ctx, sessionUserQuery).Scan(&actual); err != nil || actual != expected {
		return errors.New("runtime-controller PostgreSQL principal mismatch")
	}
	return nil
}
