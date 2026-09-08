package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

// Настоящие LOGIN и production challenge/consume/accept SQL; receipt не вставляется
// вручную. Owner publication берётся из общего migration fixture, подпись/claims
// отдельно проверяются service tests. 31s проходят по настоящим часам PostgreSQL.
func testAuthorityFreshnessProtocol(t *testing.T, port uint64) {
	ctx, cancel := context.WithTimeout(t.Context(), 65*time.Second)
	defer cancel()
	owner := workloadBoundaryConnection(t, ctx, port, "internal_rpc_authority_migrator", "internal_rpc_authority_readback_owner")
	issuer := workloadBoundaryConnection(t, ctx, port, "ira_control_plane_issuer_g1", "internal_rpc_authority_issuer")
	verifier := workloadBoundaryConnection(t, ctx, port, "ira_control_plane_verifier_g1", "internal_rpc_authority_verifier")
	attestor := workloadBoundaryConnection(t, ctx, port, "ira_readback_attestor_g4", "internal_rpc_authority_readback_attestor")
	read := func(path string) string {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("read protocol SQL")
		}
		return string(b)
	}
	own := func(name string) string { return read("testdata/freshness/" + name + ".sql") }
	sql := func(name string) string {
		return read("../../internal/repository/postgres/authority/sql/" + name + ".sql")
	}
	issueSQL := read("../../internal/repository/postgres/readback/sql/readback__issue_challenge.sql")
	consumeSQL := read("../../internal/repository/postgres/readback/sql/readback__consume_challenge.sql")
	id := func(n int) string { return fmt.Sprintf("13130000-0000-4000-8000-%012d", n) }
	digest := strings.Repeat("a", 64)
	issue := func(n int) pgx.StrictNamedArgs {
		args := pgx.StrictNamedArgs{"intent_id": "10930000-0000-4000-8000-000000000023", "challenge_id": id(n), "challenge_jti": id(n + 1), "challenge_nonce": strings.Repeat("n", 32), "challenge_digest_sha256": digest, "readback_credential_jti": id(n + 2), "readback_credential_digest_sha256": digest, "idempotency_key": id(n + 3), "semantic_request_digest_sha256": digest}
		var returned string
		if err := attestor.QueryRow(ctx, issueSQL, args).Scan(&returned); err != nil || returned != id(n) {
			t.Fatal("real durable challenge failed")
		}
		return pgx.StrictNamedArgs{"challenge_id": id(n), "receipt_id": id(n + 4), "evidence_jti": id(n + 5), "evidence_digest_sha256": digest, "verifier_generation": int64(4), "idempotency_key": id(n + 6), "semantic_request_digest_sha256": digest}
	}
	consume := func(args pgx.StrictNamedArgs) string {
		var returned string
		if err := attestor.QueryRow(ctx, consumeSQL, args).Scan(&returned); err != nil {
			t.Fatal("real durable receipt failed")
		}
		return returned
	}
	args := issue(100)
	receipt := consume(args)
	var accepted, expires time.Time
	if err := attestor.QueryRow(ctx, own("receipt"), receipt).Scan(&accepted, &expires); err != nil || expires.Sub(accepted) != 5*time.Minute {
		t.Fatal("additive phase changed legacy receipt TTL")
	}
	if got := consume(args); got != receipt {
		t.Fatal("exact receipt replay changed identity")
	}
	snapshot := pgx.StrictNamedArgs{"target_workload_id": "control-plane", "source_revision": int64(1093002), "source_digest_sha256": strings.Repeat("b", 64), "key_set_revision": int64(1), "policy_revision": int64(1), "signer_generation": int64(1), "attestation_receipt_id": receipt, "predecessor_revision": int64(1093001), "predecessor_digest_sha256": strings.Repeat("a", 64), "history_revisions": []int64{1093001}, "history_digests": []string{strings.Repeat("a", 64)}}
	activate := func(want bool) {
		var result bool
		if err := verifier.QueryRow(ctx, sql("verifier__activate_snapshot"), snapshot).Scan(&result); err != nil || result != want {
			t.Fatal("snapshot activation freshness mismatch")
		}
	}
	activate(true)
	freshArgs := pgx.StrictNamedArgs{}
	for _, k := range []string{"target_workload_id", "source_revision", "source_digest_sha256", "key_set_revision", "policy_revision", "signer_generation"} {
		freshArgs[k] = snapshot[k]
	}
	freshness := func(conn *pgx.Conn, want bool) {
		var got string
		var deadline *time.Time
		var now time.Time
		err := conn.QueryRow(ctx, sql("verifier__freshness"), freshArgs).Scan(&got, &deadline, &now)
		if err != nil || got != receipt || (deadline != nil) != want {
			t.Fatal("durable freshness readback mismatch")
		}
		if want && (deadline.After(accepted.Add(30*time.Second)) || !now.Before(*deadline)) {
			t.Fatal("receipt freshness exceeded accepted time budget")
		}
	}
	freshness(verifier, true)
	cliDB := stdlib.OpenDB(*owner.Config())
	defer cliDB.Close()
	cliStatus, err := readFreshnessStatus(ctx, cliDB)
	if err != nil || cliStatus.Version != 1 || len(cliStatus.Consumers) < 3 {
		t.Fatal("safe migrator CLI readback failed")
	}
	boundaryDenied(t, ctx, verifier, freshnessStatusSQL)
	registrations := map[int]pgx.StrictNamedArgs{}
	register := func(n int) {
		issued := time.Now().UTC().Truncate(time.Second)
		binding := pgx.StrictNamedArgs{"jti": id(n), "canonical_digest_sha256": digest, "caller_workload_id": "control-plane", "target_workload_id": "control-plane", "source_revision": int64(1093002), "source_digest_sha256": strings.Repeat("b", 64), "key_set_revision": int64(1), "policy_revision": int64(1), "signer_generation": int64(1), "issued_at": issued, "expires_at": issued.Add(30 * time.Second), "parent_jti": nil, "parent_digest_sha256": ""}
		var ok bool
		if err := issuer.QueryRow(ctx, sql("context__register_issued"), binding).Scan(&ok); err != nil || !ok {
			t.Fatalf("context registration failed: %v", err)
		}
		if err := issuer.QueryRow(ctx, sql("context__register_issued"), binding).Scan(&ok); err != nil || !ok {
			t.Fatal("exact registration readback failed")
		}
		if err := issuer.QueryRow(ctx, sql("context__issued_readback"), binding).Scan(&ok); err != nil || !ok {
			t.Fatal("exact issued binding readback rejected")
		}
		foreign := pgx.StrictNamedArgs{}
		for key, value := range binding {
			foreign[key] = value
		}
		foreign["canonical_digest_sha256"] = strings.Repeat("f", 64)
		if err := issuer.QueryRow(ctx, sql("context__issued_readback"), foreign).Scan(&ok); err != nil || ok {
			t.Fatal("foreign issued binding readback accepted")
		}
		registrations[n] = binding
	}
	acceptance := func(n int, wantSnapshot, wantReplay bool) {
		contextArgs := pgx.StrictNamedArgs{}
		for k, v := range snapshot {
			contextArgs[k] = v
		}
		contextArgs["jti"], contextArgs["canonical_digest_sha256"], contextArgs["expires_at"] = id(n), digest, time.Now().Add(30*time.Second)
		contextArgs["caller_workload_id"], contextArgs["context_signer_generation"] = "control-plane", int64(1)
		var snapshotOK, replayOK bool
		if err := verifier.QueryRow(ctx, sql("verifier__accept_context"), contextArgs).Scan(&snapshotOK, &replayOK); err != nil || snapshotOK != wantSnapshot || replayOK != wantReplay {
			t.Fatal("protected durable acceptance mismatch")
		}
	}
	register(200)
	acceptance(200, true, true)
	acceptance(200, true, false)
	boundaryDenied(t, ctx, verifier, own("activate"), 1, id(300))
	var version int64
	if err := owner.QueryRow(ctx, own("activate"), 1, id(300)).Scan(&version); err != nil || version != 2 {
		t.Fatal("freshness activation CAS failed")
	}
	if err := owner.QueryRow(ctx, own("activate"), 1, id(300)).Scan(&version); err != nil || version != 2 {
		t.Fatal("freshness activation lost ACK replay failed")
	}
	err = owner.QueryRow(ctx, own("activate"), 1, id(301)).Scan(&version)
	if pg, ok := err.(*pgconn.PgError); !ok || pg.Code != "40001" {
		t.Fatal("freshness activation drift accepted")
	}
	// Attestor больше не вызывается; verifier DB и exact old credentials доступны.
	var oldAccepted, oldExpires time.Time
	if err := attestor.QueryRow(ctx, own("receipt"), receipt).Scan(&oldAccepted, &oldExpires); err != nil || !oldAccepted.Equal(accepted) || !oldExpires.Equal(expires) {
		t.Fatal("activation rewrote an immutable old receipt")
	}
	waitUntil := func(deadline time.Time) {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		select {
		case <-ctx.Done():
			t.Fatal("protocol wait budget exhausted")
		case <-timer.C:
		}
	}
	waitUntil(accepted.Add(10 * time.Second))
	register(201)
	register(203)
	register(206)
	childBinding := pgx.StrictNamedArgs{}
	for k, v := range registrations[201] {
		childBinding[k] = v
	}
	childBinding["jti"], childBinding["parent_jti"], childBinding["parent_digest_sha256"] = id(205), id(200), digest
	childBinding["expires_at"] = registrations[200]["expires_at"]
	var childBound bool
	if err := issuer.QueryRow(ctx, sql("context__register_issued"), childBinding).Scan(&childBound); err != nil || !childBound {
		t.Fatal("durable parent continuation rejected")
	}
	boundaryDenied(t, ctx, issuer, "UPDATE internal_rpc_authority.authority_issued_context_bindings SET valid_until = clock_timestamp() + interval '1 hour' WHERE jti = $1", id(203))
	acceptance(201, true, true)
	freshness(verifier, true)
	activate(true)
	// Блокировка уникального replay key пересекает deadline. После rollback
	// blocker canonical INSERT выполняется, но post-wait eligibility уже false.
	waitUntil(accepted.Add(29 * time.Second))
	blocker := workloadBoundaryConnection(t, ctx, port, "ira_control_plane_verifier_g1", "internal_rpc_authority_verifier")
	tx, err := blocker.Begin(ctx)
	if err != nil {
		t.Fatal("begin bounded contention")
	}
	var locked bool
	lockArgs := pgx.StrictNamedArgs{"target_workload_id": "control-plane", "jti": id(206), "canonical_digest_sha256": digest, "expires_at": time.Now().Add(30 * time.Second)}
	if err := tx.QueryRow(ctx, sql("context__reserve"), lockArgs).Scan(&locked); err != nil || !locked {
		t.Fatal("reserve bounded contention key")
	}
	lateArgs := pgx.StrictNamedArgs{}
	for k, v := range snapshot {
		lateArgs[k] = v
	}
	for k, v := range lockArgs {
		lateArgs[k] = v
	}
	lateArgs["caller_workload_id"], lateArgs["context_signer_generation"] = "control-plane", int64(1)
	type lateResult struct {
		snapshot, replay bool
		err              error
	}
	done := make(chan lateResult, 1)
	go func() {
		var result lateResult
		result.err = verifier.QueryRow(ctx, sql("verifier__accept_context"), lateArgs).Scan(&result.snapshot, &result.replay)
		done <- result
	}()
	waitUntil(accepted.Add(31 * time.Second))
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal("release bounded contention")
	}
	result := <-done
	if result.err != nil || result.snapshot || result.replay {
		t.Fatal("lock wait extended accepted receipt deadline")
	}
	freshness(verifier, false)
	acceptance(202, false, false)
	activate(false)
	restarted := workloadBoundaryConnection(t, ctx, port, "ira_control_plane_verifier_g1", "internal_rpc_authority_verifier")
	freshness(restarted, false)
	var legacyValid bool
	if err := verifier.QueryRow(ctx, own("old-reader"), receipt).Scan(&legacyValid); err != nil || legacyValid {
		t.Fatal("old consumer extended receipt after CAS activation")
	}
	if got := consume(args); got != receipt {
		t.Fatal("expired receipt retry changed identity")
	}
	freshness(verifier, false)
	// Восстановление требует нового challenge/receipt; прежний JTI остаётся replay.
	args = issue(400)
	receipt = consume(args)
	snapshot["attestation_receipt_id"] = receipt
	if err := attestor.QueryRow(ctx, own("receipt"), receipt).Scan(&accepted, &expires); err != nil || expires.Sub(accepted) != 30*time.Second {
		t.Fatal("active receipt TTL exceeds30 seconds")
	}
	activate(true)
	freshness(verifier, true)
	acceptance(203, false, false)
	acceptance(205, false, false)
	acceptance(200, false, false)
	var rebound bool
	if err := issuer.QueryRow(ctx, sql("context__register_issued"), registrations[203]).Scan(&rebound); err != nil || rebound {
		t.Fatal("receipt B renewed old context A")
	}
	if err := issuer.QueryRow(ctx, sql("context__issued_readback"), registrations[203]).Scan(&rebound); err != nil || rebound {
		t.Fatal("receipt B renewed original binding readback")
	}
	register(204)
	acceptance(204, true, true)
	acceptance(204, true, false)
}
