package authorization

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/url"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/rpcprincipal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// serviceStreamPeer constructs a synthetic verified TLS peer with caller URI and scenario-specific
// certificate bytes for in-memory stream tests, without a network TLS handshake.
// serviceStreamPeer создаёт синтетический проверенный TLS peer с caller URI и certificate bytes
// конкретного сценария для in-memory stream tests без сетевого TLS handshake.
func serviceStreamPeer(t *testing.T, caller, rawCertificate string) context.Context {
	t.Helper()
	uri, err := url.Parse(caller)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{Raw: []byte(rawCertificate), URIs: []*url.URL{uri}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Minute), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	return peer.NewContext(t.Context(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}}})
}

// userServiceStreamFixture builds one exact service-v1 binding with a synthetic verified peer,
// fake owner and user credential; optional project metadata remains a locator resolved by the owner.
// userServiceStreamFixture создаёт одну точную service-v1 binding с синтетическим verified peer,
// fake owner и user credential; необязательные project metadata остаются locator владельца.
func userServiceStreamFixture(t *testing.T, binding serviceidentity.Binding, project string) (AdmissionAuthorizer, *rpcprincipal.Service, *streamOwnerFixture, *streamRevocations, context.Context) {
	t.Helper()
	caller := binding.CallerSPIFFEID
	const target = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
	revocations := &streamRevocations{}
	auth, err := serviceidentity.New(target, []serviceidentity.Binding{binding}, revocations)
	if err != nil {
		t.Fatal(err)
	}
	owner := &streamOwnerFixture{user: true}
	resolver, err := rpcprincipal.New(owner, owner)
	if err != nil {
		t.Fatal(err)
	}
	ctx := serviceStreamPeer(t, caller, "synthetic user certificate")
	md := metadata.Pairs("x-kodex-rpc-profile", "service-v1", "authorization", "Bearer user-credential")
	if project != "" {
		md.Set("x-kodex-project-ref", project)
	}
	ctx = metadata.NewIncomingContext(ctx, md)
	return auth, resolver, owner, revocations, ctx
}

// streamOwnerFixture records request digest/project and supplies synthetic owner identity,
// workload generation and optional user verification without a database.
// streamOwnerFixture учитывает request digest и project и предоставляет синтетические owner
// identity, workload generation и необязательную user verification без базы данных.
type streamOwnerFixture struct {
	digest, project string
	user            bool
}

// ResolveProofAuthority captures exact request pins and returns a fixed synthetic
// actor/organization without assigning authority from request payload fields.
// ResolveProofAuthority фиксирует точные request pins и возвращает фиксированные синтетические
// actor и organization без назначения полномочий из полей request payload.
func (owner *streamOwnerFixture) ResolveProofAuthority(_ context.Context, input platformrepo.ProofPrincipalInput) (platformrepo.ProofAuthority, error) {
	owner.digest = input.RequestDigestSHA256
	owner.project = input.ProjectRef
	return platformrepo.ProofAuthority{ActorID: "resolved-actor", OrganizationID: "resolved-org"}, nil
}

// ResolveServiceCredentialGeneration returns the fixed synthetic workload credential generation
// used to check stream principal binding.
// ResolveServiceCredentialGeneration возвращает фиксированное синтетическое поколение workload
// credential для проверки привязки stream principal.
func (*streamOwnerFixture) ResolveServiceCredentialGeneration(context.Context, string) (uint64, error) {
	return 7, nil
}

// VerifyToken accepts the synthetic user only when enabled by the fixture, otherwise returning a
// controlled verification failure.
// VerifyToken принимает синтетического пользователя только при включении fixture, иначе возвращая
// контролируемый отказ verification.
func (owner *streamOwnerFixture) VerifyToken(context.Context, string) (oidcverifier.Principal, error) {
	if !owner.user {
		return oidcverifier.Principal{}, errors.New("unexpected user verification")
	}
	return oidcverifier.Principal{Subject: "external-user", OrganizationID: "external-org", SessionRevision: 9}, nil
}

