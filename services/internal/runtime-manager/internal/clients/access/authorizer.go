// Package access adapts access-manager checks to the runtime-manager domain.
package access

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/accesscheck"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	runtimeservice "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/service"
	"google.golang.org/grpc"
)

const callerID = "runtime-manager"

// Config contains gRPC connection settings for access-manager.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

// Authorizer calls access-manager CheckAccess for runtime-manager commands and reads.
type Authorizer = accesscheck.Authorizer[runtimeservice.AuthorizationRequest]

var _ runtimeservice.Authorizer = (*Authorizer)(nil)

// NewConnectedAuthorizer creates the access-manager connection and authorizer.
func NewConnectedAuthorizer(cfg Config) (*Authorizer, *grpc.ClientConn, error) {
	return accesscheck.NewConnectedAuthorizer(accessSettings(cfg), runtimeAccessRequest, runtimeErrors())
}

func accessSettings(cfg Config) accesscheck.Config {
	return accesscheck.Config{Addr: cfg.Addr, AuthToken: cfg.AuthToken, CallerID: callerID, Timeout: cfg.Timeout}
}

func runtimeErrors() accesscheck.DomainErrors {
	errors := accesscheck.DomainErrors{}
	errors.InvalidRequest = errs.ErrInvalidArgument
	errors.Forbidden = errs.ErrForbidden
	errors.DependencyUnavailable = errs.ErrDependencyUnavailable
	return errors
}

func runtimeAccessRequest(request runtimeservice.AuthorizationRequest) accesscheck.Request {
	return accesscheck.Request{
		Subject:   accesscheck.Subject{Type: request.Subject.Type, ID: request.Subject.ID},
		ActionKey: request.ActionKey,
		Resource:  accesscheck.Resource{Type: request.ResourceType, ID: request.ResourceID},
		Scope:     accesscheck.Scope{Type: request.ScopeType, ID: request.ScopeID},
		RequestID: request.RequestID,
		RequestContext: accesscheck.RequestContext{
			Source:       request.RequestContext.Source,
			TraceID:      request.RequestContext.TraceID,
			SessionID:    request.RequestContext.SessionID,
			ClientIPHash: request.RequestContext.ClientIPHash,
		},
	}
}
