package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/value"
)

func (s *Service) findCommandResult(ctx context.Context, meta value.CommandMeta, operation string, aggregateType string) (entity.CommandResult, bool, error) {
	identity, err := commandIdentity(meta, operation)
	if err != nil {
		return entity.CommandResult{}, false, err
	}
	result, err := s.repository.GetCommandResult(ctx, identity)
	switch {
	case errors.Is(err, errs.ErrNotFound):
		return entity.CommandResult{}, false, nil
	case err != nil:
		return entity.CommandResult{}, false, err
	}
	return result, true, ensureResultMatches(result, operation, aggregateType)
}

func ensureResultMatches(result entity.CommandResult, operation string, aggregateType string) error {
	if result.Operation == operation && result.AggregateType == aggregateType {
		return nil
	}
	return errs.ErrConflict
}

func commandIdentity(meta value.CommandMeta, operation string) (query.CommandIdentity, error) {
	if err := validateCommandIdentity(meta); err != nil {
		return query.CommandIdentity{}, err
	}
	return query.CommandIdentity{
		CommandID:      meta.CommandID,
		IdempotencyKey: strings.TrimSpace(meta.IdempotencyKey),
		Operation:      operation,
		Actor:          meta.Actor,
	}, nil
}

func commandResult(meta value.CommandMeta, operation string, aggregateType string, aggregateID uuid.UUID, now time.Time) (entity.CommandResult, error) {
	if err := validateCommandIdentity(meta); err != nil {
		return entity.CommandResult{}, err
	}
	return entity.CommandResult{
		Key:            commandResultKey(meta, operation),
		CommandID:      commandIDPtr(meta.CommandID),
		IdempotencyKey: strings.TrimSpace(meta.IdempotencyKey),
		ActorType:      strings.TrimSpace(meta.Actor.Type),
		ActorID:        strings.TrimSpace(meta.Actor.ID),
		Operation:      operation,
		AggregateType:  aggregateType,
		AggregateID:    aggregateID,
		ResultPayload:  []byte("{}"),
		CreatedAt:      now,
	}, nil
}

func validateCommandIdentity(meta value.CommandMeta) error {
	if meta.CommandID == uuid.Nil && strings.TrimSpace(meta.IdempotencyKey) == "" {
		return errs.ErrInvalidArgument
	}
	if strings.TrimSpace(meta.Actor.Type) == "" || strings.TrimSpace(meta.Actor.ID) == "" {
		return errs.ErrInvalidArgument
	}
	return nil
}

func commandResultKey(meta value.CommandMeta, operation string) string {
	identityKey := meta.CommandID.String()
	if meta.CommandID == uuid.Nil {
		identityKey = operation + ":" + strings.TrimSpace(meta.Actor.Type) + ":" + strings.TrimSpace(meta.Actor.ID) + ":" + strings.TrimSpace(meta.IdempotencyKey)
	}
	return identityKey
}

func commandIDPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func expectedVersion(meta value.CommandMeta) (int64, error) {
	if meta.ExpectedVersion == nil || *meta.ExpectedVersion < 1 {
		return 0, errs.ErrInvalidArgument
	}
	return *meta.ExpectedVersion, nil
}
