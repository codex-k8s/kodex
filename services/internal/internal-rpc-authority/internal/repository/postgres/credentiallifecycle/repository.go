package credentiallifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository реализует устойчивый жизненный цикл поколений учётных данных.
type Repository struct {
	pool *pgxpool.Pool
}

// New создаёт репозиторий поверх проверенного пула PostgreSQL.
func New(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("database credential lifecycle pool is nil")
	}
	if err := validateQueries(); err != nil {
		return nil, err
	}
	return &Repository{pool: pool}, nil
}

// AcquireLease получает ограниченную аренду с новым fencing token.
func (repository *Repository) AcquireLease(
	ctx context.Context,
	holderID string,
	leaseDuration time.Duration,
) (uint64, error) {
	var fencingToken uint64
	err := repository.pool.QueryRow(
		ctx,
		acquireLeaseSQL,
		pgx.StrictNamedArgs{
			"holder_id":              holderID,
			"lease_duration_seconds": int64(leaseDuration / time.Second),
		},
	).Scan(&fencingToken)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, errors.New("database credential reconciler lease is held by another replica")
	}
	if err != nil {
		return 0, fmt.Errorf("acquire database credential reconciler lease: %w", err)
	}
	return fencingToken, nil
}

// ReconcileCredentials атомарно продвигает зарегистрированные поколения.
func (repository *Repository) ReconcileCredentials(
	ctx context.Context,
	holderID string,
	fencingToken uint64,
	requestID string,
	canonicalDigest string,
	registered model.DatabaseCredentialRegisteredSet,
) ([]model.DatabaseCredentialGeneration, error) {
	transaction, err := repository.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.Serializable,
	})
	if err != nil {
		return nil, fmt.Errorf("begin database credential reconciliation: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()
	var leaseValid bool
	if err := transaction.QueryRow(
		ctx,
		validateLeaseSQL,
		pgx.StrictNamedArgs{
			"holder_id":     holderID,
			"fencing_token": fencingToken,
		},
	).Scan(&leaseValid); err != nil {
		return nil, fmt.Errorf("validate database credential fencing token: %w", err)
	}
	if !leaseValid {
		return nil, errors.New("database credential fencing token is stale")
	}
	for _, desired := range registered.Generations {
		var accepted bool
		query := reconcileIdentitySQL
		arguments := pgx.StrictNamedArgs{
			"capability":                   string(desired.Capability),
			"principal":                    desired.Principal,
			"generation":                   desired.Generation,
			"request_id":                   requestID,
			"registered_set_digest_sha256": canonicalDigest,
		}
		if desired.Status == model.DatabaseCredentialRetired {
			query = retireIdentitySQL
		} else {
			arguments["status"] = string(desired.Status)
		}
		if err := transaction.QueryRow(
			ctx,
			query,
			arguments,
		).Scan(&accepted); err != nil {
			return nil, fmt.Errorf("reconcile database credential identity: %w", err)
		}
		if !accepted {
			return nil, errors.New("database credential identity was not reconciled")
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit database credential reconciliation: %w", err)
	}
	return repository.ReadCredentialGenerations(ctx, registered)
}

// ReadCredentialGenerations сверяет обслуживаемые поколения с реестром.
func (repository *Repository) ReadCredentialGenerations(
	ctx context.Context,
	registered model.DatabaseCredentialRegisteredSet,
) ([]model.DatabaseCredentialGeneration, error) {
	rows, err := repository.pool.Query(
		ctx,
		readGenerationsSQL,
		pgx.StrictNamedArgs{
			"registered_set_digest_sha256": registeredDigest(registered),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("read database credential generations: %w", err)
	}
	defer rows.Close()
	result := make([]model.DatabaseCredentialGeneration, 0, len(registered.Generations))
	for rows.Next() {
		var capability string
		var generation uint64
		var status string
		var principal string
		if err := rows.Scan(&capability, &generation, &status, &principal); err != nil {
			return nil, fmt.Errorf("scan database credential generation: %w", err)
		}
		desired, ok := desiredGeneration(registered, capability, generation, status, principal)
		if !ok {
			return nil, errors.New("served database credential generation differs from registry")
		}
		result = append(result, desired)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate database credential generations: %w", err)
	}
	if len(result) != len(registered.Generations) {
		return nil, errors.New("database credential registered set is incomplete")
	}
	return result, nil
}

func registeredDigest(registered model.DatabaseCredentialRegisteredSet) string {
	encoded, err := json.Marshal(registered)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func desiredGeneration(
	registered model.DatabaseCredentialRegisteredSet,
	capability string,
	generation uint64,
	status string,
	principal string,
) (model.DatabaseCredentialGeneration, bool) {
	for _, desired := range registered.Generations {
		if string(desired.Capability) == capability &&
			desired.Generation == generation &&
			string(desired.Status) == status &&
			desired.Principal == principal {
			return desired, true
		}
	}
	return model.DatabaseCredentialGeneration{}, false
}
