package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pressly/goose/v3"
	"os"
	"reflect"
	"strings"
	"testing"
)

// testIntegrationResourcePolicyQualification checks frozen036 and full037 predecessor/successor catalogs, repeat apply, unchanged proof watermark and fail-closed schema drift; backup mode also exercises SELECT-only restore and actual dump/recovery.
// testIntegrationResourcePolicyQualification проверяет frozen036 и полные037 predecessor/successor catalogs, repeat apply, watermark и закрытый отказ schema drift; backup mode проверяет SELECT-only restore и настоящий dump/recovery.
func testIntegrationResourcePolicyQualification(t *testing.T, backup bool) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261108000100); err != nil {
		t.Fatal("prepare resource policy036 predecessor", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "authority-proof-counter", "postcondition")
	profile := "integration-resource-policies"
	if backup {
		if _, err := observer.Exec(ctx, "UPDATE control_plane.authority_proof_revision_counter SET revision=41 WHERE singleton"); err != nil {
			t.Fatal(err)
		}
		if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
			t.Fatal("prepare exact036 backup ACL", err)
		}
		profile += "-backup"
	}
	readControlPlaneMigrationProof(t, ctx, observer, profile, "precondition")
	if err := goose.UpToContext(ctx, database, "migrations", 20261109000100); err != nil {
		t.Fatal("apply resource policy037", err)
	}
	if backup {
		if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
			t.Fatal("complete schema037 backup ACL handover", err)
		}
	}
	proof := readControlPlaneMigrationProof(t, ctx, observer, profile, "postcondition")
	if err := goose.UpToContext(ctx, database, "migrations", 20261109000100); err != nil {
		t.Fatal("repeat resource policy037 Up", err)
	}
	var watermark int64
	expected := int64(0)
	if backup {
		expected = 41
	}
	if err := observer.QueryRow(ctx, "SELECT revision FROM control_plane.authority_proof_revision_counter WHERE singleton").Scan(&watermark); err != nil || watermark != expected {
		t.Fatal("resource policy migration changed proof watermark", err)
	}
	sql, err := os.ReadFile("../../../../../tools/release/" + profile + "-migration-postcondition.sql")
	if err != nil {
		t.Fatal(err)
	}
	statement := strings.ReplaceAll(string(sql), "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;", "")
	statement = strings.TrimSuffix(strings.TrimSpace(statement), "ROLLBACK;")
	for _, drift := range []string{
		"ALTER TABLE control_plane.integration_approval_scopes DISABLE TRIGGER integration_approval_scopes_guard_insert",
		"ALTER TABLE control_plane.integration_approval_scopes ADD COLUMN unapproved boolean",
		"ALTER TABLE control_plane.integration_approval_scopes DROP CONSTRAINT integration_approval_scopes_initiator_check",
		"GRANT SELECT ON control_plane.integration_approval_scopes TO PUBLIC",
		"GRANT UPDATE ON control_plane.integration_grants TO kodex_backup_reader",
		"ALTER TABLE control_plane.integration_personal_invocation_credentials DROP CONSTRAINT integration_personal_invocati_resource_permission_profile_check",
		"ALTER FUNCTION control_plane.integration_pinned_capabilities(uuid) VOLATILE",
		"ALTER TABLE control_plane.runs DISABLE TRIGGER workspace_volume_root_closed",
		"ALTER TABLE control_plane.session_workspace_volumes DROP CONSTRAINT session_workspace_volumes_ready_record_check; ALTER TABLE control_plane.session_workspace_volumes ADD CONSTRAINT session_workspace_volumes_ready_record_check CHECK (ready_record IS NULL OR octet_length(ready_record) BETWEEN 1 AND 4096 AND ready_record IS JSON OBJECT WITH UNIQUE KEYS)",
	} {
		tx, err := observer.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, drift); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal("prepare schema037 counterfactual", err)
		}
		_, proofErr := tx.Exec(ctx, statement)
		rollbackErr := tx.Rollback(ctx)
		var rejected *pgconn.PgError
		if !errors.As(proofErr, &rejected) || rejected.Code != "P0001" || rollbackErr != nil {
			t.Fatal("resource policy037 accepted schema drift", proofErr)
		}
	}
	if !backup {
		return
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
			t.Fatal("restore exact037 SELECT ACL", err)
		}
	}
	readControlPlaneMigrationProof(t, ctx, observer, profile, "postcondition")
	if _, err := observer.Exec(ctx, "GRANT UPDATE ON control_plane.integration_approval_scopes TO kodex_backup_reader"); err != nil {
		t.Fatal(err)
	}
	if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err == nil {
		t.Fatal("restore037 accepted backup WRITE")
	}
	if _, err := observer.Exec(ctx, "REVOKE UPDATE ON control_plane.integration_approval_scopes FROM kodex_backup_reader"); err != nil {
		t.Fatal(err)
	}
	if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
		t.Fatal("restore037 after safe ACL repair", err)
	}
	allocator, err := os.ReadFile("../../internal/repository/postgres/platform/sql/proof_next_revision.sql")
	if err != nil {
		t.Fatal(err)
	}
	assertAuthorityProofCounterBackupRestore(t, ctx, observer, string(allocator), 41)
	assertRestoredResourcePolicyCatalog(t, ctx, observer, string(sql), proof)
}

