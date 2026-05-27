// Package clientruntime contains shared integration-gateway gRPC client helpers.
package clientruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// CallerID identifies integration-gateway in downstream service metadata.
const CallerID = "integration-gateway"

// RequestMetadata carries safe caller metadata for downstream owner services.
type RequestMetadata struct {
	AuthToken     string
	RequestID     string
	CorrelationID string
}

// ClientSettings validates a generated client dependency and returns runtime settings.
func ClientSettings(clientMissing bool, service string, authToken string, timeout time.Duration) (string, time.Duration, error) {
	if clientMissing {
		return "", 0, fmt.Errorf("%s client is required", service)
	}
	token, err := RequiredValue(authToken, service+" auth token")
	if err != nil {
		return "", 0, err
	}
	return token, EffectiveTimeout(timeout), nil
}

// NewConnection creates a plaintext in-cluster gRPC client connection.
func NewConnection(addr string, name string) (*grpc.ClientConn, error) {
	trimmed, err := RequiredValue(addr, name+" address")
	if err != nil {
		return nil, err
	}
	return grpc.NewClient(trimmed, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// RequiredValue returns a trimmed value or a named required-value error.
func RequiredValue(value string, name string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return trimmed, nil
}

// EffectiveTimeout returns the configured timeout or the client default.
func EffectiveTimeout(value time.Duration) time.Duration {
	if value <= 0 {
		return 3 * time.Second
	}
	return value
}

// OptionalString returns nil for blank strings.
func OptionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// OutgoingContext appends redaction-safe service metadata to a gRPC context.
func OutgoingContext(ctx context.Context, meta RequestMetadata) context.Context {
	values := []string{
		grpcserver.MetadataAuthorization,
		"Bearer " + strings.TrimSpace(meta.AuthToken),
		grpcserver.MetadataCallerType,
		"service",
		grpcserver.MetadataCallerID,
		CallerID,
		grpcserver.MetadataRequestSource,
		CallerID,
	}
	if meta.RequestID != "" {
		values = append(values, grpcserver.MetadataRequestID, meta.RequestID)
	}
	if meta.CorrelationID != "" {
		values = append(values, grpcserver.MetadataTraceID, meta.CorrelationID)
	}
	return metadata.AppendToOutgoingContext(ctx, values...)
}
