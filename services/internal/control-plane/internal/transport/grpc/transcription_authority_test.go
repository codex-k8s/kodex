package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/authorization"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/rpcprincipal"
	"github.com/google/uuid"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

type transcriptionAuthorityOwner struct {
	actor, tenant string
	deny          bool
	calls         int
}

func (owner *transcriptionAuthorityOwner) ResolveProofAuthority(_ context.Context, input platformrepo.ProofPrincipalInput) (platformrepo.ProofAuthority, error) {
	owner.calls++
	if owner.deny || input.ExternalActorID != "verified-subject" || input.RPCProfile != transportprofile.TrustedCluster {
		return platformrepo.ProofAuthority{}, errors.New("authority denied")
	}
	return platformrepo.ProofAuthority{ActorID: owner.actor, OrganizationID: owner.tenant}, nil
}

func (*transcriptionAuthorityOwner) ResolveServiceCredentialGeneration(context.Context, string) (uint64, error) {
	return 0, errors.New("unexpected service credential")
}

func (*transcriptionAuthorityOwner) VerifyToken(_ context.Context, token string) (oidcverifier.Principal, error) {
	if token != "synthetic-session" {
		return oidcverifier.Principal{}, errors.New("credential denied")
	}
	return oidcverifier.Principal{Subject: "verified-subject", SessionRevision: 9}, nil
}

func TestTranscriptionAuthorityRequiresVerifiedOwnerAndExactMethod(t *testing.T) {
	for _, catalog := range []bool{false, true} {
		method := sttv1.TranscriptionAuthorityService_ResolveTranscriptionAuthority_FullMethodName
		operation, permission := platformrepo.TrustedSTTAuthorityOperation, "stt.transcribe"
		var request proto.Message = &sttv1.ResolveTranscriptionAuthorityRequest{}
		if catalog {
			method = sttv1.TranscriptionAuthorityService_ResolveTranscriptionCatalogAuthority_FullMethodName
			operation, permission = platformrepo.TrustedSTTCatalogAuthorityOperation, "system.configuration.manage"
			request = &sttv1.ResolveTranscriptionCatalogAuthorityRequest{}
		}
		t.Run(permission, func(t *testing.T) {
			const caller = "spiffe://kodex.local/ns/kodex-system/sa/stt-tts-service"
			authorizer, err := serviceidentity.NewTrustedCluster(transportprofile.TrustedCluster,
				"spiffe://kodex.local/ns/kodex-system/sa/control-plane", []serviceidentity.Binding{{
					CallerSPIFFEID: caller, FullMethod: method, OperationID: operation, Permission: operation, ActorMode: serviceidentity.UserActor,
				}})
			if err != nil {
				t.Fatal(err)
			}
			for _, scenario := range []string{"valid", "no-session", "owner-denied", "invalid-actor", "invalid-tenant", "wrong-caller", "wrong-profile", "project", "cancelled"} {
				t.Run(scenario, func(t *testing.T) {
					owner := &transcriptionAuthorityOwner{actor: "11111111-1111-4111-8111-111111111111", tenant: "22222222-2222-4222-8222-222222222222"}
					resolver, err := rpcprincipal.New(owner, owner)
					if err != nil {
						t.Fatal(err)
					}
					md := metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster, serviceidentity.CallerMetadataKey, caller, "authorization", "Bearer synthetic-session")
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					switch scenario {
					case "no-session":
						md.Delete("authorization")
					case "owner-denied":
						owner.deny = true
					case "invalid-actor":
						owner.actor = "invalid"
					case "invalid-tenant":
						owner.tenant = "invalid"
					case "wrong-caller":
						md.Set(serviceidentity.CallerMetadataKey, "spiffe://kodex.local/ns/kodex-system/sa/email-bridge")
					case "wrong-profile":
						md.Set(serviceidentity.ProfileMetadataKey, "service-v1")
					case "project":
						md.Set("x-kodex-project-ref", "prj_untrusted")
					case "cancelled":
						cancel()
					}
					ctx = metadata.NewIncomingContext(ctx, md)
					result, err := authorization.ServiceIdentityUnary(authorizer, resolver)(ctx, request, &grpcgo.UnaryServerInfo{FullMethod: method}, func(ctx context.Context, _ any) (any, error) {
						if _, err := authorization.TrustedPrincipal(ctx, method+"Other"); err == nil {
							t.Fatal("principal escaped method boundary")
						}
						server := &Server{}
						if catalog {
							response, err := server.ResolveTranscriptionCatalogAuthority(ctx, request.(*sttv1.ResolveTranscriptionCatalogAuthorityRequest))
							return response.GetAuthority(), err
						}
						response, err := server.ResolveTranscriptionAuthority(ctx, request.(*sttv1.ResolveTranscriptionAuthorityRequest))
						return response.GetAuthority(), err
					})
					if scenario != "valid" {
						if err == nil {
							t.Fatal("invalid authority accepted")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					authority := result.(*sttv1.TrustedTranscriptionAuthority)
					deadline, _ := ctx.Deadline()
					if authority.GetRpcProfile() != transportprofile.TrustedCluster || authority.GetActorId() != owner.actor || authority.GetTenantId() != owner.tenant ||
						authority.GetPermission() != permission || authority.GetCredentialRevision() != 9 || uuid.Validate(authority.GetRequestId()) != nil || owner.calls != 1 ||
						!authority.GetExpiresAt().IsValid() || authority.GetExpiresAt().AsTime().After(deadline) || !authority.GetExpiresAt().AsTime().After(time.Now()) {
						t.Fatal("authority snapshot does not match verified owner and request lifetime")
					}
				})
			}
		})
	}
}

func TestTranscriptionAuthorityRejectsCallerSuppliedIdentity(t *testing.T) {
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(
		serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster,
		serviceidentity.CallerMetadataKey, "spiffe://kodex.local/ns/kodex-system/sa/stt-tts-service",
		"actor_id", "11111111-1111-4111-8111-111111111111",
		"organization_id", "22222222-2222-4222-8222-222222222222",
	))
	server := &Server{}
	if response, err := server.ResolveTranscriptionAuthority(ctx, &sttv1.ResolveTranscriptionAuthorityRequest{}); err == nil || response != nil {
		t.Fatal("unverified metadata became transcription authority")
	}
	if response, err := server.ResolveTranscriptionCatalogAuthority(ctx, &sttv1.ResolveTranscriptionCatalogAuthorityRequest{}); err == nil || response != nil {
		t.Fatal("unverified metadata became catalog authority")
	}
}
