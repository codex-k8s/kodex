package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
)

// authorityStatusIdentity captures stable function ownership and its exact definition digest without returning SQL bodies, JWS values or credentials.
// authorityStatusIdentity фиксирует stable function ownership и exact definition digest без возврата SQL bodies, JWS values либо credentials.
type authorityStatusIdentity struct {
	OID              string `json:"oid"`
	Owner            string `json:"owner"`
	ACL              string `json:"acl"`
	DefinitionSHA256 string `json:"definitionSHA256"`
}

// readAuthorityStatusIdentity reads one exact function's catalog coordinates through the synthetic administrative connection.
// readAuthorityStatusIdentity читает catalog coordinates одной exact function через synthetic administrative connection.
func readAuthorityStatusIdentity(t *testing.T, ctx context.Context, connection *pgx.Conn) authorityStatusIdentity {
	t.Helper()
	var value authorityStatusIdentity
	if err := connection.QueryRow(ctx, `SELECT procedure.oid::text,owner.rolname,procedure.proacl::text,
  encode(sha256(convert_to(pg_get_functiondef(procedure.oid),'UTF8')),'hex')
  FROM pg_proc AS procedure JOIN pg_namespace AS namespace ON namespace.oid=procedure.pronamespace
  JOIN pg_roles AS owner ON owner.oid=procedure.proowner
  WHERE namespace.nspname='internal_rpc_authority' AND procedure.proname='authority_rotation_status' AND procedure.pronargs=0`).Scan(&value.OID, &value.Owner, &value.ACL, &value.DefinitionSHA256); err != nil {
		t.Fatal("authority function identity read failed", err)
	}
	return value
}

// authorityLedgerDigest hashes all durable rotation/snapshot/readback rows in stable primary-key order; migration bookkeeping and function metadata are deliberately separate.
// authorityLedgerDigest хеширует все durable rotation/snapshot/readback rows в stable primary-key order; migration bookkeeping и function metadata проверяются отдельно.
func authorityLedgerDigest(t *testing.T, ctx context.Context, connection *pgx.Conn) string {
	t.Helper()
	var digest string
	if err := connection.QueryRow(ctx, `SELECT encode(sha256(convert_to(jsonb_build_object(
 'operations',(SELECT jsonb_agg(to_jsonb(r) ORDER BY operation_id) FROM internal_rpc_authority.authority_rotation_operations AS r),
 'phases',(SELECT jsonb_agg(to_jsonb(r) ORDER BY operation_id,phase) FROM internal_rpc_authority.authority_rotation_operation_phase_intents AS r),
 'publications',(SELECT jsonb_agg(to_jsonb(r) ORDER BY operation_id,phase) FROM internal_rpc_authority.authority_rotation_operation_publications AS r),
 'intents',(SELECT jsonb_agg(to_jsonb(r) ORDER BY intent_id) FROM internal_rpc_authority.authority_rotation_intents AS r),
 'history',(SELECT jsonb_agg(to_jsonb(r) ORDER BY source_revision) FROM internal_rpc_authority.authority_snapshot_history AS r),
 'readbacks',(SELECT jsonb_agg(to_jsonb(r) ORDER BY readback_id) FROM internal_rpc_authority.authority_snapshot_readbacks AS r)
 )::text,'UTF8')),'hex')`).Scan(&digest); err != nil {
		t.Fatal("authority ledger digest failed", err)
	}
	return digest
}

