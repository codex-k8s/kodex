package authorization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/rpcprincipal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// TestModelDiscoveryGeneratedAuthorityMapping admits the exact gateway/RPC through the checked-in generated policy, real middleware and owner resolver; missing credentials, foreign peers and revoked peers never reach the handler.
// TestModelDiscoveryGeneratedAuthorityMapping допускает exact gateway и RPC через сохранённую generated policy, настоящий middleware и owner resolver; отсутствие credentials, чужой peer и отзыв peer не достигают handler.
func TestModelDiscoveryGeneratedAuthorityMapping(t *testing.T) {
	const target = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
	const caller = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
	const operation = "platform.query.model-discovery.list"
	method := cp.PlatformQueryService_ListModelDiscovery_FullMethodName
	raw, err := os.ReadFile("../app/service-identity-policy.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name, peer, bearer, profile string
		revoked                     bool
		code                        codes.Code
	}{
		{name: "exact registered route", peer: caller, bearer: "Bearer user-credential", profile: "service-v1", code: codes.OK},
		{name: "missing bearer", peer: caller, profile: "service-v1", code: codes.Unauthenticated},
		{name: "foreign workload", peer: "spiffe://kodex.local/ns/kodex-system/sa/agent-runner", bearer: "Bearer user-credential", profile: "service-v1", code: codes.PermissionDenied},
		{name: "revoked peer", peer: caller, bearer: "Bearer user-credential", profile: "service-v1", revoked: true, code: codes.PermissionDenied},
		{name: "wrong profile", peer: caller, bearer: "Bearer user-credential", profile: "task-v1", code: codes.Unauthenticated},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			revocations := &streamRevocations{revoked: scenario.revoked}
			authorizer, digest, err := serviceidentity.FromPolicy(raw, target, revocations)
			if err != nil || len(digest) != 64 {
				t.Fatal("generated policy rejected")
			}
			owner := &streamOwnerFixture{user: true}
			resolver, err := rpcprincipal.New(owner, owner)
			if err != nil {
				t.Fatal(err)
			}
			ctx := metadata.NewIncomingContext(serviceStreamPeer(t, scenario.peer, "discovery synthetic certificate"), metadata.Pairs("x-kodex-rpc-profile", scenario.profile, "authorization", scenario.bearer))
			request := &cp.ListModelDiscoveryRequest{Query: "glm-5.3", ProviderId: "zai-coding-plan", Family: "glm", Tools: "SUPPORTED", Reasoning: "SUPPORTED", Cache: "UNKNOWN", GroupBy: "FAMILY"}
			encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			expected := sha256.Sum256(encoded)
			handled := false
			_, err = ServiceIdentityUnary(authorizer, resolver)(ctx, request, &grpc.UnaryServerInfo{FullMethod: method}, func(ctx context.Context, _ any) (any, error) {
				handled = true
				principal, lookupErr := Principal(ctx, method)
				if lookupErr != nil || principal.ActorID != "resolved-actor" || principal.AuthorityTenant != "resolved-org" || principal.Permission != operation || principal.CallerWorkload != "control-api-gateway" || principal.ProjectRef != "" || owner.digest != hex.EncodeToString(expected[:]) {
					t.Fatal("exact domain authority or request digest lost")
				}
				if _, lookupErr = Principal(ctx, cp.PlatformQueryService_ListProviderDefinitions_FullMethodName); lookupErr == nil {
					t.Fatal("principal escaped exact method")
				}
				return &cp.ListModelDiscoveryResponse{}, nil
			})
			if status.Code(err) != scenario.code || handled != (scenario.code == codes.OK) {
				t.Fatalf("authority outcome: handled=%t code=%s want=%s", handled, status.Code(err), scenario.code)
			}
		})
	}
}
