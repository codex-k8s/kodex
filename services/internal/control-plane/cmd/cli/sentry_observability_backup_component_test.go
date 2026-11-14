package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

// TestSentryObservabilityBackupUpgrade qualifies exact canonical044→045, failed/cancelled/lost-ACK attempts, no-replay rejoin and preservation of inactive durable workspace records.
// TestSentryObservabilityBackupUpgrade проверяет exact canonical044→045, failed/cancelled/lost-ACK attempts, no-replay rejoin и сохранение неактивных durable workspace records.
func TestSentryObservabilityBackupUpgrade(t *testing.T) {
	_, database, observer := newWorkflowUpgradeDatabase(t)
	ctx, cancelTest := context.WithTimeout(t.Context(), 600*time.Second)
	defer cancelTest()
	if err := goose.UpToContext(ctx, database, "migrations", 20261114120000); err != nil {
		t.Fatal("prepare canonical Sentry predecessor", err)
	}
	for _, name := range []string{"testdata/opencode_native_cutover_seed.sql", "testdata/github_backup_retained_workspace.sql"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := observer.Exec(ctx, string(raw)); err != nil {
			t.Fatal("seed retained workspace fixture", err)
		}
	}
	digestSQL, err := os.ReadFile("testdata/sentry_observability_workspace_digest.sql")
	if err != nil {
		t.Fatal(err)
	}
	var before, after string
	if err := observer.QueryRow(ctx, string(digestSQL)).Scan(&before); err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err := executeSentryCanonicalBackupUpgrade(ctx, database, func(context.Context, *sql.DB) error { calls++; return nil }); err == nil || calls != 0 {
		t.Fatal("normal schema received implicit canonical backup authority", err)
	}
	tableOnly, err := os.ReadFile("testdata/sentry_observability_table_only_backup.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := observer.Exec(ctx, string(tableOnly)); err != nil {
		t.Fatal(err)
	}
	if err := executeSentryCanonicalBackupUpgrade(ctx, database, func(context.Context, *sql.DB) error { calls++; return nil }); err == nil || calls != 0 {
		t.Fatal("table-only qualification received canonical backup admission", err)
	}
	if _, err := observer.Exec(ctx, canonicalGitHubBackupInitializer(t)); err != nil {
		t.Fatal(err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "sentry-observability-backup", "precondition")
	precondition, err := os.ReadFile("../../../../../tools/release/sentry-observability-backup-migration-precondition.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{
		"REVOKE USAGE ON SCHEMA control_plane FROM kodex_backup_reader",
		"GRANT UPDATE ON SEQUENCE control_plane.runtime_secret_drafts_generation_seq TO kodex_backup_reader",
		"ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT UPDATE ON SEQUENCES TO kodex_backup_reader",
	} {
		assertControlPlaneCatalogMutationRejected(t, ctx, observer, precondition, mutation, "Sentry observability schema catalog changed")
	}
	for _, name := range []string{"testdata/sentry_observability_active_volume.sql", "testdata/sentry_observability_active_grant.sql"} {
		mutation, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		assertControlPlaneCatalogMutationRejected(t, ctx, observer, precondition, string(mutation), "Sentry observability workspace owner is not idle")
	}
	for _, scenario := range []string{"failed-goose", "cancelled-caller"} {
		callContext, cancel := context.WithCancel(ctx)
		failure := errors.New("synthetic cancelled migration")
		err := executeSentryCanonicalBackupUpgrade(callContext, database, func(callback context.Context, db *sql.DB) error {
			if scenario == "cancelled-caller" {
				cancel()
				return failure
			}
			return goose.UpToContext(callback, db, "missing-fixture-migrations", 20261114130000)
		})
		cancel()
		if err == nil {
			t.Fatal("failed canonical migration was reported complete")
		}
		readControlPlaneMigrationProof(t, ctx, observer, "sentry-observability-backup", "precondition")
	}
	lostACK := errors.New("synthetic Sentry migration acknowledgement lost")
	err = executeSentryCanonicalBackupUpgrade(ctx, database, func(callback context.Context, db *sql.DB) error {
		if err := goose.UpToContext(callback, db, "migrations", 20261114130000); err != nil {
			return err
		}
		return lostACK
	})
	if !errors.Is(err, lostACK) {
		t.Fatal("lost canonical migration acknowledgement was hidden", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "sentry-observability-backup", "postcondition")
	if err := executeSentryCanonicalBackupUpgrade(ctx, database, func(context.Context, *sql.DB) error { calls++; return errors.New("unexpected migration replay") }); err != nil || calls != 0 {
		t.Fatal("applied045 did not rejoin without Goose", err)
	}
	if err := applyPostgresMigrations(ctx, database, sentryBackupUpgradeCommand); err != nil {
		t.Fatal("public exact045 backup command did not rejoin", err)
	}
	for repeat := 0; repeat < 2; repeat++ {
		if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
			t.Fatal("canonical045 restore rejected retained inactive workspace", err)
		}
	}
	restore, err := workspaceBackupStatements.ReadFile("workspace_backup_sql/restore_20261114130000.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Only the test transaction frame is added; the actual shipped restore body must raise the exact active-owner failure before any GRANT.
	// Добавляется лишь test transaction frame; actual shipped restore body должен выдать точный active-owner отказ до любого GRANT.
	restoreProof := []byte("BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;\n" + strings.TrimSpace(string(restore)) + "\nROLLBACK;")
	for _, name := range []string{"testdata/sentry_observability_active_volume.sql", "testdata/sentry_observability_active_grant.sql"} {
		mutation, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		assertControlPlaneCatalogMutationRejected(t, ctx, observer, restoreProof, string(mutation), "Sentry observability workspace owner is not idle")
	}
	if err := observer.QueryRow(ctx, string(digestSQL)).Scan(&after); err != nil || before != after {
		t.Fatal("canonical045 changed retained workspace records", err)
	}
	postcondition, err := os.ReadFile("../../../../../tools/release/sentry-observability-backup-migration-postcondition.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{
		"CREATE SEQUENCE public.synthetic_foreign_sequence",
		"GRANT UPDATE ON SEQUENCE public.goose_db_version_id_seq TO kodex_backup_reader",
	} {
		assertControlPlaneCatalogMutationRejected(t, ctx, observer, postcondition, mutation, "Sentry observability public sequence boundary changed")
	}
	if err := goose.DownContext(ctx, database, "migrations"); err == nil {
		t.Fatal("canonical Sentry migration accepted Down")
	}
}
