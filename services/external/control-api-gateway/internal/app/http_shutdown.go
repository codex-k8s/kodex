package app

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Сигнал процесса прекращает admission/listeners, но не отменяет уже принятый
// запрос до окончания bounded HTTP drain. Отмена клиента по-прежнему передаётся
// net/http через принадлежащий соединению дочерний контекст.
func servingContext(lifecycle context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(context.WithoutCancel(lifecycle))
}

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
