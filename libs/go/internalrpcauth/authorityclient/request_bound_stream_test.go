package authorityclient

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const boundStreamFixtureMethod = "/fixture.Files/Stream"

type boundStreamProof struct {
	digest  string
	calls   int
	failure error
}

func (proof *boundStreamProof) OperationID(method string) (string, bool) {
	return "fixture.stream", method == boundStreamFixtureMethod
}
func (proof *boundStreamProof) AuthorityProof(ctx context.Context, _, _ string) (string, string, error) {
	proof.calls++
	proof.digest, _ = RequestDigest(ctx)
	return "proof", "correlation", proof.failure
}

type boundStreamIssuer struct {
	api.AuthorizationIssuerServiceClient
	request *api.IssueAuthorizationContextRequest
}

func (issuer *boundStreamIssuer) IssueAuthorizationContext(_ context.Context, request *api.IssueAuthorizationContextRequest, _ ...grpc.CallOption) (*api.IssueAuthorizationContextResponse, error) {
	issuer.request = request
	return &api.IssueAuthorizationContextResponse{CompactJws: "authorization"}, nil
}

type boundStreamVerifier struct {
	api.AuthorizationVerifierServiceClient
	digest string
	calls  int
	replay bool
}

func (verifier *boundStreamVerifier) VerifyAuthorizationContext(_ context.Context, request *api.VerifyAuthorizationContextRequest, _ ...grpc.CallOption) (*api.VerifyAuthorizationContextResponse, error) {
	verifier.calls++
	if verifier.replay && verifier.calls > 1 {
		return nil, status.Error(codes.PermissionDenied, "replayed context")
	}
	if request.GetObservedFullMethod() != boundStreamFixtureMethod || request.GetObservedRequestDigestSha256() != verifier.digest || request.GetDownstreamPeer() == nil {
		return nil, status.Error(codes.PermissionDenied, "request binding mismatch")
	}
	return &api.VerifyAuthorizationContextResponse{Context: &api.VerifiedAuthorizationContext{Jti: "accepted"}}, nil
}

type boundStreamFixture struct {
	ctx      context.Context
	initial  proto.Message
	received bool
	sent     int
	closed   bool
}

func (stream *boundStreamFixture) Context() context.Context { return stream.ctx }
func (*boundStreamFixture) Header() (metadata.MD, error)    { return nil, nil }
func (*boundStreamFixture) Trailer() metadata.MD            { return nil }
func (*boundStreamFixture) SetHeader(metadata.MD) error     { return nil }
func (*boundStreamFixture) SendHeader(metadata.MD) error    { return nil }
func (*boundStreamFixture) SetTrailer(metadata.MD)          {}
func (stream *boundStreamFixture) CloseSend() error         { stream.closed = true; return nil }
func (stream *boundStreamFixture) SendMsg(any) error        { stream.sent++; return nil }
func (stream *boundStreamFixture) RecvMsg(message any) error {
	if stream.initial == nil || stream.received {
		return io.EOF
	}
	stream.received = true
	proto.Merge(message.(proto.Message), stream.initial)
	return nil
}

func streamFixtureDigest(t *testing.T, message proto.Message) string {
	t.Helper()
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func TestRequestBoundClientStreamIssuesProofBeforeOpeningAndSending(t *testing.T) {
	initial := wrapperspb.String("exact immutable source")
	proof, issuer := &boundStreamProof{}, &boundStreamIssuer{}
	opened := 0
	var underlying *boundStreamFixture
	interceptor := IssuerStreamClientInterceptor(issuer, proof, proof, boundStreamFixtureMethod)
	stream, err := interceptor(t.Context(), &grpc.StreamDesc{ServerStreams: true}, nil, boundStreamFixtureMethod,
		func(ctx context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
			opened++
			if proof.digest != streamFixtureDigest(t, initial) || issuer.request.GetRequestDigestSha256() != proof.digest {
				t.Error("stream opened without exact request proof")
			}
			outgoing, _ := metadata.FromOutgoingContext(ctx)
			if len(outgoing.Get(AuthorizationMetadata)) != 1 {
				t.Error("stream authorization context absent")
			}
			underlying = &boundStreamFixture{ctx: ctx}
			return underlying, nil
		})
	if err != nil || opened != 0 || proof.calls != 0 {
		t.Fatal("stream opened before the initial request")
	}
	if _, err := stream.Header(); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("headers started an unbound stream")
	}
	if err := stream.SendMsg(initial); err != nil {
		t.Fatal(err)
	}
	if opened != 1 || underlying.sent != 1 || proof.calls != 1 {
		t.Fatal("initial request cardinality differs")
	}
	if err := stream.SendMsg(wrapperspb.String("substitution")); status.Code(err) != codes.FailedPrecondition || underlying.sent != 1 {
		t.Fatal("second initial request was sent")
	}
	if err := stream.CloseSend(); err != nil || !underlying.closed {
		t.Fatal("stream request side did not close")
	}
	if err := stream.RecvMsg(&wrapperspb.StringValue{}); !errors.Is(err, io.EOF) || underlying.ctx.Err() == nil {
		t.Fatal("terminal stream was not released")
	}
}

