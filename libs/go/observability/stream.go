package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type correlationContextKey struct{}

type contextServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (stream *contextServerStream) Context() context.Context { return stream.ctx }

// StreamCorrelationServerInterceptor назначает один server-owned correlation
// UUID всему streaming RPC. Transport metadata и payload не могут его задать.
func StreamCorrelationServerInterceptor() grpc.StreamServerInterceptor {
	return func(
		service any,
		stream grpc.ServerStream,
		_ *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		correlationID, err := newCorrelationID()
		if err != nil {
			return status.Error(codes.Internal, "generate gRPC correlation ID")
		}
		ctx := context.WithValue(stream.Context(), correlationContextKey{}, correlationID)
		return handler(service, &contextServerStream{ServerStream: stream, ctx: ctx})
	}
}

// CorrelationID возвращает назначенный общим interceptor идентификатор.
func CorrelationID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	correlationID, _ := ctx.Value(correlationContextKey{}).(string)
	return correlationID
}

func newCorrelationID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", errors.New("read correlation entropy")
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], raw[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], raw[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], raw[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], raw[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], raw[10:16])
	return string(encoded), nil
}
