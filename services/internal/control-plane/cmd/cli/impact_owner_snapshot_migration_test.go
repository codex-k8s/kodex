package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed testdata/impact_owner_snapshot_upgrade_seed.sql
var impactOwnerSnapshotUpgradeSeed string

//go:embed testdata/impact_owner_snapshot_upgrade_assert.sql
var impactOwnerSnapshotUpgradeAssert string

//go:embed testdata/impact_owner_snapshot_upgrade_probe.sql
var impactOwnerSnapshotUpgradeProbe string

// TestImpactOwnerSnapshotProtocolUpgrade проверяет реальную старую схему, а не
// изменение protocol revision существующей новой строки в тесте.
func TestImpactOwnerSnapshotProtocolUpgrade(t *testing.T) {
	dsn := os.Getenv("KODEX_IMPACT_OWNER_SNAPSHOT_UPGRADE_DSN")
	if dsn == "" {
		t.Skip("dedicated disposable migration database is required")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil || config.Host != "127.0.0.1" || config.Port < 1024 ||
		config.Database != "control_plane_impact_upgrade" || config.User != "control_plane_migrator" ||
		config.Password != "" || config.TLSConfig != nil || len(config.Fallbacks) != 0 ||
		os.Getenv("KODEX_IMPACT_OWNER_SNAPSHOT_UPGRADE_DISPOSABLE") != "dedicated-loopback-container" {
		t.Fatal("dedicated disposable migration configuration is invalid")
	}
	database := stdlib.OpenDB(*config)
	database.SetMaxOpenConns(1)
	defer database.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	goose.SetBaseFS(migrations)
	if err = goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err = goose.UpToContext(ctx, database, "migrations", 20261003000900); err != nil {
		t.Fatal(err)
	}
	if _, err = database.ExecContext(ctx, impactOwnerSnapshotUpgradeSeed); err != nil {
		t.Fatalf("seed previous schema: %v", err)
	}
	if err = goose.UpToContext(ctx, database, "migrations", 20261004000100); err != nil {
		t.Fatal(err)
	}
	if _, err = database.ExecContext(ctx, impactOwnerSnapshotUpgradeAssert); err != nil {
		t.Fatalf("forward upgrade invariants: %v", err)
	}
	testHistoricalImpactOwnerQueries(t, ctx, config)
	if err = goose.UpContext(ctx, database, "migrations"); err != nil {
		t.Fatal(err)
	}
	if _, err = database.ExecContext(ctx, impactOwnerSnapshotUpgradeAssert); err != nil {
		t.Fatalf("repeat up invariants: %v", err)
	}
	testHistoricalImpactOwnerQueries(t, ctx, config)
	version, err := goose.GetDBVersionContext(ctx, database)
	if err != nil || version != 20261004000100 {
		t.Fatalf("unexpected migration version: %d, error=%v", version, err)
	}
}

// Это доказательство SQL-входа владельца, не RPC или выдачи полномочий.
// Читаем реальные queries adapter, не копируем предикат версии в тест.
func testHistoricalImpactOwnerQueries(t *testing.T, ctx context.Context, config *pgx.ConnConfig) {
	t.Helper()
	connection, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatal("connect dedicated owner query fixture")
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := connection.Close(shutdown); err != nil {
			t.Error("close dedicated owner query fixture")
		}
	}()
	tx, err := connection.Begin(ctx)
	if err != nil {
		t.Fatal("begin owner query fixture")
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := tx.Rollback(shutdown); err != nil {
			t.Error("rollback owner query fixture")
		}
	}()
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE control_plane_owner"); err != nil {
		t.Fatal("activate disposable owner role")
	}
	if _, err = tx.Exec(ctx, impactOwnerSnapshotUpgradeProbe); err != nil {
		t.Fatal("insert explicit current SQL query controls")
	}
	queries := []struct {
		name, prefix, selector string
	}{
		{"revision_impact__get", "rvip_", "ref"},
		{"role_image_impact__get", "riip_", "plan_ref"},
		{"secret_draft_impact_get", "sdip_", "plan_ref"},
	}
	for _, query := range queries {
		raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "repository", "postgres", "platform", "sql", query.name+".sql"))
		if err != nil {
			t.Fatal("read actual owner query")
		}
		currentArguments := pgx.StrictNamedArgs{
			"organization_id": "70000000-0000-4000-8000-000000000001",
			"actor_id":        "70000000-0000-4000-8000-000000000002",
			query.selector:    query.prefix + "current_v2",
		}
		if query.name == "secret_draft_impact_get" {
			currentArguments["idempotency_key"], currentArguments["operation_id"] = "", ""
		}
		assertImpactOwnerQueryCardinality(t, ctx, tx, string(raw), currentArguments, 1)
		for position := 1; position <= 4; position++ {
			arguments := pgx.StrictNamedArgs{
				"organization_id": "70000000-0000-4000-8000-000000000001",
				"actor_id":        "70000000-0000-4000-8000-000000000002",
				query.selector:    fmt.Sprintf("%supgrade_%d", query.prefix, position),
			}
			if query.name == "secret_draft_impact_get" {
				arguments["idempotency_key"], arguments["operation_id"] = "", ""
			}
			assertHistoricalImpactQueryHidden(t, ctx, tx, string(raw), arguments)
			if query.name == "secret_draft_impact_get" {
				arguments["plan_ref"] = ""
				arguments["idempotency_key"] = fmt.Sprintf("secret-upgrade_%d", position)
				assertHistoricalImpactQueryHidden(t, ctx, tx, string(raw), arguments)
			}
		}
		if query.name == "secret_draft_impact_get" {
			assertHistoricalImpactQueryHidden(t, ctx, tx, string(raw), pgx.StrictNamedArgs{
				"organization_id": "70000000-0000-4000-8000-000000000001",
				"actor_id":        "70000000-0000-4000-8000-000000000002",
				"plan_ref":        "", "idempotency_key": "",
				"operation_id": "70000000-0000-4000-8000-000000000004",
			})
		}
	}
}

func assertHistoricalImpactQueryHidden(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, arguments pgx.StrictNamedArgs) {
	t.Helper()
	assertImpactOwnerQueryCardinality(t, ctx, tx, sql, arguments, 0)
}

func assertImpactOwnerQueryCardinality(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, arguments pgx.StrictNamedArgs, expected int) {
	t.Helper()
	rows, err := tx.Query(ctx, sql, arguments)
	if err != nil {
		t.Fatal("execute actual owner query")
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal("read actual owner query")
	}
	if count != expected {
		t.Fatalf("actual owner query cardinality mismatch: got %d, expected %d", count, expected)
	}
}
