package grpc

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	av1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/providercredential"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type catalogMaterializerFixture struct {
	cleanupProviderCredentialMaterializerStub
	calls           int
	failure         providercredential.ModelCatalogFailure
	deadline        time.Time
	account, method string
	descriptor      kubernetesstore.ProviderCredentialDescriptor
}

func (fixture *catalogMaterializerFixture) ObserveModelCatalog(ctx context.Context, account string, descriptor kubernetesstore.ProviderCredentialDescriptor, method string) (providercredential.ModelCatalog, error) {
	fixture.calls++
	fixture.account, fixture.method, fixture.descriptor = account, method, descriptor
	fixture.deadline, _ = ctx.Deadline()
	result := providercredential.ModelCatalog{ObservedAt: time.Now().UTC(), Failure: fixture.failure}
	if fixture.failure == providercredential.CatalogFailureNone {
		result.Source = providercredential.CatalogRemoteAPI
		result.Models = []providercredential.CatalogModel{{ID: "fixture-model", DefaultReasoningEffort: "medium", ReasoningEfforts: []string{"low", "medium"}}}
	}
	return result, nil
}

type catalogVerifierFixture struct {
	av1.AuthorizationVerifierServiceClient
	expectedDigest string
	mutate         func(*av1.VerifiedAuthorizationContext)
	deny           bool
}

func (fixture *catalogVerifierFixture) VerifyAuthorizationContext(_ context.Context, request *av1.VerifyAuthorizationContextRequest, _ ...googlegrpc.CallOption) (*av1.VerifyAuthorizationContextResponse, error) {
	if fixture.deny || request.GetCompactJws() != "fixture-proof" || request.GetObservedRequestDigestSha256() != fixture.expectedDigest {
		return nil, status.Error(codes.PermissionDenied, "fixture owner proof rejected")
	}
	verified, _ := projectionAuthorityFixtures()
	verified.ContractVersion, verified.AuthorityAbiVersion, verified.RequestBindingMode = 1, internalrpcauth.AuthorityABIVersion, internalrpcauth.RequestBindingUnary
	verified.Authority.Project = nil
	verified.Audience = credentialProjectionAudience
	verified.Subject = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
	verified.CallerSpiffeId, verified.CallerWorkloadId = verified.Subject, "control-plane"
	verified.TargetSpiffeId, verified.TargetWorkloadId = secretBrokerSPIFFEID, secretBrokerWorkloadID
	verified.FullMethod = cp.ProviderCredentialMaterializerService_ObserveProviderModelCatalog_FullMethodName
	verified.OperationId, verified.Permission = providerCatalogOperation, providerCatalogOperation
	verified.Jti = "1037ea25-1553-4082-bc32-ccae01178ec4"
	verified.CallerCredentialRevision = 1
	verified.RequestDigestSha256 = fixture.expectedDigest
	if fixture.mutate != nil {
		fixture.mutate(verified)
	}
	return &av1.VerifyAuthorizationContextResponse{Context: verified}, nil
}

func catalogRequestFixture() *cp.ObserveProviderModelCatalogRequest {
	return &cp.ObserveProviderModelCatalogRequest{TaskRef: "pct_fixture0001", ClaimantId: "controller-fixture", ClaimGeneration: 2, Fence: "fence-fixture", AccountRef: "pacc_fixture01", AccountVersion: 3, CredentialRevisionRef: "pcrev_fixture01", CredentialRevision: 4,
		Credential:          &cp.ProviderCredentialDescriptor{SecretName: "provider-fixture", SecretUid: "f4b9c3dc-69c9-44f2-b010-d75d5e62c06e", SecretResourceVersion: "10", ContentSha256: strings.Repeat("a", 64)},
		AuthorizationMethod: cp.ProviderAuthorizationMethod_PROVIDER_AUTHORIZATION_METHOD_API_KEY, ExpiresAt: timestamppb.New(time.Now().UTC().Add(5 * time.Second))}
}

