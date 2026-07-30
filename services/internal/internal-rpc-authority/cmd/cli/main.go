package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "internal-rpc-authority CLI failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string) error {
	if len(arguments) != 1 ||
		(arguments[0] != "migrate" && arguments[0] != "status") {
		return errors.New("usage: internal-rpc-authority-cli migrate|status")
	}
	dsnFile := strings.TrimSpace(os.Getenv("INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE"))
	if dsnFile == "" {
		return errors.New("INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE is required")
	}
	raw, err := os.ReadFile(dsnFile)
	if err != nil {
		return errors.New("read PostgreSQL DSN file")
	}
	config, err := pgx.ParseConfig(strings.TrimSpace(string(raw)))
	if err != nil {
		return errors.New("parse PostgreSQL DSN")
	}
	expectedServerName := strings.TrimSpace(
		os.Getenv("INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME"),
	)
	if expectedServerName == "" ||
		config.Host != expectedServerName ||
		config.TLSConfig == nil ||
		config.TLSConfig.ServerName != expectedServerName ||
		config.TLSConfig.InsecureSkipVerify {
		return errors.New("PostgreSQL DSN must use verify-full TLS with exact server name")
	}
	database := stdlib.OpenDB(*config)
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		return errors.New("verify PostgreSQL connectivity")
	}
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return errors.New("configure PostgreSQL migration dialect")
	}
	switch arguments[0] {
	case "migrate":
		return runMigrations(ctx, database)
	case "status":
		return migrationStatus(ctx, database)
	default:
		return errors.New("unsupported CLI command")
	}
}

func runMigrations(ctx context.Context, database *sql.DB) error {
	if err := goose.UpContext(ctx, database, "migrations"); err != nil {
		return fmt.Errorf("apply internal-rpc-authority migrations: %w", err)
	}
	return nil
}

func migrationStatus(ctx context.Context, database *sql.DB) error {
	if err := goose.StatusContext(ctx, database, "migrations"); err != nil {
		return fmt.Errorf("read internal-rpc-authority migration status: %w", err)
	}
	return nil
}
