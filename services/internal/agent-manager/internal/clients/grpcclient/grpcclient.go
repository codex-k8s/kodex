// Package grpcclient contains shared helpers for agent-manager owner-service clients.
package grpcclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// NewConnection creates a lazy insecure gRPC connection for internal service traffic.
func NewConnection(addr string, service string) (*grpc.ClientConn, error) {
	target := strings.TrimSpace(addr)
	if target == "" {
		return nil, fmt.Errorf("%s address is required", service)
	}
	return grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// RequiredAuthToken validates a service-to-service bearer token.
func RequiredAuthToken(token string, service string) (string, error) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return "", fmt.Errorf("%s auth token is required", service)
	}
	return trimmed, nil
}

// TimeoutOrDefault returns a positive timeout.
func TimeoutOrDefault(value time.Duration, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

// ClientSettings contains normalized owner-service client settings.
type ClientSettings struct {
	AuthToken string
	Timeout   time.Duration
}

// RequiredClientSettings validates a generated client and returns normalized settings.
func RequiredClientSettings(client any, token string, timeout time.Duration, fallback time.Duration, service string) (ClientSettings, error) {
	if client == nil {
		return ClientSettings{}, fmt.Errorf("%s client is required", service)
	}
	authToken, err := RequiredAuthToken(token, service)
	if err != nil {
		return ClientSettings{}, err
	}
	return ClientSettings{AuthToken: authToken, Timeout: TimeoutOrDefault(timeout, fallback)}, nil
}

// BuildAdapter validates settings and constructs a typed service adapter.
func BuildAdapter[T any](
	client any,
	token string,
	timeout time.Duration,
	fallback time.Duration,
	service string,
	build func(ClientSettings) T,
) (T, error) {
	var zero T
	settings, err := RequiredClientSettings(client, token, timeout, fallback, service)
	if err != nil {
		return zero, err
	}
	return build(settings), nil
}

// OutgoingContext adds common service auth metadata.
func OutgoingContext(ctx context.Context, authToken string, callerID string) context.Context {
	return metadata.AppendToOutgoingContext(
		ctx,
		grpcserver.MetadataAuthorization,
		"Bearer "+authToken,
		grpcserver.MetadataCallerType,
		"service",
		grpcserver.MetadataCallerID,
		callerID,
	)
}

// MapReadError maps owner-service read failures to agent-manager domain errors.
func MapReadError(err error, label string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errs.ErrDependencyUnavailable
	}
	switch status.Code(err) {
	case codes.InvalidArgument:
		return errs.ErrInvalidArgument
	case codes.NotFound:
		return errs.ErrNotFound
	case codes.AlreadyExists:
		return errs.ErrAlreadyExists
	case codes.Aborted:
		return errs.ErrConflict
	case codes.FailedPrecondition:
		return errs.ErrPreconditionFailed
	case codes.PermissionDenied, codes.Unauthenticated, codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return errs.ErrDependencyUnavailable
	default:
		return fmt.Errorf("%w: %s", errs.ErrDependencyUnavailable, label)
	}
}
