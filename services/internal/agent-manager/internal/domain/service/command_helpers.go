package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

type commandReplaySpec[T any] struct {
	Operation     string
	AggregateType enum.CommandAggregateType
	Decode        func([]byte) (T, error)
	Verify        func(context.Context, entity.CommandResult, T) error
}

func findCommandReplayByType[T any](ctx context.Context, service *Service, meta value.CommandMeta, spec commandReplaySpec[T]) (T, bool, error) {
	var zero T
	identity, err := commandIdentity(meta, spec.Operation)
	if err != nil {
		return zero, false, err
	}
	result, err := service.repository.GetCommandResult(ctx, identity)
	switch {
	case errors.Is(err, errs.ErrNotFound):
		return zero, false, nil
	case err != nil:
		return zero, false, err
	}
	if !matchesReplay(result, spec.Operation, spec.AggregateType) {
		return zero, true, errs.ErrConflict
	}
	replay, err := spec.Decode(result.ResultPayload)
	if err != nil {
		return zero, true, err
	}
	if spec.Verify != nil {
		if err := spec.Verify(ctx, result, replay); err != nil {
			return zero, true, err
		}
	}
	return replay, true, nil
}

func findReplay[T any](ctx context.Context, service *Service, meta value.CommandMeta, operation string, aggregateType enum.CommandAggregateType, decode func([]byte) (T, error), verify func(context.Context, entity.CommandResult, T) error) (T, bool, error) {
	return findCommandReplayByType(ctx, service, meta, commandReplaySpec[T]{
		Operation:     operation,
		AggregateType: aggregateType,
		Decode:        decode,
		Verify:        verify,
	})
}

func matchesReplay(result entity.CommandResult, operation string, aggregateType enum.CommandAggregateType) bool {
	return result.Operation == operation && result.AggregateType == aggregateType
}

func commandIdentity(meta value.CommandMeta, operation string) (query.CommandIdentity, error) {
	idempotencyKey := strings.TrimSpace(meta.IdempotencyKey)
	if meta.CommandID == uuid.Nil && idempotencyKey == "" {
		return query.CommandIdentity{}, errs.ErrInvalidArgument
	}
	if strings.TrimSpace(meta.Actor.Type) == "" || strings.TrimSpace(meta.Actor.ID) == "" {
		return query.CommandIdentity{}, errs.ErrInvalidArgument
	}
	identity := query.CommandIdentity{Operation: operation, IdempotencyKey: idempotencyKey, Actor: meta.Actor}
	if meta.CommandID == uuid.Nil {
		return identity, nil
	}
	identity.CommandID = &meta.CommandID
	return identity, nil
}

func commandResult(meta value.CommandMeta, operation string, aggregateType enum.CommandAggregateType, aggregateID uuid.UUID, payload []byte, now time.Time) (entity.CommandResult, error) {
	identity, err := commandIdentity(meta, operation)
	if err != nil {
		return entity.CommandResult{}, errs.ErrInvalidArgument
	}
	return entity.CommandResult{
		Key:            commandResultKey(identity),
		CommandID:      identity.CommandID,
		IdempotencyKey: identity.IdempotencyKey,
		Actor:          identity.Actor,
		Operation:      operation,
		AggregateType:  aggregateType,
		AggregateID:    aggregateID,
		ResultPayload:  payload,
		CreatedAt:      now,
	}, nil
}

func commandResultKey(identity query.CommandIdentity) string {
	actor := identity.Actor.Type + ":" + identity.Actor.ID
	if identity.CommandID != nil {
		return identity.Operation + ":" + actor + ":" + identity.CommandID.String()
	}
	return identity.Operation + ":" + actor + ":" + identity.IdempotencyKey
}

func expectedVersion(meta value.CommandMeta) (int64, error) {
	if meta.ExpectedVersion == nil || *meta.ExpectedVersion < 1 {
		return 0, errs.ErrInvalidArgument
	}
	return *meta.ExpectedVersion, nil
}

func marshalCommandPayload(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return payload, nil
}
