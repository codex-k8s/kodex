// Package access adapts access-manager checks to the package-hub domain.
package access

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
	packageservice "github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	callerTypeService = "service"
	callerID          = "package-hub"
	defaultTimeout    = 3 * time.Second
)

// Config contains access-manager client settings.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

// Authorizer delegates package-hub access decisions to access-manager.
type Authorizer struct {
	client    accessaccountsv1.AccessManagerServiceClient
	token     string
	checkTime time.Duration
}

var _ packageservice.Authorizer = (*Authorizer)(nil)

// NewConnection creates a gRPC client connection to access-manager.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	addr, err := accessManagerAddress(cfg)
	if err != nil {
		return nil, err
	}
	return grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// NewAuthorizer wraps an access-manager gRPC client.
func NewAuthorizer(client accessaccountsv1.AccessManagerServiceClient, cfg Config) (*Authorizer, error) {
	if client == nil {
		return nil, fmt.Errorf("access-manager client is required")
	}
	token := strings.TrimSpace(cfg.AuthToken)
	if token == "" {
		return nil, fmt.Errorf("access-manager auth token is required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Authorizer{client: client, token: token, checkTime: timeout}, nil
}

// Authorize allows the package-hub operation only when access-manager allows it.
func (a *Authorizer) Authorize(ctx context.Context, request packageservice.AuthorizationRequest) error {
	if err := validateRequest(request); err != nil {
		return err
	}
	checkCtx, cancel := context.WithTimeout(withServiceAuth(ctx, a.token), a.checkTime)
	defer cancel()
	response, err := a.client.CheckAccess(checkCtx, checkRequest(request))
	if err != nil {
		return accessFailure(err)
	}
	if !allowed(response) {
		return errs.ErrForbidden
	}
	return nil
}

func accessManagerAddress(cfg Config) (string, error) {
	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		return "", fmt.Errorf("access-manager address is required")
	}
	return addr, nil
}

func validateRequest(request packageservice.AuthorizationRequest) error {
	required := []string{
		request.Subject.Type,
		request.Subject.ID,
		request.ActionKey,
		request.ResourceType,
	}
	for _, item := range required {
		if strings.TrimSpace(item) == "" {
			return errs.ErrInvalidArgument
		}
	}
	return nil
}

func withServiceAuth(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(
		ctx,
		grpcserver.MetadataAuthorization,
		"Bearer "+strings.TrimSpace(token),
		grpcserver.MetadataCallerType,
		callerTypeService,
		grpcserver.MetadataCallerID,
		callerID,
	)
}

func checkRequest(request packageservice.AuthorizationRequest) *accessaccountsv1.CheckAccessRequest {
	check := &accessaccountsv1.CheckAccessRequest{Audit: true}
	check.Subject = subject(request)
	check.ActionKey = strings.TrimSpace(request.ActionKey)
	check.Resource = resource(request)
	check.Scope = scope(request)
	check.Meta = meta(request)
	return check
}

func subject(request packageservice.AuthorizationRequest) *accessaccountsv1.SubjectRef {
	return &accessaccountsv1.SubjectRef{Type: strings.TrimSpace(request.Subject.Type), Id: strings.TrimSpace(request.Subject.ID)}
}

func resource(request packageservice.AuthorizationRequest) *accessaccountsv1.ResourceRef {
	return &accessaccountsv1.ResourceRef{Type: strings.TrimSpace(request.ResourceType), Id: strings.TrimSpace(request.ResourceID)}
}

func scope(request packageservice.AuthorizationRequest) *accessaccountsv1.ScopeRef {
	return &accessaccountsv1.ScopeRef{Type: strings.TrimSpace(request.ScopeType), Id: strings.TrimSpace(request.ScopeID)}
}

func meta(request packageservice.AuthorizationRequest) *accessaccountsv1.CommandMeta {
	return &accessaccountsv1.CommandMeta{
		Actor:          &accessaccountsv1.Actor{Type: strings.TrimSpace(request.Subject.Type), Id: strings.TrimSpace(request.Subject.ID)},
		RequestId:      strings.TrimSpace(request.RequestID),
		RequestContext: requestContext(request),
	}
}

func requestContext(request packageservice.AuthorizationRequest) *accessaccountsv1.RequestContext {
	return &accessaccountsv1.RequestContext{
		Source:       request.RequestContext.Source,
		TraceId:      request.RequestContext.TraceID,
		SessionId:    request.RequestContext.SessionID,
		ClientIpHash: request.RequestContext.ClientIPHash,
	}
}

func allowed(response *accessaccountsv1.CheckAccessResponse) bool {
	return response.GetDecision() == accessaccountsv1.AccessDecision_ACCESS_DECISION_ALLOW
}

func accessFailure(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return err
	case errors.Is(err, context.DeadlineExceeded):
		return errs.ErrDependencyUnavailable
	}
	switch status.Code(err) {
	case codes.InvalidArgument:
		return errs.ErrInvalidArgument
	case codes.PermissionDenied, codes.Unauthenticated:
		return errs.ErrForbidden
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return errs.ErrDependencyUnavailable
	default:
		return fmt.Errorf("%w: access-manager check failed", errs.ErrDependencyUnavailable)
	}
}