// readAuthorityMigrationObserver executes only the harness-pinned bounded repository observer over the same disposable PostgreSQL connection, accepting exactly one JSON result across its READ ONLY transaction.
// readAuthorityMigrationObserver исполняет только закреплённый harness bounded repository observer через ту же disposable PostgreSQL connection и принимает ровно один JSON result его READ ONLY transaction.
func readAuthorityMigrationObserver(t *testing.T, ctx context.Context, connection *pgx.Conn) json.RawMessage {
	t.Helper()
	path := os.Getenv("KODEX_AUTHORITY_STATE_SQL_FILE")
	expected := os.Getenv("KODEX_AUTHORITY_STATE_SQL_SHA256")
	if !filepath.IsAbs(path) || filepath.Base(path) != "authority-distribute-status-migration-state.sql" {
		t.Fatal("native observer source path rejected")
	}
	raw, err := os.ReadFile(path)
	sum := sha256.Sum256(raw)
	if err != nil || len(raw) < 1 || len(raw) > 32768 || hex.EncodeToString(sum[:]) != expected || !bytes.HasPrefix(raw, []byte("BEGIN READ ONLY;")) {
		t.Fatal("native observer source pin rejected")
	}
	results, err := connection.PgConn().Exec(ctx, string(raw)).ReadAll()
	if err != nil {
		t.Fatal("native observer execution failed", err)
	}
	var result json.RawMessage
	for _, item := range results {
		if len(item.Rows) == 0 {
			continue
		}
		if result != nil || len(item.Rows) != 1 || len(item.Rows[0]) != 1 || !json.Valid(item.Rows[0][0]) {
			t.Fatal("native observer shape rejected")
		}
		result = append(json.RawMessage(nil), item.Rows[0][0]...)
	}
	if result == nil {
		t.Fatal("native observer result missing")
	}
	return result
}

