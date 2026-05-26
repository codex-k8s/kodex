// Package agentmanager contains the platform-mcp-server adapter for agent-manager.
package agentmanager

import (
	"context"
	"fmt"
	"time"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	ownergrpc "github.com/codex-k8s/kodex/services/internal/platform-mcp-server/internal/clients/ownergrpc"
	"google.golang.org/grpc"
)

const serviceName = "agent-manager"

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
	return ownergrpc.NewConnection(ownerConfig(cfg))
}

// New wraps a generated agent-manager client.
func New(client agentsv1.AgentManagerServiceClient, cfg Config) (*Client, error) {
	if client == nil {
		return nil, fmt.Errorf("agent-manager client is required")
	}
	ownerCfg, err := ownergrpc.AuthenticatedConfig(ownerConfig(cfg))
	if err != nil {
		return nil, err
	}
	return &Client{client: client, authToken: ownerCfg.AuthToken, timeout: ownerCfg.Timeout}, nil
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
	return ownergrpc.Call(ctx, client.callConfig(), request, call)
}

func (c *Client) callConfig() ownergrpc.Config {
	return ownergrpc.Config{Service: serviceName, AuthToken: c.authToken, Timeout: c.timeout}
}

func ownerConfig(cfg Config) ownergrpc.Config {
	return ownergrpc.Config{
		Service:   serviceName,
		Addr:      cfg.Addr,
		AuthToken: cfg.AuthToken,
		Timeout:   cfg.Timeout,
	}
}
