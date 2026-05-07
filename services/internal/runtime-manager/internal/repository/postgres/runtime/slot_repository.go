package runtime

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/query"
)

const (
	defaultSlotPageSize = int32(50)
	maxSlotPageSize     = int32(200)
)

// GetCommandResult returns the aggregate linked to an idempotent command.
func (r *Repository) GetCommandResult(ctx context.Context, identity query.CommandIdentity) (entity.CommandResult, error) {
	args := pgx.NamedArgs{
		"command_id":      postgreslib.NullableCommandID(identity.CommandID),
		"idempotency_key": postgreslib.IdempotencyLookupKey(identity.CommandID, identity.IdempotencyKey),
		"actor_type":      identity.Actor.Type,
		"actor_id":        identity.Actor.ID,
		"operation":       identity.Operation,
	}
	rows, err := r.db.Query(ctx, queryCommandResultGet, args)
	if err != nil {
		return entity.CommandResult{}, wrapError(operationGetCommandResult, err)
	}
	result, err := pgx.CollectExactlyOneRow(rows, func(row pgx.CollectableRow) (entity.CommandResult, error) {
		return scanCommandResult(row)
	})
	return result, wrapError(operationGetCommandResult, err)
}

// CreateSlot stores a new slot, its command result and its outbox event atomically.
func (r *Repository) CreateSlot(ctx context.Context, slot entity.Slot, event entity.OutboxEvent, result entity.CommandResult) error {
	slotArgs, err := slotArgs(slot)
	if err != nil {
		return wrapError(operationCreateSlot, err)
	}
	err = postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		return postgreslib.RunDistinctMutations(
			ctx,
			tx,
			errs.ErrConflict,
			postgreslib.Mutation{Query: querySlotInsert, Args: slotArgs, RequireAffected: true},
			postgreslib.Mutation{Query: queryOutboxEventInsert, Args: outboxEventArgs(event), RequireAffected: true},
			postgreslib.Mutation{Query: queryCommandResultInsert, Args: commandResultArgs(result), RequireAffected: true},
		)
	})
	return wrapError(operationCreateSlot, err)
}

// UpdateSlot stores an existing slot mutation, its outbox event and optional command result atomically.
func (r *Repository) UpdateSlot(ctx context.Context, slot entity.Slot, previousVersion int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	slotArgs, err := slotUpdateArgs(slot, previousVersion)
	if err != nil {
		return wrapError(operationUpdateSlot, err)
	}
	err = postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		mutations := []postgreslib.Mutation{
			{Query: querySlotUpdate, Args: slotArgs, RequireAffected: true},
			{Query: queryOutboxEventInsert, Args: outboxEventArgs(event), RequireAffected: true},
		}
		if result != nil {
			mutations = append(mutations, postgreslib.Mutation{Query: queryCommandResultInsert, Args: commandResultArgs(*result), RequireAffected: true})
		}
		return postgreslib.RunDistinctMutations(ctx, tx, errs.ErrConflict, mutations...)
	})
	return wrapError(operationUpdateSlot, err)
}

// GetSlot returns one slot by id.
func (r *Repository) GetSlot(ctx context.Context, id uuid.UUID) (entity.Slot, error) {
	rows, err := r.db.Query(ctx, querySlotGet, pgx.NamedArgs{"id": id})
	if err != nil {
		return entity.Slot{}, wrapError(operationGetSlot, err)
	}
	slot, err := pgx.CollectExactlyOneRow(rows, func(row pgx.CollectableRow) (entity.Slot, error) {
		return scanSlot(row)
	})
	return slot, wrapError(operationGetSlot, err)
}

