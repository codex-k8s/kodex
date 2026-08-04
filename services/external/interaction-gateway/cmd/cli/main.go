package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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

type config struct {
	DSNFile             string `env:"INTERACTION_GATEWAY_MIGRATION_POSTGRES_DSN_FILE,required"`
	CAFile              string `env:"INTERACTION_GATEWAY_POSTGRES_CA_FILE,required"`
	TLSServerName       string `env:"INTERACTION_GATEWAY_POSTGRES_TLS_SERVER_NAME,required"`
	PrincipalGeneration int64  `env:"INTERACTION_GATEWAY_POSTGRES_PRINCIPAL_GENERATION,required"`
	ContextKeyID        string `env:"INTERACTION_GATEWAY_POSTGRES_CONTEXT_KEY_ID,required"`
	ContextKeyFile      string `env:"INTERACTION_GATEWAY_POSTGRES_CONTEXT_KEY_FILE,required"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "interaction-gateway CLI failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string) error {
	if len(arguments) != 2 || arguments[0] != "migrate" ||
		(arguments[1] != "expand" && arguments[1] != "up" && arguments[1] != "status" && arguments[1] != "version") {
		return errors.New("usage: interaction-gateway-cli migrate expand|up|status|version")
	}
	var value config
	if err := env.ParseWithOptions(&value, env.Options{}); err != nil {
		return errors.New("parse interaction-gateway migration configuration")
	}
	dsn, err := readFile(value.DSNFile, 64<<10)
	if err != nil {
		return errors.New("read PostgreSQL migration DSN")
	}
	parsed, err := pgx.ParseConfig(strings.TrimSpace(string(dsn)))
	if err != nil || len(parsed.Fallbacks) != 0 || parsed.Host != value.TLSServerName ||
		parsed.TLSConfig == nil || parsed.TLSConfig.ServerName != value.TLSServerName || parsed.TLSConfig.InsecureSkipVerify {
		return errors.New("postgresql migration DSN must use exact verify-full TLS")
	}
	caRaw, err := readFile(value.CAFile, 1<<20)
	if err != nil {
		return errors.New("read PostgreSQL migration CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return errors.New("parse PostgreSQL migration CA")
	}
	parsed.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS13, ServerName: value.TLSServerName, RootCAs: roots}
	database := stdlib.OpenDB(*parsed)
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		return errors.New("check PostgreSQL migration connection")
	}
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return errors.New("configure PostgreSQL migration dialect")
	}
	switch arguments[1] {
	case "expand", "up":
		if err := goose.UpContext(ctx, database, "migrations"); err != nil {
			return err
		}
		contextKey, err := readFile(value.ContextKeyFile, 128)
		if err != nil || len(contextKey) < 32 || value.PrincipalGeneration <= 0 || value.ContextKeyID == "" {
			return errors.New("postgresql runtime identity material is invalid")
		}
		if _, err := database.ExecContext(ctx,
			"SELECT interaction_gateway_reconcile_runtime_identity($1, $2, $3::bytea)",
			value.PrincipalGeneration, value.ContextKeyID, contextKey); err != nil {
			return errors.New("reconcile PostgreSQL runtime identity")
		}
		var generation int64
		var keyID string
		if err := database.QueryRowContext(ctx,
			"SELECT served_generation, context_key_id FROM interaction_gateway_runtime_credential_fence WHERE singleton",
		).Scan(&generation, &keyID); err != nil || generation != value.PrincipalGeneration || keyID != value.ContextKeyID {
			return errors.New("read back PostgreSQL runtime identity")
		}
		return nil
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

func readFile(path string, maximum int64) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("runtime file path must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximum || info.Mode().Perm()&0o037 != 0 {
		return nil, errors.New("runtime file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || int64(len(raw)) > maximum {
		return nil, errors.New("read bounded runtime file")
	}
	return raw, nil
}
