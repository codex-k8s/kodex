package app

import (
	"context"
	"errors"
	"strconv"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/interaction-gateway/internal/mattermost"
)

var (
	errDeliveryLease    = errors.New("interaction delivery lease is invalid or expired")
	errDeliveryResponse = errors.New("interaction delivery response is invalid")
)

type deliverySender interface {
	Deliver(context.Context, *controlplanev1.InteractionDeliveryClaim) (mattermost.DeliveryResult, error)
}

func processDeliveries(ctx context.Context, control controlplanev1.InteractionWorkServiceClient, adapter deliverySender, config Config) error {
	for index := int32(0); index < config.ClaimLimit; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= 2*config.RequestTimeout+time.Second {
			return nil
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

func deliveryContext(ctx context.Context, claim *controlplanev1.InteractionDeliveryClaim, completionBudget time.Duration) (context.Context, context.CancelFunc, error) {
	if claim.GetDeliveryRef() == "" {
		return nil, nil, errDeliveryLease
	}
	lease := claim.GetLease()
	return leaseContext(ctx, lease, completionBudget)
}

func leaseContext(ctx context.Context, lease *controlplanev1.WorkLease, completionBudget time.Duration) (context.Context, context.CancelFunc, error) {
	if lease == nil || lease.GetRef() == "" || lease.GetFence() == "" || lease.GetGeneration() < 1 || lease.GetExpiresAt() == nil || lease.GetExpiresAt().CheckValid() != nil {
		return nil, nil, errDeliveryLease
	}
	deadline := lease.GetExpiresAt().AsTime()
	if parent, ok := ctx.Deadline(); ok && parent.Before(deadline) {
		deadline = parent
	}
	deadline = deadline.Add(-completionBudget - time.Second)
	if !deadline.After(time.Now()) {
		return nil, nil, errDeliveryLease
	}
	child, cancel := context.WithDeadline(ctx, deadline)
	return child, cancel, nil
}

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
		Mutation:    &controlplanev1.MutationContext{IdempotencyKey: stableKey(claim.GetDeliveryRef(), lease.GetRef()+":"+strconv.FormatInt(lease.GetGeneration(), 10)+":complete")},
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
