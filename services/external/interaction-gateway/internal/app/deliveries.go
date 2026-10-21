package app

import (
	"context"
	"errors"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/interaction-gateway/internal/mattermost"
)

var (
	// errDeliveryLease rejects missing, malformed or too-short leased work before dispatching an external effect.
	// RU: errDeliveryLease отклоняет отсутствующую, неверную или слишком короткую аренду до внешнего эффекта.
	errDeliveryLease = errors.New("interaction delivery lease is invalid or expired")
	// errDeliveryResponse reports a malformed claim, result or owner readback without accepting it as a successful delivery.
	// RU: errDeliveryResponse обозначает неверную заявку, результат или ответ владельца, не принимая их за успешную доставку.
	errDeliveryResponse = errors.New("interaction delivery response is invalid")
)

// deliverySender exposes only leased delivery dispatch; the owner RPC client still controls claim and completion.
// RU: deliverySender открывает только отправку по арендованной доставке; claim и завершением по-прежнему управляет owner RPC клиент.
type deliverySender interface {
	// Deliver attempts the exact claimed delivery and returns observed external coordinates or an effect-classified error.
	// RU: Deliver выполняет точную арендованную доставку и возвращает наблюдаемые внешние координаты либо ошибку с классификацией эффекта.
	Deliver(context.Context, *controlplanev1.InteractionDeliveryClaim) (mattermost.DeliveryResult, error)
}

// processDeliveries claims one delivery at a time, bounds dispatch by its lease and completes it before claiming another.
// RU: processDeliveries арендует по одной доставке, ограничивает отправку сроком аренды и завершает её до получения следующей.
// It reserves completion time and returns adapter errors only after reporting the observed outcome to the owner.
// RU: Функция оставляет время для завершения и возвращает ошибку адаптера лишь после передачи наблюдаемого исхода владельцу.
func processDeliveries(ctx context.Context, control controlplanev1.InteractionWorkServiceClient, adapter deliverySender, config Config) error {
	for index := int32(0); index < config.ClaimLimit; index++ {
		if available, err := claimBudgetAvailable(ctx, config.RequestTimeout); err != nil || !available {
			return err
		}
		claimContext, cancelClaim := context.WithTimeout(ctx, config.RequestTimeout)
		// Не арендуем очередь, которая состарится до начала последовательной отправки.
		claimed, err := control.ClaimInteractionDeliveries(claimContext, &controlplanev1.ClaimInteractionDeliveriesRequest{WorkloadInstance: config.InstanceID, Limit: 1})
		cancelClaim()
		if err != nil {
			return err
		}
		if claimed == nil || len(claimed.GetClaims()) > 1 {
			return errDeliveryResponse
		}
		if len(claimed.GetClaims()) == 0 {
			return nil
		}
		claim := claimed.GetClaims()[0]
		sendContext, cancelSend, err := deliveryContext(ctx, claim, config.RequestTimeout)
		if err != nil {
			return err
		}
		result, deliveryErr := adapter.Deliver(sendContext, claim)
		cancelSend()
		completionContext, cancelCompletion := context.WithTimeout(ctx, config.RequestTimeout)
		err = completeDelivery(completionContext, control, claim, result, deliveryErr)
		cancelCompletion()
		if err != nil {
			return err
		}
		if deliveryErr != nil {
			return deliveryErr
		}
	}
	return nil
}

// deliveryContext requires a delivery locator and delegates lease validation and completion reserve to the common work helper.
// RU: deliveryContext требует ссылку доставки и передаёт проверку аренды и запаса завершения общему helper работы.
func deliveryContext(ctx context.Context, claim *controlplanev1.InteractionDeliveryClaim, completionBudget time.Duration) (context.Context, context.CancelFunc, error) {
	if claim.GetDeliveryRef() == "" {
		return nil, nil, errDeliveryLease
	}
	lease := claim.GetLease()
	return leaseContext(ctx, lease, completionBudget)
}

// completeDelivery classifies success, confirmed no-effect or unknown outcome, binds completion to the exact lease and verifies owner readback.
// RU: completeDelivery различает успех, подтверждённое отсутствие эффекта и неизвестный исход, привязывает завершение к точной аренде и проверяет ответ владельца.
// A mismatched external destination is reported as unknown, never successful, and this path must not affect the core run.
// RU: Несовпадающий внешний адрес передаётся как неизвестный исход, а не успех; этот путь не должен затрагивать основной запуск.
func completeDelivery(ctx context.Context, control controlplanev1.InteractionWorkServiceClient, claim *controlplanev1.InteractionDeliveryClaim, result mattermost.DeliveryResult, deliveryErr error) error {
	lease := claim.GetLease()
	if lease == nil {
		return errDeliveryLease
	}
	invalidResult := deliveryErr == nil && (result.PostRef == "" || result.ThreadRef == "" || result.TeamRef == "" || result.ChannelRef == "" ||
		(claim.GetExternalTeamRef() != "" && claim.GetExternalTeamRef() != result.TeamRef) ||
		(claim.GetExternalChannelRef() != "" && claim.GetExternalChannelRef() != result.ChannelRef) ||
		(claim.GetExternalRootPostRef() != "" && claim.GetExternalRootPostRef() != result.ThreadRef))
	if invalidResult {
		deliveryErr = errDeliveryResponse
	}
	success, code := mattermost.Outcome(deliveryErr)
	noEffect := mattermost.ConfirmedNoEffect(deliveryErr)
	unknown := !success && !noEffect
	state := "SUCCEEDED"
	if noEffect {
		state = "FAILED"
	}
	if unknown {
		state, code = "UNKNOWN_OUTCOME", "INTERACTION_OUTCOME_UNKNOWN"
	}
	response, err := control.CompleteInteractionDelivery(ctx, &controlplanev1.CompleteInteractionDeliveryRequest{
		Mutation:    &controlplanev1.MutationContext{IdempotencyKey: workCompletionKey(claim.GetDeliveryRef(), lease)},
		DeliveryRef: claim.GetDeliveryRef(), LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration(),
		Success: success, SafeErrorCode: code,
		ExternalPostRef: result.PostRef, ExternalThreadRef: result.ThreadRef,
		ExternalTeamRef: result.TeamRef, ExternalChannelRef: result.ChannelRef,
		UnknownOutcome: unknown, ConfirmedNoEffect: noEffect,
	})
	if err != nil {
		return err
	}
	if response.GetDeliveryRef() != claim.GetDeliveryRef() || response.GetState() != state || response.GetCoreRunAffected() {
		return errDeliveryResponse
	}
	if invalidResult {
		return errDeliveryResponse
	}
	return nil
}
