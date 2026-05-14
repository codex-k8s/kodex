// Package fleet implements the PostgreSQL repository for fleet-manager.
package fleet

import (
	"context"
	"embed"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/errs"
	fleetrepo "github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/repository/fleet"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/query"
)

// SQLFiles contains named SQL queries for the fleet-manager PostgreSQL repository.
//
//go:embed sql/*.sql
var SQLFiles embed.FS

var _ fleetrepo.Repository = (*Repository)(nil)

type database interface {
	Ping(ctx context.Context) error
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

// Repository persists fleet-manager aggregates in PostgreSQL.
type Repository struct {
	db database
}

var (
	operationAppendOutboxEvent                = repositoryOperation("AppendOutboxEvent")
	operationClaimOutboxEvents                = repositoryOperation("ClaimOutboxEvents")
	operationCreateFleetScope                 = repositoryOperation("CreateFleetScope")
	operationEnsurePlatformDefaultSeed        = repositoryOperation("EnsurePlatformDefaultSeed")
	operationGetClusterConnectivityCheck      = repositoryOperation("GetClusterConnectivityCheck")
	operationGetClusterHealthSnapshot         = repositoryOperation("GetClusterHealthSnapshot")
	operationGetCommandResult                 = repositoryOperation("GetCommandResult")
	operationGetFleetScope                    = repositoryOperation("GetFleetScope")
	operationGetKubernetesCluster             = repositoryOperation("GetKubernetesCluster")
	operationGetLatestClusterHealthSnapshot   = repositoryOperation("GetLatestClusterHealthSnapshot")
	operationGetPlacementDecision             = repositoryOperation("GetPlacementDecision")
	operationGetPlacementRule                 = repositoryOperation("GetPlacementRule")
	operationGetPlacementRuleByScopeKey       = repositoryOperation("GetPlacementRuleByScopeKey")
	operationGetServer                        = repositoryOperation("GetServer")
	operationListClusterHealthSnapshots       = repositoryOperation("ListClusterHealthSnapshots")
	operationListFleetScopes                  = repositoryOperation("ListFleetScopes")
	operationListKubernetesClusters           = repositoryOperation("ListKubernetesClusters")
	operationListPlacementDecisions           = repositoryOperation("ListPlacementDecisions")
	operationListPlacementRules               = repositoryOperation("ListPlacementRules")
	operationListServers                      = repositoryOperation("ListServers")
	operationMarkOutboxEventFailed            = repositoryOperation("MarkOutboxEventFailed")
	operationMarkOutboxEventPermanentlyFailed = repositoryOperation("MarkOutboxEventPermanentlyFailed")
	operationMarkOutboxEventPublished         = repositoryOperation("MarkOutboxEventPublished")
	operationPing                             = repositoryOperation("Ping")
	operationCreatePlacementDecision          = repositoryOperation("CreatePlacementDecision")
	operationCreatePlacementRule              = repositoryOperation("CreatePlacementRule")
	operationRegisterKubernetesCluster        = repositoryOperation("RegisterKubernetesCluster")
	operationRegisterServer                   = repositoryOperation("RegisterServer")
	operationStoreClusterHealthCheck          = repositoryOperation("StoreClusterHealthCheck")
	operationUpdateFleetScope                 = repositoryOperation("UpdateFleetScope")
	operationUpdateKubernetesCluster          = repositoryOperation("UpdateKubernetesCluster")
	operationUpdatePlacementRule              = repositoryOperation("UpdatePlacementRule")
	operationUpdateServer                     = repositoryOperation("UpdateServer")
)

func repositoryOperation(name string) string {
	return "domain.Repository." + name
}

// NewRepository creates a PostgreSQL-backed fleet repository.
func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// Ping checks that the fleet database is reachable.
func (r *Repository) Ping(ctx context.Context) error {
	return wrapError(operationPing, r.db.Ping(ctx))
}

// GetCommandResult returns an applied idempotent command result.
func (r *Repository) GetCommandResult(ctx context.Context, identity query.CommandIdentity) (entity.CommandResult, error) {
	return queryOne(ctx, r.db, operationGetCommandResult, queryCommandResultGet, commandIdentityArgs(identity), scanCommandResult)
}

// CreateFleetScope stores a new fleet scope, command result and event atomically.
func (r *Repository) CreateFleetScope(ctx context.Context, scope entity.FleetScope, event entity.OutboxEvent, result entity.CommandResult) error {
	return r.createWithCommandResult(ctx, operationCreateFleetScope, event, insertMutation(queryFleetScopeCreate, fleetScopeArgs(scope)), result)
}

// UpdateFleetScope stores a versioned fleet scope mutation, command result and event atomically.
func (r *Repository) UpdateFleetScope(ctx context.Context, scope entity.FleetScope, previousVersion int64, event entity.OutboxEvent, result entity.CommandResult) error {
	update := affectedMutation(queryFleetScopeUpdate, fleetScopeUpdateArgs(scope, previousVersion))
	return r.updateWithCommandResult(ctx, operationUpdateFleetScope, event, update, result)
}

// GetFleetScope returns a fleet scope by id.
func (r *Repository) GetFleetScope(ctx context.Context, id uuid.UUID) (entity.FleetScope, error) {
	return queryOne(ctx, r.db, operationGetFleetScope, queryFleetScopeGetByID, pgx.NamedArgs{"id": id}, scanFleetScope)
}

// ListFleetScopes returns fleet scopes by filter.
func (r *Repository) ListFleetScopes(ctx context.Context, filter query.FleetScopeFilter) ([]entity.FleetScope, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListFleetScopes, queryFleetScopeList, fleetScopeFilterArgs(filter), scanFleetScope)
}

