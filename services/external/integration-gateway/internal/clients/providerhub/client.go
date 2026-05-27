// Package providerhub contains integration-gateway's provider-hub client boundary.
package providerhub

import (
	"context"
	"fmt"
	"strings"
	"time"

	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/clients/clientruntime"
	"google.golang.org/grpc"
)

// Config contains provider-hub gRPC connection settings.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

// WebhookEvent is the safe edge envelope forwarded to provider-hub.
type WebhookEvent struct {
	ProviderSlug         string
	DeliveryID           string
	EventName            string
	RepositoryProviderID string
	PayloadJSON          string
	ReceivedAt           time.Time
	RequestID            string
	CorrelationID        string
	ClientIPHash         string
}

// WebhookResult contains the safe provider-hub response used by HTTP handlers.
type WebhookResult struct {
	WebhookEventID string
	Duplicate      bool
}

// Client calls provider-hub with platform service metadata.
type Client struct {
	client    providersv1.ProviderHubServiceClient
	authToken string
	timeout   time.Duration
}

// Disabled is used while the provider webhook route stays inactive.
type Disabled struct{}

// IngestWebhookEvent reports that the provider route is not active in this process.
func (Disabled) IngestWebhookEvent(context.Context, WebhookEvent) (WebhookResult, error) {
	return WebhookResult{}, ErrDisabled
}

// ErrDisabled is returned by the disabled provider-hub client.
var ErrDisabled = fmt.Errorf("provider webhook route is disabled")

// NewConnection creates a gRPC client connection to provider-hub.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return clientruntime.NewConnection(cfg.Addr, "provider-hub")
}

// New wraps a generated provider-hub client.
func New(client providersv1.ProviderHubServiceClient, cfg Config) (*Client, error) {
	if client == nil {
		return nil, fmt.Errorf("provider-hub client is required")
	}
	authToken, err := clientruntime.RequiredValue(cfg.AuthToken, "provider-hub auth token")
	if err != nil {
		return nil, err
	}
	timeout := clientruntime.EffectiveTimeout(cfg.Timeout)
	return &Client{client: client, authToken: authToken, timeout: timeout}, nil
}

// IngestWebhookEvent forwards the verified webhook envelope to provider-hub.
func (c *Client) IngestWebhookEvent(ctx context.Context, event WebhookEvent) (WebhookResult, error) {
	callCtx, cancel := context.WithTimeout(outgoingContext(ctx, c.authToken, event), c.timeout)
	defer cancel()
	idempotencyKey := webhookIdempotencyKey(event)
	request := &providersv1.IngestWebhookEventRequest{
		ProviderSlug: event.ProviderSlug,
		DeliveryId:   event.DeliveryID,
		EventName:    event.EventName,
		PayloadJson:  event.PayloadJSON,
		ReceivedAt:   event.ReceivedAt.UTC().Format(time.RFC3339Nano),
		Meta:         providerCommandMeta(event, idempotencyKey),
	}
	if event.RepositoryProviderID != "" {
		request.RepositoryProviderId = &event.RepositoryProviderID
	}
	response, err := c.client.IngestWebhookEvent(callCtx, request)
	if err != nil {
		return WebhookResult{}, err
	}
	result := WebhookResult{}
	if response.GetWebhookEvent() != nil {
		result.WebhookEventID = response.GetWebhookEvent().GetWebhookEventId()
	}
	return result, nil
}

func providerCommandMeta(event WebhookEvent, idempotencyKey string) *providersv1.CommandMeta {
	context := &providersv1.RequestContext{Source: clientruntime.CallerID}
	context.TraceId = clientruntime.OptionalString(event.CorrelationID)
	context.ClientIpHash = clientruntime.OptionalString(event.ClientIPHash)
	return &providersv1.CommandMeta{
		IdempotencyKey: &idempotencyKey,
		Actor:          &providersv1.Actor{Type: "service", Id: clientruntime.CallerID},
		Reason:         "provider webhook edge ingress",
		RequestId:      event.RequestID,
		RequestContext: context,
	}
}

func webhookIdempotencyKey(event WebhookEvent) string {
	providerSlug := strings.TrimSpace(event.ProviderSlug)
	deliveryID := strings.TrimSpace(event.DeliveryID)
	if providerSlug == "" {
		return deliveryID
	}
	if deliveryID == "" {
		return providerSlug
	}
	return providerSlug + ":" + deliveryID
}

func outgoingContext(ctx context.Context, authToken string, event WebhookEvent) context.Context {
	return clientruntime.OutgoingContext(ctx, clientruntime.RequestMetadata{
		AuthToken:     authToken,
		RequestID:     event.RequestID,
		CorrelationID: event.CorrelationID,
	})
}
