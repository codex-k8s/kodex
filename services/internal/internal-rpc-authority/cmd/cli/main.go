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
	commandUp                command = "up"
	commandStatus            command = "status"
	commandFreshnessStatus   command = "freshness-status"
	commandFreshnessWatch    command = "freshness-watch"
	commandFreshnessActivate command = "freshness-activate"
	commandRotationStatus    command = "rotation-status"
	commandRotationWatch     command = "rotation-watch"
	commandRotationAbort     command = "rotation-abort"
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
	options, err := parseFreshnessOptions(action, arguments)
	if err != nil {
		return err
	}
	rotationOptions, err := parseRotationOptions(action, arguments)
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
	case commandFreshnessStatus, commandFreshnessWatch, commandFreshnessActivate:
		return runFreshness(ctx, database, action, options, os.Stdout)
	case commandRotationStatus, commandRotationWatch, commandRotationAbort:
		return runRotation(ctx, database, action, rotationOptions, os.Stdout)
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
	if len(arguments) == 7 && arguments[0] == string(commandFreshnessActivate) {
		return commandFreshnessActivate, nil
	}
	if len(arguments) == 9 && arguments[0] == string(commandRotationAbort) {
		return commandRotationAbort, nil
	}
	if len(arguments) == 3 && arguments[0] == string(commandRotationWatch) {
		return commandRotationWatch, nil
	}
	if len(arguments) != 1 {
		return "", errors.New(cliUsage)
	}
	switch command(arguments[0]) {
	case commandUp, commandStatus, commandFreshnessStatus, commandFreshnessWatch, commandRotationStatus:
		return command(arguments[0]), nil
	default:
		return "", errors.New(cliUsage)
	}
}

const cliUsage = "usage: internal-rpc-authority-cli <up|status|freshness-status|freshness-watch|freshness-activate --expected-version 1 --activation-id UUID --confirm ACTIVATE-STAGING-AUTHORITY-FRESHNESS|rotation-status|rotation-watch --operation-id UUID|rotation-abort --intent-id UUID --source-revision NUMBER --source-digest-sha256 SHA256 --confirm ABORT-STAGING-AUTHORITY-ROTATION>"

func migrationStatus(ctx context.Context, database *sql.DB) error {
	if err := goose.StatusContext(ctx, database, "migrations"); err != nil {
		return fmt.Errorf("read internal-rpc-authority migration status: %w", err)
	}
	return nil
}
