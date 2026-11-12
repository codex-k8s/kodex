package app

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/observability/errorclass"
	"github.com/jackc/pgx/v5/pgconn"
)

// postgresStartupStep identifies a closed pool preparation phase without including configuration values.
// postgresStartupStep обозначает закрытый этап подготовки pool без значений конфигурации.
type postgresStartupStep string

const (
	// postgresReadDSN identifies private configuration loading before parsing or dialing.
	// postgresReadDSN обозначает загрузку закрытой конфигурации до разбора и подключения.
	postgresReadDSN postgresStartupStep = "READ_DSN"
	// postgresParseDSN identifies pgx configuration parsing without exposing its error text.
	// postgresParseDSN обозначает разбор конфигурации pgx без раскрытия текста ошибки.
	postgresParseDSN postgresStartupStep = "PARSE_DSN"
	// postgresOpenPool identifies construction of the capability pool before its connectivity probe.
	// postgresOpenPool обозначает создание capability pool до проверки соединения.
	postgresOpenPool postgresStartupStep = "OPEN_POOL"
	// postgresPingPool identifies the existing probe including connection and exact session-role setup.
	// postgresPingPool обозначает прежний ping, включая подключение и точную настройку session-role.
	postgresPingPool postgresStartupStep = "PING_POOL"
)

// postgresStartupError retains a private typed cause while keeping Error safe even for older CLI callers.
// postgresStartupError сохраняет закрытую типизированную причину, оставляя Error безопасным и для прежних CLI callers.
type postgresStartupError struct {
	step   postgresStartupStep
	cause  error
	budget error
}

// Error never formats the step, DSN, path, driver message or nested cause.
// Error никогда не форматирует этап, DSN, путь, сообщение драйвера либо вложенную причину.
func (*postgresStartupError) Error() string { return "PostgreSQL startup failed" }

// Unwrap preserves errors.Is/As and context matching without granting retry permission.
// Unwrap сохраняет errors.Is/As и проверку контекста, не разрешая повтор операции.
func (e *postgresStartupError) Unwrap() error { return errors.Join(e.cause, e.budget) }

// postgresStartupDiagnostic emits only registered steps and shared typed classes; raw error strings are never inspected.
// postgresStartupDiagnostic выдаёт только зарегистрированные этапы и общие типизированные классы; исходные тексты ошибок не разбираются.
func postgresStartupDiagnostic(err error) (string, string, bool) {
	var failure *postgresStartupError
	if !errors.As(err, &failure) {
		return "", "", false
	}
	step := diagnosticUnknown
	switch failure.step {
	case postgresReadDSN, postgresParseDSN, postgresOpenPool, postgresPingPool:
		step = string(failure.step)
	}
	var postgres *pgconn.PgError
	if errors.As(failure.cause, &postgres) {
		return step, errorclass.Postgres(postgres.Code), true
	}
	if class := errorclass.TLS(failure.cause); class != "unknown" {
		return step, class, true
	}
	if errors.Is(failure.cause, context.DeadlineExceeded) {
		return step, "deadline", true
	}
	if errors.Is(failure.cause, context.Canceled) {
		return step, "canceled", true
	}
	if class := errorclass.Network(failure.cause); class != "unknown" {
		return step, class, true
	}
	var parse *pgconn.ParseConfigError
	if errors.As(failure.cause, &parse) {
		return step, "configuration", true
	}
	return step, "unknown", true
}

// retryPostgresStartup admits only a typed connection refusal after higher-priority PostgreSQL/TLS/configuration rejection checks.
// retryPostgresStartup допускает только типизированный отказ подключения после приоритетных проверок PostgreSQL/TLS/configuration.
func retryPostgresStartup(err error) bool {
	var parse *pgconn.ParseConfigError
	if errors.As(err, &parse) {
		return false
	}
	_, class, _ := postgresStartupDiagnostic(&postgresStartupError{step: postgresPingPool, cause: err})
	return class == "connection_refused"
}
