package protectedrpc

import (
	"context"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestTrustedDialRequiresExplicitProfileAndExactServices(t *testing.T) {
	config := Config{RPCProfile: transportprofile.TrustedCluster, DialTimeout: time.Second,
		Policy:     TargetConfig{Target: "dns:///control-plane.kodex-system.svc:8443"},
		Credential: TargetConfig{Target: "dns:///secret-broker.kodex-system.svc:8443"}}
	client, err := Dial(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	if client.Authority == nil || !client.trustedCluster || client.issuer != nil {
		t.Fatal("trusted dependencies incomplete")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Check(t.Context()); err == nil {
		t.Fatal("closed connections reported ready")
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.RPCProfile = "" },
		func(c *Config) { c.RPCProfile = "insecure" },
		func(c *Config) { c.Policy.Target = "dns:///external.example:8443" },
		func(c *Config) { c.Credential.Target = "dns:///control-plane.kodex-system.svc:8443" },
		func(c *Config) { c.DialTimeout = 0 },
	} {
		candidate := config
		mutate(&candidate)
		if connection, err := Dial(t.Context(), candidate); err == nil {
			_ = connection.Close()
			t.Fatal("invalid trusted configuration accepted")
		}
	}
}

func TestTrustedClientRoutesAndMetadataAreClosed(t *testing.T) {
	for _, credential := range []bool{false, true} {
		for _, method := range []string{
			sttv1.TranscriptionAuthorityService_ResolveTranscriptionAuthority_FullMethodName,
			sttv1.TranscriptionAuthorityService_ResolveTranscriptionCatalogAuthority_FullMethodName,
			sttv1.TranscriptionPolicyProjectionService_ResolveTranscriptionPolicy_FullMethodName,
			sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName,
			"/unknown.Service/Method",
		} {
			allowed := method != "/unknown.Service/Method" && (credential == (method == sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName))
			md := metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster, serviceidentity.CallerMetadataKey, trustedCaller, "authorization", "Bearer synthetic", "actor_id", "untrusted")
			called := false
			err := trustedUnary(credential)(metadata.NewOutgoingContext(t.Context(), md), method, nil, nil, nil,
				func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
					called = true
					outgoing, _ := metadata.FromOutgoingContext(ctx)
					if len(outgoing) != 3 || len(outgoing.Get("actor_id")) != 0 {
						t.Fatal("untrusted metadata forwarded")
					}
					return nil
				})
			if (err == nil) != allowed || called != allowed {
				t.Fatal("destination method boundary violated")
			}
		}
	}
	method := sttv1.TranscriptionPolicyProjectionService_ResolveTranscriptionPolicy_FullMethodName
	for _, credential := range []string{"", "Bearer ", "Bearer first second", "Bearer synthetic\n"} {
		ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster, serviceidentity.CallerMetadataKey, trustedCaller, "authorization", credential))
		if err := trustedUnary(false)(ctx, method, nil, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			t.Fatal("invalid credential reached network")
			return nil
		}); err == nil {
			t.Fatal("invalid credential accepted")
		}
	}
	principal := value.Principal{RPCProfile: transportprofile.TrustedCluster, RequestID: "synthetic-request"}
	if _, err := bindTrustedDelegated(t.Context(), principal, principal.RequestID, "correlation", method, "platform.stt.policy.resolve"); err == nil {
		t.Fatal("caller-built principal accepted without private owner context")
	}
}
