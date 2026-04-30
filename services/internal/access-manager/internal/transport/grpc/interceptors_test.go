package grpc

import (
	"context"
	"io"
	"log/slog"
	"testing"

	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
)

func TestUnaryErrorInterceptorMapsDomainErrors(t *testing.T) {
	t.Parallel()

	cases := map[error]codes.Code{
		errs.ErrInvalidArgument:     codes.InvalidArgument,
		errs.ErrUnauthorizedSubject: codes.Unauthenticated,
		errs.ErrForbidden:           codes.PermissionDenied,
		errs.ErrNotFound:            codes.NotFound,
		errs.ErrAlreadyExists:       codes.AlreadyExists,
		errs.ErrConflict:            codes.Aborted,
		errs.ErrPreconditionFailed:  codes.FailedPrecondition,
	}
	interceptor := UnaryErrorInterceptor(discardLogger())
	for inputErr, expectedCode := range cases {
		inputErr := inputErr
		expectedCode := expectedCode
		t.Run(expectedCode.String(), func(t *testing.T) {
			t.Parallel()

			_, err := interceptor(context.Background(), nil, unaryInfo(), func(context.Context, any) (any, error) {
				return nil, inputErr
			})
			if status.Code(err) != expectedCode {
				t.Fatalf("code = %s, want %s", status.Code(err), expectedCode)
			}
		})
	}
}

func TestUnaryRecoveryInterceptorMapsPanic(t *testing.T) {
	t.Parallel()

	_, err := UnaryRecoveryInterceptor(discardLogger())(context.Background(), nil, unaryInfo(), func(context.Context, any) (any, error) {
		panic("boom")
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %s, want internal", status.Code(err))
	}
}

func unaryInfo() *grpcruntime.UnaryServerInfo {
	return &grpcruntime.UnaryServerInfo{FullMethod: "/kodex.access_accounts.v1.AccessManagerService/Test"}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
