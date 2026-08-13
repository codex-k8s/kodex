package readback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domainrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var consumeSerializationRetryDelays = [...]time.Duration{
	0,
	25 * time.Millisecond,
	75 * time.Millisecond,
	150 * time.Millisecond,
	300 * time.Millisecond,
}

// Repository сохраняет challenge и неизменяемые receipt проверки доставки.
type Repository struct {
	pool *pgxpool.Pool
}

// New создаёт адаптер и проверяет набор named SQL.
func New(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("readback PostgreSQL pool is nil")
	}
	if err := validateQueries(); err != nil {
		return nil, err
	}
	return &Repository{pool: pool}, nil
}

// ResolveReadbackIntent разрешает intent внутри peer boundary.
func (repository *Repository) ResolveReadbackIntent(
	ctx context.Context,
	intentID string,
	peerSPIFFEID string,
) (model.ReadbackIntent, error) {
	intent, err := scanIntent(repository.pool.QueryRow(
		ctx,
		resolveIntentSQL,
		pgx.StrictNamedArgs{
			"intent_id":      intentID,
			"peer_spiffe_id": peerSPIFFEID,
		},
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReadbackIntent{}, domainrepository.ErrNotFound
	}
	if err != nil {
		return model.ReadbackIntent{}, fmt.Errorf("resolve readback intent: %w", err)
	}
	return intent, nil
}

// IssueReadbackChallenge атомарно выпускает либо повторяет challenge.
func (repository *Repository) IssueReadbackChallenge(
	ctx context.Context,
	command domainrepository.IssueReadbackChallengeCommand,
) (model.ReadbackChallenge, error) {
	var challengeID string
	err := repository.pool.QueryRow(
		ctx,
		issueChallengeSQL,
		pgx.StrictNamedArgs{
			"challenge_id":                      command.ChallengeID,
			"challenge_jti":                     command.ChallengeJTI,
			"intent_id":                         command.IntentID,
			"readback_credential_jti":           command.ReadbackCredentialJTI,
			"readback_credential_digest_sha256": command.ReadbackCredentialDigest,
			"idempotency_key":                   command.IdempotencyKey,
			"semantic_request_digest_sha256":    command.SemanticRequestDigest,
			"challenge_nonce":                   command.ChallengeNonce,
			"challenge_digest_sha256":           command.ChallengeDigestSHA256,
		},
	).Scan(&challengeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReadbackChallenge{}, domainrepository.ErrIdempotencyConflict
	}
	if err != nil {
		return model.ReadbackChallenge{}, fmt.Errorf("issue durable readback challenge: %w", err)
	}
	return repository.LoadReadbackChallenge(ctx, challengeID, command.PeerSPIFFEID)
}

// LoadReadbackChallenge читает challenge внутри peer boundary.
func (repository *Repository) LoadReadbackChallenge(
	ctx context.Context,
	challengeID string,
	peerSPIFFEID string,
) (model.ReadbackChallenge, error) {
	challenge, err := scanChallenge(repository.pool.QueryRow(
		ctx,
		loadChallengeSQL,
		pgx.StrictNamedArgs{
			"challenge_id":   challengeID,
			"peer_spiffe_id": peerSPIFFEID,
		},
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReadbackChallenge{}, domainrepository.ErrNotFound
	}
	if err != nil {
		return model.ReadbackChallenge{}, fmt.Errorf("load durable readback challenge: %w", err)
	}
	return challenge, nil
}

// ConsumeReadbackChallenge атомарно потребляет challenge и сохраняет receipt.
func (repository *Repository) ConsumeReadbackChallenge(
	ctx context.Context,
	command domainrepository.ConsumeReadbackChallengeCommand,
) (model.ReadbackReceipt, error) {
	var lastErr error
	for _, delay := range consumeSerializationRetryDelays {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return model.ReadbackReceipt{}, ctx.Err()
			case <-timer.C:
			}
		}
		receipt, err := repository.consumeReadbackChallengeOnce(ctx, command)
		if err == nil || !isSerializationFailure(err) {
			return receipt, err
		}
		lastErr = err
	}
	return model.ReadbackReceipt{}, fmt.Errorf(
		"consume readback challenge after serialization retries: %w",
		lastErr,
	)
}

