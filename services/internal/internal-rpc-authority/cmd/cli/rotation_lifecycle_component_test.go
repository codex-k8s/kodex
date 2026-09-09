package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func testAuthorityRotationLifecycle(t *testing.T, port uint64) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	admin := workloadBoundaryConnection(t, ctx, port, "postgres", "")
	publisher := workloadBoundaryConnection(
		t,
		ctx,
		port,
		"ira_publisher_g4",
		"internal_rpc_authority_publisher",
	)
	call := func(query string, arguments ...any) bool {
		t.Helper()
		var accepted bool
		if err := publisher.QueryRow(ctx, query, arguments...).Scan(&accepted); err != nil {
			t.Fatal("authority rotation transition query failed")
		}
		return accepted
	}
	prepare := func(intent string, revision int64, registryDigest string, predecessor int64, predecessorDigest string) bool {
		t.Helper()
		return call(`SELECT internal_rpc_authority.publisher_prepare_rotation($1, $2, $3, $4, $5, 2)`,
			intent, revision, registryDigest, predecessor, predecessorDigest)
	}
	begin := func(intent string, revision int64, digest string) bool {
		t.Helper()
		return call(`SELECT internal_rpc_authority.publisher_begin_rotation_delivery($1, $2, $3)`, intent, revision, digest)
	}
	delivered := func(intent string, revision int64, digest string) bool {
		t.Helper()
		return call(`SELECT internal_rpc_authority.publisher_mark_rotation_delivered($1, $2, $3)`, intent, revision, digest)
	}
	abort := func(intent string, revision int64, digest string) bool {
		t.Helper()
		return call(`SELECT internal_rpc_authority.publisher_abort_rotation($1, $2, $3)`, intent, revision, digest)
	}
	appendSnapshot := func(intent string, revision int64, digest string, predecessor int64, predecessorDigest string) bool {
		t.Helper()
		return call(`SELECT internal_rpc_authority.publisher_append_snapshot_history(
			$1, $2, $1, 1, 1, $3, $4, repeat('j', 64), $5,
			repeat('f', 64), 2, clock_timestamp())`,
			revision, digest, predecessor, predecessorDigest, intent)
	}
	promote := func(intent string, revision int64, digest string) bool {
		t.Helper()
		return call(`SELECT internal_rpc_authority.publisher_promote_snapshot(
			$1, $2, $3, 2,
			ARRAY['rotation-required-a', 'rotation-required-b'],
			ARRAY['AUTHORIZATION_ISSUER', 'AUTHORIZATION_VERIFIER'],
			ARRAY[1, 1]::bigint[])`, intent, revision, digest)
	}
	seedReadbacks := func(revision int64, digest string, suffix int) {
		t.Helper()
		for index, role := range []string{"AUTHORIZATION_ISSUER", "AUTHORIZATION_VERIFIER"} {
			boundaryExec(t, ctx, admin, `INSERT INTO internal_rpc_authority.authority_snapshot_readbacks
				(readback_id, workload_id, role, workload_generation, source_revision, digest_sha256, verified_at)
				VALUES ($1, $2, $3, 1, $4, $5, clock_timestamp())`,
				fmt.Sprintf("20900000-0000-4000-8000-%012d", suffix+index),
				[]string{"rotation-required-a", "rotation-required-b"}[index], role, revision, digest)
		}
	}

	// Genesis остаётся покрыт baseline; здесь проверяется upgrade живой history.
	const revisionOne, revisionTwo, revisionThree int64 = 1093003, 1093004, 1093005
	predecessorDigest := strings.Repeat("b", 64)
	registryOne, snapshotOne := strings.Repeat("c", 64), strings.Repeat("d", 64)
	intentOne := "20900000-0000-4000-8000-000000000001"
	boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_rotation_intents
		SET overlap_until = clock_timestamp() - interval '1 second'
		WHERE intent_id = '10930000-0000-4000-8000-000000000011'`)
	if !prepare(intentOne, revisionOne, registryOne, 1093002, predecessorDigest) ||
		!prepare(intentOne, revisionOne, registryOne, 1093002, predecessorDigest) {
		t.Fatal("rotation intent is not restart-idempotent")
	}
	if prepare(intentOne, revisionOne, strings.Repeat("9", 64), 1093002, predecessorDigest) ||
		begin(intentOne, revisionOne, strings.Repeat("9", 64)) {
		t.Fatal("rotation intent accepted corrupted source binding")
	}
	if !begin(intentOne, revisionOne, registryOne) || !begin(intentOne, revisionOne, registryOne) {
		t.Fatal("delivery start is not restart-idempotent")
	}
	if appendSnapshot("20900000-0000-4000-8000-000000000099", revisionOne, snapshotOne, 1093002, predecessorDigest) {
		t.Fatal("legacy writer bypassed active version2 intent")
	}
	if delivered(intentOne, revisionOne, snapshotOne) {
		t.Fatal("delivery completed without durable snapshot")
	}
	if !appendSnapshot(intentOne, revisionOne, snapshotOne, 1093002, predecessorDigest) {
		t.Fatal("snapshot append was rejected")
	}
	if delivered(intentOne, revisionOne, strings.Repeat("9", 64)) {
		t.Fatal("delivery accepted corrupted snapshot digest")
	}
	if promote(intentOne, revisionOne, snapshotOne) {
		t.Fatal("rotation promoted before DELIVERED and complete readback")
	}
	if !delivered(intentOne, revisionOne, snapshotOne) || !delivered(intentOne, revisionOne, snapshotOne) {
		t.Fatal("delivery completion is not restart-idempotent")
	}
	if promote(intentOne, revisionOne, snapshotOne) {
		t.Fatal("rotation promoted with an incomplete consumer set")
	}
	seedReadbacks(revisionOne, snapshotOne, 11)
	if !promote(intentOne, revisionOne, snapshotOne) || !promote(intentOne, revisionOne, snapshotOne) {
		t.Fatal("complete rotation promotion is not restart-idempotent")
	}

	registryTwo := strings.Repeat("e", 64)
	intentTwoAbort := "20900000-0000-4000-8000-000000000002"
	if prepare(intentTwoAbort, revisionTwo, registryTwo, revisionOne, snapshotOne) {
		t.Fatal("next rotation started before bounded overlap")
	}
	boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_rotation_intents
		SET overlap_until = clock_timestamp() - interval '1 second'
		WHERE intent_id = $1`, intentOne)
	if !prepare(intentTwoAbort, revisionTwo, registryTwo, revisionOne, snapshotOne) {
		t.Fatal("next rotation did not start after bounded overlap")
	}
	if !abort(intentTwoAbort, revisionTwo, registryTwo) || abort(intentTwoAbort, revisionTwo, registryTwo) {
		t.Fatal("safe pre-delivery abort transition mismatch")
	}
	intentTwo := "20900000-0000-4000-8000-000000000003"
	if !prepare(intentTwo, revisionTwo, registryTwo, revisionOne, snapshotOne) ||
		!prepare(intentTwo, revisionTwo, registryTwo, revisionOne, snapshotOne) {
		t.Fatal("replacement after safe abort is not deterministic")
	}
	if !begin(intentTwo, revisionTwo, registryTwo) || abort(intentTwo, revisionTwo, registryTwo) {
		t.Fatal("delivery start did not close abort")
	}
	if prepare("20900000-0000-4000-8000-000000000004", revisionThree, strings.Repeat("f", 64), revisionTwo, strings.Repeat("1", 64)) {
		t.Fatal("missed revision or parallel delivery intent was accepted")
	}
	snapshotTwo := strings.Repeat("1", 64)
	if !appendSnapshot(intentTwo, revisionTwo, snapshotTwo, revisionOne, snapshotOne) ||
		!delivered(intentTwo, revisionTwo, snapshotTwo) {
		t.Fatal("second rotation delivery failed")
	}
	seedReadbacks(revisionTwo, snapshotTwo, 21)
	if !promote(intentTwo, revisionTwo, snapshotTwo) {
		t.Fatal("second rotation promotion failed")
	}
	boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_rotation_intents
		SET overlap_until = clock_timestamp() - interval '1 second'
		WHERE intent_id = $1`, intentTwo)
	if prepare("20900000-0000-4000-8000-000000000004", revisionThree+1, strings.Repeat("f", 64), revisionTwo, snapshotTwo) {
		t.Fatal("missed source revision was accepted")
	}
	if !prepare("20900000-0000-4000-8000-000000000005", revisionThree, strings.Repeat("f", 64), revisionTwo, snapshotTwo) {
		t.Fatal("third rotation did not retire the prior generation")
	}
	var retired bool
	if err := admin.QueryRow(ctx, `SELECT status = 'RETIRED' AND retired_at IS NOT NULL
		FROM internal_rpc_authority.authority_rotation_intents WHERE intent_id = $1`, intentTwo).Scan(&retired); err != nil || !retired {
		t.Fatal("prior rotation did not reach durable RETIRED")
	}
	migrator := workloadBoundaryConnection(t, ctx, port, "internal_rpc_authority_migrator", "internal_rpc_authority_readback_owner")
	var raw []byte
	if err := migrator.QueryRow(ctx, `SELECT internal_rpc_authority.authority_rotation_status()`).Scan(&raw); err != nil {
		t.Fatal("operator rotation status failed")
	}
	var status struct {
		IntentID string `json:"intentId"`
		Status   string `json:"status"`
	}
	if json.Unmarshal(raw, &status) != nil || status.IntentID != "20900000-0000-4000-8000-000000000005" || status.Status != "PREPARED" {
		t.Fatal("operator rotation status readback mismatch")
	}
	var operatorAborted bool
	if err := migrator.QueryRow(ctx, `SELECT internal_rpc_authority.publisher_abort_rotation($1, $2, $3)`,
		status.IntentID, revisionThree, strings.Repeat("f", 64)).Scan(&operatorAborted); err != nil || !operatorAborted {
		t.Fatal("operator safe abort failed")
	}
	// Оснастка делит одну disposable БД с остальными component-проверками.
	// Удаляется только синтетический диапазон этого subtest.
	boundaryExec(t, ctx, admin, `DELETE FROM internal_rpc_authority.authority_snapshot_readbacks
		WHERE workload_id IN ('rotation-required-a', 'rotation-required-b')`)
	boundaryExec(t, ctx, admin, `DELETE FROM internal_rpc_authority.authority_snapshot_history
		WHERE source_revision IN ($1, $2)`, revisionOne, revisionTwo)
	boundaryExec(t, ctx, admin, `DELETE FROM internal_rpc_authority.authority_rotation_intents
		WHERE intent_id::text LIKE '20900000-0000-4000-8000-%'`)
	// Возвращаем независимый synthetic fixture соседних subtests в его исходное
	// состояние; production history этот disposable admin path не моделирует.
	boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_rotation_intents
		SET status = 'PROMOTED', retired_at = NULL,
		    overlap_until = clock_timestamp() + interval '40 seconds'
		WHERE intent_id = '10930000-0000-4000-8000-000000000011'`)
}
