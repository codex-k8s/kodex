package service

import (
	"context"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

func (s *Service) requireAllowed(ctx context.Context, meta value.CommandMeta, actionKey string, resource value.ResourceRef, scope value.ScopeRef) error {
	actor := value.SubjectRef{Type: strings.TrimSpace(meta.Actor.Type), ID: strings.TrimSpace(meta.Actor.ID)}
	if actor.Type == "" || actor.ID == "" {
		return errs.ErrInvalidArgument
	}
	decision, err := s.CheckAccess(ctx, CheckAccessInput{
		Subject:   actor,
		ActionKey: actionKey,
		Resource:  resource,
		Scope:     scope,
		Meta:      meta,
	})
	if err != nil {
		return err
	}
	if decision.Decision != enum.AccessDecisionAllow {
		return errs.ErrForbidden
	}
	return nil
}

func (s *Service) requireAllowedInAnyScope(ctx context.Context, meta value.CommandMeta, actionKey string, resource value.ResourceRef, scopes []value.ScopeRef) error {
	scopes = compactAccessScopes(scopes)
	if len(scopes) == 0 {
		scopes = []value.ScopeRef{{Type: accessRuleScopeGlobal}}
	}
	var firstErr error
	for _, scope := range scopes {
		err := s.requireAllowed(ctx, meta, actionKey, resource, scope)
		if err == nil {
			return nil
		}
		if !errors.Is(err, errs.ErrForbidden) {
			return err
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func compactAccessScopes(scopes []value.ScopeRef) []value.ScopeRef {
	result := make([]value.ScopeRef, 0, len(scopes))
	seen := make(map[value.ScopeRef]struct{}, len(scopes))
	for _, scope := range scopes {
		scope.Type = strings.TrimSpace(scope.Type)
		scope.ID = strings.TrimSpace(scope.ID)
		if scope.Type == "" {
			continue
		}
		if scope.Type == accessRuleScopeGlobal {
			scope.ID = ""
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		result = append(result, scope)
	}
	return result
}
