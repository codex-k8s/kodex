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

	"github.com/codex-k8s/matter-codex/libs/go/eventing/natsjetstream"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/schema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed sql/*.sql
var operationalSQL embed.FS

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
	if len(arguments) == 1 && arguments[0] == "image-readback" {
		if _, err := fmt.Fprintln(os.Stdout, "control-plane image pull verified"); err != nil {
			return err
		}
		<-ctx.Done()
		return nil
	}
	if len(arguments) == 2 && arguments[0] == "broker" && arguments[1] == "bootstrap" {
		return bootstrapBroker(ctx)
	}
	if len(arguments) != 2 || arguments[0] != "migrate" {
		return errors.New("usage: control-plane-cli image-readback|broker bootstrap|migrate expand|up|status|version")
	}
	action := arguments[1]
	if action != "expand" && action != "up" &&
		action != "status" && action != "version" {
		return errors.New("usage: control-plane-cli migrate expand|up|status|version")
	}
	config, err := loadMigrationConfig()
	if err != nil {
		return err
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
	goose.SetTableName("public.goose_db_version")
	if err := goose.SetDialect("postgres"); err != nil {
		return errors.New("configure PostgreSQL migration dialect")
	}
	switch action {
	case "expand":
		if err := migrateExpand(
			ctx,
			database,
		); err != nil {
			return err
		}
		return reconcileMigrationState(ctx, database, config, roots)
	case "up":
		if err := migrateUp(
			ctx,
			database,
		); err != nil {
			return err
		}
		return reconcileMigrationState(ctx, database, config, roots)
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

func bootstrapBroker(ctx context.Context) error {
	config, err := loadBrokerConfig()
	if err != nil {
		return err
	}
	publisher, err := natsjetstream.New(natsjetstream.Config{
		URL:             config.URL,
		TLSServerName:   config.TLSServerName,
		CAFile:          config.CAFile,
		CertificateFile: config.CertificateFile,
		PrivateKeyFile:  config.PrivateKeyFile,
		CredentialsFile: config.CredentialsFile,
		Stream:          config.Stream,
		Subjects: []string{
			"control_plane.runtime_configuration_changed",
		},
		Replicas:        config.Replicas,
		MaxMessageBytes: 256 << 10,
		MaxMessages:     10_000_000,
		MaxBytes:        config.MaxBytes,
		MaxPerSubject:   5_000_000,
		MaxAge:          30 * 24 * time.Hour,
		DuplicateWindow: 2 * time.Minute,
		ConnectTimeout:  config.ConnectTimeout,
	})
	if err != nil {
		return err
	}
	defer publisher.Close()
	if err := publisher.EnsureStream(ctx); err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, "control-plane broker stream verified")
	return err
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
) error {
	if err := goose.UpToContext(
		ctx,
		database,
		"migrations",
		schema.CurrentVersion,
	); err != nil {
		return fmt.Errorf("apply control-plane expand migration: %w", err)
	}
	return nil
}

func migrateUp(
	ctx context.Context,
	database *sql.DB,
) error {
	version, err := goose.GetDBVersionContext(ctx, database)
	if err != nil {
		return err
	}
	if version < schema.CurrentVersion {
		return errors.New("expand migration must complete before up")
	}
	if err := goose.UpContext(ctx, database, "migrations"); err != nil {
		return fmt.Errorf("apply control-plane migrations: %w", err)
	}
	return nil
}

type migrationConnection interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func reconcileMigrationState(
	ctx context.Context,
	database *sql.DB,
	config migrationConfig,
	roots *x509.CertPool,
) error {
	connection, err := database.Conn(ctx)
	if err != nil {
		return errors.New("acquire control-plane migration connection")
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "SET ROLE control_plane_role_controller"); err != nil {
		return errors.New("assume control-plane role controller")
	}
	if err := reconcileRuntimePrincipals(
		ctx,
		connection,
		config.TLSServerName,
		roots,
	); err != nil {
		return err
	}
	if _, err := connection.ExecContext(ctx, "SET ROLE control_plane_owner"); err != nil {
		return errors.New("assume control-plane migration owner role")
	}
	return reconcileKeysetGeneses(ctx, connection, config)
}

func reconcileRuntimePrincipals(
	ctx context.Context,
	database migrationConnection,
	serverName string,
	roots *x509.CertPool,
) error {
	config, err := loadRuntimePrincipalConfig()
	if err != nil {
		return err
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
	principalFiles := map[string]string{
		config.CurrentName: config.CurrentDSNFile,
	}
	if config.NextName != "" {
		if config.NextDSNFile == "" {
			return errors.New("NEXT PostgreSQL DSN is required")
		}
		principalFiles[config.NextName] = config.NextDSNFile
	}
	if config.PreviousName != "" {
		if config.PreviousDSNFile == "" {
			return errors.New("PREVIOUS PostgreSQL DSN is required")
		}
		principalFiles[config.PreviousName] = config.PreviousDSNFile
	}
	bootstrapStatement, err := operationalSQL.ReadFile(
		"sql/runtime_principal__bootstrap.sql",
	)
	if err != nil {
		return errors.New("load runtime principal bootstrap SQL")
	}
	for _, principal := range principals {
		principalConfig, parseErr := parseRuntimePrincipalDSN(
			principalFiles[principal.PrincipalName],
			principal.PrincipalName,
			serverName,
			roots,
		)
		if parseErr != nil {
			return parseErr
		}
		if _, execErr := database.ExecContext(
			ctx,
			string(bootstrapStatement),
			principal.PrincipalName,
			principal.Generation,
			principalConfig.Password,
		); execErr != nil {
			return fmt.Errorf("bootstrap runtime PostgreSQL principal: %w", execErr)
		}
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

func parseRuntimePrincipalDSN(
	path string,
	expectedPrincipal string,
	serverName string,
	roots *x509.CertPool,
) (*pgx.ConnConfig, error) {
	raw, err := readRuntimeFile(path, 64<<10)
	if err != nil {
		return nil, errors.New("read bounded runtime PostgreSQL DSN")
	}
	config, err := pgx.ParseConfig(strings.TrimSpace(string(raw)))
	if err != nil || len(config.Fallbacks) != 0 ||
		config.User != expectedPrincipal || config.Password == "" ||
		config.Host != serverName || config.TLSConfig == nil ||
		config.TLSConfig.ServerName != serverName ||
		config.TLSConfig.InsecureSkipVerify {
		return nil, errors.New("runtime PostgreSQL DSN must use exact principal and verify-full TLS")
	}
	config.TLSConfig = &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: serverName,
		RootCAs:    roots,
	}
	return config, nil
}
