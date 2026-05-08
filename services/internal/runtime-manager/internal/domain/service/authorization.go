package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

// Authorizer checks whether the caller may access a runtime-manager resource.
type Authorizer interface {
	Authorize(context.Context, AuthorizationRequest) error
}

// AuthorizationRequest is the runtime-manager view of an access-manager check.
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

type authorizationInput struct {
	ctx            context.Context
	actor          value.Actor
	actionKey      string
	resource       resourceRef
	requestID      string
	requestContext value.RequestContext
}

func (s *Service) authorizeCommand(ctx context.Context, meta value.CommandMeta, actionKey string, resource resourceRef) error {
	return s.submitAuthorization(newAuthorizationInput(ctx, meta.Actor, actionKey, resource, meta.RequestID, meta.RequestContext))
}

func (s *Service) authorizeQuery(ctx context.Context, meta value.QueryMeta, actionKey string, resource resourceRef) error {
	return s.submitAuthorization(newAuthorizationInput(ctx, meta.Actor, actionKey, resource, meta.RequestID, meta.RequestContext))
}

func newAuthorizationInput(
	ctx context.Context,
	actor value.Actor,
	actionKey string,
	resource resourceRef,
	requestID string,
	requestContext value.RequestContext,
) authorizationInput {
	return authorizationInput{
		ctx:            ctx,
		actor:          actor,
		actionKey:      actionKey,
		resource:       resource,
		requestID:      requestID,
		requestContext: requestContext,
	}
}

func (s *Service) submitAuthorization(input authorizationInput) error {
	request, err := authorizationRequest(
		input.actor,
		input.actionKey,
		input.resource,
		input.requestID,
		input.requestContext,
	)
	if err != nil {
		return err
	}
	return s.authorizer.Authorize(input.ctx, request)
}

func authorizationRequest(
	actor value.Actor,
	actionKey string,
	resource resourceRef,
	requestID string,
	requestContext value.RequestContext,
) (AuthorizationRequest, error) {
	if strings.TrimSpace(actor.Type) == "" || strings.TrimSpace(actor.ID) == "" {
		return AuthorizationRequest{}, errs.ErrInvalidArgument
	}
	if strings.TrimSpace(actionKey) == "" || strings.TrimSpace(resource.Type) == "" {
		return AuthorizationRequest{}, errs.ErrInvalidArgument
	}
	var request AuthorizationRequest
	request.Subject = actor
	request.ActionKey = strings.TrimSpace(actionKey)
	request.ResourceType = strings.TrimSpace(resource.Type)
	request.ResourceID = strings.TrimSpace(resource.ID)
	request.ScopeType = strings.TrimSpace(resource.ScopeType)
	request.ScopeID = strings.TrimSpace(resource.ScopeID)
	request.RequestID = strings.TrimSpace(requestID)
	request.RequestContext = requestContext
	return request, nil
}

func slotResource(slotID uuid.UUID, projectID *uuid.UUID) resourceRef {
	return runtimeResource(accesscatalog.ResourceRuntimeSlot, slotID, projectID)
}

func workspaceResource(workspaceID uuid.UUID, projectID *uuid.UUID) resourceRef {
	return runtimeResource(accesscatalog.ResourceRuntimeWorkspace, workspaceID, projectID)
}

func jobResource(jobID uuid.UUID, projectID *uuid.UUID) resourceRef {
	return runtimeResource(accesscatalog.ResourceRuntimeJob, jobID, projectID)
}

func artifactResource(artifactID uuid.UUID, projectID *uuid.UUID) resourceRef {
	return runtimeResource(accesscatalog.ResourceRuntimeArtifactRef, artifactID, projectID)
}

func runtimeResource(resourceType string, resourceUUID uuid.UUID, projectID *uuid.UUID) resourceRef {
	resourceID := ""
	if resourceUUID != uuid.Nil {
		resourceID = resourceUUID.String()
	}
	scopeType := accesscatalog.ScopeGlobal
	scopeID := ""
	if projectID != nil {
		scopeType = accesscatalog.ScopeProject
		scopeID = projectID.String()
	}
	return resourceRef{Type: resourceType, ID: resourceID, ScopeType: scopeType, ScopeID: scopeID}
}