// RegisterServer stores a new server, command result and event atomically.
func (r *Repository) RegisterServer(ctx context.Context, server entity.Server, event entity.OutboxEvent, result entity.CommandResult) error {
	return r.createWithCommandResult(ctx, operationRegisterServer, event, insertMutation(queryServerCreate, serverArgs(server)), result)
}

// UpdateServer stores a versioned server mutation, command result and event atomically.
func (r *Repository) UpdateServer(ctx context.Context, server entity.Server, previousVersion int64, event entity.OutboxEvent, result entity.CommandResult) error {
	args := serverUpdateArgs(server, previousVersion)
	return r.updateWithCommandResult(ctx, operationUpdateServer, event, affectedMutation(queryServerUpdate, args), result)
}

// GetServer returns a server by id.
func (r *Repository) GetServer(ctx context.Context, id uuid.UUID) (entity.Server, error) {
	return queryOne(ctx, r.db, operationGetServer, queryServerGetByID, pgx.NamedArgs{"id": id}, scanServer)
}

// ListServers returns servers by filter.
func (r *Repository) ListServers(ctx context.Context, filter query.ServerFilter) ([]entity.Server, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListServers, queryServerList, serverFilterArgs(filter), scanServer)
}

// RegisterKubernetesCluster stores a new Kubernetes cluster, command result and event atomically.
func (r *Repository) RegisterKubernetesCluster(ctx context.Context, cluster entity.KubernetesCluster, event entity.OutboxEvent, result entity.CommandResult) error {
	return r.createWithCommandResult(ctx, operationRegisterKubernetesCluster, event, insertMutation(queryKubernetesClusterCreate, kubernetesClusterArgs(cluster)), result)
}

// UpdateKubernetesCluster stores a versioned Kubernetes cluster mutation, command result and event atomically.
func (r *Repository) UpdateKubernetesCluster(ctx context.Context, cluster entity.KubernetesCluster, previousVersion int64, event entity.OutboxEvent, result entity.CommandResult) error {
	return r.updateWithCommandResult(ctx, operationUpdateKubernetesCluster, event, kubernetesClusterUpdate(cluster, previousVersion), result)
}

// GetKubernetesCluster returns a Kubernetes cluster by id.
func (r *Repository) GetKubernetesCluster(ctx context.Context, id uuid.UUID) (entity.KubernetesCluster, error) {
	return queryOne(ctx, r.db, operationGetKubernetesCluster, queryKubernetesClusterGetByID, pgx.NamedArgs{"id": id}, scanKubernetesCluster)
}

// ListKubernetesClusters returns Kubernetes clusters by filter.
func (r *Repository) ListKubernetesClusters(ctx context.Context, filter query.KubernetesClusterFilter) ([]entity.KubernetesCluster, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListKubernetesClusters, queryKubernetesClusterList, kubernetesClusterFilterArgs(filter), scanKubernetesCluster)
}

