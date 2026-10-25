package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

// natsInitialRetryDelay prevents busy retry of the single transient initial
// no_servers class; it neither changes the process budget nor SDK reconnects.
// natsInitialRetryDelay предотвращает busy retry единственного временного класса
// initial no_servers; общий срок процесса и reconnect SDK не меняются.
const natsInitialRetryDelay = time.Second

// natsStartupConnect constructs one real NATS connection including the existing
// initial flush. Its caller owns a nonnil result even when an error is returned.
// natsStartupConnect создаёт одно реальное соединение NATS с прежним initial
// flush. Вызывающий владеет непустым результатом даже при одновременной ошибке.
type natsStartupConnect func(Config, *slog.Logger) (*nats.Conn, error)

// connectNATSStartup waits synchronously under Run's existing startup context,
// transferring a connected/flushed client only after a fresh context check.
// The one-shot constructor and established reconnect options remain unchanged;
// cancellation inside its DNS/dial/handshake is not implemented by this loop.
// connectNATSStartup ожидает синхронно под прежним startup-контекстом Run и
// передаёт connected/flushed клиент лишь после свежей проверки контекста.
// Конструктор одной попытки и established reconnect не меняются; отмена внутри
// его DNS/dial/handshake этим циклом не реализуется.
func connectNATSStartup(ctx context.Context, config Config, logger *slog.Logger, connect natsStartupConnect) (*nats.Conn, error) {
	var connection *nats.Conn
	err := waitForInitialNATS(ctx, natsInitialRetryDelay, func() (func(), error) {
		candidate, err := connect(config, logger)
		if candidate == nil {
			if err == nil {
				err = natsStartupFailure(natsStageConnect, nats.ErrInvalidConnection)
			}
			return nil, err
		}
		connection = candidate
		return candidate.Close, err
	})
	if err != nil {
		return nil, err
	}
	return connection, nil
}

// waitForInitialNATS owns each attempt's release function until success is
// admitted. Only a marked CONNECT/no_servers failure retries after a cancelable
// pause; permanent/unknown/flush failures and every exhausted context return.
// No new context budget, goroutine, SDK option or dependency write is created.
// waitForInitialNATS владеет release каждой попытки до допуска успеха. Лишь
// отмеченный CONNECT/no_servers повторяется после отменяемой паузы; permanent,
// unknown, flush и исчерпанный контекст завершают ожидание. Новый срок, goroutine,
// опция SDK либо запись в dependency не создаются.
func waitForInitialNATS(ctx context.Context, delay time.Duration, attempt func() (func(), error)) error {
	var lastNoServers error
	for {
		if budget := ctx.Err(); budget != nil {
			return natsStartupBudgetFailure(lastNoServers, budget)
		}
		release, err := attempt()
		if budget := ctx.Err(); budget != nil {
			if release != nil {
				release()
			}
			cause := err
			if cause == nil {
				cause = lastNoServers
			}
			return natsStartupBudgetFailure(cause, budget)
		}
		if err == nil {
			return nil
		}
		if release != nil {
			release()
		}
		if !initialNATSRetryable(err) {
			return natsStartupFailure(natsStageConnect, err)
		}
		lastNoServers = err
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return natsStartupBudgetFailure(lastNoServers, ctx.Err())
		case <-timer.C:
		}
	}
}

// initialNATSRetryable accepts only the constructor's direct CONNECT marker with
// no prior budget or nested diagnostic marker. Unknown wrappers/joins are closed;
// whole-error classification preserves permanent-cause precedence without a walker.
// initialNATSRetryable принимает лишь прямой CONNECT marker конструктора без
// прежнего budget либо вложенного diagnostic marker. Неизвестные wrapper/join
// закрыто отклоняются; классификация всего error сохраняет permanent precedence
// без построения обходчика графа ошибок.
func initialNATSRetryable(err error) bool {
	failure, ok := err.(*natsStartupError)
	if !ok || failure.stage != natsStageConnect || failure.budget != nil {
		return false
	}
	var nested *natsStartupError
	if errors.As(failure.cause, &nested) {
		return false
	}
	return natsFailureClass(failure.stage, err) == "no_servers"
}

// natsStartupBudgetFailure preserves the final typed cause and a separate
// cancellation/deadline marker without mutating an earlier diagnostic error.
// natsStartupBudgetFailure сохраняет последнюю типизированную причину и отдельный
// marker отмены/deadline, не меняя ранее созданную диагностическую ошибку.
func natsStartupBudgetFailure(cause, budget error) error {
	if cause == nil {
		cause = budget
	}
	stage := natsStageConnect
	var marked *natsStartupError
	if errors.As(cause, &marked) {
		stage = marked.stage
	}
	return &natsStartupError{stage: stage, cause: cause, budget: budget}
}
