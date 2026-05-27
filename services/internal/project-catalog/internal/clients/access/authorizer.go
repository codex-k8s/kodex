// Package access adapts access-manager checks to the project-catalog domain.
package access

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/accesscheck"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	projectservice "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/service"
	"google.golang.org/grpc"
)

const callerID = "project-catalog"

// Config contains gRPC connection settings for access-manager.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

// Authorizer calls access-manager CheckAccess for project-catalog commands and reads.
type Authorizer = accesscheck.Authorizer[projectservice.AuthorizationRequest]

var _ projectservice.Authorizer = (*Authorizer)(nil)

// NewConnectedAuthorizer creates the access-manager connection and authorizer.
func NewConnectedAuthorizer(cfg Config) (*Authorizer, *grpc.ClientConn, error) {
	return accesscheck.NewConnectedAuthorizer(accessSettings(cfg), projectAccessRequest, projectErrors())
}

func accessSettings(cfg Config) accesscheck.Config {
	return accesscheck.Config{Addr: cfg.Addr, AuthToken: cfg.AuthToken, CallerID: callerID, Timeout: cfg.Timeout}
}

func projectErrors() accesscheck.DomainErrors {
	return accesscheck.DomainErrors{
		InvalidRequest:        errs.ErrInvalidArgument,
		Forbidden:             errs.ErrForbidden,
		DependencyUnavailable: errs.ErrDependencyUnavailable,
	}
}

func projectAccessRequest(request projectservice.AuthorizationRequest) accesscheck.Request {
	return accesscheck.NewRequest(accesscheck.RequestFields{
		SubjectType:  request.Subject.Type,
		SubjectID:    request.Subject.ID,
		ActionKey:    request.ActionKey,
		ResourceType: request.ResourceType,
		ResourceID:   request.ResourceID,
		ScopeType:    request.ScopeType,
		ScopeID:      request.ScopeID,
		RequestID:    request.RequestID,
		Context: accesscheck.RequestContext{
			Source:       request.RequestContext.Source,
			TraceID:      request.RequestContext.TraceID,
			SessionID:    request.RequestContext.SessionID,
			ClientIPHash: request.RequestContext.ClientIPHash,
		},
	})
}