// StoreClusterHealthCheck stores connectivity check, health snapshot, latest cluster health, command result and events atomically.
func (r *Repository) StoreClusterHealthCheck(ctx context.Context, cluster entity.KubernetesCluster, check entity.ClusterConnectivityCheck, snapshot entity.ClusterHealthSnapshot, events []entity.OutboxEvent, result entity.CommandResult) error {
	err := r.withTx(ctx, operationStoreClusterHealthCheck, func(tx pgx.Tx) error {
		mutations := []mutation{
			insertMutation(queryClusterConnectivityCheckCreate, clusterConnectivityCheckArgs(check)),
			insertMutation(queryClusterHealthSnapshotCreate, clusterHealthSnapshotArgs(snapshot)),
			affectedMutation(queryKubernetesClusterUpdateHealth, kubernetesClusterHealthArgs(cluster)),
			commandResultMutation(result),
		}
		if err := runMutations(ctx, tx, mutations); err != nil {
			return err
		}
		for index := range events {
			if err := insertOutboxEvent(ctx, tx, events[index]); err != nil {
				return err
			}
		}
		return nil
	})
	return wrapError(operationStoreClusterHealthCheck, err)
}

// GetClusterConnectivityCheck returns one connectivity check by id.
func (r *Repository) GetClusterConnectivityCheck(ctx context.Context, id uuid.UUID) (entity.ClusterConnectivityCheck, error) {
	return queryOne(ctx, r.db, operationGetClusterConnectivityCheck, queryClusterConnectivityCheckGetByID, pgx.NamedArgs{"id": id}, scanClusterConnectivityCheck)
}

// GetClusterHealthSnapshot returns one health snapshot by id.
func (r *Repository) GetClusterHealthSnapshot(ctx context.Context, id uuid.UUID) (entity.ClusterHealthSnapshot, error) {
	return queryOne(ctx, r.db, operationGetClusterHealthSnapshot, queryClusterHealthSnapshotGetByID, pgx.NamedArgs{"id": id}, scanClusterHealthSnapshot)
}

// GetLatestClusterHealthSnapshot returns the newest health snapshot for one cluster.
func (r *Repository) GetLatestClusterHealthSnapshot(ctx context.Context, clusterID uuid.UUID) (entity.ClusterHealthSnapshot, error) {
	return queryOne(ctx, r.db, operationGetLatestClusterHealthSnapshot, queryClusterHealthSnapshotGetLatest, pgx.NamedArgs{"cluster_id": clusterID}, scanClusterHealthSnapshot)
}

// ListClusterHealthSnapshots returns health snapshots by filter.
func (r *Repository) ListClusterHealthSnapshots(ctx context.Context, filter query.ClusterHealthSnapshotFilter) ([]entity.ClusterHealthSnapshot, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListClusterHealthSnapshots, queryClusterHealthSnapshotList, clusterHealthSnapshotFilterArgs(filter), scanClusterHealthSnapshot)
}

// CreatePlacementRule stores a new placement rule and command result atomically.
func (r *Repository) CreatePlacementRule(ctx context.Context, rule entity.PlacementRule, result entity.CommandResult) error {
	return r.mutate(ctx, operationCreatePlacementRule, insertMutation(queryPlacementRuleCreate, placementRuleArgs(rule)), commandResultMutation(result))
}

// UpdatePlacementRule stores a versioned placement rule mutation and command result atomically.
func (r *Repository) UpdatePlacementRule(ctx context.Context, rule entity.PlacementRule, previousVersion int64, result entity.CommandResult) error {
	return r.mutate(ctx, operationUpdatePlacementRule, affectedMutation(queryPlacementRuleUpdate, placementRuleUpdateArgs(rule, previousVersion)), commandResultMutation(result))
}

// GetPlacementRule returns a placement rule by id.
func (r *Repository) GetPlacementRule(ctx context.Context, id uuid.UUID) (entity.PlacementRule, error) {
	return queryOne(ctx, r.db, operationGetPlacementRule, queryPlacementRuleGetByID, pgx.NamedArgs{"id": id}, scanPlacementRule)
}

// GetPlacementRuleByScopeKey returns a placement rule by scope and rule key.
func (r *Repository) GetPlacementRuleByScopeKey(ctx context.Context, fleetScopeID uuid.UUID, ruleKey string) (entity.PlacementRule, error) {
	return queryOne(ctx, r.db, operationGetPlacementRuleByScopeKey, queryPlacementRuleGetByScopeKey, placementRuleScopeKeyArgs(fleetScopeID, ruleKey), scanPlacementRule)
}

// ListPlacementRules returns placement rules by filter.
func (r *Repository) ListPlacementRules(ctx context.Context, filter query.PlacementRuleFilter) ([]entity.PlacementRule, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListPlacementRules, queryPlacementRuleList, placementRuleFilterArgs(filter), scanPlacementRule)
}

