package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

const (
	actionGateRequest = accesscatalog.ActionGovernanceGateRequest
	actionGateRead    = accesscatalog.ActionGovernanceGateRead
	actionGateDecide  = accesscatalog.ActionGovernanceGateDecide
)

// Authorizer checks whether the caller may access governance-manager state.
type Authorizer interface {
	Authorize(context.Context, AuthorizationRequest) error
}

// AuthorizationRequest is the governance-manager view of an access-manager check.
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

type resourceRef struct {
	Type      string
	ID        string
	ScopeType string
	ScopeID   string
}

func (s *Service) authorizeCommand(ctx context.Context, meta CommandMeta, actionKey string, resource resourceRef) error {
	return s.authorize(ctx, meta.Actor, actionKey, resource, meta.RequestID, meta.RequestContext)
}

func (s *Service) authorizeQuery(ctx context.Context, meta QueryMeta, actionKey string, resource resourceRef) error {
	return s.authorize(ctx, meta.Actor, actionKey, resource, meta.RequestID, meta.RequestContext)
}

func (s *Service) authorize(ctx context.Context, actor value.Actor, actionKey string, resource resourceRef, requestID string, requestContext value.RequestContext) error {
	if strings.TrimSpace(actor.Type) == "" || strings.TrimSpace(actor.ID) == "" {
		return errs.ErrInvalidArgument
	}
	if s.authorizer == nil {
		return errs.ErrDependencyUnavailable
	}
	actionKey = strings.TrimSpace(actionKey)
	resource.Type = strings.TrimSpace(resource.Type)
	resource.ID = strings.TrimSpace(resource.ID)
	resource.ScopeType = strings.TrimSpace(resource.ScopeType)
	resource.ScopeID = strings.TrimSpace(resource.ScopeID)
	if actionKey == "" || resource.Type == "" || resource.ID == "" || resource.ScopeType == "" {
		return errs.ErrInvalidArgument
	}
	if resource.ScopeType != accesscatalog.ScopeGlobal && resource.ScopeID == "" {
		return errs.ErrInvalidArgument
	}
	return s.authorizer.Authorize(ctx, AuthorizationRequest{
		Subject:        actor,
		ActionKey:      actionKey,
		ResourceType:   resource.Type,
		ResourceID:     resource.ID,
		ScopeType:      resource.ScopeType,
		ScopeID:        resource.ScopeID,
		RequestID:      strings.TrimSpace(requestID),
		RequestContext: requestContext,
	})
}

func gateResource(id uuid.UUID) resourceRef {
	resourceID := ""
	if id != uuid.Nil {
		resourceID = id.String()
	}
	return resourceRef{Type: accesscatalog.ResourceGovernanceGate, ID: resourceID, ScopeType: accesscatalog.ScopeGlobal}
}

func gateTargetResource(target value.ExternalRef) resourceRef {
	resource := gateResource(uuid.Nil)
	resource.ID = strings.TrimSpace(target.Ref)
	return resource
}
