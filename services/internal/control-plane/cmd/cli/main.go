package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/caarlos0/env/v11"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

const schemaVersion int64 = 20260731000100

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "control-plane CLI failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string) error {
	if len(arguments) != 2 || arguments[0] != "migrate" {
		return errors.New("usage: control-plane-cli migrate expand|up|status|version")
	}
	action := arguments[1]
	if action != "expand" && action != "up" &&
		action != "status" && action != "version" {
		return errors.New("usage: control-plane-cli migrate expand|up|status|version")
	}
	config := struct {
		DSNFile       string `env:"CONTROL_PLANE_POSTGRES_MIGRATION_DSN_FILE,required,notEmpty"`
		TLSServerName string `env:"CONTROL_PLANE_POSTGRES_TLS_SERVER_NAME,required,notEmpty"`
		CAFile        string `env:"CONTROL_PLANE_POSTGRES_CA_FILE,required,notEmpty"`
	}{}
	if err := env.Parse(&config); err != nil {
		return errors.New("parse migration environment")
	}
	raw, err := readRuntimeFile(config.DSNFile, 64<<10)
	if err != nil {
		return errors.New("read bounded PostgreSQL migration DSN")
	}
	pgxConfig, err := pgx.ParseConfig(strings.TrimSpace(string(raw)))
	if err != nil {
		return errors.New("parse PostgreSQL migration DSN")
	}
	if len(pgxConfig.Fallbacks) != 0 ||
		pgxConfig.Host != config.TLSServerName ||
		pgxConfig.TLSConfig == nil ||
		pgxConfig.TLSConfig.ServerName != config.TLSServerName ||
		pgxConfig.TLSConfig.InsecureSkipVerify {
		return errors.New("PostgreSQL migration DSN must use verify-full TLS")
	}
	caRaw, err := readRuntimeFile(config.CAFile, 1<<20)
	if err != nil {
		return errors.New("read PostgreSQL migration CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return errors.New("parse PostgreSQL migration CA")
	}
	pgxConfig.TLSConfig = &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: config.TLSServerName,
		RootCAs:    roots,
	}
	database := stdlib.OpenDB(*pgxConfig)
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		return errors.New("check PostgreSQL migration connection")
	}
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return errors.New("configure PostgreSQL migration dialect")
	}
	switch action {
	case "expand":
		return migrateExpand(ctx, database)
	case "up":
		return migrateUp(ctx, database)
	case "status":
		return goose.StatusContext(ctx, database, "migrations")
	case "version":
		version, err := goose.GetDBVersionContext(ctx, database)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(os.Stdout, "current migration version: %d\n", version)
		return err
	default:
		return errors.New("unsupported migration command")
	}
}

func readRuntimeFile(path string, maximum int64) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("runtime file path is not absolute")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 ||
		info.Size() > maximum || info.Mode().Perm()&0o007 != 0 {
		return nil, errors.New("runtime file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || int64(len(raw)) > maximum {
		return nil, errors.New("read bounded runtime file")
	}
	return raw, nil
}

func migrateExpand(ctx context.Context, database *sql.DB) error {
	if err := goose.UpToContext(
		ctx,
		database,
		"migrations",
		schemaVersion,
	); err != nil {
		return fmt.Errorf("apply control-plane expand migration: %w", err)
	}
	return nil
}

func migrateUp(ctx context.Context, database *sql.DB) error {
	version, err := goose.GetDBVersionContext(ctx, database)
	if err != nil {
		return err
	}
	if version < schemaVersion {
		return errors.New("expand migration must complete before up")
	}
	if err := goose.UpContext(ctx, database, "migrations"); err != nil {
		return fmt.Errorf("apply control-plane migrations: %w", err)
	}
	return nil
}
