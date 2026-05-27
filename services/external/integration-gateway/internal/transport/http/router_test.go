package httptransport

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	interactionhubclient "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/clients/interactionhub"
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

func TestProviderWebhookUnsupportedProviderWithRequiredHeadersReturnsSourceNotAllowed(t *testing.T) {
	providerHub := &fakeProviderHub{}
	router := newTestRouterWithVerifier(t, enabledTestConfig(1024), providerHub, newGitHubVerifier(t, testWebhookSecret))

	req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/gitlab", strings.NewReader(`{"action":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Delivery", "delivery-unsupported-provider")
	req.Header.Set("X-GitHub-Event", "ping")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	body := expectSafeError(t, rec, http.StatusBadRequest)
	if body.Code != generated.SafeErrorCodeSourceNotAllowed || body.RequestId == "" || body.Retryable {
		t.Fatalf("SafeError = %+v, want source_not_allowed with request_id", body)
	}
	expectProviderHubCalls(t, providerHub, 0)
}

func TestProviderWebhookCallsProviderHubWhenEnabled(t *testing.T) {
	providerHub := &fakeProviderHub{result: providerhubclient.WebhookResult{WebhookEventID: "webhook-1"}}
	payload := `{"action":"ping"}`
	router := newTestRouterWithVerifier(t, Config{
		ServiceName:            "integration-gateway",
		OpenAPISpecPath:        "../../../../../../specs/openapi/integration-gateway.v1.yaml",
		RequestTimeout:         time.Second,
		MaxBodyBytes:           1024,
		ProviderWebhookEnabled: true,
		AllowedProviderSlugs:   []string{"github"},
	}, providerHub, newGitHubVerifier(t, testWebhookSecret))

	req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Delivery", "delivery-2")
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-Hub-Signature-256", githubSignature(testWebhookSecret, payload))
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
	if providerHub.event.RequestID == "" || providerHub.event.CorrelationID == "" {
		t.Fatalf("providerHub event lacks correlation metadata: %+v", providerHub.event)
	}
}

func TestProviderWebhookRepeatedDeliveryIsForwardedToProviderHub(t *testing.T) {
	providerHub := &fakeProviderHub{result: providerhubclient.WebhookResult{WebhookEventID: "webhook-1"}}
	router := newTestRouterWithVerifier(t, Config{
		ServiceName:                    "integration-gateway",
		OpenAPISpecPath:                "../../../../../../specs/openapi/integration-gateway.v1.yaml",
		RequestTimeout:                 time.Second,
		MaxBodyBytes:                   1024,
		ProviderWebhookEnabled:         true,
		AllowedProviderSlugs:           []string{"github"},
		ProviderWebhookRateLimitBurst:  10,
		ProviderWebhookRateLimitWindow: time.Minute,
	}, providerHub, newGitHubVerifier(t, testWebhookSecret))

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, signedGitHubWebhookRequest("delivery-repeat", `{"action":"ping"}`))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusAccepted, rec.Body.String())
		}
	}
	events := providerHub.eventsSnapshot()
	if len(events) != 2 {
		t.Fatalf("provider-hub calls = %d, want 2", len(events))
	}
	for _, event := range events {
		if event.ProviderSlug != "github" || event.DeliveryID != "delivery-repeat" {
			t.Fatalf("provider-hub event = %+v, want repeated github delivery", event)
		}
	}
}

func TestProviderWebhookRejectsInvalidGitHubSignature(t *testing.T) {
	providerHub := &fakeProviderHub{}
	payload := `{"action":"ping","secret":"do-not-leak"}`
	router := newTestRouterWithVerifier(t, Config{
		ServiceName:            "integration-gateway",
		OpenAPISpecPath:        "../../../../../../specs/openapi/integration-gateway.v1.yaml",
		RequestTimeout:         time.Second,
		MaxBodyBytes:           1024,
		ProviderWebhookEnabled: true,
		AllowedProviderSlugs:   []string{"github"},
	}, providerHub, newGitHubVerifier(t, testWebhookSecret))

	req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Delivery", "delivery-bad-signature")
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-Hub-Signature-256", githubSignature("wrong-secret", payload))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	var body generated.SafeError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode SafeError: %v", err)
	}
	if body.Code != generated.SafeErrorCodeSignatureInvalid || body.Retryable {
		t.Fatalf("SafeError = %+v, want signature_invalid non-retryable", body)
	}
	if strings.Contains(rec.Body.String(), "do-not-leak") || strings.Contains(rec.Body.String(), testWebhookSecret) || strings.Contains(rec.Body.String(), "wrong-secret") {
		t.Fatalf("SafeError leaked sensitive input: %s", rec.Body.String())
	}
	if providerHub.event.ProviderSlug != "" {
		t.Fatalf("provider hub was called: %+v", providerHub.event)
	}
}

