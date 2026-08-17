// Package gateway реализует PostgreSQL-адаптер авторитетного execution state.
package gateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/enum"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const transactionAttempts = 3

type Config struct {
	PrincipalName       string
	PrincipalGeneration uint64
	ContextKeyID        string
	ContextSigningKey   []byte
	ContextTTL          time.Duration
	CleanupBase         context.Context
	CleanupTimeout      time.Duration
}

type Repository struct {
	pool   *pgxpool.Pool
	config Config
}

type transaction struct {
	tx        pgx.Tx
	tenantID  string
	projectID string
	actorID   string
}

var (
	_ domainrepo.Repository  = (*Repository)(nil)
	_ domainrepo.Transaction = (*transaction)(nil)
)

func New(pool *pgxpool.Pool, config Config) (*Repository, error) {
	if pool == nil || config.PrincipalName == "" || config.PrincipalGeneration == 0 ||
		config.ContextKeyID == "" || len(config.ContextSigningKey) < 32 ||
		config.ContextTTL < time.Second || config.ContextTTL > 10*time.Second ||
		config.CleanupBase == nil || config.CleanupTimeout < time.Second || config.CleanupTimeout > time.Minute {
		return nil, errors.New("integration gateway PostgreSQL configuration is invalid")
	}
	return &Repository{pool: pool, config: config}, nil
}

func (repository *Repository) Transact(ctx context.Context, scope domainrepo.Scope, callback func(domainrepo.Transaction) error) error {
	if callback == nil {
		return errors.New("transaction callback is required")
	}
	scope = normalizeScope(scope)
	var last error
	for attempt := 0; attempt < transactionAttempts; attempt++ {
		tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite})
		if err != nil {
			return mapError(err)
		}
		if err := repository.setScope(ctx, tx, scope); err != nil {
			return errors.Join(err, repository.rollback(tx))
		}
		err = callback(&transaction{tx: tx, tenantID: scope.TenantID, projectID: scope.ProjectID, actorID: scope.ActorID})
		if err == nil {
			err = tx.Commit(ctx)
		} else {
			err = errors.Join(err, repository.rollback(tx))
		}
		if !retryable(err) {
			return mapError(err)
		}
		last = err
	}
	return fmt.Errorf("transaction retry exhausted: %w", last)
}

func (repository *Repository) NextExecutionScope(ctx context.Context) (domainrepo.Scope, bool, error) {
	var scope domainrepo.Scope
	err := repository.pool.QueryRow(ctx, sqlNextExecutionScope, pgx.StrictNamedArgs{}).Scan(&scope.TenantID, &scope.ProjectID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (scope.TenantID == "" || scope.ProjectID == "") {
		return domainrepo.Scope{}, false, nil
	}
	if err != nil {
		return domainrepo.Scope{}, false, mapError(err)
	}
	scope.ActorID = "system:integration-gateway-worker"
	return scope, true, nil
}

func (repository *Repository) NextLifecycleScope(ctx context.Context) (domainrepo.Scope, bool, error) {
	var scope domainrepo.Scope
	err := repository.pool.QueryRow(ctx, sqlNextLifecycleScope, pgx.StrictNamedArgs{}).Scan(&scope.TenantID, &scope.ProjectID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (scope.TenantID == "" || scope.ProjectID == "") {
		return domainrepo.Scope{}, false, nil
	}
	if err != nil {
		return domainrepo.Scope{}, false, mapError(err)
	}
	scope.ActorID = "system:integration-gateway-lifecycle"
	return scope, true, nil
}

func (repository *Repository) NextContinuationScope(ctx context.Context) (domainrepo.Scope, bool, error) {
	var scope domainrepo.Scope
	err := repository.pool.QueryRow(ctx, sqlNextContinuationScope, pgx.StrictNamedArgs{}).Scan(
		&scope.TenantID, &scope.ProjectID,
	)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (scope.TenantID == "" || scope.ProjectID == "") {
		return domainrepo.Scope{}, false, nil
	}
	if err != nil {
		return domainrepo.Scope{}, false, mapError(err)
	}
	scope.ActorID = "system:integration-gateway-continuation"
	return scope, true, nil
}

func (repository *Repository) ListTools(ctx context.Context, scope domainrepo.Scope, sessionID string) ([]domainrepo.ToolBinding, error) {
	bindings := make([]domainrepo.ToolBinding, 0)
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sqlToolsList, pgx.StrictNamedArgs{
			"transport_session_id": sessionID, "tenant_id": scope.TenantID, "project_id": scope.ProjectID,
		})
		if err != nil {
			return err
		}
		defer rows.Close()
		seen := make(map[string]struct{})
		for rows.Next() {
			var definitionRaw, connectionRaw, grantRaw []byte
			if err := rows.Scan(&definitionRaw, &connectionRaw, &grantRaw); err != nil {
				return err
			}
			var definition entity.Definition
			var connection entity.Connection
			var grant entity.Grant
			if json.Unmarshal(definitionRaw, &definition) != nil || json.Unmarshal(connectionRaw, &connection) != nil || json.Unmarshal(grantRaw, &grant) != nil {
				return errors.New("stored tool binding is invalid")
			}
			for _, tool := range definition.Tools {
				if !slices.Contains(grant.Capabilities, tool.Capability) || !slices.Contains(grant.Permissions, tool.Permission) {
					continue
				}
				if _, duplicate := seen[tool.Name]; duplicate {
					return errs.ErrConflict
				}
				seen[tool.Name] = struct{}{}
				bindings = append(bindings, domainrepo.ToolBinding{
					Tool: tool, Connection: connection, Grant: grant,
					DefinitionDigest: definition.Digest,
				})
			}
		}
		return rows.Err()
	})
	return bindings, err
}

