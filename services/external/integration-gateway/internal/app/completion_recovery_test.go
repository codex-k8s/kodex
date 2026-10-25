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
	"google.golang.org/protobuf/proto"
)

// completionReceiptFixture models only the owner's immutable idempotency receipts, including a lost acknowledgement after recording one request. It does not emulate owner authorization, PostgreSQL or an external provider.
// completionReceiptFixture моделирует только неизменяемые квитанции идемпотентности владельца, включая потерю ответа после записи запроса. Она не имитирует авторизацию владельца, PostgreSQL или внешнего провайдера.
type completionReceiptFixture struct {
	cp.RuntimeWorkServiceClient
	receipts      map[string]*cp.CompleteIntegrationInvocationRequest
	requests      []*cp.CompleteIntegrationInvocationRequest
	dropNextReply bool
}

// CompleteIntegrationInvocation copies request bytes semantically before retaining them, rejects changed input under a recorded key and can lose one response after the receipt has been stored.
// CompleteIntegrationInvocation сохраняет независимую копию запроса, отклоняет изменённый ввод под записанным ключом и может потерять один ответ после сохранения квитанции.
func (fixture *completionReceiptFixture) CompleteIntegrationInvocation(_ context.Context, request *cp.CompleteIntegrationInvocationRequest, _ ...grpc.CallOption) (*cp.CompleteIntegrationInvocationResponse, error) {
	snapshot := proto.Clone(request).(*cp.CompleteIntegrationInvocationRequest)
	fixture.requests = append(fixture.requests, snapshot)
	key := snapshot.GetMutation().GetIdempotencyKey()
	if prior := fixture.receipts[key]; prior != nil {
		if !proto.Equal(prior, snapshot) {
			return nil, status.Error(codes.AlreadyExists, "completion key already has a different input")
		}
	} else {
		fixture.receipts[key] = snapshot
	}
	if fixture.dropNextReply {
		fixture.dropNextReply = false
		return nil, status.Error(codes.Unavailable, "completion acknowledgement was lost")
	}
	return &cp.CompleteIntegrationInvocationResponse{}, nil
}

// TestInvocationRecoveryCompletionKeepsIndependentReceipts reproduces UNKNOWN completion followed by a fresh recovery generation. Exact replay must reuse its receipt, changed input in the old generation must fail, and either terminal recovery result must be accepted without changing external-effect identity.
// TestInvocationRecoveryCompletionKeepsIndependentReceipts воспроизводит завершение UNKNOWN с последующим новым поколением восстановления. Точный повтор использует прежнюю квитанцию, изменение ввода старого поколения отклоняется, а оба конечных исхода восстановления принимаются без смены идентичности внешнего эффекта.
func TestInvocationRecoveryCompletionKeepsIndependentReceipts(t *testing.T) {
	for _, outcome := range []string{"confirmed_failure", "confirmed_success"} {
		t.Run(outcome, func(t *testing.T) {
			fixture := &completionReceiptFixture{receipts: make(map[string]*cp.CompleteIntegrationInvocationRequest), dropNextReply: true}
			client := &controlplaneclient.Client{Runtime: fixture}
			first := &cp.IntegrationInvocationClaim{InvocationRef: "inv_recovery_fixture", EffectKey: "effect-fixture", InputDigest: "input-fixture", Lease: &cp.WorkLease{Ref: "lease_first", Fence: "first-fence", Generation: 7}}
			unknown := &integration.UnknownOutcomeError{}
			if err := completeInvocation(t.Context(), client, first, integration.Result{}, unknown); status.Code(err) != codes.Unavailable || len(fixture.requests) != 1 || len(fixture.receipts) != 1 {
				t.Fatal("lost completion acknowledgement was hidden, retried or not retained")
			}
			if err := completeInvocation(t.Context(), client, first, integration.Result{}, unknown); err != nil || len(fixture.receipts) != 1 {
				t.Fatal("exact completion replay did not reuse the original receipt")
			}
			failure := &integration.SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
			if err := completeInvocation(t.Context(), client, first, integration.Result{}, failure); status.Code(err) != codes.AlreadyExists || len(fixture.receipts) != 1 {
				t.Fatal("changed outcome replaced the original generation receipt")
			}
			recovery := proto.Clone(first).(*cp.IntegrationInvocationClaim)
			recovery.Lease = &cp.WorkLease{Ref: "lease_recovery", Fence: "recovery-fence", Generation: 8}
			result := integration.Result{}
			var operationErr error = failure
			if outcome == "confirmed_success" {
				operationErr = nil
				result = integration.Result{Summary: `{"count":1}`, Receipt: integration.Receipt{EffectKey: first.EffectKey, InputDigest: first.InputDigest, ProviderEffectRef: "fixture-effect:1", ResponseDigest: "fixture-response-digest"}}
			}
			if err := completeInvocation(t.Context(), client, recovery, result, operationErr); err != nil {
				t.Fatalf("recovery completion collided with the previous generation: %v", err)
			}
			if err := completeInvocation(t.Context(), client, recovery, result, operationErr); err != nil || len(fixture.receipts) != 2 || len(fixture.requests) != 5 {
				t.Fatal("recovery replay changed receipt cardinality or retried internally")
			}
			original, terminal, replay := fixture.requests[0], fixture.requests[3], fixture.requests[4]
			if original.GetMutation().GetIdempotencyKey() == terminal.GetMutation().GetIdempotencyKey() || !proto.Equal(terminal, replay) || !original.UnknownOutcome || terminal.UnknownOutcome || terminal.InvocationRef != first.InvocationRef || terminal.Generation != 8 || terminal.LeaseRef != recovery.Lease.Ref || terminal.Fence != recovery.Lease.Fence {
				t.Fatal("completion receipts lost generation, immutable replay or invocation pins")
			}
			if outcome == "confirmed_success" {
				if !terminal.Success || terminal.GetEffectReceipt().GetEffectKey() != first.EffectKey || terminal.GetEffectReceipt().GetInputDigest() != first.InputDigest {
					t.Fatal("recovery changed the confirmed external effect identity")
				}
			} else if terminal.Success || terminal.EffectReceipt != nil || terminal.SafeErrorCode != "INTEGRATION_REQUEST_REJECTED" {
				t.Fatal("failed recovery fabricated an external effect receipt")
			}
		})
	}
}
