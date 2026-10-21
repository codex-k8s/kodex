package authorityclient

import (
	"context"
	"errors"
	"io"

	api "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// requestBoundStreamNotStarted is the bounded precondition text used before
// the first request has been authorized and the underlying stream exists.
// RU: requestBoundStreamNotStarted — ограниченный текст отказа до допуска
// первого запроса и создания нижележащего stream.
const requestBoundStreamNotStarted = "request-bound stream has no authorized initial request"

// requestBoundStreamMethod selects only an exact, nonempty full method from
// the caller's explicitly request-bound stream profile.
// RU: requestBoundStreamMethod выбирает только точный непустой полный метод
// из явно заданного caller профиля stream с привязкой к запросу.
func requestBoundStreamMethod(method string, methods []string) bool {
	for _, candidate := range methods {
		if candidate != "" && candidate == method {
			return true
		}
	}
	return false
}

// requestBoundClientStream delays opening the RPC until its one initial
// request has a digest-bound proof and an issued authorization context. The
// generated server-stream client sends that request before exposing the handle.
// RU: requestBoundClientStream откладывает открытие RPC, пока единственный
// первичный запрос не получит привязанный к digest proof и выпущенный контекст
// авторизации. Generated client отправляет запрос до выдачи stream handle.
type requestBoundClientStream struct {
	ctx         context.Context
	cancel      context.CancelFunc
	description *grpc.StreamDesc
	connection  *grpc.ClientConn
	method      string
	streamer    grpc.Streamer
	options     []grpc.CallOption
	issuer      api.AuthorizationIssuerServiceClient
	operations  OperationResolver
	proofs      ProofProvider
	stream      grpc.ClientStream
	sent        bool
}

// newRequestBoundClientStream accepts only a server-stream descriptor and
// holds the exact operation dependencies until SendMsg authorizes the request.
// RU: newRequestBoundClientStream принимает только дескриптор server-stream
// и сохраняет точные зависимости операции до допуска запроса в SendMsg.
func newRequestBoundClientStream(ctx context.Context, description *grpc.StreamDesc, connection *grpc.ClientConn, method string,
	streamer grpc.Streamer, options []grpc.CallOption, issuer api.AuthorizationIssuerServiceClient, operations OperationResolver, proofs ProofProvider,
) (grpc.ClientStream, error) {
	if description == nil || description.ClientStreams || !description.ServerStreams {
		return nil, status.Error(codes.Internal, "request-bound server stream descriptor is invalid")
	}
	return &requestBoundClientStream{ctx: ctx, description: description, connection: connection, method: method,
		streamer: streamer, options: options, issuer: issuer, operations: operations, proofs: proofs}, nil
}

// SendMsg permits one initial request, binds its digest to a proof, obtains
// an authorization context, and opens/sends on the stream only after issuance.
// A stream opened but failing to send is canceled before the error returns.
// RU: SendMsg допускает один первичный запрос, связывает его digest с proof,
// получает контекст авторизации и лишь затем открывает stream и отправляет
// запрос. При ошибке отправки уже открытый stream отменяется до возврата.
func (stream *requestBoundClientStream) SendMsg(request any) error {
	if stream.sent {
		return status.Error(codes.FailedPrecondition, "initial stream request already sent")
	}
	stream.sent = true
	operation, ok := stream.operations.OperationID(stream.method)
	if !ok {
		return status.Error(codes.PermissionDenied, "internal RPC operation is not registered")
	}
	bound, digest, err := bindRequestDigest(stream.ctx, request)
	if err != nil {
		return err
	}
	proof, correlation, err := authorityProofWithRetry(bound, stream.proofs, operation, stream.method)
	if err != nil {
		return newLocalAuthorityError(err, correlation, StageProofResolve)
	}
	issued, err := stream.issuer.IssueAuthorizationContext(bound, &api.IssueAuthorizationContextRequest{
		OperationId: operation, CorrelationId: correlation, AuthorityProofCompactJws: proof, RequestDigestSha256: digest,
	})
	if err != nil {
		return newLocalAuthorityError(err, correlation, StageContextIssue)
	}
	if issued.GetCompactJws() == "" {
		return status.Error(codes.Unauthenticated, "authorization context required")
	}
	bound = metadata.AppendToOutgoingContext(bound, AuthorizationMetadata, issued.GetCompactJws())
	stream.ctx, stream.cancel = context.WithCancel(bound)
	stream.stream, err = stream.streamer(stream.ctx, stream.description, stream.connection, stream.method, stream.options...)
	if err == nil {
		err = stream.stream.SendMsg(request)
	}
	if err != nil {
		stream.cancel()
	}
	return err
}

// Header reads response headers only after the authorized stream exists.
// RU: Header читает заголовки ответа только после создания допущенного stream.
func (stream *requestBoundClientStream) Header() (metadata.MD, error) {
	if stream.stream == nil {
		return nil, status.Error(codes.FailedPrecondition, requestBoundStreamNotStarted)
	}
	return stream.stream.Header()
}

// Trailer returns no metadata before the authorized stream is opened.
// RU: Trailer не возвращает метаданные до открытия допущенного stream.
func (stream *requestBoundClientStream) Trailer() metadata.MD {
	if stream.stream == nil {
		return nil
	}
	return stream.stream.Trailer()
}

// CloseSend closes the opened send side and cancels the bound context on error.
// RU: CloseSend закрывает сторону отправки открытого stream и при ошибке
// отменяет привязанный контекст.
func (stream *requestBoundClientStream) CloseSend() error {
	if stream.stream == nil {
		return status.Error(codes.FailedPrecondition, requestBoundStreamNotStarted)
	}
	err := stream.stream.CloseSend()
	if err != nil {
		stream.cancel()
	}
	return err
}

// Context exposes the original context before SendMsg and the cancelable,
// authorization-bound context once the stream has been opened.
// RU: Context возвращает исходный контекст до SendMsg и отменяемый контекст
// с привязкой к авторизации после открытия stream.
func (stream *requestBoundClientStream) Context() context.Context { return stream.ctx }

// RecvMsg refuses reception before authorization and cancels the bound
// context when the underlying stream ends or fails.
// RU: RecvMsg запрещает получение до авторизации и отменяет привязанный
// контекст при завершении либо ошибке нижележащего stream.
func (stream *requestBoundClientStream) RecvMsg(message any) error {
	if stream.stream == nil {
		return status.Error(codes.FailedPrecondition, requestBoundStreamNotStarted)
	}
	err := stream.stream.RecvMsg(message)
	if err != nil {
		stream.cancel()
	}
	return err
}

// requestBoundServerStream verifies the exact first request digest before
// exposing verified context or allowing any response to leave the server.
// RU: requestBoundServerStream проверяет digest точного первого запроса до
// выдачи проверенного контекста и разрешения любого ответа сервера.
type requestBoundServerStream struct {
	grpc.ServerStream
	ctx                context.Context
	verifier           api.AuthorizationVerifierServiceClient
	transport          *api.DownstreamTransportPeer
	compact, method    string
	received, verified bool
}

// Context returns the verified context after the first request succeeds;
// before that it exposes only the original transport context.
// RU: Context возвращает проверенный контекст после успешного первого
// запроса; до этого доступен только исходный транспортный контекст.
func (stream *requestBoundServerStream) Context() context.Context { return stream.ctx }

// RecvMsg accepts exactly one initial request, verifies its digest against
// the transport-bound context, and only then installs continuation provenance.
// RU: RecvMsg принимает ровно один первичный запрос, сверяет его digest с
// транспортно-привязанным контекстом и только затем устанавливает
// происхождение продолжения.
func (stream *requestBoundServerStream) RecvMsg(request any) error {
	if stream.received {
		return status.Error(codes.FailedPrecondition, "initial stream request already received")
	}
	stream.received = true
	if err := stream.ServerStream.RecvMsg(request); err != nil {
		if errors.Is(err, io.EOF) {
			return status.Error(codes.InvalidArgument, "initial stream request is required")
		}
		return err
	}
	_, digest, err := bindRequestDigest(stream.ctx, request)
	if err != nil {
		return err
	}
	verified, err := stream.verifier.VerifyAuthorizationContext(stream.ctx, &api.VerifyAuthorizationContextRequest{
		CompactJws: stream.compact, ObservedFullMethod: stream.method, DownstreamPeer: stream.transport, ObservedRequestDigestSha256: digest,
	})
	if err != nil {
		return err
	}
	if verified.GetContext() == nil {
		return status.Error(codes.Internal, "verified authorization context missing")
	}
	stream.ctx = context.WithValue(stream.ctx, verifiedContextKey{}, verified.GetContext())
	stream.ctx = context.WithValue(stream.ctx, continuationSourceKey{}, continuationSource{compact: stream.compact, verified: verified.GetContext()})
	stream.verified = true
	return nil
}

// SendMsg prevents response messages until first-request verification succeeds.
// RU: SendMsg запрещает отправку ответа до успешной проверки первого запроса.
func (stream *requestBoundServerStream) SendMsg(message any) error {
	if !stream.verified {
		return status.Error(codes.Unauthenticated, requestBoundStreamNotStarted)
	}
	return stream.ServerStream.SendMsg(message)
}

// SendHeader prevents response headers until first-request verification succeeds.
// RU: SendHeader запрещает отправку заголовков до проверки первого запроса.
func (stream *requestBoundServerStream) SendHeader(header metadata.MD) error {
	if !stream.verified {
		return status.Error(codes.Unauthenticated, requestBoundStreamNotStarted)
	}
	return stream.ServerStream.SendHeader(header)
}
