package main

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pressly/goose/v3"
)

// TestJiraCreateFieldsMigration executes exact034→035 once and repeats Up without replay, proving
// both complete catalogs, preserved function identity/ABI/ACL and repeatable backup restoration.
// A frozen034 reader and a forged backup write must reject actual035 rather than broaden admission.
// TestJiraCreateFieldsMigration исполняет exact034→035 и повторяет Up без replay, проверяя полные
// каталоги, function identity/ABI/ACL и backup restore. Старый reader и лишний backup WRITE запрещены.
func TestJiraCreateFieldsMigration(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261106000100); err != nil {
		t.Fatal("prepare exact Jira create fields predecessor")
	}
	readControlPlaneMigrationProof(t, ctx, observer, "jira-create-fields", "precondition")
	const identity = `SELECT oid::bigint,proargnames,COALESCE(proacl::text,''),md5(prosrc) FROM pg_proc WHERE oid='control_plane.integration_grant_admission(uuid,uuid,uuid,text,text,text,text,text,text,text[])'::regprocedure`
	var beforeOID, afterOID int64
	var beforeArgs, afterArgs []string
	var beforeACL, afterACL, beforeMD5, afterMD5 string
	if observer.QueryRow(ctx, identity).Scan(&beforeOID, &beforeArgs, &beforeACL, &beforeMD5) != nil {
		t.Fatal("read Jira admission predecessor ABI")
	}
	for attempt := 0; attempt < 2; attempt++ {
		if goose.UpToContext(ctx, database, "migrations", 20261107000100) != nil {
			t.Fatal("apply or repeat Jira create fields migration")
		}
	}
	if observer.QueryRow(ctx, identity).Scan(&afterOID, &afterArgs, &afterACL, &afterMD5) != nil {
		t.Fatal("read Jira admission successor ABI")
	}
	if beforeOID != afterOID || !slices.Equal(beforeArgs, afterArgs) || beforeACL != afterACL || beforeMD5 == afterMD5 {
		t.Fatal("Jira admission migration changed unrelated identity or failed to change body")
	}
	readControlPlaneMigrationProof(t, ctx, observer, "jira-create-fields", "postcondition")
	old, err := os.ReadFile("../../../../../tools/release/personal-credentials-migration-postcondition.sql")
	if err != nil {
		t.Fatal(err)
	}
	_, err = observer.Exec(ctx, string(old))
	var rejected *pgconn.PgError
	if !errors.As(err, &rejected) || rejected.Code != "P0001" || !strings.Contains(rejected.Message, "schema catalog changed") {
		t.Fatal("frozen034 verifier did not reject actual035 catalog")
	}
	if _, err := observer.Exec(ctx, "ROLLBACK"); err != nil {
		t.Fatal("close rejected historical proof")
	}
	for attempt := 0; attempt < 2; attempt++ {
		if applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand) != nil {
			t.Fatal("restore exact035 backup ACL")
		}
	}
	readControlPlaneMigrationProof(t, ctx, observer, "jira-create-fields-backup", "postcondition")
	if _, err := observer.Exec(ctx, "GRANT UPDATE ON control_plane.integration_personal_credentials TO kodex_backup_reader"); err != nil {
		t.Fatal("prepare disposable backup counterfactual")
	}
	if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err == nil {
		t.Fatal("035 restoration accepted backup WRITE")
	}
	if _, err := observer.Exec(ctx, "REVOKE UPDATE ON control_plane.integration_personal_credentials FROM kodex_backup_reader"); err != nil {
		t.Fatal("remove disposable backup counterfactual")
	}
	readControlPlaneMigrationProof(t, ctx, observer, "jira-create-fields-backup", "postcondition")
}
