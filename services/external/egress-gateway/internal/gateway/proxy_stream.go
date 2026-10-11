package gateway

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

func writeProxyHTTPResponse(connection net.Conn, response *http.Response, limits policy.Limits, ctx context.Context, cancel context.CancelCauseFunc) error {
	if response.Body != nil && response.Body != http.NoBody {
		response.Body = proxyIdleResponseBody{ReadCloser: response.Body, ctx: ctx, cancel: cancel, timeout: duration(limits.IdleTimeoutMilliseconds)}
	}
	return response.Write(proxyDeadlineWriter{connection: connection, timeout: duration(limits.WriteTimeoutMilliseconds)})
}

// Каждый Write ограничен прежним бюджетом; весь активный HTTP body не имеет
// ложного общего write deadline. Idle чтение ограничено отдельно.
type proxyDeadlineWriter struct {
	connection net.Conn
	timeout    time.Duration
}

func (writer proxyDeadlineWriter) Write(value []byte) (int, error) {
	if err := writer.connection.SetWriteDeadline(time.Now().Add(writer.timeout)); err != nil {
		return 0, err
	}
	return writer.connection.Write(value)
}

type proxyIdleResponseBody struct {
	io.ReadCloser
	ctx     context.Context
	cancel  context.CancelCauseFunc
	timeout time.Duration
}

func (body proxyIdleResponseBody) Read(value []byte) (int, error) {
	timer := time.AfterFunc(body.timeout, func() { body.cancel(context.DeadlineExceeded) })
	n, err := body.ReadCloser.Read(value)
	timer.Stop()
	if err != nil && context.Cause(body.ctx) == context.DeadlineExceeded {
		return n, context.DeadlineExceeded
	}
	return n, err
}
