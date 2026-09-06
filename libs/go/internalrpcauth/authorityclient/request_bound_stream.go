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

const requestBoundStreamNotStarted = "request-bound stream has no authorized initial request"

func requestBoundStreamMethod(method string, methods []string) bool {
	for _, candidate := range methods {
		if candidate != "" && candidate == method {
			return true
		}
	}
	return false
}

// Один initial request проверяется до открытия stream. Generated server-stream
// client выполняет SendMsg и CloseSend до возврата handle вызывающей стороне.
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

func newRequestBoundClientStream(ctx context.Context, description *grpc.StreamDesc, connection *grpc.ClientConn, method string,
	streamer grpc.Streamer, options []grpc.CallOption, issuer api.AuthorizationIssuerServiceClient, operations OperationResolver, proofs ProofProvider,
) (grpc.ClientStream, error) {
	if description == nil || description.ClientStreams || !description.ServerStreams {
		return nil, status.Error(codes.Internal, "request-bound server stream descriptor is invalid")
	}
	return &requestBoundClientStream{ctx: ctx, description: description, connection: connection, method: method,
		streamer: streamer, options: options, issuer: issuer, operations: operations, proofs: proofs}, nil
}

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

func (stream *requestBoundClientStream) Header() (metadata.MD, error) {
	if stream.stream == nil {
		return nil, status.Error(codes.FailedPrecondition, requestBoundStreamNotStarted)
	}
	return stream.stream.Header()
}

func (stream *requestBoundClientStream) Trailer() metadata.MD {
	if stream.stream == nil {
		return nil
	}
	return stream.stream.Trailer()
}

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

func (stream *requestBoundClientStream) Context() context.Context { return stream.ctx }

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

type requestBoundServerStream struct {
	grpc.ServerStream
	ctx                context.Context
	verifier           api.AuthorizationVerifierServiceClient
	transport          *api.DownstreamTransportPeer
	compact, method    string
	received, verified bool
}

func (stream *requestBoundServerStream) Context() context.Context { return stream.ctx }

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

func (stream *requestBoundServerStream) SendMsg(message any) error {
	if !stream.verified {
		return status.Error(codes.Unauthenticated, requestBoundStreamNotStarted)
	}
	return stream.ServerStream.SendMsg(message)
}

func (stream *requestBoundServerStream) SendHeader(header metadata.MD) error {
	if !stream.verified {
		return status.Error(codes.Unauthenticated, requestBoundStreamNotStarted)
	}
	return stream.ServerStream.SendHeader(header)
}
