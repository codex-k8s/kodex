package app

import (
	"context"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
	for _, outcome := range []string{"success", "unknown", "denied", "stale-fence"} {
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
			case "stale-fence":
				fixture.err = status.Error(codes.PermissionDenied, "stale fence")
			}
			err := completeInvocation(t.Context(), client, claim, result, operationErr)
			if (err != nil) != (fixture.err != nil) || len(fixture.requests) != 1 {
				t.Fatal("completion error hidden or retried")
			}
			r := fixture.requests[0]
			if r.InvocationRef != claim.InvocationRef || r.LeaseRef != claim.Lease.Ref || r.Fence != claim.Lease.Fence || r.Generation != 7 || r.Mutation.IdempotencyKey != stableKey(claim.InvocationRef, "complete") {
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
