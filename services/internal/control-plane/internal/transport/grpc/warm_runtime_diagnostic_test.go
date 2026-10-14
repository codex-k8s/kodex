package grpc

import (
	"context"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"google.golang.org/grpc/status"
)

func TestWarmRuntimeDiagnosticDoesNotChangePublicErrorStatus(t *testing.T) {
	for _, original := range []error{errs.ErrConflict, errs.ErrVersionMismatch, errs.ErrUnavailable, errs.ErrForbidden, errs.ErrUnauthorized, errs.ErrInvalid, context.Canceled, context.DeadlineExceeded} {
		wrapped := errs.WithWarmRuntimeStage(original, errs.WarmReconcileCatalog)
		before, after := status.Convert(transportError(original)), status.Convert(transportError(wrapped))
		if before.Code() != after.Code() || before.Message() != after.Message() {
			t.Fatal("diagnostic changed the public authority/error boundary")
		}
	}
}
