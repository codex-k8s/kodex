package app

import (
	"context"
	"errors"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const (
	// maximumLeaseRenewalTries caps owner renewal calls at four, including the initial attempt.
	// maximumLeaseRenewalTries ограничивает обращения к владельцу для продления четырьмя, включая первую попытку.
	maximumLeaseRenewalTries = 4
	// leaseRenewalExpirySafetyMargin caps the local expiry reduction at one second;
	// short leases instead reserve one tenth of their remaining owner-confirmed duration.
	// leaseRenewalExpirySafetyMargin ограничивает локальное сокращение срока одной секундой;
	// для коротких аренд резервируется одна десятая оставшейся подтверждённой владельцем длительности.
	leaseRenewalExpirySafetyMargin = time.Second
)

// errLeaseRenewalBinding marks invalid request pins or an absent, foreign or expired owner renewal response.
// It never represents permission to extend the last confirmed expiry.
// errLeaseRenewalBinding обозначает неверные привязки запроса либо отсутствующий, чужой или просроченный ответ владельца.
// Эта ошибка никогда не означает разрешения продлить последний подтверждённый срок.
var errLeaseRenewalBinding = errors.New("runtime lease renewal response binding is invalid")

// renewLease renews exact lease/fence/generation pins within the parent and last confirmed expiry, retaining that expiry on failure.
// Only transient delivery errors with a prior confirmed expiry permit bounded retries. A timely matching owner response produces a shortened monotonic local deadline; late success cannot revive expired authority.
// renewLease продлевает точные привязки аренды/ограждения/поколения в пределах родительского и последнего подтверждённого срока, сохраняя прежний срок при ошибке.
// Только временные ошибки доставки при известном подтверждённом сроке допускают ограниченные повторы. Своевременный совпадающий ответ владельца даёт сокращённый монотонный локальный срок; поздний успех не возрождает истёкшие полномочия.
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
			// A late success cannot revive a confirmed lease that has already expired.
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
			// The margin only shortens the owner expiry. Once derived, the monotonic local
			// deadline is not shifted by subsequent wall-clock corrections.
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

// renewalContextError first preserves a context error, then independently compares its deadline with the current time.
// An elapsed deadline is rejected even if the timer goroutine has not yet published DeadlineExceeded.
// renewalContextError сначала сохраняет ошибку контекста, затем независимо сверяет его срок с текущим временем.
// Истёкший срок отклоняется, даже если горутина таймера ещё не установила DeadlineExceeded.
func renewalContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

// leaseBoundedRequest creates a child deadline at the earlier of the request budget and nonzero confirmed lease expiry.
// Parent cancellation and any earlier parent deadline remain effective; callers own the returned cancel function.
// leaseBoundedRequest создаёт дочерний срок по минимуму бюджета запроса и ненулевого подтверждённого срока аренды.
// Отмена и более ранний срок родителя продолжают действовать; возвращённую функцию отмены обязан вызвать потребитель.
func leaseBoundedRequest(parent context.Context, timeout time.Duration, confirmedUntil time.Time) (context.Context, context.CancelFunc) {
	deadline := time.Now().Add(timeout)
	if !confirmedUntil.IsZero() && confirmedUntil.Before(deadline) {
		deadline = confirmedUntil
	}
	return context.WithDeadline(parent, deadline)
}

// resetLeaseExpiry stops and opportunistically drains the existing timer before selecting the new confirmed expiry.
// A zero expiry disables the caller's select arm by returning nil; otherwise the reused timer channel is returned.
// resetLeaseExpiry останавливает имеющийся таймер и при необходимости неблокирующе очищает канал перед установкой подтверждённого срока.
// Нулевой срок отключает ветку select возвратом nil; иначе возвращается канал переиспользуемого таймера.
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
