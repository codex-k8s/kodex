package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	domainrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository реализует устойчивое состояние publisher в PostgreSQL.
type Repository struct {
	pool *pgxpool.Pool
}

// LoadSnapshotHistory читает ограниченную forward-only цепочку.
func (repository *Repository) LoadSnapshotHistory(
	ctx context.Context,
) (model.AuthoritySnapshotHistory, error) {
	rows, err := repository.pool.Query(ctx, loadSnapshotHistorySQL)
	if err != nil {
		return model.AuthoritySnapshotHistory{}, fmt.Errorf(
			"load publisher snapshot history: %w",
			err,
		)
	}
	defer rows.Close()
	result := model.AuthoritySnapshotHistory{}
	for rows.Next() {
		var item model.RevisionDigest
		if err := rows.Scan(&item.Revision, &item.DigestSHA256); err != nil {
			return model.AuthoritySnapshotHistory{}, fmt.Errorf(
				"scan publisher snapshot history: %w",
				err,
			)
		}
		result.Current = append(result.Current, item)
	}
	if err := rows.Err(); err != nil {
		return model.AuthoritySnapshotHistory{}, fmt.Errorf(
			"iterate publisher snapshot history: %w",
			err,
		)
	}
	sort.Slice(result.Current, func(left, right int) bool {
		return result.Current[left].Revision < result.Current[right].Revision
	})
	return result, nil
}

// LoadSnapshotPublication возвращает immutable same-input publication.
func (repository *Repository) LoadSnapshotPublication(
	ctx context.Context,
	sourceRevision uint64,
	inputDigest string,
) (model.AuthoritySnapshotPublication, bool, error) {
	var result model.AuthoritySnapshotPublication
	err := repository.pool.QueryRow(
		ctx,
		loadSnapshotPublicationSQL,
		pgx.StrictNamedArgs{
			"source_revision":                 sourceRevision,
			"publication_input_digest_sha256": inputDigest,
		},
	).Scan(
		&result.IntentID,
		&result.InputDigestSHA256,
		&result.SourceRevision,
		&result.SourceDigestSHA256,
		&result.KeySetRevision,
		&result.PolicyRevision,
		&result.SignerGeneration,
		&result.PredecessorRevision,
		&result.PredecessorDigestSHA256,
		&result.SnapshotCompactJWS,
		&result.PublishedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.AuthoritySnapshotPublication{}, false, nil
	}
	if err != nil {
		return model.AuthoritySnapshotPublication{}, false, fmt.Errorf(
			"load publisher snapshot publication: %w",
			err,
		)
	}
	return result, true, nil
}

// AppendSnapshot фиксирует immutable publication intent и payload.
func (repository *Repository) AppendSnapshot(
	ctx context.Context,
	value model.AuthoritySnapshotPublication,
	expectedReadbacks int,
) (model.AuthoritySnapshotPublication, error) {
	var accepted bool
	if err := repository.pool.QueryRow(
		ctx,
		appendSnapshotSQL,
		pgx.StrictNamedArgs{
			"source_revision":                 value.SourceRevision,
			"source_digest_sha256":            value.SourceDigestSHA256,
			"key_set_revision":                value.KeySetRevision,
			"policy_revision":                 value.PolicyRevision,
			"signer_generation":               value.SignerGeneration,
			"predecessor_revision":            value.PredecessorRevision,
			"predecessor_digest_sha256":       value.PredecessorDigestSHA256,
			"snapshot_compact_jws":            value.SnapshotCompactJWS,
			"publication_intent_id":           value.IntentID,
			"publication_input_digest_sha256": value.InputDigestSHA256,
			"expected_readback_count":         expectedReadbacks,
			"published_at":                    value.PublishedAt,
		},
	).Scan(&accepted); err != nil {
		return model.AuthoritySnapshotPublication{}, fmt.Errorf(
			"append publisher snapshot history: %w",
			err,
		)
	}
	if !accepted {
		existing, found, loadErr := repository.LoadSnapshotPublication(
			ctx,
			value.SourceRevision,
			value.InputDigestSHA256,
		)
		if loadErr != nil {
			return model.AuthoritySnapshotPublication{}, loadErr
		}
		if found &&
			existing.IntentID == value.IntentID &&
			existing.SourceDigestSHA256 == value.SourceDigestSHA256 &&
			existing.KeySetRevision == value.KeySetRevision &&
			existing.PolicyRevision == value.PolicyRevision &&
			existing.SignerGeneration == value.SignerGeneration &&
			existing.PredecessorRevision == value.PredecessorRevision &&
			existing.PredecessorDigestSHA256 == value.PredecessorDigestSHA256 {
			return existing, nil
		}
		return model.AuthoritySnapshotPublication{}, domainrepository.ErrSnapshotRollback
	}
	return value, nil
}