// CreatePlacementDecision stores one placement decision, command result and event atomically.
func (r *Repository) CreatePlacementDecision(ctx context.Context, decision entity.PlacementDecision, event entity.OutboxEvent, result entity.CommandResult) error {
	return r.createWithCommandResult(ctx, operationCreatePlacementDecision, event, insertMutation(queryPlacementDecisionCreate, placementDecisionArgs(decision)), result)
}

// GetPlacementDecision returns one placement decision by id.
func (r *Repository) GetPlacementDecision(ctx context.Context, id uuid.UUID) (entity.PlacementDecision, error) {
	return queryOne(ctx, r.db, operationGetPlacementDecision, queryPlacementDecisionGetByID, pgx.NamedArgs{"id": id}, scanPlacementDecision)
}

// ListPlacementDecisions returns placement decisions by filter.
func (r *Repository) ListPlacementDecisions(ctx context.Context, filter query.PlacementDecisionFilter) ([]entity.PlacementDecision, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListPlacementDecisions, queryPlacementDecisionList, placementDecisionFilterArgs(filter), scanPlacementDecision)
}

// EnsurePlatformDefaultSeed stores bootstrap default fleet data if it is absent.
func (r *Repository) EnsurePlatformDefaultSeed(ctx context.Context, scope entity.FleetScope, cluster entity.KubernetesCluster, events []entity.OutboxEvent) error {
	err := r.withTx(ctx, operationEnsurePlatformDefaultSeed, func(tx pgx.Tx) error {
		scopeTag, err := tx.Exec(ctx, queryFleetScopeSeedCreate, fleetScopeArgs(scope))
		if err != nil {
			return err
		}
		if scopeTag.RowsAffected() > 0 && len(events) > 0 {
			if err := insertOutboxEvent(ctx, tx, events[0]); err != nil {
				return err
			}
		}
		clusterTag, err := tx.Exec(ctx, queryKubernetesClusterSeedCreate, kubernetesClusterArgs(cluster))
		if err != nil {
			return err
		}
		if clusterTag.RowsAffected() > 0 && len(events) > 1 {
			if err := insertOutboxEvent(ctx, tx, events[1]); err != nil {
				return err
			}
		}
		return nil
	})
	return wrapError(operationEnsurePlatformDefaultSeed, err)
}

// AppendOutboxEvent stores one fleet domain event in the local outbox.
func (r *Repository) AppendOutboxEvent(ctx context.Context, event entity.OutboxEvent) error {
	tag, err := r.db.Exec(ctx, queryOutboxEventInsert, outboxEventArgs(event))
	if err != nil {
		return wrapError(operationAppendOutboxEvent, err)
	}
	if tag.RowsAffected() == 0 {
		return wrapError(operationAppendOutboxEvent, errs.ErrConflict)
	}
	return nil
}

// ClaimOutboxEvents leases unpublished outbox events for delivery.
func (r *Repository) ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error) {
	args, ok := postgreslib.OutboxClaimArgs(limit, now, lockedUntil)
	if !ok {
		return nil, wrapError(operationClaimOutboxEvents, errs.ErrInvalidArgument)
	}
	return r.claimOutboxEventsWithArgs(ctx, args)
}

func (r *Repository) claimOutboxEventsWithArgs(ctx context.Context, args pgx.NamedArgs) ([]entity.OutboxEvent, error) {
	rows, err := r.db.Query(ctx, queryOutboxEventClaim, args)
	if err != nil {
		return nil, wrapError(operationClaimOutboxEvents, err)
	}
	events, err := postgreslib.ScanRows(rows, scanOutboxEvent)
	if err != nil {
		return nil, wrapError(operationClaimOutboxEvents, err)
	}
	return events, nil
}

// MarkOutboxEventPublished marks a leased outbox event as published.
func (r *Repository) MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error {
	return r.finishPublishedOutboxEvent(ctx, outboxPublishedMutation{id: id, attempt: attemptCount, at: publishedAt})
}

// MarkOutboxEventFailed schedules a leased outbox event for retry.
func (r *Repository) MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error {
	return r.finishFailedOutboxEvent(ctx, operationMarkOutboxEventFailed, queryOutboxEventMarkFailed, "next_attempt_at", id, attemptCount, nextAttemptAt, lastError)
}

// MarkOutboxEventPermanentlyFailed moves a leased outbox event to terminal failure.
func (r *Repository) MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error {
	return r.finishFailedOutboxEvent(ctx, operationMarkOutboxEventPermanentlyFailed, queryOutboxEventMarkPermanentlyFailed, "failed_permanently_at", id, attemptCount, failedAt, lastError)
}

