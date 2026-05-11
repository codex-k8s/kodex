package runtime

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	runtimerepo "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/repository/runtime"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/query"
)

// ClaimReusableSlot atomically reserves one safe prewarmed or ready slot for deterministic reuse.
func (r *Repository) ClaimReusableSlot(ctx context.Context, filter query.ReusableSlotFilter, recordFactory runtimerepo.SlotReuseRecordFactory) (entity.Slot, error) {
	return claimOneWithEventAndResult(ctx, r.db, operationClaimReusableSlot, querySlotClaimReusable, reusableSlotArgs(filter), scanSlot, recordFactory)
}

// CreateCleanupPolicy stores a cleanup policy and command result atomically.
func (r *Repository) CreateCleanupPolicy(ctx context.Context, policy entity.CleanupPolicy, result entity.CommandResult) error {
	return r.mutateRecordWithCommandResult(ctx, operationCreateCleanupPolicy, queryCleanupPolicyInsert, cleanupPolicyArgs(policy), result)
}

// UpdateCleanupPolicy stores cleanup policy changes and command result atomically.
func (r *Repository) UpdateCleanupPolicy(ctx context.Context, policy entity.CleanupPolicy, previousVersion int64, result entity.CommandResult) error {
	return r.mutateRecordWithCommandResult(ctx, operationUpdateCleanupPolicy, queryCleanupPolicyUpdate, cleanupPolicyUpdateArgs(policy, previousVersion), result)
}

// GetCleanupPolicy returns one cleanup policy by id.
func (r *Repository) GetCleanupPolicy(ctx context.Context, id uuid.UUID) (entity.CleanupPolicy, error) {
	return getByID(ctx, r.db, id, queryCleanupPolicyGet, operationGetCleanupPolicy, scanCleanupPolicy)
}

// RunCleanupBatch claims expired runtime objects, records cleanup events and command result atomically.
func (r *Repository) RunCleanupBatch(ctx context.Context, filter query.CleanupBatchFilter, recordFactory runtimerepo.CleanupBatchRecordFactory) (runtimerepo.CleanupBatchResult, error) {
	var result runtimerepo.CleanupBatchResult
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		policies, err := r.loadCleanupPolicies(ctx, tx, filter)
		if err != nil {
			return err
		}
		remaining := filter.Limit
		for _, policy := range policies {
			if remaining <= 0 {
				break
			}
			cleaned, failed, err := r.claimCleanupSlots(ctx, tx, policy, filter, remaining)
			if err != nil {
				return err
			}
			result.CleanedSlots = append(result.CleanedSlots, cleaned...)
			result.FailedSlots = append(result.FailedSlots, failed...)
			remaining -= len(cleaned) + len(failed)
		}
		result.CleanedCount = len(result.CleanedSlots)
		result.FailedCount = len(result.FailedSlots)
		result.ClaimedCount = result.CleanedCount + result.FailedCount
		result.AffectedSlotIDs = affectedCleanupSlotIDs(result)
		events, command, err := recordFactory(result)
		if err != nil {
			return err
		}
		for _, event := range events {
			if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, postgreslib.Mutation{Query: queryOutboxEventInsert, Args: outboxEventArgs(event), RequireAffected: true}); err != nil {
				return err
			}
		}
		return postgreslib.RunMutation(ctx, tx, errs.ErrConflict, postgreslib.Mutation{Query: queryCommandResultInsert, Args: commandResultArgs(command), RequireAffected: true})
	})
	return result, wrapError(operationRunCleanupBatch, err)
}

// CreatePrewarmPool stores a prewarm pool and command result atomically.
func (r *Repository) CreatePrewarmPool(ctx context.Context, pool entity.PrewarmPool, result entity.CommandResult) error {
	return r.mutateRecordWithCommandResult(ctx, operationCreatePrewarmPool, queryPrewarmPoolInsert, prewarmPoolArgs(pool), result)
}

// UpdatePrewarmPool stores prewarm pool changes and command result atomically.
func (r *Repository) UpdatePrewarmPool(ctx context.Context, pool entity.PrewarmPool, previousVersion int64, result entity.CommandResult) error {
	args := prewarmPoolArgs(pool)
	return r.mutateRecordWithCommandResult(ctx, operationUpdatePrewarmPool, queryPrewarmPoolUpdate, setPreviousVersion(args, previousVersion), result)
}

// GetPrewarmPool returns one prewarm pool by id.
func (r *Repository) GetPrewarmPool(ctx context.Context, id uuid.UUID) (entity.PrewarmPool, error) {
	return getByID(ctx, r.db, id, queryPrewarmPoolGet, operationGetPrewarmPool, scanPrewarmPool)
}

