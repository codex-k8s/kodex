package app

import (
	"context"
	"strconv"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// claimBudgetAvailable preserves cancellation errors and checks that claim and completion RPC budgets plus one second remain before leasing work.
// RU: claimBudgetAvailable сохраняет ошибку отмены и проверяет запас двух RPC-бюджетов и одной секунды до получения аренды.
// An insufficient deadline is an idle result, not a failed effect; this local preflight neither claims nor extends a lease.
// RU: Недостаточный срок означает отсутствие новой работы, а не неудачный эффект; локальная проверка не получает и не продлевает аренду.
func claimBudgetAvailable(ctx context.Context, requestTimeout time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	deadline, bounded := ctx.Deadline()
	return !bounded || time.Until(deadline) > 2*requestTimeout+time.Second, nil
}

// leaseContext validates the lease locator, fence, generation and expiry, then reserves completion time before the earlier parent or lease deadline.
// RU: leaseContext проверяет ссылку, fence, поколение и срок аренды, затем оставляет запас завершения до более раннего срока родителя или аренды.
// It does not renew owner authority; the caller must cancel the returned child after dispatch.
// RU: Функция не продлевает полномочия владельца; вызывающая сторона обязана отменить возвращённый дочерний контекст после отправки.
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

// workCompletionKey preserves the stable completion identity for a work reference, lease reference and generation without minting authority.
// RU: workCompletionKey сохраняет устойчивую идентичность завершения по ссылке работы, аренде и поколению, не выдавая полномочий.
func workCompletionKey(reference string, lease *controlplanev1.WorkLease) string {
	return stableKey(reference, lease.GetRef()+":"+strconv.FormatInt(lease.GetGeneration(), 10)+":complete")
}
