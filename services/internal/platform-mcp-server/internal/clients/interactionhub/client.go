// Package interactionhub contains the platform-mcp-server adapter for interaction-hub.
package interactionhub

import (
	"context"
	"fmt"
	"time"

	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	ownergrpc "github.com/codex-k8s/kodex/services/internal/platform-mcp-server/internal/clients/ownergrpc"
	"google.golang.org/grpc"
)

const serviceName = "interaction-hub"

// Config contains interaction-hub gRPC connection settings.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

// Client calls interaction-hub with platform service metadata.
type Client struct {
	client    interactionsv1.InteractionHubServiceClient
	authToken string
	timeout   time.Duration
}

// NewConnection creates a gRPC client connection to interaction-hub.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return ownergrpc.NewConnection(ownerConfig(cfg))
}

// New wraps a generated interaction-hub client.
func New(client interactionsv1.InteractionHubServiceClient, cfg Config) (*Client, error) {
	if client == nil {
		return nil, fmt.Errorf("interaction-hub client is required")
	}
	ownerCfg, err := ownergrpc.AuthenticatedConfig(ownerConfig(cfg))
	if err != nil {
		return nil, err
	}
	result := &Client{client: client}
	result.authToken = ownerCfg.AuthToken
	result.timeout = ownerCfg.Timeout
	return result, nil
}

// ListOwnerInboxItems routes owner inbox reads to interaction-hub.
func (c *Client) ListOwnerInboxItems(ctx context.Context, request *interactionsv1.ListOwnerInboxItemsRequest) (*interactionsv1.ListOwnerInboxItemsResponse, error) {
	return ownergrpc.Call(ctx, c.callConfig(), request, c.client.ListOwnerInboxItems)
}

// GetOwnerInboxItem routes one owner inbox read to interaction-hub.
func (c *Client) GetOwnerInboxItem(ctx context.Context, request *interactionsv1.GetOwnerInboxItemRequest) (*interactionsv1.OwnerInboxItemResponse, error) {
	return ownergrpc.Call(ctx, c.callConfig(), request, c.client.GetOwnerInboxItem)
}

// RecordInteractionResponse routes owner response recording to interaction-hub.
func (c *Client) RecordInteractionResponse(ctx context.Context, request *interactionsv1.RecordInteractionResponseRequest) (*interactionsv1.InteractionResponseResponse, error) {
	return ownergrpc.Call(ctx, c.callConfig(), request, c.client.RecordInteractionResponse)
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
