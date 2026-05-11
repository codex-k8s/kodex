package accesscheck

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Client performs access-manager CheckAccess calls with platform service metadata.
type Client struct {
	client     accessaccountsv1.AccessManagerServiceClient
	authToken  string
	callerType string
	callerID   string
	timeout    time.Duration
}

// RequestMapper converts a domain-specific authorization request to accesscheck.Request.
type RequestMapper[T any] func(T) Request

// Authorizer adapts shared CheckAccess behavior to a domain-specific request type.
type Authorizer[T any] struct {
	checker *Client
	mapper  RequestMapper[T]
	errors  DomainErrors
}

// NewConnection creates a gRPC client connection to access-manager.
func NewConnection(addr string) (*grpc.ClientConn, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, fmt.Errorf("access-manager address is required")
	}
	return grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// New creates a shared CheckAccess client.
func New(client accessaccountsv1.AccessManagerServiceClient, cfg Config) (*Client, error) {
	if client == nil {
		return nil, fmt.Errorf("access-manager client is required")
	}
	authToken := strings.TrimSpace(cfg.AuthToken)
	if authToken == "" {
		return nil, fmt.Errorf("access-manager auth token is required")
	}
	callerID := strings.TrimSpace(cfg.CallerID)
	if callerID == "" {
		return nil, fmt.Errorf("access-manager caller id is required")
	}
	callerType := strings.TrimSpace(cfg.CallerType)
	if callerType == "" {
		callerType = DefaultCallerType
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Client{client: client, authToken: authToken, callerType: callerType, callerID: callerID, timeout: timeout}, nil
}

// NewAuthorizer creates a reusable authorizer for one domain request type.
func NewAuthorizer[T any](
	client accessaccountsv1.AccessManagerServiceClient,
	cfg Config,
	mapper RequestMapper[T],
	domainErrors DomainErrors,
) (*Authorizer[T], error) {
	checker, err := New(client, cfg)
	if err != nil {
		return nil, err
	}
	if mapper == nil {
		return nil, fmt.Errorf("access request mapper is required")
	}
	if err := validateDomainErrors(domainErrors); err != nil {
		return nil, err
	}
	return &Authorizer[T]{checker: checker, mapper: mapper, errors: domainErrors}, nil
}

// Authorize implements the service-domain Authorizer interface for T.
func (a *Authorizer[T]) Authorize(ctx context.Context, request T) error {
	return MapError(a.checker.Check(ctx, a.mapper(request)), a.errors)
}

// Check allows the operation only when access-manager returns an allow decision.
func (c *Client) Check(ctx context.Context, request Request) error {
	if err := validateRequest(request); err != nil {
		return err
	}
	checkCtx, cancel := context.WithTimeout(c.outgoingContext(ctx), c.timeout)
	defer cancel()
	response, err := c.client.CheckAccess(checkCtx, checkAccessRequest(request))
	if err != nil {
		return mapAccessError(err)
	}
	if response.GetDecision() != accessaccountsv1.AccessDecision_ACCESS_DECISION_ALLOW {
		return ErrForbidden
	}
	return nil
}

// ResolveExternalAccountUsage confirms external account usage and returns only a secret reference.
func (c *Client) ResolveExternalAccountUsage(ctx context.Context, request ExternalAccountUsageRequest) (ExternalAccountUsage, error) {
	if err := validateExternalAccountUsageRequest(request); err != nil {
		return ExternalAccountUsage{}, err
	}
	checkCtx, cancel := context.WithTimeout(c.outgoingContext(ctx), c.timeout)
	defer cancel()
	response, err := c.client.ResolveExternalAccountUsage(checkCtx, externalAccountUsageRequest(request))
	if err != nil {
		return ExternalAccountUsage{}, mapAccessError(err)
	}
	return ExternalAccountUsage{
		ExternalAccountID: response.GetExternalAccountId(),
		ProviderID:        response.GetProviderId(),
		ProviderSlug:      response.GetProviderSlug(),
		SecretRefID:       response.GetSecretRefId(),
		SecretStoreType:   response.GetSecretStoreType(),
		SecretStoreRef:    response.GetSecretStoreRef(),
		AllowedActionKeys: append([]string(nil), response.GetAllowedActionKeys()...),
	}, nil
}

// NewRequest builds a typed accesscheck request from service-domain fields.
func NewRequest(fields RequestFields) Request {
	return Request{
		Subject:        Subject{Type: fields.SubjectType, ID: fields.SubjectID},
		ActionKey:      fields.ActionKey,
		Resource:       Resource{Type: fields.ResourceType, ID: fields.ResourceID},
		Scope:          Scope{Type: fields.ScopeType, ID: fields.ScopeID},
		RequestID:      fields.RequestID,
		RequestContext: fields.Context,
	}
}

// MapError converts shared accesscheck errors to service-domain errors.
func MapError(err error, target DomainErrors) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return target.InvalidRequest
	case errors.Is(err, ErrForbidden):
		return target.Forbidden
	case errors.Is(err, ErrDependencyUnavailable):
		return target.DependencyUnavailable
	case errors.Is(err, context.DeadlineExceeded):
		return target.DependencyUnavailable
	default:
		return fmt.Errorf("%w: access-manager check failed", target.DependencyUnavailable)
	}
}

