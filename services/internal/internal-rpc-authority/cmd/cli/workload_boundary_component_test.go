package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Проверяется настоящий LOGIN, а не SET SESSION AUTHORIZATION администратора.
// Порт принадлежит одноразовому контейнеру публичной оснастки.
func workloadBoundaryConnection(t *testing.T, ctx context.Context, port uint64, principal, capability string) *pgx.Conn {
	t.Helper()
	config, err := pgx.ParseConfig(fmt.Sprintf("host=127.0.0.1 port=%d dbname=internal_rpc_authority user=%s sslmode=disable", port, principal))
	if err != nil {
		t.Fatal("parse disposable connection")
	}
	connection, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatal("connect disposable principal")
	}
	t.Cleanup(func() { _ = connection.Close(context.Background()) })
	if capability != "" {
		boundaryExec(t, ctx, connection, "SET ROLE "+pgx.Identifier{capability}.Sanitize())
	}
	return connection
}

func boundaryExec(t *testing.T, ctx context.Context, connection *pgx.Conn, query string, args ...any) pgconn.CommandTag {
	t.Helper()
	tag, err := connection.Exec(ctx, query, args...)
	if err != nil {
		// Текст PostgreSQL может включать параметры. Печатаем только SQLSTATE.
		code := "transport"
		if postgres, ok := err.(*pgconn.PgError); ok {
			code = postgres.Code
		}
		t.Fatalf("disposable boundary statement failed: %s", code)
	}
	return tag
}

func boundaryDenied(t *testing.T, ctx context.Context, connection *pgx.Conn, query string, args ...any) {
	t.Helper()
	_, err := connection.Exec(ctx, query, args...)
	postgres, ok := err.(*pgconn.PgError)
	if !ok || postgres.Code != "42501" {
		t.Fatal("boundary did not reject statement with insufficient privilege")
	}
}