type outboxPublishedMutation struct {
	id      uuid.UUID
	attempt int
	at      time.Time
}

func (r *Repository) finishPublishedOutboxEvent(ctx context.Context, mutation outboxPublishedMutation) error {
	err := postgreslib.ApplyOutboxPublished(ctx, r.db, queryOutboxEventMarkPublished, errs.ErrInvalidArgument, mutation.id, mutation.attempt, mutation.at)
	return wrapError(operationMarkOutboxEventPublished, err)
}

func (r *Repository) finishFailedOutboxEvent(ctx context.Context, operation string, query string, timestampField string, id uuid.UUID, attempt int, at time.Time, message string) error {
	err := postgreslib.ApplyOutboxDeliveryFailure(ctx, r.db, query, errs.ErrInvalidArgument, id, attempt, timestampField, at, message)
	return wrapError(operation, err)
}

func (r *Repository) withTx(ctx context.Context, operation string, fn func(tx pgx.Tx) error) error {
	return wrapError(operation, postgreslib.WithTx(ctx, r.db, fn))
}

type mutation = postgreslib.Mutation

func (r *Repository) mutate(ctx context.Context, operation string, mutations ...mutation) error {
	run := func(tx pgx.Tx) error { return runMutations(ctx, tx, mutations) }
	return r.withTx(ctx, operation, run)
}

func (r *Repository) mutateWithOutbox(ctx context.Context, operation string, event entity.OutboxEvent, mutations ...mutation) error {
	mutations = append(mutations, affectedMutation(queryOutboxEventInsert, outboxEventArgs(event)))
	return r.mutate(ctx, operation, mutations...)
}

func (r *Repository) createWithCommandResult(ctx context.Context, operation string, event entity.OutboxEvent, create mutation, result entity.CommandResult) error {
	return r.mutateWithOutbox(ctx, operation, event, create, commandResultMutation(result))
}

func (r *Repository) updateWithCommandResult(ctx context.Context, operation string, event entity.OutboxEvent, update mutation, result entity.CommandResult) error {
	return r.createWithCommandResult(ctx, operation, event, update, result)
}

func commandResultMutation(result entity.CommandResult) mutation {
	return affectedMutation(queryCommandResultCreate, commandResultArgs(result))
}

func insertOutboxEvent(ctx context.Context, db execer, event entity.OutboxEvent) error {
	return postgreslib.RunMutation(ctx, db, errs.ErrConflict, affectedMutation(queryOutboxEventInsert, outboxEventArgs(event)))
}

func insertMutation(queryText string, args pgx.NamedArgs) mutation {
	return mutation{Query: queryText, Args: args}
}

func affectedMutation(queryText string, args pgx.NamedArgs) mutation {
	return mutation{Query: queryText, Args: args, RequireAffected: true}
}

func kubernetesClusterUpdate(cluster entity.KubernetesCluster, previousVersion int64) mutation {
	args := kubernetesClusterUpdateArgs(cluster, previousVersion)
	return affectedMutation(queryKubernetesClusterUpdate, args)
}

func runMutations(ctx context.Context, tx pgx.Tx, mutations []mutation) error {
	return postgreslib.RunDistinctMutations(ctx, tx, errs.ErrConflict, mutations...)
}

func queryOne[T any](ctx context.Context, db queryer, operation string, sql string, args pgx.NamedArgs, scan func(postgreslib.RowScanner) (T, error)) (T, error) {
	row := db.QueryRow(ctx, sql, args)
	value, scanErr := scan(row)
	return value, wrapError(operation, scanErr)
}

func queryMany[T any](ctx context.Context, db queryer, operation string, sql string, args pgx.NamedArgs, scan func(postgreslib.RowScanner) (T, error)) ([]T, error) {
	rows, err := db.Query(ctx, sql, args)
	if err != nil {
		return nil, wrapError(operation, err)
	}
	defer rows.Close()
	items := make([]T, 0)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, wrapError(operation, err)
		}
		items = append(items, item)
	}
	return items, wrapError(operation, rows.Err())
}

func queryPage[T any](ctx context.Context, db queryer, operation string, sql string, paging pageQueryArgs, scan func(postgreslib.RowScanner) (T, error)) ([]T, query.PageResult, error) {
	items, listErr := queryMany(ctx, db, operation, sql, paging.args, scan)
	if listErr != nil {
		emptyPage := query.PageResult{}
		return nil, emptyPage, listErr
	}
	values, page := pageResult(items, paging.limit, paging.nextOffset)
	return values, page, nil
}
