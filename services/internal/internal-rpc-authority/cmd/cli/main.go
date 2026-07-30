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

const initialSchemaVersion int64 = 20260730000100

type command string

const (
	commandExpand   command = "expand"
	commandContract command = "contract"
	commandUp       command = "up"
	commandStatus   command = "status"
	commandVersion  command = "version"
)

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
	action, err := parseCommand(arguments)
	if err != nil {
		return err
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
		len(config.Fallbacks) != 0 ||
		config.Host != expectedServerName ||
		config.TLSConfig == nil ||
		config.TLSConfig.RootCAs == nil ||
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
	switch action {
	case commandExpand:
		return migrateExpand(ctx, database)
	case commandContract:
		return migrateContract(ctx, database)
	case commandUp:
		return migrateUp(ctx, database)
	case commandStatus:
		return migrationStatus(ctx, database)
	case commandVersion:
		return migrationVersion(ctx, database)
	default:
		return errors.New("unsupported CLI command")
	}
}

func parseCommand(arguments []string) (command, error) {
	if len(arguments) != 2 || arguments[0] != "migrate" {
		return "", errors.New(
			"usage: internal-rpc-authority-cli migrate expand|contract|up|status|version",
		)
	}
	switch command(arguments[1]) {
	case commandExpand, commandContract, commandUp, commandStatus, commandVersion:
		return command(arguments[1]), nil
	default:
		return "", errors.New(
			"usage: internal-rpc-authority-cli migrate expand|contract|up|status|version",
		)
	}
}

func migrateExpand(ctx context.Context, database *sql.DB) error {
	if err := goose.UpToContext(ctx, database, "migrations", initialSchemaVersion); err != nil {
		return fmt.Errorf("apply internal-rpc-authority expand migrations: %w", err)
	}
	return nil
}

func migrateContract(ctx context.Context, database *sql.DB) error {
	version, err := goose.GetDBVersionContext(ctx, database)
	if err != nil {
		return fmt.Errorf("read internal-rpc-authority migration version: %w", err)
	}
	if version < initialSchemaVersion {
		return errors.New("expand migrations must complete before contract")
	}
	return nil
}

func migrateUp(ctx context.Context, database *sql.DB) error {
	version, err := goose.GetDBVersionContext(ctx, database)
	if err != nil {
		return fmt.Errorf("read internal-rpc-authority migration version: %w", err)
	}
	if version < initialSchemaVersion {
		return errors.New("expand migrations must complete before up")
	}
	if err := goose.UpContext(ctx, database, "migrations"); err != nil {
		return fmt.Errorf("apply internal-rpc-authority remaining migrations: %w", err)
	}
	return nil
}

func migrationStatus(ctx context.Context, database *sql.DB) error {
	if err := goose.StatusContext(ctx, database, "migrations"); err != nil {
		return fmt.Errorf("read internal-rpc-authority migration status: %w", err)
	}
	return nil
}

func migrationVersion(ctx context.Context, database *sql.DB) error {
	version, err := goose.GetDBVersionContext(ctx, database)
	if err != nil {
		return fmt.Errorf("read internal-rpc-authority migration version: %w", err)
	}
	_, err = fmt.Fprintf(os.Stdout, "current migration version: %d\n", version)
	return err
}
