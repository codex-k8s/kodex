package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/value"
)

// Authorizer checks whether the caller may access a fleet-manager resource.
type Authorizer interface {
	Authorize(context.Context, AuthorizationRequest) error
}

// AuthorizationRequest is the fleet-manager view of an access-manager check.
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

// AllowAllAuthorizer is used by tests and disabled local compositions.
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

func (s *Service) authorizeList(ctx context.Context, meta value.QueryMeta, actionKey string, resourceType string) error {
	return s.authorizeQuery(ctx, meta, actionKey, globalFleetResource(resourceType))
}

func (s *Service) authorize(ctx context.Context, actor value.Actor, actionKey string, resource resourceRef, requestID string, requestContext value.RequestContext) error {
	if strings.TrimSpace(actor.Type) == "" || strings.TrimSpace(actor.ID) == "" {
		return errs.ErrInvalidArgument
	}
	if strings.TrimSpace(actionKey) == "" || strings.TrimSpace(resource.Type) == "" {
		return errs.ErrInvalidArgument
	}
	return s.authorizer.Authorize(ctx, authorizationRequest(actor, actionKey, resource, requestID, requestContext))
}

func authorizationRequest(actor value.Actor, actionKey string, resource resourceRef, requestID string, requestContext value.RequestContext) AuthorizationRequest {
	request := AuthorizationRequest{Subject: actor, RequestContext: requestContext}
	request.ActionKey = strings.TrimSpace(actionKey)
	request.ResourceType = strings.TrimSpace(resource.Type)
	request.ResourceID = strings.TrimSpace(resource.ID)
	request.ScopeType = strings.TrimSpace(resource.ScopeType)
	request.ScopeID = strings.TrimSpace(resource.ScopeID)
	request.RequestID = strings.TrimSpace(requestID)
	return request
}

func fleetResource(resourceType string, resourceID uuid.UUID, scopeID *uuid.UUID) resourceRef {
	ref := resourceRef{Type: resourceType, ScopeType: accesscatalog.ScopeGlobal}
	if resourceID != uuid.Nil {
		ref.ID = resourceID.String()
	}
	// access-manager supports global/organization/project/repository scopes today.
	// Fleet scope itself is a resource, not an access-rule scope.
	_ = scopeID
	return ref
}

func globalFleetResource(resourceType string) resourceRef {
	return resourceRef{Type: resourceType, ScopeType: accesscatalog.ScopeGlobal}
}
