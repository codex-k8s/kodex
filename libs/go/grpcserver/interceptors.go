package grpcserver

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ErrorObserver interface {
	ObserveUnexpected(context.Context, string, codes.Code, error)
}

type ErrorObserverFunc func(context.Context, string, codes.Code, error)

func (observer ErrorObserverFunc) ObserveUnexpected(
	ctx context.Context,
	method string,
	code codes.Code,
	err error,
) {
	observer(ctx, method, code, err)
}

func IsUnexpectedCode(code codes.Code) bool {
	switch code {
	case codes.Internal, codes.Unavailable, codes.Unknown, codes.DataLoss:
		return true
	default:
		return false
	}
}

func ErrorBoundary(observer ErrorObserver) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (response any, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = status.Error(codes.Internal, "internal server error")
				if observer != nil {
					observer.ObserveUnexpected(
						ctx,
						info.FullMethod,
						codes.Internal,
						fmt.Errorf("panic recovered"),
					)
				}
			}
		}()
		response, err = handler(ctx, request)
		if err != nil {
			code := status.Code(err)
			if observer != nil && IsUnexpectedCode(code) {
				observer.ObserveUnexpected(ctx, info.FullMethod, code, err)
			}
		}
		return response, err
	}
}
