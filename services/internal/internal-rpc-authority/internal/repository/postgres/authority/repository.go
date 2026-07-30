package authority

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool             *pgxpool.Pool
	targetWorkloadID string
	queries          querySet
}

func New(pool *pgxpool.Pool, targetWorkloadID string) (*Store, error) {
	if pool == nil || targetWorkloadID == "" {
		return nil, errors.New("invalid authority store configuration")
	}
	queries, err := loadQueries()
	if err != nil {
		return nil, err
	}
	return &Store{
		pool:             pool,
		targetWorkloadID: targetWorkloadID,
		queries:          queries,
	}, nil
}

func (store *Store) Reserve(
	ctx context.Context,
	reservation repository.Reservation,
) error {
	query := store.queries.contextReserve
	args := contextReservationArgs(reservation)
	if reservation.Kind == repository.ReservationAuthorityProof {
		query = store.queries.proofReserve
		args = proofReservationArgs(reservation)
	}
	var accepted bool
	err := store.pool.QueryRow(
		ctx,
		query,
		args,
	).Scan(&accepted)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !accepted {
		return repository.ErrReplay
	}
	if err != nil {
		return fmt.Errorf("reserve replay identifier: %w", err)
	}
	return nil
}

func (store *Store) ActivateSnapshot(
	ctx context.Context,
	state repository.SnapshotState,
) error {
	var accepted bool
	if err := store.pool.QueryRow(
		ctx,
		store.queries.verifierActivateSnapshot,
		snapshotArgs(store.targetWorkloadID, state),
	).Scan(&accepted); err != nil {
		return fmt.Errorf("activate served snapshot: %w", err)
	}
	if !accepted {
		return repository.ErrSnapshotRollback
	}
	return nil
}

func (store *Store) AcceptVerification(
	ctx context.Context,
	state repository.SnapshotState,
	reservation repository.Reservation,
) error {
	args := snapshotArgs(store.targetWorkloadID, state)
	for key, value := range contextReservationArgs(reservation) {
		args[key] = value
	}
	var snapshotAccepted bool
	var replayAccepted bool
	if err := store.pool.QueryRow(
		ctx,
		store.queries.verifierAcceptContext,
		args,
	).Scan(&snapshotAccepted, &replayAccepted); err != nil {
		return fmt.Errorf("accept verified context: %w", err)
	}
	if !snapshotAccepted {
		return repository.ErrSnapshotRollback
	}
	if !replayAccepted {
		return repository.ErrReplay
	}
	return nil
}

func (store *Store) Ready(
	ctx context.Context,
	expected repository.SnapshotState,
) error {
	if err := store.pool.Ping(ctx); err != nil {
		return fmt.Errorf("check PostgreSQL connectivity: %w", err)
	}
	var ready bool
	if err := store.pool.QueryRow(
		ctx,
		store.queries.verifierReadiness,
		snapshotArgs(store.targetWorkloadID, expected),
	).Scan(&ready); err != nil {
		return fmt.Errorf("check served snapshot: %w", err)
	}
	if !ready {
		return repository.ErrNotReady
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin replay readiness transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()
	probeID, err := randomUUID()
	if err != nil {
		return fmt.Errorf("create replay readiness identifier: %w", err)
	}
	var accepted bool
	if err := transaction.QueryRow(
		ctx,
		store.queries.contextReserve,
		pgx.StrictNamedArgs{
			"target_workload_id":      store.targetWorkloadID,
			"jti":                     probeID,
			"canonical_digest_sha256": strings.Repeat("0", 64),
			"expires_at":              time.Now().UTC().Add(time.Minute),
		},
	).Scan(&accepted); err != nil {
		return fmt.Errorf("check replay write path: %w", err)
	}
	if !accepted {
		return repository.ErrNotReady
	}
	if err := transaction.Rollback(ctx); err != nil {
		return fmt.Errorf("rollback replay readiness transaction: %w", err)
	}
	return nil
}

func (store *Store) DeleteExpired(ctx context.Context, deleteBefore time.Time) error {
	if _, err := store.pool.Exec(
		ctx,
		store.queries.reservationsDeleteExpired,
		pgx.StrictNamedArgs{"delete_before": deleteBefore},
	); err != nil {
		return fmt.Errorf("delete expired replay reservations: %w", err)
	}
	return nil
}

func (store *Store) Close() {
	store.pool.Close()
}

func proofReservationArgs(value repository.Reservation) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"caller_workload_id":      value.ScopeID,
		"operation_id":            value.OperationID,
		"authority_proof_issuer":  value.Issuer,
		"proof_revision":          value.Revision,
		"jti":                     value.JTI,
		"canonical_digest_sha256": value.Digest,
		"expires_at":              value.ExpiresAt.UTC(),
	}
}

func contextReservationArgs(value repository.Reservation) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"target_workload_id":      value.ScopeID,
		"jti":                     value.JTI,
		"canonical_digest_sha256": value.Digest,
		"expires_at":              value.ExpiresAt.UTC(),
	}
}

func snapshotArgs(
	targetWorkloadID string,
	value repository.SnapshotState,
) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"target_workload_id":        targetWorkloadID,
		"source_revision":           value.SourceRevision,
		"source_digest_sha256":      value.SourceDigestSHA256,
		"predecessor_revision":      value.PredecessorRevision,
		"predecessor_digest_sha256": value.PredecessorDigestSHA256,
		"key_set_revision":          value.KeySetRevision,
		"policy_revision":           value.PolicyRevision,
		"signer_generation":         value.SignerGeneration,
	}
}

func randomUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" +
		encoded[16:20] + "-" + encoded[20:32], nil
}
