package httptransport

import (
	"encoding/json"
	stdhttp "net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	interactionhubclient "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/clients/interactionhub"
	providerhubclient "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/clients/providerhub"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/transport/http/generated"
)

type handlers struct {
	registry         routeRegistry
	providerHub      ProviderHubClient
	interactionHub   InteractionHubClient
	providerVerifier ProviderWebhookVerifier
	externalVerifier ExternalCallbackVerifier
	webhookGuard     *providerWebhookGuard
	openAPISpecPath  string
}

func newHandlers(
	registry routeRegistry,
	providerHub ProviderHubClient,
	interactionHub InteractionHubClient,
	providerVerifier ProviderWebhookVerifier,
	externalVerifier ExternalCallbackVerifier,
	webhookGuard *providerWebhookGuard,
	openAPISpecPath string,
) handlers {
	return handlers{
		registry:         registry,
		providerHub:      providerHub,
		interactionHub:   interactionHub,
		providerVerifier: providerVerifier,
		externalVerifier: externalVerifier,
		webhookGuard:     webhookGuard,
		openAPISpecPath:  openAPISpecPath,
	}
}

func (h handlers) providerWebhook(c *echo.Context) error {
	providerSlug, err := echo.PathParam[string](c, "provider_slug")
	if err != nil {
		return NewSafeError(stdhttp.StatusBadRequest, CodeInvalidRequest, "provider slug is invalid", false)
	}
	providerSlug = strings.TrimSpace(providerSlug)
	setRouteDiagnostics(c.Request(), routeIDProviderWebhook, providerSlug)
	if !h.registry.providerWebhookAllowed(providerSlug) {
		return NewSafeError(stdhttp.StatusBadRequest, CodeSourceNotAllowed, "provider webhook route is not active", false)
	}
	deliveryID, eventName, err := providerHeaders(c.Request())
	if err != nil {
		return err
	}
	body := requestBodyFromContext(c.Request().Context())
	if err := h.providerVerifier.VerifyProviderWebhook(c.Request().Context(), c.Request(), ProviderWebhookVerificationInput{
		ProviderSlug: providerSlug,
		Payload:      body,
	}); err != nil {
		return providerWebhookVerificationError(err)
	}
	lease, safeErr := h.webhookGuard.acquire(routeIDProviderWebhook, providerSlug)
	if safeErr != nil {
		return safeErr
	}
	defer lease.Release()
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
	callbackSource, err := echo.PathParam[string](c, "callback_source")
	if err != nil {
		return NewSafeError(stdhttp.StatusBadRequest, CodeInvalidRequest, "callback source is invalid", false)
	}
	callbackSource = strings.TrimSpace(callbackSource)
	setRouteDiagnostics(c.Request(), routeIDExternalCallback, callbackSource)
	if !h.registry.externalCallbackAllowed(callbackSource) {
		return NewSafeError(stdhttp.StatusBadRequest, CodeSourceNotAllowed, "external callback route is not active", false)
	}
	body := requestBodyFromContext(c.Request().Context())
	if err := h.externalVerifier.VerifyExternalCallback(c.Request().Context(), c.Request(), ExternalCallbackVerificationInput{
		CallbackSource: callbackSource,
		Payload:        body,
	}); err != nil {
		return externalCallbackVerificationError(err)
	}
	callback, err := externalCallbackEnvelope(c.Request(), callbackSource, body)
	if err != nil {
		return err
	}
	lease, safeErr := h.webhookGuard.acquire(routeIDExternalCallback, callbackSource)
	if safeErr != nil {
		return safeErr
	}
	defer lease.Release()
	result, err := h.interactionHub.RecordChannelCallback(c.Request().Context(), callback)
	if err != nil {
		return interactionHubError(err)
	}
	callbackID := callback.CallbackID
	if strings.TrimSpace(result.CallbackID) != "" {
		callbackID = result.CallbackID
	}
	response := generated.ExternalCallbackAccepted{
		RequestId:      requestIDFromContext(c.Request().Context()),
		CorrelationId:  callback.CorrelationID,
		CallbackSource: callbackSource,
		CallbackId:     callbackID,
		DeliveryId:     optionalStringPtr(callback.DeliveryID),
		RequestRef:     optionalStringPtr(callback.RequestRef),
		OwnerService:   optionalStringPtr("interaction-hub"),
		Status:         generated.ExternalCallbackAcceptedStatusAccepted,
	}
	writeJSON(c.Response(), stdhttp.StatusAccepted, response)
	return nil
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
	if deliveryID == "" {
		return "", "", NewSafeError(stdhttp.StatusBadRequest, CodeInvalidRequest, "delivery id is required", false)
	}
	if eventName == "" {
		return "", "", NewSafeError(stdhttp.StatusBadRequest, CodeInvalidRequest, "event name is required", false)
	}
	return deliveryID, eventName, nil
}

