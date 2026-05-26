// Package providerhub contains the platform-mcp-server adapter for provider-hub.
package providerhub

import (
	"context"
	"fmt"
	"time"

	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	ownergrpc "github.com/codex-k8s/kodex/services/internal/platform-mcp-server/internal/clients/ownergrpc"
	"google.golang.org/grpc"
)

const serviceName = "provider-hub"

// Config contains provider-hub gRPC connection settings.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

// Client calls provider-hub with platform service metadata.
type Client struct {
	client    providersv1.ProviderHubServiceClient
	authToken string
	timeout   time.Duration
}

// NewConnection creates a gRPC client connection to provider-hub.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return ownergrpc.NewConnection(ownerConfig(cfg))
}

// New wraps a generated provider-hub client.
func New(client providersv1.ProviderHubServiceClient, cfg Config) (*Client, error) {
	return newClient(client, ownerConfig(cfg))
}

func newClient(client providersv1.ProviderHubServiceClient, cfg ownergrpc.Config) (*Client, error) {
	if client == nil {
		return nil, fmt.Errorf("provider-hub client is required")
	}
	ownerCfg, err := ownergrpc.AuthenticatedConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{client: client, authToken: ownerCfg.AuthToken, timeout: ownerCfg.Timeout}, nil
}

// GetWorkItemProjection routes a projection read to provider-hub.
func (c *Client) GetWorkItemProjection(ctx context.Context, request *providersv1.GetWorkItemProjectionRequest) (*providersv1.WorkItemProjectionResponse, error) {
	return callOwner(ctx, c, request, c.client.GetWorkItemProjection)
}

// FindWorkItemByProviderRef routes a provider ref lookup to provider-hub.
func (c *Client) FindWorkItemByProviderRef(ctx context.Context, request *providersv1.FindWorkItemByProviderRefRequest) (*providersv1.WorkItemProjectionResponse, error) {
	return callOwner(ctx, c, request, c.client.FindWorkItemByProviderRef)
}

// ListWorkItemProjections routes a projection list read to provider-hub.
func (c *Client) ListWorkItemProjections(ctx context.Context, request *providersv1.ListWorkItemProjectionsRequest) (*providersv1.ListWorkItemProjectionsResponse, error) {
	return callOwner(ctx, c, request, c.client.ListWorkItemProjections)
}

// ListComments routes a comment list read to provider-hub.
func (c *Client) ListComments(ctx context.Context, request *providersv1.ListCommentsRequest) (*providersv1.ListCommentsResponse, error) {
	return callOwner(ctx, c, request, c.client.ListComments)
}

// ListRelationships routes a relationship list read to provider-hub.
func (c *Client) ListRelationships(ctx context.Context, request *providersv1.ListRelationshipsRequest) (*providersv1.ListRelationshipsResponse, error) {
	return callOwner(ctx, c, request, c.client.ListRelationships)
}

// RegisterProviderArtifactSignal routes an artifact signal to provider-hub.
func (c *Client) RegisterProviderArtifactSignal(ctx context.Context, request *providersv1.RegisterProviderArtifactSignalRequest) (*providersv1.ProviderArtifactSignalResponse, error) {
	return callOwner(ctx, c, request, c.client.RegisterProviderArtifactSignal)
}

// CreateIssue routes issue creation to provider-hub.
func (c *Client) CreateIssue(ctx context.Context, request *providersv1.CreateIssueRequest) (*providersv1.ProviderOperationResponse, error) {
	return callOwner(ctx, c, request, c.client.CreateIssue)
}

// UpdateIssue routes issue update to provider-hub.
func (c *Client) UpdateIssue(ctx context.Context, request *providersv1.UpdateIssueRequest) (*providersv1.ProviderOperationResponse, error) {
	return callOwner(ctx, c, request, c.client.UpdateIssue)
}

// CreateComment routes comment creation to provider-hub.
func (c *Client) CreateComment(ctx context.Context, request *providersv1.CreateCommentRequest) (*providersv1.ProviderOperationResponse, error) {
	return callOwner(ctx, c, request, c.client.CreateComment)
}

// UpdateComment routes comment update to provider-hub.
func (c *Client) UpdateComment(ctx context.Context, request *providersv1.UpdateCommentRequest) (*providersv1.ProviderOperationResponse, error) {
	return callOwner(ctx, c, request, c.client.UpdateComment)
}

// CreatePullRequest routes PR/MR creation to provider-hub.
func (c *Client) CreatePullRequest(ctx context.Context, request *providersv1.CreatePullRequestRequest) (*providersv1.ProviderOperationResponse, error) {
	return callOwner(ctx, c, request, c.client.CreatePullRequest)
}

// UpdatePullRequest routes PR/MR update to provider-hub.
func (c *Client) UpdatePullRequest(ctx context.Context, request *providersv1.UpdatePullRequestRequest) (*providersv1.ProviderOperationResponse, error) {
	return callOwner(ctx, c, request, c.client.UpdatePullRequest)
}

// CreateReviewSignal routes review signal creation to provider-hub.
func (c *Client) CreateReviewSignal(ctx context.Context, request *providersv1.CreateReviewSignalRequest) (*providersv1.ProviderOperationResponse, error) {
	return callOwner(ctx, c, request, c.client.CreateReviewSignal)
}

// UpdateRelationship routes relationship update to provider-hub.
func (c *Client) UpdateRelationship(ctx context.Context, request *providersv1.UpdateRelationshipRequest) (*providersv1.ProviderOperationResponse, error) {
	return callOwner(ctx, c, request, c.client.UpdateRelationship)
}

// CreateRepository routes repository creation to provider-hub.
func (c *Client) CreateRepository(ctx context.Context, request *providersv1.CreateRepositoryRequest) (*providersv1.ProviderOperationResponse, error) {
	return callOwner(ctx, c, request, c.client.CreateRepository)
}

// CreateBootstrapPullRequest routes bootstrap PR/MR creation to provider-hub.
func (c *Client) CreateBootstrapPullRequest(ctx context.Context, request *providersv1.CreateBootstrapPullRequestRequest) (*providersv1.ProviderOperationResponse, error) {
	return callOwner(ctx, c, request, c.client.CreateBootstrapPullRequest)
}

// CreateAdoptionPullRequest routes adoption PR/MR creation to provider-hub.
func (c *Client) CreateAdoptionPullRequest(ctx context.Context, request *providersv1.CreateAdoptionPullRequestRequest) (*providersv1.ProviderOperationResponse, error) {
	return callOwner(ctx, c, request, c.client.CreateAdoptionPullRequest)
}

func callOwner[Request any, Response any](
	ctx context.Context,
	client *Client,
	request Request,
	call func(context.Context, Request, ...grpc.CallOption) (Response, error),
) (Response, error) {
	cfg := client.callConfig()
	return ownergrpc.Call(ctx, cfg, request, call)
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
