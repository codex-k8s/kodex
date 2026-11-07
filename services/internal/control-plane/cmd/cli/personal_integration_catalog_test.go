package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
)

// TestPersonalIntegrationCredentialCatalog captures or verifies only authored schema metadata in
// the disposable PostgreSQL fixture. Capture is explicitly separate from release qualification;
// ordinary execution requires the independently checked manifest and exact migration/reader pins.
// TestPersonalIntegrationCredentialCatalog читает либо проверяет лишь authored schema metadata
// одноразовой PostgreSQL fixture. Capture явно отделён от release qualification; обычный запуск
// требует независимо проверенный manifest и точные migration/reader pins.
func TestPersonalIntegrationCredentialCatalog(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261105000100); err != nil {
		t.Fatal("prepare exact personal catalog predecessor", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "platform-oidc-identity", "postcondition")
	capture := os.Getenv("KODEX_PERSONAL_SCHEMA_CAPTURE") == "1"
	if !capture {
		readControlPlaneMigrationProof(t, ctx, observer, "personal-credentials", "precondition")
	}
	var beforeDefinition, beforeMD5 string
	if err := observer.QueryRow(ctx, `SELECT pg_get_functiondef(oid),md5(prosrc) FROM pg_proc WHERE oid='control_plane.integration_grant_admission(uuid,uuid,uuid,text,text,text,text,text,text,text[])'::regprocedure`).Scan(&beforeDefinition, &beforeMD5); err != nil {
		t.Fatal("read personal admission predecessor schema", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261106000100); err != nil {
		t.Fatal("apply authored personal schema for catalog", err)
	}
	reader, err := os.ReadFile("../../../../../tools/release/personal-credentials-catalog.sql")
	if err != nil {
		t.Fatal("read personal schema catalog query")
	}
	tx, err := observer.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal("open readonly schema capture")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout='5s'; SET LOCAL lock_timeout='500ms'; SET LOCAL search_path=pg_catalog"); err != nil {
		t.Fatal("bound schema-only catalog reader")
	}
	var raw json.RawMessage
	if err := tx.QueryRow(ctx, string(reader)).Scan(&raw); err != nil {
		t.Fatal("read personal schema metadata", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("close readonly schema capture", err)
	}
	var catalog map[string]any
	if json.Unmarshal(raw, &catalog) != nil {
		t.Fatal("decode personal schema metadata")
	}
	migration, err := migrations.ReadFile("migrations/20261106000100_personal_integration_credentials.sql")
	if err != nil {
		t.Fatal("read compiled personal migration")
	}
	migrationHash, readerHash, predecessorHash := sha256.Sum256(migration), sha256.Sum256(reader), sha256.Sum256([]byte(beforeDefinition))
	observed := map[string]any{"kind": "EXPECTED_SCHEMA_CONTRACT", "version": "20261106000100", "source": "Disposable PostgreSQL 18.3; authored migration and frozen033 predecessor; schema metadata only", "migrationSHA256": hex.EncodeToString(migrationHash[:]), "catalogReaderSHA256": hex.EncodeToString(readerHash[:]), "admissionPredecessorDefinitionSHA256": hex.EncodeToString(predecessorHash[:]), "admissionPredecessorSourceMD5": beforeMD5, "catalog": catalog}
	for key, name := range map[string]string{
		"predecessorPostconditionSHA256":       "tools/release/platform-oidc-identity-migration-postcondition.sql",
		"backupPredecessorPostconditionSHA256": "tools/release/platform-oidc-identity-backup-migration-postcondition.sql",
		"restorePredecessorSHA256":             "services/internal/control-plane/cmd/cli/workspace_backup_sql/restore_20261105000100.sql",
		"idlePredecessorSHA256":                "tools/release/platform-oidc-identity-migration-precondition.sql",
	} {
		body, err := os.ReadFile("../../../../../" + name)
		if err != nil {
			t.Fatal("read frozen personal catalog predecessor")
		}
		hash := sha256.Sum256(body)
		observed[key] = hex.EncodeToString(hash[:])
	}
	if capture {
		t.Log("PERSONAL_SCHEMA_CAPTURE=" + workflowPDLCTestJSON(t, observed))
		return
	}
	expected, err := os.ReadFile("../../../../../tools/release/personal-credentials-schema-manifest.json")
	if err != nil {
		t.Fatal("read authored personal schema manifest")
	}
	var manifest map[string]any
	if json.Unmarshal(expected, &manifest) != nil || workflowPDLCTestJSON(t, observed) != workflowPDLCTestJSON(t, manifest) {
		t.Fatal("personal schema catalog or provenance differs from authored manifest")
	}
	readControlPlaneMigrationProof(t, ctx, observer, "personal-credentials", "postcondition")
	proof, err := os.ReadFile("../../../../../tools/release/personal-credentials-migration-postcondition.sql")
	if err != nil {
		t.Fatal("read personal drift rejection proof")
	}
	for _, mutation := range []string{
		"GRANT SELECT ON control_plane.integration_personal_credentials TO PUBLIC",
		"GRANT SELECT(ref) ON control_plane.integration_personal_credentials TO PUBLIC",
		"ALTER TABLE control_plane.integration_personal_credentials ALTER COLUMN generation DROP NOT NULL",
		"ALTER TABLE control_plane.integration_personal_credentials DISABLE TRIGGER USER",
		"CREATE INDEX personal_catalog_unregistered_index ON control_plane.integration_personal_credentials(subject_id)",
		"CREATE POLICY personal_catalog_unregistered_policy ON control_plane.integration_personal_credentials USING(true)",
		"CREATE TABLE control_plane.integration_personal_unregistered_probe(id uuid)",
		"ALTER FUNCTION control_plane.integration_connection_configuration_ready(uuid) SECURITY DEFINER",
		"ALTER FUNCTION control_plane.purge_project_database(uuid,uuid,text) SECURITY INVOKER",
		"CREATE TABLE control_plane.personal_purge_extra_probe(project_id uuid REFERENCES control_plane.projects(id))",
	} {
		assertControlPlaneCatalogMutationRejected(t, ctx, observer, proof, mutation, "personal integration schema catalog changed")
	}
	readControlPlaneMigrationProof(t, ctx, observer, "personal-credentials", "postcondition")
}
