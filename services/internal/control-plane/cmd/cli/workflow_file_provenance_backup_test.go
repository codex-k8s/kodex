package main

import "testing"

// TestWorkflowFileProvenanceBackupUpgrade runs the production commands from schema 046 to 051
// on an isolated PostgreSQL database with canonical backup grants and retained workspace data.
// It checks each release boundary, the final rejoin and preservation of the predicate identity.
// TestWorkflowFileProvenanceBackupUpgrade запускает штатные команды со схемы 046 до 051
// на изолированной БД PostgreSQL с каноническими backup grants и сохранёнными данными workspace.
// Проверяет каждую границу поставки, повторный вход на 051 и сохранность идентичности предиката.
func TestWorkflowFileProvenanceBackupUpgrade(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	retainedBefore := preparePersonalIntegrationPredecessor(t, ctx, database, observer)
	if _, err := observer.Exec(ctx, canonicalGitHubBackupInitializer(t)); err != nil {
		t.Fatal("establish canonical backup grants on schema 046", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "personal-integrations-backup", "precondition")
	if err := applyPostgresMigrations(ctx, database, personalIntegrationsBackupUpgradeCommand); err != nil {
		t.Fatal("apply the canonical backup cohort from 046 to 049", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "personal-integrations-backup", "postcondition")
	if err := applyPostgresMigrations(ctx, database, workflowFileProvenanceCommand); err == nil {
		t.Fatal("ART 050 maintenance command accepted schema049")
	}
	if err := applyPostgresMigrations(ctx, database, artifactRetentionGrantCommand); err != nil {
		t.Fatal("register the artifact-retention worker as the migrator", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "workflow-file-provenance-backup", "precondition")
	readMetadata := func() (uint32, string, string) {
		t.Helper()
		var oid uint32
		var acl, body string
		var selectAllowed, mutationAllowed, backupExecute, definer bool
		if err := observer.QueryRow(ctx, `
SELECT p.oid,coalesce(p.proacl::text,''),md5(p.prosrc),
  has_table_privilege('kodex_backup_reader','control_plane.session_workspace_storage','SELECT'),
  has_table_privilege('kodex_backup_reader','control_plane.session_workspace_storage','INSERT,UPDATE,DELETE'),
  has_function_privilege('kodex_backup_reader',p.oid,'EXECUTE'),p.prosecdef
FROM pg_proc p
WHERE p.oid='control_plane.runtime_file_source_visible_revision(uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid)'::regprocedure`).
			Scan(&oid, &acl, &body, &selectAllowed, &mutationAllowed, &backupExecute, &definer); err != nil ||
			!selectAllowed || mutationAllowed || backupExecute || definer {
			t.Fatal("workflow file predicate authority or backup reader privilege mismatch", err)
		}
		return oid, acl, body
	}
	if _, err := observer.Exec(ctx, "GRANT TRUNCATE ON control_plane.session_workspace_storage TO kodex_backup_reader"); err != nil {
		t.Fatal("inject unexpected backup authority", err)
	}
	if err := applyPostgresMigrations(ctx, database, workflowFileProvenanceCommand); err == nil {
		t.Fatal("maintenance command accepted a backup writer")
	}
	if _, err := observer.Exec(ctx, "REVOKE TRUNCATE ON control_plane.session_workspace_storage FROM kodex_backup_reader"); err != nil {
		t.Fatal("remove synthetic backup write grant", err)
	}
	if version, err := readControlPlaneSchemaHead(ctx, database); err != nil || version != artifactRetentionGrantVersion {
		t.Fatal("rejected command changed the durable head", err)
	}
	oidBefore, aclBefore, bodyBefore := readMetadata()
	if bodyBefore != "1fa42c481cd3935099281855abf46c96" {
		t.Fatal("unexpected workflow file predecessor body")
	}
	for range 2 {
		if err := applyPostgresMigrations(ctx, database, workflowFileProvenanceCommand); err != nil {
			t.Fatal("apply or rejoin schema051 with backup grants", err)
		}
	}
	oidAfter, aclAfter, bodyAfter := readMetadata()
	if oidAfter != oidBefore || aclAfter != aclBefore || bodyAfter != "a016906fbdbbf7aae09f0bc2f28dbe6f" {
		t.Fatal("workflow file migration changed predicate identity or grants")
	}
	var versions int
	if err := observer.QueryRow(ctx, "SELECT count(*) FROM public.goose_db_version WHERE version_id=$1 AND is_applied", workflowFileProvenanceVersion).Scan(&versions); err != nil || versions != 1 {
		t.Fatal("migration rejoin wrote a duplicate version", err)
	}
	tx, err := observer.Begin(ctx)
	if err != nil {
		t.Fatal("start backup reader transaction", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET TRANSACTION READ ONLY"); err != nil {
		t.Fatal("make backup reader transaction read only", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE kodex_backup_reader"); err != nil {
		t.Fatal("assume backup reader authority", err)
	}
	if _, err := tx.Exec(ctx, "SELECT 1 FROM control_plane.session_workspace_storage LIMIT 0"); err != nil {
		t.Fatal("read workspace storage with backup role", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal("finish backup reader transaction", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "workflow-file-provenance-backup", "postcondition")
	if retainedAfter := personalIntegrationRetainedWorkspaceDigest(t, ctx, observer); retainedAfter != retainedBefore {
		t.Fatal("the composed forward upgrade changed retained workspace data")
	}
}
