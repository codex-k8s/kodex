// Package access adapts access-manager checks to the governance-manager domain.
package access

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/accesscheck"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	governanceservice "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/service"
	"google.golang.org/grpc"
)

const callerID = "governance-manager"

// Config contains gRPC connection settings for access-manager.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

// Authorizer calls access-manager CheckAccess for governance-manager commands and reads.
type Authorizer = accesscheck.Authorizer[governanceservice.AuthorizationRequest]

var _ governanceservice.Authorizer = (*Authorizer)(nil)

// NewConnectedAuthorizer creates the access-manager connection and authorizer.
func NewConnectedAuthorizer(cfg Config) (*Authorizer, *grpc.ClientConn, error) {
	return accesscheck.NewConnectedAuthorizer(accessSettings(cfg), governanceAccessRequest, governanceErrors())
}

func accessSettings(cfg Config) accesscheck.Config {
	return accesscheck.Config{Addr: cfg.Addr, AuthToken: cfg.AuthToken, CallerID: callerID, Timeout: cfg.Timeout}
}

func governanceErrors() accesscheck.DomainErrors {
	return accesscheck.DomainErrors{
		InvalidRequest:        errs.ErrInvalidArgument,
		Forbidden:             errs.ErrForbidden,
		DependencyUnavailable: errs.ErrDependencyUnavailable,
	}
}

func governanceAccessRequest(request governanceservice.AuthorizationRequest) accesscheck.Request {
	return accesscheck.NewRequestFromValues(
		request.Subject.Type,
		request.Subject.ID,
		request.ActionKey,
		request.ResourceType,
		request.ResourceID,
		request.ScopeType,
		request.ScopeID,
		request.RequestID,
		request.RequestContext.Source,
		request.RequestContext.TraceID,
		request.RequestContext.SessionID,
		request.RequestContext.ClientIPHash,
	)
}
