package main

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/observability/errorclass"
)

// databaseRetryDelay spaces only transient connection probes; migration SQL is never retried by this wait loop.
// databaseRetryDelay разделяет только временно отказавшие проверки соединения; SQL миграций этим циклом никогда не повторяется.
const databaseRetryDelay = time.Second

// connectionWaitFailure retains the last network cause and the exhausted caller budget for separate safe diagnostic classification.
// connectionWaitFailure сохраняет последнюю сетевую причину и исчерпанный бюджет вызывающего для раздельной безопасной классификации.
type connectionWaitFailure struct{ cause, budget error }

// Error returns a fixed wait-exhaustion message without formatting network hostnames, DSNs or nested errors.
// Error возвращает фиксированное сообщение об исчерпании ожидания без сетевых имён, DSN или форматирования вложенных ошибок.
func (*connectionWaitFailure) Error() string { return "database connection wait exhausted" }

// Unwrap exposes both typed causes for internal errors.Is/As inspection, not for printing private details.
// Unwrap раскрывает обе типизированные причины для внутренних errors.Is/As, не для печати закрытых подробностей.
func (e *connectionWaitFailure) Unwrap() []error { return []error{e.cause, e.budget} }

// withReadyDatabase retries only classified transient ping failures under the unchanged caller context and runs apply once after success.
// Permanent failures, cancellation or exhausted budget stop before apply; an apply error is returned without restarting SQL.
// withReadyDatabase повторяет только классифицированные временные отказы ping в неизменном контексте и однократно запускает apply после успеха.
// Постоянный отказ, отмена или исчерпание бюджета останавливают вызов до apply; его ошибка возвращается без повторного SQL.
func withReadyDatabase(ctx context.Context, ping func(context.Context) error, apply func(context.Context) error, delay time.Duration) error {
	var last error
	for {
		if err := ctx.Err(); err != nil {
			return failure(stageDatabaseConnect, &connectionWaitFailure{cause: last, budget: err})
		}
		err := ping(ctx)
		if err == nil {
			if err = ctx.Err(); err != nil {
				return failure(stageDatabaseConnect, &connectionWaitFailure{cause: last, budget: err})
			}
			return apply(ctx)
		}
		if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			if last == nil {
				last = err
			}
			return failure(stageDatabaseConnect, &connectionWaitFailure{cause: last, budget: ctx.Err()})
		}
		// Классификатор сначала исключает TLS/auth/configuration даже у joined error.
		if !retryableConnection(err) {
			return failure(stageDatabaseConnect, err)
		}
		last = err
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return failure(stageDatabaseConnect, &connectionWaitFailure{cause: last, budget: ctx.Err()})
		case <-timer.C:
		}
	}
}

// retryableConnection admits only the explicit transient DNS/network classes after the shared classifier excludes TLS, authentication and config errors.
// retryableConnection допускает только явные временные DNS/network-классы после исключения общим классификатором TLS, auth и config-ошибок.
func retryableConnection(err error) bool {
	// Общая диагностика знает permanent SQL/TLS/configuration до сетевых wrappers.
	switch failureClass(err) {
	case "dns_temporary", "dns_timeout", "connection_refused", "connection_reset", "host_unreachable", "network_unreachable", "network_timeout":
		return true
	default:
		return false
	}
}

// networkFailureClass maps typed DNS conditions and known syscall errors to fixed labels; arbitrary network error text is never emitted.
// networkFailureClass переводит типизированные состояния DNS и известные syscall-ошибки в фиксированные метки; произвольный текст сетевой ошибки не выводится.
func networkFailureClass(err error) string {
	return errorclass.Network(err)
}
