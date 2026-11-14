package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/interaction-gateway/internal/telegram"
	"google.golang.org/grpc"
)

// telegramDeliveryControl reuses the old delivery recorder while adding the exact owner Begin RPC; it never talks to a provider or fabricates an owner approval.
// telegramDeliveryControl переиспользует прежний delivery recorder, добавляя exact owner Begin RPC; provider и фиктивные owner approvals отсутствуют.
type telegramDeliveryControl struct {
	deliveryControl
	begin func(context.Context, *cp.BeginInteractionDeliveryRequest) (*cp.BeginInteractionDeliveryResponse, error)
}

// BeginInteractionDelivery exposes the bounded immutable admission request to the synthetic owner callback.
// BeginInteractionDelivery передаёт bounded immutable admission request синтетическому owner callback.
func (control telegramDeliveryControl) BeginInteractionDelivery(ctx context.Context, request *cp.BeginInteractionDeliveryRequest, _ ...grpc.CallOption) (*cp.BeginInteractionDeliveryResponse, error) {
	return control.begin(ctx, request)
}

// telegramSenderFunc adapts an in-process fixture while preserving the worker-provided Begin callback and leased claim.
// telegramSenderFunc адаптирует in-process fixture, сохраняя переданные worker Begin callback и leased claim.
type telegramSenderFunc func(context.Context, *cp.InteractionDeliveryClaim, telegram.BeginOwner) (*cp.TelegramDeliveryReceipt, error)

// Deliver invokes only the selected fixture and does not perform a network effect.
// Deliver вызывает лишь выбранный fixture и не совершает сетевой эффект.
func (sender telegramSenderFunc) Deliver(ctx context.Context, claim *cp.InteractionDeliveryClaim, begin telegram.BeginOwner) (*cp.TelegramDeliveryReceipt, error) {
	return sender(ctx, claim, begin)
}

// TestTelegramWorkerPreservesAdmissionCompletionAndUnknownClassification checks exact request pins, bounded independent completion, quota-retry metadata and a safe fallback for unclassified transport errors.
// TestTelegramWorkerPreservesAdmissionCompletionAndUnknownClassification проверяет exact request pins, bounded independent completion, quota-retry metadata и безопасный fallback нераспознанных transport errors.
func TestTelegramWorkerPreservesAdmissionCompletionAndUnknownClassification(t *testing.T) {
	for _, outcome := range []string{"success", "rate-limit", "unknown", "missing-receipt", "owner-closed"} {
		t.Run(outcome, func(t *testing.T) {
			claim := deliveryClaim()
			claim.Telegram = &cp.TelegramDeliveryPin{InputDigest: strings.Repeat("a", 64)}
			begun, completed := 0, 0
			control := telegramDeliveryControl{deliveryControl: deliveryControl{complete: func(ctx context.Context, request *cp.CompleteInteractionDeliveryRequest) (*cp.CompleteInteractionDeliveryResponse, error) {
				completed++
				if ctx.Err() != nil {
					t.Fatal("completion inherited cancelled dispatch context")
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("completion is unbounded")
				}
				if request.GetInputDigest() != claim.Telegram.InputDigest || request.GetDeliveryRef() != claim.DeliveryRef || request.GetLeaseRef() != claim.Lease.Ref || request.GetFence() != claim.Lease.Fence || request.GetGeneration() != claim.Lease.Generation ||
					request.GetExternalTeamRef() != "" || request.GetExternalChannelRef() != "" || request.GetExternalPostRef() != "" || request.GetExternalThreadRef() != "" {
					t.Fatal("Telegram completion lost exact immutable lease or leaked another provider coordinates")
				}
				switch outcome {
				case "success":
					if !request.Success || request.TelegramReceipt == nil || request.UnknownOutcome || request.ConfirmedNoEffect {
						t.Fatal("success receipt classification changed")
					}
				case "rate-limit":
					if request.Success || request.UnknownOutcome || !request.ConfirmedNoEffect || request.RetryAfterSeconds != 17 || request.SafeErrorCode != "TELEGRAM_RATE_LIMITED" {
						t.Fatal("confirmed provider flood wait lost exact retry metadata")
					}
				default:
					if request.Success || !request.UnknownOutcome || request.ConfirmedNoEffect || request.SafeErrorCode != "TELEGRAM_OUTCOME_UNKNOWN" || request.TelegramReceipt != nil {
						t.Fatal("unclassified send did not remain safe unknown")
					}
				}
				return completionResponse(request), nil
			}}, begin: func(ctx context.Context, request *cp.BeginInteractionDeliveryRequest) (*cp.BeginInteractionDeliveryResponse, error) {
				begun++
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("Begin is unbounded")
				}
				if request.InputDigest != claim.Telegram.InputDigest || request.DeliveryRef != claim.DeliveryRef || request.Generation != claim.Lease.Generation {
					t.Fatal("Begin lost immutable claim pins")
				}
				return &cp.BeginInteractionDeliveryResponse{DeliveryRef: claim.DeliveryRef, InputDigest: claim.Telegram.InputDigest, Allowed: outcome != "owner-closed"}, nil
			}}
			sender := telegramSenderFunc(func(ctx context.Context, actual *cp.InteractionDeliveryClaim, begin telegram.BeginOwner) (*cp.TelegramDeliveryReceipt, error) {
				if actual != claim {
					t.Fatal("worker replaced leased input")
				}
				response, err := begin(ctx, &cp.BeginInteractionDeliveryRequest{DeliveryRef: actual.DeliveryRef, LeaseRef: actual.Lease.Ref, Fence: actual.Lease.Fence, Generation: actual.Lease.Generation, InputDigest: actual.Telegram.InputDigest})
				if err != nil {
					return nil, err
				}
				if !response.Allowed {
					return nil, &telegram.SafeError{Code: "TELEGRAM_ADMISSION_CLOSED", NoEffect: true}
				}
				switch outcome {
				case "success":
					return &cp.TelegramDeliveryReceipt{BotId: 123456, ChatId: -1001234, TopicId: 77, MessageId: 123}, nil
				case "rate-limit":
					return nil, &telegram.SafeError{Code: "TELEGRAM_RATE_LIMITED", NoEffect: true, RetryAfter: 17 * time.Second}
				case "missing-receipt":
					return nil, nil
				default:
					return nil, errors.New("synthetic private transport URL sentinel")
				}
			})
			err := processTelegramDelivery(t.Context(), control, sender, claim, Config{RequestTimeout: time.Second})
			if begun != 1 || outcome == "owner-closed" && (completed != 0 || err != nil) || outcome != "owner-closed" && completed != 1 || outcome == "success" && err != nil {
				t.Fatal("worker repeated admission, skipped completion or rewrote owner-closed lease")
			}
		})
	}
}

