// Package access adapts access-manager checks to the project-catalog domain.
package access

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	projectservice "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	callerTypeService = "service"
	callerID          = "project-catalog"
)

// Config contains gRPC connection settings for access-manager.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

// Authorizer calls access-manager CheckAccess for project-catalog commands and reads.
type Authorizer struct {
	client    accessaccountsv1.AccessManagerServiceClient
	authToken string
	timeout   time.Duration
}

var _ projectservice.Authorizer = (*Authorizer)(nil)

// NewConnection creates a lazy gRPC client connection to access-manager.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		return nil, fmt.Errorf("access-manager address is required")
	}
	return grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// NewAuthorizer wraps a generated access-manager client.
func NewAuthorizer(client accessaccountsv1.AccessManagerServiceClient, cfg Config) (*Authorizer, error) {
	if client == nil {
		return nil, fmt.Errorf("access-manager client is required")
	}
	if strings.TrimSpace(cfg.AuthToken) == "" {
		return nil, fmt.Errorf("access-manager auth token is required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &Authorizer{client: client, authToken: strings.TrimSpace(cfg.AuthToken), timeout: timeout}, nil
}

// Authorize denies project-catalog work unless access-manager returns an allow decision.
func (a *Authorizer) Authorize(ctx context.Context, request projectservice.AuthorizationRequest) error {
	if err := validateRequest(request); err != nil {
		return err
	}
	checkCtx, cancel := context.WithTimeout(outgoingAuthContext(ctx, a.authToken), a.timeout)
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

func validateRequest(request projectservice.AuthorizationRequest) error {
	switch {
	case strings.TrimSpace(request.Subject.Type) == "":
		return errs.ErrInvalidArgument
	case strings.TrimSpace(request.Subject.ID) == "":
		return errs.ErrInvalidArgument
	case strings.TrimSpace(request.ActionKey) == "":
		return errs.ErrInvalidArgument
	case strings.TrimSpace(request.ResourceType) == "":
		return errs.ErrInvalidArgument
	default:
		return nil
	}
}

func outgoingAuthContext(ctx context.Context, authToken string) context.Context {
	return metadata.AppendToOutgoingContext(
		ctx,
		grpcserver.MetadataAuthorization,
		"Bearer "+strings.TrimSpace(authToken),
		grpcserver.MetadataCallerType,
		callerTypeService,
		grpcserver.MetadataCallerID,
		callerID,
	)
}

func checkAccessRequest(request projectservice.AuthorizationRequest) *accessaccountsv1.CheckAccessRequest {
	return &accessaccountsv1.CheckAccessRequest{
		Subject: &accessaccountsv1.SubjectRef{
			Type: strings.TrimSpace(request.Subject.Type),
			Id:   strings.TrimSpace(request.Subject.ID),
		},
		ActionKey: strings.TrimSpace(request.ActionKey),
		Resource: &accessaccountsv1.ResourceRef{
			Type: strings.TrimSpace(request.ResourceType),
			Id:   strings.TrimSpace(request.ResourceID),
		},
		Scope: &accessaccountsv1.ScopeRef{
			Type: strings.TrimSpace(request.ScopeType),
			Id:   strings.TrimSpace(request.ScopeID),
		},
		Audit: true,
		Meta: &accessaccountsv1.CommandMeta{
			Actor: &accessaccountsv1.Actor{
				Type: strings.TrimSpace(request.Subject.Type),
				Id:   strings.TrimSpace(request.Subject.ID),
			},
			RequestId: request.RequestID,
			RequestContext: &accessaccountsv1.RequestContext{
				Source:       request.RequestContext.Source,
				TraceId:      request.RequestContext.TraceID,
				SessionId:    request.RequestContext.SessionID,
				ClientIpHash: request.RequestContext.ClientIPHash,
			},
		},
	}
}

func mapAccessError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return err
	case errors.Is(err, context.DeadlineExceeded):
		return errs.ErrDependencyUnavailable
	}
	switch status.Code(err) {
	case codes.InvalidArgument:
		return errs.ErrInvalidArgument
	case codes.PermissionDenied:
		return errs.ErrForbidden
	case codes.Unauthenticated:
		return errs.ErrForbidden
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return errs.ErrDependencyUnavailable
	default:
		return fmt.Errorf("%w: access-manager check failed", errs.ErrDependencyUnavailable)
	}
}
