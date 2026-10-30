package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestWorkflowContentPageMigration proves that only the intended function body changes,
// preserving OID, authority and all twenty catalog entries through stock latest-up and
// backup SELECT restoration; replay is a no-op, while drift and Down fail closed.
// TestWorkflowContentPageMigration доказывает изменение только нужного тела функции с
// сохранением OID, полномочий и двадцати записей каталога через штатный latest-up и
// восстановление backup SELECT; повтор ничего не меняет, drift и Down закрыто отклоняются.
func TestWorkflowContentPageMigration(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261029000300); err != nil {
		t.Fatal("prepare content page exact predecessor", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "workflow-content-page", "precondition")
	if _, err := observer.Exec(ctx, "GRANT SELECT ON ALL TABLES IN SCHEMA public, control_plane TO kodex_backup_reader; ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT SELECT ON TABLES TO kodex_backup_reader"); err != nil {
		t.Fatal("prepare canonical backup SELECT before content page upgrade")
	}
	readControlPlaneMigrationProof(t, ctx, observer, "workflow-content-page-backup", "precondition")
	var before, after []byte
	const identity = `SELECT jsonb_build_array(oid::text,proowner::text,proacl::text,provolatile,prosecdef,proconfig,proargnames,prorettype::text)
FROM pg_proc WHERE oid='control_plane.workflow_pdlc_result_valid(jsonb,jsonb,jsonb[])'::regprocedure`
	if err := observer.QueryRow(ctx, identity).Scan(&before); err != nil {
		t.Fatal("read content page predecessor identity")
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := applyPostgresMigrations(ctx, database, "up"); err != nil {
			t.Fatal("apply content page forward migration", err)
		}
		if err := observer.QueryRow(ctx, identity).Scan(&after); err != nil || !bytes.Equal(before, after) {
			t.Fatal("content page migration changed identity or authority")
		}
		readControlPlaneMigrationProof(t, ctx, observer, "workflow-content-page-backup", "postcondition")
		if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
			t.Fatal("content page backup ACL restoration failed", err)
		}
		readControlPlaneMigrationProof(t, ctx, observer, "workflow-content-page-backup", "postcondition")
	}
	catalogSQL, err := os.ReadFile("../../../../../tools/release/workflow-pdlc-catalog-readback.sql")
	if err != nil {
		t.Fatal("read content page catalog query")
	}
	var actual []byte
	if err := observer.QueryRow(ctx, string(catalogSQL)).Scan(&actual); err != nil {
		t.Fatal("read native content page catalog", err)
	}
	expectedBytes, err := os.ReadFile("../../../../../tools/release/workflow-content-page-schema-manifest.json")
	if err != nil {
		t.Fatal("read expected content page contract")
	}
	var expected struct {
		Catalog json.RawMessage `json:"catalog"`
	}
	if json.Unmarshal(expectedBytes, &expected) != nil {
		t.Fatal("invalid content page contract")
	}
	var equal bool
	if err := observer.QueryRow(ctx, "SELECT $1::jsonb=$2::jsonb", string(actual), string(expected.Catalog)).Scan(&equal); err != nil || !equal {
		t.Fatal("actual content page catalog differs from expected contract")
	}
	proofSQL, err := os.ReadFile("../../../../../tools/release/workflow-content-page-backup-migration-postcondition.sql")
	if err != nil {
		t.Fatal("read content page postcondition")
	}
	for _, mutation := range []string{
		"ALTER FUNCTION control_plane.workflow_pdlc_result_valid(jsonb,jsonb,jsonb[]) SECURITY DEFINER",
		"ALTER FUNCTION control_plane.workflow_pdlc_result_valid(jsonb,jsonb,jsonb[]) SET search_path=public",
		"GRANT EXECUTE ON FUNCTION control_plane.workflow_pdlc_result_valid(jsonb,jsonb,jsonb[]) TO PUBLIC",
		"ALTER FUNCTION control_plane.workflow_pdlc_result_valid(jsonb,jsonb,jsonb[]) VOLATILE",
	} {
		if _, err := observer.Exec(ctx, "BEGIN; "+mutation); err != nil {
			t.Fatal("prepare content page catalog counterfactual")
		}
		results, readErr := observer.PgConn().Exec(ctx, string(proofSQL)).ReadAll()
		failed := readErr != nil
		for _, result := range results {
			failed = failed || result.Err != nil
		}
		if _, err := observer.Exec(ctx, "ROLLBACK"); err != nil || !failed {
			t.Fatal("content page proof accepted catalog drift")
		}
	}
	if err := goose.DownContext(ctx, database, "migrations"); err == nil {
		t.Fatal("content page migration unexpectedly allowed Down")
	}
	readControlPlaneMigrationProof(t, ctx, observer, "workflow-content-page-backup", "postcondition")
}
