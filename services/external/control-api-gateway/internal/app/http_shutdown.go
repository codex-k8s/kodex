package app

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// servingContext preserves lifecycle values while detaching its cancellation/deadline, then adds an explicit serving cancel function. Process cancellation therefore does not abort accepted HTTP requests before drain; net/http still supplies connection/client cancellation.
// servingContext сохраняет lifecycle values, отделяя cancellation и deadline, затем добавляет явную serving cancel function. Отмена процесса не прерывает принятые HTTP requests до drain; connection и client cancellation по-прежнему предоставляет net/http.
func servingContext(lifecycle context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(context.WithoutCancel(lifecycle))
}

// shutdownHTTPServer creates an independent timeout from the uncancelled lifecycle view, attempts graceful Shutdown and joins a forced Close error if drain fails. Neither standard method owns hijacked WebSockets; their contexts are cancelled separately by Run.
// shutdownHTTPServer создаёт независимый timeout из неотменяемого представления lifecycle, пытается выполнить graceful Shutdown и добавляет ошибку принудительного Close при отказе drain. Ни один стандартный метод не владеет hijacked WebSockets; их contexts отдельно отменяет Run.
func shutdownHTTPServer(lifecycle context.Context, server *http.Server, budget time.Duration) error {
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(lifecycle), budget)
	defer cancel()
	err := server.Shutdown(shutdown)
	if err != nil {
		// Shutdown не закрывает активные соединения после timeout самостоятельно.
		// Close отменяет их request contexts до закрытия downstream клиентов.
		err = errors.Join(err, server.Close())
	}
	return err
}
