// Package access adapts access-manager checks to the fleet-manager domain.
package access

import (
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/accesscheck"
	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/errs"
	fleetservice "github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/service"
	"google.golang.org/grpc"
)

const callerID = "fleet-manager"

// Config contains gRPC connection settings for access-manager.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

// Authorizer calls access-manager CheckAccess for fleet-manager commands and reads.
type Authorizer = accesscheck.Authorizer[fleetservice.AuthorizationRequest]

var _ fleetservice.Authorizer = (*Authorizer)(nil)

// NewConnection creates a lazy gRPC client connection to access-manager.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return accesscheck.NewConnection(strings.TrimSpace(cfg.Addr))
}

// NewAuthorizer wraps a generated access-manager client.
func NewAuthorizer(client accessaccountsv1.AccessManagerServiceClient, cfg Config) (*Authorizer, error) {
	settings := accesscheck.Config{
		AuthToken: cfg.AuthToken,
		CallerID:  callerID,
		Timeout:   cfg.Timeout,
	}
	domainErrors := accesscheck.DomainErrors{
		InvalidRequest:        errs.ErrInvalidArgument,
		Forbidden:             errs.ErrForbidden,
		DependencyUnavailable: errs.ErrDependencyUnavailable,
	}
	return accesscheck.NewAuthorizer(client, settings, accessRequest, domainErrors)
}

func accessRequest(request fleetservice.AuthorizationRequest) accesscheck.Request {
	fields := accesscheck.RequestFields{}
	fields.SubjectType = request.Subject.Type
	fields.SubjectID = request.Subject.ID
	fields.ActionKey = request.ActionKey
	fields.ResourceType = request.ResourceType
	fields.ResourceID = request.ResourceID
	fields.ScopeType = request.ScopeType
	fields.ScopeID = request.ScopeID
	fields.RequestID = request.RequestID
	fields.Context.Source = request.RequestContext.Source
	fields.Context.TraceID = request.RequestContext.TraceID
	fields.Context.SessionID = request.RequestContext.SessionID
	fields.Context.ClientIPHash = request.RequestContext.ClientIPHash
	return accesscheck.NewRequest(fields)
}