func TestProviderWebhookRateLimitRejectsBeforeProviderHub(t *testing.T) {
	providerHub := &fakeProviderHub{result: providerhubclient.WebhookResult{WebhookEventID: "webhook-1"}}
	router := newTestRouterWithVerifier(t, rateLimitTestConfig(), providerHub, newGitHubVerifier(t, testWebhookSecret))

	first := httptest.NewRecorder()
	router.ServeHTTP(first, signedGitHubWebhookRequest("delivery-rate-1", `{"action":"ping"}`))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d, want %d, body = %s", first.Code, http.StatusAccepted, first.Body.String())
	}
	second := httptest.NewRecorder()
	router.ServeHTTP(second, signedGitHubWebhookRequest("delivery-rate-2", `{"action":"ping"}`))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d, body = %s", second.Code, http.StatusTooManyRequests, second.Body.String())
	}
	if second.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After = %q, want 1", second.Header().Get("Retry-After"))
	}
	var body generated.SafeError
	if err := json.Unmarshal(second.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode SafeError: %v", err)
	}
	if body.Code != generated.SafeErrorCodeRateLimited || !body.Retryable {
		t.Fatalf("SafeError = %+v, want retryable rate_limited", body)
	}
	if providerHub.eventCount() != 1 {
		t.Fatalf("provider-hub calls = %d, want 1", providerHub.eventCount())
	}
}

func TestProviderWebhookInvalidSignatureDoesNotConsumeRateLimit(t *testing.T) {
	providerHub := &fakeProviderHub{result: providerhubclient.WebhookResult{WebhookEventID: "webhook-1"}}
	router := newTestRouterWithVerifier(t, rateLimitTestConfig(), providerHub, newGitHubVerifier(t, testWebhookSecret))

	payload := `{"action":"ping"}`
	invalidReq := githubWebhookRequest("delivery-auth-before-limit-1", payload)
	invalidReq.Header.Set("X-Hub-Signature-256", githubSignature("wrong-secret", payload))
	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, invalidReq)
	if invalid.Code != http.StatusUnauthorized {
		t.Fatalf("invalid status = %d, want %d, body = %s", invalid.Code, http.StatusUnauthorized, invalid.Body.String())
	}

	valid := httptest.NewRecorder()
	router.ServeHTTP(valid, signedGitHubWebhookRequest("delivery-auth-before-limit-2", payload))
	if valid.Code != http.StatusAccepted {
		t.Fatalf("valid status = %d, want %d, body = %s", valid.Code, http.StatusAccepted, valid.Body.String())
	}
	if providerHub.eventCount() != 1 {
		t.Fatalf("provider-hub calls = %d, want 1", providerHub.eventCount())
	}
}

