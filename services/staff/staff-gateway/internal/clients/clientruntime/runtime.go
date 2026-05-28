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

const CallerID = "staff-gateway"

type RequestMetadata struct {
	AuthToken     string
	RequestID     string
	CorrelationID string
}

func NewConnection(addr string, serviceName string) (*grpc.ClientConn, error) {
	target := strings.TrimSpace(addr)
	if target == "" {
		return nil, fmt.Errorf("%s address is required", serviceName)
	}
	dialOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	conn, err := grpc.NewClient(target, dialOptions...)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func ClientSettings(clientMissing bool, serviceName string, authToken string, timeout time.Duration) (string, time.Duration, error) {
	if clientMissing {
		return "", 0, fmt.Errorf("%s client is required", serviceName)
	}
	token, err := requireTrimmed(authToken, serviceName+" auth token")
	if err != nil {
		return "", 0, err
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return token, timeout, nil
}

func OptionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

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
	if strings.TrimSpace(meta.RequestID) != "" {
		values = append(values, grpcserver.MetadataRequestID, strings.TrimSpace(meta.RequestID))
	}
	if strings.TrimSpace(meta.CorrelationID) != "" {
		values = append(values, grpcserver.MetadataTraceID, strings.TrimSpace(meta.CorrelationID))
	}
	return metadata.AppendToOutgoingContext(ctx, values...)
}

func OutgoingCallContext(ctx context.Context, meta RequestMetadata, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(OutgoingContext(ctx, meta), timeout)
}

func requireTrimmed(value string, name string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return trimmed, nil
}
