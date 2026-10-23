package gateway

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

// writeProxyHTTPResponse streams a provider response with separate idle-read and per-write budgets.
// writeProxyHTTPResponse передаёт ответ провайдера с отдельными лимитами простоя чтения и каждой записи.
func writeProxyHTTPResponse(connection net.Conn, response *http.Response, limits policy.Limits, ctx context.Context, cancel context.CancelCauseFunc) error {
	if response.Body != nil && response.Body != http.NoBody {
		response.Body = proxyIdleResponseBody{ReadCloser: response.Body, ctx: ctx, cancel: cancel, timeout: duration(limits.IdleTimeoutMilliseconds)}
	}
	return response.Write(proxyDeadlineWriter{connection: connection, timeout: duration(limits.WriteTimeoutMilliseconds)})
}

// proxyDeadlineWriter bounds each write without imposing one deadline on the entire active HTTP body.
// proxyDeadlineWriter ограничивает каждую запись без общего срока для всего активного HTTP-тела.
// Idle reads have an independent budget in proxyIdleResponseBody.
// Простой чтения отдельно ограничивает proxyIdleResponseBody.
type proxyDeadlineWriter struct {
	connection net.Conn
	timeout    time.Duration
}

// Write refreshes the destination write deadline immediately before forwarding the bytes.
// Write обновляет срок записи непосредственно перед передачей байтов в назначение.
func (writer proxyDeadlineWriter) Write(value []byte) (int, error) {
	if err := writer.connection.SetWriteDeadline(time.Now().Add(writer.timeout)); err != nil {
		return 0, err
	}
	return writer.connection.Write(value)
}

// proxyIdleResponseBody cancels the response context if one body read exceeds the idle budget.
// proxyIdleResponseBody отменяет контекст ответа при превышении времени простоя одного чтения тела.
type proxyIdleResponseBody struct {
	io.ReadCloser
	ctx     context.Context
	cancel  context.CancelCauseFunc
	timeout time.Duration
}

// Read returns a deadline error when its timer caused cancellation, preserving other read errors.
// Read возвращает ошибку срока при отмене таймером, сохраняя остальные ошибки чтения.
func (body proxyIdleResponseBody) Read(value []byte) (int, error) {
	timer := time.AfterFunc(body.timeout, func() { body.cancel(context.DeadlineExceeded) })
	n, err := body.ReadCloser.Read(value)
	timer.Stop()
	if err != nil && context.Cause(body.ctx) == context.DeadlineExceeded {
		return n, context.DeadlineExceeded
	}
	return n, err
}
