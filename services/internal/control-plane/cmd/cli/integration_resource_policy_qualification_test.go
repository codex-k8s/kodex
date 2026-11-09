package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
)

// testIntegrationResourcePolicyQualification checks the036→037 migration prerequisites, changed schema objects, repeat apply, unchanged proof watermark and rejected policy drift; backup mode also exercises SELECT-only restore and actual dump/recovery.
// testIntegrationResourcePolicyQualification проверяет условия036→037, изменённые schema objects, repeat apply, watermark и отказ policy drift; backup mode проверяет SELECT-only restore и настоящий dump/recovery.
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
	if backup {
		// Исторический backup bootstrap мог заранее выдать SELECT этим existing tables.
		if _, err := observer.Exec(ctx, "SET ROLE control_plane_owner; GRANT SELECT ON control_plane.integration_grants,control_plane.integration_approval_scopes TO kodex_backup_reader; RESET ROLE"); err != nil {
			t.Fatal(err)
		}
		readControlPlaneMigrationProof(t, ctx, observer, profile, "precondition")
		// Доступ SELECT может быть выдан любому подмножеству: это не меняет смысл миграции.
		if _, err := observer.Exec(ctx, "SET ROLE control_plane_owner; REVOKE SELECT ON control_plane.integration_approval_scopes FROM kodex_backup_reader RESTRICT; RESET ROLE"); err != nil {
			t.Fatal(err)
		}
		readControlPlaneMigrationProof(t, ctx, observer, profile, "precondition")
		for _, statement := range []string{
			"SET ROLE control_plane_owner; GRANT SELECT,UPDATE ON control_plane.integration_approval_scopes TO kodex_backup_reader; RESET ROLE",
			"SET ROLE control_plane_owner; REVOKE UPDATE ON control_plane.integration_approval_scopes FROM kodex_backup_reader RESTRICT; GRANT SELECT ON control_plane.integration_approval_scopes TO kodex_backup_reader WITH GRANT OPTION; RESET ROLE",
		} {
			if _, err := observer.Exec(ctx, statement); err != nil {
				t.Fatal(err)
			}
			assertResourcePolicyPredecessorRejected(t, ctx, observer, profile)
		}
		if _, err := observer.Exec(ctx, "SET ROLE control_plane_owner; REVOKE SELECT,UPDATE ON control_plane.integration_approval_scopes FROM kodex_backup_reader RESTRICT; REVOKE SELECT ON control_plane.integration_grants FROM kodex_backup_reader RESTRICT; RESET ROLE"); err != nil {
			t.Fatal(err)
		}
		readControlPlaneMigrationProof(t, ctx, observer, profile, "precondition")
	}

	abiBefore := readResourcePolicyFunctionABI(t, ctx, observer)
	if err := goose.UpToContext(ctx, database, "migrations", 20261109000100); err != nil {
		t.Fatal("apply resource policy037", err)
	}
	if backup {
		if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
			t.Fatal("complete schema037 backup ACL handover", err)
		}
	}
	if !bytes.Equal(abiBefore, readResourcePolicyFunctionABI(t, ctx, observer)) {
		t.Fatal("resource policy migration changed existing function identity, ABI or privileges")
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
	drifts := []string{
		"ALTER TABLE control_plane.integration_approval_scopes DISABLE TRIGGER integration_approval_scopes_guard_insert",
		"ALTER TABLE control_plane.integration_approval_scopes ALTER COLUMN scope_lifetime DROP NOT NULL",
		"ALTER TABLE control_plane.integration_approval_scopes DROP CONSTRAINT integration_approval_scopes_initiator_check",
		"GRANT SELECT ON control_plane.integration_approval_scopes TO PUBLIC",
		"GRANT UPDATE ON control_plane.integration_grants TO kodex_backup_reader",
		"ALTER TABLE control_plane.integration_personal_invocation_credentials DROP CONSTRAINT integration_personal_invocati_resource_permission_profile_check",
		"ALTER FUNCTION control_plane.integration_pinned_capabilities(uuid) VOLATILE",
		"ALTER TABLE control_plane.runs DISABLE TRIGGER workspace_volume_root_closed",
		"ALTER TABLE control_plane.session_workspace_volumes DROP CONSTRAINT session_workspace_volumes_ready_record_check; ALTER TABLE control_plane.session_workspace_volumes ADD CONSTRAINT session_workspace_volumes_ready_record_check CHECK (ready_record IS NULL OR octet_length(ready_record) BETWEEN 1 AND 4096 AND ready_record IS JSON OBJECT WITH UNIQUE KEYS)",
	}
	if backup {
		drifts = append(drifts, "ALTER ROLE kodex_backup_reader SUPERUSER")
	}
	for _, drift := range drifts {
		tx, err := observer.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, drift); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal("prepare schema037 counterfactual", err)
		}
		assertResourcePolicyStatementRejected(t, ctx, observer, profile, "postcondition", statement)
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal("rollback schema037 counterfactual", err)
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
	assertRestoredResourcePolicyReadback(t, ctx, observer, string(sql), proof)
}

