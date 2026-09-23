package authorization

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
	"google.golang.org/protobuf/types/known/timestamppb"
)

type trustedOwnerClient struct {
	snapshot          *sttv1.TrustedTranscriptionAuthority
	calls             int
	metadata          metadata.MD
	deadline          time.Time
	expiresAtDeadline bool
}

func (client *trustedOwnerClient) responseSnapshot(ctx context.Context) *sttv1.TrustedTranscriptionAuthority {
	client.deadline, _ = ctx.Deadline()
	if client.expiresAtDeadline && client.snapshot != nil {
		client.snapshot.ExpiresAt = timestamppb.New(client.deadline)
	}
	return client.snapshot
}

func (client *trustedOwnerClient) ResolveTranscriptionAuthority(ctx context.Context, _ *sttv1.ResolveTranscriptionAuthorityRequest, _ ...grpc.CallOption) (*sttv1.ResolveTranscriptionAuthorityResponse, error) {
	client.calls++
	client.metadata, _ = metadata.FromOutgoingContext(ctx)
	return &sttv1.ResolveTranscriptionAuthorityResponse{Authority: client.responseSnapshot(ctx)}, nil
}

func (client *trustedOwnerClient) ResolveTranscriptionCatalogAuthority(ctx context.Context, _ *sttv1.ResolveTranscriptionCatalogAuthorityRequest, _ ...grpc.CallOption) (*sttv1.ResolveTranscriptionCatalogAuthorityResponse, error) {
	client.calls++
	client.metadata, _ = metadata.FromOutgoingContext(ctx)
	return &sttv1.ResolveTranscriptionCatalogAuthorityResponse{Authority: client.responseSnapshot(ctx)}, nil
}

func TestTrustedResolverValidatesOwnerSnapshotAndDoesNotForwardIdentity(t *testing.T) {
	for _, catalog := range []bool{false, true} {
		method, operation, permission, domainPermission := sttv1.SpeechToTextService_Transcribe_FullMethodName, transcribeOperation, value.TransportPermissionTranscribe, value.PermissionTranscribe
		if catalog {
			method, operation, permission, domainPermission = sttv1.SpeechToTextService_GetModelCatalog_FullMethodName, modelCatalogOperation, value.PermissionManageConfiguration, value.PermissionManageConfiguration
		}
		t.Run(domainPermission, func(t *testing.T) {
			const caller = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
			authorizer, err := serviceidentity.NewTrustedCluster(transportprofile.TrustedCluster, trustedSTTCaller, []serviceidentity.Binding{{CallerSPIFFEID: caller, FullMethod: method, OperationID: operation, Permission: permission, ActorMode: serviceidentity.UserActor}})
			if err != nil {
				t.Fatal(err)
			}
			for _, scenario := range []string{"valid", "missing", "profile", "permission", "actor", "tenant", "revision", "expired", "too-long", "deadline", "no-session", "duplicate-session", "project"} {
				t.Run(scenario, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
					defer cancel()
					client := &trustedOwnerClient{snapshot: &sttv1.TrustedTranscriptionAuthority{
						RpcProfile: transportprofile.TrustedCluster, RequestId: "33333333-3333-4333-8333-333333333333",
						ActorId: "11111111-1111-4111-8111-111111111111", TenantId: "22222222-2222-4222-8222-222222222222",
						CredentialRevision: 9, Permission: domainPermission, ExpiresAt: timestamppb.New(time.Now().Add(5 * time.Second)),
					}}
					md := metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster, serviceidentity.CallerMetadataKey, caller, "authorization", "Bearer synthetic-session", "actor_id", "attacker")
					switch scenario {
					case "missing":
						client.snapshot = nil
					case "profile":
						client.snapshot.RpcProfile = "service-v1"
					case "permission":
						client.snapshot.Permission = "wrong"
					case "actor":
						client.snapshot.ActorId = "bad"
					case "tenant":
						client.snapshot.TenantId = "bad"
					case "revision":
						client.snapshot.CredentialRevision = 0
					case "expired":
						client.snapshot.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
					case "too-long":
						client.snapshot.ExpiresAt = timestamppb.New(time.Now().Add(time.Minute))
					case "deadline":
						client.snapshot.ExpiresAt = timestamppb.New(time.Now().Add(20 * time.Second))
					case "no-session":
						md.Delete("authorization")
					case "duplicate-session":
						md.Append("authorization", "Bearer another-session")
					case "project":
						md.Set("x-kodex-project-ref", "prj_untrusted")
					}
					ctx = metadata.NewIncomingContext(ctx, md)
					ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("actor_id", "attacker", "authorization", "Bearer unrelated"))
					resolver, err := NewTrustedResolver(transportprofile.TrustedCluster, client)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := resolver.Resolve(ctx, method); err == nil || client.calls != 0 {
						t.Fatal("unadmitted request reached owner")
					}
					_, err = authorizer.UnaryServerInterceptor()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, func(ctx context.Context, _ any) (any, error) {
						resolved, err := resolver.Resolve(ctx, method)
						if err != nil {
							return nil, err
						}
						principal, err := Principal(resolved, method)
						if err != nil || principal.ActorID != client.snapshot.GetActorId() || principal.TenantID != client.snapshot.GetTenantId() || principal.ProjectID != "" || principal.RPCProfile != transportprofile.TrustedCluster || !validSHA256(principal.AuthorityDigestSHA256) {
							t.Fatal("owner principal not bound")
						}
						if _, err := Principal(resolved, method+"Other"); err == nil {
							t.Fatal("method boundary lost")
						}
						if len(client.metadata) != 3 || client.metadata.Get("authorization")[0] != "Bearer synthetic-session" || client.metadata.Get(serviceidentity.CallerMetadataKey)[0] != trustedSTTCaller {
							t.Fatal("unexpected outgoing metadata")
						}
						cancel()
						if _, err := Principal(resolved, method); err == nil {
							t.Fatal("cancelled principal accepted")
						}
						return nil, nil
					})
					if (err == nil) != (scenario == "valid") {
						t.Fatalf("unexpected result for %s: %v", scenario, err)
					}
				})
			}
		})
	}
	if _, err := NewTrustedResolver("service-v1", &trustedOwnerClient{}); err == nil {
		t.Fatal("implicit trusted profile accepted")
	}
}

