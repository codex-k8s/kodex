package main

import (
	"os"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestPlatformOIDCIdentityMigration qualifies forward033 on a disposable exact032 database
// with the shipped backup default ACL. Full catalog proofs and repeated ACL restoration
// reject unauthorized grants, missing reads, sequence drift and destructive Goose Down.
// TestPlatformOIDCIdentityMigration проверяет forward033 на одноразовой exact032 БД
// со штатными backup default ACL. Полный catalog proof и повторное восстановление ACL
// отклоняют лишние grants, отсутствие чтения, drift sequence и destructive Goose Down.
func TestPlatformOIDCIdentityMigration(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261030000100); err != nil {
		t.Fatal("prepare platform OIDC exact predecessor", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "platform-oidc-identity", "precondition")
	if _, err := observer.Exec(ctx, "GRANT SELECT ON ALL TABLES IN SCHEMA public, control_plane TO kodex_backup_reader; ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT SELECT ON TABLES TO kodex_backup_reader"); err != nil {
		t.Fatal("prepare canonical backup SELECT before platform OIDC upgrade", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "platform-oidc-identity-backup", "precondition")
	if err := goose.UpToContext(ctx, database, "migrations", 20261105000100); err != nil {
		t.Fatal("apply platform OIDC forward migration", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "platform-oidc-identity-backup", "postcondition")
	for attempt := 0; attempt < 2; attempt++ {
		if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
			t.Fatal("restore platform OIDC backup ACL", err)
		}
		readControlPlaneMigrationProof(t, ctx, observer, "platform-oidc-identity-backup", "postcondition")
	}
	proof, err := os.ReadFile("../../../../../tools/release/platform-oidc-identity-backup-migration-postcondition.sql")
	if err != nil {
		t.Fatal("read platform OIDC backup postcondition", err)
	}
	for _, mutation := range []string{
		"GRANT UPDATE ON control_plane.oidc_identity_sessions TO kodex_backup_reader",
		"GRANT UPDATE (state) ON control_plane.oidc_identity_sessions TO kodex_backup_reader",
		"REVOKE SELECT ON control_plane.oidc_identity_observations FROM kodex_backup_reader",
		"GRANT SELECT ON SEQUENCE control_plane.oidc_identity_session_revisions TO kodex_backup_reader",
		"ALTER SEQUENCE control_plane.oidc_identity_session_revisions CYCLE",
		"ALTER TABLE control_plane.oidc_identity_sessions DISABLE TRIGGER USER",
	} {
		assertControlPlaneCatalogMutationRejected(t, ctx, observer, proof, mutation, "platform OIDC identity schema catalog changed")
	}
	if _, err := observer.Exec(ctx, "GRANT UPDATE ON control_plane.oidc_identity_sessions TO kodex_backup_reader"); err != nil {
		t.Fatal("prepare unexpected backup write", err)
	}
	if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err == nil {
		t.Fatal("platform OIDC ACL restoration accepted backup write")
	}
	var readable bool
	if err := observer.QueryRow(ctx, "SELECT has_table_privilege('kodex_backup_reader','control_plane.oidc_identity_sessions','SELECT')").Scan(&readable); err != nil || !readable {
		t.Fatal("failed platform OIDC ACL restoration lost backup SELECT", err)
	}
	if _, err := observer.Exec(ctx, "REVOKE UPDATE ON control_plane.oidc_identity_sessions FROM kodex_backup_reader"); err != nil {
		t.Fatal("remove disposable backup write counterfactual", err)
	}
	if err := goose.DownContext(ctx, database, "migrations"); err == nil {
		t.Fatal("platform OIDC migration unexpectedly allowed Down")
	}
	readControlPlaneMigrationProof(t, ctx, observer, "platform-oidc-identity-backup", "postcondition")
}
