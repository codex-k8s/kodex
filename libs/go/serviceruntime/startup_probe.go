package serviceruntime

import (
	"context"
	"errors"
	"time"
)

// StartupProbeFailure retains an operation cause separately from an exhausted caller budget; it never formats either.
// StartupProbeFailure хранит причину операции отдельно от исчерпанного бюджета вызывающего; ни одна не форматируется.
type StartupProbeFailure struct {
	// Cause is the last terminal probe error or the last retryable error when cancellation interrupted waiting.
	// Cause — последний постоянный отказ либо последний временный отказ, если ожидание прервано отменой.
	Cause error
	// Budget is set only when the original caller context has ended; it never extends that context.
	// Budget задаётся только при завершении исходного context вызывающего и никогда его не продлевает.
	Budget error
	// PreviousRetry preserves a prior retryable probe when cancellation overlaps a new error; service policy chooses diagnostic precedence.
	// PreviousRetry сохраняет прежний временный отказ при совпадении отмены с новой ошибкой; приоритет диагностики выбирает сервис.
	PreviousRetry error
}

// WaitStartupProbe runs sequential bounded readiness probes under an existing context and service-owned retry predicate.
// Only a context with a deadline permits retries; no goroutine, pool, SQL, logging or cleanup is owned here.
// A nil result transfers admission to the caller. Cancellation after a successful probe still rejects admission.
// WaitStartupProbe выполняет последовательные bounded readiness проверки в прежнем context с сервисным предикатом повтора.
// Повторы разрешены только context с deadline; goroutine, pool, SQL, логирование и cleanup здесь не принадлежат библиотеке.
// Nil передаёт допуск вызывающему. Отмена после успешной проверки всё равно отклоняет допуск.
func WaitStartupProbe(ctx context.Context, probe func(context.Context) error, retryable func(error) bool, probeBudget, delay time.Duration) *StartupProbeFailure {
	if probe == nil || retryable == nil || probeBudget <= 0 || delay <= 0 {
		return &StartupProbeFailure{Cause: errors.New("startup probe configuration is invalid")}
	}
	var last error
	for {
		if err := ctx.Err(); err != nil {
			if last == nil {
				last = err
			}
			return &StartupProbeFailure{Cause: last, Budget: err}
		}
		bounded, cancel := context.WithTimeout(ctx, probeBudget)
		err := probe(bounded)
		if err == nil {
			err = bounded.Err()
		}
		cancel()
		if err == nil {
			err = ctx.Err()
		}
		if err == nil {
			return nil
		}
		if budget := ctx.Err(); budget != nil {
			return &StartupProbeFailure{Cause: err, Budget: budget, PreviousRetry: last}
		}
		_, hasDeadline := ctx.Deadline()
		if !hasDeadline || !retryable(err) {
			return &StartupProbeFailure{Cause: err}
		}
		last = err
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return &StartupProbeFailure{Cause: last, Budget: ctx.Err()}
		case <-timer.C:
		}
	}
}