func TestTrustedResolverLeavesDeadlineMarginForNestedOwnerRPC(t *testing.T) {
	const caller = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
	method := sttv1.SpeechToTextService_GetModelCatalog_FullMethodName
	authorizer, err := serviceidentity.NewTrustedCluster(transportprofile.TrustedCluster, trustedSTTCaller, []serviceidentity.Binding{{
		CallerSPIFFEID: caller, FullMethod: method, OperationID: modelCatalogOperation,
		Permission: value.PermissionManageConfiguration, ActorMode: serviceidentity.UserActor,
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	outerDeadline, _ := ctx.Deadline()
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(
		serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster,
		serviceidentity.CallerMetadataKey, caller,
		"authorization", "Bearer synthetic-session",
	))
	client := &trustedOwnerClient{expiresAtDeadline: true, snapshot: &sttv1.TrustedTranscriptionAuthority{
		RpcProfile: transportprofile.TrustedCluster, RequestId: "33333333-3333-4333-8333-333333333333",
		ActorId: "11111111-1111-4111-8111-111111111111", TenantId: "22222222-2222-4222-8222-222222222222",
		CredentialRevision: 9, Permission: value.PermissionManageConfiguration,
	}}
	resolver, err := NewTrustedResolver(transportprofile.TrustedCluster, client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = authorizer.UnaryServerInterceptor()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, func(ctx context.Context, _ any) (any, error) {
		return resolver.Resolve(ctx, method)
	})
	if err != nil {
		t.Fatalf("snapshot at nested RPC deadline was rejected: %v", err)
	}
	if client.deadline.IsZero() || outerDeadline.Sub(client.deadline) < trustedAuthorityDeadlineMargin {
		t.Fatal("nested owner RPC did not reserve the deadline margin")
	}
}