func catalogPeerContext(t *testing.T) context.Context {
	t.Helper()
	identity, err := url.Parse("spiffe://kodex.local/ns/kodex-system/sa/control-plane")
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{URIs: []*url.URL{identity}, Raw: []byte("synthetic-peer-certificate")}
	ctx := peer.NewContext(t.Context(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{PeerCertificates: []*x509.Certificate{certificate}, VerifiedChains: [][]*x509.Certificate{{certificate}}}}})
	return metadata.NewIncomingContext(ctx, metadata.Pairs(authorityclient.AuthorizationMetadata, "fixture-proof"))
}

func TestProviderCatalogMiddlewareRejectsDetachedAuthorityBeforeCredentialRead(t *testing.T) {
	for _, mode := range []string{"allowed", "missing_mtls", "missing_bearer", "revoked", "changed_payload", "wrong_operation", "wrong_permission", "foreign_caller", "project", "expired", "binding", "continuation", "unknown_payload", "unknown_auth"} {
		t.Run(mode, func(t *testing.T) {
			request := catalogRequestFixture()
			raw, _ := proto.MarshalOptions{Deterministic: true}.Marshal(request)
			sum := sha256.Sum256(raw)
			verifier := &catalogVerifierFixture{expectedDigest: hex.EncodeToString(sum[:])}
			ctx := catalogPeerContext(t)
			switch mode {
			case "missing_mtls":
				ctx = metadata.NewIncomingContext(t.Context(), metadata.Pairs(authorityclient.AuthorizationMetadata, "fixture-proof"))
			case "missing_bearer":
				ctx = metadata.NewIncomingContext(ctx, metadata.MD{})
			case "revoked":
				verifier.deny = true
			case "changed_payload":
				request.AccountRef = "pacc_foreign01"
			case "wrong_operation":
				verifier.mutate = func(v *av1.VerifiedAuthorizationContext) { v.OperationId = "other" }
			case "wrong_permission":
				verifier.mutate = func(v *av1.VerifiedAuthorizationContext) { v.Permission = "organization.manage" }
			case "foreign_caller":
				verifier.mutate = func(v *av1.VerifiedAuthorizationContext) { v.CallerWorkloadId = "agent-runner" }
			case "project":
				verifier.mutate = func(v *av1.VerifiedAuthorizationContext) {
					v.Authority.Project = projectionIdentity("e92277a1-c5d0-4d40-af73-54c34a256ef5", "fixture-project", 'c')
				}
			case "expired":
				verifier.mutate = func(v *av1.VerifiedAuthorizationContext) { v.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second)) }
			case "binding":
				verifier.mutate = func(v *av1.VerifiedAuthorizationContext) { v.RequestBindingMode = internalrpcauth.RequestBindingStream }
			case "continuation":
				verifier.mutate = func(v *av1.VerifiedAuthorizationContext) { v.Continuation = &av1.ContinuationLineage{} }
			case "unknown_payload":
				request.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
			case "unknown_auth":
				request.AuthorizationMethod = cp.ProviderAuthorizationMethod(999)
			}
			if mode == "unknown_payload" || mode == "unknown_auth" {
				raw, _ = proto.MarshalOptions{Deterministic: true}.Marshal(request)
				sum = sha256.Sum256(raw)
				verifier.expectedDigest = hex.EncodeToString(sum[:])
			}
			materializer := &catalogMaterializerFixture{failure: providercredential.CatalogFailureNone}
			server := &Server{providerCredentials: materializer}
			response, err := authorityclient.VerifierUnaryServerInterceptor(verifier)(ctx, request, &googlegrpc.UnaryServerInfo{FullMethod: cp.ProviderCredentialMaterializerService_ObserveProviderModelCatalog_FullMethodName}, func(ctx context.Context, _ any) (any, error) { return server.ObserveProviderModelCatalog(ctx, request) })
			if mode != "allowed" {
				if err == nil || materializer.calls != 0 {
					t.Fatal("detached authority reached credential read")
				}
				return
			}
			if err != nil || response.(*cp.ObserveProviderModelCatalogResponse).GetFailure() != cp.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_NONE || materializer.calls != 1 || materializer.account != request.AccountRef || materializer.descriptor.SecretResourceVersion != request.Credential.SecretResourceVersion || materializer.method != providercredential.CatalogMethodAPIKey || materializer.deadline.After(request.ExpiresAt.AsTime()) {
				t.Fatal("exact catalog binding was not preserved")
			}
		})
	}
}
