package app

import (
	"context"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/authorization"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/clients/protectedrpc"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type appAuthorityFixture struct {
	sttv1.TranscriptionAuthorityServiceClient
	calls int
}

func (client *appAuthorityFixture) ResolveTranscriptionCatalogAuthority(context.Context, *sttv1.ResolveTranscriptionCatalogAuthorityRequest, ...grpc.CallOption) (*sttv1.ResolveTranscriptionCatalogAuthorityResponse, error) {
	client.calls++
	return &sttv1.ResolveTranscriptionCatalogAuthorityResponse{Authority: &sttv1.TrustedTranscriptionAuthority{
		RpcProfile: transportprofile.TrustedCluster, RequestId: "33333333-3333-4333-8333-333333333333",
		ActorId: "11111111-1111-4111-8111-111111111111", TenantId: "22222222-2222-4222-8222-222222222222",
		CredentialRevision: 9, Permission: value.PermissionManageConfiguration, ExpiresAt: timestamppb.New(time.Now().Add(5 * time.Second)),
	}}, nil
}

func TestTrustedRPCCompositionHasNoSocketOrTLSDependency(t *testing.T) {
	connection, err := dialProfileAuthority(t.Context(), transportprofile.TrustedCluster, authorityclient.LocalConfig{})
	if err != nil || connection.Issuer() != nil || connection.Verifier() != nil || connection.Close() != nil {
		t.Fatal("trusted runtime requires authority socket")
	}
	client := &appAuthorityFixture{}
	security, err := newRPCSecurity(Config{RPCProfile: transportprofile.TrustedCluster}, connection, &protectedrpc.Client{Authority: client})
	if err != nil || len(security.options) != 0 || security.unary == nil || security.stream == nil {
		t.Fatal("trusted runtime requires TLS or lacks authorization")
	}
	method := sttv1.SpeechToTextService_GetModelCatalog_FullMethodName
	for _, scenario := range []string{"valid", "wrong-caller", "no-session", "unknown-method", "technical-method"} {
		md := metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster, serviceidentity.CallerMetadataKey, callerSPIFFEID, "authorization", "Bearer synthetic-session")
		candidate := method
		switch scenario {
		case "wrong-caller":
			md.Set(serviceidentity.CallerMetadataKey, "spiffe://kodex.local/ns/kodex-system/sa/email-bridge")
		case "no-session":
			md.Delete("authorization")
		case "unknown-method":
			candidate = "/unknown.Service/Method"
		case "technical-method":
			candidate = sttv1.SpeechToTextService_CheckReadiness_FullMethodName
		}
		before := client.calls
		called := false
		_, err := security.unary(metadata.NewIncomingContext(t.Context(), md), &sttv1.GetModelCatalogRequest{}, &grpc.UnaryServerInfo{FullMethod: candidate}, func(ctx context.Context, _ any) (any, error) {
			called = true
			principal, err := authorization.Principal(ctx, method)
			if err != nil || principal.RPCProfile != transportprofile.TrustedCluster || principal.Permission != value.PermissionManageConfiguration {
				t.Fatal("runtime did not bind owner principal")
			}
			return nil, nil
		})
		if scenario == "valid" {
			if err != nil || !called || client.calls != before+1 {
				t.Fatal("valid runtime request rejected")
			}
		} else if err == nil || called || client.calls != before {
			t.Fatal("invalid runtime request reached owner or handler")
		}
	}
	if _, err := newRPCSecurity(Config{}, connection, nil); err == nil {
		t.Fatal("protected runtime accepted missing verifier")
	}
	if _, err := newRPCSecurity(Config{RPCProfile: "insecure"}, connection, nil); err == nil {
		t.Fatal("unknown runtime profile accepted")
	}
}
