package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// postgresStartupPingBudget caps the single startup probe inside the unchanged service startup budget.
// postgresStartupPingBudget ограничивает единственный начальный ping внутри прежнего startup budget сервиса.
const postgresStartupPingBudget = 10 * time.Second

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

// openPostgres loads the unchanged DSN/CA/TLS/pool configuration and admits a pool only after one bounded ping.
// Any failed or canceled probe closes the pool and returns before Run can construct storage or workers.
// openPostgres загружает прежние DSN/CA/TLS/pool настройки и допускает pool лишь после одного ограниченного ping.
// Отказ либо отмена закрывает pool и возвращает управление до создания storage и workers в Run.
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

// verifyPostgresStartup performs one context-aware probe with a ten-second cap and no retries.
// Failure or cancellation before/after ping closes exactly this pool; successful ownership passes to Run.
// verifyPostgresStartup выполняет один context-aware ping с пределом десять секунд и без повторов.
// Отказ либо отмена до/после ping закрывает именно этот pool; успешное владение передаётся Run.
func verifyPostgresStartup(ctx context.Context, pool postgresStartupPool) error {
	probe, cancel := context.WithTimeout(ctx, postgresStartupPingBudget)
	defer cancel()
	err := probe.Err()
	if err == nil {
		err = pool.Ping(probe)
	}
	if err == nil {
		err = probe.Err()
	}
	if err != nil {
		pool.Close()
		return startupFailure(stageVerifyPostgreSQLConnection, err)
	}
	return nil
}
