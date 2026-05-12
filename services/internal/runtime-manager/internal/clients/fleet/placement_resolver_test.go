package fleet

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMapFleetError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		code codes.Code
		want error
	}{
		{name: "invalid argument", code: codes.InvalidArgument, want: errs.ErrInvalidArgument},
		{name: "forbidden", code: codes.PermissionDenied, want: errs.ErrForbidden},
		{name: "rejected", code: codes.FailedPrecondition, want: errs.ErrPlacementRejected},
		{name: "missing dependency state", code: codes.NotFound, want: errs.ErrPreconditionFailed},
		{name: "conflict", code: codes.Aborted, want: errs.ErrConflict},
		{name: "unavailable", code: codes.Unavailable, want: errs.ErrDependencyUnavailable},
	}
	for _, item := range cases {
		item := item
		t.Run(item.name, func(t *testing.T) {
			t.Parallel()

			got := mapFleetError(status.Error(item.code, item.name))
			if !errors.Is(got, item.want) {
				t.Fatalf("mapFleetError(%s) = %v, want %v", item.code, got, item.want)
			}
		})
	}
}
