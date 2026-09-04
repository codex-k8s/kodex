package authorityclient

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net/url"
	"testing"
	"time"

	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeVerifier struct {
	response *internalrpcauthorityv1.VerifyAuthorizationContextResponse
	request  *internalrpcauthorityv1.VerifyAuthorizationContextRequest
}

type fakeIssuer struct {
	response             *internalrpcauthorityv1.IssueAuthorizationContextResponse
	continuationResponse *internalrpcauthorityv1.IssueAuthorizationContextResponse
	continuationRequest  *internalrpcauthorityv1.IssueContinuationAuthorizationContextRequest
	calls                int
}

type fakeOperationResolver map[string]string

func (resolver fakeOperationResolver) OperationID(fullMethod string) (string, bool) {
	operation, ok := resolver[fullMethod]
	return operation, ok
}

type failingProofProvider struct {
	err           error
	correlationID string
}

type scriptedProofProvider struct {
	errors          []error
	calls           int
	retryWasBounded bool
}

func (provider *scriptedProofProvider) AuthorityProof(
	ctx context.Context,
	_, _ string,
) (string, string, error) {
	provider.calls++
	if provider.calls == 2 {
		deadline, ok := ctx.Deadline()
		remaining := time.Until(deadline)
		provider.retryWasBounded = ok && remaining > 0 && remaining <= proofRetryTimeout
	}
	index := provider.calls - 1
	if index < len(provider.errors) && provider.errors[index] != nil {
		return "", "correlation", provider.errors[index]
	}
	return "proof", "correlation", nil
}

func (provider failingProofProvider) AuthorityProof(
	context.Context,
	string,
	string,
) (string, string, error) {
	return "", provider.correlationID, provider.err
}

func (verifier *fakeVerifier) VerifyAuthorizationContext(
	_ context.Context,
	request *internalrpcauthorityv1.VerifyAuthorizationContextRequest,
	_ ...grpc.CallOption,
) (*internalrpcauthorityv1.VerifyAuthorizationContextResponse, error) {
	verifier.request = request
	return verifier.response, nil
}

func (issuer *fakeIssuer) IssueAuthorizationContext(
	_ context.Context,
	_ *internalrpcauthorityv1.IssueAuthorizationContextRequest,
	_ ...grpc.CallOption,
) (*internalrpcauthorityv1.IssueAuthorizationContextResponse, error) {
	issuer.calls++
	return issuer.response, nil
}

func (issuer *fakeIssuer) IssueContinuationAuthorizationContext(
	_ context.Context,
	request *internalrpcauthorityv1.IssueContinuationAuthorizationContextRequest,
	_ ...grpc.CallOption,
) (*internalrpcauthorityv1.IssueAuthorizationContextResponse, error) {
	issuer.continuationRequest = request
	return issuer.continuationResponse, nil
}

func (*fakeIssuer) CheckReadiness(
	context.Context,
	*internalrpcauthorityv1.AuthorizationIssuerServiceCheckReadinessRequest,
	...grpc.CallOption,
) (*internalrpcauthorityv1.AuthorizationIssuerServiceCheckReadinessResponse, error) {
	return nil, nil
}

func (*fakeVerifier) CheckReadiness(
	context.Context,
	*internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessRequest,
	...grpc.CallOption,
) (*internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessResponse, error) {
	return nil, nil
}

