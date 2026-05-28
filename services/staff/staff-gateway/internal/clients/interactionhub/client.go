package interactionhub

import (
	"context"
	"time"

	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	"github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/clients/clientruntime"
	"google.golang.org/grpc"
)

type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

type Client struct {
	client    interactionsv1.InteractionHubServiceClient
	authToken string
	timeout   time.Duration
}

type runtimeSettings struct {
	authToken string
	timeout   time.Duration
}

func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return clientruntime.NewConnection(cfg.Addr, "interaction-hub")
}

func New(client interactionsv1.InteractionHubServiceClient, cfg Config) (*Client, error) {
	settings, err := newRuntimeSettings(client, cfg)
	if err != nil {
		return nil, err
	}
	return &Client{client: client, authToken: settings.authToken, timeout: settings.timeout}, nil
}

func newRuntimeSettings(client interactionsv1.InteractionHubServiceClient, cfg Config) (runtimeSettings, error) {
	authToken, timeout, err := clientruntime.ClientSettings(client == nil, "interaction-hub", cfg.AuthToken, cfg.Timeout)
	if err != nil {
		return runtimeSettings{}, err
	}
	return runtimeSettings{authToken: authToken, timeout: timeout}, nil
}

func (c *Client) ListOwnerInboxItems(ctx context.Context, request *interactionsv1.ListOwnerInboxItemsRequest) (*interactionsv1.ListOwnerInboxItemsResponse, error) {
	return invokeQuery(ctx, c, request.GetMeta(), func(callCtx context.Context) (*interactionsv1.ListOwnerInboxItemsResponse, error) {
		return c.client.ListOwnerInboxItems(callCtx, request)
	})
}

func (c *Client) GetOwnerInboxItem(ctx context.Context, request *interactionsv1.GetOwnerInboxItemRequest) (*interactionsv1.OwnerInboxItemResponse, error) {
	callCtx, cancel := c.callContext(ctx, request.GetMeta())
	defer cancel()
	response, err := c.client.GetOwnerInboxItem(callCtx, request)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func (c *Client) RecordInteractionResponse(ctx context.Context, request *interactionsv1.RecordInteractionResponseRequest) (*interactionsv1.InteractionResponseResponse, error) {
	callCtx, cancel := c.callContext(ctx, queryMetaFromCommand(request.GetMeta()))
	defer cancel()
	return c.client.RecordInteractionResponse(callCtx, request)
}

func (c *Client) callContext(ctx context.Context, meta *interactionsv1.QueryMeta) (context.Context, context.CancelFunc) {
	requestID := ""
	correlationID := ""
	if meta != nil {
		requestID = meta.GetRequestId()
		if meta.GetRequestContext() != nil {
			correlationID = meta.GetRequestContext().GetTraceId()
		}
	}
	outgoing := clientruntime.OutgoingContext(ctx, clientruntime.RequestMetadata{
		AuthToken:     c.authToken,
		RequestID:     requestID,
		CorrelationID: correlationID,
	})
	return context.WithTimeout(outgoing, c.timeout)
}

func invokeQuery[Response any](
	ctx context.Context,
	client *Client,
	meta *interactionsv1.QueryMeta,
	call func(context.Context) (*Response, error),
) (*Response, error) {
	callCtx, cancel := client.callContext(ctx, meta)
	defer cancel()
	return call(callCtx)
}

func queryMetaFromCommand(meta *interactionsv1.CommandMeta) *interactionsv1.QueryMeta {
	if meta == nil {
		return nil
	}
	return &interactionsv1.QueryMeta{
		Actor:          meta.GetActor(),
		RequestId:      meta.GetRequestId(),
		RequestContext: meta.GetRequestContext(),
	}
}