// TestServiceStreamBindsArtifactMetadataAndAcceptsBodyChunks checks first artifact metadata
// resolves exact project/digest before later upload chunks and a response pass through the wrapped
// stream.
// TestServiceStreamBindsArtifactMetadataAndAcceptsBodyChunks проверяет разрешение точных project и
// digest по первой artifact metadata до следующих upload chunks и ответа через stream оболочку.
func TestServiceStreamBindsArtifactMetadataAndAcceptsBodyChunks(t *testing.T) {
	const caller = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
	method := cp.PlatformCommandService_UploadArtifact_FullMethodName
	auth, resolver, owner, _, ctx := userServiceStreamFixture(t, serviceidentity.Binding{CallerSPIFFEID: caller, FullMethod: method, OperationID: "artifact.upload", Permission: "artifact.upload", ActorMode: serviceidentity.UserActor, ProjectRequired: true}, "prj_abcdefghijk")
	transport := &streamTransportFixture{ctx: ctx}
	err := ServiceIdentityStream(auth, resolver)(nil, transport, &grpc.StreamServerInfo{FullMethod: method, IsClientStream: true}, func(_ any, stream grpc.ServerStream) error {
		if err := stream.RecvMsg(&cp.UploadArtifactRequest{Part: &cp.UploadArtifactRequest_Metadata{Metadata: &cp.UploadArtifactMetadata{ProjectRef: "prj_abcdefghijk", FileName: "fixture.txt", SizeBytes: 1}}}); err != nil {
			return err
		}
		if _, err := Principal(stream.Context(), method); err != nil || owner.project != "prj_abcdefghijk" || len(owner.digest) != 64 {
			t.Fatal("upload metadata principal missing")
		}
		if err := stream.RecvMsg(&cp.UploadArtifactRequest{Part: &cp.UploadArtifactRequest_Chunk{Chunk: []byte("x")}}); err != nil {
			return err
		}
		return stream.SendMsg(&cp.UploadArtifactResponse{})
	})
	if err != nil || transport.sent != 1 {
		t.Fatalf("artifact upload stream failed: sent=%d err=%v", transport.sent, err)
	}
}

// TestServiceStreamBindsAgentAvatarMetadata checks avatar metadata installs the user/project
// principal and permits one response using the registered avatar upload method.
// TestServiceStreamBindsAgentAvatarMetadata проверяет установку user и project principal по avatar
// metadata и допуск одного ответа зарегистрированного метода avatar upload.
func TestServiceStreamBindsAgentAvatarMetadata(t *testing.T) {
	const caller = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
	method := cp.PlatformCommandService_UploadAgentAvatar_FullMethodName
	auth, resolver, owner, _, ctx := userServiceStreamFixture(t, serviceidentity.Binding{CallerSPIFFEID: caller, FullMethod: method, OperationID: "platform.command.agents.avatar.upload", Permission: "agent.avatar.manage", ActorMode: serviceidentity.UserActor, ProjectRequired: true}, "prj_abcdefghijk")
	transport := &streamTransportFixture{ctx: ctx}
	err := ServiceIdentityStream(auth, resolver)(nil, transport, &grpc.StreamServerInfo{FullMethod: method, IsClientStream: true}, func(_ any, stream grpc.ServerStream) error {
		if err := stream.RecvMsg(&cp.UploadAgentAvatarRequest{Part: &cp.UploadAgentAvatarRequest_Metadata{Metadata: &cp.UploadAgentAvatarMetadata{ProjectRef: "prj_abcdefghijk", AgentRef: "agt_abcdefghijk", FileName: "avatar.png", MediaType: "image/png", SizeBytes: 1}}}); err != nil {
			return err
		}
		if _, err := Principal(stream.Context(), method); err != nil || owner.project != "prj_abcdefghijk" || len(owner.digest) != 64 {
			t.Fatal("agent avatar upload metadata principal missing")
		}
		return stream.SendMsg(&cp.UploadAgentAvatarResponse{})
	})
	if err != nil || transport.sent != 1 {
		t.Fatalf("agent avatar upload stream failed: sent=%d err=%v", transport.sent, err)
	}
}

// streamRevocations holds the synthetic peer-revocation flag used to deny further stream messages
// after initial admission.
// streamRevocations хранит синтетический признак peer revocation для запрета следующих stream
// сообщений после исходного admission.
type streamRevocations struct{ revoked bool }

// CheckPeer rejects only the fixture revocation flag without reading a live revocation registry or
// certificate source.
// CheckPeer отклоняет только установленный fixture revocation flag без чтения живого реестра
// отзывов либо certificate source.
func (state *streamRevocations) CheckPeer(context.Context, serviceidentity.PeerIdentity) error {
	if state.revoked {
		return errors.New("revoked")
	}
	return nil
}

// streamTransportFixture provides a synthetic context and sent-message count while embedding
// unused transport methods for interceptor tests.
// streamTransportFixture предоставляет синтетический контекст и счётчик отправленных сообщений,
// встраивая неиспользуемые transport методы для interceptor tests.
type streamTransportFixture struct {
	grpc.ServerStream
	ctx  context.Context
	sent int
}

// Context returns the synthetic incoming peer/metadata context supplied to the transport fixture.
// Context возвращает синтетический incoming peer и metadata context, переданный transport fixture.
func (stream *streamTransportFixture) Context() context.Context { return stream.ctx }

