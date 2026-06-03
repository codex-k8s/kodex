package projectcatalog

import (
	"context"
	"fmt"
	"time"

	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	"github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/clients/clientruntime"
	"google.golang.org/grpc"
)

type Config = clientruntime.Config

type Client struct {
	client    projectsv1.ProjectCatalogServiceClient
	authToken string
	timeout   time.Duration
}

func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return clientruntime.NewConnection(cfg.Addr, "project-catalog")
}

func New(client projectsv1.ProjectCatalogServiceClient, cfg Config) (*Client, error) {
	settings, err := projectCatalogSettings(client, cfg)
	if err != nil {
		return nil, err
	}
	output := &Client{client: client}
	output.authToken = settings.authToken
	output.timeout = settings.timeout
	return output, nil
}

func (c *Client) GetSelfDeploySignal(ctx context.Context, request *projectsv1.GetSelfDeploySignalRequest) (*projectsv1.SelfDeploySignalResponse, error) {
	return callQuery(ctx, c, request, c.client.GetSelfDeploySignal)
}

func (c *Client) ListRepositories(ctx context.Context, request *projectsv1.ListRepositoriesRequest) (*projectsv1.ListRepositoriesResponse, error) {
	return callQuery(ctx, c, request, c.client.ListRepositories)
}

type runtimeSettings struct {
	authToken string
	timeout   time.Duration
}

func projectCatalogSettings(client projectsv1.ProjectCatalogServiceClient, cfg Config) (runtimeSettings, error) {
	if client == nil {
		return runtimeSettings{}, fmt.Errorf("project-catalog client is required")
	}
	authToken, timeout, err := clientruntime.ClientSettings(false, "project-catalog", cfg.AuthToken, cfg.Timeout)
	if err != nil {
		return runtimeSettings{}, err
	}
	return runtimeSettings{authToken: authToken, timeout: timeout}, nil
}

type queryRequest interface {
	GetMeta() *projectsv1.QueryMeta
}

func callQuery[Request queryRequest, Response any](ctx context.Context, client *Client, request Request, invoke func(context.Context, Request, ...grpc.CallOption) (*Response, error)) (*Response, error) {
	metadata := projectRequestMetadata(client.authToken, request.GetMeta())
	callCtx, cancel := clientruntime.OutgoingCallContext(ctx, metadata, client.timeout)
	defer cancel()
	response, err := invoke(callCtx, request)
	return response, err
}

func projectRequestMetadata(authToken string, meta *projectsv1.QueryMeta) clientruntime.RequestMetadata {
	requestID := ""
	traceID := ""
	if meta != nil {
		requestID = meta.GetRequestId()
		if context := meta.GetRequestContext(); context != nil {
			traceID = context.GetTraceId()
		}
	}
	return clientruntime.RequestMetadata{AuthToken: authToken, RequestID: requestID, CorrelationID: traceID}
}
