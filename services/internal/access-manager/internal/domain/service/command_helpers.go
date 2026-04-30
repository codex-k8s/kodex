package service

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

func (s *Service) findCommandResult(
	ctx context.Context,
	meta value.CommandMeta,
	operation string,
	aggregateType string,
) (entity.CommandResult, bool, error) {
	identity, err := commandIdentity(meta)
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