func TestProviderWebhookBackpressureRejectsBeforeProviderHub(t *testing.T) {
	release := make(chan struct{})
	providerHub := &fakeProviderHub{
		result:  providerhubclient.WebhookResult{WebhookEventID: "webhook-1"},
		block:   release,
		started: make(chan struct{}),
	}
	router := newTestRouterWithVerifier(t, Config{
		ServiceName:                    "integration-gateway",
		OpenAPISpecPath:                "../../../../../../specs/openapi/integration-gateway.v1.yaml",
		RequestTimeout:                 5 * time.Second,
		MaxBodyBytes:                   1024,
		ProviderWebhookEnabled:         true,
		AllowedProviderSlugs:           []string{"github"},
		ProviderWebhookMaxInFlight:     1,
		ProviderWebhookRateLimitBurst:  10,
		ProviderWebhookRateLimitWindow: time.Minute,
		ProviderWebhookRetryAfter:      time.Second,
	}, providerHub, newGitHubVerifier(t, testWebhookSecret))

	firstDone := make(chan int, 1)
	go func() {
		first := httptest.NewRecorder()
		router.ServeHTTP(first, signedGitHubWebhookRequest("delivery-pressure-1", `{"action":"ping"}`))
		firstDone <- first.Code
	}()
	select {
	case <-providerHub.started:
	case <-time.After(time.Second):
		t.Fatal("provider-hub call did not start")
	}

	second := httptest.NewRecorder()
	router.ServeHTTP(second, signedGitHubWebhookRequest("delivery-pressure-2", `{"action":"ping"}`))
	if second.Code != http.StatusServiceUnavailable {
		close(release)
		t.Fatalf("second status = %d, want %d, body = %s", second.Code, http.StatusServiceUnavailable, second.Body.String())
	}
	if second.Header().Get("Retry-After") != "1" {
		close(release)
		t.Fatalf("Retry-After = %q, want 1", second.Header().Get("Retry-After"))
	}
	var body generated.SafeError
	if err := json.Unmarshal(second.Body.Bytes(), &body); err != nil {
		close(release)
		t.Fatalf("decode SafeError: %v", err)
	}
	if body.Code != generated.SafeErrorCodeBackpressure || !body.Retryable {
		close(release)
		t.Fatalf("SafeError = %+v, want retryable backpressure", body)
	}
	if providerHub.eventCount() != 1 {
		close(release)
		t.Fatalf("provider-hub calls = %d, want 1", providerHub.eventCount())
	}
	close(release)
	if code := <-firstDone; code != http.StatusAccepted {
		t.Fatalf("first status = %d, want %d", code, http.StatusAccepted)
	}
}

