// Package access adapts access-manager checks to the runtime-manager domain.
package access

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/accesscheck"
	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
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

// NewConnection creates a lazy gRPC client connection to access-manager.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return accesscheck.NewConnection(cfg.Addr)
}

// NewAuthorizer wraps a generated access-manager client.
func NewAuthorizer(client accessaccountsv1.AccessManagerServiceClient, cfg Config) (*Authorizer, error) {
	settings := accesscheck.Config{AuthToken: cfg.AuthToken, CallerID: callerID, Timeout: cfg.Timeout}
	return accesscheck.NewAuthorizer(client, settings, runtimeAccessRequest, runtimeErrors())
}

func runtimeErrors() accesscheck.DomainErrors {
	errors := accesscheck.DomainErrors{}
	errors.InvalidRequest = errs.ErrInvalidArgument
	errors.Forbidden = errs.ErrForbidden
	errors.DependencyUnavailable = errs.ErrDependencyUnavailable
	return errors
}

func runtimeAccessRequest(request runtimeservice.AuthorizationRequest) accesscheck.Request {
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
