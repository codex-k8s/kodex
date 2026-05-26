// Package access adapts access-manager checks to the governance-manager domain.
package access

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/accesscheck"
	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
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

// NewConnection creates a lazy gRPC client connection to access-manager.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return accesscheck.NewConnection(cfg.Addr)
}

// NewAuthorizer wraps a generated access-manager client.
func NewAuthorizer(client accessaccountsv1.AccessManagerServiceClient, cfg Config) (*Authorizer, error) {
	settings := accesscheck.Config{AuthToken: cfg.AuthToken, CallerID: callerID, Timeout: cfg.Timeout}
	return accesscheck.NewAuthorizer(client, settings, governanceAccessRequest, governanceErrors())
}

func governanceErrors() accesscheck.DomainErrors {
	return accesscheck.DomainErrors{
		InvalidRequest:        errs.ErrInvalidArgument,
		Forbidden:             errs.ErrForbidden,
		DependencyUnavailable: errs.ErrDependencyUnavailable,
	}
}

func governanceAccessRequest(request governanceservice.AuthorizationRequest) accesscheck.Request {
	fields := accesscheck.RequestFields{
		SubjectType:  request.Subject.Type,
		SubjectID:    request.Subject.ID,
		ActionKey:    request.ActionKey,
		ResourceType: request.ResourceType,
		ResourceID:   request.ResourceID,
		ScopeType:    request.ScopeType,
		ScopeID:      request.ScopeID,
		RequestID:    request.RequestID,
	}
	fields.Context.Source = request.RequestContext.Source
	fields.Context.TraceID = request.RequestContext.TraceID
	fields.Context.SessionID = request.RequestContext.SessionID
	fields.Context.ClientIPHash = request.RequestContext.ClientIPHash
	return accesscheck.NewRequest(fields)
}
