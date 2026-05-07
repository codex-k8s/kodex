package provider

import (
	"context"
	"embed"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
)

// SQLFiles contains named SQL queries for the provider-hub PostgreSQL repository.
//
//go:embed sql/*.sql
var SQLFiles embed.FS

var _ providerrepo.Repository = (*Repository)(nil)

type database interface {
	execer
	queryer
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Repository stores provider-hub state in PostgreSQL.
type Repository struct {
	db database
}

// NewRepository creates a PostgreSQL repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	if pool == nil {
		panic("provider-hub postgres pool is required")
	}
	return &Repository{db: pool}
}

// Ping verifies connectivity to provider-hub storage.
func (r *Repository) Ping(ctx context.Context) error {
	pool, ok := r.db.(*pgxpool.Pool)
	if !ok {
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping provider-hub postgres: %w", err)
	}
	return nil
}

// UpsertAccountRuntimeState creates or updates provider-side account state.
func (r *Repository) UpsertAccountRuntimeState(ctx context.Context, state entity.ProviderAccountRuntimeState) (entity.ProviderAccountRuntimeState, error) {
	return queryOne(ctx, r.db, operationUpsertAccountRuntimeState, queryAccountRuntimeStateUpsert, accountRuntimeStateArgs(state), scanAccountRuntimeState)
}

// GetAccountRuntimeState returns provider-side account state.
func (r *Repository) GetAccountRuntimeState(ctx context.Context, lookup query.AccountRuntimeStateLookup) (entity.ProviderAccountRuntimeState, error) {
	return queryOne(ctx, r.db, operationGetAccountRuntimeState, queryAccountRuntimeStateGet, accountRuntimeStateLookupArgs(lookup), scanAccountRuntimeState)
}

// ListAccountRuntimeStates returns provider-side account states.
func (r *Repository) ListAccountRuntimeStates(ctx context.Context, filter query.AccountRuntimeStateFilter) ([]entity.ProviderAccountRuntimeState, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListAccountRuntimeStates, queryAccountRuntimeStateList, accountRuntimeStateFilterArgs(filter), scanAccountRuntimeState)
}

// RecordLimitSnapshot stores a provider limit snapshot and updates account runtime state atomically.
func (r *Repository) RecordLimitSnapshot(ctx context.Context, snapshot entity.ProviderLimitSnapshot, state entity.ProviderAccountRuntimeState) (entity.ProviderLimitSnapshot, error) {
	var stored entity.ProviderLimitSnapshot
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		if _, err := queryOne(ctx, tx, operationUpsertAccountRuntimeState, queryAccountRuntimeStateUpsert, accountRuntimeStateArgs(state), scanAccountRuntimeState); err != nil {
			return err
		}
		var recordErr error
		stored, recordErr = queryOne(ctx, tx, operationRecordLimitSnapshot, queryLimitSnapshotUpsert, limitSnapshotArgs(snapshot), scanLimitSnapshot)
		if errors.Is(recordErr, errs.ErrNotFound) {
			return errs.ErrConflict
		}
		return recordErr
	})
	if err != nil {
		return entity.ProviderLimitSnapshot{}, wrapError(operationRecordLimitSnapshot, err)
	}
	return stored, nil
}

// ListLimitSnapshots returns provider limit snapshots.
func (r *Repository) ListLimitSnapshots(ctx context.Context, filter query.LimitSnapshotFilter) ([]entity.ProviderLimitSnapshot, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListLimitSnapshots, queryLimitSnapshotList, limitSnapshotFilterArgs(filter), scanLimitSnapshot)
}

// RecordProviderOperation stores a provider operation audit record.
func (r *Repository) RecordProviderOperation(ctx context.Context, operation entity.ProviderOperation) (entity.ProviderOperation, error) {
	return queryOne(ctx, r.db, operationRecordProviderOperation, queryProviderOperationInsert, providerOperationArgs(operation), scanProviderOperation)
}

// ListProviderOperations returns provider operation audit records.
func (r *Repository) ListProviderOperations(ctx context.Context, filter query.ProviderOperationFilter) ([]entity.ProviderOperation, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListProviderOperations, queryProviderOperationList, providerOperationFilterArgs(filter), scanProviderOperation)
}

const (
	operationGetAccountRuntimeState    = "domain.Repository.GetAccountRuntimeState"
	operationListAccountRuntimeStates  = "domain.Repository.ListAccountRuntimeStates"
	operationUpsertAccountRuntimeState = "domain.Repository.UpsertAccountRuntimeState"
	operationRecordLimitSnapshot       = "domain.Repository.RecordLimitSnapshot"
	operationListLimitSnapshots        = "domain.Repository.ListLimitSnapshots"
	operationRecordProviderOperation   = "domain.Repository.RecordProviderOperation"
	operationListProviderOperations    = "domain.Repository.ListProviderOperations"
)

func queryOne[T any](ctx context.Context, db queryer, operation string, sql string, args pgx.NamedArgs, scan func(postgreslib.RowScanner) (T, error)) (T, error) {
	result, err := scan(db.QueryRow(ctx, sql, args))
	if err != nil {
		return result, wrapError(operation, err)
	}
	return result, nil
}

func queryPage[T any](ctx context.Context, db queryer, operation string, sql string, paging pageQueryArgs, scan func(postgreslib.RowScanner) (T, error)) ([]T, query.PageResult, error) {
	rows, err := db.Query(ctx, sql, paging.args)
	if err != nil {
		return nil, query.PageResult{}, wrapError(operation, err)
	}
	items, err := postgreslib.ScanRows(rows, scan)
	if err != nil {
		return nil, query.PageResult{}, wrapError(operation, err)
	}
	pageItems, page := pageResult(items, paging.limit, paging.nextOffset)
	return pageItems, page, nil
}
