package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// impactOwnerSnapshotUpgradeSeed embeds historical synthetic impact snapshots to exercise a real forward schema migration.
// impactOwnerSnapshotUpgradeSeed встраивает исторические синтетические impact snapshots для проверки настоящей forward schema migration.
//
//go:embed testdata/impact_owner_snapshot_upgrade_seed.sql
var impactOwnerSnapshotUpgradeSeed string

// impactOwnerSnapshotUpgradeAssert embeds the expected owner snapshot invariants checked after initial and repeated migration up.
// impactOwnerSnapshotUpgradeAssert встраивает ожидаемые owner snapshot invariants после начального и повторного migration up.
//
//go:embed testdata/impact_owner_snapshot_upgrade_assert.sql
var impactOwnerSnapshotUpgradeAssert string

// impactOwnerSnapshotUpgradeProbe embeds current-protocol controls used to prove real owner SQL distinguishes current and historical snapshots.
// impactOwnerSnapshotUpgradeProbe встраивает current-protocol controls для проверки различения текущих и исторических snapshots настоящим owner SQL.
//
//go:embed testdata/impact_owner_snapshot_upgrade_probe.sql
var impactOwnerSnapshotUpgradeProbe string

// TestImpactOwnerSnapshotProtocolUpgrade upgrades an actual gated historical disposable schema,
// verifies forward/repeated-up invariants through real owner queries and checks the latest
// embedded migration version.
// TestImpactOwnerSnapshotProtocolUpgrade обновляет настоящую явно разрешённую историческую
// disposable schema, проверяет forward и repeated-up invariants реальными owner queries и сверяет
// последнюю embedded migration version.
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
	if err != nil || version != latestEmbeddedImpactUpgradeMigration(t) {
		t.Fatalf("unexpected migration version: %d, error=%v", version, err)
	}
}

// latestEmbeddedImpactUpgradeMigration validates embedded migration filenames and returns the
// greatest positive version, requiring the impact protocol upgrade to be present.
// latestEmbeddedImpactUpgradeMigration проверяет имена embedded migration files и возвращает
// наибольшую положительную version, требуя наличие impact protocol upgrade.
func latestEmbeddedImpactUpgradeMigration(t *testing.T) int64 {
	t.Helper()
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal("read embedded migration registry")
	}
	var latest int64
	for _, entry := range entries {
		prefix, _, found := strings.Cut(entry.Name(), "_")
		version, err := strconv.ParseInt(prefix, 10, 64)
		if entry.IsDir() || !found || err != nil || version <= 0 || !strings.HasSuffix(entry.Name(), ".sql") {
			t.Fatal("embedded migration registry is invalid")
		}
		if version > latest {
			latest = version
		}
	}
	if latest < 20261004000100 {
		t.Fatal("impact owner upgrade migration is unavailable")
	}
	return latest
}

// testHistoricalImpactOwnerQueries runs actual adapter SQL under the disposable owner role to
// distinguish current and hidden historical snapshots, with independent bounded rollback/close;
// this is not RPC authorization proof.
// testHistoricalImpactOwnerQueries исполняет настоящий adapter SQL под disposable owner role для
// различения текущих и скрытых исторических snapshots с независимыми ограниченными rollback и
// close; это не доказательство RPC authorization.
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

// assertHistoricalImpactQueryHidden requires the actual owner query to return zero rows for one
// historical protocol fixture.
// assertHistoricalImpactQueryHidden требует нулевую выборку настоящего owner query для одной
// исторической protocol fixture.
func assertHistoricalImpactQueryHidden(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, arguments pgx.StrictNamedArgs) {
	t.Helper()
	assertImpactOwnerQueryCardinality(t, ctx, tx, sql, arguments, 0)
}

// assertImpactOwnerQueryCardinality executes the supplied actual query with strict named
// arguments, counts rows and checks iteration failure before comparing exact cardinality.
// assertImpactOwnerQueryCardinality исполняет переданный настоящий query со strict named
// arguments, учитывает строки и проверяет iteration failure до сравнения точной cardinality.
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
