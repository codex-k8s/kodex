// Package ownergrpc contains shared gRPC owner-client helpers for platform-mcp-server.
package ownergrpc

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

const callerID = "platform-mcp-server"

// Config contains one owner-service gRPC connection settings.
type Config struct {
	Service   string
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

// NewConnection creates a gRPC client connection to an owner service.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	addr, err := RequiredValue(cfg.Addr, cfg.Service+" address")
	if err != nil {
		return nil, err
	}
	dialOptions := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	return grpc.NewClient(addr, dialOptions...)
}

// RequiredValue validates a required config value.
func RequiredValue(value string, name string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return trimmed, nil
}

// Timeout returns an effective owner-client timeout.
func Timeout(value time.Duration) time.Duration {
	if value <= 0 {
		return 3 * time.Second
	}
	return value
}

// AuthenticatedConfig validates the owner auth token and applies default timeout.
func AuthenticatedConfig(cfg Config) (Config, error) {
	authToken, err := RequiredValue(cfg.AuthToken, cfg.Service+" auth token")
	if err != nil {
		return Config{}, err
	}
	cfg.AuthToken = authToken
	cfg.Timeout = Timeout(cfg.Timeout)
	return cfg, nil
}

// Call invokes an owner RPC with platform service metadata.
func Call[Request any, Response any](
	ctx context.Context,
	cfg Config,
	request Request,
	call func(context.Context, Request, ...grpc.CallOption) (Response, error),
) (Response, error) {
	callCtx, cancel := context.WithTimeout(outgoingContext(ctx, cfg), Timeout(cfg.Timeout))
	defer cancel()
	return call(callCtx, request)
}

func outgoingContext(ctx context.Context, cfg Config) context.Context {
	return metadata.AppendToOutgoingContext(
		ctx,
		grpcserver.MetadataAuthorization,
		"Bearer "+strings.TrimSpace(cfg.AuthToken),
		grpcserver.MetadataCallerType,
		"service",
		grpcserver.MetadataCallerID,
		callerID,
	)
}
