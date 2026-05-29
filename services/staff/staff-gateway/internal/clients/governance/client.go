package governance

import (
	"context"
	"fmt"
	"time"

	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	"github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/clients/clientruntime"
	"google.golang.org/grpc"
)

type Config = clientruntime.Config

type Client struct {
	client    governancev1.GovernanceManagerServiceClient
	authToken string
	timeout   time.Duration
}

func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return clientruntime.NewConnection(cfg.Addr, "governance-manager")
}

func New(client governancev1.GovernanceManagerServiceClient, cfg Config) (*Client, error) {
	if client == nil {
		return nil, fmt.Errorf("governance-manager client is required")
	}
	settings, err := governanceSettings(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{client: client, authToken: settings.authToken, timeout: settings.timeout}, nil
}

type runtimeSettings struct {
	authToken string
	timeout   time.Duration
}

func governanceSettings(cfg Config) (runtimeSettings, error) {
	authToken, timeout, err := clientruntime.ClientSettings(false, "governance-manager", cfg.AuthToken, cfg.Timeout)
	if err != nil {
		return runtimeSettings{}, err
	}
	return runtimeSettings{authToken: authToken, timeout: timeout}, nil
}

func (c *Client) GetGovernanceSummary(ctx context.Context, request *governancev1.GetGovernanceSummaryRequest) (*governancev1.GovernanceSummaryResponse, error) {
	meta := governanceRequestMetadata(c.authToken, request.GetMeta())
	callCtx, cancel := clientruntime.OutgoingCallContext(ctx, meta, c.timeout)
	defer cancel()
	return c.client.GetGovernanceSummary(callCtx, request)
}

func governanceRequestMetadata(authToken string, meta *governancev1.QueryMeta) clientruntime.RequestMetadata {
	output := clientruntime.RequestMetadata{AuthToken: authToken}
	if meta == nil {
		return output
	}
	output.RequestID = meta.GetRequestId()
	if context := meta.GetRequestContext(); context != nil {
		output.CorrelationID = context.GetTraceId()
	}
	return output
}
