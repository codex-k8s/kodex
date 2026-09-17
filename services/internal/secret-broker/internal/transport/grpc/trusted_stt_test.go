package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type denyingSTTOwner struct {
	fakeOwner
	calls int
}

type materializingSTTOwner struct {
	fakeOwner
	events       *[]string
	request      *cp.ResolveTranscriptionCredentialProjectionRequest
	wrongAccount bool
}

func (owner *materializingSTTOwner) ResolveTranscriptionCredentialProjection(_ context.Context, request *cp.ResolveTranscriptionCredentialProjectionRequest) (*cp.ResolveTranscriptionCredentialProjectionResponse, error) {
	*owner.events = append(*owner.events, "owner")
	owner.request = request
	account := request.GetProviderAccountRef()
	if owner.wrongAccount {
		account = "pacc_other"
	}
	return &cp.ResolveTranscriptionCredentialProjectionResponse{ProviderCredential: &cp.ProviderCredentialBinding{
		AccountRef: account, CredentialRevision: int64(request.GetProviderCredentialGeneration()),
		SecretName: "synthetic-provider-secret", SecretUid: "synthetic-uid", SecretResourceVersion: "7", ContentSha256: strings.Repeat("b", 64),
	}, ExpiresAt: timestamppb.New(time.Now().Add(10 * time.Second))}, nil
}

type materializingSTTStore struct {
	fakeStore
	events     *[]string
	descriptor kubernetesstore.ProviderCredentialDescriptor
	account    string
	raw        []byte
	fail       bool
}

