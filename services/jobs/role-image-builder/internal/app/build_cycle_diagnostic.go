package app

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// buildCycleDegradedMessage is the fixed worker transition text shared by its log and sanitized
// telemetry projection. Provider messages, request bodies and credential material never enter it.
// buildCycleDegradedMessage задаёт постоянный текст перехода worker для журнала и безопасной
// telemetry projection. Provider messages, request bodies и credentials в него не входят.
const buildCycleDegradedMessage = "role image build delivery degraded"

// buildCycleRPCCode extracts only a canonical error code, including wrapped/joined RPC failures.
// Context errors supply a fallback; plain text and out-of-range codes cannot forge a diagnostic.
// buildCycleRPCCode извлекает лишь канонический error code, включая wrapped/joined RPC failures.
// Context errors дают fallback; текст ошибки и внеположенные codes не подделывают diagnostic.
func buildCycleRPCCode(err error) codes.Code {
	code := status.Code(err)
	if code == codes.Unknown {
		if errors.Is(err, context.DeadlineExceeded) {
			code = codes.DeadlineExceeded
		} else if errors.Is(err, context.Canceled) {
			code = codes.Canceled
		}
	}
	if code < codes.Canceled || code > codes.Unauthenticated {
		return codes.Unknown
	}
	return code
}

// reportBuildCycleDegraded emits the existing transition with one closed code and sends a fresh
// sanitized error to telemetry. It never formats or unwraps the original error into either sink,
// and does not retry a command, mint authority or alter the worker's cancel/join behavior.
// reportBuildCycleDegraded записывает прежний переход с одним закрытым code и отправляет новую
// безопасную error в telemetry. Исходная ошибка не форматируется и не раскрывается в этих sinks;
// команда не повторяется, authority не создаётся, cancel/join worker не меняется.
func reportBuildCycleDegraded(ctx context.Context, logger *slog.Logger, capture func(context.Context, error), err error) {
	code := buildCycleRPCCode(err).String()
	logger.WarnContext(ctx, buildCycleDegradedMessage, "error_class", "control_plane_or_buildkit", "rpc_code", code)
	capture(ctx, errors.New(buildCycleDegradedMessage+": "+code))
}
