package main

import (
	"context"
	_ "embed"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed testdata/scheduler_upgrade.sql
var schedulerUpgradeFixture string

func TestScheduleProtocolUpgrade(t *testing.T) {
	dsn := os.Getenv("KODEX_CONTROL_PLANE_MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("disposable migration database is required")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid migration test configuration")
	}
	database := stdlib.OpenDB(*config)
	defer database.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20260904000300); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, schedulerUpgradeFixture); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20260904000400); err != nil {
		t.Fatal(err)
	}
	var valid bool
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SET LOCAL ROLE control_plane_owner"); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM control_plane.schedule_revisions WHERE schedule_id=(SELECT id FROM control_plane.schedules WHERE ref='sch_upgrade'))=2
		AND EXISTS(SELECT 1 FROM control_plane.schedules s JOIN control_plane.schedule_revisions r ON r.id=s.current_revision_id
		 WHERE s.ref='sch_upgrade' AND s.enabled AND r.revision=2 AND r.target_version=7
		 AND r.target_digest=encode(digest(convert_to('agt_upgrade' || chr(31) || '7','UTF8'),'sha256'),'hex')
		 AND r.automation_text='Existing automation' AND s.next_run_at IS NOT NULL)
		AND EXISTS(SELECT 1 FROM control_plane.schedule_revisions WHERE ref='srev_upgrade01' AND digest=repeat('a',64))
		AND EXISTS(SELECT 1 FROM control_plane.schedule_occurrences WHERE ref='occ_upgrade'
		 AND state='CANCELLED' AND lease_ref IS NULL AND completed_at IS NOT NULL)
	`).Scan(&valid); err != nil || !valid {
		t.Fatalf("schedule upgrade invariants: valid=%v err=%v", valid, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpContext(ctx, database, "migrations"); err != nil {
		t.Fatal(err)
	}
}
