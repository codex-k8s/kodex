package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"

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
// sanitized error to telemetry. A typed SDK failure contributes its existing normalized enums;
// raw causes, correlations and arbitrary diagnostic interfaces never enter either sink.
// It does not retry a command, mint authority or alter the worker's cancel/join behavior.
// reportBuildCycleDegraded записывает прежний переход с одним закрытым code и отправляет новую
// безопасную error в telemetry. Typed SDK failure добавляет уже нормализованные enums;
// исходные causes, correlation и произвольные diagnostic interfaces в sinks не попадают;
// команда не повторяется, authority не создаётся, cancel/join worker не меняется.
func reportBuildCycleDegraded(ctx context.Context, logger *slog.Logger, capture func(context.Context, error), err error) {
	code := buildCycleRPCCode(err).String()
	attributes := []any{"error_class", "control_plane_or_buildkit", "rpc_code", code}
	message := buildCycleDegradedMessage + ": " + code
	var local *authorityclient.LocalAuthorityError
	if errors.As(err, &local) {
		diagnostic := local.Diagnostic()
		attributes = append(attributes, "authority_stage", string(diagnostic.Stage),
			"dependency_code", diagnostic.Code.String(), "authority_reason", diagnostic.Reason.String(),
			"authority_failure_stage", diagnostic.AuthorityStage.String())
		message += " [" + diagnostic.String() + "]"
	}
	logger.WarnContext(ctx, buildCycleDegradedMessage, attributes...)
	capture(ctx, errors.New(message))
}