// readinessInteractionControl permits only the protected harmless source read; an accidental claim or provider call would hit the absent embedded client.
// readinessInteractionControl допускает лишь защищённое безвредное source read; случайный claim либо provider call попадёт в отсутствующий embedded client.
type readinessInteractionControl struct {
	cp.InteractionWorkServiceClient
	read func(context.Context, *cp.ListInteractionSourcesRequest) (*cp.ListInteractionSourcesResponse, error)
}

// ListInteractionSources forwards the same protected startup/readiness call without treating an empty provider catalog as a provider verification.
// ListInteractionSources передаёт тот же protected startup/readiness call и не считает пустой provider catalog проверкой провайдера.
func (control readinessInteractionControl) ListInteractionSources(ctx context.Context, request *cp.ListInteractionSourcesRequest, _ ...grpc.CallOption) (*cp.ListInteractionSourcesResponse, error) {
	return control.read(ctx, request)
}

// TestInteractionStartupReadinessRequiresProtectedOwnerPath rejects missing or failed local authority before RPC and rejects unavailable/partial owner replies before workers can start.
// TestInteractionStartupReadinessRequiresProtectedOwnerPath отклоняет отсутствующий либо неверный local authority до RPC и unavailable/partial owner replies до запуска workers.
func TestInteractionStartupReadinessRequiresProtectedOwnerPath(t *testing.T) {
	calls := 0
	control := readinessInteractionControl{read: func(context.Context, *cp.ListInteractionSourcesRequest) (*cp.ListInteractionSourcesResponse, error) {
		calls++
		return &cp.ListInteractionSourcesResponse{}, nil
	}}
	if !errors.Is(checkInteractionOwner(t.Context(), nil, control), errInteractionOwnerReady) || calls != 0 {
		t.Fatal("missing local authority reached owner RPC")
	}
	if !errors.Is(checkInteractionOwner(t.Context(), func(context.Context) error { return errors.New("synthetic private auth diagnostic") }, control), errInteractionOwnerReady) || calls != 0 {
		t.Fatal("failed local authority reached owner RPC")
	}
	if err := checkInteractionOwner(t.Context(), func(context.Context) error { return nil }, control); err != nil || calls != 1 {
		t.Fatal("protected empty owner read is unavailable", err)
	}
	control.read = func(context.Context, *cp.ListInteractionSourcesRequest) (*cp.ListInteractionSourcesResponse, error) {
		return nil, nil
	}
	if !errors.Is(checkInteractionOwner(t.Context(), func(context.Context) error { return nil }, control), errInteractionOwnerReady) {
		t.Fatal("partial owner response became ready")
	}
}
