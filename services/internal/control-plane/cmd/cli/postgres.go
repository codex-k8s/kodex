package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

const (
	// postgresPingBudget bounds the single initial probe independently of the migration's caller budget.
	// postgresPingBudget ограничивает единственный начальный ping независимо от caller budget миграции.
	postgresPingBudget = 10 * time.Second
	// postgresCheckCompleted is the only successful check output and contains no connection metadata.
	// postgresCheckCompleted является единственным успешным выводом check без метаданных соединения.
	postgresCheckCompleted = "PostgreSQL connection check completed"
)

// postgresCLIDependencies separates database construction and Goose entry for offline lifecycle tests.
// Production callbacks never retry migration SQL or expose a generic database command interface.
// postgresCLIDependencies разделяет создание database и вход в Goose для автономных lifecycle tests.
// Production callbacks не повторяют SQL миграций и не предоставляют универсальный интерфейс команд БД.
type postgresCLIDependencies struct {
	open    func(*pgx.ConnConfig) *sql.DB
	migrate func(context.Context, *sql.DB, string) error
}

// runPostgres parses the selected DSN, opens a handle and performs one bounded initial PingContext.
// Check returns before Goose setup/calls; up/status run once with the original caller context.
// runPostgres разбирает выбранный DSN, открывает handle и делает один ограниченный начальный PingContext.
// Check возвращается до настройки и вызовов Goose; up/status работают однократно с исходным caller context.
func runPostgres(ctx context.Context, command string, output io.Writer, dependencies postgresCLIDependencies) error {
	if command != "check" && command != "up" && command != "status" {
		return cliFailure(stageArguments, nil)
	}
	if err := ctx.Err(); err != nil {
		return cliFailure(stageDatabaseConnect, err)
	}
	dsnFile := strings.TrimSpace(os.Getenv("CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE"))
	if dsnFile == "" {
		return cliFailure(stageConfiguration, nil)
	}
	raw, err := os.ReadFile(dsnFile)
	if err != nil {
		return cliFailure(stageDSNRead, err)
	}
	dsn := strings.TrimSpace(string(raw))
	if dsn == "" {
		return cliFailure(stageConfiguration, nil)
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return cliFailure(stageDSNParse, err)
	}
	database := dependencies.open(config)
	defer database.Close()
	probe, cancel := context.WithTimeout(ctx, postgresPingBudget)
	err = probe.Err()
	if err == nil {
		err = database.PingContext(probe)
	}
	if err == nil {
		err = probe.Err()
	}
	cancel()
	if err != nil {
		return cliFailure(stageDatabaseConnect, err)
	}
	if err := ctx.Err(); err != nil {
		return cliFailure(stageDatabaseConnect, err)
	}
	if command == "check" {
		if _, err := fmt.Fprintln(output, postgresCheckCompleted); err != nil {
			return cliFailure(stageOutput, err)
		}
		return nil
	}
	if err := dependencies.migrate(ctx, database, command); err != nil {
		return cliFailure(stageMigration, err)
	}
	return nil
}

// openPostgresDatabase constructs the lazy pgx database/sql handle without claiming connectivity.
// openPostgresDatabase создаёт ленивый pgx database/sql handle без утверждения о готовом соединении.
func openPostgresDatabase(config *pgx.ConnConfig) *sql.DB { return stdlib.OpenDB(*config) }

// applyPostgresMigrations is the sole Goose entry, reserved for up/status after successful ping.
// Status can initialize Goose version state and is deliberately excluded from connection-only check.
// applyPostgresMigrations является единственным входом в Goose для up/status после успешного ping.
// Status может инициализировать состояние версий Goose и намеренно исключён из connection-only check.
func applyPostgresMigrations(ctx context.Context, database *sql.DB, command string) error {
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return cliFailure(stageDialect, err)
	}
	if command == "status" {
		return goose.StatusContext(ctx, database, "migrations")
	}
	return goose.UpContext(ctx, database, "migrations")
}