func (repository *Repository) GetConnection(ctx context.Context, scope domainrepo.Scope, connectionID string) (entity.Connection, error) {
	var connection entity.Connection
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(ctx, sqlConnectionGet, pgx.StrictNamedArgs{
			"connection_id": connectionID, "tenant_id": scope.TenantID, "project_id": scope.ProjectID,
		}).Scan(&raw); err != nil {
			return err
		}
		if json.Unmarshal(raw, &connection) != nil {
			return errors.New("stored connection is invalid")
		}
		return nil
	})
	return connection, err
}

func (repository *Repository) GetInvocation(ctx context.Context, scope domainrepo.Scope, invocationID string) (entity.Invocation, *entity.Approval, *entity.Result, error) {
	var invocation entity.Invocation
	var approval *entity.Approval
	var result *entity.Result
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		var invocationRaw []byte
		var approvalRaw, resultRaw []byte
		if err := tx.QueryRow(ctx, sqlInvocationGet, pgx.StrictNamedArgs{
			"invocation_id": invocationID, "tenant_id": scope.TenantID, "project_id": scope.ProjectID,
		}).Scan(&invocationRaw, &approvalRaw, &resultRaw); err != nil {
			return err
		}
		if err := json.Unmarshal(invocationRaw, &invocation); err != nil {
			return errors.New("stored invocation is invalid")
		}
		if len(approvalRaw) > 0 {
			approval = &entity.Approval{}
			if err := json.Unmarshal(approvalRaw, approval); err != nil {
				return errors.New("stored approval is invalid")
			}
		}
		if len(resultRaw) > 0 {
			result = &entity.Result{}
			if err := json.Unmarshal(resultRaw, result); err != nil {
				return errors.New("stored result is invalid")
			}
		}
		return nil
	})
	return invocation, approval, result, err
}

func (repository *Repository) TouchSession(ctx context.Context, scope domainrepo.Scope, sessionID, tokenDigest string, now, expiresAt time.Time, maximumRequests uint64, maximumConcurrency uint32) (entity.TransportSession, error) {
	var session entity.TransportSession
	err := repository.write(ctx, scope, func(tx pgx.Tx) error {
		var raw []byte
		var acquired, tokenMatches bool
		var status enum.SessionStatus
		var requestCount uint64
		var concurrentRequests uint32
		var storedExpiresAt, lastSeenAt time.Time
		if err := tx.QueryRow(ctx, sqlSessionTouch, pgx.StrictNamedArgs{
			"transport_session_id": sessionID, "tenant_id": scope.TenantID, "project_id": scope.ProjectID,
			"token_digest": tokenDigest, "now": now, "expires_at": expiresAt,
			"maximum_requests": maximumRequests, "maximum_concurrency": maximumConcurrency,
		}).Scan(&raw, &status, &requestCount, &concurrentRequests, &storedExpiresAt, &lastSeenAt, &acquired, &tokenMatches); err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &session); err != nil {
			return errors.New("stored transport session is invalid")
		}
		session.Status = status
		session.RequestCount = requestCount
		session.ConcurrentRequests = concurrentRequests
		session.ExpiresAt = storedExpiresAt
		session.LastSeenAt = lastSeenAt
		if !acquired {
			if !tokenMatches {
				return errs.ErrForbidden
			}
			if session.Status == enum.SessionClosed || session.Status == enum.SessionExpired || !session.ExpiresAt.After(now) {
				return errs.ErrExpired
			}
			return errs.ErrQuotaExceeded
		}
		return nil
	})
	return session, err
}

