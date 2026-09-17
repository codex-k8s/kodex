package grpc

import (
	"context"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTrustedProjectionSelectorRequiresExactServerAdmission(t *testing.T) {
	method := sb.RuntimeCredentialProjectionService_MaterializeRuntimeCredentials_FullMethodName
	authorizer, err := serviceidentity.NewTrustedCluster(transportprofile.TrustedCluster, secretBrokerSPIFFEID, []serviceidentity.Binding{{
		CallerSPIFFEID: runtimeControllerSPIFFEID, FullMethod: method, OperationID: runtimeProjectionOperation,
		Permission: runtimeProjectionOperation, ActorMode: serviceidentity.ServiceActor,
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster,
		serviceidentity.CallerMetadataKey, runtimeControllerSPIFFEID))
	if _, err := runtimeProjectionAuthority(ctx, method, runtimeProjectionOperation); err == nil {
		t.Fatal("unverified request metadata became authority")
	}
	_, err = authorizer.UnaryServerInterceptor()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, func(ctx context.Context, _ any) (any, error) {
		selector, err := runtimeProjectionAuthority(ctx, method, runtimeProjectionOperation)
		if err != nil || !proto.Equal(selector, &cp.CredentialProjectionAuthority{RpcProfile: transportprofile.TrustedCluster,
			CallerWorkloadId: runtimeControllerWorkloadID, CallerFullMethod: method}) {
			t.Fatal("trusted selector contains caller-assigned owner fields")
		}
		if _, err := runtimeProjectionAuthority(ctx, method, assistantProjectionOperation); err == nil {
			t.Fatal("admission for another operation accepted")
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTrustedProjectionManifestUsesOnlyOwnerSnapshot(t *testing.T) {
	method := sb.RuntimeCredentialProjectionService_MaterializeRuntimeCredentials_FullMethodName
	selector := &cp.CredentialProjectionAuthority{RpcProfile: transportprofile.TrustedCluster,
		CallerWorkloadId: runtimeControllerWorkloadID, CallerFullMethod: method}
	expires := timestamppb.New(time.Now().UTC().Add(30 * time.Second))
	request := &sb.MaterializeRuntimeCredentialsRequest{Generation: 2, RuntimeRevisionDigest: strings.Repeat("a", 64)}
	owner := &cp.CredentialProjectionAuthority{RpcProfile: transportprofile.TrustedCluster,
		CallerWorkloadId: runtimeControllerWorkloadID, CallerFullMethod: method,
		ActorId: "c20ac176-c0ca-499f-91a4-6fc65c4ef30e", TenantId: "71adb021-5229-4903-9f75-9fd34797665a",
		ProjectId: "e92277a1-c5d0-4d40-af73-54c34a256ef5", SourceRevision: 2, SourceDigestSha256: request.RuntimeRevisionDigest,
		CallerCredentialRevision: 1, ExpiresAt: expires}
	resolved := &cp.ResolveRuntimeCredentialProjectionResponse{Authority: owner, ExpiresAt: expires, ProviderCredential: &cp.ProviderCredentialBinding{}}
	manifest, err := runtimeProjectionManifest(selector, request, resolved)
	if err != nil || manifest.Authority.ActorID != owner.ActorId || manifest.Authority.ProofJTI != "" || manifest.Authority.RPCProfile != transportprofile.TrustedCluster {
		t.Fatal("owner snapshot was not preserved")
	}
	for name, mutate := range map[string]func(*cp.ResolveRuntimeCredentialProjectionResponse){
		"missing owner": func(r *cp.ResolveRuntimeCredentialProjectionResponse) { r.Authority = nil },
		"fake proof":    func(r *cp.ResolveRuntimeCredentialProjectionResponse) { r.Authority.ProofJti = "fake-proof" },
		"generation":    func(r *cp.ResolveRuntimeCredentialProjectionResponse) { r.Authority.SourceRevision++ },
		"profile":       func(r *cp.ResolveRuntimeCredentialProjectionResponse) { r.Authority.RpcProfile = "" },
		"expiry":        func(r *cp.ResolveRuntimeCredentialProjectionResponse) { r.Authority.ExpiresAt = nil },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := proto.Clone(resolved).(*cp.ResolveRuntimeCredentialProjectionResponse)
			mutate(candidate)
			if _, err := runtimeProjectionManifest(selector, request, candidate); err == nil {
				t.Fatal("incomplete owner snapshot accepted")
			}
		})
	}
}
