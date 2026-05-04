package service

import (
	"context"
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
