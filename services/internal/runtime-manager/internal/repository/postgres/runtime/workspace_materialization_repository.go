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
	defaultWorkspaceMaterializationPageSize = int32(50)
	maxWorkspaceMaterializationPageSize     = int32(200)
)

// PrepareRuntime creates a slot, starts materialization and stores both events and command result atomically.
func (r *Repository) PrepareRuntime(
	ctx context.Context,
	slot entity.Slot,
	materialization entity.WorkspaceMaterialization,
	slotEvent entity.OutboxEvent,
	workspaceEvent entity.OutboxEvent,
	result entity.CommandResult,
) error {
	slotArgs, err := slotArgs(slot)
	if err != nil {
		return wrapError(operationPrepareRuntime, err)
	}
	workspaceArgs, err := workspaceMaterializationArgs(materialization)
	if err != nil {
		return wrapError(operationPrepareRuntime, err)
	}
	err = postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		return postgreslib.RunDistinctMutations(
			ctx,
			tx,
			errs.ErrConflict,
			postgreslib.Mutation{Query: querySlotInsert, Args: slotArgs, RequireAffected: true},
			postgreslib.Mutation{Query: queryWorkspaceMaterializationInsert, Args: workspaceArgs, RequireAffected: true},
			postgreslib.Mutation{Query: queryOutboxEventInsert, Args: outboxEventArgs(slotEvent), RequireAffected: true},
			postgreslib.Mutation{Query: queryOutboxEventInsert, Args: outboxEventArgs(workspaceEvent), RequireAffected: true},
			postgreslib.Mutation{Query: queryCommandResultInsert, Args: commandResultArgs(result), RequireAffected: true},
		)
	})
	return wrapError(operationPrepareRuntime, err)
}

// CreateWorkspaceMaterialization starts materialization in an existing slot atomically with the slot state update.
func (r *Repository) CreateWorkspaceMaterialization(
	ctx context.Context,
	slot entity.Slot,
	materialization entity.WorkspaceMaterialization,
	previousSlotVersion int64,
	event entity.OutboxEvent,
	result entity.CommandResult,
) error {
	slotArgs, err := slotUpdateArgs(slot, previousSlotVersion)
	if err != nil {
		return wrapError(operationCreateWorkspaceMaterialization, err)
	}
	workspaceArgs, err := workspaceMaterializationArgs(materialization)
	if err != nil {
		return wrapError(operationCreateWorkspaceMaterialization, err)
	}
	err = postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		return postgreslib.RunDistinctMutations(
			ctx,
			tx,
			errs.ErrConflict,
			postgreslib.Mutation{Query: querySlotUpdate, Args: slotArgs, RequireAffected: true},
			postgreslib.Mutation{Query: queryWorkspaceMaterializationInsert, Args: workspaceArgs, RequireAffected: true},
			postgreslib.Mutation{Query: queryOutboxEventInsert, Args: outboxEventArgs(event), RequireAffected: true},
			postgreslib.Mutation{Query: queryCommandResultInsert, Args: commandResultArgs(result), RequireAffected: true},
		)
	})
	return wrapError(operationCreateWorkspaceMaterialization, err)
}

// UpdateWorkspaceMaterialization stores materialization progress, slot state, optional event and command result atomically.
func (r *Repository) UpdateWorkspaceMaterialization(
	ctx context.Context,
	slot entity.Slot,
	materialization entity.WorkspaceMaterialization,
	previousSlotVersion int64,
	previousMaterializationVersion int64,
	event *entity.OutboxEvent,
	result entity.CommandResult,
) error {
	slotArgs, err := slotUpdateArgs(slot, previousSlotVersion)
	if err != nil {
		return wrapError(operationUpdateWorkspaceMaterialization, err)
	}
	workspaceArgs, err := workspaceMaterializationUpdateArgs(materialization, previousMaterializationVersion)
	if err != nil {
		return wrapError(operationUpdateWorkspaceMaterialization, err)
	}
	err = postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		mutations := []postgreslib.Mutation{
			{Query: querySlotUpdate, Args: slotArgs, RequireAffected: true},
			{Query: queryWorkspaceMaterializationUpdate, Args: workspaceArgs, RequireAffected: true},
			{Query: queryCommandResultInsert, Args: commandResultArgs(result), RequireAffected: true},
		}
		if event != nil {
			mutations = append(mutations, postgreslib.Mutation{Query: queryOutboxEventInsert, Args: outboxEventArgs(*event), RequireAffected: true})
		}
		return postgreslib.RunDistinctMutations(ctx, tx, errs.ErrConflict, mutations...)
	})
	return wrapError(operationUpdateWorkspaceMaterialization, err)
}

