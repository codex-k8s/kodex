package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/model"
	domainrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("publisher PostgreSQL pool is nil")
	}
	if err := validateQueries(); err != nil {
		return nil, err
	}
	return &Repository{pool: pool}, nil
}

func (repository *Repository) LoadPublishedCredential(
	ctx context.Context,
	idempotencyKey string,
) (model.PublishedCredential, bool, error) {
	result, err := scanPublished(repository.pool.QueryRow(
		ctx,
		loadDeliverySQL,
		pgx.StrictNamedArgs{"idempotency_key": idempotencyKey},
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.PublishedCredential{}, false, nil
	}
	if err != nil {
		return model.PublishedCredential{}, false, fmt.Errorf(
			"load publisher delivery receipt: %w",
			err,
		)
	}
	return result, true, nil
}

func (repository *Repository) SavePublishedCredential(
	ctx context.Context,
	value model.PublishedCredential,
) (model.PublishedCredential, error) {
	result, err := scanPublished(repository.pool.QueryRow(
		ctx,
		saveDeliverySQL,
		pgx.StrictNamedArgs{
			"idempotency_key":               value.IdempotencyKey,
			"directive_jti":                 value.DirectiveJTI,
			"directive_digest_sha256":       value.DirectiveDigest,
			"delivery_receipt_compact_jws":  value.DeliveryReceiptJWS,
			"role_credential_digest_sha256": value.RoleCredentialDigest,
			"credential_generation":         value.CredentialGeneration,
			"ack_key_generation":            value.ACKKeyGeneration,
			"accepted_at":                   value.AcceptedAt,
		},
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.PublishedCredential{}, domainrepository.ErrIdempotencyConflict
	}
	if err != nil {
		return model.PublishedCredential{}, fmt.Errorf(
			"save publisher delivery receipt: %w",
			err,
		)
	}
	return result, nil
}

func (repository *Repository) PublisherReady(ctx context.Context) error {
	var ready bool
	if err := repository.pool.QueryRow(ctx, readinessSQL).Scan(&ready); err != nil {
		return fmt.Errorf("verify publisher persistence boundary: %w", err)
	}
	if !ready {
		return errors.New("publisher persistence boundary is not ready")
	}
	return nil
}

func (repository *Repository) PinReadbackIntent(
	ctx context.Context,
	value model.ReadbackIntent,
) (model.ReadbackIntent, error) {
	var result model.ReadbackIntent
	var publicJWK []byte
	err := repository.pool.QueryRow(
		ctx,
		pinReadbackIntentSQL,
		pgx.StrictNamedArgs{
			"intent_id":                        value.IntentID,
			"kind":                             value.Kind,
			"intent_revision":                  value.IntentRevision,
			"intent_digest_sha256":             value.IntentDigestSHA256,
			"workload_id":                      value.WorkloadID,
			"workload_spiffe_id":               value.WorkloadSPIFFEID,
			"role":                             value.Role,
			"workload_generation":              value.WorkloadGeneration,
			"credential_generation":            value.CredentialGeneration,
			"material_generation":              value.MaterialGeneration,
			"possession_key_generation":        value.PossessionKeyGeneration,
			"possession_key_kid":               value.PossessionKeyID,
			"possession_public_jwk":            value.PossessionPublicJWK,
			"possession_key_thumbprint_sha256": value.PossessionKeyThumbprint,
			"source_revision":                  value.SourceRevision,
			"served_state_digest_sha256":       value.ServedStateDigestSHA256,
			"expires_at":                       value.ExpiresAt,
		},
	).Scan(
		&result.IntentID,
		&result.Kind,
		&result.IntentRevision,
		&result.IntentDigestSHA256,
		&result.WorkloadID,
		&result.WorkloadSPIFFEID,
		&result.Role,
		&result.WorkloadGeneration,
		&result.CredentialGeneration,
		&result.MaterialGeneration,
		&result.PossessionKeyID,
		&result.PossessionKeyGeneration,
		&publicJWK,
		&result.PossessionKeyThumbprint,
		&result.SourceRevision,
		&result.ServedStateDigestSHA256,
		&result.Status,
		&result.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReadbackIntent{}, domainrepository.ErrIdempotencyConflict
	}
	if err != nil {
		return model.ReadbackIntent{}, fmt.Errorf("pin publisher readback intent: %w", err)
	}
	if !json.Valid(publicJWK) {
		return model.ReadbackIntent{}, errors.New("pinned readback public JWK is invalid")
	}
	result.PossessionPublicJWK = append(result.PossessionPublicJWK[:0], publicJWK...)
	return result, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanPublished(row rowScanner) (model.PublishedCredential, error) {
	var result model.PublishedCredential
	err := row.Scan(
		&result.IdempotencyKey,
		&result.DirectiveJTI,
		&result.DirectiveDigest,
		&result.DeliveryReceiptJWS,
		&result.RoleCredentialDigest,
		&result.CredentialGeneration,
		&result.ACKKeyGeneration,
		&result.AcceptedAt,
	)
	return result, err
}

var _ domainrepository.PublisherStore = (*Repository)(nil)
