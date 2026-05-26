package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	providerhubclient "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/clients/providerhub"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/transport/http/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRouterServesOpenAPISpec(t *testing.T) {
	router := newTestRouter(t, Config{
		ServiceName:            "integration-gateway",
		OpenAPISpecPath:        "../../../../../../specs/openapi/integration-gateway.v1.yaml",
		RequestTimeout:         time.Second,
		MaxBodyBytes:           1024,
		ProviderWebhookEnabled: false,
		AllowedProviderSlugs:   []string{"github"},
	}, &fakeProviderHub{})

	req := httptest.NewRequest(http.MethodGet, "/openapi/integration-gateway.v1.yaml", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "kodex integration-gateway API") {
		t.Fatalf("OpenAPI body does not contain expected title")
	}
}

func TestProviderWebhookRouteDisabledReturnsSafeError(t *testing.T) {
	router := newTestRouter(t, Config{
		ServiceName:            "integration-gateway",
		OpenAPISpecPath:        "../../../../../../specs/openapi/integration-gateway.v1.yaml",
		RequestTimeout:         time.Second,
		MaxBodyBytes:           1024,
		ProviderWebhookEnabled: false,
		AllowedProviderSlugs:   []string{"github"},
	}, &fakeProviderHub{})

	req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(`{"zen":"keep it logically small"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	req.Header.Set("X-GitHub-Event", "ping")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	var body generated.SafeError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode SafeError: %v", err)
	}
	if body.Code != generated.SafeErrorCodeSourceNotAllowed || body.RequestId == "" || body.Retryable {
		t.Fatalf("SafeError = %+v, want source_not_allowed with request_id", body)
	}
}

func TestProviderWebhookCallsProviderHubWhenEnabled(t *testing.T) {
	providerHub := &fakeProviderHub{result: providerhubclient.WebhookResult{WebhookEventID: "webhook-1"}}
	router := newTestRouterWithVerifier(t, Config{
		ServiceName:            "integration-gateway",
		OpenAPISpecPath:        "../../../../../../specs/openapi/integration-gateway.v1.yaml",
		RequestTimeout:         time.Second,
		MaxBodyBytes:           1024,
		ProviderWebhookEnabled: true,
		AllowedProviderSlugs:   []string{"github"},
	}, providerHub, allowAllVerifier{})

	req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(`{"action":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Delivery", "delivery-2")
	req.Header.Set("X-GitHub-Event", "ping")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if providerHub.event.ProviderSlug != "github" || providerHub.event.DeliveryID != "delivery-2" || providerHub.event.EventName != "ping" {
		t.Fatalf("providerHub event = %+v", providerHub.event)
	}
	if providerHub.event.PayloadJSON != `{"action":"ping"}` {
		t.Fatalf("payload = %q", providerHub.event.PayloadJSON)
	}
}

func TestProviderWebhookRejectsWhenVerifierMissing(t *testing.T) {
	providerHub := &fakeProviderHub{}
	router := newTestRouter(t, Config{
		ServiceName:            "integration-gateway",
		OpenAPISpecPath:        "../../../../../../specs/openapi/integration-gateway.v1.yaml",
		RequestTimeout:         time.Second,
		MaxBodyBytes:           1024,
		ProviderWebhookEnabled: true,
		AllowedProviderSlugs:   []string{"github"},
	}, providerHub)

	req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(`{"action":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Delivery", "delivery-verify")
	req.Header.Set("X-GitHub-Event", "ping")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	var body generated.SafeError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode SafeError: %v", err)
	}
	if body.Code != generated.SafeErrorCodeSignatureInvalid {
		t.Fatalf("code = %s, want signature_invalid", body.Code)
	}
	if providerHub.event.ProviderSlug != "" {
		t.Fatalf("provider hub was called: %+v", providerHub.event)
	}
}

func TestProviderWebhookRejectsUndeclaredExternalDeliveryFallback(t *testing.T) {
	providerHub := &fakeProviderHub{}
	router := newTestRouter(t, Config{
		ServiceName:            "integration-gateway",
		OpenAPISpecPath:        "../../../../../../specs/openapi/integration-gateway.v1.yaml",
		RequestTimeout:         time.Second,
		MaxBodyBytes:           1024,
		ProviderWebhookEnabled: true,
		AllowedProviderSlugs:   []string{"github"},
	}, providerHub)

	req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(`{"action":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Kodex-External-Delivery", "external-delivery")
	req.Header.Set("X-GitHub-Event", "ping")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	var body generated.SafeError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode SafeError: %v", err)
	}
	if body.Code != generated.SafeErrorCodeInvalidRequest {
		t.Fatalf("code = %s, want invalid_request", body.Code)
	}
	if providerHub.event.ProviderSlug != "" {
		t.Fatalf("provider hub was called: %+v", providerHub.event)
	}
}

func TestProviderHubErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		code       codes.Code
		statusCode int
		errorCode  generated.SafeErrorCode
		retryable  bool
	}{
		{
			name:       "invalid argument",
			code:       codes.InvalidArgument,
			statusCode: http.StatusBadRequest,
			errorCode:  generated.SafeErrorCodeInvalidRequest,
			retryable:  false,
		},
		{
			name:       "failed precondition",
			code:       codes.FailedPrecondition,
			statusCode: http.StatusBadRequest,
			errorCode:  generated.SafeErrorCodeInvalidRequest,
			retryable:  false,
		},
		{
			name:       "resource exhausted",
			code:       codes.ResourceExhausted,
			statusCode: http.StatusTooManyRequests,
			errorCode:  generated.SafeErrorCodeRateLimited,
			retryable:  true,
		},
		{
			name:       "deadline exceeded",
			code:       codes.DeadlineExceeded,
			statusCode: http.StatusServiceUnavailable,
			errorCode:  generated.SafeErrorCodeDownstreamUnavailable,
			retryable:  true,
		},
		{
			name:       "unavailable",
			code:       codes.Unavailable,
			statusCode: http.StatusServiceUnavailable,
			errorCode:  generated.SafeErrorCodeDownstreamUnavailable,
			retryable:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providerHub := &fakeProviderHub{err: status.Error(tt.code, "internal provider-hub detail")}
			router := newTestRouterWithVerifier(t, Config{
				ServiceName:            "integration-gateway",
				OpenAPISpecPath:        "../../../../../../specs/openapi/integration-gateway.v1.yaml",
				RequestTimeout:         time.Second,
				MaxBodyBytes:           1024,
				ProviderWebhookEnabled: true,
				AllowedProviderSlugs:   []string{"github"},
			}, providerHub, allowAllVerifier{})

			req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(`{"action":"ping"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-GitHub-Delivery", "delivery-error")
			req.Header.Set("X-GitHub-Event", "ping")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.statusCode {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.statusCode, rec.Body.String())
			}
			var body generated.SafeError
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode SafeError: %v", err)
			}
			if body.Code != tt.errorCode || body.Retryable != tt.retryable {
				t.Fatalf("SafeError = %+v, want code %s retryable %t", body, tt.errorCode, tt.retryable)
			}
			if strings.Contains(body.Message, "internal provider-hub detail") {
				t.Fatalf("SafeError leaked downstream details: %+v", body)
			}
		})
	}
}

func TestPayloadTooLargeReturnsSafeError(t *testing.T) {
	router := newTestRouter(t, Config{
		ServiceName:            "integration-gateway",
		OpenAPISpecPath:        "../../../../../../specs/openapi/integration-gateway.v1.yaml",
		RequestTimeout:         time.Second,
		MaxBodyBytes:           8,
		ProviderWebhookEnabled: true,
		AllowedProviderSlugs:   []string{"github"},
	}, &fakeProviderHub{})

	req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(`{"too":"large"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Delivery", "delivery-3")
	req.Header.Set("X-GitHub-Event", "ping")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	var body generated.SafeError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode SafeError: %v", err)
	}
	if body.Code != generated.SafeErrorCodePayloadTooLarge {
		t.Fatalf("code = %s, want payload_too_large", body.Code)
	}
}

func TestOpenAPIValidationRejectsMalformedJSON(t *testing.T) {
	router := newTestRouter(t, Config{
		ServiceName:            "integration-gateway",
		OpenAPISpecPath:        "../../../../../../specs/openapi/integration-gateway.v1.yaml",
		RequestTimeout:         time.Second,
		MaxBodyBytes:           1024,
		ProviderWebhookEnabled: true,
		AllowedProviderSlugs:   []string{"github"},
	}, &fakeProviderHub{})

	req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(`{"broken"`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Delivery", "delivery-4")
	req.Header.Set("X-GitHub-Event", "ping")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var body generated.SafeError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode SafeError: %v", err)
	}
	if body.Code != generated.SafeErrorCodeInvalidRequest {
		t.Fatalf("code = %s, want invalid_request", body.Code)
	}
}

func newTestRouter(t *testing.T, cfg Config, providerHub ProviderHubClient) *Router {
	t.Helper()
	router, err := NewRouter(context.Background(), cfg, providerHub, nil)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	return router
}

func newTestRouterWithVerifier(t *testing.T, cfg Config, providerHub ProviderHubClient, verifier ProviderWebhookVerifier) *Router {
	t.Helper()
	router, err := NewRouterWithVerifier(context.Background(), cfg, providerHub, verifier, nil)
	if err != nil {
		t.Fatalf("NewRouterWithVerifier() error = %v", err)
	}
	return router
}

type allowAllVerifier struct{}

func (allowAllVerifier) VerifyProviderWebhook(context.Context, *http.Request, ProviderWebhookVerificationInput) error {
	return nil
}

type fakeProviderHub struct {
	event  providerhubclient.WebhookEvent
	result providerhubclient.WebhookResult
	err    error
}

func (f *fakeProviderHub) IngestWebhookEvent(ctx context.Context, event providerhubclient.WebhookEvent) (providerhubclient.WebhookResult, error) {
	f.event = event
	if f.err != nil {
		return providerhubclient.WebhookResult{}, f.err
	}
	return f.result, nil
}
