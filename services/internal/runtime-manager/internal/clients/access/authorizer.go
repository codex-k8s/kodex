// Package access adapts access-manager checks to the runtime-manager domain.
package access

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	runtimeservice "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	callerTypeService = "service"
	callerID          = "runtime-manager"
)

// Config contains gRPC connection settings for access-manager.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

// Authorizer calls access-manager CheckAccess for runtime-manager commands and reads.
type Authorizer struct {
	client    accessaccountsv1.AccessManagerServiceClient
	authToken string
	timeout   time.Duration
}

var _ runtimeservice.Authorizer = (*Authorizer)(nil)

// NewConnection creates a lazy gRPC client connection to access-manager.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return newAccessManagerConnection(strings.TrimSpace(cfg.Addr))
}

func newAccessManagerConnection(addr string) (*grpc.ClientConn, error) {
	if addr == "" {
		return nil, fmt.Errorf("access-manager address is required")
	}
	return grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// NewAuthorizer wraps a generated access-manager client.
func NewAuthorizer(client accessaccountsv1.AccessManagerServiceClient, cfg Config) (*Authorizer, error) {
	switch {
	case client == nil:
		return nil, fmt.Errorf("access-manager client is required")
	case strings.TrimSpace(cfg.AuthToken) == "":
		return nil, fmt.Errorf("access-manager auth token is required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &Authorizer{client: client, authToken: strings.TrimSpace(cfg.AuthToken), timeout: timeout}, nil
}

// Authorize denies runtime work unless access-manager returns an allow decision.
func (a *Authorizer) Authorize(ctx context.Context, request runtimeservice.AuthorizationRequest) error {
	if err := validateRequest(request); err != nil {
		return err
	}
	checkCtx, cancel := a.checkContext(ctx)
	defer cancel()
	response, err := a.client.CheckAccess(checkCtx, checkAccessRequest(request))
	if err != nil {
		return mapAccessError(err)
	}
	if response.GetDecision() != accessaccountsv1.AccessDecision_ACCESS_DECISION_ALLOW {
		return errs.ErrForbidden
	}
	return nil
}

func (a *Authorizer) checkContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(outgoingAuthContext(ctx, a.authToken), a.timeout)
}

func validateRequest(request runtimeservice.AuthorizationRequest) error {
	if hasBlankRequiredField(
		request.Subject.Type,
		request.Subject.ID,
		request.ActionKey,
		request.ResourceType,
	) {
		return errs.ErrInvalidArgument
	}
	return nil
}

func hasBlankRequiredField(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return true
		}
	}
	return false
}

func outgoingAuthContext(ctx context.Context, authToken string) context.Context {
	metadataPairs := []string{
		grpcserver.MetadataAuthorization,
		bearerToken(authToken),
		grpcserver.MetadataCallerType,
		callerTypeService,
		grpcserver.MetadataCallerID,
		callerID,
	}
	return metadata.AppendToOutgoingContext(ctx, metadataPairs...)
}

func bearerToken(authToken string) string {
	return "Bearer " + strings.TrimSpace(authToken)
}

func checkAccessRequest(request runtimeservice.AuthorizationRequest) *accessaccountsv1.CheckAccessRequest {
	return &accessaccountsv1.CheckAccessRequest{
		Subject:   subjectRef(request),
		ActionKey: strings.TrimSpace(request.ActionKey),
		Resource:  resourceRef(request),
		Scope:     scopeRef(request),
		Audit:     true,
		Meta:      commandMetaRef(request),
	}
}

func subjectRef(request runtimeservice.AuthorizationRequest) *accessaccountsv1.SubjectRef {
	return &accessaccountsv1.SubjectRef{Type: strings.TrimSpace(request.Subject.Type), Id: strings.TrimSpace(request.Subject.ID)}
}

func resourceRef(request runtimeservice.AuthorizationRequest) *accessaccountsv1.ResourceRef {
	return &accessaccountsv1.ResourceRef{Type: strings.TrimSpace(request.ResourceType), Id: strings.TrimSpace(request.ResourceID)}
}

func scopeRef(request runtimeservice.AuthorizationRequest) *accessaccountsv1.ScopeRef {
	return &accessaccountsv1.ScopeRef{Type: strings.TrimSpace(request.ScopeType), Id: strings.TrimSpace(request.ScopeID)}
}

func commandMetaRef(request runtimeservice.AuthorizationRequest) *accessaccountsv1.CommandMeta {
	return &accessaccountsv1.CommandMeta{
		Actor:     subjectActor(request),
		RequestId: strings.TrimSpace(request.RequestID),
		RequestContext: &accessaccountsv1.RequestContext{
			Source:       request.RequestContext.Source,
			TraceId:      request.RequestContext.TraceID,
			SessionId:    request.RequestContext.SessionID,
			ClientIpHash: request.RequestContext.ClientIPHash,
		},
	}
}

func subjectActor(request runtimeservice.AuthorizationRequest) *accessaccountsv1.Actor {
	return &accessaccountsv1.Actor{
		Type: strings.TrimSpace(request.Subject.Type),
		Id:   strings.TrimSpace(request.Subject.ID),
	}
}

func mapAccessError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return err
	case errors.Is(err, context.DeadlineExceeded):
		return errs.ErrDependencyUnavailable
	}
	if mapped, ok := accessErrorByCode(status.Code(err)); ok {
		return mapped
	}
	return fmt.Errorf("%w: access-manager check failed", errs.ErrDependencyUnavailable)
}

func accessErrorByCode(code codes.Code) (error, bool) {
	switch code {
	case codes.InvalidArgument:
		return errs.ErrInvalidArgument, true
	case codes.PermissionDenied, codes.Unauthenticated:
		return errs.ErrForbidden, true
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return errs.ErrDependencyUnavailable, true
	default:
		return nil, false
	}
}
