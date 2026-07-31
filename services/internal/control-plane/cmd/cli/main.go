package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed sql/*.sql
var operationalSQL embed.FS

const schemaVersion int64 = 20260731000300

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
		return migrateExpand(
			ctx,
			database,
			config.TLSServerName,
			roots,
		)
	case "up":
		return migrateUp(
			ctx,
			database,
			config.TLSServerName,
			roots,
		)
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

func migrateExpand(
	ctx context.Context,
	database *sql.DB,
	serverName string,
	roots *x509.CertPool,
) error {
	if err := goose.UpToContext(
		ctx,
		database,
		"migrations",
		schemaVersion,
	); err != nil {
		return fmt.Errorf("apply control-plane expand migration: %w", err)
	}
	return reconcileRuntimePrincipals(
		ctx,
		database,
		serverName,
		roots,
	)
}

func migrateUp(
	ctx context.Context,
	database *sql.DB,
	serverName string,
	roots *x509.CertPool,
) error {
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
	return reconcileRuntimePrincipals(
		ctx,
		database,
		serverName,
		roots,
	)
}

type runtimePrincipalConfig struct {
	ContextKeyID       string `env:"CONTROL_PLANE_POSTGRES_CONTEXT_KEY_ID,required,notEmpty"`
	ContextKeyFile     string `env:"CONTROL_PLANE_POSTGRES_CONTEXT_KEY_FILE,required,notEmpty"`
	CurrentName        string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_CURRENT_PRINCIPAL,required,notEmpty"`
	CurrentGeneration  uint64 `env:"CONTROL_PLANE_POSTGRES_RUNTIME_CURRENT_GENERATION,required"`
	CurrentNotBefore   string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_CURRENT_NOT_BEFORE,required,notEmpty"`
	CurrentNotAfter    string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_CURRENT_NOT_AFTER,required,notEmpty"`
	NextName           string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_NEXT_PRINCIPAL"`
	NextGeneration     uint64 `env:"CONTROL_PLANE_POSTGRES_RUNTIME_NEXT_GENERATION"`
	NextNotBefore      string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_NEXT_NOT_BEFORE"`
	NextNotAfter       string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_NEXT_NOT_AFTER"`
	NextDSNFile        string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_NEXT_DSN_FILE"`
	PreviousName       string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_PREVIOUS_PRINCIPAL"`
	PreviousGeneration uint64 `env:"CONTROL_PLANE_POSTGRES_RUNTIME_PREVIOUS_GENERATION"`
	PreviousNotBefore  string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_PREVIOUS_NOT_BEFORE"`
	PreviousNotAfter   string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_PREVIOUS_NOT_AFTER"`
}

type runtimePrincipal struct {
	PrincipalName string    `json:"principal_name"`
	Generation    uint64    `json:"generation"`
	Status        string    `json:"status"`
	NotBefore     time.Time `json:"not_before"`
	NotAfter      time.Time `json:"not_after"`
}

func reconcileRuntimePrincipals(
	ctx context.Context,
	database *sql.DB,
	serverName string,
	roots *x509.CertPool,
) error {
	var config runtimePrincipalConfig
	if err := env.Parse(&config); err != nil {
		return errors.New("parse runtime principal reconciliation environment")
	}
	key, err := readRuntimeFile(config.ContextKeyFile, 128)
	if err != nil || len(key) < 32 {
		return errors.New("read bounded PostgreSQL context signing key")
	}
	principals := make([]runtimePrincipal, 0, 3)
	current, err := parseRuntimePrincipal(
		config.CurrentName,
		config.CurrentGeneration,
		"CURRENT",
		config.CurrentNotBefore,
		config.CurrentNotAfter,
	)
	if err != nil {
		return err
	}
	principals = append(principals, current)
	for _, candidate := range []struct {
		name       string
		generation uint64
		status     string
		notBefore  string
		notAfter   string
	}{
		{
			config.NextName,
			config.NextGeneration,
			"NEXT",
			config.NextNotBefore,
			config.NextNotAfter,
		},
		{
			config.PreviousName,
			config.PreviousGeneration,
			"PREVIOUS",
			config.PreviousNotBefore,
			config.PreviousNotAfter,
		},
	} {
		if candidate.name == "" && candidate.generation == 0 &&
			candidate.notBefore == "" && candidate.notAfter == "" {
			continue
		}
		parsed, parseErr := parseRuntimePrincipal(
			candidate.name,
			candidate.generation,
			candidate.status,
			candidate.notBefore,
			candidate.notAfter,
		)
		if parseErr != nil {
			return parseErr
		}
		principals = append(principals, parsed)
	}
	payload, err := json.Marshal(principals)
	if err != nil {
		return errors.New("encode runtime principal reconciliation input")
	}
	statement, err := operationalSQL.ReadFile("sql/runtime_principals__reconcile.sql")
	if err != nil {
		return errors.New("load runtime principal reconciliation SQL")
	}
	if _, err := database.ExecContext(ctx, string(statement), payload, config.ContextKeyID, key); err != nil {
		return fmt.Errorf("reconcile runtime PostgreSQL principals: %w", err)
	}
	if config.NextName == "" {
		if config.NextDSNFile != "" {
			return errors.New("NEXT PostgreSQL DSN is unexpected")
		}
		return nil
	}
	if config.NextDSNFile == "" {
		return errors.New("NEXT PostgreSQL DSN is required for readback")
	}
	nextRaw, err := readRuntimeFile(config.NextDSNFile, 64<<10)
	if err != nil {
		return errors.New("read bounded NEXT PostgreSQL DSN")
	}
	nextConfig, err := pgx.ParseConfig(strings.TrimSpace(string(nextRaw)))
	if err != nil || len(nextConfig.Fallbacks) != 0 ||
		nextConfig.Host != serverName ||
		nextConfig.TLSConfig == nil ||
		nextConfig.TLSConfig.ServerName != serverName ||
		nextConfig.TLSConfig.InsecureSkipVerify {
		return errors.New("NEXT PostgreSQL DSN must use exact verify-full TLS")
	}
	nextConfig.TLSConfig = &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: serverName,
		RootCAs:    roots,
	}
	nextDatabase := stdlib.OpenDB(*nextConfig)
	defer nextDatabase.Close()
	readbackStatement, err := operationalSQL.ReadFile(
		"sql/runtime_principal__readback.sql",
	)
	if err != nil {
		return errors.New("load NEXT PostgreSQL principal readback SQL")
	}
	var generation uint64
	if err := nextDatabase.QueryRowContext(
		ctx,
		string(readbackStatement),
	).Scan(&generation); err != nil || generation != config.NextGeneration {
		return errors.New("NEXT PostgreSQL principal readback failed")
	}
	return nil
}

func parseRuntimePrincipal(
	name string,
	generation uint64,
	status, notBeforeRaw, notAfterRaw string,
) (runtimePrincipal, error) {
	notBefore, beforeErr := time.Parse(time.RFC3339, notBeforeRaw)
	notAfter, afterErr := time.Parse(time.RFC3339, notAfterRaw)
	if name == "" || generation == 0 || beforeErr != nil || afterErr != nil ||
		!notAfter.After(notBefore) {
		return runtimePrincipal{}, errors.New("runtime principal lifecycle input is invalid")
	}
	return runtimePrincipal{
		PrincipalName: name,
		Generation:    generation,
		Status:        status,
		NotBefore:     notBefore.UTC(),
		NotAfter:      notAfter.UTC(),
	}, nil
}