// SnapshotPublicationReady продвигает intent только после полного readback set.
func (repository *Repository) SnapshotPublicationReady(
	ctx context.Context,
	value model.AuthoritySnapshotPublication,
	expectedReadbacks int,
) error {
	var ready bool
	if err := repository.pool.QueryRow(
		ctx,
		promoteSnapshotSQL,
		pgx.StrictNamedArgs{
			"publication_intent_id":   value.IntentID,
			"source_revision":         value.SourceRevision,
			"source_digest_sha256":    value.SourceDigestSHA256,
			"expected_readback_count": expectedReadbacks,
		},
	).Scan(&ready); err != nil {
		return fmt.Errorf("promote publisher snapshot: %w", err)
	}
	if !ready {
		return errors.New("publisher snapshot readback set is incomplete")
	}
	return nil
}

// New создаёт репозиторий поверх проверенного пула PostgreSQL.
func New(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("publisher PostgreSQL pool is nil")
	}
	if err := validateQueries(); err != nil {
		return nil, err
	}
	return &Repository{pool: pool}, nil
}

// LoadPublishedCredential читает ранее опубликованное удостоверение.
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

// SavePublishedCredential идемпотентно сохраняет опубликованное удостоверение.
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

// PublisherReady проверяет доступность чтения и записи publisher.
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

// PinReadbackIntent фиксирует ожидаемое состояние для независимой проверки.
func (repository *Repository) PinReadbackIntent(
	ctx context.Context,
	value model.ReadbackIntent,
) (model.ReadbackIntent, error) {
	args := pgx.StrictNamedArgs{
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
	}
	result, err := scanReadbackIntent(repository.pool.QueryRow(
		ctx,
		pinReadbackIntentSQL,
		args,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		// INSERT ... ON CONFLICT может дождаться конкурентного commit, сохранив
		// старый snapshot statement. Отдельный запрос увидит commit без выдачи
		// publisher права UPDATE.
		result, err = scanReadbackIntent(repository.pool.QueryRow(
			ctx,
			loadPinnedReadbackIntentSQL,
			pgx.StrictNamedArgs{
				"intent_id":            value.IntentID,
				"intent_digest_sha256": value.IntentDigestSHA256,
				"workload_spiffe_id":   value.WorkloadSPIFFEID,
			},
		))
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReadbackIntent{}, domainrepository.ErrIdempotencyConflict
	}
	if err != nil {
		return model.ReadbackIntent{}, fmt.Errorf("pin publisher readback intent: %w", err)
	}
	return result, nil
}

func scanReadbackIntent(row rowScanner) (model.ReadbackIntent, error) {
	var result model.ReadbackIntent
	var publicJWK []byte
	err := row.Scan(
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
	if err != nil {
		return model.ReadbackIntent{}, err
	}
	if !json.Valid(publicJWK) {
		return model.ReadbackIntent{}, errors.New("pinned readback public JWK is invalid")
	}
	result.PossessionPublicJWK = append([]byte(nil), publicJWK...)
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
