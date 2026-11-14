package main

import (
	"os"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestTelegramNotificationsUpgrade applies the exact predecessor and forward-only Telegram notifications migration in an isolated database, surfacing native schema failures without contacting Telegram notifications.
// TestTelegramNotificationsUpgrade применяет точный predecessor и forward-only Telegram notifications migration в отдельной БД, показывая native schema failures без обращения к Telegram notifications.
func TestTelegramNotificationsUpgrade(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261114130000); err != nil {
		t.Fatal("prepare Telegram notifications predecessor", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "telegram-notifications", "precondition")
	if _, err := observer.Exec(ctx, "GRANT SELECT ON control_plane.integration_connections TO PUBLIC"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261114140000); err == nil || !strings.Contains(err.Error(), "Telegram notifications exact predecessor catalog changed") {
		t.Fatal("drifted predecessor was accepted", err)
	}
	if _, err := observer.Exec(ctx, "REVOKE SELECT ON control_plane.integration_connections FROM PUBLIC"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261114140000); err != nil {
		t.Fatal("apply Telegram notifications", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261114140000); err != nil {
		t.Fatal("repeat Telegram notifications", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "telegram-notifications", "postcondition")
	proof, err := os.ReadFile("../../../../../tools/release/telegram-notifications-migration-postcondition.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{
		"ALTER TABLE control_plane.integration_personal_credentials DISABLE TRIGGER USER",
		"GRANT UPDATE ON control_plane.repository_bundle_publications TO kodex_backup_reader",
		"ALTER TABLE control_plane.integration_definitions ALTER COLUMN adapter SET DEFAULT 'TELEGRAM_INTERACTION'",
	} {
		assertControlPlaneCatalogMutationRejected(t, ctx, observer, proof, mutation, "Telegram notifications schema catalog changed")
	}
	if _, err := observer.Exec(ctx, "GRANT SELECT ON ALL TABLES IN SCHEMA control_plane,public TO kodex_backup_reader; ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT SELECT ON TABLES TO kodex_backup_reader"); err != nil {
		t.Fatal(err)
	}
	if _, err := observer.Exec(ctx, "CREATE VIEW public.synthetic_backup_view AS SELECT 1 AS safe_fixture"); err != nil {
		t.Fatal(err)
	}
	if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err == nil {
		t.Fatal("backup restore expanded read authority to an unknown public view")
	}
	if _, err := observer.Exec(ctx, "DROP VIEW public.synthetic_backup_view"); err != nil {
		t.Fatal(err)
	}
	for repeat := 0; repeat < 2; repeat++ {
		if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
			t.Fatal("restore exact046 backup ACL", err)
		}
	}
	readControlPlaneMigrationProof(t, ctx, observer, "telegram-notifications-backup", "postcondition")
	if err := goose.DownContext(ctx, database, "migrations"); err == nil {
		t.Fatal("forward-only Telegram notifications migration accepted Down")
	}
	var shape bool
	if err := observer.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='control_plane.integration_definitions'::regclass AND position('TELEGRAM_INTERACTION' IN pg_get_constraintdef(oid))>0) AND EXISTS(SELECT 1 FROM pg_class WHERE oid='control_plane.telegram_subscriptions'::regclass)`).Scan(&shape); err != nil || !shape {
		t.Fatal("Telegram notifications schema is incomplete", err)
	}
}
