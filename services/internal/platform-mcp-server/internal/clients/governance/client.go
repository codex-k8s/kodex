// Package governance contains the platform-mcp-server adapter for governance-manager.
package governance

import (
	"context"
	"fmt"
	"time"

	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	ownergrpc "github.com/codex-k8s/kodex/services/internal/platform-mcp-server/internal/clients/ownergrpc"
	"google.golang.org/grpc"
)

const serviceName = "governance-manager"

// Config contains governance-manager gRPC connection settings.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

// Client calls governance-manager with platform service metadata.
type Client struct {
	client    governancev1.GovernanceManagerServiceClient
	authToken string
	timeout   time.Duration
}

// NewConnection creates a gRPC client connection to governance-manager.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return ownergrpc.NewConnection(ownerConfig(cfg))
}

// New wraps a generated governance-manager client.
func New(client governancev1.GovernanceManagerServiceClient, cfg Config) (*Client, error) {
	if client == nil {
		return nil, fmt.Errorf("governance-manager client is required")
	}
	ownerCfg := ownerConfig(cfg)
	authenticated, err := ownergrpc.AuthenticatedConfig(ownerCfg)
	if err != nil {
		return nil, err
	}
	result := &Client{client: client}
	result.authToken = authenticated.AuthToken
	result.timeout = authenticated.Timeout
	return result, nil
}

// EvaluateRisk routes risk evaluation to governance-manager.
func (c *Client) EvaluateRisk(ctx context.Context, request *governancev1.EvaluateRiskRequest) (*governancev1.RiskAssessmentResponse, error) {
	return ownergrpc.Call(ctx, c.callConfig(), request, c.client.EvaluateRisk)
}

// ReevaluateRisk routes risk reevaluation to governance-manager.
func (c *Client) ReevaluateRisk(ctx context.Context, request *governancev1.ReevaluateRiskRequest) (*governancev1.RiskAssessmentResponse, error) {
	return ownergrpc.Call(ctx, c.callConfig(), request, c.client.ReevaluateRisk)
}

// GetRiskAssessment routes risk assessment reads to governance-manager.
func (c *Client) GetRiskAssessment(ctx context.Context, request *governancev1.GetRiskAssessmentRequest) (*governancev1.RiskAssessmentResponse, error) {
	return ownergrpc.Call(ctx, c.callConfig(), request, c.client.GetRiskAssessment)
}

// ListRiskAssessments routes risk assessment list reads to governance-manager.
func (c *Client) ListRiskAssessments(ctx context.Context, request *governancev1.ListRiskAssessmentsRequest) (*governancev1.ListRiskAssessmentsResponse, error) {
	return ownergrpc.Call(ctx, c.callConfig(), request, c.client.ListRiskAssessments)
}

// RequestGate routes gate request creation to governance-manager.
func (c *Client) RequestGate(ctx context.Context, request *governancev1.RequestGateRequest) (*governancev1.GateRequestResponse, error) {
	return ownergrpc.Call(ctx, c.callConfig(), request, c.client.RequestGate)
}

// GetGateRequest routes gate request reads to governance-manager.
func (c *Client) GetGateRequest(ctx context.Context, request *governancev1.GetGateRequestRequest) (*governancev1.GateRequestResponse, error) {
	return ownergrpc.Call(ctx, c.callConfig(), request, c.client.GetGateRequest)
}

// ListGateRequests routes gate request list reads to governance-manager.
func (c *Client) ListGateRequests(ctx context.Context, request *governancev1.ListGateRequestsRequest) (*governancev1.ListGateRequestsResponse, error) {
	return ownergrpc.Call(ctx, c.callConfig(), request, c.client.ListGateRequests)
}

// SubmitGateDecision routes gate decisions to governance-manager.
func (c *Client) SubmitGateDecision(ctx context.Context, request *governancev1.SubmitGateDecisionRequest) (*governancev1.GateDecisionResponse, error) {
	return ownergrpc.Call(ctx, c.callConfig(), request, c.client.SubmitGateDecision)
}

// CancelGate routes gate cancellation to governance-manager.
func (c *Client) CancelGate(ctx context.Context, request *governancev1.CancelGateRequest) (*governancev1.GateRequestResponse, error) {
	return ownergrpc.Call(ctx, c.callConfig(), request, c.client.CancelGate)
}

// ExpireGate routes gate expiry to governance-manager.
func (c *Client) ExpireGate(ctx context.Context, request *governancev1.ExpireGateRequest) (*governancev1.GateRequestResponse, error) {
	return ownergrpc.Call(ctx, c.callConfig(), request, c.client.ExpireGate)
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
