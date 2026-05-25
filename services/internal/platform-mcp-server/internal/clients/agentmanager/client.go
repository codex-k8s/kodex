// Package agentmanager contains the platform-mcp-server adapter for agent-manager.
package agentmanager

import (
	"context"
	"fmt"
	"strings"
	"time"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const callerID = "platform-mcp-server"

// Config contains agent-manager gRPC connection settings.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

// Client calls agent-manager with platform service metadata.
type Client struct {
	client    agentsv1.AgentManagerServiceClient
	authToken string
	timeout   time.Duration
}

// NewConnection creates a gRPC client connection to agent-manager.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	addr, err := requiredConfigValue(cfg.Addr, "agent-manager address")
	if err != nil {
		return nil, err
	}
	dialOptions := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	return grpc.NewClient(addr, dialOptions...)
}

// New wraps a generated agent-manager client.
func New(client agentsv1.AgentManagerServiceClient, cfg Config) (*Client, error) {
	if client == nil {
		return nil, fmt.Errorf("agent-manager client is required")
	}
	authToken, err := requiredConfigValue(cfg.AuthToken, "agent-manager auth token")
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &Client{client: client, authToken: authToken, timeout: timeout}, nil
}

func requiredConfigValue(value string, name string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return trimmed, nil
}

// StartAgentSession routes a session command to agent-manager.
func (c *Client) StartAgentSession(ctx context.Context, request *agentsv1.StartAgentSessionRequest) (*agentsv1.AgentSessionResponse, error) {
	return callOwner(ctx, c, request, c.client.StartAgentSession)
}

// StartAgentRun routes a run command to agent-manager.
func (c *Client) StartAgentRun(ctx context.Context, request *agentsv1.StartAgentRunRequest) (*agentsv1.AgentRunResponse, error) {
	return callOwner(ctx, c, request, c.client.StartAgentRun)
}

// RecordRunState routes a run state command to agent-manager.
func (c *Client) RecordRunState(ctx context.Context, request *agentsv1.RecordRunStateRequest) (*agentsv1.AgentRunResponse, error) {
	return callOwner(ctx, c, request, c.client.RecordRunState)
}

// RecordSessionStateSnapshot routes a session snapshot command to agent-manager.
func (c *Client) RecordSessionStateSnapshot(ctx context.Context, request *agentsv1.RecordSessionStateSnapshotRequest) (*agentsv1.AgentSessionStateSnapshotResponse, error) {
	return callOwner(ctx, c, request, c.client.RecordSessionStateSnapshot)
}

// GetAgentSession routes a session read to agent-manager.
func (c *Client) GetAgentSession(ctx context.Context, request *agentsv1.GetAgentSessionRequest) (*agentsv1.AgentSessionResponse, error) {
	return callOwner(ctx, c, request, c.client.GetAgentSession)
}

// ListAgentRuns routes a run list read to agent-manager.
func (c *Client) ListAgentRuns(ctx context.Context, request *agentsv1.ListAgentRunsRequest) (*agentsv1.ListAgentRunsResponse, error) {
	return callOwner(ctx, c, request, c.client.ListAgentRuns)
}

func callOwner[Request any, Response any](
	ctx context.Context,
	client *Client,
	request Request,
	call func(context.Context, Request, ...grpc.CallOption) (Response, error),
) (Response, error) {
	callCtx, cancel := context.WithTimeout(client.outgoingContext(ctx), client.timeout)
	defer cancel()
	return call(callCtx, request)
}

func (c *Client) outgoingContext(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(
		ctx,
		grpcserver.MetadataAuthorization,
		"Bearer "+c.authToken,
		grpcserver.MetadataCallerType,
		"service",
		grpcserver.MetadataCallerID,
		callerID,
	)
}
