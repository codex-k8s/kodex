package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type invocationControl struct {
	controlplanev1.RuntimeWorkServiceClient
	claimTests   func(context.Context, *controlplanev1.ClaimIntegrationConnectionTestsRequest) (*controlplanev1.ClaimIntegrationConnectionTestsResponse, error)
	claimCalls   func(context.Context, *controlplanev1.ClaimIntegrationInvocationsRequest) (*controlplanev1.ClaimIntegrationInvocationsResponse, error)
	completeTest func(context.Context, *controlplanev1.CompleteIntegrationConnectionTestRequest) (*controlplanev1.CompleteIntegrationConnectionTestResponse, error)
	completeCall func(context.Context, *controlplanev1.CompleteIntegrationInvocationRequest) (*controlplanev1.CompleteIntegrationInvocationResponse, error)
}

func (control invocationControl) ClaimIntegrationConnectionTests(ctx context.Context, request *controlplanev1.ClaimIntegrationConnectionTestsRequest, _ ...grpc.CallOption) (*controlplanev1.ClaimIntegrationConnectionTestsResponse, error) {
	return control.claimTests(ctx, request)
}
func (control invocationControl) ClaimIntegrationInvocations(ctx context.Context, request *controlplanev1.ClaimIntegrationInvocationsRequest, _ ...grpc.CallOption) (*controlplanev1.ClaimIntegrationInvocationsResponse, error) {
	return control.claimCalls(ctx, request)
}
func (control invocationControl) CompleteIntegrationConnectionTest(ctx context.Context, request *controlplanev1.CompleteIntegrationConnectionTestRequest, _ ...grpc.CallOption) (*controlplanev1.CompleteIntegrationConnectionTestResponse, error) {
	return control.completeTest(ctx, request)
}
func (control invocationControl) CompleteIntegrationInvocation(ctx context.Context, request *controlplanev1.CompleteIntegrationInvocationRequest, _ ...grpc.CallOption) (*controlplanev1.CompleteIntegrationInvocationResponse, error) {
	return control.completeCall(ctx, request)
}

type invocationAdapter struct {
	execute func(context.Context, *controlplanev1.IntegrationInvocationClaim) (*controlplanev1.IntegrationEffectReceipt, error)
	test    func(context.Context, *controlplanev1.IntegrationConnectionTestClaim) (string, error)
}

func (adapter invocationAdapter) Execute(ctx context.Context, claim *controlplanev1.IntegrationInvocationClaim) (*controlplanev1.IntegrationEffectReceipt, error) {
	return adapter.execute(ctx, claim)
}
func (adapter invocationAdapter) TestConnection(ctx context.Context, claim *controlplanev1.IntegrationConnectionTestClaim) (string, error) {
	return adapter.test(ctx, claim)
}

func invocationClaim() *controlplanev1.IntegrationInvocationClaim {
	return &controlplanev1.IntegrationInvocationClaim{InvocationRef: "invocation", DefinitionKey: "mattermost", EffectKey: "effect", InputDigest: "input-digest", Risk: controlplanev1.IntegrationRisk_INTEGRATION_RISK_WRITE, Lease: deliveryClaim().GetLease()}
}

func invocationReceipt(claim *controlplanev1.IntegrationInvocationClaim) *controlplanev1.IntegrationEffectReceipt {
	const summary = `{"post_id":"post"}`
	digest := sha256.Sum256([]byte(summary))
	return &controlplanev1.IntegrationEffectReceipt{EffectKey: claim.GetEffectKey(), InputDigest: claim.GetInputDigest(), ProviderEffectRef: "post", ResponseDigest: hex.EncodeToString(digest[:]), ResultSummary: summary}
}

func TestInvocationCompletesExactLeaseBeforeClaimingNext(t *testing.T) {
	claimed, completed := 0, 0
	keys := map[string]bool{}
	control := invocationControl{
		claimTests: func(ctx context.Context, request *controlplanev1.ClaimIntegrationConnectionTestsRequest) (*controlplanev1.ClaimIntegrationConnectionTestsResponse, error) {
			if _, ok := ctx.Deadline(); !ok || request.GetLimit() != 1 {
				t.Fatal("unbounded connection test claim")
			}
			return &controlplanev1.ClaimIntegrationConnectionTestsResponse{}, nil
		},
		claimCalls: func(ctx context.Context, request *controlplanev1.ClaimIntegrationInvocationsRequest) (*controlplanev1.ClaimIntegrationInvocationsResponse, error) {
			if _, ok := ctx.Deadline(); !ok || request.GetLimit() != 1 || claimed != completed || request.GetWorkloadInstance() != "instance" {
				t.Fatal("invocation lease queued or unbounded")
			}
			claimed++
			claim := invocationClaim()
			claim.Lease.Generation = int64(claimed)
			return &controlplanev1.ClaimIntegrationInvocationsResponse{Claims: []*controlplanev1.IntegrationInvocationClaim{claim}}, nil
		},
		completeCall: func(ctx context.Context, request *controlplanev1.CompleteIntegrationInvocationRequest) (*controlplanev1.CompleteIntegrationInvocationResponse, error) {
			if _, ok := ctx.Deadline(); !ok || ctx.Err() != nil {
				t.Fatal("completion context is cancelled or unbounded")
			}
			if !request.GetSuccess() || request.GetUnknownOutcome() || request.GetLeaseRef() != "lease" || request.GetFence() != "fixture-fence" || request.GetGeneration() != int64(claimed) || request.GetInvocationRef() != "invocation" || request.GetEffectReceipt().GetEffectKey() != "effect" {
				t.Fatal("completion lost invocation identity")
			}
			key := request.GetMutation().GetIdempotencyKey()
			if key == "" || keys[key] {
				t.Fatal("completion key reused across attempts")
			}
			keys[key] = true
			completed++
			return &controlplanev1.CompleteIntegrationInvocationResponse{Run: &controlplanev1.Run{Ref: "run"}}, nil
		},
	}
	adapter := invocationAdapter{execute: func(ctx context.Context, claim *controlplanev1.IntegrationInvocationClaim) (*controlplanev1.IntegrationEffectReceipt, error) {
		deadline, ok := ctx.Deadline()
		if !ok || !deadline.Before(claim.GetLease().GetExpiresAt().AsTime().Add(-time.Second)) {
			t.Fatal("external effect has no completion reserve")
		}
		return invocationReceipt(claim), nil
	}}
	if err := processIntegrationWork(t.Context(), control, adapter, Config{InstanceID: "instance", ClaimLimit: 2, RequestTimeout: time.Second}); err != nil {
		t.Fatal(err)
	}
	if completed != 2 {
		t.Fatalf("completed=%d", completed)
	}
}

