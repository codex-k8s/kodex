package readback

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// callReadbackRPC preserves this client's fixed three-attempt policy for an
// already-bound idempotent request: only Unavailable retries, after 100/250 ms.
// The callback owns the exact request and credentials. Cancellation during a
// wait returns the context error directly; a final RPC error is stage-wrapped.
// The first call has no added context precheck, and no goroutine is started.
// callReadbackRPC сохраняет фиксированную политику клиента из трёх попыток для
// уже связанного идемпотентного запроса: повторяется только Unavailable после
// 100/250 мс. Точным запросом и credentials владеет callback. Отмена ожидания
// возвращает ошибку контекста напрямую; итоговая ошибка RPC обёрнута этапом.
// Первая попытка не получает новой проверки контекста, goroutine не создаётся.
func callReadbackRPC[T any](
	ctx context.Context,
	operation string,
	call func() (T, error),
) (T, error) {
	var response T
	var err error
	for attempt, delay := range []time.Duration{0, 100 * time.Millisecond, 250 * time.Millisecond} {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				var zero T
				return zero, ctx.Err()
			case <-timer.C:
			}
		}
		response, err = call()
		if err == nil || status.Code(err) != codes.Unavailable || attempt == 2 {
			break
		}
	}
	if err != nil {
		return response, fmt.Errorf("%s: %w", operation, err)
	}
	return response, nil
}
