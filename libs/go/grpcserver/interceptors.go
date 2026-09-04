package grpcserver

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RejectMalformedUnary отклоняет payload, помеченный StrictProtoCodec.
func RejectMalformedUnary(
	ctx context.Context,
	request any,
	_ *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	if HasMalformedProto(request) {
		return nil, status.Error(codes.InvalidArgument, "protobuf request is malformed")
	}
	return handler(ctx, request)
}

// RejectMalformedStream проверяет каждое сообщение streaming RPC после decode.
func RejectMalformedStream(
	service any,
	stream grpc.ServerStream,
	_ *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	return handler(service, &strictServerStream{ServerStream: stream})
}

type strictServerStream struct{ grpc.ServerStream }

func (stream *strictServerStream) RecvMsg(message any) error {
	if err := stream.ServerStream.RecvMsg(message); err != nil {
		return err
	}
	if HasMalformedProto(message) {
		return status.Error(codes.InvalidArgument, "protobuf request is malformed")
	}
	return nil
}

// ErrorObserver получает неожиданные ошибки единожды на серверной границе.
type ErrorObserver interface {
	ObserveUnexpected(context.Context, string, codes.Code, error)
}

// ErrorObserverFunc адаптирует функцию к ErrorObserver.
type ErrorObserverFunc func(context.Context, string, codes.Code, error)

// ObserveUnexpected передаёт неожиданную ошибку функции-наблюдателю.
func (observer ErrorObserverFunc) ObserveUnexpected(
	ctx context.Context,
	method string,
	code codes.Code,
	err error,
) {
	observer(ctx, method, code, err)
}

// IsUnexpectedCode определяет закрытый набор неожиданных кодов gRPC.
func IsUnexpectedCode(code codes.Code) bool {
	switch code {
	case codes.Internal, codes.Unavailable, codes.Unknown, codes.DataLoss:
		return true
	default:
		return false
	}
}

func normalizeContextError(ctx context.Context, err error) error {
	if err == nil || !IsUnexpectedCode(status.Code(err)) {
		return err
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return status.FromContextError(contextErr).Err()
	}
	return err
}

// ErrorBoundary перехватывает panic и уведомляет о неожиданных ошибках один раз.
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
		err = normalizeContextError(ctx, err)
		if err != nil {
			code := status.Code(err)
			if observer != nil && IsUnexpectedCode(code) {
				observer.ObserveUnexpected(ctx, info.FullMethod, code, err)
			}
		}
		return response, err
	}
}

// StreamErrorBoundary применяет единую panic/error boundary к streaming RPC.
func StreamErrorBoundary(observer ErrorObserver) grpc.StreamServerInterceptor {
	return func(
		service any,
		stream grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = status.Error(codes.Internal, "internal server error")
				if observer != nil {
					observer.ObserveUnexpected(stream.Context(), info.FullMethod, codes.Internal, fmt.Errorf("panic recovered"))
				}
			}
		}()
		err = handler(service, stream)
		err = normalizeContextError(stream.Context(), err)
		if err != nil {
			code := status.Code(err)
			if observer != nil && IsUnexpectedCode(code) {
				observer.ObserveUnexpected(stream.Context(), info.FullMethod, code, err)
			}
		}
		return err
	}
}
