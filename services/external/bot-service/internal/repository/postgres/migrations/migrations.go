package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sync"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var migrationFiles embed.FS

var (
	configureOnce sync.Once
	configureErr  error
)

func Run(ctx context.Context, dsn string) error {
	return run(ctx, dsn, 0)
}

// RunTo применяет миграции до указанной версии включительно.
func RunTo(ctx context.Context, dsn string, version int64) error {
	if version <= 0 {
		return fmt.Errorf("migration version must be positive")
	}
	return run(ctx, dsn, version)
}

func run(ctx context.Context, dsn string, version int64) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open goose database: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping goose database: %w", err)
	}
	configureGoose()
	if configureErr != nil {
		return configureErr
	}
	if version > 0 {
		if err := goose.UpToContext(ctx, db, ".", version); err != nil {
			return fmt.Errorf("run goose migrations through version %d: %w", version, err)
		}
		return nil
	}
	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("run goose migrations: %w", err)
	}
	return nil
}

func configureGoose() {
	configureOnce.Do(func() {
		goose.SetBaseFS(migrationFiles)
		configureErr = goose.SetDialect("postgres")
	})
}
