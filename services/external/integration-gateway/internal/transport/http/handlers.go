package httptransport

import (
	stdhttp "net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	providerhubclient "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/clients/providerhub"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/transport/http/generated"
)

type handlers struct {
	registry        routeRegistry
	providerHub     ProviderHubClient
	verifier        ProviderWebhookVerifier
	openAPISpecPath string
}

func newHandlers(registry routeRegistry, providerHub ProviderHubClient, verifier ProviderWebhookVerifier, openAPISpecPath string) handlers {
	return handlers{registry: registry, providerHub: providerHub, verifier: verifier, openAPISpecPath: openAPISpecPath}
}

func (h handlers) providerWebhook(c *echo.Context) error {
	providerSlug, err := echo.PathParam[string](c, "provider_slug")
	if err != nil {
		return NewSafeError(stdhttp.StatusBadRequest, CodeInvalidRequest, "provider slug is invalid", false)
	}
	providerSlug = strings.TrimSpace(providerSlug)
	if !h.registry.providerWebhookAllowed(providerSlug) {
		return NewSafeError(stdhttp.StatusBadRequest, CodeSourceNotAllowed, "provider webhook route is not active", false)
	}
	deliveryID, eventName, err := providerHeaders(c.Request())
	if err != nil {
		return err
	}
	body := requestBodyFromContext(c.Request().Context())
	if err := h.verifier.VerifyProviderWebhook(c.Request().Context(), c.Request(), ProviderWebhookVerificationInput{
		ProviderSlug: providerSlug,
		Payload:      body,
	}); err != nil {
		return WrapSafeError(stdhttp.StatusUnauthorized, CodeSignatureInvalid, "provider webhook signature is invalid", false, err)
	}
	result, err := h.providerHub.IngestWebhookEvent(c.Request().Context(), providerhubclient.WebhookEvent{
		ProviderSlug:  providerSlug,
		DeliveryID:    deliveryID,
		EventName:     eventName,
		PayloadJSON:   string(body),
		ReceivedAt:    time.Now().UTC(),
		RequestID:     requestIDFromContext(c.Request().Context()),
		CorrelationID: requestIDFromContext(c.Request().Context()),
	})
	if err != nil {
		return providerHubError(err)
	}
	status := generated.ProviderWebhookAcceptedStatusAccepted
	if result.Duplicate {
		status = generated.ProviderWebhookAcceptedStatusDuplicateAccepted
	}
	correlationID := requestIDFromContext(c.Request().Context())
	response := generated.ProviderWebhookAccepted{
		RequestId:                 requestIDFromContext(c.Request().Context()),
		CorrelationId:             correlationID,
		ProviderSlug:              providerSlug,
		DeliveryId:                deliveryID,
		EventName:                 eventName,
		ProviderHubWebhookEventId: optionalStringPtr(result.WebhookEventID),
		Status:                    status,
	}
	writeJSON(c.Response(), stdhttp.StatusAccepted, response)
	return nil
}

func (h handlers) externalCallback(c *echo.Context) error {
	return NewSafeError(stdhttp.StatusBadRequest, CodeSourceNotAllowed, "external callback route is not active", false)
}

func (h handlers) openAPISpec(c *echo.Context) error {
	data, err := readSpec(h.openAPISpecPath)
	if err != nil {
		return WrapSafeError(stdhttp.StatusInternalServerError, CodeDownstreamUnavailable, "OpenAPI spec is unavailable", true, err)
	}
	w := c.Response()
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(stdhttp.StatusOK)
	_, _ = w.Write(data)
	return nil
}

func providerHeaders(req *stdhttp.Request) (string, string, error) {
	deliveryID := strings.TrimSpace(req.Header.Get("X-GitHub-Delivery"))
	eventName := strings.TrimSpace(req.Header.Get("X-GitHub-Event"))
	if eventName == "" {
		eventName = strings.TrimSpace(req.Header.Get("X-Gitlab-Event"))
	}
	if deliveryID == "" {
		return "", "", NewSafeError(stdhttp.StatusBadRequest, CodeInvalidRequest, "delivery id is required", false)
	}
	if eventName == "" {
		return "", "", NewSafeError(stdhttp.StatusBadRequest, CodeInvalidRequest, "event name is required", false)
	}
	return deliveryID, eventName, nil
}

func optionalStringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
