package grpc

import (
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
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
