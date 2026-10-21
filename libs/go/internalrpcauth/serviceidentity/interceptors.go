package serviceidentity

import (
	"context"

	"google.golang.org/grpc"
)

// admissionKey keeps transport admission in a private context slot so caller
// metadata cannot forge the value observed by downstream handlers.
// RU: admissionKey хранит транспортный допуск в закрытом ключе контекста,
// чтобы metadata вызывающего не подделали значение для обработчиков.
type admissionKey struct{}

// FromContext returns only admission installed by a server interceptor;
// resource ownership and the domain principal remain separate checks.
// RU: FromContext возвращает лишь допуск серверного interceptor; владение
// ресурсом и доменный principal проверяются отдельно.
func FromContext(ctx context.Context) (Admission, bool) {
	admission, ok := ctx.Value(admissionKey{}).(Admission)
	return admission, ok
}

// UnaryServerInterceptor admits a unary RPC before its handler receives the
// request and places the server-owned transport decision in its context.
// RU: UnaryServerInterceptor допускает unary RPC до вызова обработчика и
// помещает серверное транспортное решение в его контекст.
func (authorizer *Authorizer) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return unaryAdmissionInterceptor(authorizer.Admit)
}

// unaryAdmissionInterceptor shares only the unary admission-to-handler
// sequence; each server profile still supplies its own authority decision.
// RU: unaryAdmissionInterceptor объединяет лишь последовательность допуска
// unary-вызова; решение о полномочиях каждый серверный профиль принимает сам.
func unaryAdmissionInterceptor(admit func(context.Context, string) (Admission, error)) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		admission, err := admit(ctx, info.FullMethod)
		if err != nil {
			return nil, err
		}
		return handler(context.WithValue(ctx, admissionKey{}, admission), request)
	}
}

// StreamServerInterceptor admits a stream before the handler starts and wraps
// message operations so certificate validity and revocation are rechecked.
// RU: StreamServerInterceptor допускает поток до запуска обработчика и
// оборачивает сообщения для повторной проверки сертификата и отзыва.
func (authorizer *Authorizer) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		admission, err := authorizer.Admit(stream.Context(), info.FullMethod)
		if err != nil {
			return err
		}
		return handler(server, &admittedStream{ServerStream: stream, ctx: context.WithValue(stream.Context(), admissionKey{}, admission), authorizer: authorizer, method: info.FullMethod})
	}
}

// admittedStream carries the initial admission and authorizer for a protected
// stream while retaining the underlying gRPC transport behavior.
// RU: admittedStream хранит начальный допуск и authorizer защищённого потока,
// сохраняя поведение нижележащего gRPC-транспорта.
type admittedStream struct {
	grpc.ServerStream
	ctx        context.Context
	authorizer *Authorizer
	method     string
}

// Context exposes the server-installed admission to the stream handler,
// without accepting authority values from incoming stream messages.
// RU: Context передаёт обработчику серверный допуск, не принимая полномочия
// из входящих сообщений потока.
func (stream *admittedStream) Context() context.Context { return stream.ctx }

// RecvMsg rechecks peer admission before and after the blocking read so a
// revoked or expired peer cannot deliver a newly received message to handler.
// RU: RecvMsg повторно проверяет допуск до и после блокирующего чтения, чтобы
// отозванный или просроченный peer не передал сообщение обработчику.
func (stream *admittedStream) RecvMsg(message any) error {
	if _, err := stream.authorizer.Admit(stream.ctx, stream.method); err != nil {
		return err
	}
	if err := stream.ServerStream.RecvMsg(message); err != nil {
		return err
	}
	_, err := stream.authorizer.Admit(stream.ctx, stream.method)
	return err
}

// SendMsg rechecks current peer admission immediately before forwarding an
// outgoing stream message to the underlying gRPC transport.
// RU: SendMsg повторно проверяет текущий допуск peer непосредственно перед
// передачей исходящего сообщения нижележащему gRPC-транспорту.
func (stream *admittedStream) SendMsg(message any) error {
	if _, err := stream.authorizer.Admit(stream.ctx, stream.method); err != nil {
		return err
	}
	return stream.ServerStream.SendMsg(message)
}