// assertRestoredResourcePolicyReadback compares the migration readback with actual dump-restored schema metadata; only the explicitly allocated loopback clone database name differs.
// assertRestoredResourcePolicyReadback сравнивает migration readback с actual dump-restored metadata; отличается лишь имя явно созданной loopback clone БД.
func assertRestoredResourcePolicyReadback(t *testing.T, ctx context.Context, observer *pgx.Conn, sql string, source []byte) {
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
	if actualView["database"] != "authority_counter_restore" {
		t.Fatal("unexpected restored policy database")
	}
	actualView["database"] = "control_plane"
	if !reflect.DeepEqual(expectedView, actualView) {
		t.Fatal("dump-restored037 policy readback differs from source")
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

// assertResourcePolicyPredecessorRejected executes the actual precondition after a synthetic privilege mutation; SQL errors are not accepted as permission denial.
// assertResourcePolicyPredecessorRejected исполняет actual precondition после synthetic изменения прав; ошибка SQL не считается отказом полномочий.
func assertResourcePolicyPredecessorRejected(t *testing.T, ctx context.Context, observer *pgx.Conn, profile string) {
	t.Helper()
	raw, err := os.ReadFile("../../../../../tools/release/" + profile + "-migration-precondition.sql")
	if err != nil {
		t.Fatal(err)
	}
	assertResourcePolicyStatementRejected(t, ctx, observer, profile, "precondition", string(raw))
}

// assertResourcePolicyStatementRejected requires the shipped validator to reject a successful SQL readback, preserving the caller's transaction.
// assertResourcePolicyStatementRejected требует отказ shipped validator после успешного SQL readback, сохраняя caller transaction.
func assertResourcePolicyStatementRejected(t *testing.T, ctx context.Context, observer *pgx.Conn, profile, phase, statement string) {
	t.Helper()
	release, err := filepath.Abs("../../../../../tools/release")
	if err != nil {
		t.Fatal(err)
	}
	results, err := observer.PgConn().Exec(ctx, statement).ReadAll()
	if err != nil {
		t.Fatal("execute resource policy check", err)
	}
	var proof []byte
	for _, result := range results {
		if result.Err != nil {
			t.Fatal(result.Err)
		}
		if len(result.Rows) == 0 {
			continue
		}
		if proof != nil || len(result.Rows) != 1 || len(result.Rows[0]) != 1 {
			t.Fatal("resource policy check shape invalid")
		}
		proof = result.Rows[0][0]
	}
	if !json.Valid(proof) {
		t.Fatal("resource policy check JSON invalid")
	}
	function := "validIntegrationResourcePolicies"
	if strings.HasSuffix(profile, "-backup") {
		function += "Backup"
	}
	if phase == "precondition" {
		function += "MigrationPrecondition"
	} else {
		function += "MigrationPostcondition"
	}
	check := exec.CommandContext(ctx, "node", "--input-type=module", "-e", `
import {pathToFileURL} from 'node:url';
const module = await import(pathToFileURL(process.argv[1]));
let raw=''; for await(const chunk of process.stdin) raw+=chunk;
if(module[process.argv[2]](JSON.parse(raw))) process.exit(1);
`, filepath.Join(release, "control-plane-immutable-migration.mjs"), function)
	check.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8"}
	check.Stdin = bytes.NewReader(proof)
	if _, err := check.CombinedOutput(); err != nil {
		t.Fatal("resource policy check accepted invalid schema or privileges", err)
	}
}

// readResourcePolicyFunctionABI reads only the identity and calling contract of the five replaced functions in the disposable fixture; function bodies and full catalogs are excluded.
// readResourcePolicyFunctionABI читает identity и контракт вызова пяти заменяемых функций disposable fixture, без тел функций и полного каталога.
func readResourcePolicyFunctionABI(t *testing.T, ctx context.Context, observer *pgx.Conn) []byte {
	t.Helper()
	statement, err := os.ReadFile("testdata/sql/integration_policy_function_abi.sql")
	if err != nil {
		t.Fatal(err)
	}
	var value []byte
	if err := observer.QueryRow(ctx, string(statement)).Scan(&value); err != nil || !json.Valid(value) {
		t.Fatal("resource policy function ABI readback failed", err)
	}
	return value
}