// GetWorkspaceMaterialization returns one materialization attempt by id.
func (r *Repository) GetWorkspaceMaterialization(ctx context.Context, id uuid.UUID) (entity.WorkspaceMaterialization, error) {
	materialization, err := queryOne(ctx, r.db, queryWorkspaceMaterializationGet, pgx.NamedArgs{"id": id}, scanWorkspaceMaterialization)
	return materialization, wrapError(operationGetWorkspaceMaterialization, err)
}

// ListWorkspaceMaterializations returns materialization attempts matching the filter and page.
func (r *Repository) ListWorkspaceMaterializations(ctx context.Context, filter query.WorkspaceMaterializationFilter) ([]entity.WorkspaceMaterialization, query.PageResult, error) {
	limit, offset, nextOffset := postgreslib.OffsetPageBounds(filter.Page.PageSize, filter.Page.PageToken, defaultWorkspaceMaterializationPageSize, maxWorkspaceMaterializationPageSize)
	args := pgx.NamedArgs{
		"slot_id":      postgreslib.NullableUUID(filter.SlotID),
		"agent_run_id": postgreslib.NullableUUID(filter.AgentRunID),
		"statuses":     postgreslib.StringValues(filter.Statuses),
		"limit":        limit + 1,
		"offset":       offset,
	}
	rows, err := r.db.Query(ctx, queryWorkspaceMaterializationList, args)
	if err != nil {
		return nil, query.PageResult{}, wrapError(operationListWorkspaceMaterializations, err)
	}
	items, err := postgreslib.ScanRows(rows, scanWorkspaceMaterialization)
	if err != nil {
		return nil, query.PageResult{}, wrapError(operationListWorkspaceMaterializations, err)
	}
	items, nextToken := postgreslib.TrimOffsetPage(items, limit, nextOffset)
	return items, query.PageResult{NextPageToken: nextToken}, nil
}

func workspaceMaterializationArgs(materialization entity.WorkspaceMaterialization) (pgx.NamedArgs, error) {
	sourcesJSON, err := json.Marshal(materialization.Sources)
	if err != nil {
		return nil, err
	}
	return pgx.NamedArgs{
		"id":                 materialization.ID,
		"slot_id":            materialization.SlotID,
		"status":             string(materialization.Status),
		"policy_digest":      materialization.PolicyDigest,
		"sources_json":       string(sourcesJSON),
		"fingerprint":        materialization.Fingerprint,
		"started_at":         postgreslib.NullableTime(materialization.StartedAt),
		"finished_at":        postgreslib.NullableTime(materialization.FinishedAt),
		"last_error_code":    materialization.LastErrorCode,
		"last_error_message": materialization.LastErrorMessage,
		"version":            materialization.Version,
		"created_at":         materialization.CreatedAt,
		"updated_at":         materialization.UpdatedAt,
	}, nil
}

func workspaceMaterializationUpdateArgs(materialization entity.WorkspaceMaterialization, previousVersion int64) (pgx.NamedArgs, error) {
	args, err := workspaceMaterializationArgs(materialization)
	if err != nil {
		return nil, err
	}
	args["previous_version"] = previousVersion
	return args, nil
}
