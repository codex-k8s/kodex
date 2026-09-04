package authorization

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type principalVerifier struct {
	verified *internalrpcauthorityv1.VerifiedAuthorizationContext
}

func (verifier principalVerifier) VerifyAuthorizationContext(
	context.Context,
	*internalrpcauthorityv1.VerifyAuthorizationContextRequest,
	...grpc.CallOption,
) (*internalrpcauthorityv1.VerifyAuthorizationContextResponse, error) {
	return &internalrpcauthorityv1.VerifyAuthorizationContextResponse{Context: verifier.verified}, nil
}

func (principalVerifier) CheckReadiness(
	context.Context,
	*internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessRequest,
	...grpc.CallOption,
) (*internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessResponse, error) {
	return &internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessResponse{Ready: true}, nil
}

func TestPrincipalUsesAuthoritySourceRevision(t *testing.T) {
	verified := validVerifiedAuthorizationContext()
	verified.SourceRevision = 7
	verified.PolicyRevision = 19

	principal, err := Principal(verifiedPrincipalContext(t, verified), sttv1.SpeechToTextService_Transcribe_FullMethodName)
	if err != nil {
		t.Fatal(err)
	}
	if principal.AuthorityRevision != verified.SourceRevision || principal.AuthorityRevision == verified.PolicyRevision {
		t.Fatalf("authority revision = %d, ожидалась source revision %d", principal.AuthorityRevision, verified.SourceRevision)
	}
	if principal.AuthorityDigestSHA256 != verified.SourceDigestSha256 {
		t.Fatal("authority digest не взят из source snapshot")
	}
}

func TestPrincipalRejectsInvalidSourceRevision(t *testing.T) {
	for _, test := range []struct {
		name     string
		revision uint64
	}{
		{name: "zero", revision: 0},
		{name: "above JSON safe integer", revision: maximumAuthorityRevision + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			verified := validVerifiedAuthorizationContext()
			verified.SourceRevision = test.revision
			if _, err := Principal(verifiedPrincipalContext(t, verified), sttv1.SpeechToTextService_Transcribe_FullMethodName); err == nil {
				t.Fatalf("source revision %d принят", test.revision)
			}
		})
	}
}

func validVerifiedAuthorizationContext() *internalrpcauthorityv1.VerifiedAuthorizationContext {
	return &internalrpcauthorityv1.VerifiedAuthorizationContext{
		ContractVersion:  1,
		Audience:         expectedAudience,
		TargetWorkloadId: expectedWorkloadID,
		CallerWorkloadId: expectedCaller,
		FullMethod:       sttv1.SpeechToTextService_Transcribe_FullMethodName,
		OperationId:      transcribeOperation,
		Permission:       value.PermissionTranscribe,
		Authority: &internalrpcauthorityv1.CallerAuthority{
			Actor:   &internalrpcauthorityv1.AuthorityIdentity{Id: "actor"},
			Tenant:  &internalrpcauthorityv1.AuthorityIdentity{Id: "tenant"},
			Project: &internalrpcauthorityv1.AuthorityIdentity{Id: "prj_abcdefgh"},
		},
		Jti:                "request",
		ExpiresAt:          timestamppb.New(time.Now().Add(time.Minute)),
		SourceRevision:     7,
		SourceDigestSha256: strings.Repeat("a", 64),
		PolicyRevision:     19,
	}
}

func verifiedPrincipalContext(t *testing.T, verified *internalrpcauthorityv1.VerifiedAuthorizationContext) context.Context {
	t.Helper()
	spiffeID, err := url.Parse("spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway")
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{Raw: []byte("test-certificate"), URIs: []*url.URL{spiffeID}}
	base := peer.NewContext(t.Context(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{certificate},
		VerifiedChains:   [][]*x509.Certificate{{certificate}},
	}}})
	base = metadata.NewIncomingContext(base, metadata.Pairs(authorityclient.AuthorizationMetadata, "compact"))
	interceptor := authorityclient.VerifierUnaryServerInterceptor(principalVerifier{verified: verified})
	var authorized context.Context
	_, err = interceptor(base, nil, &grpc.UnaryServerInfo{FullMethod: sttv1.SpeechToTextService_Transcribe_FullMethodName}, func(ctx context.Context, _ any) (any, error) {
		authorized = ctx
		return nil, nil
	})
	if err != nil {
		t.Fatalf("создание проверенного контекста: %v", err)
	}
	return authorized
}