// RecvMsg simulates a successful transport receive without overwriting the request already
// supplied by the test.
// RecvMsg имитирует успешный transport receive без перезаписи запроса, уже переданного тестом.
func (*streamTransportFixture) RecvMsg(any) error { return nil }

// SendMsg increments the synthetic send count without delivering content to a network client.
// SendMsg увеличивает синтетический send count без доставки содержимого сетевому клиенту.
func (stream *streamTransportFixture) SendMsg(any) error { stream.sent++; return nil }

// TestServiceStreamBindsInitialRequestAndRevokesBeforeNextChunk checks runtime streaming requires
// the first request, pins its digest/generation and refuses sending after peer revocation.
// TestServiceStreamBindsInitialRequestAndRevokesBeforeNextChunk проверяет требование первого
// запроса runtime streaming, привязку его digest и generation и запрет отправки после peer
// revocation.
func TestServiceStreamBindsInitialRequestAndRevokesBeforeNextChunk(t *testing.T) {
	const caller = "spiffe://kodex.local/ns/kodex-system/sa/runtime-controller"
	const target = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
	method := cp.RuntimeWorkService_StreamExecutionArtifact_FullMethodName
	revocations := &streamRevocations{}
	auth, err := serviceidentity.New(target, []serviceidentity.Binding{{CallerSPIFFEID: caller, FullMethod: method, OperationID: "artifact.stream", Permission: "artifact.stream", ActorMode: serviceidentity.ServiceActor}}, revocations)
	if err != nil {
		t.Fatal(err)
	}
	owner := &streamOwnerFixture{}
	resolver, err := rpcprincipal.New(owner, owner)
	if err != nil {
		t.Fatal(err)
	}
	ctx := serviceStreamPeer(t, caller, "synthetic certificate")
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("x-kodex-rpc-profile", "service-v1"))
	transport := &streamTransportFixture{ctx: ctx}
	err = ServiceIdentityStream(auth, resolver)(nil, transport, &grpc.StreamServerInfo{FullMethod: method, IsServerStream: true}, func(_ any, stream grpc.ServerStream) error {
		if err := stream.SendMsg(nil); status.Code(err) != codes.PermissionDenied {
			t.Fatal("send before request accepted")
		}
		if err := stream.RecvMsg(&cp.StreamExecutionArtifactRequest{LeaseRef: "synthetic-lease"}); err != nil {
			return err
		}
		principal, err := Principal(stream.Context(), method)
		if err != nil || principal.CredentialRevision != 7 || len(owner.digest) != 64 {
			t.Fatal("request principal missing")
		}
		if err := stream.SendMsg(&cp.StreamExecutionArtifactResponse{}); err != nil {
			return err
		}
		revocations.revoked = true
		if err := stream.SendMsg(&cp.StreamExecutionArtifactResponse{}); status.Code(err) != codes.PermissionDenied {
			t.Fatal("revoked stream sent chunk")
		}
		return nil
	})
	if err != nil || transport.sent != 1 {
		t.Fatal("unexpected stream result")
	}
}

// TestServiceStreamBindsDownloadArtifactUser checks registered user artifact download resolves the
// owner actor and initial request digest before sending one response.
// TestServiceStreamBindsDownloadArtifactUser проверяет разрешение owner actor и digest первого
// запроса зарегистрированного user artifact download до отправки одного ответа.
func TestServiceStreamBindsDownloadArtifactUser(t *testing.T) {
	const caller = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
	method := cp.PlatformCommandService_DownloadArtifact_FullMethodName
	auth, resolver, owner, _, ctx := userServiceStreamFixture(t, serviceidentity.Binding{CallerSPIFFEID: caller, FullMethod: method, OperationID: "artifact.download", Permission: "artifact.download", ActorMode: serviceidentity.UserActor}, "")
	transport := &streamTransportFixture{ctx: ctx}
	err := ServiceIdentityStream(auth, resolver)(nil, transport, &grpc.StreamServerInfo{FullMethod: method, IsServerStream: true}, func(_ any, stream grpc.ServerStream) error {
		if err := stream.RecvMsg(&cp.DownloadArtifactRequest{ArtifactRef: "art_abcdefghijk", Purpose: cp.ArtifactDownloadPurpose_ARTIFACT_DOWNLOAD_PURPOSE_DOWNLOAD}); err != nil {
			return err
		}
		principal, err := Principal(stream.Context(), method)
		if err != nil || principal.ActorID != "resolved-actor" || len(owner.digest) != 64 {
			t.Fatal("download principal missing")
		}
		return stream.SendMsg(&cp.DownloadArtifactResponse{})
	})
	if err != nil || transport.sent != 1 {
		t.Fatalf("artifact download stream failed: sent=%d err=%v", transport.sent, err)
	}
}