func TestInvocationRejectsExpiredLeaseAndForeignOwnerBeforeDispatch(t *testing.T) {
	for _, mutate := range []func(*controlplanev1.IntegrationInvocationClaim){
		func(claim *controlplanev1.IntegrationInvocationClaim) { claim.DefinitionKey = "github" },
		func(claim *controlplanev1.IntegrationInvocationClaim) { claim.Lease = nil },
		func(claim *controlplanev1.IntegrationInvocationClaim) {
			claim.Lease.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
		},
		func(claim *controlplanev1.IntegrationInvocationClaim) { claim.Lease.Generation = 0 },
		func(claim *controlplanev1.IntegrationInvocationClaim) { claim.Lease.Fence = "" },
	} {
		claim := invocationClaim()
		mutate(claim)
		if err := processInvocation(t.Context(), invocationControl{}, invocationAdapter{}, claim, Config{RequestTimeout: time.Second}); err == nil {
			t.Fatal("invalid claim dispatched")
		}
	}
}

func TestInvocationInvalidEffectReceiptCannotBecomeSuccess(t *testing.T) {
	for _, risk := range []controlplanev1.IntegrationRisk{controlplanev1.IntegrationRisk_INTEGRATION_RISK_READ, controlplanev1.IntegrationRisk_INTEGRATION_RISK_WRITE} {
		for _, mutate := range []func(*controlplanev1.IntegrationEffectReceipt){
			func(receipt *controlplanev1.IntegrationEffectReceipt) { receipt.EffectKey = "other" },
			func(receipt *controlplanev1.IntegrationEffectReceipt) { receipt.InputDigest = "other" },
			func(receipt *controlplanev1.IntegrationEffectReceipt) { receipt.ResponseDigest = "" },
			func(receipt *controlplanev1.IntegrationEffectReceipt) { receipt.ProviderEffectRef = "" },
		} {
			claim := invocationClaim()
			claim.Risk = risk
			adapter := invocationAdapter{execute: func(_ context.Context, claim *controlplanev1.IntegrationInvocationClaim) (*controlplanev1.IntegrationEffectReceipt, error) {
				receipt := invocationReceipt(claim)
				mutate(receipt)
				return receipt, nil
			}}
			control := invocationControl{completeCall: func(_ context.Context, request *controlplanev1.CompleteIntegrationInvocationRequest) (*controlplanev1.CompleteIntegrationInvocationResponse, error) {
				if request.GetSuccess() || request.GetEffectReceipt() != nil || request.GetUnknownOutcome() != (risk == controlplanev1.IntegrationRisk_INTEGRATION_RISK_WRITE) {
					t.Fatal("invalid receipt accepted or mutation retried")
				}
				return &controlplanev1.CompleteIntegrationInvocationResponse{Run: &controlplanev1.Run{Ref: "run"}}, nil
			}}
			if err := processInvocation(t.Context(), control, adapter, claim, Config{RequestTimeout: time.Second}); !errors.Is(err, errDeliveryResponse) {
				t.Fatalf("receipt rejection=%v", err)
			}
		}
	}
}

func TestConnectionTestUsesExactCompletionAndRejectsWrongReadback(t *testing.T) {
	for _, reference := range []string{"connection", "other"} {
		claim := &controlplanev1.IntegrationConnectionTestClaim{TestRef: "test", ConnectionRef: "connection", DefinitionKey: "mattermost", Lease: deliveryClaim().GetLease()}
		adapter := invocationAdapter{test: func(ctx context.Context, _ *controlplanev1.IntegrationConnectionTestClaim) (string, error) {
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("unbounded connection test")
			}
			return "i18n:INTEGRATION_TEST_SUCCEEDED", nil
		}}
		control := invocationControl{completeTest: func(ctx context.Context, request *controlplanev1.CompleteIntegrationConnectionTestRequest) (*controlplanev1.CompleteIntegrationConnectionTestResponse, error) {
			if ctx.Err() != nil || !request.GetSuccess() || request.GetTestRef() != "test" || request.GetLeaseRef() != "lease" || request.GetFence() != "fixture-fence" || request.GetGeneration() != 1 || request.GetMutation().GetIdempotencyKey() == "" {
				t.Fatal("test completion not exact")
			}
			return &controlplanev1.CompleteIntegrationConnectionTestResponse{Connection: &controlplanev1.IntegrationConnection{Ref: reference}}, nil
		}}
		err := processConnectionTest(t.Context(), control, adapter, claim, Config{RequestTimeout: time.Second})
		if (err == nil) != (reference == "connection") {
			t.Fatalf("connection readback=%v", err)
		}
	}
}