func TestProviderWebhookRejectsMissingGitHubSignature(t *testing.T) {
	providerHub := &fakeProviderHub{}
	router := newTestRouterWithVerifier(t, Config{
		ServiceName:            "integration-gateway",
		OpenAPISpecPath:        "../../../../../../specs/openapi/integration-gateway.v1.yaml",
		RequestTimeout:         time.Second,
		MaxBodyBytes:           1024,
		ProviderWebhookEnabled: true,
		AllowedProviderSlugs:   []string{"github"},
	}, providerHub, newGitHubVerifier(t, testWebhookSecret))

	req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(`{"action":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Delivery", "delivery-missing-signature")
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

func TestProviderWebhookRejectsMissingGitHubEventHeader(t *testing.T) {
	providerHub := &fakeProviderHub{}
	router := newTestRouterWithVerifier(t, Config{
		ServiceName:            "integration-gateway",
		OpenAPISpecPath:        "../../../../../../specs/openapi/integration-gateway.v1.yaml",
		RequestTimeout:         time.Second,
		MaxBodyBytes:           1024,
		ProviderWebhookEnabled: true,
		AllowedProviderSlugs:   []string{"github"},
	}, providerHub, newGitHubVerifier(t, testWebhookSecret))

	req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(`{"action":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Delivery", "delivery-missing-event")
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

func TestProviderWebhookRejectsWhenVerifierMissing(t *testing.T) {
	providerHub := &fakeProviderHub{}
	router := newTestRouter(t, enabledTestConfig(1024), providerHub)

	req := githubWebhookRequest("delivery-verify", `{"action":"ping"}`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	body := expectSafeError(t, rec, http.StatusUnauthorized)
	if body.Code != generated.SafeErrorCodeSignatureInvalid {
		t.Fatalf("code = %s, want signature_invalid", body.Code)
	}
	expectProviderHubCalls(t, providerHub, 0)
}

func TestProviderWebhookRejectsUndeclaredExternalDeliveryFallback(t *testing.T) {
	providerHub := &fakeProviderHub{}
	router := newTestRouter(t, enabledTestConfig(1024), providerHub)

	req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(`{"action":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Kodex-External-Delivery", "external-delivery")
	req.Header.Set("X-GitHub-Event", "ping")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	body := expectSafeError(t, rec, http.StatusBadRequest)
	if body.Code != generated.SafeErrorCodeInvalidRequest {
		t.Fatalf("code = %s, want invalid_request", body.Code)
	}
	expectProviderHubCalls(t, providerHub, 0)
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
			payload := `{"action":"ping"}`
			router := newTestRouterWithVerifier(t, Config{
				ServiceName:            "integration-gateway",
				OpenAPISpecPath:        "../../../../../../specs/openapi/integration-gateway.v1.yaml",
				RequestTimeout:         time.Second,
				MaxBodyBytes:           1024,
				ProviderWebhookEnabled: true,
				AllowedProviderSlugs:   []string{"github"},
			}, providerHub, newGitHubVerifier(t, testWebhookSecret))

			req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-GitHub-Delivery", "delivery-error")
			req.Header.Set("X-GitHub-Event", "ping")
			req.Header.Set("X-Hub-Signature-256", githubSignature(testWebhookSecret, payload))
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

func TestProviderWebhookSafeValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		maxBody   int64
		delivery  string
		payload   string
		status    int
		errorCode generated.SafeErrorCode
	}{
		{
			name:      "payload too large",
			maxBody:   8,
			delivery:  "delivery-3",
			payload:   `{"too":"large"}`,
			status:    http.StatusRequestEntityTooLarge,
			errorCode: generated.SafeErrorCodePayloadTooLarge,
		},
		{
			name:      "malformed json",
			maxBody:   1024,
			delivery:  "delivery-4",
			payload:   `{"broken"`,
			status:    http.StatusBadRequest,
			errorCode: generated.SafeErrorCodeInvalidRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newTestRouter(t, enabledTestConfig(tt.maxBody), &fakeProviderHub{})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, githubWebhookRequest(tt.delivery, tt.payload))

			body := expectSafeError(t, rec, tt.status)
			if body.Code != tt.errorCode {
				t.Fatalf("code = %s, want %s", body.Code, tt.errorCode)
			}
		})
	}
}

func TestOpenAPIValidationRejectsUnsupportedContentType(t *testing.T) {
	router := newTestRouter(t, Config{
		ServiceName:            "integration-gateway",
		OpenAPISpecPath:        "../../../../../../specs/openapi/integration-gateway.v1.yaml",
		RequestTimeout:         time.Second,
		MaxBodyBytes:           1024,
		ProviderWebhookEnabled: true,
		AllowedProviderSlugs:   []string{"github"},
	}, &fakeProviderHub{})

	req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(`{"action":"ping"}`))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-GitHub-Delivery", "delivery-content-type")
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
	if body.Code != generated.SafeErrorCodeInvalidRequest || body.RequestId == "" || body.CorrelationId == nil {
		t.Fatalf("SafeError = %+v, want OpenAPI-compatible invalid_request", body)
	}
}

func TestExternalCallbackRouteDisabledReturnsSafeError(t *testing.T) {
	interactionHub := &fakeInteractionHub{}
	router := newTestRouterWithClientsAndVerifiers(t, Config{
		ServiceName:             "integration-gateway",
		OpenAPISpecPath:         "../../../../../../specs/openapi/integration-gateway.v1.yaml",
		RequestTimeout:          time.Second,
		MaxBodyBytes:            1024,
		ExternalCallbackEnabled: false,
		AllowedCallbackSources:  []string{"channel-package"},
		ProviderWebhookEnabled:  false,
		AllowedProviderSlugs:    []string{"github"},
	}, &fakeProviderHub{}, interactionHub, rejectingProviderWebhookVerifier{}, rejectingExternalCallbackVerifier{})

	req := externalCallbackRequest("channel-package", validExternalCallbackPayload("callback-disabled", "delivery-disabled"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	body := expectSafeError(t, rec, http.StatusBadRequest)
	if body.Code != generated.SafeErrorCodeSourceNotAllowed || body.RequestId == "" || body.Retryable {
		t.Fatalf("SafeError = %+v, want source_not_allowed with request_id", body)
	}
	expectInteractionHubCalls(t, interactionHub, 0)
}

func TestExternalCallbackCallsInteractionHubWhenEnabled(t *testing.T) {
	interactionHub := &fakeInteractionHub{result: interactionhubclient.CallbackResult{CallbackID: "callback-accepted"}}
	payload := validExternalCallbackPayload("callback-1", "delivery-1")
	router := newTestRouterWithClientsAndVerifiers(t, enabledExternalCallbackTestConfig(1024), &fakeProviderHub{}, interactionHub, rejectingProviderWebhookVerifier{}, newExternalCallbackVerifier(t, testExternalCallbackSecret))

	req := externalCallbackRequest("channel-package", payload)
	req.Header.Set("X-Kodex-External-Delivery", "callback-1")
	req.Header.Set("X-Kodex-External-Signature", githubSignature(testExternalCallbackSecret, payload))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if interactionHub.callback.CallbackSource != "channel-package" ||
		interactionHub.callback.CallbackID != "callback-1" ||
		interactionHub.callback.DeliveryID != "delivery-1" ||
		interactionHub.callback.Action != "approve" {
		t.Fatalf("interactionHub callback = %+v", interactionHub.callback)
	}
	if interactionHub.callback.ContractVersion != "interaction.channel.v1" ||
		interactionHub.callback.ActorRef != "external_account:actor-1" ||
		interactionHub.callback.AnswerSummary != "approved" {
		t.Fatalf("interactionHub callback safe fields = %+v", interactionHub.callback)
	}
	if interactionHub.callback.RequestID == "" ||
		interactionHub.callback.CorrelationID != "correlation-1" ||
		!strings.Contains(interactionHub.callback.GatewayRef, "integration-gateway/channel-package/") {
		t.Fatalf("interactionHub callback lacks correlation metadata: %+v", interactionHub.callback)
	}
	var body generated.ExternalCallbackAccepted
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode ExternalCallbackAccepted: %v", err)
	}
	if body.CallbackId != "callback-accepted" || body.CallbackSource != "channel-package" || body.OwnerService == nil || *body.OwnerService != "interaction-hub" {
		t.Fatalf("ExternalCallbackAccepted = %+v", body)
	}
}

func TestExternalCallbackRejectsInvalidSignature(t *testing.T) {
	interactionHub := &fakeInteractionHub{}
	payload := `{"contract_version":"interaction.channel.v1","callback_id":"callback-bad-signature","delivery_id":"delivery-bad-signature","action":"approve","answer_summary":"secret=do-not-leak"}`
	router := newTestRouterWithClientsAndVerifiers(t, enabledExternalCallbackTestConfig(1024), &fakeProviderHub{}, interactionHub, rejectingProviderWebhookVerifier{}, newExternalCallbackVerifier(t, testExternalCallbackSecret))

	req := externalCallbackRequest("channel-package", payload)
	req.Header.Set("X-Kodex-External-Signature", githubSignature("wrong-external-secret", payload))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	body := expectSafeError(t, rec, http.StatusUnauthorized)
	if body.Code != generated.SafeErrorCodeSignatureInvalid || body.Retryable {
		t.Fatalf("SafeError = %+v, want signature_invalid non-retryable", body)
	}
	for _, forbidden := range []string{"do-not-leak", testExternalCallbackSecret, "wrong-external-secret", "sha256="} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("SafeError leaked sensitive input: %s", rec.Body.String())
		}
	}
	expectInteractionHubCalls(t, interactionHub, 0)
}

func TestExternalCallbackSafeValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		maxBody   int64
		payload   string
		status    int
		errorCode generated.SafeErrorCode
	}{
		{
			name:      "payload too large",
			maxBody:   8,
			payload:   validExternalCallbackPayload("callback-large", "delivery-large"),
			status:    http.StatusRequestEntityTooLarge,
			errorCode: generated.SafeErrorCodePayloadTooLarge,
		},
		{
			name:      "missing required body field",
			maxBody:   1024,
			payload:   `{"contract_version":"interaction.channel.v1","callback_id":"callback-missing-action","delivery_id":"delivery-missing-action"}`,
			status:    http.StatusBadRequest,
			errorCode: generated.SafeErrorCodeInvalidRequest,
		},
		{
			name:      "missing delivery and request ref",
			maxBody:   1024,
			payload:   `{"contract_version":"interaction.channel.v1","callback_id":"callback-missing-link","action":"approve"}`,
			status:    http.StatusBadRequest,
			errorCode: generated.SafeErrorCodeInvalidRequest,
		},
		{
			name:      "header idempotency mismatch",
			maxBody:   1024,
			payload:   validExternalCallbackPayload("callback-header", "delivery-header"),
			status:    http.StatusBadRequest,
			errorCode: generated.SafeErrorCodeInvalidRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interactionHub := &fakeInteractionHub{}
			router := newTestRouterWithClientsAndVerifiers(t, enabledExternalCallbackTestConfig(tt.maxBody), &fakeProviderHub{}, interactionHub, rejectingProviderWebhookVerifier{}, newExternalCallbackVerifier(t, testExternalCallbackSecret))
			req := externalCallbackRequest("channel-package", tt.payload)
			req.Header.Set("X-Kodex-External-Signature", githubSignature(testExternalCallbackSecret, tt.payload))
			if tt.name == "header idempotency mismatch" {
				req.Header.Set("X-Kodex-External-Delivery", "different-callback")
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			body := expectSafeError(t, rec, tt.status)
			if body.Code != tt.errorCode {
				t.Fatalf("code = %s, want %s", body.Code, tt.errorCode)
			}
			expectInteractionHubCalls(t, interactionHub, 0)
		})
	}
}

func TestExternalCallbackRateLimitRejectsBeforeInteractionHub(t *testing.T) {
	interactionHub := &fakeInteractionHub{result: interactionhubclient.CallbackResult{CallbackID: "callback-rate-1"}}
	router := newTestRouterWithClientsAndVerifiers(t, rateLimitExternalCallbackTestConfig(), &fakeProviderHub{}, interactionHub, rejectingProviderWebhookVerifier{}, newExternalCallbackVerifier(t, testExternalCallbackSecret))

	firstPayload := validExternalCallbackPayload("callback-rate-1", "delivery-rate-1")
	first := httptest.NewRecorder()
	router.ServeHTTP(first, signedExternalCallbackRequest("channel-package", firstPayload))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d, want %d, body = %s", first.Code, http.StatusAccepted, first.Body.String())
	}
	secondPayload := validExternalCallbackPayload("callback-rate-2", "delivery-rate-2")
	second := httptest.NewRecorder()
	router.ServeHTTP(second, signedExternalCallbackRequest("channel-package", secondPayload))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d, body = %s", second.Code, http.StatusTooManyRequests, second.Body.String())
	}
	body := expectSafeError(t, second, http.StatusTooManyRequests)
	if body.Code != generated.SafeErrorCodeRateLimited || !body.Retryable {
		t.Fatalf("SafeError = %+v, want retryable rate_limited", body)
	}
	expectInteractionHubCalls(t, interactionHub, 1)
}

func TestExternalCallbackInteractionHubFailureReturnsSafeError(t *testing.T) {
	interactionHub := &fakeInteractionHub{err: status.Error(codes.Unavailable, "internal interaction-hub detail")}
	payload := validExternalCallbackPayload("callback-downstream", "delivery-downstream")
	router := newTestRouterWithClientsAndVerifiers(t, enabledExternalCallbackTestConfig(1024), &fakeProviderHub{}, interactionHub, rejectingProviderWebhookVerifier{}, newExternalCallbackVerifier(t, testExternalCallbackSecret))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, signedExternalCallbackRequest("channel-package", payload))

	body := expectSafeError(t, rec, http.StatusServiceUnavailable)
	if body.Code != generated.SafeErrorCodeDownstreamUnavailable || !body.Retryable {
		t.Fatalf("SafeError = %+v, want retryable downstream_unavailable", body)
	}
	if strings.Contains(body.Message, "internal interaction-hub detail") {
		t.Fatalf("SafeError leaked downstream details: %+v", body)
	}
	expectInteractionHubCalls(t, interactionHub, 1)
}

func TestProviderWebhookSafeAuditSummaryRedactsSensitiveInput(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	providerHub := &fakeProviderHub{}
	router, err := NewRouterWithVerifier(context.Background(), Config{
		ServiceName:            "integration-gateway",
		OpenAPISpecPath:        "../../../../../../specs/openapi/integration-gateway.v1.yaml",
		RequestTimeout:         time.Second,
		MaxBodyBytes:           1024,
		ProviderWebhookEnabled: true,
		AllowedProviderSlugs:   []string{"github"},
	}, providerHub, newGitHubVerifier(t, testWebhookSecret), logger)
	if err != nil {
		t.Fatalf("NewRouterWithVerifier() error = %v", err)
	}

	payload := `{"action":"ping","secret":"do-not-leak"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Delivery", "delivery-audit")
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-Hub-Signature-256", githubSignature("wrong-secret", payload))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	logText := logs.String()
	for _, want := range []string{
		"route_id=provider_webhook",
		"source=github",
		"status=401",
		"payload_size_bucket=1-1KiB",
		"reject_reason=signature_invalid",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs = %q, want %q", logText, want)
		}
	}
	for _, forbidden := range []string{"do-not-leak", testWebhookSecret, "wrong-secret", "sha256="} {
		if strings.Contains(logText, forbidden) {
			t.Fatalf("logs leaked sensitive input: %s", logText)
		}
	}
	if providerHub.eventCount() != 0 {
		t.Fatalf("provider-hub calls = %d, want 0", providerHub.eventCount())
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

func newTestRouterWithClientsAndVerifiers(
	t *testing.T,
	cfg Config,
	providerHub ProviderHubClient,
	interactionHub InteractionHubClient,
	providerVerifier ProviderWebhookVerifier,
	externalVerifier ExternalCallbackVerifier,
) *Router {
	t.Helper()
	router, err := NewRouterWithClientsAndVerifiers(context.Background(), cfg, providerHub, interactionHub, providerVerifier, externalVerifier, nil)
	if err != nil {
		t.Fatalf("NewRouterWithClientsAndVerifiers() error = %v", err)
	}
	return router
}

const testWebhookSecret = "github-webhook-secret"
const testExternalCallbackSecret = "external-callback-secret"

func newGitHubVerifier(t *testing.T, secret string) ProviderWebhookVerifier {
	t.Helper()
	t.Setenv("KODEX_TEST_GITHUB_WEBHOOK_SECRET", secret)
	resolver, err := secretresolver.NewMux(map[string]secretresolver.Backend{
		secretresolver.StoreTypeEnv: secretresolver.NewEnvBackend(),
	})
	if err != nil {
		t.Fatalf("NewMux() error = %v", err)
	}
	return NewGitHubProviderWebhookVerifier(resolver, secretresolver.SecretRef{
		StoreType: secretresolver.StoreTypeEnv,
		StoreRef:  "KODEX_TEST_GITHUB_WEBHOOK_SECRET",
	})
}

func newExternalCallbackVerifier(t *testing.T, secret string) ExternalCallbackVerifier {
	t.Helper()
	t.Setenv("KODEX_TEST_EXTERNAL_CALLBACK_SECRET", secret)
	resolver, err := secretresolver.NewMux(map[string]secretresolver.Backend{
		secretresolver.StoreTypeEnv: secretresolver.NewEnvBackend(),
	})
	if err != nil {
		t.Fatalf("NewMux() error = %v", err)
	}
	return NewExternalCallbackHMACVerifier(resolver, secretresolver.SecretRef{
		StoreType: secretresolver.StoreTypeEnv,
		StoreRef:  "KODEX_TEST_EXTERNAL_CALLBACK_SECRET",
	})
}

func githubSignature(secret string, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func signedGitHubWebhookRequest(deliveryID string, payload string) *http.Request {
	req := githubWebhookRequest(deliveryID, payload)
	req.Header.Set("X-Hub-Signature-256", githubSignature(testWebhookSecret, payload))
	return req
}

func githubWebhookRequest(deliveryID string, payload string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Delivery", deliveryID)
	req.Header.Set("X-GitHub-Event", "ping")
	return req
}

func signedExternalCallbackRequest(callbackSource string, payload string) *http.Request {
	req := externalCallbackRequest(callbackSource, payload)
	req.Header.Set("X-Kodex-External-Signature", githubSignature(testExternalCallbackSecret, payload))
	return req
}

func externalCallbackRequest(callbackSource string, payload string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/external-callbacks/"+callbackSource, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func validExternalCallbackPayload(callbackID string, deliveryID string) string {
	return `{"contract_version":"interaction.channel.v1","callback_id":"` + callbackID + `","delivery_id":"` + deliveryID + `","actor_ref":"external_account:actor-1","action":"approve","answer_summary":"approved","correlation_id":"correlation-1"}`
}

func enabledTestConfig(maxBodyBytes int64) Config {
	return Config{
		ServiceName:            "integration-gateway",
		OpenAPISpecPath:        "../../../../../../specs/openapi/integration-gateway.v1.yaml",
		RequestTimeout:         time.Second,
		MaxBodyBytes:           maxBodyBytes,
		ProviderWebhookEnabled: true,
		AllowedProviderSlugs:   []string{"github"},
	}
}

func enabledExternalCallbackTestConfig(maxBodyBytes int64) Config {
	return Config{
		ServiceName:                     "integration-gateway",
		OpenAPISpecPath:                 "../../../../../../specs/openapi/integration-gateway.v1.yaml",
		RequestTimeout:                  time.Second,
		MaxBodyBytes:                    maxBodyBytes,
		ProviderWebhookEnabled:          false,
		AllowedProviderSlugs:            []string{"github"},
		ExternalCallbackEnabled:         true,
		AllowedCallbackSources:          []string{"channel-package"},
		ExternalCallbackMaxInFlight:     32,
		ExternalCallbackRateLimitBurst:  120,
		ExternalCallbackRateLimitWindow: time.Second,
		ExternalCallbackRetryAfter:      time.Second,
	}
}

func rateLimitTestConfig() Config {
	cfg := enabledTestConfig(1024)
	cfg.ProviderWebhookMaxInFlight = 10
	cfg.ProviderWebhookRateLimitBurst = 1
	cfg.ProviderWebhookRateLimitWindow = time.Minute
	cfg.ProviderWebhookRetryAfter = time.Second
	return cfg
}

func rateLimitExternalCallbackTestConfig() Config {
	cfg := enabledExternalCallbackTestConfig(1024)
	cfg.ExternalCallbackMaxInFlight = 10
	cfg.ExternalCallbackRateLimitBurst = 1
	cfg.ExternalCallbackRateLimitWindow = time.Minute
	cfg.ExternalCallbackRetryAfter = time.Second
	return cfg
}

func expectSafeError(t *testing.T, rec *httptest.ResponseRecorder, status int) generated.SafeError {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, status, rec.Body.String())
	}
	var body generated.SafeError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode SafeError: %v", err)
	}
	return body
}

func expectProviderHubCalls(t *testing.T, providerHub *fakeProviderHub, want int) {
	t.Helper()
	if providerHub.eventCount() != want {
		t.Fatalf("provider-hub calls = %d, want %d", providerHub.eventCount(), want)
	}
}

func expectInteractionHubCalls(t *testing.T, interactionHub *fakeInteractionHub, want int) {
	t.Helper()
	if interactionHub.callbackCount() != want {
		t.Fatalf("interaction-hub calls = %d, want %d", interactionHub.callbackCount(), want)
	}
}

type fakeProviderHub struct {
	mu      sync.Mutex
	started chan struct{}
	block   <-chan struct{}
	once    sync.Once
	event   providerhubclient.WebhookEvent
	events  []providerhubclient.WebhookEvent
	result  providerhubclient.WebhookResult
	err     error
}

func (f *fakeProviderHub) IngestWebhookEvent(ctx context.Context, event providerhubclient.WebhookEvent) (providerhubclient.WebhookResult, error) {
	f.mu.Lock()
	f.event = event
	f.events = append(f.events, event)
	f.mu.Unlock()
	if f.started != nil {
		f.once.Do(func() {
			close(f.started)
		})
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return providerhubclient.WebhookResult{}, ctx.Err()
		}
	}
	if f.err != nil {
		return providerhubclient.WebhookResult{}, f.err
	}
	return f.result, nil
}

func (f *fakeProviderHub) eventCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

func (f *fakeProviderHub) eventsSnapshot() []providerhubclient.WebhookEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]providerhubclient.WebhookEvent(nil), f.events...)
}

type fakeInteractionHub struct {
	mu        sync.Mutex
	started   chan struct{}
	block     <-chan struct{}
	once      sync.Once
	callback  interactionhubclient.CallbackEnvelope
	callbacks []interactionhubclient.CallbackEnvelope
	result    interactionhubclient.CallbackResult
	err       error
}

func (f *fakeInteractionHub) RecordChannelCallback(ctx context.Context, callback interactionhubclient.CallbackEnvelope) (interactionhubclient.CallbackResult, error) {
	f.mu.Lock()
	f.callback = callback
	f.callbacks = append(f.callbacks, callback)
	f.mu.Unlock()
	if f.started != nil {
		f.once.Do(func() {
			close(f.started)
		})
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return interactionhubclient.CallbackResult{}, ctx.Err()
		}
	}
	if f.err != nil {
		return interactionhubclient.CallbackResult{}, f.err
	}
	return f.result, nil
}

func (f *fakeInteractionHub) callbackCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.callbacks)
}
