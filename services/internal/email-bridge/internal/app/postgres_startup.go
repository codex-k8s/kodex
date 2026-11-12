package app

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/observability/errorclass"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/jackc/pgx/v5/pgconn"
)

// databaseFailureClass prioritizes typed database/TLS/configuration failures over transport and cancellation while never formatting a cause.
// The bare unavailable sentinel means the schema assertion returned false; wrapped unknown dependency failures remain unknown.
// databaseFailureClass ставит типизированные отказы БД/TLS/configuration выше transport и отмены, никогда не форматируя причину.
// Сам unavailable sentinel означает ложную проверку схемы; обёрнутые неизвестные ошибки зависимости остаются unknown.
func databaseFailureClass(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return errorclass.Postgres(pg.Code)
	}
	if class := errorclass.TLS(err); class != "unknown" {
		return class
	}
	var parse *pgconn.ParseConfigError
	if errors.As(err, &parse) {
		return "connection_configuration"
	}
	class := errorclass.Network(err)
	if class != "unknown" && class != "network" {
		return class
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if err == errs.Unavailable {
		return "readiness_rejected"
	}
	return class
}

// retryPostgresConnectivity permits only a typed refusal while cancellation and all higher-priority permanent failures close admission.
// retryPostgresConnectivity допускает только типизированный отказ подключения; отмена и все приоритетные постоянные отказы закрывают допуск.
func retryPostgresConnectivity(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return databaseFailureClass(err) == "connection_refused"
}

// verifyPostgresStartup uses the shared bounded connectivity probe inside the caller's original budget, then checks role/schema exactly once.
// It owns no pool, worker or SQL transaction; caller cleanup remains authoritative and canceled or late-success checks cannot admit startup.
// verifyPostgresStartup использует общую ограниченную проверку подключения в исходном бюджете, затем проверяет роль и схему ровно один раз.
// Pool, worker и SQL-транзакции ему не принадлежат; cleanup остаётся у вызывающего, отмена и поздний успех не допускают startup.
func verifyPostgresStartup(ctx context.Context, ping, ready func(context.Context) error) error {
	if failed := serviceruntime.WaitStartupProbe(ctx, ping, retryPostgresConnectivity, 10*time.Second, time.Second); failed != nil {
		cause := failed.Cause
		class := databaseFailureClass(cause)
		if failed.PreviousRetry != nil && (class == "canceled" || class == "timeout") {
			cause = failed.PreviousRetry
		}
		return failure(stageDatabasePing, errors.Join(cause, failed.Budget))
	}
	if err := ctx.Err(); err != nil {
		return failure(stageDatabaseReady, err)
	}
	if err := ready(ctx); err != nil {
		return failure(stageDatabaseReady, err)
	}
	return failure(stageDatabaseReady, ctx.Err())
}
