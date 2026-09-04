package integration

import (
	"context"
	"net/http"
	"net/url"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/generated/emailbridge"
)

func (adapter *Adapter) testEmail(ctx context.Context, request Request, configuration map[string]string) error {
	definition := adapter.definitions["email"]
	capability, _ := definition.Capability(definition.Spec.HealthCheck.Operation)
	body, err := adapter.emailJSON(ctx, request, capability, configuration, http.MethodGet, "/v1/health", nil, "")
	if err == nil {
		err = emailHealth(body)
	}
	return err
}

func emailHealth(body []byte) error {
	var health emailbridge.Health
	if decodeProviderJSON(body, &health) != nil || health.Status != emailbridge.Ready {
		return &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	return nil
}

func (adapter *Adapter) executeEmail(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte) (Result, error) {
	switch request.Operation {
	case "email.message.status.read":
		var input struct {
			MessageID string `json:"message_id"`
		}
		if decodeProviderJSON(canonicalInput, &input) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		body, err := adapter.emailJSON(ctx, request, capability, configuration, http.MethodGet, "/v1/messages/"+url.PathEscape(input.MessageID), nil, "")
		if err != nil {
			return Result{}, err
		}
		var status emailbridge.MessageStatus
		if decodeProviderJSON(body, &status) != nil || status.MessageId != input.MessageID || !status.Status.Valid() {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "email-message:"+status.MessageId, status)
	case "email.delivery.health.read":
		body, err := adapter.emailJSON(ctx, request, capability, configuration, http.MethodGet, "/v1/health", nil, "")
		if err != nil {
			return Result{}, err
		}
		var provider struct {
			Status string `json:"status"`
		}
		if decodeProviderJSON(body, &provider) != nil || emailHealth(body) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "email-bridge:health", map[string]any{"status": provider.Status})
	case "email.message.send":
		if err := adapter.testEmail(ctx, request, configuration); err != nil {
			return Result{}, err
		}
		var input struct {
			To       string `json:"to"`
			Subject  string `json:"subject"`
			BodyText string `json:"body_text"`
		}
		if decodeProviderJSON(canonicalInput, &input) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		body, err := adapter.emailJSON(ctx, request, capability, configuration, http.MethodPost, "/v1/messages",
			emailbridge.MessageInput{From: configuration["from_address"], To: input.To, Subject: input.Subject, BodyText: input.BodyText}, request.EffectKey)
		if IsUnknownOutcome(err) {
			if reconciled, reconcileErr := adapter.emailJSON(ctx, request, capability, configuration, http.MethodGet,
				"/v1/messages/by-idempotency-key/"+url.PathEscape(request.EffectKey), nil, ""); reconcileErr == nil {
				body, err = reconciled, nil
			}
		}
		if err != nil {
			return Result{}, err
		}
		var provider emailbridge.MessageStatus
		if decodeProviderJSON(body, &provider) != nil || provider.MessageId == "" || !provider.Status.Valid() {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		if provider.Status == emailbridge.Unknown {
			return Result{}, &UnknownOutcomeError{}
		}
		return providerResult(request, "email-message:"+provider.MessageId, provider)
	default:
		return Result{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
}

func (adapter *Adapter) emailJSON(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, method, path string, body any, effectKey string) ([]byte, error) {
	return adapter.callProvider(ctx, providerCall{
		BaseURL: configuration["base_url"], Method: method, Path: path, Body: body, Query: url.Values{"sender": {configuration["from_address"]}},
		AuthScheme: "BEARER", Credential: request.Credential, EffectKey: effectKey, Capability: capability,
	})
}