// ReconcilePrewarmPool changes actual prewarmed slots toward target capacity atomically.
func (r *Repository) ReconcilePrewarmPool(ctx context.Context, filter query.PrewarmPoolReconcileFilter, recordFactory runtimerepo.PrewarmPoolReconcileRecordFactory) (entity.PrewarmPool, error) {
	var reconciled entity.PrewarmPool
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		pool, err := queryOne(ctx, tx, queryPrewarmPoolGetForUpdate, pgx.NamedArgs{"id": filter.PrewarmPoolID}, scanPrewarmPool)
		if err != nil {
			return err
		}
		currentSize, err := r.countPrewarmSlots(ctx, tx, pool)
		if err != nil {
			return err
		}
		excessSlots, err := r.listExcessPrewarmSlots(ctx, tx, pool, currentSize)
		if err != nil {
			return err
		}
		record, events, command, err := recordFactory(runtimerepo.PrewarmPoolReconcileState{
			Pool:        pool,
			CurrentSize: currentSize,
			ExcessSlots: excessSlots,
		})
		if err != nil {
			return err
		}
		args := prewarmPoolArgs(record.Pool)
		args["previous_version"] = pool.Version
		if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, postgreslib.Mutation{Query: queryPrewarmPoolUpdate, Args: args, RequireAffected: true}); err != nil {
			return err
		}
		for _, slot := range record.CreatedSlots {
			args, err := slotArgs(slot)
			if err != nil {
				return err
			}
			if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, postgreslib.Mutation{Query: querySlotInsert, Args: args, RequireAffected: true}); err != nil {
				return err
			}
		}
		for _, slot := range record.CleanupSlots {
			args, err := slotUpdateArgs(slot, slot.Version-1)
			if err != nil {
				return err
			}
			if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, postgreslib.Mutation{Query: querySlotUpdate, Args: args, RequireAffected: true}); err != nil {
				return err
			}
		}
		for _, event := range events {
			if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, postgreslib.Mutation{Query: queryOutboxEventInsert, Args: outboxEventArgs(event), RequireAffected: true}); err != nil {
				return err
			}
		}
		if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, postgreslib.Mutation{Query: queryCommandResultInsert, Args: commandResultArgs(command), RequireAffected: true}); err != nil {
			return err
		}
		reconciled = record.Pool
		return nil
	})
	return reconciled, wrapError(operationReconcilePrewarmPool, err)
}

func reusableSlotArgs(filter query.ReusableSlotFilter) pgx.NamedArgs {
	repositoryIDs, err := json.Marshal(filter.RepositoryIDs)
	if err != nil {
		repositoryIDs = []byte("[]")
	}
	return pgx.NamedArgs{
		"runtime_profile":     filter.RuntimeProfile,
		"runtime_mode":        string(filter.RuntimeMode),
		"fingerprint":         filter.Fingerprint,
		"agent_run_id":        postgreslib.NullableUUID(filter.AgentRunID),
		"project_id":          postgreslib.NullableUUID(filter.ProjectID),
		"repository_ids_json": string(repositoryIDs),
		"fleet_scope_id":      postgreslib.NullableUUID(filter.FleetScopeID),
		"cluster_id":          postgreslib.NullableUUID(filter.ClusterID),
		"lease_owner":         filter.LeaseOwner,
		"lease_until":         filter.LeaseUntil,
		"now":                 filter.Now,
	}
}

func cleanupPolicyArgs(policy entity.CleanupPolicy) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                  policy.ID,
		"scope_type":          string(policy.ScopeType),
		"scope_id":            policy.ScopeID,
		"ttl_seconds":         policy.TTLSeconds,
		"failed_ttl_seconds":  policy.FailedTTLSeconds,
		"keep_short_log_tail": policy.KeepShortLogTail,
		"status":              string(policy.Status),
		"created_at":          policy.CreatedAt,
		"updated_at":          policy.UpdatedAt,
		"version":             policy.Version,
	}
}

func cleanupPolicyUpdateArgs(policy entity.CleanupPolicy, previousVersion int64) pgx.NamedArgs {
	return setPreviousVersion(cleanupPolicyArgs(policy), previousVersion)
}

func prewarmPoolArgs(pool entity.PrewarmPool) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                   pool.ID,
		"scope_type":           string(pool.ScopeType),
		"scope_id":             pool.ScopeID,
		"runtime_profile":      pool.RuntimeProfile,
		"fleet_scope_id":       postgreslib.NullableUUID(pool.FleetScopeID),
		"target_size":          pool.TargetSize,
		"status":               string(pool.Status),
		"last_capacity_status": string(pool.LastCapacityStatus),
		"created_at":           pool.CreatedAt,
		"updated_at":           pool.UpdatedAt,
		"version":              pool.Version,
	}
}

