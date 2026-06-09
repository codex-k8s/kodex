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
