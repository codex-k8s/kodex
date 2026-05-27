// Package httptransport contains the integration-gateway HTTP boundary.
package httptransport

import (
	"context"
	stdhttp "net/http"
	"time"

	interactionhubclient "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/clients/interactionhub"
	providerhubclient "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/clients/providerhub"
)

// Config contains the HTTP transport runtime contract.
type Config struct {
	ServiceName                     string
	OpenAPISpecPath                 string
	RequestTimeout                  time.Duration
	MaxBodyBytes                    int64
	ProviderWebhookEnabled          bool
	AllowedProviderSlugs            []string
	ProviderWebhookMaxInFlight      int
	ProviderWebhookRateLimitBurst   int
	ProviderWebhookRateLimitWindow  time.Duration
	ProviderWebhookRetryAfter       time.Duration
	ExternalCallbackEnabled         bool
	AllowedCallbackSources          []string
	ExternalCallbackMaxInFlight     int
	ExternalCallbackRateLimitBurst  int
	ExternalCallbackRateLimitWindow time.Duration
	ExternalCallbackRetryAfter      time.Duration
}

// ProviderHubClient is the owner-service client interface used by provider webhook routes.
type ProviderHubClient interface {
	IngestWebhookEvent(context.Context, providerhubclient.WebhookEvent) (providerhubclient.WebhookResult, error)
}

// InteractionHubClient is the owner-service client interface used by external callback routes.
type InteractionHubClient interface {
	RecordChannelCallback(context.Context, interactionhubclient.CallbackEnvelope) (interactionhubclient.CallbackResult, error)
}

// ProviderWebhookVerifier verifies provider webhook authenticity before routing to provider-hub.
type ProviderWebhookVerifier interface {
	VerifyProviderWebhook(context.Context, *stdhttp.Request, ProviderWebhookVerificationInput) error
}

// ExternalCallbackVerifier verifies external callback authenticity before routing to interaction-hub.
type ExternalCallbackVerifier interface {
	VerifyExternalCallback(context.Context, *stdhttp.Request, ExternalCallbackVerificationInput) error
}

// ProviderWebhookVerificationInput is the redaction-safe verifier input.
type ProviderWebhookVerificationInput struct {
	ProviderSlug string
	Payload      []byte
}

// ExternalCallbackVerificationInput is the redaction-safe verifier input.
type ExternalCallbackVerificationInput struct {
	CallbackSource string
	Payload        []byte
}
