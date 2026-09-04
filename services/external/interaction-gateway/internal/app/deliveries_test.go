package app

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	texti18n "github.com/codex-k8s/kodex/libs/go/i18n"
	"github.com/codex-k8s/kodex/services/external/interaction-gateway/internal/mattermost"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type deliveryControl struct {
	controlplanev1.InteractionWorkServiceClient
	claim    func(context.Context, *controlplanev1.ClaimInteractionDeliveriesRequest) (*controlplanev1.ClaimInteractionDeliveriesResponse, error)
	complete func(context.Context, *controlplanev1.CompleteInteractionDeliveryRequest) (*controlplanev1.CompleteInteractionDeliveryResponse, error)
}

func (control deliveryControl) ClaimInteractionDeliveries(ctx context.Context, request *controlplanev1.ClaimInteractionDeliveriesRequest, _ ...grpc.CallOption) (*controlplanev1.ClaimInteractionDeliveriesResponse, error) {
	return control.claim(ctx, request)
}
func (control deliveryControl) CompleteInteractionDelivery(ctx context.Context, request *controlplanev1.CompleteInteractionDeliveryRequest, _ ...grpc.CallOption) (*controlplanev1.CompleteInteractionDeliveryResponse, error) {
	return control.complete(ctx, request)
}

type senderFunc func(context.Context, *controlplanev1.InteractionDeliveryClaim) (string, string, error)

func (fn senderFunc) Deliver(ctx context.Context, claim *controlplanev1.InteractionDeliveryClaim) (string, string, error) {
	return fn(ctx, claim)
}

func deliveryClaim() *controlplanev1.InteractionDeliveryClaim {
	return &controlplanev1.InteractionDeliveryClaim{DeliveryRef: "delivery", Lease: &controlplanev1.WorkLease{Ref: "lease", Fence: "fixture-fence", Generation: 1, ExpiresAt: timestamppb.New(time.Now().Add(45 * time.Second))}}
}

func completionResponse(request *controlplanev1.CompleteInteractionDeliveryRequest) *controlplanev1.CompleteInteractionDeliveryResponse {
	state := "SUCCEEDED"
	if request.GetUnknownOutcome() {
		state = "UNKNOWN_OUTCOME"
	}
	if request.GetConfirmedNoEffect() {
		state = "FAILED"
	}
	return &controlplanev1.CompleteInteractionDeliveryResponse{DeliveryRef: request.GetDeliveryRef(), State: state}
}

func noEffectFailure(t *testing.T) error {
	t.Helper()
	text, err := texti18n.New(texti18n.Config{Locale: texti18n.DefaultLocale, MessageFS: fstest.MapFS{"messages.en.yaml": {Data: []byte("READY:\n  other: Ready\n")}}, MessageFiles: []string{"messages.en.yaml"}})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := mattermost.New(mattermost.Config{CredentialDirectory: t.TempDir(), ProxyURL: "http://egress-gateway.kodex-system.svc.cluster.local:8080", Timeout: time.Second}, text)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = adapter.Deliver(t.Context(), nil)
	return err
}

