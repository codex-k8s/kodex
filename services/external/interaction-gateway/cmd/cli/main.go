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
	"slices"
	"strings"
	"syscall"

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
	DSNFile                     string `env:"INTERACTION_GATEWAY_MIGRATION_POSTGRES_DSN_FILE,required"`
	CAFile                      string `env:"INTERACTION_GATEWAY_POSTGRES_CA_FILE,required"`
	TLSServerName               string `env:"INTERACTION_GATEWAY_POSTGRES_TLS_SERVER_NAME,required"`
	PrincipalGeneration         int64  `env:"INTERACTION_GATEWAY_POSTGRES_PRINCIPAL_GENERATION,required"`
	PreviousPrincipalGeneration int64  `env:"INTERACTION_GATEWAY_POSTGRES_PREVIOUS_PRINCIPAL_GENERATION"`
	MappingManifestFile         string `env:"INTERACTION_GATEWAY_MATTERMOST_MAPPING_MANIFEST_FILE,required"`
	KeysetGenesisEnabled        bool   `env:"INTERACTION_GATEWAY_KEYSET_GENESIS_ENABLED"`
	ReadbackPublicKeysetFile    string `env:"INTERACTION_GATEWAY_READBACK_PUBLIC_KEYSET_FILE"`
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
	goose.SetTableName("public.goose_db_version")
	if err := goose.SetDialect("postgres"); err != nil {
		return errors.New("configure PostgreSQL migration dialect")
	}
	switch arguments[1] {
	case "expand", "up":
		if err := goose.UpContext(ctx, database, "migrations"); err != nil {
			return err
		}
		connection, err := database.Conn(ctx)
		if err != nil {
			return errors.New("acquire interaction gateway migration connection")
		}
		defer connection.Close()
		if _, err := connection.ExecContext(ctx, "SET ROLE interaction_gateway_migrator"); err != nil {
			return errors.New("assume interaction gateway migration role")
		}
		if err := reconcileTenantPrincipal(ctx, connection, value); err != nil {
			return err
		}
		return reconcileReadbackKeysetGenesis(ctx, connection, value)
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

type migrationConnection interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func reconcileTenantPrincipal(ctx context.Context, database migrationConnection, value config) error {
	raw, err := readFile(value.MappingManifestFile, 1<<20)
	if err != nil {
		return errors.New("read Mattermost mapping for PostgreSQL principal")
	}
	var manifest struct {
		Channels []struct {
			OrganizationID string `json:"organization_id"`
			ProjectID      string `json:"project_id"`
		} `json:"channels"`
	}
	if json.Unmarshal(raw, &manifest) != nil || len(manifest.Channels) == 0 || value.PrincipalGeneration <= 0 {
		return errors.New("PostgreSQL principal mapping is invalid")
	}
	organizationID := manifest.Channels[0].OrganizationID
	projects := map[string]struct{}{}
	for _, channel := range manifest.Channels {
		if channel.OrganizationID != organizationID || channel.ProjectID == "" {
			return errors.New("PostgreSQL principal must own exactly one organization")
		}
		projects[channel.ProjectID] = struct{}{}
	}
	projectIDs := make([]string, 0, len(projects))
	for projectID := range projects {
		projectIDs = append(projectIDs, projectID)
	}
	slices.Sort(projectIDs)
	projectJSON, err := json.Marshal(projectIDs)
	if err != nil {
		return errors.New("encode PostgreSQL principal project mapping")
	}
	var currentGeneration int64
	highWatermarkSQL, err := operationalSQL.ReadFile("sql/tenant_principal__high_watermark.sql")
	if err != nil {
		return errors.New("load PostgreSQL principal high-watermark query")
	}
	currentErr := database.QueryRowContext(ctx, string(highWatermarkSQL)).Scan(&currentGeneration)
	if currentErr == nil && currentGeneration == value.PrincipalGeneration {
		if readbackTenantPrincipal(ctx, database, value.PrincipalGeneration, organizationID, projectIDs) == nil {
			return reconcileRetiredTenantPrincipal(ctx, database, value)
		}
	}
	if currentErr != nil && !errors.Is(currentErr, sql.ErrNoRows) {
		return errors.New("read PostgreSQL principal high-watermark")
	}
	stageSQL, err := operationalSQL.ReadFile("sql/tenant_principal__stage.sql")
	if err != nil {
		return errors.New("load PostgreSQL principal stage command")
	}
	if _, err := database.ExecContext(ctx, string(stageSQL),
		value.PrincipalGeneration, organizationID, projectJSON); err != nil {
		return errors.New("stage PostgreSQL tenant principal")
	}
	promoteSQL, err := operationalSQL.ReadFile("sql/tenant_principal__promote.sql")
	if err != nil {
		return errors.New("load PostgreSQL principal promote command")
	}
	if currentErr != nil || currentGeneration != value.PrincipalGeneration {
		if _, err := database.ExecContext(ctx, string(promoteSQL), value.PrincipalGeneration); err != nil {
			return errors.New("promote PostgreSQL tenant principal")
		}
	}
	if err := readbackTenantPrincipal(ctx, database, value.PrincipalGeneration, organizationID, projectIDs); err != nil {
		return err
	}
	return reconcileRetiredTenantPrincipal(ctx, database, value)
}

func reconcileRetiredTenantPrincipal(ctx context.Context, database migrationConnection, value config) error {
	if value.PreviousPrincipalGeneration == 0 {
		return nil
	}
	if value.PreviousPrincipalGeneration < 0 || value.PreviousPrincipalGeneration >= value.PrincipalGeneration {
		return errors.New("PostgreSQL previous principal generation is invalid")
	}
	statement, err := operationalSQL.ReadFile("sql/tenant_principal__retire.sql")
	if err != nil {
		return errors.New("load PostgreSQL principal retire command")
	}
	if _, err := database.ExecContext(ctx, string(statement), value.PreviousPrincipalGeneration); err != nil {
		return errors.New("retire PostgreSQL tenant principal")
	}
	readback, err := operationalSQL.ReadFile("sql/tenant_principal__retire_readback.sql")
	if err != nil {
		return errors.New("load PostgreSQL principal retire readback")
	}
	var status string
	var canLogin, member bool
	var activeSessions int64
	if err := database.QueryRowContext(ctx, string(readback), value.PreviousPrincipalGeneration).Scan(
		&status, &canLogin, &member, &activeSessions); err != nil || status != "RETIRED" || canLogin || member || activeSessions != 0 {
		return errors.New("PostgreSQL tenant principal retirement readback failed")
	}
	return nil
}

func readbackTenantPrincipal(ctx context.Context, database migrationConnection, generation int64,
	organizationID string, projectIDs []string) error {
	readbackSQL, err := operationalSQL.ReadFile("sql/tenant_principal__readback.sql")
	if err != nil {
		return errors.New("load PostgreSQL tenant principal readback query")
	}
	rows, err := database.QueryContext(ctx, string(readbackSQL), generation)
	if err != nil {
		return errors.New("read back PostgreSQL tenant principal")
	}
	defer rows.Close()
	readProjects := make([]string, 0, len(projectIDs))
	for rows.Next() {
		var readOrganizationID, projectID string
		if rows.Scan(&readOrganizationID, &projectID) != nil || readOrganizationID != organizationID {
			return errors.New("PostgreSQL tenant principal readback mismatch")
		}
		readProjects = append(readProjects, projectID)
	}
	if err := rows.Err(); err != nil || !slices.Equal(readProjects, projectIDs) {
		return errors.New("PostgreSQL tenant principal project readback mismatch")
	}
	return nil
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
