package gateway

import (
	"context"
	"io"
	"mime"
	"net"
	"net/http"
	"time"

	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

func providerResponsesIsSSE(diagnostic providerResponsesDiagnostic, response *http.Response) bool {
	if !diagnostic.enabled() || response.StatusCode != http.StatusOK || len(response.Header.Values("Content-Type")) != 1 {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	return err == nil && mediaType == "text/event-stream"
}

func writeProxyHTTPResponse(connection net.Conn, response *http.Response, limits policy.Limits, streaming bool, ctx context.Context, cancel context.CancelCauseFunc) error {
	if streaming {
		response.Body = proxyIdleResponseBody{ReadCloser: response.Body, ctx: ctx, cancel: cancel, timeout: duration(limits.IdleTimeoutMilliseconds)}
		return response.Write(proxyDeadlineWriter{connection: connection, timeout: duration(limits.WriteTimeoutMilliseconds)})
	}
	if err := connection.SetWriteDeadline(time.Now().Add(duration(limits.WriteTimeoutMilliseconds))); err != nil {
		return err
	}
	return response.Write(connection)
}

// Каждый Write ограничен прежним бюджетом; весь активный SSE не имеет
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
