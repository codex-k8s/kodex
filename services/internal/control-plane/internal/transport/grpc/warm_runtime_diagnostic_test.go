package grpc

import (
	"context"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"google.golang.org/grpc/status"
)

// TestWarmRuntimeDiagnosticDoesNotChangePublicErrorStatus checks adding a warm-runtime diagnostic
// stage preserves the public status code and message for owner errors and context termination.
// TestWarmRuntimeDiagnosticDoesNotChangePublicErrorStatus проверяет сохранение публичных кода и
// сообщения status при добавлении warm-runtime stage к ошибкам владельца и завершению context.
func TestWarmRuntimeDiagnosticDoesNotChangePublicErrorStatus(t *testing.T) {
	for _, original := range []error{errs.ErrConflict, errs.ErrVersionMismatch, errs.ErrUnavailable, errs.ErrForbidden, errs.ErrUnauthorized, errs.ErrInvalid, context.Canceled, context.DeadlineExceeded} {
		wrapped := errs.WithWarmRuntimeStage(original, errs.WarmReconcileCatalog)
		before, after := status.Convert(transportError(original)), status.Convert(transportError(wrapped))
		if before.Code() != after.Code() || before.Message() != after.Message() {
			t.Fatal("diagnostic changed the public authority/error boundary")
		}
	}
}
