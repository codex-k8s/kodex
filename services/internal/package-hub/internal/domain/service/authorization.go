package service

import (
	"context"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

// Authorizer checks whether the actor may access a package-hub resource.
type Authorizer interface {
	Authorize(context.Context, AuthorizationRequest) error
}

// AuthorizationRequest is the package-hub view of an access-manager check.
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

// AllowAllAuthorizer is used in tests and local compositions where access-manager is not wired yet.
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

func (s *Service) authorize(ctx context.Context, actor value.Actor, actionKey string, resource resourceRef, requestID string, requestContext value.RequestContext) error {
	request, err := authorizationRequest(actor, actionKey, resource, requestID, requestContext)
	if err != nil {
		return err
	}
	return s.authorizer.Authorize(ctx, request)
}

func authorizationRequest(actor value.Actor, actionKey string, resource resourceRef, requestID string, requestContext value.RequestContext) (AuthorizationRequest, error) {
	actorType := strings.TrimSpace(actor.Type)
	actorID := strings.TrimSpace(actor.ID)
	actionKey = strings.TrimSpace(actionKey)
	resourceType := strings.TrimSpace(resource.Type)
	if actorType == "" || actorID == "" {
		return AuthorizationRequest{}, errs.ErrInvalidArgument
	}
	if actionKey == "" || resourceType == "" {
		return AuthorizationRequest{}, errs.ErrInvalidArgument
	}
	request := AuthorizationRequest{Subject: value.Actor{Type: actorType, ID: actorID}, RequestContext: requestContext}
	request.ActionKey = actionKey
	request.ResourceType = resourceType
	request.ResourceID = strings.TrimSpace(resource.ID)
	request.ScopeType = strings.TrimSpace(resource.ScopeType)
	request.ScopeID = strings.TrimSpace(resource.ScopeID)
	request.RequestID = strings.TrimSpace(requestID)
	return request, nil
}

func globalResource(resourceType string) resourceRef {
	return resourceRef{Type: resourceType, ScopeType: packageScopeGlobal}
}

func globalResourceWithID(resourceType string, id string) resourceRef {
	return resourceRef{Type: resourceType, ID: id, ScopeType: packageScopeGlobal}
}

func organizationScopedResource(resourceType string, id string, organizationID string) resourceRef {
	return resourceRef{Type: resourceType, ID: id, ScopeType: packageScopeOrganization, ScopeID: organizationID}
}
