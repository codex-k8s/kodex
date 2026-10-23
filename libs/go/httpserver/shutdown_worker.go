package httpserver

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// ShutdownWorker adapts caller-owned HTTP serve/shutdown callbacks to a worker; construction starts nothing.
// When the worker context is canceled, shutdown receives its values but a fresh timeout without the parent cancellation/deadline,
// then the worker joins serve and combines errors except the normal http.ErrServerClosed result.
// Callbacks and timeout must already be valid; shutdown must cooperate with its context, and this worker does not join hijacked connections or handlers left active after timeout.
// ShutdownWorker подключает принадлежащие потребителю HTTP callbacks обслуживания/остановки к работнику; создание ничего не запускает.
// При отмене контекста работника остановка получает его значения, но свежий таймаут без родительской отмены/срока,
// затем работник дожидается возврата обслуживания и объединяет ошибки, кроме штатного результата http.ErrServerClosed.
// Callbacks и таймаут должны быть заранее корректны; остановка обязана соблюдать контекст, а работник не ожидает hijacked-соединения и обработчики, оставшиеся активными после таймаута.
func ShutdownWorker(serve func() error, shutdown func(context.Context) error, timeout time.Duration) func(context.Context) error {
	return func(ctx context.Context) error {
		done := make(chan error, 1)
		go func() { done <- serve() }()
		select {
		case err := <-done:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ctx.Done():
			shutdownContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
			defer cancel()
			err := shutdown(shutdownContext)
			serveErr := <-done
			if !errors.Is(serveErr, http.ErrServerClosed) {
				err = errors.Join(err, serveErr)
			}
			return err
		}
	}
}