// ListSlots returns slots matching the filter and page.
func (r *Repository) ListSlots(ctx context.Context, filter query.SlotFilter) ([]entity.Slot, query.PageResult, error) {
	limit, offset, nextOffset := postgreslib.OffsetPageBounds(filter.Page.PageSize, filter.Page.PageToken, defaultSlotPageSize, maxSlotPageSize)
	args := pgx.NamedArgs{
		"project_id":      postgreslib.NullableUUID(filter.ProjectID),
		"statuses":        postgreslib.StringValues(filter.Statuses),
		"runtime_profile": filter.RuntimeProfile,
		"fleet_scope_id":  postgreslib.NullableUUID(filter.FleetScopeID),
		"agent_run_id":    postgreslib.NullableUUID(filter.AgentRunID),
		"limit":           limit + 1,
		"offset":          offset,
	}
	rows, err := r.db.Query(ctx, querySlotList, args)
	if err != nil {
		return nil, query.PageResult{}, wrapError(operationListSlots, err)
	}
	slots, err := postgreslib.ScanRows(rows, scanSlot)
	if err != nil {
		return nil, query.PageResult{}, wrapError(operationListSlots, err)
	}
	slots, nextToken := postgreslib.TrimOffsetPage(slots, limit, nextOffset)
	return slots, query.PageResult{NextPageToken: nextToken}, nil
}

func slotArgs(slot entity.Slot) (pgx.NamedArgs, error) {
	repositoryIDs, err := json.Marshal(slot.RepositoryIDs)
	if err != nil {
		return nil, err
	}
	return pgx.NamedArgs{
		"id":                  slot.ID,
		"slot_key":            slot.SlotKey,
		"status":              string(slot.Status),
		"runtime_mode":        string(slot.RuntimeMode),
		"is_prewarmed":        slot.IsPrewarmed,
		"fleet_scope_id":      postgreslib.NullableUUID(slot.FleetScopeID),
		"cluster_id":          postgreslib.NullableUUID(slot.ClusterID),
		"namespace_name":      slot.NamespaceName,
		"agent_run_id":        postgreslib.NullableUUID(slot.AgentRunID),
		"project_id":          postgreslib.NullableUUID(slot.ProjectID),
		"repository_ids_json": string(repositoryIDs),
		"runtime_profile":     slot.RuntimeProfile,
		"fingerprint":         slot.Fingerprint,
		"lease_owner":         slot.LeaseOwner,
		"lease_until":         postgreslib.NullableTime(slot.LeaseUntil),
		"last_error_code":     slot.LastErrorCode,
		"last_error_message":  slot.LastErrorMessage,
		"version":             slot.Version,
		"created_at":          slot.CreatedAt,
		"updated_at":          slot.UpdatedAt,
	}, nil
}

func slotUpdateArgs(slot entity.Slot, previousVersion int64) (pgx.NamedArgs, error) {
	args, err := slotArgs(slot)
	if err != nil {
		return nil, err
	}
	args["previous_version"] = previousVersion
	return args, nil
}

func outboxEventArgs(event entity.OutboxEvent) pgx.NamedArgs {
	nextAttemptAt := event.NextAttemptAt
	if nextAttemptAt.IsZero() {
		nextAttemptAt = event.OccurredAt
	}
	return pgx.NamedArgs{
		"id":              event.ID,
		"event_type":      event.EventType,
		"schema_version":  event.SchemaVersion,
		"aggregate_type":  event.AggregateType,
		"aggregate_id":    event.AggregateID,
		"payload":         string(event.Payload),
		"occurred_at":     event.OccurredAt,
		"next_attempt_at": nextAttemptAt,
	}
}

func commandResultArgs(result entity.CommandResult) pgx.NamedArgs {
	args := make(pgx.NamedArgs, 10)
	args["key"] = result.Key
	args["command_id"] = postgreslib.NullableUUID(result.CommandID)
	args["idempotency_key"] = result.IdempotencyKey
	args["actor_type"] = result.Actor.Type
	args["actor_id"] = result.Actor.ID
	args["operation"] = result.Operation
	args["aggregate_type"] = result.AggregateType
	args["aggregate_id"] = result.AggregateID
	args["result_payload"] = postgreslib.JSONPayload(result.ResultPayload)
	args["created_at"] = result.CreatedAt
	return args
}