func (repository *Repository) consumeReadbackChallengeOnce(
	ctx context.Context,
	command domainrepository.ConsumeReadbackChallengeCommand,
) (model.ReadbackReceipt, error) {
	transaction, err := repository.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.Serializable,
	})
	if err != nil {
		return model.ReadbackReceipt{}, fmt.Errorf("begin readback consume transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()
	challenge, err := scanChallenge(transaction.QueryRow(
		ctx,
		loadChallengeSQL,
		pgx.StrictNamedArgs{
			"challenge_id":   command.ChallengeID,
			"peer_spiffe_id": command.PeerSPIFFEID,
		},
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReadbackReceipt{}, domainrepository.ErrNotFound
	}
	if err != nil {
		return model.ReadbackReceipt{}, fmt.Errorf("lock readback challenge: %w", err)
	}
	receipt, found, err := loadReceipt(
		ctx,
		transaction,
		command.PeerSPIFFEID,
		command.IdempotencyKey,
	)
	if err != nil {
		return model.ReadbackReceipt{}, err
	}
	if found {
		if receipt.ChallengeID != command.ChallengeID ||
			receipt.EvidenceJTI != command.EvidenceJTI ||
			receipt.EvidenceDigestSHA256 != command.EvidenceDigestSHA256 ||
			receipt.SemanticRequestDigest != command.SemanticRequestDigest {
			return model.ReadbackReceipt{}, domainrepository.ErrIdempotencyConflict
		}
		receipt.Intent = challenge.Intent
		if err := transaction.Commit(ctx); err != nil {
			return model.ReadbackReceipt{}, fmt.Errorf("commit persisted readback receipt retry: %w", err)
		}
		return receipt, nil
	}
	if challenge.ConsumedAt != nil {
		return model.ReadbackReceipt{}, domainrepository.ErrReplay
	}
	var receiptID string
	err = transaction.QueryRow(
		ctx,
		consumeChallengeSQL,
		pgx.StrictNamedArgs{
			"challenge_id":                   command.ChallengeID,
			"receipt_id":                     command.ReceiptID,
			"evidence_jti":                   command.EvidenceJTI,
			"evidence_digest_sha256":         command.EvidenceDigestSHA256,
			"idempotency_key":                command.IdempotencyKey,
			"semantic_request_digest_sha256": command.SemanticRequestDigest,
			"verifier_generation":            command.VerifierGeneration,
		},
	).Scan(&receiptID)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReadbackReceipt{}, domainrepository.ErrExpired
	}
	if err != nil {
		return model.ReadbackReceipt{}, fmt.Errorf("consume durable readback challenge: %w", err)
	}
	receipt, found, err = loadReceipt(
		ctx,
		transaction,
		command.PeerSPIFFEID,
		command.IdempotencyKey,
	)
	if err != nil {
		return model.ReadbackReceipt{}, err
	}
	if !found || receipt.ReceiptID != receiptID {
		return model.ReadbackReceipt{}, errors.New(
			"readback consume function returned no immutable receipt",
		)
	}
	receipt.Intent = challenge.Intent
	if err := transaction.Commit(ctx); err != nil {
		return model.ReadbackReceipt{}, fmt.Errorf("commit readback consume transaction: %w", err)
	}
	return receipt, nil
}

func isSerializationFailure(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "40001"
}

// ActivateReadbackTrust активирует независимо подтверждённый served trust.
func (repository *Repository) ActivateReadbackTrust(
	ctx context.Context,
	state model.ReadbackTrustState,
) error {
	var accepted bool
	if err := repository.pool.QueryRow(
		ctx,
		activateTrustSQL,
		readbackTrustArgs(state),
	).Scan(&accepted); err != nil {
		return fmt.Errorf("activate durable readback trust: %w", err)
	}
	if !accepted {
		return domainrepository.ErrSnapshotRollback
	}
	return nil
}

// ReadbackReady проверяет доступность и точный durable served trust.
func (repository *Repository) ReadbackReady(
	ctx context.Context,
	state model.ReadbackTrustState,
) error {
	var ready bool
	if err := repository.pool.QueryRow(ctx, readinessSQL).Scan(&ready); err != nil {
		return fmt.Errorf("verify readback persistence boundary: %w", err)
	}
	if !ready {
		return errors.New("readback persistence boundary is not ready")
	}
	if err := repository.pool.QueryRow(
		ctx,
		trustReadinessSQL,
		readbackTrustReadinessArgs(state),
	).Scan(&ready); err != nil {
		return fmt.Errorf("verify readback trust watermark: %w", err)
	}
	if !ready {
		return errors.New("readback trust watermark is not ready")
	}
	return nil
}

func readbackTrustReadinessArgs(state model.ReadbackTrustState) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"root_id":                       state.RootID,
		"root_fingerprint_sha256":       state.RootFingerprintSHA256,
		"manifest_bundle_revision":      state.ManifestBundleRevision,
		"manifest_bundle_digest_sha256": state.ManifestBundleDigestSHA256,
		"trust_source_revision":         state.TrustSourceRevision,
		"trust_set_digest_sha256":       state.TrustSetDigestSHA256,
		"trust_key_set_revision":        state.TrustKeySetRevision,
		"signer_generation":             state.SignerGeneration,
		"served_state_digest_sha256":    state.ServedStateDigestSHA256,
	}
}

