package callback

import (
	"context"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// terminalProjectionRetryDelay bounds the single transport retry without extending the caller deadline.
// terminalProjectionRetryDelay ограничивает единственный transport retry без продления срока caller.
const terminalProjectionRetryDelay = 100 * time.Millisecond

// recordToolProjection retries only an unchanged terminal activity receipt after a transient RPC failure.
// It never dispatches the tool, repeats an external effect or replays the initial authorizing phase.
// A missing event is still rejected by the caller; permanent/authority failures remain closed.
// recordToolProjection повторяет только неизменный terminal activity receipt после transient RPC failure.
// Инструмент и внешний effect не исполняются повторно; initial authorizing phase не повторяется.
// Caller по-прежнему отклоняет пустой event; permanent/authority failures остаются закрытыми.
func (server *Server) recordToolProjection(ctx context.Context, request *controlplanev1.RecordRunToolCallRequest) (*controlplanev1.RecordRunToolCallResponse, error) {
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		bounded, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
		response, err := server.control.Runtime.RecordRunToolCall(bounded, request)
		cancel()
		if err == nil || request.GetRevision() != 2 || attempt == 1 ||
			(status.Code(err) != codes.Unavailable && status.Code(err) != codes.DeadlineExceeded) {
			return response, err
		}
		timer := time.NewTimer(terminalProjectionRetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, status.FromContextError(ctx.Err()).Err()
		case <-timer.C:
		}
	}
}
