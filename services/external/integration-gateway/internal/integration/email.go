package integration

import (
	"context"
	"net/http"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

func (adapter *Adapter) testEmail(ctx context.Context, request Request, configuration map[string]string) error {
	definition := adapter.definitions["email"]
	capability, _ := definition.Capability(definition.Spec.HealthCheck.Operation)
	_, err := adapter.emailJSON(ctx, request, capability, configuration, http.MethodGet, "/v1/health", nil, "")
	return err
}

func (adapter *Adapter) executeEmail(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte) (Result, error) {
	switch request.Operation {
	case "email.delivery.health.read":
		body, err := adapter.emailJSON(ctx, request, capability, configuration, http.MethodGet, "/v1/health", nil, "")
		if err != nil {
			return Result{}, err
		}
		var provider struct {
			Status string `json:"status"`
		}
		if decodeProviderJSON(body, &provider) != nil || provider.Status == "" {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "email-bridge:health", map[string]any{"status": provider.Status})
	case "email.message.send":
		var input struct {
			To       string `json:"to"`
			Subject  string `json:"subject"`
			BodyText string `json:"body_text"`
		}
		if decodeProviderJSON(canonicalInput, &input) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		body, err := adapter.emailJSON(ctx, request, capability, configuration, http.MethodPost, "/v1/messages",
			map[string]any{"from": configuration["from_address"], "to": input.To, "subject": input.Subject, "body_text": input.BodyText}, request.EffectKey)
		if err != nil {
			return Result{}, err
		}
		var provider struct {
			MessageID string `json:"message_id"`
			Status    string `json:"status"`
		}
		if decodeProviderJSON(body, &provider) != nil || provider.MessageID == "" || provider.Status == "" {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "email-message:"+provider.MessageID, map[string]any{"message_id": provider.MessageID, "status": provider.Status})
	default:
		return Result{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
}

func (adapter *Adapter) emailJSON(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, method, path string, body any, effectKey string) ([]byte, error) {
	return adapter.callProvider(ctx, providerCall{
		BaseURL: configuration["base_url"], Method: method, Path: path, Body: body,
		AuthScheme: "BEARER", Credential: request.Credential, EffectKey: effectKey, Capability: capability,
	})
}