func externalCallbackEnvelope(req *stdhttp.Request, callbackSource string, body []byte) (interactionhubclient.CallbackEnvelope, error) {
	var payload generated.ExternalChannelCallbackRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		return interactionhubclient.CallbackEnvelope{}, WrapSafeError(stdhttp.StatusBadRequest, CodeInvalidRequest, "external callback body is invalid", false, err)
	}
	if err := externalCallbackHeaderMatches(req, payload.CallbackId); err != nil {
		return interactionhubclient.CallbackEnvelope{}, err
	}
	correlationID := stringValue(payload.CorrelationId)
	if correlationID == "" {
		correlationID = requestIDFromContext(req.Context())
	}
	callback := interactionhubclient.CallbackEnvelope{
		CallbackSource:  strings.ToLower(strings.TrimSpace(callbackSource)),
		ContractVersion: strings.TrimSpace(payload.ContractVersion),
		CallbackID:      strings.TrimSpace(payload.CallbackId),
		DeliveryID:      stringValue(payload.DeliveryId),
		RequestRef:      stringValue(payload.RequestRef),
		ActorRef:        stringValue(payload.ActorRef),
		Action:          strings.TrimSpace(payload.Action),
		AnswerSummary:   stringValue(payload.AnswerSummary),
		GatewayRef:      gatewayRef(callbackSource, requestIDFromContext(req.Context())),
		ReceivedAt:      time.Now().UTC(),
		RequestID:       requestIDFromContext(req.Context()),
		CorrelationID:   correlationID,
	}
	if payload.AnswerObject != nil {
		callback.AnswerObject = interactionhubclient.ObjectRef{
			URI:       strings.TrimSpace(payload.AnswerObject.ObjectUri),
			Digest:    strings.TrimSpace(payload.AnswerObject.ObjectDigest),
			SizeBytes: payload.AnswerObject.ObjectSizeBytes,
		}
	}
	if err := validateExternalCallbackEnvelope(callback); err != nil {
		return interactionhubclient.CallbackEnvelope{}, err
	}
	return callback, nil
}

func externalCallbackHeaderMatches(req *stdhttp.Request, callbackID string) error {
	headerID := strings.TrimSpace(req.Header.Get("X-Kodex-External-Delivery"))
	if headerID == "" {
		return nil
	}
	if headerID != strings.TrimSpace(callbackID) {
		return NewSafeError(stdhttp.StatusBadRequest, CodeInvalidRequest, "external callback idempotency key is invalid", false)
	}
	return nil
}

func validateExternalCallbackEnvelope(callback interactionhubclient.CallbackEnvelope) error {
	if callback.ContractVersion == "" ||
		callback.CallbackID == "" ||
		callback.Action == "" ||
		(callback.DeliveryID == "" && callback.RequestRef == "") ||
		callback.CorrelationID == "" {
		return NewSafeError(stdhttp.StatusBadRequest, CodeInvalidRequest, "external callback envelope is invalid", false)
	}
	return nil
}

func gatewayRef(callbackSource string, requestID string) string {
	source := strings.ToLower(strings.TrimSpace(callbackSource))
	if source == "" {
		source = "unknown"
	}
	return "integration-gateway/" + source + "/" + strings.TrimSpace(requestID)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func optionalStringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
