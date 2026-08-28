package authority

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	domainrepository "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const authorityPostgresTestDSNEnv = "KODEX_AUTHORITY_POSTGRES_TEST_DSN"

func TestAcceptVerificationExactSnapshotDoesNotWaitForWatermarkRowLock(t *testing.T) {
	dsn := os.Getenv(authorityPostgresTestDSNEnv)
	if dsn == "" {
		t.Skip(authorityPostgresTestDSNEnv + " is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse PostgreSQL test DSN: %v", err)
	}
	poolConfig.MaxConns = 20
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatalf("connect to PostgreSQL test database: %v", err)
	}
	defer pool.Close()
	prepareAuthorityConcurrencyFixture(t, ctx, pool)

	const (
		workloadID = "control-api-gateway"
		receiptID  = "00000000-0000-4000-8000-000000000001"
	)
	state := snapshotStateForConcurrencyTest(receiptID)
	if _, err := pool.Exec(ctx, `
		INSERT INTO internal_rpc_authority.authority_snapshot_watermarks (
			target_workload_id, source_revision, source_digest_sha256,
			key_set_revision, policy_revision, signer_generation,
			readback_attestation_receipt_id, served_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, clock_timestamp())`,
		workloadID,
		state.SourceRevision,
		state.SourceDigestSHA256,
		state.KeySetRevision,
		state.PolicyRevision,
		state.SignerGeneration,
		state.AttestationReceiptID,
	); err != nil {
		t.Fatalf("seed accepted snapshot: %v", err)
	}

	store, err := New(pool, workloadID, domainrepository.ReservationAuthorizationContext)
	if err != nil {
		t.Fatalf("create authority store: %v", err)
	}
	blocker, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin watermark blocker transaction: %v", err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err := blocker.Exec(ctx, `
		UPDATE internal_rpc_authority.authority_snapshot_watermarks
		SET served_at = served_at + interval '1 second'
		WHERE target_workload_id = $1`, workloadID); err != nil {
		t.Fatalf("lock accepted watermark row: %v", err)
	}

	const parallelReads = 12
	start := make(chan struct{})
	errorsByRead := make(chan error, parallelReads)
	var wait sync.WaitGroup
	for index := 0; index < parallelReads; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			readCtx, readCancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
			defer readCancel()
			errorsByRead <- store.AcceptVerification(
				readCtx,
				state,
				contextReservationForConcurrencyTest(workloadID, index+1),
			)
		}()
	}
	close(start)
	wait.Wait()
	close(errorsByRead)
	for err := range errorsByRead {
		if err != nil {
			t.Errorf("exact snapshot acceptance waited for watermark row lock: %v", err)
		}
	}
	if t.Failed() {
		return
	}

	var reservations int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM internal_rpc_authority.authority_replay_reservations
		WHERE target_workload_id = $1`, workloadID).Scan(&reservations); err != nil {
		t.Fatalf("count durable replay reservations: %v", err)
	}
	if reservations != parallelReads {
		t.Fatalf("durable replay reservations = %d, want %d", reservations, parallelReads)
	}

	if err := blocker.Rollback(ctx); err != nil {
		t.Fatalf("release watermark row lock: %v", err)
	}

	replayed := contextReservationForConcurrencyTest(workloadID, 1)
	if err := store.AcceptVerification(ctx, state, replayed); !errors.Is(err, domainrepository.ErrReplay) {
		t.Fatalf("duplicate replay reservation error = %v, want ErrReplay", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE internal_rpc_authority.test_restore_fence SET open = false`); err != nil {
		t.Fatalf("close restore fence: %v", err)
	}
	if err := store.AcceptVerification(
		ctx,
		state,
		contextReservationForConcurrencyTest(workloadID, parallelReads+1),
	); !errors.Is(err, domainrepository.ErrSnapshotRollback) {
		t.Fatalf("closed restore fence error = %v, want ErrSnapshotRollback", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE internal_rpc_authority.test_restore_fence SET open = true`); err != nil {
		t.Fatalf("reopen restore fence: %v", err)
	}

	invalidReceiptState := state
	invalidReceiptState.AttestationReceiptID = "00000000-0000-4000-8000-000000000099"
	if err := store.AcceptVerification(
		ctx,
		invalidReceiptState,
		contextReservationForConcurrencyTest(workloadID, parallelReads+2),
	); !errors.Is(err, domainrepository.ErrSnapshotRollback) {
		t.Fatalf("invalid receipt error = %v, want ErrSnapshotRollback", err)
	}

	advanced := state
	advanced.SourceRevision = 2
	advanced.SourceDigestSHA256 = strings.Repeat("b", 64)
	advanced.PredecessorRevision = 1
	advanced.PredecessorDigestSHA256 = state.SourceDigestSHA256
	advanced.AttestationReceiptID = "00000000-0000-4000-8000-000000000002"
	advanced.History = []domainrepository.RevisionDigest{{
		Revision:     1,
		DigestSHA256: state.SourceDigestSHA256,
	}}
	if _, err := pool.Exec(ctx, `
		INSERT INTO internal_rpc_authority.test_receipts (
			receipt_id, workload_id, source_revision, source_digest_sha256
		) VALUES ($1, $2, $3, $4)`,
		advanced.AttestationReceiptID,
		workloadID,
		advanced.SourceRevision,
		advanced.SourceDigestSHA256,
	); err != nil {
		t.Fatalf("seed advanced snapshot receipt: %v", err)
	}
	if err := store.AcceptVerification(
		ctx,
		advanced,
		contextReservationForConcurrencyTest(workloadID, parallelReads+3),
	); err != nil {
		t.Fatalf("advance snapshot watermark: %v", err)
	}
	var revision uint64
	var digest string
	if err := pool.QueryRow(ctx, `
		SELECT source_revision, source_digest_sha256
		FROM internal_rpc_authority.authority_snapshot_watermarks
		WHERE target_workload_id = $1`, workloadID).Scan(&revision, &digest); err != nil {
		t.Fatalf("read advanced snapshot watermark: %v", err)
	}
	if revision != advanced.SourceRevision || digest != advanced.SourceDigestSHA256 {
		t.Fatalf("advanced snapshot = (%d, %s), want (%d, %s)",
			revision, digest, advanced.SourceRevision, advanced.SourceDigestSHA256)
	}

	if err := store.AcceptVerification(
		ctx,
		state,
		contextReservationForConcurrencyTest(workloadID, parallelReads+4),
	); !errors.Is(err, domainrepository.ErrSnapshotRollback) {
		t.Fatalf("snapshot rollback error = %v, want ErrSnapshotRollback", err)
	}
}

func prepareAuthorityConcurrencyFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	const fixture = `
		DROP SCHEMA IF EXISTS internal_rpc_authority CASCADE;
		CREATE SCHEMA internal_rpc_authority;
		CREATE TABLE internal_rpc_authority.authority_snapshot_watermarks (
			target_workload_id text PRIMARY KEY,
			source_revision bigint NOT NULL,
			source_digest_sha256 text NOT NULL,
			key_set_revision bigint NOT NULL,
			policy_revision bigint NOT NULL,
			signer_generation bigint NOT NULL,
			readback_attestation_receipt_id uuid,
			served_at timestamptz NOT NULL
		);
		CREATE TABLE internal_rpc_authority.authority_replay_reservations (
			target_workload_id text NOT NULL,
			jti uuid NOT NULL,
			canonical_digest_sha256 text NOT NULL,
			expires_at timestamptz NOT NULL,
			accepted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
			PRIMARY KEY (target_workload_id, jti)
		);
		CREATE TABLE internal_rpc_authority.test_restore_fence (open boolean NOT NULL);
		INSERT INTO internal_rpc_authority.test_restore_fence (open) VALUES (true);
		CREATE TABLE internal_rpc_authority.test_receipts (
			receipt_id uuid PRIMARY KEY,
			workload_id text NOT NULL,
			source_revision bigint NOT NULL,
			source_digest_sha256 text NOT NULL
		);
		CREATE FUNCTION internal_rpc_authority.runtime_restore_fence_allows_work()
		RETURNS boolean LANGUAGE sql STABLE AS $$
			SELECT open FROM internal_rpc_authority.test_restore_fence
		$$;
		CREATE FUNCTION internal_rpc_authority.validate_snapshot_attestation_receipt(
			p_receipt_id uuid,
			p_workload_id text,
			p_source_revision bigint,
			p_source_digest_sha256 text
		) RETURNS boolean LANGUAGE sql STABLE AS $$
			SELECT EXISTS (
				SELECT 1 FROM internal_rpc_authority.test_receipts AS receipt
				WHERE receipt.receipt_id = p_receipt_id
				  AND receipt.workload_id = p_workload_id
				  AND receipt.source_revision = p_source_revision
				  AND receipt.source_digest_sha256 = p_source_digest_sha256
			)
		$$;
		INSERT INTO internal_rpc_authority.test_receipts (
			receipt_id, workload_id, source_revision, source_digest_sha256
		) VALUES (
			'00000000-0000-4000-8000-000000000001',
			'control-api-gateway',
			1,
			repeat('a', 64)
		);`
	if _, err := pool.Exec(ctx, fixture); err != nil {
		t.Fatalf("prepare authority concurrency fixture: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DROP SCHEMA IF EXISTS internal_rpc_authority CASCADE`)
	})
}

func snapshotStateForConcurrencyTest(receiptID string) domainrepository.SnapshotState {
	return domainrepository.SnapshotState{
		SourceRevision:          1,
		SourceDigestSHA256:      strings.Repeat("a", 64),
		PredecessorRevision:     0,
		PredecessorDigestSHA256: strings.Repeat("0", 64),
		KeySetRevision:          1,
		PolicyRevision:          1,
		SignerGeneration:        1,
		AttestationReceiptID:    receiptID,
	}
}

func contextReservationForConcurrencyTest(
	workloadID string,
	sequence int,
) domainrepository.Reservation {
	return domainrepository.Reservation{
		Kind:      domainrepository.ReservationAuthorizationContext,
		ScopeID:   workloadID,
		JTI:       fmt.Sprintf("10000000-0000-4000-8000-%012d", sequence),
		Digest:    strings.Repeat("c", 64),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
}
