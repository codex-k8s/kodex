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

	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type fakeVerifier struct {
	response *internalrpcauthorityv1.VerifyAuthorizationContextResponse
	request  *internalrpcauthorityv1.VerifyAuthorizationContextRequest
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
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/example.v1.Service/Method"},
		handler,
	)
	if err != nil || response != "ok" {
		t.Fatalf("layered request failed: response=%v err=%v", response, err)
	}
	if verifier.request.GetObservedFullMethod() != "/example.v1.Service/Method" ||
		verifier.request.GetCompactJws() != "compact" ||
		verifier.request.GetDownstreamPeer().GetSpiffeId() !=
			"spiffe://mattercodex.local/ns/mattercodex-system/sa/caller" {
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
		{name: "rejected", err: status.Error(codes.PermissionDenied, "private resolver failure"), code: codes.Unauthenticated},
	} {
		t.Run(test.name, func(t *testing.T) {
			interceptor := IssuerUnaryClientInterceptor(nil, fakeOperationResolver{method: operation}, failingProofProvider{
				err: test.err, correlationID: correlationID,
			})
			called := false
			err := interceptor(context.Background(), method, nil, nil, nil,
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

func testCertificate(t *testing.T) *x509.Certificate {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate certificate key: %v", err)
	}
	spiffeID, err := url.Parse(
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/caller",
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
