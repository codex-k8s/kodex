// Package httptransport contains the integration-gateway HTTP boundary.
package httptransport

import (
	"context"
	stdhttp "net/http"
	"time"

	providerhubclient "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/clients/providerhub"
)

// Config contains the HTTP transport runtime contract.
type Config struct {
	ServiceName            string
	OpenAPISpecPath        string
	RequestTimeout         time.Duration
	MaxBodyBytes           int64
	ProviderWebhookEnabled bool
	AllowedProviderSlugs   []string
}

// ProviderHubClient is the owner-service client interface used by provider webhook routes.
type ProviderHubClient interface {
	IngestWebhookEvent(context.Context, providerhubclient.WebhookEvent) (providerhubclient.WebhookResult, error)
}

// ProviderWebhookVerifier verifies provider webhook authenticity before routing to provider-hub.
type ProviderWebhookVerifier interface {
	VerifyProviderWebhook(context.Context, *stdhttp.Request, ProviderWebhookVerificationInput) error
}

// ProviderWebhookVerificationInput is the redaction-safe verifier input.
type ProviderWebhookVerificationInput struct {
	ProviderSlug string
	Payload      []byte
}
