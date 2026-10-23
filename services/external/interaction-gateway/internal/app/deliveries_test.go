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

// deliveryControl overrides only delivery claim and completion RPCs so tests can inspect exact requests without an owner service.
// RU: deliveryControl подменяет только RPC получения и завершения доставки для проверки точных запросов без сервиса-владельца.
type deliveryControl struct {
	controlplanev1.InteractionWorkServiceClient
	claim    func(context.Context, *controlplanev1.ClaimInteractionDeliveriesRequest) (*controlplanev1.ClaimInteractionDeliveriesResponse, error)
	complete func(context.Context, *controlplanev1.CompleteInteractionDeliveryRequest) (*controlplanev1.CompleteInteractionDeliveryResponse, error)
}

// ClaimInteractionDeliveries forwards context and request to the synthetic claim callback without changing their identity.
// RU: ClaimInteractionDeliveries передаёт контекст и запрос синтетическому обработчику claim без изменения их идентичности.
func (control deliveryControl) ClaimInteractionDeliveries(ctx context.Context, request *controlplanev1.ClaimInteractionDeliveriesRequest, _ ...grpc.CallOption) (*controlplanev1.ClaimInteractionDeliveriesResponse, error) {
	return control.claim(ctx, request)
}

// CompleteInteractionDelivery exposes the worker's completion request to a fixture callback without performing a real mutation.
// RU: CompleteInteractionDelivery передаёт запрос завершения worker обработчику оснастки без реального изменения состояния.
func (control deliveryControl) CompleteInteractionDelivery(ctx context.Context, request *controlplanev1.CompleteInteractionDeliveryRequest, _ ...grpc.CallOption) (*controlplanev1.CompleteInteractionDeliveryResponse, error) {
	return control.complete(ctx, request)
}

// senderFunc adapts an in-process callback to leased delivery dispatch without contacting Mattermost.
// RU: senderFunc превращает локальный обработчик в отправителя арендованной доставки без обращения к Mattermost.
type senderFunc func(context.Context, *controlplanev1.InteractionDeliveryClaim) (mattermost.DeliveryResult, error)

// Deliver forwards the exact dispatch context and claim to the test callback.
// RU: Deliver передаёт точный контекст отправки и claim обработчику теста.
func (fn senderFunc) Deliver(ctx context.Context, claim *controlplanev1.InteractionDeliveryClaim) (mattermost.DeliveryResult, error) {
	return fn(ctx, claim)
}

// deliveryClaim constructs a fresh synthetic generation-one delivery lease with enough time for an isolated test dispatch.
// RU: deliveryClaim создаёт новую синтетическую аренду доставки первого поколения с достаточным временем для изолированной тестовой отправки.
func deliveryClaim() *controlplanev1.InteractionDeliveryClaim {
	return &controlplanev1.InteractionDeliveryClaim{DeliveryRef: "delivery", Lease: &controlplanev1.WorkLease{Ref: "lease", Fence: "fixture-fence", Generation: 1, ExpiresAt: timestamppb.New(time.Now().Add(45 * time.Second))}}
}

// completionResponse reflects the requested outcome into a synthetic owner readback without marking the core run as affected.
// RU: completionResponse отражает запрошенный исход в синтетическом ответе владельца, не отмечая изменение основного запуска.
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

// noEffectFailure obtains a real adapter preflight error from a nil claim using isolated credentials and an embedded fixture catalog.
// RU: noEffectFailure получает настоящую ошибку предварительной проверки адаптера по nil claim с изолированными реквизитами и каталогом оснастки.
// The request fails before any remote effect, allowing completion classification to exercise ConfirmedNoEffect rather than a fabricated marker.
// RU: Запрос отклоняется до внешнего эффекта, позволяя проверить ConfirmedNoEffect по реальной классификации, а не выдуманному маркеру.
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
	_, err = adapter.Deliver(t.Context(), nil)
	return err
}

// TestDeliveryCompletionClassifiesEffectAndUsesAttemptIdentity checks success, uncertain dispatch and proven preflight failure with distinct generation keys.
// RU: TestDeliveryCompletionClassifiesEffectAndUsesAttemptIdentity проверяет успех, неопределённую отправку и доказанный предварительный отказ с разными ключами поколений.
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
				if err := completeDelivery(t.Context(), control, claim, deliveryResult(), tc.err); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// TestDeliveryClaimsOnlyWhenReadyToSendAndReservesCompletionTime proves sequential single claims, lease-bounded dispatch and a separate live completion context.
