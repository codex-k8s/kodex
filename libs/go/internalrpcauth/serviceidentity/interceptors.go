package serviceidentity

import (
	"context"

	"google.golang.org/grpc"
)

type admissionKey struct{}

// FromContext возвращает только транспортный допуск, установленный серверным
// interceptor. Доменный principal и права ресурса здесь не создаются.
func FromContext(ctx context.Context) (Admission, bool) {
	admission, ok := ctx.Value(admissionKey{}).(Admission)
	return admission, ok
}

func (authorizer *Authorizer) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		admission, err := authorizer.Admit(ctx, info.FullMethod)
		if err != nil {
			return nil, err
		}
		return handler(context.WithValue(ctx, admissionKey{}, admission), request)
	}
}

func (authorizer *Authorizer) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		admission, err := authorizer.Admit(stream.Context(), info.FullMethod)
		if err != nil {
			return err
		}
		return handler(server, &admittedStream{ServerStream: stream, ctx: context.WithValue(stream.Context(), admissionKey{}, admission), authorizer: authorizer, method: info.FullMethod})
	}
}

type admittedStream struct {
	grpc.ServerStream
	ctx        context.Context
	authorizer *Authorizer
	method     string
}

func (stream *admittedStream) Context() context.Context { return stream.ctx }

// Давно открытый stream не сохраняет доступ после отзыва или истечения
// сертификата. Проверка после блокирующего Recv предшествует выдаче в handler.
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

func (stream *admittedStream) SendMsg(message any) error {
	if _, err := stream.authorizer.Admit(stream.ctx, stream.method); err != nil {
		return err
	}
	return stream.ServerStream.SendMsg(message)
}
