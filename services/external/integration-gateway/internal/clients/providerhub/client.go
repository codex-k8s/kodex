// Package providerhub contains integration-gateway's provider-hub client boundary.
package providerhub

import (
	"context"
	"fmt"
	"strings"
	"time"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const callerID = "integration-gateway"

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
	addr, err := requiredValue(cfg.Addr, "provider-hub address")
	if err != nil {
		return nil, err
	}
	return grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// New wraps a generated provider-hub client.
func New(client providersv1.ProviderHubServiceClient, cfg Config) (*Client, error) {
	if client == nil {
		return nil, fmt.Errorf("provider-hub client is required")
	}
	authToken, err := requiredValue(cfg.AuthToken, "provider-hub auth token")
	if err != nil {
		return nil, err
	}
	return &Client{client: client, authToken: authToken, timeout: effectiveTimeout(cfg.Timeout)}, nil
}

// IngestWebhookEvent forwards the verified webhook envelope to provider-hub.
func (c *Client) IngestWebhookEvent(ctx context.Context, event WebhookEvent) (WebhookResult, error) {
	callCtx, cancel := context.WithTimeout(outgoingContext(ctx, c.authToken, event), c.timeout)
	defer cancel()
	request := &providersv1.IngestWebhookEventRequest{
		ProviderSlug: event.ProviderSlug,
		DeliveryId:   event.DeliveryID,
		EventName:    event.EventName,
		PayloadJson:  event.PayloadJSON,
		ReceivedAt:   event.ReceivedAt.UTC().Format(time.RFC3339Nano),
		Meta: &providersv1.CommandMeta{
			IdempotencyKey: &event.DeliveryID,
			Actor:          &providersv1.Actor{Type: "service", Id: callerID},
			Reason:         "provider webhook edge ingress",
			RequestId:      event.RequestID,
			RequestContext: &providersv1.RequestContext{
				Source:       callerID,
				TraceId:      optionalString(event.CorrelationID),
				ClientIpHash: optionalString(event.ClientIPHash),
			},
		},
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

func outgoingContext(ctx context.Context, authToken string, event WebhookEvent) context.Context {
	values := []string{
		grpcserver.MetadataAuthorization,
		"Bearer " + strings.TrimSpace(authToken),
		grpcserver.MetadataCallerType,
		"service",
		grpcserver.MetadataCallerID,
		callerID,
		grpcserver.MetadataRequestSource,
		callerID,
	}
	if event.RequestID != "" {
		values = append(values, grpcserver.MetadataRequestID, event.RequestID)
	}
	if event.CorrelationID != "" {
		values = append(values, grpcserver.MetadataTraceID, event.CorrelationID)
	}
	return metadata.AppendToOutgoingContext(ctx, values...)
}

func requiredValue(value string, name string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return trimmed, nil
}

func effectiveTimeout(value time.Duration) time.Duration {
	if value <= 0 {
		return 3 * time.Second
	}
	return value
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