func TestRequestBoundClientStreamDoesNotOpenAfterProofDenial(t *testing.T) {
	proof := &boundStreamProof{failure: status.Error(codes.PermissionDenied, "denied")}
	issuer := &boundStreamIssuer{}
	opened := false
	stream, err := IssuerStreamClientInterceptor(issuer, proof, proof, boundStreamFixtureMethod)(t.Context(), &grpc.StreamDesc{ServerStreams: true}, nil, boundStreamFixtureMethod,
		func(context.Context, *grpc.StreamDesc, *grpc.ClientConn, string, ...grpc.CallOption) (grpc.ClientStream, error) {
			opened = true
			return nil, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if stream.SendMsg(wrapperspb.String("source")) == nil || opened || issuer.request != nil || proof.calls != 1 {
		t.Fatal("proof denial opened a stream or retried an effect")
	}
}

func TestRequestBoundServerStreamVerifiesActualInitialMessageBeforeOwner(t *testing.T) {
	initial := wrapperspb.String("exact immutable source")
	certificate := testCertificate(t)
	base := peer.NewContext(t.Context(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{certificate}, VerifiedChains: [][]*x509.Certificate{{certificate}},
	}}})
	base = metadata.NewIncomingContext(base, metadata.Pairs(AuthorizationMetadata, "authorization"))
	for _, scenario := range []string{"exact", "changed request", "missing message", "no mtls", "no authorization", "duplicate authorization", "replay"} {
		t.Run(scenario, func(t *testing.T) {
			verifier := &boundStreamVerifier{digest: streamFixtureDigest(t, initial), replay: scenario == "replay"}
			ctx, message := base, proto.Clone(initial)
			switch scenario {
			case "changed request":
				message = wrapperspb.String("foreign source")
			case "missing message":
				message = nil
			case "no mtls":
				ctx = metadata.NewIncomingContext(t.Context(), metadata.Pairs(AuthorizationMetadata, "authorization"))
			case "no authorization":
				ctx = metadata.NewIncomingContext(base, metadata.MD{})
			case "duplicate authorization":
				ctx = metadata.NewIncomingContext(base, metadata.Pairs(AuthorizationMetadata, "authorization", AuthorizationMetadata, "other"))
			}
			ownerCalls := 0
			invoke := func() error {
				underlying := &boundStreamFixture{ctx: ctx, initial: message}
				return VerifierStreamServerInterceptor(verifier, boundStreamFixtureMethod)(nil, underlying,
					&grpc.StreamServerInfo{FullMethod: boundStreamFixtureMethod, IsServerStream: true}, func(_ any, stream grpc.ServerStream) error {
						if _, ok := VerifiedAuthorizationContext(stream.Context()); ok {
							t.Error("initial bytes were not verified before context publication")
						}
						if stream.SendMsg(wrapperspb.String("premature")) == nil || underlying.sent != 0 {
							t.Error("unverified stream exposed bytes")
						}
						var decoded wrapperspb.StringValue
						if err := stream.RecvMsg(&decoded); err != nil {
							return err
						}
						if _, ok := VerifiedAuthorizationContext(stream.Context()); !ok {
							t.Error("verified initial context missing")
						}
						ownerCalls++
						return stream.SendMsg(wrapperspb.String("bounded chunk"))
					})
			}
			err := invoke()
			if scenario == "exact" || scenario == "replay" {
				if err != nil || ownerCalls != 1 {
					t.Fatalf("exact stream rejected: %v", err)
				}
				if scenario == "replay" && (invoke() == nil || ownerCalls != 1) {
					t.Fatal("replayed stream reached owner")
				}
			} else if err == nil || ownerCalls != 0 {
				t.Fatal("unverified stream reached owner")
			}
		})
	}
}