func setPreviousVersion(args pgx.NamedArgs, previousVersion int64) pgx.NamedArgs {
	args["previous_version"] = previousVersion
	return args
}

func (r *Repository) loadCleanupPolicies(ctx context.Context, tx pgx.Tx, filter query.CleanupBatchFilter) ([]entity.CleanupPolicy, error) {
	rows, err := tx.Query(ctx, queryCleanupPolicyListActive, pgx.NamedArgs{"id": postgreslib.NullableUUID(filter.CleanupPolicyID)})
	if err != nil {
		return nil, err
	}
	policies, err := postgreslib.ScanRows(rows, scanCleanupPolicy)
	if err != nil {
		return nil, err
	}
	if filter.CleanupPolicyID != nil && len(policies) == 0 {
		return nil, errs.ErrNotFound
	}
	return policies, nil
}

func (r *Repository) claimCleanupSlots(ctx context.Context, tx pgx.Tx, policy entity.CleanupPolicy, filter query.CleanupBatchFilter, limit int) ([]entity.Slot, []entity.Slot, error) {
	args := cleanupSlotClaimArgs(policy, filter, limit)
	cleanRows, err := tx.Query(ctx, queryCleanupSlotClaimCleanable, args)
	if err != nil {
		return nil, nil, err
	}
	cleaned, err := postgreslib.ScanRows(cleanRows, scanSlot)
	if err != nil {
		return nil, nil, err
	}
	if !policy.KeepShortLogTail {
		for _, slot := range cleaned {
			if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, postgreslib.Mutation{
				Query: queryCleanupSlotScrubJobTails,
				Args:  pgx.NamedArgs{"slot_id": slot.ID, "now": filter.Now},
			}); err != nil {
				return nil, nil, err
			}
		}
	}
	remaining := limit - len(cleaned)
	if remaining <= 0 {
		return cleaned, nil, nil
	}
	blockedRows, err := tx.Query(ctx, queryCleanupSlotClaimBlocked, cleanupSlotClaimArgs(policy, filter, remaining))
	if err != nil {
		return nil, nil, err
	}
	failed, err := postgreslib.ScanRows(blockedRows, scanSlot)
	return cleaned, failed, err
}

func cleanupSlotClaimArgs(policy entity.CleanupPolicy, filter query.CleanupBatchFilter, limit int) pgx.NamedArgs {
	return pgx.NamedArgs{
		"scope_type":         string(policy.ScopeType),
		"scope_id":           policy.ScopeID,
		"ttl_seconds":        policy.TTLSeconds,
		"failed_ttl_seconds": policy.FailedTTLSeconds,
		"limit":              limit,
		"now":                filter.Now,
	}
}

func affectedCleanupSlotIDs(result runtimerepo.CleanupBatchResult) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(result.CleanedSlots)+len(result.FailedSlots))
	for _, slot := range result.CleanedSlots {
		ids = append(ids, slot.ID)
	}
	for _, slot := range result.FailedSlots {
		ids = append(ids, slot.ID)
	}
	return ids
}

func (r *Repository) countPrewarmSlots(ctx context.Context, tx pgx.Tx, pool entity.PrewarmPool) (int64, error) {
	count, err := queryOne(ctx, tx, queryPrewarmPoolCountSlots, prewarmPoolScopeArgs(pool, 0), func(row postgreslib.RowScanner) (int64, error) {
		var value int64
		if err := row.Scan(&value); err != nil {
			return 0, err
		}
		return value, nil
	})
	return count, err
}

func (r *Repository) listExcessPrewarmSlots(ctx context.Context, tx pgx.Tx, pool entity.PrewarmPool, currentSize int64) ([]entity.Slot, error) {
	excess := currentSize - pool.TargetSize
	if excess <= 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, queryPrewarmPoolListExcessSlots, prewarmPoolScopeArgs(pool, excess))
	if err != nil {
		return nil, err
	}
	return postgreslib.ScanRows(rows, scanSlot)
}

func prewarmPoolScopeArgs(pool entity.PrewarmPool, limit int64) pgx.NamedArgs {
	return pgx.NamedArgs{
		"scope_type":      string(pool.ScopeType),
		"scope_id":        pool.ScopeID,
		"runtime_profile": pool.RuntimeProfile,
		"fleet_scope_id":  postgreslib.NullableUUID(pool.FleetScopeID),
		"limit":           limit,
	}
}
