package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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

type config struct {
	MigrationDSNFile      string `env:"INTEGRATION_GATEWAY_MIGRATION_DSN_FILE"`
	PostgresCAFile        string `env:"INTEGRATION_GATEWAY_POSTGRES_CA_FILE"`
	PostgresTLSServerName string `env:"INTEGRATION_GATEWAY_POSTGRES_TLS_SERVER_NAME"`
	ContextKeyID          string `env:"INTEGRATION_GATEWAY_POSTGRES_CONTEXT_KEY_ID"`
	ContextKeyFile        string `env:"INTEGRATION_GATEWAY_POSTGRES_CONTEXT_KEY_FILE"`
	CurrentDSNFile        string `env:"INTEGRATION_GATEWAY_POSTGRES_CURRENT_DSN_FILE"`
	CurrentGeneration     uint64 `env:"INTEGRATION_GATEWAY_POSTGRES_CURRENT_GENERATION"`
	CurrentNotBefore      string `env:"INTEGRATION_GATEWAY_POSTGRES_CURRENT_NOT_BEFORE"`
	CurrentNotAfter       string `env:"INTEGRATION_GATEWAY_POSTGRES_CURRENT_NOT_AFTER"`
	NextDSNFile           string `env:"INTEGRATION_GATEWAY_POSTGRES_NEXT_DSN_FILE"`
	NextGeneration        uint64 `env:"INTEGRATION_GATEWAY_POSTGRES_NEXT_GENERATION"`
	NextNotBefore         string `env:"INTEGRATION_GATEWAY_POSTGRES_NEXT_NOT_BEFORE"`
	NextNotAfter          string `env:"INTEGRATION_GATEWAY_POSTGRES_NEXT_NOT_AFTER"`
	PreviousDSNFile       string `env:"INTEGRATION_GATEWAY_POSTGRES_PREVIOUS_DSN_FILE"`
	PreviousGeneration    uint64 `env:"INTEGRATION_GATEWAY_POSTGRES_PREVIOUS_GENERATION"`
	PreviousNotBefore     string `env:"INTEGRATION_GATEWAY_POSTGRES_PREVIOUS_NOT_BEFORE"`
	PreviousNotAfter      string `env:"INTEGRATION_GATEWAY_POSTGRES_PREVIOUS_NOT_AFTER"`
}