func validateDomainErrors(target DomainErrors) error {
	switch {
	case target.InvalidRequest == nil:
		return fmt.Errorf("invalid request domain error is required")
	case target.Forbidden == nil:
		return fmt.Errorf("forbidden domain error is required")
	case target.DependencyUnavailable == nil:
		return fmt.Errorf("dependency unavailable domain error is required")
	default:
		return nil
	}
}

func validateRequest(request Request) error {
	required := []string{
		request.Subject.Type,
		request.Subject.ID,
		request.ActionKey,
		request.Resource.Type,
	}
	for _, item := range required {
		if strings.TrimSpace(item) == "" {
			return ErrInvalidRequest
		}
	}
	return nil
}

func validateExternalAccountUsageRequest(request ExternalAccountUsageRequest) error {
	required := []string{
		request.ExternalAccountID,
		request.ActionKey,
		request.Scope.Type,
		request.Scope.ID,
	}
	for _, item := range required {
		if strings.TrimSpace(item) == "" {
			return ErrInvalidRequest
		}
	}
	return nil
}

func (c *Client) outgoingContext(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(
		ctx,
		grpcserver.MetadataAuthorization,
		"Bearer "+c.authToken,
		grpcserver.MetadataCallerType,
		c.callerType,
		grpcserver.MetadataCallerID,
		c.callerID,
	)
}

func externalAccountUsageRequest(request ExternalAccountUsageRequest) *accessaccountsv1.ResolveExternalAccountUsageRequest {
	return &accessaccountsv1.ResolveExternalAccountUsageRequest{
		ExternalAccountId: strings.TrimSpace(request.ExternalAccountID),
		ActionKey:         strings.TrimSpace(request.ActionKey),
		UsageScope: &accessaccountsv1.ScopeRef{
			Type: strings.TrimSpace(request.Scope.Type),
			Id:   strings.TrimSpace(request.Scope.ID),
		},
	}
}

func checkAccessRequest(request Request) *accessaccountsv1.CheckAccessRequest {
	return &accessaccountsv1.CheckAccessRequest{
		Subject: &accessaccountsv1.SubjectRef{
			Type: strings.TrimSpace(request.Subject.Type),
			Id:   strings.TrimSpace(request.Subject.ID),
		},
		ActionKey: strings.TrimSpace(request.ActionKey),
		Resource: &accessaccountsv1.ResourceRef{
			Type: strings.TrimSpace(request.Resource.Type),
			Id:   strings.TrimSpace(request.Resource.ID),
		},
		Scope: &accessaccountsv1.ScopeRef{
			Type: strings.TrimSpace(request.Scope.Type),
			Id:   strings.TrimSpace(request.Scope.ID),
		},
		Audit: true,
		Meta: &accessaccountsv1.CommandMeta{
			Actor: &accessaccountsv1.Actor{
				Type: strings.TrimSpace(request.Subject.Type),
				Id:   strings.TrimSpace(request.Subject.ID),
			},
			RequestId: strings.TrimSpace(request.RequestID),
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
		return ErrDependencyUnavailable
	}
	switch status.Code(err) {
	case codes.InvalidArgument:
		return ErrInvalidRequest
	case codes.PermissionDenied, codes.Unauthenticated:
		return ErrForbidden
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return ErrDependencyUnavailable
	default:
		return fmt.Errorf("%w: check access failed", ErrDependencyUnavailable)
	}
}