func (store *materializingSTTStore) ReadProviderCredentialExact(_ context.Context, account string, descriptor kubernetesstore.ProviderCredentialDescriptor) ([]byte, error) {
	*store.events = append(*store.events, "store")
	store.account, store.descriptor = account, descriptor
	if store.fail {
		return nil, errors.New("synthetic storage failure")
	}
	store.raw = []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"synthetic-stt-fixture-key"}`)
	return store.raw, nil
}

func (owner *denyingSTTOwner) ResolveTranscriptionCredentialProjection(context.Context, *cp.ResolveTranscriptionCredentialProjectionRequest) (*cp.ResolveTranscriptionCredentialProjectionResponse, error) {
	owner.calls++
	return nil, status.Error(codes.PermissionDenied, "synthetic owner denial")
}

func TestTrustedSTTProjectionRelaysOnlyExplicitSessionToOwner(t *testing.T) {
	method := sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName
	authorizer, err := serviceidentity.NewTrustedCluster(transportprofile.TrustedCluster, secretBrokerSPIFFEID, []serviceidentity.Binding{{
		CallerSPIFFEID: sttSPIFFEID, FullMethod: method, OperationID: sttCredentialOperation, Permission: sttCredentialOperation, ActorMode: serviceidentity.ServiceActor,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"valid", "materialize", "wrong-binding", "store-failure", "owner-denied", "missing-session", "duplicate-session", "tampered-snapshot", "expired", "project", "wrong-caller"} {
		t.Run(scenario, func(t *testing.T) {
			_, locator := projectionAuthorityFixtures()
			locator.Actor, locator.Tenant, locator.Project = nil, nil, nil
			locator.ProjectId = ""
			locator.ExpiresAt = timestamppb.New(time.Now().Add(20 * time.Second))
			snapshot := &sttv1.TrustedTranscriptionAuthority{RpcProfile: transportprofile.TrustedCluster,
				RequestId: locator.RequestId, ActorId: locator.RootActorId, TenantId: locator.TenantId,
				CredentialRevision: locator.SourceRevision, Permission: "stt.transcribe", ExpiresAt: locator.ExpiresAt}
			raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(raw)
			locator.SourceDigestSha256 = hex.EncodeToString(digest[:])
			md := metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster, serviceidentity.CallerMetadataKey, sttSPIFFEID, "authorization", "Bearer synthetic-session", "actor_id", "untrusted")
			switch scenario {
			case "missing-session":
				md.Delete("authorization")
			case "duplicate-session":
				md.Append("authorization", "Bearer another")
			case "tampered-snapshot":
				locator.SourceDigestSha256 = "invalid"
			case "expired":
				locator.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
			case "project":
				locator.ProjectId = "prj_untrusted"
			case "wrong-caller":
				md.Set(serviceidentity.CallerMetadataKey, runtimeControllerSPIFFEID)
			}
			ctx := metadata.NewIncomingContext(t.Context(), md)
			_, err = authorizer.UnaryServerInterceptor()(ctx, nil, &grpcgo.UnaryServerInfo{FullMethod: method}, func(ctx context.Context, _ any) (any, error) {
				if scenario == "materialize" || scenario == "wrong-binding" || scenario == "store-failure" {
					events := []string{}
					owner := &materializingSTTOwner{events: &events, wrongAccount: scenario == "wrong-binding"}
					store := &materializingSTTStore{events: &events, fail: scenario == "store-failure"}
					server := &Server{owner: owner, store: store}
					request := &sttv1.ProjectTranscriptionCredentialRequest{Authority: locator, ProviderAccountRef: "pacc_fixture", ProviderCredentialGeneration: 3, ConfigRevision: 5, ConfigDigestSha256: strings.Repeat("c", 64)}
					response, err := server.ProjectTranscriptionCredential(ctx, request)
					if owner.request == nil || owner.request.GetConfigRevision() != 5 || owner.request.GetConfigDigestSha256() != request.ConfigDigestSha256 || owner.request.GetAuthority().GetRpcProfile() != transportprofile.TrustedCluster {
						t.Fatal("owner configuration binding lost")
					}
					if scenario == "wrong-binding" {
						if strings.Join(events, ",") != "owner" || response != nil || err == nil {
							t.Fatal("wrong owner descriptor reached secret store")
						}
						return response, err
					}
					if strings.Join(events, ",") != "owner,store" || store.account != request.ProviderAccountRef || store.descriptor.SecretName != "synthetic-provider-secret" || store.descriptor.SecretUID != "synthetic-uid" || store.descriptor.SecretResourceVersion != "7" || store.descriptor.ContentSHA256 != strings.Repeat("b", 64) {
						t.Fatal("secret read did not use exact owner descriptor")
					}
					if scenario == "materialize" {
						if err != nil || string(response.GetApiKey()) != "synthetic-stt-fixture-key" || !proto.Equal(response.GetAuthority(), locator) || response.GetConfigRevision() != 5 {
							t.Fatal("successful projection response mismatch")
						}
						for _, b := range store.raw {
							if b != 0 {
								t.Fatal("source credential bytes were not cleared")
							}
						}
						clear(response.ApiKey)
					} else if err == nil || response != nil {
						t.Fatal("storage failure returned credential")
					}
					return response, err
				}
				if scenario == "owner-denied" {
					owner := &denyingSTTOwner{}
					server := &Server{owner: owner}
					response, err := server.ProjectTranscriptionCredential(ctx, &sttv1.ProjectTranscriptionCredentialRequest{Authority: locator})
					if owner.calls != 1 || response != nil || status.Code(err) != codes.PermissionDenied {
						t.Fatal("owner denial did not stop secret projection")
					}
					return response, err
				}
				authority, bound, err := transcriptionProjectionAuthority(ctx, locator)
				if err != nil {
					return nil, err
				}
				if authority.GetRpcProfile() != transportprofile.TrustedCluster || authority.GetProofJti() != "" || authority.GetActorId() != locator.RootActorId || authority.GetCallerCredentialRevision() != locator.SourceRevision {
					t.Fatal("trusted envelope changed owner snapshot")
				}
				ownerMethod := cp.RuntimeSecretWorkService_ResolveTranscriptionCredentialProjection_FullMethodName
				outgoing, err := controlplaneclient.TrustedUnaryClientInterceptor(transportprofile.TrustedCluster, "secret-broker", map[string]string{"platform.credential-projections.stt.resolve": ownerMethod})
				if err != nil {
					t.Fatal(err)
				}
				return nil, outgoing(bound, ownerMethod, nil, nil, nil, func(ctx context.Context, _ string, _, _ any, _ *grpcgo.ClientConn, _ ...grpcgo.CallOption) error {
					headers, _ := metadata.FromOutgoingContext(ctx)
					if len(headers.Get("authorization")) != 1 || headers.Get("authorization")[0] != "Bearer synthetic-session" ||
						len(headers.Get("actor_id")) != 0 || headers.Get(serviceidentity.CallerMetadataKey)[0] != secretBrokerSPIFFEID {
						t.Fatal("owner session relay violated metadata boundary")
					}
					return nil
				})
			})
			if (err == nil) != (scenario == "valid" || scenario == "materialize") {
				t.Fatalf("unexpected projection result: %v", err)
			}
		})
	}
}