type principal struct {
	Name       string    `json:"principal_name"`
	Generation uint64    `json:"generation"`
	Status     string    `json:"status"`
	NotBefore  time.Time `json:"not_before"`
	NotAfter   time.Time `json:"not_after"`
	Password   string    `json:"-"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cleanupBase := context.Background()
	if err := run(ctx, cleanupBase, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "integration-gateway CLI failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx, cleanupBase context.Context, arguments []string) error {
	if len(arguments) == 1 && arguments[0] == "image-readback" {
		if _, err := fmt.Fprintln(os.Stdout, "integration-gateway image pull verified"); err != nil {
			return err
		}
		<-ctx.Done()
		return nil
	}
	if len(arguments) != 2 || arguments[0] != "migrate" ||
		(arguments[1] != "up" && arguments[1] != "status" && arguments[1] != "version") {
		return errors.New("usage: integration-gateway-cli image-readback|migrate up|status|version")
	}
	configuration, err := loadConfig()
	if err != nil {
		return err
	}
	pgxConfig, _, err := parseDSN(configuration.MigrationDSNFile, configuration.PostgresCAFile, configuration.PostgresTLSServerName)
	if err != nil {
		return err
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
	switch arguments[1] {
	case "status":
		return goose.StatusContext(ctx, database, "migrations")
	case "version":
		version, err := goose.GetDBVersionContext(ctx, database)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(os.Stdout, "current migration version: %d\n", version)
		return err
	case "up":
		if err := goose.UpContext(ctx, database, "migrations"); err != nil {
			return errors.New("apply integration gateway migrations")
		}
		return reconcile(ctx, cleanupBase, configuration, pgxConfig)
	default:
		return errors.New("unsupported migration command")
	}
}

func loadConfig() (config, error) {
	value := config{
		MigrationDSNFile:      "/var/run/secrets/mattercodex/integration-gateway/postgres-migrator/dsn",
		PostgresCAFile:        "/var/run/config/mattercodex/integration-gateway/postgres/ca.pem",
		PostgresTLSServerName: "integration-gateway-postgresql-rw.mattercodex-system.svc.cluster.local",
		ContextKeyID:          "integration-gateway-db-context-g1",
		ContextKeyFile:        "/var/run/secrets/mattercodex/integration-gateway/postgres-context/key",
		CurrentDSNFile:        "/var/run/secrets/mattercodex/integration-gateway/postgres-runtime/dsn",
		CurrentGeneration:     1, CurrentNotBefore: "2026-01-01T00:00:00Z", CurrentNotAfter: "2027-01-01T00:00:00Z",
	}
	if err := env.Parse(&value); err != nil {
		return config{}, err
	}
	for _, path := range []string{value.MigrationDSNFile, value.PostgresCAFile, value.ContextKeyFile, value.CurrentDSNFile} {
		if !filepath.IsAbs(path) {
			return config{}, errors.New("migration runtime path is invalid")
		}
	}
	if value.ContextKeyID == "" || value.CurrentGeneration == 0 {
		return config{}, errors.New("migration generation is invalid")
	}
	return value, nil
}

func reconcile(ctx, cleanupBase context.Context, configuration config, migrationConfig *pgx.ConnConfig) (resultErr error) {
	candidates := []struct {
		file                        string
		generation                  uint64
		status, notBefore, notAfter string
	}{
		{configuration.CurrentDSNFile, configuration.CurrentGeneration, "CURRENT", configuration.CurrentNotBefore, configuration.CurrentNotAfter},
		{configuration.NextDSNFile, configuration.NextGeneration, "NEXT", configuration.NextNotBefore, configuration.NextNotAfter},
		{configuration.PreviousDSNFile, configuration.PreviousGeneration, "PREVIOUS", configuration.PreviousNotBefore, configuration.PreviousNotAfter},
	}
	principals := make([]principal, 0, 3)
	for _, candidate := range candidates {
		if candidate.file == "" && candidate.generation == 0 && candidate.notBefore == "" && candidate.notAfter == "" {
			continue
		}
		if !filepath.IsAbs(candidate.file) || candidate.generation == 0 {
			return errors.New("runtime principal configuration is invalid")
		}
		parsed, _, err := parseDSN(candidate.file, configuration.PostgresCAFile, configuration.PostgresTLSServerName)
		if err != nil {
			return err
		}
		notBefore, err := time.Parse(time.RFC3339, candidate.notBefore)
		if err != nil {
			return errors.New("runtime principal not-before is invalid")
		}
		notAfter, err := time.Parse(time.RFC3339, candidate.notAfter)
		if err != nil || !notAfter.After(notBefore) {
			return errors.New("runtime principal not-after is invalid")
		}
		expectedName := fmt.Sprintf("integration_gateway_runtime_g%d", candidate.generation)
		if parsed.User != expectedName || parsed.Password == "" || len(parsed.Password) < 32 || len(parsed.Password) > 1024 || strings.TrimSpace(parsed.Password) != parsed.Password {
			return errors.New("runtime principal identity is invalid")
		}
		principals = append(principals, principal{
			Name: parsed.User, Generation: candidate.generation,
			Status: candidate.status, NotBefore: notBefore, NotAfter: notAfter, Password: parsed.Password,
		})
	}
	key, err := readFile(configuration.ContextKeyFile, 128)
	if err != nil || len(key) < 32 {
		return errors.New("PostgreSQL context key is unavailable")
	}
	connection, err := pgx.ConnectConfig(ctx, migrationConfig)
	if err != nil {
		return errors.New("open PostgreSQL reconciliation connection")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(cleanupBase), 5*time.Second)
		defer cancel()
		resultErr = errors.Join(resultErr, connection.Close(cleanup))
	}()
	stateSQL, _ := operationalSQL.ReadFile("sql/runtime__state.sql")
	var highWatermark uint64
	var requestedCurrentStatus string
	if err := connection.QueryRow(ctx, string(stateSQL), pgx.StrictNamedArgs{
		"current_generation": configuration.CurrentGeneration,
	}).Scan(&highWatermark, &requestedCurrentStatus); err != nil {
		return errors.New("read runtime credential high-watermark")
	}
	servedGeneration := highWatermark
	if highWatermark > 0 {
		if configuration.CurrentGeneration < highWatermark ||
			(requestedCurrentStatus != "CURRENT" && requestedCurrentStatus != "NEXT") {
			return errors.New("runtime credential rollback is forbidden")
		}
		if err := readbackRuntimeIdentity(ctx, configuration, fmt.Sprintf(
			"integration_gateway_runtime_g%d", configuration.CurrentGeneration,
		)); err != nil {
			return err
		}
		servedGeneration = configuration.CurrentGeneration
	}
	tx, err := connection.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(cleanupBase), 5*time.Second)
		defer cancel()
		if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			resultErr = errors.Join(resultErr, fmt.Errorf("rollback PostgreSQL reconciliation: %w", err))
		}
	}()
	registeredSQL, _ := operationalSQL.ReadFile("sql/runtime__registered.sql")
	rows, err := tx.Query(ctx, string(registeredSQL))
	if err != nil {
		return err
	}
	var registered []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) != nil {
			rows.Close()
			return errors.New("read registered runtime principal")
		}
		registered = append(registered, name)
	}
	rows.Close()
	desired := make(map[string]struct{}, len(principals))
	bootstrapSQL, _ := operationalSQL.ReadFile("sql/runtime__bootstrap.sql")
	for _, value := range principals {
		desired[value.Name] = struct{}{}
		if _, err := tx.Exec(ctx, string(bootstrapSQL), value.Name, value.Generation, value.Password); err != nil {
			return errors.New("bootstrap runtime database role")
		}
	}
	retireSQL, _ := operationalSQL.ReadFile("sql/runtime__retire.sql")
	for _, name := range registered {
		if _, keep := desired[name]; keep {
			continue
		}
		if _, err := tx.Exec(ctx, string(retireSQL), name); err != nil {
			return errors.New("retire runtime database role")
		}
	}
	payload, err := json.Marshal(principals)
	if err != nil {
		return errors.New("encode runtime principals")
	}
	reconcileSQL, _ := operationalSQL.ReadFile("sql/runtime__reconcile.sql")
	var reconciledPrincipals, reconciledKeys, reconciledFences int
	if err := tx.QueryRow(ctx, string(reconcileSQL), pgx.StrictNamedArgs{
		"principals": payload, "context_key_id": configuration.ContextKeyID, "context_key": key,
		"current_generation": configuration.CurrentGeneration, "served_generation": servedGeneration,
	}).Scan(&reconciledPrincipals, &reconciledKeys, &reconciledFences); err != nil {
		return errors.New("reconcile runtime database identities")
	}
	if reconciledPrincipals != len(principals) || reconciledKeys != 1 || reconciledFences != 1 {
		return errors.New("runtime database identity reconciliation is not forward-only")
	}
	return tx.Commit(ctx)
}

func readbackRuntimeIdentity(ctx context.Context, configuration config, expectedName string) error {
	runtimeConfig, _, err := parseDSN(configuration.CurrentDSNFile, configuration.PostgresCAFile,
		configuration.PostgresTLSServerName)
	if err != nil {
		return err
	}
	readback, err := pgx.ConnectConfig(ctx, runtimeConfig)
	if err != nil {
		return errors.New("open runtime identity readback connection")
	}
	defer readback.Close(ctx)
	var sessionUser string
	var ready bool
	if err := readback.QueryRow(
		ctx,
		"SELECT session_user::text, integration_gateway.runtime_identity_ready()",
	).Scan(&sessionUser, &ready); err != nil || sessionUser != expectedName || !ready {
		return errors.New("runtime identity exact served-state readback failed")
	}
	return nil
}

func parseDSN(path, caFile, serverName string) (*pgx.ConnConfig, *x509.CertPool, error) {
	raw, err := readFile(path, 64<<10)
	if err != nil {
		return nil, nil, errors.New("read PostgreSQL DSN")
	}
	parsed, err := pgx.ParseConfig(strings.TrimSpace(string(raw)))
	if err != nil || parsed.Host != serverName || len(parsed.Fallbacks) != 0 || parsed.TLSConfig == nil || parsed.TLSConfig.InsecureSkipVerify {
		return nil, nil, errors.New("PostgreSQL DSN must use exact verify-full TLS")
	}
	caRaw, err := readFile(caFile, 1<<20)
	if err != nil {
		return nil, nil, errors.New("read PostgreSQL CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, nil, errors.New("parse PostgreSQL CA")
	}
	parsed.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS13, ServerName: serverName, RootCAs: roots}
	return parsed, roots, nil
}

func readFile(path string, maximum int64) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("runtime file path is not absolute")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximum || info.Mode().Perm()&0o007 != 0 {
		return nil, errors.New("runtime file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || int64(len(raw)) > maximum {
		return nil, errors.New("read bounded runtime file")
	}
	return raw, nil
}
