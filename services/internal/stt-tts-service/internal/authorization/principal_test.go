package authorization

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type principalVerifier struct {
	verified *internalrpcauthorityv1.VerifiedAuthorizationContext
}

func (verifier principalVerifier) VerifyAuthorizationContext(context.Context, *internalrpcauthorityv1.VerifyAuthorizationContextRequest, ...grpc.CallOption) (*internalrpcauthorityv1.VerifyAuthorizationContextResponse, error) {
	return &internalrpcauthorityv1.VerifyAuthorizationContextResponse{Context: verifier.verified}, nil
}
func (principalVerifier) CheckReadiness(context.Context, *internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessRequest, ...grpc.CallOption) (*internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessResponse, error) {
	return &internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessResponse{Ready: true}, nil
}

func TestPrincipalUsesAuthoritySourceRevision(t *testing.T) {
	verified := validVerifiedAuthorizationContext()
	principal, err := Principal(verifiedPrincipalContext(t, verified), sttv1.SpeechToTextService_Transcribe_FullMethodName)
	if err != nil {
		t.Fatal(err)
	}
	if principal.AuthorityRevision != verified.SourceRevision || principal.AuthorityRevision == verified.PolicyRevision ||
		principal.Actor.Reference == "" || principal.Tenant.Reference == "" || principal.Project.Reference == "" {
		t.Fatal("source revision или identity provenance потеряны")
	}
}

func TestPrincipalRejectsInvalidSourceRevision(t *testing.T) {
	for _, revision := range []uint64{0, maximumAuthorityRevision + 1} {
		verified := validVerifiedAuthorizationContext()
		verified.SourceRevision = revision
		if _, err := Principal(verifiedPrincipalContext(t, verified), sttv1.SpeechToTextService_Transcribe_FullMethodName); err == nil {
			t.Fatalf("source revision %d принят", revision)
		}
	}
}

func TestPrincipalRejectsMissingIdentityProvenance(t *testing.T) {
	verified := validVerifiedAuthorizationContext()
	verified.Authority.Project.Provenance = nil
	if _, err := Principal(verifiedPrincipalContext(t, verified), sttv1.SpeechToTextService_Transcribe_FullMethodName); err == nil {
		t.Fatal("project без provenance принят")
	}
}

func validVerifiedAuthorizationContext() *internalrpcauthorityv1.VerifiedAuthorizationContext {
	digest := strings.Repeat("a", 64)
	identity := func(id string) *internalrpcauthorityv1.AuthorityIdentity {
		return &internalrpcauthorityv1.AuthorityIdentity{Id: id, Provenance: &internalrpcauthorityv1.AuthorityProvenance{
			Source:    internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_DOMAIN_STATE,
			Reference: "source:" + id, Revision: 3, DigestSha256: digest,
		}}
	}
	return &internalrpcauthorityv1.VerifiedAuthorizationContext{
		ContractVersion: 1, Audience: expectedAudience, TargetWorkloadId: expectedWorkloadID, CallerWorkloadId: expectedCaller,
		FullMethod: sttv1.SpeechToTextService_Transcribe_FullMethodName, OperationId: transcribeOperation,
		Permission: value.TransportPermissionTranscribe,
		Authority:  &internalrpcauthorityv1.CallerAuthority{Actor: identity("actor"), Tenant: identity("tenant"), Project: identity("prj_abcdefgh")},
		Jti:        uuid.NewString(), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
		SourceRevision: 7, SourceDigestSha256: digest, PolicyRevision: 19,
		AuthorityAbiVersion: internalrpcauth.AuthorityABIVersion,
		RequestBindingMode:  internalrpcauth.RequestBindingStream,
	}
}

func TestPrincipalExactPermissionMapping(t *testing.T) {
	for _, permission := range []string{value.TransportPermissionTranscribe, value.PermissionTranscribe, value.ConfigurationCapability, "speech.transcribe", "platform.stt.*", ""} {
		verified := validVerifiedAuthorizationContext()
		verified.Permission = permission
		principal, err := Principal(verifiedPrincipalContext(t, verified), sttv1.SpeechToTextService_Transcribe_FullMethodName)
		if permission == value.TransportPermissionTranscribe {
			if err != nil || principal.Permission != value.PermissionTranscribe {
				t.Fatal("exact mapping нарушен")
			}
		} else if err == nil {
			t.Fatal("неверное RPC permission принято")
		}
	}
}

func TestPrincipalOrganizationWithoutProject(t *testing.T) {
	verified := validVerifiedAuthorizationContext()
	verified.Authority.Project = nil
	principal, err := Principal(verifiedPrincipalContext(t, verified), sttv1.SpeechToTextService_Transcribe_FullMethodName)
	if err != nil || principal.ProjectID != "" || principal.TenantID == "" || principal.Project != (value.AuthorityProvenance{}) {
		t.Fatal("organization scope потерян")
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
		PeerCertificates: []*x509.Certificate{certificate}, VerifiedChains: [][]*x509.Certificate{{certificate}},
	}}})
	base = metadata.NewIncomingContext(base, metadata.Pairs(authorityclient.AuthorizationMetadata, "compact"))
	interceptor := authorityclient.VerifierUnaryServerInterceptor(principalVerifier{verified: verified})
	var authorized context.Context
	_, err = interceptor(base, &sttv1.TranscribeRequest{}, &grpc.UnaryServerInfo{FullMethod: sttv1.SpeechToTextService_Transcribe_FullMethodName}, func(ctx context.Context, _ any) (any, error) {
		authorized = ctx
		return nil, nil
	})
	if err != nil {
		t.Fatalf("создание проверенного контекста: %v", err)
	}
	return authorized
}