func testWorkloadDatabaseBoundary(t *testing.T, port uint64) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	admin := workloadBoundaryConnection(t, ctx, port, "postgres", "")
	owner := workloadBoundaryConnection(t, ctx, port, "internal_rpc_authority_migrator", "internal_rpc_authority_readback_owner")
	email := workloadBoundaryConnection(t, ctx, port, "ira_email_bridge_issuer_g1", "internal_rpc_authority_issuer")
	issuer := workloadBoundaryConnection(t, ctx, port, "ira_control_plane_issuer_g1", "internal_rpc_authority_issuer")
	verifier := workloadBoundaryConnection(t, ctx, port, "ira_control_plane_verifier_g1", "internal_rpc_authority_verifier")
	retired := workloadBoundaryConnection(t, ctx, port, "ira_session_archive_issuer_g1", "internal_rpc_authority_issuer")

	for _, item := range []struct {
		name       string
		connection *pgx.Conn
		workload   string
		capability string
	}{
		{"email", email, "email-bridge", "ISSUER"},
		{"issuer", issuer, "control-plane", "ISSUER"},
		{"verifier", verifier, "control-plane", "VERIFIER"},
	} {
		t.Run(item.name+" identity", func(t *testing.T) {
			var own, foreign, wrongPurpose bool
			if err := item.connection.QueryRow(ctx, `SELECT
				internal_rpc_authority.workload_database_identity_allows_work($1, $2),
				internal_rpc_authority.workload_database_identity_allows_work('runtime-controller', $2),
				internal_rpc_authority.workload_database_identity_allows_work($1, 'PUBLISHER')`, item.workload, item.capability).Scan(&own, &foreign, &wrongPurpose); err != nil || !own || foreign || wrongPurpose {
				t.Fatal("exact database identity readback failed")
			}
			boundaryDenied(t, ctx, item.connection, `SELECT * FROM internal_rpc_authority.authority_workload_database_identities`)
			boundaryDenied(t, ctx, item.connection, `UPDATE internal_rpc_authority.authority_workload_database_identities SET generation = 2`)
			boundaryDenied(t, ctx, item.connection, `TRUNCATE internal_rpc_authority.authority_snapshot_watermarks`)
			boundaryDenied(t, ctx, item.connection, `ALTER TABLE internal_rpc_authority.authority_snapshot_watermarks DISABLE ROW LEVEL SECURITY`)
			boundaryDenied(t, ctx, item.connection, `ALTER TABLE internal_rpc_authority.authority_snapshot_watermarks DISABLE TRIGGER guard_snapshot_watermark`)
		})
	}

	const proofInsert = `INSERT INTO internal_rpc_authority.authority_proof_reservations
		(caller_workload_id, jti, canonical_digest_sha256, expires_at)
		VALUES ($1, $2, repeat('a', 64), clock_timestamp() + interval '1 minute')`
	boundaryExec(t, ctx, issuer, proofInsert, "control-plane", "10930000-0000-4000-8000-000000000001")
	boundaryExec(t, ctx, email, proofInsert, "email-bridge", "10930000-0000-4000-8000-000000000002")
	boundaryDenied(t, ctx, email, proofInsert, "control-plane", "10930000-0000-4000-8000-000000000003")
	if tag := boundaryExec(t, ctx, email, `DELETE FROM internal_rpc_authority.authority_proof_reservations WHERE caller_workload_id = 'control-plane'`); tag.RowsAffected() != 0 {
		t.Fatal("issuer deleted foreign proof")
	}
	boundaryDenied(t, ctx, email, `DELETE FROM internal_rpc_authority.authority_proof_reservations WHERE caller_workload_id = 'email-bridge'`)
	// Owner fixture изображает запись, пережившую настоящий retention. Runtime
	// не может изменить timestamp либо отключить этот guard.
	boundaryExec(t, ctx, admin, `ALTER TABLE internal_rpc_authority.authority_proof_reservations DISABLE TRIGGER guard_proof_reservation`)
	boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_proof_reservations
		SET expires_at = clock_timestamp() - interval '11 minutes', accepted_at = clock_timestamp() - interval '12 minutes'
		WHERE caller_workload_id = 'control-plane'`)
	boundaryExec(t, ctx, admin, `ALTER TABLE internal_rpc_authority.authority_proof_reservations ENABLE TRIGGER guard_proof_reservation`)
	if tag := boundaryExec(t, ctx, email, `DELETE FROM internal_rpc_authority.authority_proof_reservations WHERE caller_workload_id = 'control-plane'`); tag.RowsAffected() != 0 {
		t.Fatal("issuer deleted expired foreign proof")
	}
	if tag := boundaryExec(t, ctx, issuer, `DELETE FROM internal_rpc_authority.authority_proof_reservations WHERE expires_at < clock_timestamp() - interval '10 minutes'`); tag.RowsAffected() != 1 {
		t.Fatal("owner could not clean expired proof after retention")
	}
	boundaryExec(t, ctx, issuer, `INSERT INTO internal_rpc_authority.authority_proof_watermarks
		(caller_workload_id, operation_id, authority_proof_issuer, proof_revision, canonical_payload_digest_sha256, updated_at)
		VALUES ('control-plane', 'test.operation', 'test.issuer', 2, repeat('a', 64), clock_timestamp())`)
	boundaryDenied(t, ctx, issuer, `UPDATE internal_rpc_authority.authority_proof_watermarks SET proof_revision = 1`)
	boundaryDenied(t, ctx, issuer, `UPDATE internal_rpc_authority.authority_proof_watermarks SET canonical_payload_digest_sha256 = repeat('b', 64)`)
	if tag := boundaryExec(t, ctx, email, `UPDATE internal_rpc_authority.authority_proof_watermarks SET proof_revision = 9007199254740991`); tag.RowsAffected() != 0 {
		t.Fatal("issuer changed foreign proof watermark")
	}
	boundaryExec(t, ctx, issuer, `UPDATE internal_rpc_authority.authority_proof_watermarks SET proof_revision = 3, canonical_payload_digest_sha256 = repeat('b', 64)`)

	const replayInsert = `INSERT INTO internal_rpc_authority.authority_replay_reservations
		(target_workload_id, jti, canonical_digest_sha256, expires_at)
		VALUES ($1, '10930000-0000-4000-8000-000000000004', repeat('c', 64), clock_timestamp() + interval '1 minute')`
	boundaryExec(t, ctx, verifier, replayInsert, "control-plane")
	boundaryDenied(t, ctx, verifier, `DELETE FROM internal_rpc_authority.authority_replay_reservations`)
	boundaryDenied(t, ctx, verifier, replayInsert, "secret-broker")
	boundaryDenied(t, ctx, email, replayInsert, "email-bridge")
	boundaryExec(t, ctx, email, `SELECT set_config('kodex.workload_id', 'control-plane', false)`)
	var foreignRows int
	if err := email.QueryRow(ctx, `SELECT count(*) FROM internal_rpc_authority.authority_replay_reservations`).Scan(&foreignRows); err != nil || foreignRows != 0 {
		t.Fatal("caller GUC exposed foreign replay")
	}
	var ownParent int
	if err := issuer.QueryRow(ctx, `SELECT count(*) FROM internal_rpc_authority.authority_replay_reservations`).Scan(&ownParent); err != nil || ownParent != 1 {
		t.Fatal("same-workload continuation parent unavailable")
	}

	const snapshotInsert = `INSERT INTO internal_rpc_authority.authority_snapshot_watermarks
		(target_workload_id, source_revision, source_digest_sha256, key_set_revision, policy_revision, signer_generation, served_at, readback_attestation_receipt_id)
		VALUES ($1, $2, $3, 1, 1, 1, clock_timestamp(), $4)`
	boundaryExec(t, ctx, verifier, snapshotInsert, "control-plane", int64(1093001), strings.Repeat("a", 64), "10930000-0000-4000-8000-000000000021")
	boundaryExec(t, ctx, email, snapshotInsert, "email-bridge", int64(1093001), strings.Repeat("a", 64), "10930000-0000-4000-8000-000000000022")
	boundaryDenied(t, ctx, email, snapshotInsert, "control-plane", int64(1093001), strings.Repeat("a", 64), "10930000-0000-4000-8000-000000000021")
	if tag := boundaryExec(t, ctx, email, `UPDATE internal_rpc_authority.authority_snapshot_watermarks SET source_revision = 9007199254740991 WHERE target_workload_id = 'control-plane'`); tag.RowsAffected() != 0 {
		t.Fatal("issuer changed foreign snapshot watermark")
	}
	boundaryDenied(t, ctx, email, `UPDATE internal_rpc_authority.authority_snapshot_watermarks SET source_revision = 9007199254740991`)
	boundaryDenied(t, ctx, email, `UPDATE internal_rpc_authority.authority_snapshot_watermarks SET key_set_revision = 9007199254740991`)
	boundaryDenied(t, ctx, email, `UPDATE internal_rpc_authority.authority_snapshot_watermarks SET readback_attestation_receipt_id = '10930000-0000-4000-8000-000000000021'`)
	boundaryExec(t, ctx, verifier, `UPDATE internal_rpc_authority.authority_snapshot_watermarks SET
		source_revision = 1093002, source_digest_sha256 = repeat('b', 64),
		readback_attestation_receipt_id = '10930000-0000-4000-8000-000000000023'`)
	boundaryDenied(t, ctx, verifier, `UPDATE internal_rpc_authority.authority_snapshot_watermarks SET
		source_revision = 1093001, source_digest_sha256 = repeat('a', 64),
		readback_attestation_receipt_id = '10930000-0000-4000-8000-000000000021'`)
	testCanonicalBoundaryQueries(t, ctx, issuer, verifier)
	testSnapshotPublishedSignerNegatives(t, ctx, admin, verifier)

	// Владелец обновляет реестр при живой сессии. Уже принятый SET ROLE не
	// сохраняет доступ после retirement даже до termination backend.
	boundaryExec(t, ctx, retired, proofInsert, "session-archive", "10930000-0000-4000-8000-000000000005")
	boundaryExec(t, ctx, retired, `BEGIN`)
	boundaryExec(t, ctx, retired, `SELECT internal_rpc_authority.workload_database_identity_allows_work('session-archive', 'ISSUER')`)
	boundaryExec(t, ctx, owner, `SET lock_timeout = '100ms'`)
	_, retireErr := owner.Exec(ctx, `UPDATE internal_rpc_authority.authority_workload_database_identities
		SET lifecycle_status = 'RETIRED' WHERE principal = 'ira_session_archive_issuer_g1'`)
	if postgres, ok := retireErr.(*pgconn.PgError); !ok || postgres.Code != "55P03" {
		t.Fatal("retirement did not wait for in-flight identity transaction")
	}
	boundaryExec(t, ctx, retired, `ROLLBACK`)
	boundaryExec(t, ctx, owner, `RESET lock_timeout`)
	boundaryExec(t, ctx, owner, `UPDATE internal_rpc_authority.authority_workload_database_identities
		SET lifecycle_status = 'RETIRED' WHERE principal = 'ira_session_archive_issuer_g1'`)
	boundaryDenied(t, ctx, retired, proofInsert, "session-archive", "10930000-0000-4000-8000-000000000006")
	var retiredRows int
	if err := retired.QueryRow(ctx, `SELECT count(*) FROM internal_rpc_authority.authority_proof_reservations`).Scan(&retiredRows); err != nil || retiredRows != 0 {
		t.Fatal("retired live session retained row visibility")
	}
	boundaryDenied(t, ctx, owner, `UPDATE internal_rpc_authority.authority_workload_database_identities
		SET lifecycle_status = 'CURRENT', retired_at = NULL WHERE principal = 'ira_session_archive_issuer_g1'`)
	boundaryExec(t, ctx, owner, `RESET ROLE`)
	boundaryExec(t, ctx, owner, `ALTER ROLE ira_session_archive_issuer_g1 NOLOGIN`)
	boundaryExec(t, ctx, owner, `REVOKE internal_rpc_authority_issuer FROM ira_session_archive_issuer_g1`)
	boundaryExec(t, ctx, owner, `SET ROLE pg_signal_backend`)
	boundaryExec(t, ctx, owner, `SELECT pg_terminate_backend($1, 1000)`, retired.PgConn().PID())
	var retiredReadback bool
	if err := admin.QueryRow(ctx, `SELECT NOT rolcanlogin
		AND NOT EXISTS (SELECT 1 FROM pg_auth_members WHERE member = pg_roles.oid)
		AND NOT EXISTS (SELECT 1 FROM pg_stat_activity WHERE usename = pg_roles.rolname)
		FROM pg_roles WHERE rolname = 'ira_session_archive_issuer_g1'`).Scan(&retiredReadback); err != nil || !retiredReadback {
		t.Fatal("retirement role and backend readback failed")
	}
}

func testCanonicalBoundaryQueries(t *testing.T, ctx context.Context, issuer, verifier *pgx.Conn) {
	t.Helper()
	read := func(name string) string {
		t.Helper()
		query, err := os.ReadFile("../../internal/repository/postgres/authority/sql/" + name + ".sql")
		if err != nil {
			t.Fatal("read canonical authority SQL")
		}
		return string(query)
	}
	proof := pgx.StrictNamedArgs{
		"caller_workload_id": "control-plane", "operation_id": "test.operation",
		"authority_proof_issuer": "test.issuer", "proof_revision": int64(4),
		"canonical_digest_sha256": strings.Repeat("d", 64),
		"jti":                     "10930000-0000-4000-8000-000000000031", "expires_at": time.Now().Add(time.Minute),
	}
	var accepted bool
	if err := issuer.QueryRow(ctx, read("proof__reserve"), proof).Scan(&accepted); err != nil || !accepted {
		t.Fatal("canonical proof reserve rejected own workload")
	}
	if err := issuer.QueryRow(ctx, read("proof__reserve"), proof).Scan(&accepted); err != nil || accepted {
		t.Fatal("canonical proof replay was not rejected")
	}
	proof["proof_revision"], proof["jti"] = int64(1), "10930000-0000-4000-8000-000000000032"
	if err := issuer.QueryRow(ctx, read("proof__reserve"), proof).Scan(&accepted); err != nil || !accepted {
		t.Fatal("canonical out-of-order proof reserve rejected valid work")
	}
	snapshot := pgx.StrictNamedArgs{
		"target_workload_id": "control-plane", "source_revision": int64(1093002),
		"source_digest_sha256": strings.Repeat("b", 64), "key_set_revision": int64(1),
		"policy_revision": int64(1), "signer_generation": int64(1),
		"attestation_receipt_id": "10930000-0000-4000-8000-000000000023",
		"predecessor_revision":   int64(1093001), "predecessor_digest_sha256": strings.Repeat("a", 64),
		"history_revisions": []int64{1093001}, "history_digests": []string{strings.Repeat("a", 64)},
	}
	if err := verifier.QueryRow(ctx, read("verifier__activate_snapshot"), snapshot).Scan(&accepted); err != nil || !accepted {
		t.Fatal("canonical exact snapshot activation rejected")
	}
	snapshot["jti"], snapshot["canonical_digest_sha256"], snapshot["expires_at"] =
		"10930000-0000-4000-8000-000000000033", strings.Repeat("d", 64), time.Now().Add(time.Minute)
	var reserved bool
	if err := verifier.QueryRow(ctx, read("verifier__accept_context"), snapshot).Scan(&accepted, &reserved); err != nil || !accepted || !reserved {
		t.Fatal("canonical context acceptance rejected")
	}
	if err := verifier.QueryRow(ctx, read("verifier__accept_context"), snapshot).Scan(&accepted, &reserved); err != nil || !accepted || reserved {
		t.Fatal("canonical context replay was not rejected")
	}
	continuation := pgx.StrictNamedArgs{
		"parent_target_workload_id": "control-plane", "parent_jti": snapshot["jti"],
		"parent_canonical_digest_sha256": strings.Repeat("d", 64),
		"caller_workload_id":             "control-plane", "jti": "10930000-0000-4000-8000-000000000034",
		"canonical_digest_sha256": strings.Repeat("e", 64), "expires_at": time.Now().Add(time.Minute),
	}
	if err := issuer.QueryRow(ctx, read("continuation__reserve"), continuation).Scan(&accepted, &reserved); err != nil || !accepted || !reserved {
		t.Fatal("canonical continuation could not read same-workload parent")
	}
}

func seedBoundaryAttestations(t *testing.T, ctx context.Context, admin *pgx.Conn) {
	t.Helper()
	// Синтетический owner snapshot и независимые receipts. Не заменяем
	// production validation function заглушкой: проверяются реальные JOIN.
	boundaryExec(t, ctx, admin, `INSERT INTO internal_rpc_authority.authority_snapshot_history
		(source_revision, source_digest_sha256, key_set_revision, policy_revision, signer_generation,
		 predecessor_revision, predecessor_digest_sha256, canonical_payload, published_at, publication_intent_id)
		VALUES (1093001, repeat('a', 64), 1, 1, 7, 1093000, repeat('0', 64), '{}', clock_timestamp(), '10930000-0000-4000-8000-000000000010'),
		(1093002, repeat('b', 64), 1, 1, 9, 1093001, repeat('a', 64), '{}', clock_timestamp(), '10930000-0000-4000-8000-000000000011')`)
	for _, revision := range []int64{1093001, 1093002} {
		boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_snapshot_history
			SET snapshot_compact_jws = $1 WHERE source_revision = $2`, boundarySnapshotJWS(t, "", revision), revision)
	}
	boundaryExec(t, ctx, admin, `INSERT INTO internal_rpc_authority.authority_rotation_intents
		(intent_id, source_revision, source_digest_sha256, status, created_at, updated_at)
		VALUES ('10930000-0000-4000-8000-000000000010', 1093001, repeat('a', 64), 'PROMOTED', clock_timestamp(), clock_timestamp()),
		('10930000-0000-4000-8000-000000000011', 1093002, repeat('b', 64), 'PROMOTED', clock_timestamp(), clock_timestamp())`)
	for index, item := range []struct {
		workload, role, digest string
		revision               int64
	}{
		{"control-plane", "AUTHORIZATION_VERIFIER", "a", 1093001},
		{"email-bridge", "AUTHORIZATION_ISSUER", "a", 1093001},
		{"control-plane", "AUTHORIZATION_VERIFIER", "b", 1093002},
	} {
		id := fmt.Sprintf("10930000-0000-4000-8000-%012d", 21+index)
		spiffe := "spiffe://kodex.local/test/" + item.workload
		boundaryExec(t, ctx, admin, `INSERT INTO internal_rpc_authority.authority_readback_intents
			(intent_id, kind, intent_revision, intent_digest_sha256, workload_id, role, workload_generation,
			 credential_generation, possession_key_generation, status, expires_at, workload_spiffe_id,
			 material_generation, possession_key_kid, possession_key_generation_exact, possession_public_jwk,
			 possession_key_thumbprint_sha256, source_revision, served_state_digest_sha256)
			VALUES ($1, 'SNAPSHOT', 1, repeat('a',64), $2, $3, 1, 1, 1, 'PINNED', clock_timestamp()+interval '5 minutes',
			 $4, 1, 'test-key', 1, '{}', repeat('a',64), $5, $6)`, id, item.workload, item.role, spiffe, item.revision, strings.Repeat(item.digest, 64))
		boundaryExec(t, ctx, admin, `INSERT INTO internal_rpc_authority.authority_readback_attestation_challenges
			(challenge_id, challenge_jti, intent_id, request_digest_sha256, nonce, issued_at, expires_at, consumed_at,
			 peer_spiffe_id, readback_credential_jti, readback_credential_digest_sha256, idempotency_key,
			 semantic_request_digest_sha256, challenge_digest_sha256)
			VALUES ($1, $1, $1, repeat('a',64), 'test-nonce', clock_timestamp(), clock_timestamp()+interval '5 minutes',
			 clock_timestamp(), $2, $1, repeat('a',64), $1, repeat('a',64), repeat('a',64))`, id, spiffe)
		boundaryExec(t, ctx, admin, `INSERT INTO internal_rpc_authority.authority_readback_attestation_receipts
			(receipt_id, challenge_id, semantic_request_digest_sha256, evidence_digest_sha256, verifier_generation,
			 accepted_at, expires_at, evidence_jti, idempotency_key, peer_spiffe_id)
			VALUES ($1, $1, repeat('a',64), repeat('a',64), 1, clock_timestamp(), clock_timestamp()+interval '5 minutes', $1, $1, $2)`, id, spiffe)
	}
}

