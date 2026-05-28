package agentmanager

import (
	"context"
	"fmt"
	"time"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	"github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/clients/clientruntime"
	"google.golang.org/grpc"
)

type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

type Client struct {
	client    agentsv1.AgentManagerServiceClient
	authToken string
	timeout   time.Duration
}

func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return clientruntime.NewConnection(cfg.Addr, "agent-manager")
}

func New(client agentsv1.AgentManagerServiceClient, cfg Config) (*Client, error) {
	if client == nil {
		return nil, fmt.Errorf("agent-manager client is required")
	}
	authToken, timeout, err := clientruntime.ClientSettings(false, "agent-manager", cfg.AuthToken, cfg.Timeout)
	if err != nil {
		return nil, err
	}
	return &Client{client: client, authToken: authToken, timeout: timeout}, nil
}

func (c *Client) GetAgentRunRuntimeStatus(ctx context.Context, request *agentsv1.GetAgentRunRuntimeStatusRequest) (*agentsv1.AgentRunRuntimeStatusResponse, error) {
	callCtx, cancel := c.callContext(ctx, request.GetMeta())
	defer cancel()
	return c.client.GetAgentRunRuntimeStatus(callCtx, request)
}

func (c *Client) callContext(ctx context.Context, meta *agentsv1.QueryMeta) (context.Context, context.CancelFunc) {
	return clientruntime.OutgoingCallContext(ctx, agentRequestMetadata(c.authToken, meta), c.timeout)
}

func agentRequestMetadata(authToken string, meta *agentsv1.QueryMeta) clientruntime.RequestMetadata {
	requestID := ""
	traceID := ""
	if meta != nil {
		requestID = meta.GetRequestId()
		if meta.GetRequestContext() != nil {
			traceID = meta.GetRequestContext().GetTraceId()
		}
	}
	return clientruntime.RequestMetadata{AuthToken: authToken, RequestID: requestID, CorrelationID: traceID}
}
