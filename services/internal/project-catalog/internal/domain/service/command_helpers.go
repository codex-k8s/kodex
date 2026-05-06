package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

func (s *Service) findCommandResult(ctx context.Context, meta value.CommandMeta, operation string, aggregateType string) (entity.CommandResult, bool, error) {
	identity, err := commandIdentity(meta, operation)
	if err != nil {
		return entity.CommandResult{}, false, err
	}
	result, err := s.repository.GetCommandResult(ctx, identity)
	if errors.Is(err, errs.ErrNotFound) {
		return entity.CommandResult{}, false, nil
	}
	if err != nil {
		return entity.CommandResult{}, false, err
	}
	if result.Operation != operation || result.AggregateType != aggregateType {
		return entity.CommandResult{}, false, errs.ErrConflict
	}
	return result, true, nil
}

func findScopedCommandReplay[Aggregate any](
	s *Service,
	ctx context.Context,
	meta value.CommandMeta,
	operation string,
	aggregateType string,
	expectedScopeID uuid.UUID,
	load func(context.Context, uuid.UUID) (Aggregate, error),
	scopeID func(Aggregate) uuid.UUID,
) (Aggregate, bool, error) {
	var zero Aggregate
	result, ok, err := s.findCommandResult(ctx, meta, operation, aggregateType)
	if err != nil || !ok {
		return zero, ok, err
	}
	aggregate, err := load(ctx, result.AggregateID)
	if err != nil {
		return zero, true, err
	}
	if scopeID(aggregate) != expectedScopeID {
		return zero, true, errs.ErrConflict
	}
	return aggregate, true, nil
}

func commandIdentity(meta value.CommandMeta, operation string) (query.CommandIdentity, error) {
	if meta.CommandID == uuid.Nil && strings.TrimSpace(meta.IdempotencyKey) == "" {
		return query.CommandIdentity{}, errs.ErrInvalidArgument
	}
	return query.CommandIdentity{
		CommandID:      meta.CommandID,
		IdempotencyKey: strings.TrimSpace(meta.IdempotencyKey),
		Operation:      operation,
	}, nil
}

func commandResult(meta value.CommandMeta, operation string, aggregateType string, aggregateID uuid.UUID, now time.Time) (*entity.CommandResult, error) {
	if meta.CommandID == uuid.Nil && strings.TrimSpace(meta.IdempotencyKey) == "" {
		return nil, errs.ErrInvalidArgument
	}
	identityKey := meta.CommandID.String()
	if meta.CommandID == uuid.Nil {
		identityKey = operation + ":" + strings.TrimSpace(meta.IdempotencyKey)
	}
	return &entity.CommandResult{
		Key:            identityKey,
		CommandID:      meta.CommandID,
		IdempotencyKey: strings.TrimSpace(meta.IdempotencyKey),
		Operation:      operation,
		AggregateType:  aggregateType,
		AggregateID:    aggregateID,
		ResultPayload:  []byte("{}"),
		CreatedAt:      now,
	}, nil
}

func expectedVersion(meta value.CommandMeta) (int64, error) {
	if meta.ExpectedVersion == nil || *meta.ExpectedVersion < 1 {
		return 0, errs.ErrInvalidArgument
	}
	return *meta.ExpectedVersion, nil
}

func previousVersion(meta value.CommandMeta) (*int64, error) {
	if meta.ExpectedVersion == nil {
		return nil, nil
	}
	if *meta.ExpectedVersion < 1 {
		return nil, errs.ErrInvalidArgument
	}
	return meta.ExpectedVersion, nil
}
