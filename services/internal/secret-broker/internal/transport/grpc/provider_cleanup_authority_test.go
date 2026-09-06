package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	av1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

func TestCleanupRecoveryOriginIsBoundToVerifiedRequest(t *testing.T) {
	for _, scenario := range []string{"allowed", "missing bearer", "changed origin", "changed legacy range"} {
		t.Run(scenario, func(t *testing.T) {
			request := &cp.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest{TaskRef: "pcct_current1234", AccountRef: "pacc_cleanup1234", LeaseGeneration: 3,
				TargetKind: cp.ProviderCredentialCleanupTargetKind_PROVIDER_CREDENTIAL_CLEANUP_TARGET_KIND_CREDENTIAL,
				Credential: &cp.ProviderCredentialDescriptor{}, RecoveryIdentity: &cp.ProviderCredentialCleanupRecoveryIdentity{TaskRef: "pcct_origin1234", LeaseGeneration: 1}}
			raw, _ := proto.MarshalOptions{Deterministic: true}.Marshal(request)
			sum := sha256.Sum256(raw)
			verifier := &catalogVerifierFixture{expectedDigest: hex.EncodeToString(sum[:]), mutate: func(v *av1.VerifiedAuthorizationContext) {
				v.FullMethod = cp.ProviderCredentialMaterializerService_CleanupProviderCredential_FullMethodName
				v.OperationId, v.Permission = "platform.provider-credentials.cleanup", "platform.provider-credentials.cleanup"
			}}
			ctx := catalogPeerContext(t)
			switch scenario {
			case "missing bearer":
				ctx = metadata.NewIncomingContext(ctx, metadata.MD{})
			case "changed origin":
				request.RecoveryIdentity.TaskRef = "pcct_foreign1234"
			case "changed legacy range":
				request.RecoveryIdentity.LegacyLastGeneration = 3
			}
			calls := 0
			_, err := authorityclient.VerifierUnaryServerInterceptor(verifier)(ctx, request, &googlegrpc.UnaryServerInfo{FullMethod: cp.ProviderCredentialMaterializerService_CleanupProviderCredential_FullMethodName}, func(context.Context, any) (any, error) {
				calls++
				return &cp.ProviderCredentialMaterializerServiceCleanupProviderCredentialResponse{}, nil
			})
			if scenario == "allowed" {
				if err != nil || calls != 1 {
					t.Fatalf("valid cleanup proof rejected: %v", err)
				}
			} else if err == nil || calls != 0 {
				t.Fatal("unbound recovery reached cleanup handler")
			}
		})
	}
}
