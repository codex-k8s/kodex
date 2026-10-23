package grpcserver

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RejectMalformedUnary rejects a unary request marked malformed by the strict
// codec before any application handler can consume its partial protobuf value.
// RU: RejectMalformedUnary отклоняет помеченный строгим codec запрос до того,
// как обработчик сможет использовать частично разобранный protobuf.
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

// RejectMalformedStream wraps streaming reads so every decoded message is
// checked before it reaches the application stream handler.
// RU: RejectMalformedStream оборачивает чтение потока и проверяет каждое
// декодированное сообщение до передачи обработчику приложения.
func RejectMalformedStream(
	service any,
	stream grpc.ServerStream,
	_ *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	return handler(service, &strictServerStream{ServerStream: stream})
}

// strictServerStream preserves the original server stream while intercepting
// inbound messages for strict protobuf validation.
// RU: strictServerStream сохраняет исходный поток сервера и перехватывает
// входящие сообщения для строгой проверки protobuf.
type strictServerStream struct{ grpc.ServerStream }

// RecvMsg delegates the read, then rejects unknown or malformed protobuf
// fields without exposing the decoded message to the handler.
// RU: RecvMsg читает сообщение через исходный поток и отклоняет неизвестные
// либо повреждённые поля до выдачи результата обработчику.
func (stream *strictServerStream) RecvMsg(message any) error {
	if err := stream.ServerStream.RecvMsg(message); err != nil {
		return err
	}
	if HasMalformedProto(message) {
		return status.Error(codes.InvalidArgument, "protobuf request is malformed")
	}
	return nil
}

// ErrorObserver receives one observation for an unexpected server-boundary
// failure, separate from the status returned to the client.
// RU: ErrorObserver получает одно наблюдение неожиданного сбоя на серверной
// границе отдельно от статуса, возвращаемого клиенту.
type ErrorObserver interface {
	// ObserveUnexpected records the method, closed code and original error once.
	// RU: ObserveUnexpected один раз фиксирует метод, закрытый код и исходную ошибку.
	ObserveUnexpected(context.Context, string, codes.Code, error)
}

// ErrorObserverFunc adapts a function to the ErrorObserver boundary.
// RU: ErrorObserverFunc приспосабливает функцию к границе ErrorObserver.
type ErrorObserverFunc func(context.Context, string, codes.Code, error)

// ObserveUnexpected forwards an unexpected failure to the wrapped observer
// function without adding another log or observer event.
// RU: ObserveUnexpected передаёт неожиданный сбой обёрнутой функции без
// создания дополнительного лога либо второго события наблюдения.
func (observer ErrorObserverFunc) ObserveUnexpected(
	ctx context.Context,
	method string,
	code codes.Code,
	err error,
) {
	observer(ctx, method, code, err)
}

// IsUnexpectedCode recognizes the closed gRPC status set that should reach
// the error observer: Internal, Unavailable, Unknown and DataLoss.
// RU: IsUnexpectedCode распознаёт закрытый набор кодов gRPC для наблюдения:
// Internal, Unavailable, Unknown и DataLoss.
func IsUnexpectedCode(code codes.Code) bool {
	switch code {
	case codes.Internal, codes.Unavailable, codes.Unknown, codes.DataLoss:
		return true
	default:
		return false
	}
}

// normalizeContextError replaces a late unexpected status with cancellation
// or deadline status when the request context already ended.
// RU: normalizeContextError заменяет поздний неожиданный статус отменой либо
// истечением срока, если контекст запроса уже завершился.
func normalizeContextError(ctx context.Context, err error) error {
	if err == nil || !IsUnexpectedCode(status.Code(err)) {
		return err
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return status.FromContextError(contextErr).Err()
	}
	return err
}

// ErrorBoundary recovers unary panics, normalizes completed contexts and
// reports each live unexpected error exactly once at the server boundary.
// RU: ErrorBoundary перехватывает panic unary RPC, учитывает завершённый
// контекст и сообщает о живой неожиданной ошибке ровно один раз.
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

// StreamErrorBoundary applies the same panic recovery, context normalization
// and single unexpected-error observation to streaming RPCs.
// RU: StreamErrorBoundary применяет тот же перехват panic, нормализацию
// контекста и однократное наблюдение ошибок к потоковым RPC.
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