func TestDeliveryCompletionClassifiesEffectAndUsesAttemptIdentity(t *testing.T) {
	for _, tc := range []struct {
		name              string
		err               error
		unknown, noEffect bool
	}{
		{name: "success"}, {name: "uncertain", err: context.DeadlineExceeded, unknown: true}, {name: "preflight", err: noEffectFailure(t), noEffect: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keys := map[string]bool{}
			control := deliveryControl{complete: func(ctx context.Context, request *controlplanev1.CompleteInteractionDeliveryRequest) (*controlplanev1.CompleteInteractionDeliveryResponse, error) {
				if request.GetUnknownOutcome() != tc.unknown || request.GetConfirmedNoEffect() != tc.noEffect || request.GetSuccess() != (tc.err == nil) {
					t.Fatalf("wrong effect outcome: %v", request)
				}
				if request.GetUnknownOutcome() && request.GetSafeErrorCode() != "INTERACTION_OUTCOME_UNKNOWN" {
					t.Fatal("unknown result not classified")
				}
				key := request.GetMutation().GetIdempotencyKey()
				if keys[key] {
					t.Fatal("idempotency key reused across lease generation")
				}
				keys[key] = true
				return completionResponse(request), nil
			}}
			claim := deliveryClaim()
			for generation := int64(1); generation <= 2; generation++ {
				claim.Lease.Generation = generation
				if err := completeDelivery(t.Context(), control, claim, "post", "post", tc.err); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestDeliveryClaimsOnlyWhenReadyToSendAndReservesCompletionTime(t *testing.T) {
	claimed, completed := 0, 0
	control := deliveryControl{
		claim: func(ctx context.Context, request *controlplanev1.ClaimInteractionDeliveriesRequest) (*controlplanev1.ClaimInteractionDeliveriesResponse, error) {
			if request.GetLimit() != 1 || claimed != completed {
				t.Fatal("queued leases acquired before previous delivery completion")
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("unbounded claim RPC")
			}
			claimed++
			return &controlplanev1.ClaimInteractionDeliveriesResponse{Claims: []*controlplanev1.InteractionDeliveryClaim{deliveryClaim()}}, nil
		},
		complete: func(ctx context.Context, request *controlplanev1.CompleteInteractionDeliveryRequest) (*controlplanev1.CompleteInteractionDeliveryResponse, error) {
			if ctx.Err() != nil {
				t.Fatal("completion inherited cancelled send context")
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("unbounded completion RPC")
			}
			completed++
			return completionResponse(request), nil
		},
	}
	sender := senderFunc(func(ctx context.Context, claim *controlplanev1.InteractionDeliveryClaim) (string, string, error) {
		deadline, ok := ctx.Deadline()
		if !ok || !deadline.Before(claim.GetLease().GetExpiresAt().AsTime().Add(-time.Second)) {
			t.Fatal("lease expiry did not bound external operation")
		}
		return "post", "post", nil
	})
	if err := processDeliveries(t.Context(), control, sender, Config{ClaimLimit: 3, RequestTimeout: time.Second}); err != nil {
		t.Fatal(err)
	}
	if claimed != 3 || completed != 3 {
		t.Fatalf("claimed=%d completed=%d", claimed, completed)
	}
}

func TestDeliveryDoesNotDispatchExpiredOrMalformedLease(t *testing.T) {
	for _, mutate := range []func(*controlplanev1.InteractionDeliveryClaim){
		func(claim *controlplanev1.InteractionDeliveryClaim) {
			claim.Lease.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
		},
		func(claim *controlplanev1.InteractionDeliveryClaim) { claim.Lease.ExpiresAt = nil },
		func(claim *controlplanev1.InteractionDeliveryClaim) { claim.Lease.Generation = 0 },
		func(claim *controlplanev1.InteractionDeliveryClaim) { claim.Lease.Fence = "" },
	} {
		claim := deliveryClaim()
		mutate(claim)
		control := deliveryControl{claim: func(context.Context, *controlplanev1.ClaimInteractionDeliveriesRequest) (*controlplanev1.ClaimInteractionDeliveriesResponse, error) {
			return &controlplanev1.ClaimInteractionDeliveriesResponse{Claims: []*controlplanev1.InteractionDeliveryClaim{claim}}, nil
		}}
		sender := senderFunc(func(context.Context, *controlplanev1.InteractionDeliveryClaim) (string, string, error) {
			t.Fatal("expired lease dispatched")
			return "", "", nil
		})
		if err := processDeliveries(t.Context(), control, sender, Config{ClaimLimit: 1, RequestTimeout: time.Second}); !errors.Is(err, errDeliveryLease) {
			t.Fatalf("invalid lease = %v", err)
		}
	}
}

func TestCompletionRejectsWrongOwnerReadback(t *testing.T) {
	control := deliveryControl{complete: func(context.Context, *controlplanev1.CompleteInteractionDeliveryRequest) (*controlplanev1.CompleteInteractionDeliveryResponse, error) {
		return &controlplanev1.CompleteInteractionDeliveryResponse{DeliveryRef: "other", State: "SUCCEEDED"}, nil
	}}
	if err := completeDelivery(t.Context(), control, deliveryClaim(), "post", "post", nil); !errors.Is(err, errDeliveryResponse) {
		t.Fatalf("wrong readback = %v", err)
	}
}
