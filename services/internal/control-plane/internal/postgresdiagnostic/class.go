// Package postgresdiagnostic classifies this service's typed PostgreSQL connection failures without exposing their details.
// Пакет postgresdiagnostic классифицирует типизированные отказы PostgreSQL этого сервиса без раскрытия подробностей.
package postgresdiagnostic

import (
	"context"
	"database/sql/driver"
	"errors"
	"os"

	"github.com/codex-k8s/kodex/libs/go/observability/errorclass"
	"github.com/jackc/pgx/v5/pgconn"
)

// Class prioritizes typed PostgreSQL/TLS/configuration failures over network wrappers.
// Driver cause loss stays bad_connection; cancellation/deadline and unknown causes get fixed labels.
// Class даёт типизированным PostgreSQL/TLS/configuration отказам приоритет над сетевыми wrappers.
// Потеря причины драйвером остаётся bad_connection; отмена, срок и неизвестные причины получают фиксированные метки.
func Class(err error) string {
	var database *pgconn.PgError
	if errors.As(err, &database) {
		return errorclass.Postgres(database.Code)
	}
	if class := errorclass.TLS(err); class != "unknown" {
		return class
	}
	var configuration *pgconn.ParseConfigError
	if errors.As(err, &configuration) {
		return "connection_configuration"
	}
	var path *os.PathError
	if errors.As(err, &path) {
		return "filesystem"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, driver.ErrBadConn) {
		return "bad_connection"
	}
	return errorclass.Network(err)
}
