package httpserver

import "context"

// Worker adapts Serve to a caller-owned worker context without changing listener ownership.
// RU: Worker подключает Serve к контексту работника, не меняя владельца listener.
// Calling Worker starts nothing; running the returned function starts Serve and waits for its result or cancellation.
// RU: Вызов Worker ничего не запускает; возвращённая функция запускает Serve и ждёт его результата либо отмены.
// Cancellation returns the context error without closing or joining Serve; the composition root must separately call Shutdown.
// RU: Отмена возвращает ошибку контекста без закрытия и ожидания Serve; composition root обязан отдельно вызвать Shutdown.
func (server *Server) Worker() func(context.Context) error {
	return func(ctx context.Context) error {
		done := make(chan error, 1)
		go func() { done <- server.Serve() }()
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
