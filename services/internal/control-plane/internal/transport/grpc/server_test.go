package grpc

import (
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTransportErrorDistinguishesMissingCapabilityFromConcurrentConflict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		code codes.Code
	}{
		{name: "missing capability", err: errs.ErrCapabilityRequired, code: codes.FailedPrecondition},
		{name: "already resolved", err: errs.ErrAlreadyResolved, code: codes.FailedPrecondition},
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

func errorInfoReason(err error) string {
	for _, detail := range status.Convert(err).Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok && info.GetDomain() == controlPlaneErrorDomain {
			return info.GetReason()
		}
	}
	return ""
}
