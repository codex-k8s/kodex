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
