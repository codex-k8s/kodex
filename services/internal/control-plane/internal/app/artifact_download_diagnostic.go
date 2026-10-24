package app

import (
	"context"
	"log/slog"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/observability/artifactread"
	"google.golang.org/grpc/codes"
)

const (
	// unexpectedGRPCStreamFailureMessage provides the fixed stream-error observer message without
	// interpolating raw cause or request content.
	// unexpectedGRPCStreamFailureMessage задаёт фиксированное сообщение stream error observer без
	// подстановки raw cause либо request content.
	unexpectedGRPCStreamFailureMessage = "unexpected gRPC stream failure"
	// artifactReadStageKey names the structured safe owner-download stage field populated only from
	// the typed diagnostic projection.
	// artifactReadStageKey задаёт имя структурированного поля безопасного owner download stage только
	// из типизированной diagnostic проекции.
	artifactReadStageKey = "safe_stage"
	// artifactReadClassKey names the closed artifact-read error classification field rather than an
	// arbitrary dependency error string.
	// artifactReadClassKey задаёт имя поля закрытой классификации artifact read ошибки вместо
	// произвольной строки ошибки зависимости.
	artifactReadClassKey = "error_class"
	// artifactReadSQLStateKey names the normalized SQLSTATE projection field, with unknown driver
	// values collapsed by the diagnostic type.
	// artifactReadSQLStateKey задаёт имя поля нормализованной SQLSTATE проекции с объединением
	// неизвестных driver значений diagnostic типом.
	artifactReadSQLStateKey = "sqlstate"
	// artifactReadAttemptKey names the bounded owner-read attempt counter in the structured download
	// failure observation.
	// artifactReadAttemptKey задаёт имя поля ограниченного счётчика попыток owner read в
	// структурированном наблюдении отказа скачивания.
	artifactReadAttemptKey = "attempt"
	// artifactReadExhaustedKey names the boolean retry-budget exhaustion field without authorizing
	// another attempt or changing owner state.
	// artifactReadExhaustedKey задаёт имя bool поля исчерпания retry бюджета без допуска новой попытки
	// либо изменения owner state.
	artifactReadExhaustedKey = "retry_exhausted"
)

// logUnexpectedGRPCStreamFailure logs the supplied interceptor method/code and adds only typed
// closed diagnostic fields for exact DownloadArtifactRevision; raw errors, correlation and request
// values are not copied.
// logUnexpectedGRPCStreamFailure записывает переданные interceptor method и code и добавляет
// только типизированные закрытые diagnostic поля точного DownloadArtifactRevision; raw errors,
// correlation и request значения не копируются.
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
