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
	summaryMeta := governanceRequestMetadata(request.GetMeta())
	summaryCall := c.client.GetGovernanceSummary
	return callGovernance(ctx, c.authToken, c.timeout, summaryMeta, summaryCall, request)
}

func (c *Client) SubmitGateDecision(ctx context.Context, request *governancev1.SubmitGateDecisionRequest) (*governancev1.GateDecisionResponse, error) {
	meta := governanceCommandMetadata(request.GetMeta())
	return callGovernance(ctx, c.authToken, c.timeout, meta, c.client.SubmitGateDecision, request)
}

func callGovernance[Request, Response any](ctx context.Context, authToken string, timeout time.Duration, meta clientruntime.RequestMetadata, call func(context.Context, *Request, ...grpc.CallOption) (*Response, error), request *Request) (*Response, error) {
	meta.AuthToken = authToken
	callCtx, cancel := clientruntime.OutgoingCallContext(ctx, meta, timeout)
	defer cancel()
	return call(callCtx, request)
}

func governanceRequestMetadata(meta *governancev1.QueryMeta) clientruntime.RequestMetadata {
	if meta == nil {
		return clientruntime.RequestMetadata{}
	}
	return governanceMetadata(meta.GetRequestId(), meta.GetRequestContext())
}

func governanceCommandMetadata(meta *governancev1.CommandMeta) clientruntime.RequestMetadata {
	if meta == nil {
		return clientruntime.RequestMetadata{}
	}
	return governanceMetadata(meta.GetRequestId(), meta.GetRequestContext())
}

func governanceMetadata(requestID string, context *governancev1.RequestContext) clientruntime.RequestMetadata {
	output := clientruntime.RequestMetadata{RequestID: requestID}
	if context != nil {
		output.CorrelationID = context.GetTraceId()
	}
	return output
}
