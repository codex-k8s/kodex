package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

func (s *Service) findCommandResult(ctx context.Context, meta value.CommandMeta, operation string, aggregateType string) (entity.CommandResult, bool, error) {
	result, found, err := s.loadCommandResult(ctx, meta, operation)
	switch {
	case err != nil:
		return entity.CommandResult{}, false, err
	case !found:
		return entity.CommandResult{}, false, nil
	case result.Operation != operation:
		return entity.CommandResult{}, false, errs.ErrConflict
	case result.AggregateType != aggregateType:
		return entity.CommandResult{}, false, errs.ErrConflict
	default:
		return result, true, nil
	}
}

func (s *Service) loadCommandResult(ctx context.Context, meta value.CommandMeta, operation string) (entity.CommandResult, bool, error) {
	identity, err := commandIdentity(meta, operation)
	if err != nil {
		return entity.CommandResult{}, false, err
	}
	result, err := s.repository.GetCommandResult(ctx, identity)
	if errors.Is(err, errs.ErrNotFound) {
		return entity.CommandResult{}, false, nil
	}
	return result, err == nil, err
}

func (s *Service) slotReplay(ctx context.Context, meta value.CommandMeta, operation string, expectedSlotID *uuid.UUID) (entity.Slot, bool, error) {
	return aggregateReplay(ctx, meta, operation, aggregateTypeSlot, expectedSlotID, s.findCommandResult, s.repository.GetSlot)
}

func aggregateReplay[T any](
	ctx context.Context,
	meta value.CommandMeta,
	operation string,
	aggregateType string,
	expectedID *uuid.UUID,
	find func(context.Context, value.CommandMeta, string, string) (entity.CommandResult, bool, error),
	load func(context.Context, uuid.UUID) (T, error),
) (T, bool, error) {
	var zero T
	result, ok, err := find(ctx, meta, operation, aggregateType)
	if err != nil || !ok {
		return zero, ok, err
	}
	if expectedID != nil && result.AggregateID != *expectedID {
		return zero, true, errs.ErrConflict
	}
	aggregate, err := load(ctx, result.AggregateID)
	return aggregate, true, err
}

func commandIdentity(meta value.CommandMeta, operation string) (query.CommandIdentity, error) {
	idempotencyKey := strings.TrimSpace(meta.IdempotencyKey)
	if meta.CommandID == uuid.Nil && idempotencyKey == "" {
		return query.CommandIdentity{}, errs.ErrInvalidArgument
	}
	actor := normalizedActor(meta.Actor)
	if actor.Type == "" || actor.ID == "" {
		return query.CommandIdentity{}, errs.ErrInvalidArgument
	}
	return query.CommandIdentity{CommandID: meta.CommandID, IdempotencyKey: idempotencyKey, Operation: operation, Actor: actor}, nil
}

func commandResult(meta value.CommandMeta, operation string, aggregateType string, aggregateID uuid.UUID, payload []byte, now time.Time) (entity.CommandResult, error) {
	idempotencyKey := strings.TrimSpace(meta.IdempotencyKey)
	if meta.CommandID == uuid.Nil && idempotencyKey == "" {
		return entity.CommandResult{}, errs.ErrInvalidArgument
	}
	actor := normalizedActor(meta.Actor)
	if actor.Type == "" || actor.ID == "" {
		return entity.CommandResult{}, errs.ErrInvalidArgument
	}
	if strings.TrimSpace(aggregateType) == "" {
		return entity.CommandResult{}, errs.ErrInvalidArgument
	}
	identityKey := meta.CommandID.String()
	if meta.CommandID == uuid.Nil {
		identityKey = operation + ":" + actor.Type + ":" + actor.ID + ":" + idempotencyKey
	}
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	return entity.CommandResult{
		Key:            identityKey,
		CommandID:      nullableUUID(meta.CommandID),
		IdempotencyKey: idempotencyKey,
		Actor:          actor,
		Operation:      operation,
		AggregateType:  strings.TrimSpace(aggregateType),
		AggregateID:    aggregateID,
		ResultPayload:  payload,
		CreatedAt:      now,
	}, nil
}

func normalizedActor(actor value.Actor) value.Actor {
	return value.Actor{Type: strings.TrimSpace(actor.Type), ID: strings.TrimSpace(actor.ID)}
}

func expectedVersion(meta value.CommandMeta) (int64, error) {
	if meta.ExpectedVersion == nil || *meta.ExpectedVersion < 1 {
		return 0, errs.ErrInvalidArgument
	}
	return *meta.ExpectedVersion, nil
}

func nullableUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}
