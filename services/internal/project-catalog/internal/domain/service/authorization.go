package service

import (
	"context"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

// Authorizer checks whether the command actor may access a project-catalog resource.
type Authorizer interface {
	Authorize(context.Context, AuthorizationRequest) error
}

// AuthorizationRequest is the project-catalog view of an access-manager check.
type AuthorizationRequest struct {
	Subject        value.Actor
	ActionKey      string
	ResourceType   string
	ResourceID     string
	ScopeType      string
	ScopeID        string
	RequestID      string
	RequestContext value.RequestContext
}

// AllowAllAuthorizer is used in unit tests and local compositions where access-manager is not wired yet.
type AllowAllAuthorizer struct{}

// Authorize allows the request without side effects.
func (AllowAllAuthorizer) Authorize(context.Context, AuthorizationRequest) error {
	return nil
}

func (s *Service) authorizeCommand(ctx context.Context, meta value.CommandMeta, actionKey string, resource resourceRef) error {
	return s.authorize(ctx, meta.Actor, actionKey, resource, meta.RequestID, meta.RequestContext)
}

func (s *Service) authorizeQuery(ctx context.Context, meta value.QueryMeta, actionKey string, resource resourceRef) error {
	return s.authorize(ctx, meta.Actor, actionKey, resource, meta.RequestID, meta.RequestContext)
}

func (s *Service) authorize(
	ctx context.Context,
	actor value.Actor,
	actionKey string,
	resource resourceRef,
	requestID string,
	requestContext value.RequestContext,
) error {
	if strings.TrimSpace(actor.Type) == "" || strings.TrimSpace(actor.ID) == "" {
		return errs.ErrInvalidArgument
	}
	if strings.TrimSpace(actionKey) == "" || strings.TrimSpace(resource.Type) == "" {
		return errs.ErrInvalidArgument
	}
	return s.authorizer.Authorize(ctx, AuthorizationRequest{
		Subject:        actor,
		ActionKey:      strings.TrimSpace(actionKey),
		ResourceType:   strings.TrimSpace(resource.Type),
		ResourceID:     strings.TrimSpace(resource.ID),
		ScopeType:      strings.TrimSpace(resource.ScopeType),
		ScopeID:        strings.TrimSpace(resource.ScopeID),
		RequestID:      strings.TrimSpace(requestID),
		RequestContext: requestContext,
	})
}
