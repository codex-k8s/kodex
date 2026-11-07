package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestPersonalIntegrationCredentialMigration checks exact033-to034 forward/repeat application,
// the closed backup catalog and purge graph, preserved admission ABI, minimal runtime ACL and
// repeatable backup SELECT restoration. Unexpected backup writes and destructive Down are
// rejected in the disposable loopback fixture; installed functional acceptance is separate.
// TestPersonalIntegrationCredentialMigration проверяет exact033-to034 forward/repeat apply,
// закрытый backup catalog и purge graph, сохранённый admission ABI, минимальный runtime ACL и
// повторное восстановление backup SELECT. Неожиданный backup WRITE и destructive Down закрыто
// отклоняются в disposable loopback fixture; installed functional acceptance выполняется отдельно.
func TestPersonalIntegrationCredentialMigration(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261105000100); err != nil {
		t.Fatal("prepare personal credential exact predecessor")
	}
	const admissionIdentity = `SELECT oid::bigint,proargnames,COALESCE(proacl::text,''),pg_get_functiondef(oid)
FROM pg_proc WHERE oid='control_plane.integration_grant_admission(uuid,uuid,uuid,text,text,text,text,text,text,text[])'::regprocedure`
	var beforeOID, afterOID int64
	var beforeArgs, afterArgs []string
	var beforeACL, afterACL, beforeDefinition, afterDefinition string
	if err := observer.QueryRow(ctx, admissionIdentity).Scan(&beforeOID, &beforeArgs, &beforeACL, &beforeDefinition); err != nil {
		t.Fatal("read grant admission predecessor identity")
	}
	if _, err := observer.Exec(ctx, "GRANT SELECT ON ALL TABLES IN SCHEMA public, control_plane TO kodex_backup_reader; ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT SELECT ON TABLES TO kodex_backup_reader"); err != nil {
		t.Fatal("prepare canonical backup defaults")
	}
	readControlPlaneMigrationProof(t, ctx, observer, "personal-credentials-backup", "precondition")
	for attempt := 0; attempt < 2; attempt++ {
		if err := goose.UpToContext(ctx, database, "migrations", 20261106000100); err != nil {
			t.Fatal("apply personal credential forward migration")
		}
	}
	if err := observer.QueryRow(ctx, admissionIdentity).Scan(&afterOID, &afterArgs, &afterACL, &afterDefinition); err != nil {
		t.Fatal("read forward grant admission identity")
	}
	beforeFragment := "WHEN NOT connection.enabled OR connection.state<>'CONNECTED'\n              OR NOT connection.credential_ready"
	afterFragment := "WHEN ((p_purpose='GRANT' OR connection.credential_policy='INITIATOR_REQUIRED') AND NOT control_plane.integration_connection_configuration_ready(connection.id))\n              OR (p_purpose='USE' AND connection.credential_policy='PROJECT_SHARED' AND (NOT connection.enabled OR connection.state<>'CONNECTED' OR NOT connection.credential_ready))"
	if beforeOID != afterOID || !slices.Equal(beforeArgs, afterArgs) || beforeACL != afterACL || strings.Count(beforeDefinition, beforeFragment) != 1 ||
		strings.Replace(beforeDefinition, beforeFragment, afterFragment, 1) != afterDefinition {
		t.Fatal("forward grant readiness changed its ABI, ACL or unrelated predecessor body")
	}
	var helperACL bool
	if err := observer.QueryRow(ctx, `SELECT has_function_privilege('control_plane_runtime','control_plane.integration_connection_configuration_ready(uuid)','EXECUTE')
AND NOT EXISTS (SELECT 1 FROM pg_proc procedure_row CROSS JOIN LATERAL aclexplode(procedure_row.proacl) acl
WHERE procedure_row.oid='control_plane.integration_connection_configuration_ready(uuid)'::regprocedure AND acl.grantee=0 AND acl.privilege_type='EXECUTE')`).Scan(&helperACL); err != nil || !helperACL {
		t.Fatal("configuration readiness helper execute ACL is invalid")
	}
	var head int64
	var minimalACL, backupReadable bool
	err := observer.QueryRow(ctx, `SELECT
        (SELECT max(version_id) FILTER (WHERE is_applied) FROM public.goose_db_version),
        has_table_privilege('control_plane_runtime','control_plane.integration_personal_credentials','SELECT')
          AND has_table_privilege('control_plane_runtime','control_plane.integration_personal_credentials','INSERT')
          AND has_table_privilege('control_plane_runtime','control_plane.integration_personal_credentials','UPDATE')
          AND NOT has_table_privilege('control_plane_runtime','control_plane.integration_personal_credentials','DELETE')
          AND has_table_privilege('control_plane_runtime','control_plane.integration_personal_credential_revisions','SELECT')
          AND has_table_privilege('control_plane_runtime','control_plane.integration_personal_credential_revisions','INSERT')
          AND NOT has_table_privilege('control_plane_runtime','control_plane.integration_personal_credential_revisions','UPDATE')
          AND NOT has_table_privilege('control_plane_runtime','control_plane.integration_personal_credential_revisions','DELETE')
          AND has_table_privilege('control_plane_runtime','control_plane.integration_personal_credential_tests','SELECT')
          AND has_table_privilege('control_plane_runtime','control_plane.integration_personal_credential_tests','INSERT')
          AND has_table_privilege('control_plane_runtime','control_plane.integration_personal_credential_tests','UPDATE')
          AND NOT has_table_privilege('control_plane_runtime','control_plane.integration_personal_credential_tests','DELETE')
          AND has_table_privilege('control_plane_runtime','control_plane.integration_personal_invocation_credentials','SELECT')
          AND has_table_privilege('control_plane_runtime','control_plane.integration_personal_invocation_credentials','INSERT')
          AND NOT has_table_privilege('control_plane_runtime','control_plane.integration_personal_invocation_credentials','UPDATE')
          AND NOT has_table_privilege('control_plane_runtime','control_plane.integration_personal_invocation_credentials','DELETE')
          AND has_table_privilege('control_plane_runtime','control_plane.integration_personal_invocation_admissions','SELECT')
          AND has_table_privilege('control_plane_runtime','control_plane.integration_personal_invocation_admissions','INSERT')
          AND NOT has_table_privilege('control_plane_runtime','control_plane.integration_personal_invocation_admissions','UPDATE')
          AND NOT has_table_privilege('control_plane_runtime','control_plane.integration_personal_invocation_admissions','DELETE'),
        has_table_privilege('kodex_backup_reader','control_plane.integration_personal_credentials','SELECT')
          AND has_table_privilege('kodex_backup_reader','control_plane.integration_personal_credential_revisions','SELECT')
          AND NOT has_table_privilege('kodex_backup_reader','control_plane.integration_personal_credentials','UPDATE')
          AND has_table_privilege('kodex_backup_reader','control_plane.integration_personal_credential_tests','SELECT')
          AND NOT has_table_privilege('kodex_backup_reader','control_plane.integration_personal_credential_tests','UPDATE')
          AND has_table_privilege('kodex_backup_reader','control_plane.integration_personal_invocation_credentials','SELECT')
          AND NOT has_table_privilege('kodex_backup_reader','control_plane.integration_personal_invocation_credentials','UPDATE')
          AND has_table_privilege('kodex_backup_reader','control_plane.integration_personal_invocation_admissions','SELECT')
          AND NOT has_table_privilege('kodex_backup_reader','control_plane.integration_personal_invocation_admissions','UPDATE')`).Scan(&head, &minimalACL, &backupReadable)
	if err != nil || head != 20261106000100 || !minimalACL || !backupReadable {
		t.Fatal("personal credential migration head or ACL readback failed")
	}
	readControlPlaneMigrationProof(t, ctx, observer, "personal-credentials-backup", "postcondition")
	for attempt := 0; attempt < 2; attempt++ {
		if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
			t.Fatal("restore exact personal backup ACL", err)
		}
	}
	readControlPlaneMigrationProof(t, ctx, observer, "personal-credentials-backup", "postcondition")
	if _, err := observer.Exec(ctx, "GRANT UPDATE ON control_plane.integration_personal_credentials TO kodex_backup_reader"); err != nil {
		t.Fatal("prepare disposable personal backup write")
	}
	if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err == nil {
		t.Fatal("personal ACL restoration accepted unexpected backup write")
	}
	var retainedRead bool
	if err := observer.QueryRow(ctx, "SELECT has_table_privilege('kodex_backup_reader','control_plane.integration_personal_credentials','SELECT')").Scan(&retainedRead); err != nil || !retainedRead {
		t.Fatal("failed personal ACL restoration removed backup SELECT", err)
	}
	if _, err := observer.Exec(ctx, "REVOKE UPDATE ON control_plane.integration_personal_credentials FROM kodex_backup_reader"); err != nil {
		t.Fatal("remove disposable personal backup write")
	}
	if err := goose.DownContext(ctx, database, "migrations"); err == nil {
		t.Fatal("personal credential migration allowed destructive Down")
	}
	if err := observer.QueryRow(ctx, "SELECT max(version_id) FILTER (WHERE is_applied) FROM public.goose_db_version").Scan(&head); err != nil || head != 20261106000100 {
		t.Fatal("failed Down changed the schema head")
	}
}
