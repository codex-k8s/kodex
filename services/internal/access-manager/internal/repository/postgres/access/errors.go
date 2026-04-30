package access

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
)

const (
	postgresUniqueViolation     = "23505"
	postgresForeignKeyViolation = "23503"
	postgresCheckViolation      = "23514"
	postgresSerialization       = "40001"
	postgresDeadlock            = "40P01"
)

func wrapError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", operation, errs.ErrNotFound)
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case postgresUniqueViolation:
			return fmt.Errorf("%s: %w", operation, errors.Join(errs.ErrAlreadyExists, err))
		case postgresForeignKeyViolation:
			return fmt.Errorf("%s: %w", operation, errors.Join(errs.ErrPreconditionFailed, err))
		case postgresCheckViolation:
			return fmt.Errorf("%s: %w", operation, errors.Join(errs.ErrInvalidArgument, err))
		case postgresSerialization, postgresDeadlock:
			return fmt.Errorf("%s: %w", operation, errors.Join(errs.ErrConflict, err))
		}
	}

	return fmt.Errorf("%s: %w", operation, err)
}