func TestVerifierInterceptorRequiresBothMTLSAndAuthorizationContext(t *testing.T) {
	certificate := testCertificate(t)
	verified := &internalrpcauthorityv1.VerifiedAuthorizationContext{Jti: "accepted"}
	verifier := &fakeVerifier{
		response: &internalrpcauthorityv1.VerifyAuthorizationContextResponse{Context: verified},
	}
	interceptor := VerifierUnaryServerInterceptor(verifier)
	handler := func(ctx context.Context, _ any) (any, error) {
		got, ok := VerifiedAuthorizationContext(ctx)
		if !ok || got.GetJti() != "accepted" {
			t.Fatal("verified context was not propagated")
		}
		return "ok", nil
	}
	base := peer.NewContext(context.Background(), &peer.Peer{
		AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{certificate},
			VerifiedChains:   [][]*x509.Certificate{{certificate}},
		}},
	})
	if _, err := interceptor(
		base,
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/example.v1.Service/Method"},
		handler,
	); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("mTLS-only request code = %s", status.Code(err))
	}
	tokenOnly := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs(AuthorizationMetadata, "compact"),
	)
	if _, err := interceptor(
		tokenOnly,
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/example.v1.Service/Method"},
		handler,
	); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("token-only request code = %s", status.Code(err))
	}
	both := metadata.NewIncomingContext(
		base,
		metadata.Pairs(AuthorizationMetadata, "compact"),
	)
	response, err := interceptor(
		both,
		&emptypb.Empty{},
		&grpc.UnaryServerInfo{FullMethod: "/example.v1.Service/Method"},
		handler,
	)
	if err != nil || response != "ok" {
		t.Fatalf("layered request failed: response=%v err=%v", response, err)
	}
	if verifier.request.GetObservedFullMethod() != "/example.v1.Service/Method" ||
		verifier.request.GetCompactJws() != "compact" ||
		verifier.request.GetObservedRequestDigestSha256() != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" ||
		verifier.request.GetDownstreamPeer().GetSpiffeId() !=
			"spiffe://kodex.local/ns/kodex-system/sa/caller" {
		t.Fatalf("verifier request lost exact binding: %+v", verifier.request)
	}
}

func TestIssuerInterceptorClassifiesLocalProofFailure(t *testing.T) {
	t.Parallel()

	const (
		method        = "/example.v1.Service/Method"
		operation     = "example.read"
		correlationID = "bf51b17a-94d2-4f7e-a7f4-1b014fceec0d"
	)
	for _, test := range []struct {
		name string
		err  error
		code codes.Code
	}{
		{name: "transient", err: status.Error(codes.Unavailable, "private resolver failure"), code: codes.Unavailable},
		{name: "deadline", err: status.Error(codes.DeadlineExceeded, "private resolver failure"), code: codes.DeadlineExceeded},
		{name: "canceled", err: status.Error(codes.Canceled, "private resolver failure"), code: codes.Canceled},
		{name: "rejected", err: status.Error(codes.PermissionDenied, "private resolver failure"), code: codes.Unauthenticated},
	} {
		t.Run(test.name, func(t *testing.T) {
			interceptor := IssuerUnaryClientInterceptor(nil, fakeOperationResolver{method: operation}, failingProofProvider{
				err: test.err, correlationID: correlationID,
			})
			called := false
			err := interceptor(context.Background(), method, &emptypb.Empty{}, nil, nil,
				func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
					called = true
					return nil
				},
			)
			if called {
				t.Fatal("downstream RPC was called without authority proof")
			}
			var failure *LocalAuthorityError
			if !errors.As(err, &failure) || status.Code(err) != test.code ||
				failure.CorrelationID() != correlationID {
				t.Fatalf("local authority failure = %#v, code = %s", err, status.Code(err))
			}
		})
	}
}

func TestAuthorityProofRetriesOneBoundedTransientAttempt(t *testing.T) {
	t.Parallel()

	for _, code := range []codes.Code{codes.Canceled, codes.DeadlineExceeded, codes.Unavailable} {
		t.Run(code.String(), func(t *testing.T) {
			provider := &scriptedProofProvider{errors: []error{status.Error(code, "transient")}}
			proof, _, err := authorityProofWithRetry(t.Context(), provider, "operation", "/example.v1.Service/Method")
			if err != nil || proof != "proof" || provider.calls != 2 || !provider.retryWasBounded {
				t.Fatalf("proof retry: proof=%q err=%v calls=%d bounded=%t", proof, err, provider.calls, provider.retryWasBounded)
			}
		})
	}
}

func TestAuthorityProofStopsAfterSecondTransientFailure(t *testing.T) {
	t.Parallel()

	provider := &scriptedProofProvider{errors: []error{
		status.Error(codes.Canceled, "first transient"),
		status.Error(codes.Unavailable, "second transient"),
	}}
	_, _, err := authorityProofWithRetry(t.Context(), provider, "operation", "/example.v1.Service/Method")
	if status.Code(err) != codes.Unavailable || provider.calls != 2 || !provider.retryWasBounded {
		t.Fatalf("second transient result: err=%v calls=%d bounded=%t", err, provider.calls, provider.retryWasBounded)
	}
}