// Публичный payload синтетической owner publication; криптографический loader
// проверяется отдельно. Этот fixture не заменяет receipt/history JOIN заглушкой.
func boundarySnapshotJWS(t *testing.T, variant string, revision int64) string {
	t.Helper()
	key := map[string]any{"status": "CURRENT", "purpose": "AUTHORIZATION_CONTEXT", "generation": 1}
	issuer := map[string]any{"workload_id": "control-plane", "keys": []any{key}}
	issuers := []any{issuer, map[string]any{"workload_id": "email-bridge", "keys": []any{key}}}
	switch variant {
	case "foreign":
		issuer["workload_id"] = "foreign-workload"
	case "wrong purpose":
		key["purpose"] = "MANIFEST"
	case "wrong generation":
		key["generation"] = 2
	case "not current":
		key["status"] = "PREVIOUS"
	case "duplicate key":
		issuer["keys"] = []any{key, key}
	case "duplicate issuer":
		issuers = append(issuers, issuer)
	case "missing keys":
		delete(issuer, "keys")
	}
	manifestGeneration := 9
	if revision == 1093001 {
		manifestGeneration = 7
	}
	payload, err := json.Marshal(map[string]any{"source_revision": revision, "key_set_revision": 1, "policy_revision": 1, "signer_generation": manifestGeneration, "issuers": issuers})
	if err != nil {
		t.Fatal("encode synthetic public snapshot")
	}
	if variant == "malformed JSON" {
		payload = []byte(`{"issuers":`)
	}
	return "synthetic." + base64.RawURLEncoding.EncodeToString(payload) + ".synthetic"
}

