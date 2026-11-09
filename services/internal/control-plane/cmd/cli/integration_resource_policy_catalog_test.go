package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
	"os"
	"testing"
)

// TestIntegrationResourcePolicyCatalog captures authored migration metadata only in the dedicated disposable fixture; capture is separate from release qualification and never reads business rows or credential values.
// TestIntegrationResourcePolicyCatalog получает лишь metadata authored migration в dedicated disposable fixture; capture отделён от release qualification и не читает business rows либо credential values.
func TestIntegrationResourcePolicyCatalog(t *testing.T) {
	if os.Getenv("KODEX_RESOURCE_POLICY_SCHEMA_CAPTURE") != "1" {
		t.Skip("resource policy schema capture requires explicit opt-in")
	}
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261108000100); err != nil {
		t.Fatal("prepare exact resource-policy predecessor", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "authority-proof-counter", "postcondition")
	reader, err := os.ReadFile("../../../../../tools/release/integration-resource-policies-catalog.sql")
	if err != nil {
		t.Fatal(err)
	}
	volumeReader, err := os.ReadFile("../../../../../tools/release/integration-resource-policies-volume-catalog.sql")
	if err != nil {
		t.Fatal(err)
	}
	volumeCatalog := readResourcePolicySchemaCatalog(t, ctx, observer, volumeReader)
	predecessorCatalog := readResourcePolicySchemaCatalog(t, ctx, observer, reader)
	if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
		t.Fatal("prepare frozen036 backup catalog", err)
	}
	backupPredecessorCatalog := readResourcePolicySchemaCatalog(t, ctx, observer, reader)
	if _, err := observer.Exec(ctx, "SET ROLE control_plane_owner; REVOKE SELECT ON control_plane.integration_connections,control_plane.integration_invocations,control_plane.integration_personal_credential_revisions,control_plane.integration_personal_credential_tests,control_plane.integration_personal_credentials,control_plane.integration_personal_invocation_admissions,control_plane.integration_personal_invocation_credentials,control_plane.runs,control_plane.integration_grants,control_plane.integration_approval_scopes,control_plane.authority_proof_revision_counter FROM kodex_backup_reader RESTRICT; RESET ROLE"); err != nil {
		t.Fatal("remove only synthetic backup SELECT for normal capture", err)
	}
	restoredPredecessor := readResourcePolicySchemaCatalog(t, ctx, observer, reader)
	var beforeCanonical, restoredCanonical any
	if json.Unmarshal(predecessorCatalog, &beforeCanonical) != nil || json.Unmarshal(restoredPredecessor, &restoredCanonical) != nil || workflowPDLCTestJSON(t, beforeCanonical) != workflowPDLCTestJSON(t, restoredCanonical) {
		t.Fatal("schema-only backup capture changed normal predecessor")
	}

	if err := goose.UpToContext(ctx, database, "migrations", 20261109000100); err != nil {
		t.Fatal("apply authored resource-policy migration", err)
	}
	raw := readResourcePolicySchemaCatalog(t, ctx, observer, reader)
	migration, err := migrations.ReadFile("migrations/20261109000100_integration_resource_policies.sql")
	if err != nil {
		t.Fatal(err)
	}
	mHash, rHash := sha256.Sum256(migration), sha256.Sum256(reader)
	observed := map[string]any{"kind": "EXPECTED_SCHEMA_CONTRACT", "version": "20261109000100", "source": "Disposable PostgreSQL18.3; authored migration and frozen036 predecessor; schema metadata only", "migrationSHA256": hex.EncodeToString(mHash[:]), "catalogReaderSHA256": hex.EncodeToString(rHash[:]), "catalog": raw, "predecessorCatalog": predecessorCatalog, "backupPredecessorCatalog": backupPredecessorCatalog}
	observed["volumeCatalog"] = volumeCatalog
	volumeHash := sha256.Sum256(volumeReader)
	observed["volumeCatalogReaderSHA256"] = hex.EncodeToString(volumeHash[:])
	for key, name := range map[string]string{"predecessorPostconditionSHA256": "tools/release/authority-proof-counter-migration-postcondition.sql", "backupPredecessorPostconditionSHA256": "tools/release/authority-proof-counter-backup-migration-postcondition.sql", "idlePredecessorSHA256": "tools/release/authority-proof-counter-migration-precondition.sql", "restorePredecessorSHA256": "services/internal/control-plane/cmd/cli/workspace_backup_sql/restore_20261108000100.sql"} {
		body, err := os.ReadFile("../../../../../" + name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		observed[key] = hex.EncodeToString(sum[:])
	}
	t.Log("RESOURCE_POLICY_SCHEMA_CAPTURE=" + workflowPDLCTestJSON(t, observed))
}

// readResourcePolicySchemaCatalog bounds a repeatable-read schema-only snapshot for independent predecessor and successor manifests; no business data is read.
// readResourcePolicySchemaCatalog ограничивает repeatable-read schema-only snapshot для независимых predecessor и successor manifests; business data не читаются.
func readResourcePolicySchemaCatalog(t *testing.T, ctx context.Context, observer *pgx.Conn, reader []byte) json.RawMessage {
	t.Helper()
	tx, err := observer.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout='5s'; SET LOCAL lock_timeout='500ms'; SET LOCAL search_path=pg_catalog"); err != nil {
		t.Fatal(err)
	}
	var raw json.RawMessage
	if err := tx.QueryRow(ctx, string(reader)).Scan(&raw); err != nil {
		t.Fatal("read authored schema metadata", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return raw
}