func TestIssuerInterceptorOpensDownstreamOnlyAfterSuccessfulProofRetry(t *testing.T) {
	t.Parallel()

	const method = "/example.v1.Service/Method"
	provider := &scriptedProofProvider{errors: []error{status.Error(codes.Canceled, "transient")}}
	issuer := &fakeIssuer{response: &internalrpcauthorityv1.IssueAuthorizationContextResponse{CompactJws: "issued"}}
	interceptor := IssuerUnaryClientInterceptor(issuer, fakeOperationResolver{method: "example.read"}, provider)
	invocations := 0
	err := interceptor(t.Context(), method, &emptypb.Empty{}, nil, nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			invocations++
			return nil
		},
	)
	if err != nil || provider.calls != 2 || issuer.calls != 1 || invocations != 1 {
		t.Fatalf("interceptor retry: err=%v proof_calls=%d issuer_calls=%d downstream_calls=%d", err, provider.calls, issuer.calls, invocations)
	}
}

func TestContinuationInterceptorUsesOnlyVerifiedParent(t *testing.T) {
	t.Parallel()

	const (
		method      = "/example.v1.Projection/Resolve"
		operation   = "example.projection.resolve"
		requestID   = "request-1"
		correlation = "correlation-1"
	)
	verified := &internalrpcauthorityv1.VerifiedAuthorizationContext{Jti: "parent-jti"}
	parent := context.WithValue(t.Context(), continuationSourceKey{}, continuationSource{
		compact: "parent-compact", verified: verified,
	})
	bound, err := BindContinuation(parent, operation, method, requestID, correlation)
	if err != nil {
		t.Fatalf("привязать continuation: %v", err)
	}
	issuer := &fakeIssuer{continuationResponse: &internalrpcauthorityv1.IssueAuthorizationContextResponse{CompactJws: "child-compact"}}
	interceptor := ContinuationUnaryClientInterceptor(issuer, fakeOperationResolver{method: operation})
	invocations := 0
	err = interceptor(bound, method, &emptypb.Empty{}, nil, nil,
		func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			invocations++
			metadata, ok := metadata.FromOutgoingContext(ctx)
			if !ok || len(metadata.Get(AuthorizationMetadata)) != 1 || metadata.Get(AuthorizationMetadata)[0] != "child-compact" {
				t.Fatal("выпущенный child context не передан downstream")
			}
			return nil
		},
	)
	if err != nil || invocations != 1 || issuer.continuationRequest == nil {
		t.Fatalf("continuation RPC: err=%v invocations=%d request=%+v", err, invocations, issuer.continuationRequest)
	}
	if issuer.continuationRequest.GetParentAuthorizationContextCompactJws() != "parent-compact" ||
		issuer.continuationRequest.GetOperationId() != operation ||
		issuer.continuationRequest.GetRequestId() != requestID ||
		issuer.continuationRequest.GetCorrelationId() != correlation ||
		issuer.continuationRequest.GetRequestDigestSha256() != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("continuation потерял exact binding: %+v", issuer.continuationRequest)
	}
	if _, err := BindContinuation(t.Context(), operation, method, requestID, correlation); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("контекст без проверенного parent не отклонён: %v", err)
	}
}

func TestAuthorityProofDoesNotRetryRejectedOrCanceledRequest(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		ctx  context.Context
		err  error
	}{
		{name: "permission denied", ctx: t.Context(), err: status.Error(codes.PermissionDenied, "rejected")},
		{name: "unauthenticated", ctx: t.Context(), err: status.Error(codes.Unauthenticated, "rejected")},
		{name: "request canceled", ctx: canceledContext(), err: status.Error(codes.Canceled, "request canceled")},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &scriptedProofProvider{errors: []error{test.err}}
			_, _, _ = authorityProofWithRetry(test.ctx, provider, "operation", "/example.v1.Service/Method")
			if provider.calls != 1 {
				t.Fatalf("non-retryable proof calls = %d, want 1", provider.calls)
			}
		})
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func testCertificate(t *testing.T) *x509.Certificate {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate certificate key: %v", err)
	}
	spiffeID, err := url.Parse(
		"spiffe://kodex.local/ns/kodex-system/sa/caller",
	)
	if err != nil {
		t.Fatalf("parse SPIFFE ID: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "caller"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		URIs:         []*url.URL{spiffeID},
	}
	raw, err := x509.CreateCertificate(
		rand.Reader,
		template,
		template,
		&privateKey.PublicKey,
		privateKey,
	)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certificate, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return certificate
}
