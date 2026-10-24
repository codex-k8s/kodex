package grpc

import (
	"context"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestTransportErrorDistinguishesMissingCapabilityFromConcurrentConflict checks capability,
// already-resolved and in-use failures map to FailedPrecondition while concurrent conflict maps to
// Aborted.
// TestTransportErrorDistinguishesMissingCapabilityFromConcurrentConflict проверяет
// FailedPrecondition для отсутствующей capability, уже принятого решения и занятого ресурса, а
// Aborted — для конкурентного конфликта.
func TestTransportErrorDistinguishesMissingCapabilityFromConcurrentConflict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		code codes.Code
	}{
		{name: "missing capability", err: errs.ErrCapabilityRequired, code: codes.FailedPrecondition},
		{name: "already resolved", err: errs.ErrAlreadyResolved, code: codes.FailedPrecondition},
		{name: "resource in use", err: errs.ErrResourceInUse, code: codes.FailedPrecondition},
		{name: "concurrent conflict", err: errs.ErrConflict, code: codes.Aborted},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if actual := status.Code(transportError(test.err)); actual != test.code {
				t.Fatalf("transport code = %s, want %s", actual, test.code)
			}
		})
	}
}

// TestTransportErrorMarksResourceInUse checks in-use failures preserve FailedPrecondition and the
// closed resource-in-use reason.
// TestTransportErrorMarksResourceInUse проверяет сохранение FailedPrecondition и закрытой причины
// resource-in-use для занятого ресурса.
func TestTransportErrorMarksResourceInUse(t *testing.T) {
	t.Parallel()

	actual := transportError(errs.ErrResourceInUse)
	if status.Code(actual) != codes.FailedPrecondition {
		t.Fatalf("resource in use code = %s, want %s", status.Code(actual), codes.FailedPrecondition)
	}
	if reason := errorInfoReason(actual); reason != resourceInUseReason {
		t.Fatalf("resource in use reason = %q, want %q", reason, resourceInUseReason)
	}
}

// TestTransportErrorPreservesRequestCancellation checks caller cancellation and deadline failures
// retain their distinct Canceled and DeadlineExceeded transport codes.
// TestTransportErrorPreservesRequestCancellation проверяет сохранение отдельных transport codes
// Canceled и DeadlineExceeded для отмены и истечения срока caller.
func TestTransportErrorPreservesRequestCancellation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		err  error
		code codes.Code
	}{
		{name: "canceled", err: context.Canceled, code: codes.Canceled},
		{name: "deadline", err: context.DeadlineExceeded, code: codes.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if actual := status.Code(transportError(test.err)); actual != test.code {
				t.Fatalf("transport code = %s, want %s", actual, test.code)
			}
		})
	}
}

// TestTransportErrorPreservesAuthenticationBoundary checks ordinary unauthenticated failure has no
// fresh-auth reason while fresh-auth rejection is PermissionDenied with the exact reason.
// TestTransportErrorPreservesAuthenticationBoundary проверяет отсутствие fresh-auth reason у
// обычного unauthenticated-отказа и PermissionDenied с точной причиной у требования свежего входа.
func TestTransportErrorPreservesAuthenticationBoundary(t *testing.T) {
	t.Parallel()

	unauthenticated := transportError(errs.ErrUnauthorized)
	if actual := status.Code(unauthenticated); actual != codes.Unauthenticated {
		t.Fatalf("ordinary authentication code = %s, want %s", actual, codes.Unauthenticated)
	}
	if actual := errorInfoReason(unauthenticated); actual != "" {
		t.Fatalf("ordinary authentication unexpectedly contains reason %q", actual)
	}

	freshAuthentication := transportError(errs.ErrFreshAuthenticationRequired)
	if actual := status.Code(freshAuthentication); actual != codes.PermissionDenied {
		t.Fatalf("fresh authentication code = %s, want %s", actual, codes.PermissionDenied)
	}
	if actual := errorInfoReason(freshAuthentication); actual != freshAuthenticationRequiredReason {
		t.Fatalf("fresh authentication reason = %q, want %q", actual, freshAuthenticationRequiredReason)
	}
}

// errorInfoReason returns the first ErrorInfo reason in the control-plane domain, ignoring other
// detail types and domains.
// errorInfoReason возвращает первую причину ErrorInfo домена control-plane, пропуская другие типы
// деталей и домены.
func errorInfoReason(err error) string {
	for _, detail := range status.Convert(err).Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok && info.GetDomain() == controlPlaneErrorDomain {
			return info.GetReason()
		}
	}
	return ""
}