// assertRestoredResourcePolicyCatalog compares the entire validated source proof with actual dump-restored schema metadata; only the explicitly allocated loopback clone database name differs.
// assertRestoredResourcePolicyCatalog сравнивает полный validated source proof с actual dump-restored metadata; отличается лишь имя явно созданной loopback clone БД.
func assertRestoredResourcePolicyCatalog(t *testing.T, ctx context.Context, observer *pgx.Conn, sql string, source []byte) {
	t.Helper()
	config := observer.Config().Copy()
	if config.Host != "127.0.0.1" || config.Database != "control_plane" || config.Password != "" || config.TLSConfig != nil {
		t.Fatal("resource policy restore requires disposable loopback fixture")
	}
	config.Database = "authority_counter_restore"
	clone, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = clone.Close(ctx) }()
	results, err := clone.PgConn().Exec(ctx, sql).ReadAll()
	if err != nil {
		t.Fatal("read complete restored037 schema", err)
	}
	var actual []byte
	for _, result := range results {
		if result.Err != nil {
			t.Fatal(result.Err)
		}
		if len(result.Rows) > 0 {
			if actual != nil || len(result.Rows) != 1 || len(result.Rows[0]) != 1 {
				t.Fatal("restored037 proof shape invalid")
			}
			actual = result.Rows[0][0]
		}
	}
	var expectedView, actualView map[string]any
	if json.Unmarshal(source, &expectedView) != nil || json.Unmarshal(actual, &actualView) != nil {
		t.Fatal("restored037 proof JSON invalid")
	}
	normalizeResourcePolicyCloneDatabase(actualView)
	if !reflect.DeepEqual(expectedView, actualView) {
		t.Fatal("dump-restored037 full catalog differs from qualified source")
	}
}

// normalizeResourcePolicyCloneDatabase changes only known database metadata in this synthetic clone comparison; production release validators and SQL remain unchanged.
// normalizeResourcePolicyCloneDatabase меняет лишь known database metadata при synthetic clone comparison; production release validators и SQL не меняются.
func normalizeResourcePolicyCloneDatabase(value map[string]any) {
	for key, item := range value {
		if key == "database" && item == "authority_counter_restore" {
			value[key] = "control_plane"
		} else if object, ok := item.(map[string]any); ok {
			normalizeResourcePolicyCloneDatabase(object)
		}
	}
}

// TestIntegrationResourcePolicyMigration qualifies the existing-installation backup profile and actual recovery against exact037.
// TestIntegrationResourcePolicyMigration квалифицирует existing-installation backup profile и actual recovery по exact037.
func TestIntegrationResourcePolicyMigration(t *testing.T) {
	testIntegrationResourcePolicyQualification(t, true)
}

// TestIntegrationResourcePolicyFreshInstall qualifies the normal fresh-install profile without treating the backup ACL profile as equivalent.
// TestIntegrationResourcePolicyFreshInstall квалифицирует normal fresh-install profile без подмены backup ACL profile.
func TestIntegrationResourcePolicyFreshInstall(t *testing.T) {
	testIntegrationResourcePolicyQualification(t, false)
}
