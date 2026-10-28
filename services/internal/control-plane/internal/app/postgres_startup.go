package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/postgresdiagnostic"
	"github.com/jackc/pgx/v5/pgxpool"
)

// postgresStartupPingBudget caps each initial connectivity probe inside the unchanged service startup budget.
// postgresStartupPingBudget ограничивает каждый начальный ping внутри прежнего startup budget сервиса.
const postgresStartupPingBudget = 10 * time.Second

// postgresStartupRetryDelay spaces only typed connection-refused probes without restarting the caller budget.
// postgresStartupRetryDelay разделяет только типизированные connection-refused ping без перезапуска caller budget.
const postgresStartupRetryDelay = time.Second

// postgresStartupPool exposes only the initial probe and rejected-pool cleanup, never application SQL.
// postgresStartupPool предоставляет только начальную проверку и закрытие отвергнутого pool, без прикладного SQL.
type postgresStartupPool interface {
	// Ping performs the existing pool probe under the caller's bounded context.
	// Ping выполняет прежнюю проверку pool в ограниченном контексте вызывающего.
	Ping(context.Context) error
	// Close releases the rejected pool before the startup error can reach its caller.
	// Close освобождает отвергнутый pool до возврата startup error вызывающему.
	Close()
}

// openPostgres loads the unchanged DSN/CA/TLS/pool configuration and admits a pool after bounded connectivity readiness.
// Exhausted or terminal failure closes the pool and returns before Run can construct storage or workers.
// openPostgres загружает прежние DSN/CA/TLS/pool настройки и допускает pool после ограниченной проверки доступности.
// Исчерпание бюджета либо постоянный отказ закрывает pool до создания storage и workers в Run.
func openPostgres(ctx context.Context, config Config) (*pgxpool.Pool, error) {
	if err := ctx.Err(); err != nil {
		return nil, startupFailure(stageVerifyPostgreSQLConnection, err)
	}
	poolConfig, err := loadPostgresPoolConfig(config)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, startupFailure(stageVerifyPostgreSQLConnection, err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, startupFailure(stageOpenPostgreSQLPool, err)
	}
	if err := verifyPostgresStartup(ctx, pool); err != nil {
		return nil, err
	}
	return pool, nil
}

// loadPostgresPoolConfig preserves the prior TLS13 root/SNI and pool-size policy without dialing.
// File/configuration causes remain private typed errors and malformed CA never downgrades TLS.
// loadPostgresPoolConfig сохраняет прежние TLS13 root/SNI и pool size без подключения.
// Причины file/configuration остаются приватными типизированными ошибками; неверный CA не ослабляет TLS.
func loadPostgresPoolConfig(config Config) (*pgxpool.Config, error) {
	rawDSN, err := readBoundedFile(config.PostgresDSNFile)
	if err != nil {
		return nil, startupFailure(stageLoadPostgreSQLConfiguration, err)
	}
	poolConfig, err := parsePostgresStartupConfig(rawDSN)
	if err != nil {
		return nil, startupFailure(stageParsePostgreSQLConfiguration, err)
	}
	ca, err := readBoundedFile(config.PostgresCAFile)
	if err != nil {
		return nil, startupFailure(stageLoadPostgreSQLCA, err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, startupFailure(stageParsePostgreSQLCA, nil)
	}
	poolConfig.ConnConfig.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: config.PostgresTLSServerName}
	poolConfig.MaxConns = config.PostgresMaxConnections
	return poolConfig, nil
}

// parsePostgresStartupConfig clears the caller's raw DSN bytes after parsing on both success and failure.
// Parsed connection fields remain private pool configuration, as before; the function does not log or dial.
// parsePostgresStartupConfig очищает исходные байты DSN вызывающего после успешного и неуспешного разбора.
// Разобранные поля остаются приватной конфигурацией pool, как прежде; функция не логирует и не подключается.
func parsePostgresStartupConfig(raw []byte) (*pgxpool.Config, error) {
	defer clear(raw)
	return pgxpool.ParseConfig(strings.TrimSpace(string(raw)))
}

// verifyPostgresStartup waits only through typed connection-refused errors under Run's original startup deadline.
// Each probe has a ten-second cap; cancel-aware pauses never authorize SQL/bootstrap/worker retries.
// Terminal failure or cancellation closes exactly this pool; successful ownership passes to Run.
// verifyPostgresStartup ожидает только типизированные connection-refused отказы под исходным startup deadline Run.
// Каждый ping ограничен десятью секундами; отменяемые паузы не разрешают повторы SQL/bootstrap/workers.
// Постоянный отказ либо отмена закрывает именно этот pool; успешное владение передаётся Run.
func verifyPostgresStartup(ctx context.Context, pool postgresStartupPool) error {
	accepted := false
	defer func() {
		if !accepted {
			pool.Close()
		}
	}()
	var lastRefused error
	for {
		if err := ctx.Err(); err != nil {
			if lastRefused == nil {
				return startupFailure(stageVerifyPostgreSQLConnection, err)
			}
			return &startupError{stage: stageVerifyPostgreSQLConnection, cause: lastRefused, budget: err}
		}
		probe, cancel := context.WithTimeout(ctx, postgresStartupPingBudget)
		err := pool.Ping(probe)
		if err == nil {
			err = probe.Err()
		}
		cancel()
		if err == nil {
			err = ctx.Err()
		}
		if err == nil {
			accepted = true
			return nil
		}
		class := postgresdiagnostic.Class(err)
		if budget := ctx.Err(); budget != nil {
			if lastRefused != nil && (class == "canceled" || class == "timeout") {
				err = lastRefused
			}
			return &startupError{stage: stageVerifyPostgreSQLConnection, cause: err, budget: budget}
		}
		if class != "connection_refused" {
			return startupFailure(stageVerifyPostgreSQLConnection, err)
		}
		lastRefused = err
		timer := time.NewTimer(postgresStartupRetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return &startupError{stage: stageVerifyPostgreSQLConnection, cause: lastRefused, budget: ctx.Err()}
		case <-timer.C:
		}
	}
}
