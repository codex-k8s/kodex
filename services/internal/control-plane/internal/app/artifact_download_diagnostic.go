package app

import (
	"context"
	"log/slog"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/observability/artifactread"
	"google.golang.org/grpc/codes"
)

const (
	unexpectedGRPCStreamFailureMessage = "unexpected gRPC stream failure"
	artifactReadStageKey               = "safe_stage"
	artifactReadClassKey               = "error_class"
	artifactReadSQLStateKey            = "sqlstate"
	artifactReadAttemptKey             = "attempt"
	artifactReadExhaustedKey           = "retry_exhausted"
)

// Exact method назначает server interceptor, а не request payload. Новые поля
// принадлежат только закрытой owner DOWNLOAD проекции; correlation не добавляется.
func logUnexpectedGRPCStreamFailure(ctx context.Context, method string, code codes.Code, err error) {
	attributes := []slog.Attr{slog.String("method", method), slog.String("code", code.String())}
	if method == controlplanev1.PlatformCommandService_DownloadArtifactRevision_FullMethodName {
		if diagnostic, ok := artifactread.From(err); ok {
			attributes = append(attributes,
				slog.String(artifactReadStageKey, diagnostic.Stage),
				slog.String(artifactReadClassKey, diagnostic.Class),
				slog.String(artifactReadSQLStateKey, diagnostic.SQLState),
				slog.Int(artifactReadAttemptKey, diagnostic.Attempt),
				slog.Bool(artifactReadExhaustedKey, diagnostic.RetryExhausted),
			)
		}
	}
	slog.LogAttrs(ctx, slog.LevelError, unexpectedGRPCStreamFailureMessage, attributes...)
}