// RU: TestDeliveryClaimsOnlyWhenReadyToSendAndReservesCompletionTime доказывает последовательные одиночные claim, ограниченную арендой отправку и отдельный живой контекст завершения.
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
	sender := senderFunc(func(ctx context.Context, claim *controlplanev1.InteractionDeliveryClaim) (mattermost.DeliveryResult, error) {
		deadline, ok := ctx.Deadline()
		if !ok || !deadline.Before(claim.GetLease().GetExpiresAt().AsTime().Add(-time.Second)) {
			t.Fatal("lease expiry did not bound external operation")
		}
		return deliveryResult(), nil
	})
	if err := processDeliveries(t.Context(), control, sender, Config{ClaimLimit: 3, RequestTimeout: time.Second}); err != nil {
		t.Fatal(err)
	}
	if claimed != 3 || completed != 3 {
		t.Fatalf("claimed=%d completed=%d", claimed, completed)
	}
}

// TestDeliveryDoesNotDispatchExpiredOrMalformedLease rejects expired or incomplete expiry, generation and fence metadata before calling the sender.
// RU: TestDeliveryDoesNotDispatchExpiredOrMalformedLease отклоняет истёкшие или неполные срок, поколение и fence до вызова отправителя.
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
		sender := senderFunc(func(context.Context, *controlplanev1.InteractionDeliveryClaim) (mattermost.DeliveryResult, error) {
			t.Fatal("expired lease dispatched")
			return mattermost.DeliveryResult{}, nil
		})
		if err := processDeliveries(t.Context(), control, sender, Config{ClaimLimit: 1, RequestTimeout: time.Second}); !errors.Is(err, errDeliveryLease) {
			t.Fatalf("invalid lease = %v", err)
		}
	}
}

// TestCompletionRejectsWrongOwnerReadback refuses an otherwise successful completion response naming another delivery.
// RU: TestCompletionRejectsWrongOwnerReadback отклоняет формально успешный ответ завершения со ссылкой на другую доставку.
func TestCompletionRejectsWrongOwnerReadback(t *testing.T) {
	control := deliveryControl{complete: func(context.Context, *controlplanev1.CompleteInteractionDeliveryRequest) (*controlplanev1.CompleteInteractionDeliveryResponse, error) {
		return &controlplanev1.CompleteInteractionDeliveryResponse{DeliveryRef: "other", State: "SUCCEEDED"}, nil
	}}
	if err := completeDelivery(t.Context(), control, deliveryClaim(), deliveryResult(), nil); !errors.Is(err, errDeliveryResponse) {
		t.Fatalf("wrong readback = %v", err)
	}
}

// deliveryResult supplies synthetic post, thread, team and channel coordinates for completion binding tests.
// RU: deliveryResult задаёт синтетические координаты сообщения, ветки, команды и канала для проверки привязки завершения.
func deliveryResult() mattermost.DeliveryResult {
	return mattermost.DeliveryResult{PostRef: "post", ThreadRef: "post", TeamRef: "team", ChannelRef: "channel"}
}

// TestAcknowledgementCompletionBindsExactExternalDestination accepts matching coordinates and reports a changed channel as unknown rather than success.
// RU: TestAcknowledgementCompletionBindsExactExternalDestination принимает совпадающие координаты и передаёт изменённый канал как неизвестный исход, а не успех.
func TestAcknowledgementCompletionBindsExactExternalDestination(t *testing.T) {
	for _, correct := range []bool{true, false} {
		claim := deliveryClaim()
		claim.ExternalTeamRef, claim.ExternalChannelRef, claim.ExternalRootPostRef = "team", "channel", "post"
		result := deliveryResult()
		if !correct {
			result.ChannelRef = "other"
		}
		control := deliveryControl{complete: func(_ context.Context, request *controlplanev1.CompleteInteractionDeliveryRequest) (*controlplanev1.CompleteInteractionDeliveryResponse, error) {
			if request.GetExternalTeamRef() != result.TeamRef || request.GetExternalChannelRef() != result.ChannelRef || request.GetExternalThreadRef() != result.ThreadRef || request.GetSuccess() != correct || request.GetUnknownOutcome() == correct {
				t.Fatal("delivery lost exact destination or accepted mismatch")
			}
			return completionResponse(request), nil
		}}
		err := completeDelivery(t.Context(), control, claim, result, nil)
		if (err == nil) != correct {
			t.Fatalf("completion=%v", err)
		}
	}
}
