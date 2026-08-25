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

	"github.com/caarlos0/env/v11"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

type command string

const (
	commandUp     command = "up"
	commandStatus command = "status"
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
	environment := struct {
		DSNFile       string `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE,required,notEmpty"`
		TLSServerName string `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME,required,notEmpty"`
	}{}
	if err := env.Parse(&environment); err != nil {
		return errors.New("parse migration environment configuration")
	}
	raw, err := securefile.Read(environment.DSNFile, 16<<10)
	if err != nil {
		return fmt.Errorf("read PostgreSQL DSN file: %w", err)
	}
	defer clear(raw)
	config, err := pgx.ParseConfig(strings.TrimSpace(string(raw)))
	if err != nil {
		return errors.New("parse PostgreSQL DSN")
	}
	expectedServerName := environment.TLSServerName
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
	goose.SetTableName("public.goose_db_version")
	if err := goose.SetDialect("postgres"); err != nil {
		return errors.New("configure PostgreSQL migration dialect")
	}
	switch action {
	case commandUp:
		if err := goose.UpContext(ctx, database, "migrations"); err != nil {
			return fmt.Errorf("apply internal-rpc-authority migrations: %w", err)
		}
		return nil
	case commandStatus:
		return migrationStatus(ctx, database)
	default:
		return errors.New("unsupported CLI command")
	}
}

func parseCommand(arguments []string) (command, error) {
	if len(arguments) != 1 {
		return "", errors.New("usage: internal-rpc-authority-cli <up|status>")
	}
	switch command(arguments[0]) {
	case commandUp, commandStatus:
		return command(arguments[0]), nil
	default:
		return "", errors.New("usage: internal-rpc-authority-cli <up|status>")
	}
}

func migrationStatus(ctx context.Context, database *sql.DB) error {
	if err := goose.StatusContext(ctx, database, "migrations"); err != nil {
		return fmt.Errorf("read internal-rpc-authority migration status: %w", err)
	}
	return nil
}
