package app

import (
	"context"
	"errors"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const (
	maximumLeaseRenewalTries       = 4
	leaseRenewalExpirySafetyMargin = time.Second
)

var errLeaseRenewalBinding = errors.New("runtime lease renewal response binding is invalid")

// confirmedUntil принадлежит последнему проверенному owner ответу, не input.
// Любой неоднозначный RPC сохраняет прежнюю границу; retry не продлевает её.
func (runtime *runtime) renewLease(parent context.Context, input runtimecontract.RunnerInput, phase renewalPhase, confirmedUntil time.Time) (time.Time, error) {
	if input.LeaseRef == "" || input.LeaseFence == "" || input.LeaseGeneration < 1 {
		return confirmedUntil, errLeaseRenewalBinding
	}
	bounded := parent
	cancelBounded := func() {}
	if !confirmedUntil.IsZero() {
		bounded, cancelBounded = context.WithDeadline(parent, confirmedUntil)
	}
	defer cancelBounded()
	var err error
	for attempt := 0; attempt < maximumLeaseRenewalTries; attempt++ {
		if cause := renewalContextError(bounded); cause != nil {
			return confirmedUntil, cause
		}
		request, cancel := context.WithTimeout(bounded, runtime.config.RequestTimeout)
		response, callErr := runtime.control.RenewExecution(request, &controlplanev1.RenewExecutionRequest{
			LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration})
		if callErr == nil {
			callErr = renewalContextError(request)
		}
		runtime.logRenewalFailure(request, parent, input, phase, callErr)
		cancel()
		err = callErr
		if err == nil {
			// Поздний success не оживляет уже истёкшую подтверждённую lease.
			if cause := renewalContextError(bounded); cause != nil {
				return confirmedUntil, cause
			}
			lease := response.GetLease()
			expiry := lease.GetExpiresAt()
			if lease.GetRef() != input.LeaseRef || lease.GetFence() != input.LeaseFence || lease.GetGeneration() != input.LeaseGeneration ||
				expiry == nil || expiry.CheckValid() != nil {
				return confirmedUntil, errLeaseRenewalBinding
			}
			now := time.Now()
			remaining := expiry.AsTime().Sub(now)
			if remaining <= 0 {
				return confirmedUntil, errLeaseRenewalBinding
			}
			// Запас только сокращает owner срок. Монотонная локальная граница
			// не переносится wall-clock коррекцией после получения ответа.
			margin := min(leaseRenewalExpirySafetyMargin, remaining/10)
			return now.Add(remaining - margin), nil
		}
		if confirmedUntil.IsZero() || !transientControlPlaneFailure(err) || attempt == maximumLeaseRenewalTries-1 || renewalContextError(bounded) != nil {
			return confirmedUntil, err
		}
		delay := min(250*time.Millisecond, runtime.config.RequestTimeout/10) << attempt
		if !waitWithoutCancel(bounded, delay) {
			return confirmedUntil, errors.Join(err, bounded.Err())
		}
	}
	return confirmedUntil, err
}

// Timer goroutine может ещё не выставить Err на уже истёкшем deadline.
// Срок проверяется также монотонными часами, без ожидания scheduler callback.
func renewalContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

func leaseBoundedRequest(parent context.Context, timeout time.Duration, confirmedUntil time.Time) (context.Context, context.CancelFunc) {
	deadline := time.Now().Add(timeout)
	if !confirmedUntil.IsZero() && confirmedUntil.Before(deadline) {
		deadline = confirmedUntil
	}
	return context.WithDeadline(parent, deadline)
}

func resetLeaseExpiry(timer *time.Timer, confirmedUntil time.Time) <-chan time.Time {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	if confirmedUntil.IsZero() {
		return nil
	}
	timer.Reset(time.Until(confirmedUntil))
	return timer.C
}