func testSnapshotGenerationUpgradeRejection(t *testing.T, port uint64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	admin := workloadBoundaryConnection(t, ctx, port, "postgres", "")
	verifier := workloadBoundaryConnection(t, ctx, port, "ira_control_plane_verifier_g1", "internal_rpc_authority_verifier")
	seedBoundaryAttestations(t, ctx, admin)
	// До forward migration правильный workload generation=1 отвергается из-за
	// независимого manifest generation=7. После up тот же fixture принят ниже.
	boundaryDenied(t, ctx, verifier, `INSERT INTO internal_rpc_authority.authority_snapshot_watermarks
		(target_workload_id, source_revision, source_digest_sha256, key_set_revision, policy_revision, signer_generation, served_at, readback_attestation_receipt_id)
		VALUES ('control-plane', 1093001, repeat('a',64), 1, 1, 1, clock_timestamp(), '10930000-0000-4000-8000-000000000021')`)
}

func testSnapshotPublishedSignerNegatives(t *testing.T, ctx context.Context, admin, verifier *pgx.Conn) {
	t.Helper()
	for _, variant := range []string{"foreign", "wrong purpose", "wrong generation", "not current", "duplicate key", "duplicate issuer", "missing keys", "malformed JSON"} {
		t.Run("published signer "+variant, func(t *testing.T) {
			boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_snapshot_history
				SET snapshot_compact_jws = $1 WHERE source_revision = 1093002`, boundarySnapshotJWS(t, variant, 1093002))
			boundaryDenied(t, ctx, verifier, `UPDATE internal_rpc_authority.authority_snapshot_watermarks
				SET served_at = clock_timestamp() WHERE target_workload_id = 'control-plane'`)
		})
	}
	boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_snapshot_history
		SET snapshot_compact_jws = $1 WHERE source_revision = 1093002`, boundarySnapshotJWS(t, "", 1093002))
	boundaryExec(t, ctx, verifier, `UPDATE internal_rpc_authority.authority_snapshot_watermarks
		SET served_at = clock_timestamp() WHERE target_workload_id = 'control-plane'`)
}