func readbackTrustArgs(state model.ReadbackTrustState) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"root_id":                         state.RootID,
		"root_fingerprint_sha256":         state.RootFingerprintSHA256,
		"manifest_bundle_revision":        state.ManifestBundleRevision,
		"manifest_bundle_digest_sha256":   state.ManifestBundleDigestSHA256,
		"trust_source_revision":           state.TrustSourceRevision,
		"trust_set_digest_sha256":         state.TrustSetDigestSHA256,
		"trust_key_set_revision":          state.TrustKeySetRevision,
		"signer_generation":               state.SignerGeneration,
		"predecessor_state_digest_sha256": state.PredecessorStateDigestSHA256,
		"served_state_digest_sha256":      state.ServedStateDigestSHA256,
		"served_at":                       state.ServedAt,
	}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanIntent(row rowScanner) (model.ReadbackIntent, error) {
	var intent model.ReadbackIntent
	var possessionPublicJWK []byte
	err := row.Scan(
		&intent.IntentID,
		&intent.Kind,
		&intent.IntentRevision,
		&intent.IntentDigestSHA256,
		&intent.WorkloadID,
		&intent.WorkloadSPIFFEID,
		&intent.Role,
		&intent.WorkloadGeneration,
		&intent.CredentialGeneration,
		&intent.MaterialGeneration,
		&intent.PossessionKeyID,
		&intent.PossessionKeyGeneration,
		&possessionPublicJWK,
		&intent.PossessionKeyThumbprint,
		&intent.SourceRevision,
		&intent.ServedStateDigestSHA256,
		&intent.Status,
		&intent.ExpiresAt,
	)
	if err != nil {
		return model.ReadbackIntent{}, err
	}
	intent.PossessionPublicJWK = json.RawMessage(possessionPublicJWK)
	return intent, nil
}

func scanChallenge(row rowScanner) (model.ReadbackChallenge, error) {
	var challenge model.ReadbackChallenge
	var possessionPublicJWK []byte
	err := row.Scan(
		&challenge.ChallengeID,
		&challenge.ChallengeJTI,
		&challenge.Nonce,
		&challenge.DigestSHA256,
		&challenge.ReadbackCredentialJTI,
		&challenge.ReadbackCredentialDigest,
		&challenge.IdempotencyKey,
		&challenge.SemanticRequestDigest,
		&challenge.IssuedAt,
		&challenge.ExpiresAt,
		&challenge.ConsumedAt,
		&challenge.Intent.IntentID,
		&challenge.Intent.Kind,
		&challenge.Intent.IntentRevision,
		&challenge.Intent.IntentDigestSHA256,
		&challenge.Intent.WorkloadID,
		&challenge.Intent.WorkloadSPIFFEID,
		&challenge.Intent.Role,
		&challenge.Intent.WorkloadGeneration,
		&challenge.Intent.CredentialGeneration,
		&challenge.Intent.MaterialGeneration,
		&challenge.Intent.PossessionKeyID,
		&challenge.Intent.PossessionKeyGeneration,
		&possessionPublicJWK,
		&challenge.Intent.PossessionKeyThumbprint,
		&challenge.Intent.SourceRevision,
		&challenge.Intent.ServedStateDigestSHA256,
		&challenge.Intent.Status,
		&challenge.Intent.ExpiresAt,
	)
	if err != nil {
		return model.ReadbackChallenge{}, err
	}
	challenge.Intent.PossessionPublicJWK = json.RawMessage(possessionPublicJWK)
	return challenge, nil
}

func loadReceipt(
	ctx context.Context,
	transaction pgx.Tx,
	peerSPIFFEID string,
	idempotencyKey string,
) (model.ReadbackReceipt, bool, error) {
	var receipt model.ReadbackReceipt
	err := transaction.QueryRow(
		ctx,
		loadReceiptSQL,
		pgx.StrictNamedArgs{
			"peer_spiffe_id":  peerSPIFFEID,
			"idempotency_key": idempotencyKey,
		},
	).Scan(
		&receipt.ReceiptID,
		&receipt.ChallengeID,
		&receipt.EvidenceJTI,
		&receipt.EvidenceDigestSHA256,
		&receipt.SemanticRequestDigest,
		&receipt.VerifierGeneration,
		&receipt.AcceptedAt,
		&receipt.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReadbackReceipt{}, false, nil
	}
	if err != nil {
		return model.ReadbackReceipt{}, false, fmt.Errorf("read persisted readback receipt: %w", err)
	}
	return receipt, true, nil
}
