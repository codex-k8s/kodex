package app

import (
	"context"
	"errors"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	"google.golang.org/protobuf/proto"
)

// errKubernetesLease reports loss of confirmed local lease lifetime without exposing the original fence or owner diagnostics.
// errKubernetesLease сообщает об утрате подтверждённого локального срока lease без раскрытия исходного fence либо owner diagnostics.
var errKubernetesLease = errors.New("managed Kubernetes invocation lease is no longer confirmed")

// errKubernetesRenewalProtocol preserves a malformed owner response as a failure even when later cleanup confirms terminal state.
// errKubernetesRenewalProtocol сохраняет malformed owner response как ошибку даже после подтверждения terminal state при очистке.
var errKubernetesRenewalProtocol = errors.New("managed Kubernetes renewal protocol failed")

// runKubernetesRenewed owns execution, renewal and an independent lease watchdog through positive join. A blocked renewal cannot extend the last confirmed deadline, and canceled execution still retains capacity until all local cleanup returns.
// runKubernetesRenewed владеет execution, renewal и независимым lease watchdog до positive join. Заблокированное продление не расширяет последний подтверждённый срок, а отменённая execution сохраняет ёмкость до возврата всей локальной очистки.
func runKubernetesRenewed(ctx context.Context, input kubernetesActivityInput, owner cp.ManagedKubernetesOwnerServiceClient, execute kubernetesActivityRunner) error {
	if ctx == nil || ctx.Err() != nil || input.binding == nil || owner == nil || execute == nil || !input.leaseUntil.After(time.Now()) || input.leaseUntil.After(time.Now().Add(30*time.Second)) {
		return errKubernetesLease
	}
	lifecycle, cancel := context.WithCancel(ctx)
	defer cancel()
	binding := proto.Clone(input.binding).(*sb.ManagedKubernetesInvocationBinding)
	updates := make(chan time.Time)
	result := make(chan error, 1)
	workers := serviceruntime.StartWorkers(lifecycle,
		func(work context.Context) error {
			defer cancel()
			if work.Err() != nil {
				result <- work.Err()
				return nil
			}
			result <- execute(work, input)
			return nil
		},
		func(work context.Context) error {
			return renewKubernetesLease(work, binding, input.leaseUntil, owner, updates)
		},
		func(work context.Context) error { return watchKubernetesLease(work, input.leaseUntil, updates) },
	)
	// The enclosing consumer/application supplies the bounded shutdown observer; abandoning this join would falsely release an active slot.
	// Ограниченное наблюдение shutdown принадлежит внешнему consumer/application; отказ от этого join ложно освободил бы активный slot.
	err := workers.Wait(context.WithoutCancel(ctx))
	executionErr := <-result
	if executionErr == nil && kubernetesResolvedLeaseLoss(err) {
		return nil
	}
	return errors.Join(err, executionErr)
}

// kubernetesResolvedLeaseLoss recognizes only exact local lease/cancellation causes, recursively checking every joined error; a shared cause never hides another failure.
// kubernetesResolvedLeaseLoss распознаёт только точные локальные lease/cancellation причины, проверяя каждую joined error; общая причина не скрывает другую ошибку.
func kubernetesResolvedLeaseLoss(err error) bool {
	if err == nil || err == errKubernetesLease || err == context.Canceled || err == context.DeadlineExceeded {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range joined.Unwrap() {
			if !kubernetesResolvedLeaseLoss(cause) {
				return false
			}
		}
		return true
	}
	return false
}

// renewKubernetesLease renews before one third of the remaining lifetime passes, preserving exact owner binding and publishing only validated deadlines.
// renewKubernetesLease продлевает lease до истечения трети оставшегося срока, сохраняя exact owner binding и публикуя только проверенные deadlines.
func renewKubernetesLease(ctx context.Context, binding *sb.ManagedKubernetesInvocationBinding, until time.Time, owner cp.ManagedKubernetesOwnerServiceClient, updates chan<- time.Time) error {
	for {
		remaining := time.Until(until)
		if remaining <= 0 {
			return errKubernetesLease
		}
		timer := time.NewTimer(min(10*time.Second, remaining/3))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		next, err := integration.RenewKubernetesInvocation(ctx, binding, until, owner)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			var safe *integration.SafeError
			if errors.As(err, &safe) && safe.Code == "INTEGRATION_UNAVAILABLE" {
				return errKubernetesLease
			}
			if errors.As(err, &safe) && safe.Code == "INTEGRATION_REQUEST_REJECTED" && !until.After(time.Now()) {
				return errKubernetesLease
			}
			return errKubernetesRenewalProtocol
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case updates <- next:
			until = next
		}
	}
}

// watchKubernetesLease cancels the worker group at the last confirmed deadline even if the renewal RPC ignores cancellation; a late acknowledgement cannot revive an expired local activity.
// watchKubernetesLease отменяет worker group по последнему подтверждённому сроку, даже если renewal RPC игнорирует отмену; позднее acknowledgement не оживляет истёкшую локальную activity.
func watchKubernetesLease(ctx context.Context, until time.Time, updates <-chan time.Time) error {
	timer := time.NewTimer(max(0, time.Until(until)))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errKubernetesLease
		case next := <-updates:
			now := time.Now()
			if !until.After(now) || !next.After(now) {
				return errKubernetesLease
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			until = next
			timer.Reset(time.Until(until))
		}
	}
}
