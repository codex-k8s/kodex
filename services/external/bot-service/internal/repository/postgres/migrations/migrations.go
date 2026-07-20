package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"
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
	return run(ctx, dsn, 0, "")
}

// RunForRuntimeRole применяет миграции и выдаёт отдельно подготовленной runtime-роли только DML-контракт.
func RunForRuntimeRole(ctx context.Context, dsn string, runtimeRole string) error {
	runtimeRole = strings.TrimSpace(runtimeRole)
	if runtimeRole == "" {
		return fmt.Errorf("runtime database role is required")
	}
	return run(ctx, dsn, 0, runtimeRole)
}

// RunTo применяет миграции до указанной версии включительно.
func RunTo(ctx context.Context, dsn string, version int64) error {
	if version <= 0 {
		return fmt.Errorf("migration version must be positive")
	}
	return run(ctx, dsn, version, "")
}

// DownOne запрашивает один откат goose. Forward-only миграции возвращают ошибку и сохраняют версию.
func DownOne(ctx context.Context, dsn string) error {
	db, err := openDatabase(ctx, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	configureGoose()
	if configureErr != nil {
		return configureErr
	}
	if err := goose.DownContext(ctx, db, "."); err != nil {
		return fmt.Errorf("run goose down: %w", err)
	}
	return nil
}

// Version возвращает точную версию схемы goose для проверки миграций.
func Version(ctx context.Context, dsn string) (int64, error) {
	db, err := openDatabase(ctx, dsn)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	version, err := goose.GetDBVersionContext(ctx, db)
	if err != nil {
		return 0, fmt.Errorf("read goose database version: %w", err)
	}
	return version, nil
}

func run(ctx context.Context, dsn string, version int64, runtimeRole string) error {
	db, err := openDatabase(ctx, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	if runtimeRole != "" {
		if _, err := db.ExecContext(ctx, `select set_config('matter_codex.runtime_role', $1, false)`, runtimeRole); err != nil {
			return fmt.Errorf("configure migration runtime role: %w", err)
		}
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

func openDatabase(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open goose database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping goose database: %w", err)
	}
	return db, nil
}

func configureGoose() {
	configureOnce.Do(func() {
		goose.SetBaseFS(migrationFiles)
		configureErr = goose.SetDialect("postgres")
	})
}
