// Package access adapts access-manager checks to the package-hub domain.
package access

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/accesscheck"
	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
	packageservice "github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/service"
	"google.golang.org/grpc"
)

const callerID = "package-hub"

// Config contains access-manager client settings.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

// Authorizer delegates package-hub access decisions to access-manager.
type Authorizer = accesscheck.Authorizer[packageservice.AuthorizationRequest]

var _ packageservice.Authorizer = (*Authorizer)(nil)

// NewConnection creates a gRPC client connection to access-manager.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return accesscheck.NewConnection(cfg.Addr)
}

// NewAuthorizer wraps an access-manager gRPC client.
func NewAuthorizer(client accessaccountsv1.AccessManagerServiceClient, cfg Config) (*Authorizer, error) {
	return accesscheck.NewAuthorizer(client, accesscheck.Config{
		AuthToken: cfg.AuthToken,
		CallerID:  callerID,
		Timeout:   cfg.Timeout,
	}, packageAccessRequest, packageErrors())
}

func packageErrors() accesscheck.DomainErrors {
	return accesscheck.DomainErrors{
		InvalidRequest:        errs.ErrInvalidArgument,
		Forbidden:             errs.ErrForbidden,
		DependencyUnavailable: errs.ErrDependencyUnavailable,
	}
}

func packageAccessRequest(request packageservice.AuthorizationRequest) accesscheck.Request {
	context := accesscheck.RequestContext{}
	context.Source = request.RequestContext.Source
	context.TraceID = request.RequestContext.TraceID
	context.SessionID = request.RequestContext.SessionID
	context.ClientIPHash = request.RequestContext.ClientIPHash
	return accesscheck.NewRequest(accesscheck.RequestFields{
		SubjectType:  request.Subject.Type,
		SubjectID:    request.Subject.ID,
		ActionKey:    request.ActionKey,
		ResourceType: request.ResourceType,
		ResourceID:   request.ResourceID,
		ScopeType:    request.ScopeType,
		ScopeID:      request.ScopeID,
		RequestID:    request.RequestID,
		Context:      context,
	})
}
