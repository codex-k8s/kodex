package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

func (s *Service) requireRepository() error {
	if s == nil || s.repository == nil {
		return errs.ErrPreconditionFailed
	}
	return nil
}

func validateScope(scope value.ScopeRef) error {
	if strings.TrimSpace(scope.Type) == "" || strings.TrimSpace(scope.Ref) == "" {
		return errs.ErrInvalidArgument
	}
	return nil
}

func sameScope(left value.ScopeRef, right value.ScopeRef) bool {
	return left.Type == right.Type && left.Ref == right.Ref
}

func validateSlug(slug string) error {
	if strings.TrimSpace(slug) == "" {
		return errs.ErrInvalidArgument
	}
	return nil
}

func validateID(id uuid.UUID) error {
	if id == uuid.Nil {
		return errs.ErrInvalidArgument
	}
	return nil
}

func getByID[T any](ctx context.Context, s *Service, id uuid.UUID, getter func(context.Context, uuid.UUID) (T, error)) (T, error) {
	var zero T
	if err := s.requireRepository(); err != nil {
		return zero, err
	}
	if err := validateID(id); err != nil {
		return zero, err
	}
	return getter(ctx, id)
}

func listFromRepository[T any, F any](ctx context.Context, s *Service, filter F, lister func(context.Context, F) ([]T, value.PageResult, error)) ([]T, value.PageResult, error) {
	if err := s.requireRepository(); err != nil {
		return nil, value.PageResult{}, err
	}
	return lister(ctx, filter)
}

func normalizeObjectPayload(payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return []byte("{}"), nil
	}
	var object map[string]any
	if err := json.Unmarshal(payload, &object); err != nil {
		return nil, errs.ErrInvalidArgument
	}
	return payload, nil
}