func (repository *Repository) ReleaseSession(ctx context.Context, scope domainrepo.Scope, sessionID string) error {
	return repository.write(ctx, scope, func(tx pgx.Tx) error {
		var result string
		return tx.QueryRow(ctx, sqlSessionRelease, pgx.StrictNamedArgs{
			"transport_session_id": sessionID, "tenant_id": scope.TenantID, "project_id": scope.ProjectID,
		}).Scan(&result)
	})
}

func (repository *Repository) Check(ctx context.Context) error {
	var member, active, continuationReady bool
	if err := repository.pool.QueryRow(ctx, sqlReadinessCheck, pgx.StrictNamedArgs{}).Scan(
		&member, &active, &continuationReady,
	); err != nil || !member || !active || !continuationReady {
		return errors.New("integration gateway database is not ready")
	}
	return nil
}

func (repository *Repository) read(ctx context.Context, scope domainrepo.Scope, callback func(pgx.Tx) error) error {
	// Callback логически остается read-only, но активация подписанного RLS scope
	// сохраняет transaction-bound context и блокирует активный credential.
	return repository.withTransaction(ctx, scope, pgx.ReadCommitted, pgx.ReadWrite, callback)
}

func (repository *Repository) write(ctx context.Context, scope domainrepo.Scope, callback func(pgx.Tx) error) error {
	return repository.withTransaction(ctx, scope, pgx.Serializable, pgx.ReadWrite, callback)
}

func (repository *Repository) withTransaction(ctx context.Context, scope domainrepo.Scope, isolation pgx.TxIsoLevel, access pgx.TxAccessMode, callback func(pgx.Tx) error) error {
	scope = normalizeScope(scope)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: isolation, AccessMode: access})
	if err != nil {
		return mapError(err)
	}
	if err := repository.setScope(ctx, tx, scope); err != nil {
		return errors.Join(err, repository.rollback(tx))
	}
	if err := callback(tx); err != nil {
		return mapError(errors.Join(err, repository.rollback(tx)))
	}
	if err := tx.Commit(ctx); err != nil {
		return mapError(errors.Join(err, repository.rollback(tx)))
	}
	return nil
}

func (repository *Repository) rollback(tx pgx.Tx) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(repository.config.CleanupBase), repository.config.CleanupTimeout)
	defer cancel()
	if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return fmt.Errorf("rollback PostgreSQL transaction: %w", err)
	}
	return nil
}

func (repository *Repository) setScope(ctx context.Context, tx pgx.Tx, scope domainrepo.Scope) error {
	nonce := uuid.NewString()
	expires := time.Now().UTC().Add(repository.config.ContextTTL).UnixMicro()
	canonical := "v1\n" + repository.config.PrincipalName + "\n" + strconv.FormatUint(repository.config.PrincipalGeneration, 10) + "\n" +
		scope.TenantID + "\n" + scope.ProjectID + "\n" + scope.ActorID + "\n" + nonce + "\n" + strconv.FormatInt(expires, 10)
	mac := hmac.New(sha256.New, repository.config.ContextSigningKey)
	_, _ = mac.Write([]byte(canonical))
	_, err := tx.Exec(ctx, sqlTransactionSetScope, pgx.StrictNamedArgs{
		"tenant_id": scope.TenantID, "project_id": scope.ProjectID, "actor_id": scope.ActorID,
		"principal_name": repository.config.PrincipalName, "principal_generation": repository.config.PrincipalGeneration,
		"context_key_id": repository.config.ContextKeyID, "nonce": nonce, "expires_unix_micro": expires, "signature": mac.Sum(nil),
	})
	return mapError(err)
}

func normalizeScope(scope domainrepo.Scope) domainrepo.Scope {
	if scope.TenantID == "" {
		scope.TenantID = "system"
	}
	if scope.ProjectID == "" {
		scope.ProjectID = "system"
	}
	if scope.ActorID == "" {
		scope.ActorID = "system:integration-gateway"
	}
	return scope
}

func retryable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01")
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return errs.ErrConflict
		case "23503", "23514", "22P02":
			return errs.ErrInvalid
		case "28000", "42501":
			return errs.ErrForbidden
		}
	}
	return err
}
