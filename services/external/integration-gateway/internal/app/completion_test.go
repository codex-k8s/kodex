package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestInvocationFailureLogIsClosedAndSingle checks the worker boundary against wrapped untrusted diagnostics and distinguishes local rejection from an observed provider response.
// TestInvocationFailureLogIsClosedAndSingle проверяет границу worker с обёрнутой недоверенной диагностикой и отличает локальный отказ от наблюдённого HTTP-ответа.
func TestInvocationFailureLogIsClosedAndSingle(t *testing.T) {
	for _, fixture := range []struct {
		name, class, level string
		err                error
		status             int
	}{
		{name: "success"},
		{name: "local rejection", class: "INTEGRATION_REQUEST_REJECTED", level: "INFO", err: &integration.SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}},
		{name: "provider rejection", class: "INTEGRATION_REQUEST_REJECTED", level: "INFO", err: fmt.Errorf("synthetic-private-detail: %w", &integration.SafeError{Code: "INTEGRATION_REQUEST_REJECTED", ProviderHTTPStatus: 422}), status: 422},
		{name: "authentication", class: "INTEGRATION_AUTH_REJECTED", level: "INFO", err: &integration.SafeError{Code: "INTEGRATION_AUTH_REJECTED", ProviderHTTPStatus: 401}, status: 401},
		{name: "transport", class: "INTEGRATION_UNAVAILABLE", level: "WARN", err: errors.New("synthetic-private-detail")},
		{name: "unknown", class: "integration_outcome_unknown", level: "WARN", err: &integration.UnknownOutcomeError{}},
		{name: "unclassified safe code", class: "INTEGRATION_UNAVAILABLE", level: "WARN", err: &integration.SafeError{Code: "synthetic-private-detail"}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))
			logInvocationOutcome(t.Context(), logger, fixture.err)
			if fixture.err == nil {
				if output.Len() != 0 {
					t.Fatal("success emitted a failure")
				}
				return
			}
			var event map[string]any
			if strings.Count(output.String(), "\n") != 1 || json.Unmarshal(output.Bytes(), &event) != nil || strings.Contains(output.String(), "synthetic-private-detail") {
				t.Fatal("failure diagnostics leaked or were duplicated")
			}
			if event["error_class"] != fixture.class || event["level"] != fixture.level {
				t.Fatal("closed classification or severity changed")
			}
			if fixture.name == "unknown" {
				if event["stage"] != "unclassified" || event["provider_http_status"] != nil {
					t.Fatal("unknown outcome changed its existing diagnostic")
				}
			} else if event["provider_http_status"] != float64(fixture.status) {
				t.Fatal("observed status was lost or invented")
			}
		})
	}
}

// completionFixture captures invocation completion requests and injects an owner RPC failure without a live transport or mutation.
// RU: completionFixture сохраняет запросы завершения вызова и подставляет ошибку owner RPC без живого транспорта и изменения состояния.
type completionFixture struct {
	cp.RuntimeWorkServiceClient
	requests []*cp.CompleteIntegrationInvocationRequest
	err      error
}

// CompleteIntegrationInvocation records each request before returning the configured error, exposing hidden retries to the caller's assertions.
// RU: CompleteIntegrationInvocation записывает каждый запрос до возврата заданной ошибки, позволяя проверке обнаружить скрытые повторы.
func (f *completionFixture) CompleteIntegrationInvocation(_ context.Context, r *cp.CompleteIntegrationInvocationRequest, _ ...grpc.CallOption) (*cp.CompleteIntegrationInvocationResponse, error) {
	f.requests = append(f.requests, r)
	return &cp.CompleteIntegrationInvocationResponse{}, f.err
}

// TestEmailCompletionPreservesFenceAndUnknown covers success, unknown outcome, denial and stale-fence RPC failure without fabricating receipts or retrying.
// RU: TestEmailCompletionPreservesFenceAndUnknown проверяет успех, неопределённый результат, отказ и RPC-ошибку fence без выдуманных квитанций и повторов.
// Every report must preserve invocation, lease, generation and idempotency pins; only classified success may carry the exact effect receipt.
// RU: Каждый отчёт обязан сохранять привязки invocation, lease, generation и идемпотентности; точная квитанция эффекта допустима только для классифицированного успеха.
func TestEmailCompletionPreservesFenceAndUnknown(t *testing.T) {
	for _, outcome := range []string{"success", "unknown", "denied", "pre-effect-response", "pre-effect-timeout", "stale-fence"} {
		t.Run(outcome, func(t *testing.T) {
			fixture := &completionFixture{}
			client := &controlplaneclient.Client{Runtime: fixture}
			claim := &cp.IntegrationInvocationClaim{InvocationRef: "inv_email_fixture", Lease: &cp.WorkLease{Ref: "lease_fixture", Fence: "fixture-fence", Generation: 7}}
			result := integration.Result{Summary: `{"message_id":"receipt","status":"accepted"}`, Receipt: integration.Receipt{EffectKey: "effect-fixture", InputDigest: "input-fixture", ProviderEffectRef: "email-message:receipt", ResponseDigest: "response-fixture"}}
			var operationErr error
			switch outcome {
			case "unknown":
				operationErr = &integration.UnknownOutcomeError{}
			case "denied":
				operationErr = &integration.SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
			case "pre-effect-response":
				operationErr = &integration.SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			case "pre-effect-timeout":
				operationErr = &integration.SafeError{Code: "INTEGRATION_UNAVAILABLE"}
			case "stale-fence":
				fixture.err = status.Error(codes.PermissionDenied, "stale fence")
			}
			err := completeInvocation(t.Context(), client, claim, result, operationErr)
			if (err != nil) != (fixture.err != nil) || len(fixture.requests) != 1 {
				t.Fatal("completion error hidden or retried")
			}
			r := fixture.requests[0]
			if r.InvocationRef != claim.InvocationRef || r.LeaseRef != claim.Lease.Ref || r.Fence != claim.Lease.Fence || r.Generation != 7 || r.Mutation.IdempotencyKey != stableKey(claim.InvocationRef, "complete:7") {
				t.Fatal("completion changed claim identity")
			}
			if r.UnknownOutcome != (outcome == "unknown") || r.Success != (operationErr == nil) || (r.EffectReceipt != nil) != r.Success {
				t.Fatal("completion changed unknown state or fabricated receipt")
			}
			if r.Success && (r.EffectReceipt.EffectKey != result.Receipt.EffectKey || r.EffectReceipt.ProviderEffectRef != result.Receipt.ProviderEffectRef) {
				t.Fatal("completion lost exact effect receipt")
			}
		})
	}
}