// testAuthorityDistributeStatusUpgrade reproduces the missing delivered snapshot before any consumer readback, upgrades the owner function while that operation remains active, then checks provenance rejection with rolled-back administrative fault injection. This is not a real publisher delivery or signed-attestation test.
// testAuthorityDistributeStatusUpgrade воспроизводит отсутствие delivered snapshot до любого consumer readback, обновляет owner function при активной operation и проверяет provenance rejection через откатываемые administrative faults. Это не реальная publisher delivery либо signed-attestation проверка.
func testAuthorityDistributeStatusUpgrade(t *testing.T, port uint64, database *sql.DB) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	admin := workloadBoundaryConnection(t, ctx, port, "postgres", "")
	publisher := workloadBoundaryConnection(t, ctx, port, "ira_publisher_g4", "internal_rpc_authority_publisher")
	migrator := workloadBoundaryConnection(t, ctx, port, "internal_rpc_authority_migrator", "internal_rpc_authority_readback_owner")
	boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_rotation_intents SET overlap_until=clock_timestamp()-interval '1 second' WHERE status='PROMOTED'`)
	var base int64
	var baseDigest string
	if err := admin.QueryRow(ctx, `SELECT source_revision,source_digest_sha256 FROM internal_rpc_authority.authority_snapshot_history ORDER BY source_revision DESC LIMIT 1`).Scan(&base, &baseDigest); err != nil {
		t.Fatal("native predecessor snapshot missing")
	}
	operation := "31900000-0000-4000-8000-000000000001"
	intent := "31900000-0000-4000-8000-000000000002"
	registry := strings.Repeat("6", 64)
	snapshot := strings.Repeat("7", 64)
	input := strings.Repeat("8", 64)
	revision := base + 1
	var status string
	if err := publisher.QueryRow(ctx, `SELECT status FROM internal_rpc_authority.publisher_load_or_prepare_rotation_operation($1,$2,$3,$4,$5,15)`, operation, base+1, registry, base, baseDigest).Scan(&status); err != nil || status != "DISTRIBUTING" {
		t.Fatal("native operation preparation failed", err)
	}
	var accepted bool
	if err := publisher.QueryRow(ctx, `SELECT internal_rpc_authority.publisher_prepare_rotation_phase($1,'DISTRIBUTE',$2,$3,$4,$5,$6,15,$7)`, operation, intent, revision, registry, base, baseDigest, input).Scan(&accepted); err != nil || !accepted {
		t.Fatal("native DISTRIBUTE phase preparation failed", err)
	}
	if err := publisher.QueryRow(ctx, `SELECT internal_rpc_authority.publisher_begin_rotation_delivery($1,$2,$3)`, intent, revision, registry).Scan(&accepted); err != nil || !accepted {
		t.Fatal("native delivery start failed", err)
	}
	if err := publisher.QueryRow(ctx, `SELECT internal_rpc_authority.publisher_append_snapshot_history($1,$2,$1,107,1,$3,$4,repeat('j',64),$5,$6,15,clock_timestamp())`, revision, snapshot, base, baseDigest, intent, input).Scan(&accepted); err != nil || !accepted {
		t.Fatal("native snapshot append failed", err)
	}
	if err := publisher.QueryRow(ctx, `SELECT internal_rpc_authority.publisher_mark_rotation_delivered($1,$2,$3)`, intent, revision, snapshot).Scan(&accepted); err != nil || !accepted {
		t.Fatal("native delivered marker failed", err)
	}
	var oldRaw []byte
	if err := migrator.QueryRow(ctx, rotationStatusSQL).Scan(&oldRaw); err != nil {
		t.Fatal("old native status read failed", err)
	}
	var old map[string]json.RawMessage
	if json.Unmarshal(oldRaw, &old) != nil || old["sourceRevision"] != nil || old["sourceDigestSHA256"] != nil {
		t.Fatal("old pre-readback defect was not reproduced")
	}
	before := readAuthorityStatusIdentity(t, ctx, admin)
	beforeObserver := readAuthorityMigrationObserver(t, ctx, admin)
	ledger := authorityLedgerDigest(t, ctx, admin)
	if before.DefinitionSHA256 != "c615a3d183ca67cadff5cf027d12adb7207bf9a4cf456dea02affe6f63b86af1" {
		t.Fatal("native function predecessor differs")
	}
	// Canonical ACL drift must fail inside Goose, even when function source is exact.
	// Даже при exact function source canonical ACL drift должен отказать внутри Goose.
	boundaryExec(t, ctx, admin, `GRANT EXECUTE ON FUNCTION internal_rpc_authority.authority_rotation_status() TO PUBLIC`)
	if err := goose.UpContext(ctx, database, "migrations"); err == nil {
		t.Fatal("migration admitted foreign function ACL")
	}
	boundaryExec(t, ctx, admin, `REVOKE EXECUTE ON FUNCTION internal_rpc_authority.authority_rotation_status() FROM PUBLIC`)
	var originalDefinition string
	if err := admin.QueryRow(ctx, `SELECT pg_get_functiondef('internal_rpc_authority.authority_rotation_status()'::regprocedure)`).Scan(&originalDefinition); err != nil {
		t.Fatal("native predecessor definition read failed")
	}
	if _, err := admin.Exec(ctx, strings.Replace(originalDefinition, "SELECT CASE", "SELECT /* synthetic predecessor drift */ CASE", 1)); err != nil {
		t.Fatal("native predecessor drift injection failed")
	}
	if err := goose.UpContext(ctx, database, "migrations"); err == nil {
		t.Fatal("migration admitted foreign function definition")
	}
	if _, err := admin.Exec(ctx, originalDefinition); err != nil {
		t.Fatal("native predecessor restoration failed")
	}
	if restored := readAuthorityStatusIdentity(t, ctx, admin); restored != before || authorityLedgerDigest(t, ctx, admin) != ledger {
		t.Fatal("refused predecessor probes changed identity or ledger")
	}
	if err := goose.UpContext(ctx, database, "migrations"); err != nil {
		t.Fatal("native active DISTRIBUTE upgrade failed", err)
	}
	after := readAuthorityStatusIdentity(t, ctx, admin)
	afterObserver := readAuthorityMigrationObserver(t, ctx, admin)
	if before.OID != after.OID || before.Owner != after.Owner || before.ACL != after.ACL || before.DefinitionSHA256 == after.DefinitionSHA256 || authorityLedgerDigest(t, ctx, admin) != ledger {
		t.Fatal("owner upgrade changed function identity or durable ledger")
	}
	var currentRaw []byte
	if err := migrator.QueryRow(ctx, rotationStatusSQL).Scan(&currentRaw); err != nil {
		t.Fatal("new native status read failed", err)
	}
	current, err := decodeRotationStatus(currentRaw)
	if err != nil || current.OperationID != operation || current.Status != "DISTRIBUTING" || current.Phase != "DISTRIBUTE" || current.ProtocolVersion != 2 || current.SourceRevision != revision || current.SourceDigestSHA256 != snapshot || current.RegistrySourceDigestSHA256 == snapshot || current.ActualReadbacks == nil || *current.ActualReadbacks != 0 || current.ExpectedReadbacks == nil || *current.ExpectedReadbacks != 15 || current.CompletedAt != nil {
		t.Fatal("delivered source contract was not restored before readbacks", err)
	}
	var publicationCount int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM internal_rpc_authority.authority_rotation_operation_publications WHERE operation_id=$1`, operation).Scan(&publicationCount); err != nil || publicationCount != 0 {
		t.Fatal("pre-readback fixture already had a promoted operation publication")
	}
	faults := []string{
		`UPDATE internal_rpc_authority.authority_rotation_operations SET base_digest_sha256=repeat('a',64) WHERE operation_id=$1`,
		`UPDATE internal_rpc_authority.authority_rotation_operations SET base_revision=base_revision+30 WHERE operation_id=$1`,
		`UPDATE internal_rpc_authority.authority_rotation_operations SET registry_digest_sha256=repeat('a',64) WHERE operation_id=$1`,
		`UPDATE internal_rpc_authority.authority_rotation_operations SET expected_readback_count=14 WHERE operation_id=$1`,
		`UPDATE internal_rpc_authority.authority_rotation_operation_phase_intents SET source_revision=source_revision+30 WHERE operation_id=$1`,
		`UPDATE internal_rpc_authority.authority_rotation_operation_phase_intents SET registry_digest_sha256=repeat('a',64) WHERE operation_id=$1`,
		`UPDATE internal_rpc_authority.authority_rotation_operation_phase_intents SET publication_input_digest_sha256=repeat('a',64) WHERE operation_id=$1`,
		`UPDATE internal_rpc_authority.authority_rotation_operation_phase_intents SET phase='SWITCH' WHERE operation_id=$1`,
		`UPDATE internal_rpc_authority.authority_rotation_intents SET source_revision=source_revision+30 WHERE intent_id=$1`,
		`UPDATE internal_rpc_authority.authority_rotation_intents SET source_digest_sha256=repeat('a',64) WHERE intent_id=$1`,
		`UPDATE internal_rpc_authority.authority_rotation_intents SET registry_source_digest_sha256=repeat('a',64) WHERE intent_id=$1`,
		`UPDATE internal_rpc_authority.authority_rotation_intents SET predecessor_revision=predecessor_revision+30 WHERE intent_id=$1`,
		`UPDATE internal_rpc_authority.authority_rotation_intents SET predecessor_digest_sha256=repeat('a',64) WHERE intent_id=$1`,
		`UPDATE internal_rpc_authority.authority_rotation_intents SET expected_readback_count=14 WHERE intent_id=$1`,
		`UPDATE internal_rpc_authority.authority_rotation_intents SET status='DELIVERING' WHERE intent_id=$1`,
		`UPDATE internal_rpc_authority.authority_rotation_intents SET protocol_version=1 WHERE intent_id=$1`,
		`UPDATE internal_rpc_authority.authority_snapshot_history SET source_digest_sha256=encode(sha256(convert_to('synthetic-status-binding-fault','UTF8')),'hex') WHERE publication_intent_id=$1`,
		`UPDATE internal_rpc_authority.authority_snapshot_history SET publication_input_digest_sha256=repeat('a',64) WHERE publication_intent_id=$1`,
		`UPDATE internal_rpc_authority.authority_snapshot_history SET predecessor_revision=predecessor_revision+30 WHERE publication_intent_id=$1`,
		`UPDATE internal_rpc_authority.authority_snapshot_history SET predecessor_digest_sha256=repeat('a',64) WHERE publication_intent_id=$1`,
		`UPDATE internal_rpc_authority.authority_snapshot_history SET expected_readback_count=14 WHERE publication_intent_id=$1`,
		`UPDATE internal_rpc_authority.authority_snapshot_history SET published_at=clock_timestamp()+interval '1 second' WHERE publication_intent_id=$1`,
	}
	for _, fault := range faults {
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal("begin native fault")
		}
		argument := operation
		if strings.Contains(fault, "WHERE intent_id") || strings.Contains(fault, "WHERE publication_intent_id") {
			argument = intent
		}
		if _, err := tx.Exec(ctx, fault, argument); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal("inject native provenance fault failed", err)
		}
		var raw []byte
		if err := tx.QueryRow(ctx, rotationStatusSQL).Scan(&raw); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal("faulted status read failed", err)
		}
		var state map[string]json.RawMessage
		if json.Unmarshal(raw, &state) != nil || state["sourceRevision"] != nil || state["sourceDigestSHA256"] != nil {
			_ = tx.Rollback(ctx)
			t.Fatal("inconsistent delivered history remained eligible")
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal("native fault rollback failed")
		}
	}
	if authorityLedgerDigest(t, ctx, admin) != ledger {
		t.Fatal("negative native probes changed ledger")
	}
	if err := goose.UpContext(ctx, database, "migrations"); err != nil {
		t.Fatal("repeat native upgrade failed", err)
	}
	if version, err := goose.GetDBVersionContext(ctx, database); err != nil || version != 20261115000100 {
		t.Fatal("new native Goose version differs")
	}
	if err := goose.DownContext(ctx, database, "migrations"); err == nil {
		t.Fatal("forward-only status migration was reverted")
	}
	if final := readAuthorityStatusIdentity(t, ctx, admin); final != after || authorityLedgerDigest(t, ctx, admin) != ledger {
		t.Fatal("refused rollback changed identity or ledger")
	}
	var rows int
	if version, err := goose.GetDBVersionContext(ctx, database); err != nil || version != 20261115000100 {
		t.Fatal("refused rollback changed Goose version")
	}
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM public.goose_db_version`).Scan(&rows); err != nil || rows != 11 {
		t.Fatal("repeat or refused rollback changed Goose history")
	}
	if output := os.Getenv("KODEX_AUTHORITY_DISTRIBUTE_PROOF_OUTPUT"); output != "" {
		dto, err := json.Marshal(current)
		if err != nil {
			t.Fatal("native DTO marshal failed")
		}
		proof := struct {
			Schema           string                  `json:"schema"`
			BeforeObserver   json.RawMessage         `json:"beforeObserver"`
			AfterObserver    json.RawMessage         `json:"afterObserver"`
			Before           authorityStatusIdentity `json:"before"`
			After            authorityStatusIdentity `json:"after"`
			LedgerSHA256     string                  `json:"ledgerSHA256"`
			Old              json.RawMessage         `json:"oldStatus"`
			Current          json.RawMessage         `json:"currentStatus"`
			DTO              json.RawMessage         `json:"goDTO"`
			NegativeBindings int                     `json:"negativeBindings"`
			Publications     int                     `json:"operationPublications"`
		}{"kodex.authority-distribute-status-native/v1", beforeObserver, afterObserver, before, after, ledger, oldRaw, currentRaw, dto, len(faults), publicationCount}
		raw, err := json.Marshal(proof)
		if err != nil {
			t.Fatal("native proof marshal failed")
		}
		file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal("native proof create failed")
		}
		if _, err := file.Write(raw); err != nil {
			file.Close()
			t.Fatal("native proof write failed")
		}
		if file.Sync() != nil || file.Close() != nil {
			t.Fatal("native proof close failed")
		}
	}
}
