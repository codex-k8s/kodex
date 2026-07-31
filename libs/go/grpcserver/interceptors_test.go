package grpcserver

import (
	"testing"

	"google.golang.org/grpc/codes"
)

func TestUnexpectedCodesAreClosed(t *testing.T) {
	for _, code := range []codes.Code{
		codes.Internal,
		codes.Unavailable,
		codes.Unknown,
		codes.DataLoss,
	} {
		if !IsUnexpectedCode(code) {
			t.Fatalf("%s must be unexpected", code)
		}
	}
	for _, code := range []codes.Code{
		codes.InvalidArgument,
		codes.Unauthenticated,
		codes.PermissionDenied,
		codes.NotFound,
		codes.AlreadyExists,
		codes.FailedPrecondition,
		codes.Canceled,
	} {
		if IsUnexpectedCode(code) {
			t.Fatalf("%s must be expected", code)
		}
	}
}
