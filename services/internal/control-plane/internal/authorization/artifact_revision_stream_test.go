package authorization

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"io"
	"net"
	"net/url"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/rpcprincipal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

func revisionStreamPeer(t *testing.T, caller string) context.Context {
	t.Helper()
	uri, err := url.Parse(caller)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{Raw: []byte("synthetic user certificate"), URIs: []*url.URL{uri}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Minute), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	return peer.NewContext(t.Context(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}}})
}

type revisionWireContext struct {
	grpc.ServerStream
	ctx context.Context
}

func (stream *revisionWireContext) Context() context.Context { return stream.ctx }

// Оснастка использует тот же проверенный синтетический TLS peer, что существующие
// stream tests; реальные credentials, owner PostgreSQL и object storage не нужны.
func revisionStreamFixture(t *testing.T, actor serviceidentity.ActorMode) (AdmissionAuthorizer, *rpcprincipal.Service, *streamOwnerFixture, *streamRevocations, context.Context) {
	t.Helper()
	const caller = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
	const target = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
	method := cp.PlatformCommandService_DownloadArtifactRevision_FullMethodName
	revocations := &streamRevocations{}
	auth, err := serviceidentity.New(target, []serviceidentity.Binding{{CallerSPIFFEID: caller, FullMethod: method, OperationID: "platform.command.artifact-revisions.download", Permission: "platform.command.artifact-revisions.download", ActorMode: actor}}, revocations)
	if err != nil {
		t.Fatal(err)
	}
	owner := &streamOwnerFixture{user: true}
	resolver, err := rpcprincipal.New(owner, owner)
	if err != nil {
		t.Fatal(err)
	}
	ctx := revisionStreamPeer(t, caller)
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("x-kodex-rpc-profile", "service-v1", "authorization", "Bearer user-credential"))
	return auth, resolver, owner, revocations, ctx
}

func TestArtifactRevisionStreamBindsExactRequestAndRevokes(t *testing.T) {
	method := cp.PlatformCommandService_DownloadArtifactRevision_FullMethodName
	for _, purpose := range []cp.ArtifactDownloadPurpose{cp.ArtifactDownloadPurpose_ARTIFACT_DOWNLOAD_PURPOSE_DOWNLOAD, cp.ArtifactDownloadPurpose_ARTIFACT_DOWNLOAD_PURPOSE_PREVIEW} {
		t.Run(purpose.String(), func(t *testing.T) {
			auth, resolver, owner, revocations, ctx := revisionStreamFixture(t, serviceidentity.UserActor)
			transport := &streamTransportFixture{ctx: ctx}
			request := &cp.DownloadArtifactRevisionRequest{ArtifactRef: "art_abcdefghijk", RevisionRef: "arv_abcdefghijk", Purpose: purpose}
			err := ServiceIdentityStream(auth, resolver)(nil, transport, &grpc.StreamServerInfo{FullMethod: method, IsServerStream: true}, func(_ any, stream grpc.ServerStream) error {
				if status.Code(stream.SendMsg(&cp.DownloadArtifactRevisionResponse{})) != codes.PermissionDenied {
					t.Fatal("send before request accepted")
				}
				if err := stream.RecvMsg(request); err != nil {
					return err
				}
				raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(request)
				if err != nil {
					return err
				}
				digest := sha256.Sum256(raw)
				principal, err := Principal(stream.Context(), method)
				if err != nil || principal.ActorID != "resolved-actor" || owner.digest != hex.EncodeToString(digest[:]) {
					t.Fatal("exact revision request principal missing")
				}
				if _, err := Principal(stream.Context(), cp.PlatformCommandService_DownloadArtifact_FullMethodName); err == nil {
					t.Fatal("principal reused by current-content method")
				}
				if status.Code(stream.RecvMsg(request)) != codes.InvalidArgument {
					t.Fatal("second request accepted")
				}
				if err := stream.SendMsg(&cp.DownloadArtifactRevisionResponse{Data: []byte("old")}); err != nil {
					return err
				}
				revocations.revoked = true
				if status.Code(stream.SendMsg(&cp.DownloadArtifactRevisionResponse{})) != codes.PermissionDenied {
					t.Fatal("revoked revision stream sent chunk")
				}
				return nil
			})
			if err != nil || transport.sent != 1 {
				t.Fatalf("revision stream rejected: sent=%d code=%s", transport.sent, status.Code(err))
			}
		})
	}
}

func TestArtifactRevisionStreamRejectsWrongBoundary(t *testing.T) {
	method := cp.PlatformCommandService_DownloadArtifactRevision_FullMethodName
	for _, name := range []string{"client-stream", "bidi-stream", "unary-shape", "unknown-method", "service-actor", "missing-credential", "foreign-peer", "wrong-profile"} {
		t.Run(name, func(t *testing.T) {
			actor := serviceidentity.UserActor
			if name == "service-actor" {
				actor = serviceidentity.ServiceActor
			}
			auth, resolver, owner, _, ctx := revisionStreamFixture(t, actor)
			info := &grpc.StreamServerInfo{FullMethod: method, IsServerStream: true}
			switch name {
			case "client-stream":
				info.IsClientStream, info.IsServerStream = true, false
			case "bidi-stream":
				info.IsClientStream = true
			case "unary-shape":
				info.IsServerStream = false
			case "unknown-method":
				info.FullMethod += "Shadow"
			case "missing-credential":
				ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("x-kodex-rpc-profile", "service-v1"))
			case "foreign-peer":
				ctx = peer.NewContext(ctx, &peer.Peer{})
			case "wrong-profile":
				ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("x-kodex-rpc-profile", "legacy", "authorization", "Bearer user-credential"))
			}
			transport := &streamTransportFixture{ctx: ctx}
			err := ServiceIdentityStream(auth, resolver)(nil, transport, info, func(_ any, stream grpc.ServerStream) error {
				return stream.RecvMsg(&cp.DownloadArtifactRevisionRequest{ArtifactRef: "art_abcdefghijk", RevisionRef: "arv_abcdefghijk"})
			})
			if err == nil || transport.sent != 0 || owner.digest != "" {
				t.Fatal("invalid revision stream reached domain owner")
			}
		})
	}
}

type revisionWireServer struct {
	cp.UnimplementedPlatformCommandServiceServer
}

func (*revisionWireServer) DownloadArtifactRevision(request *cp.DownloadArtifactRevisionRequest, stream cp.PlatformCommandService_DownloadArtifactRevisionServer) error {
	principal, err := Principal(stream.Context(), cp.PlatformCommandService_DownloadArtifactRevision_FullMethodName)
	if err != nil || principal.ActorID != "resolved-actor" || request.ArtifactRef != "art_abcdefghijk" || request.RevisionRef != "arv_abcdefghijk" {
		return status.Error(codes.PermissionDenied, "synthetic exact request rejected")
	}
	if err := stream.Send(&cp.DownloadArtifactRevisionResponse{FileName: "fixture.txt", MediaType: "text/plain", SizeBytes: 3}); err != nil {
		return err
	}
	return stream.Send(&cp.DownloadArtifactRevisionResponse{Data: []byte("old")})
}

func TestArtifactRevisionGeneratedWireThroughServiceStream(t *testing.T) {
	auth, resolver, _, _, peerCtx := revisionStreamFixture(t, serviceidentity.UserActor)
	verifiedPeer, _ := peer.FromContext(peerCtx)
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.ChainStreamInterceptor(func(s any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return handler(s, &revisionWireContext{ServerStream: stream, ctx: peer.NewContext(stream.Context(), verifiedPeer)})
	}, ServiceIdentityStream(auth, resolver)))
	cp.RegisterPlatformCommandServiceServer(server, &revisionWireServer{})
	t.Cleanup(server.Stop)
	t.Cleanup(func() { _ = listener.Close() })
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient("passthrough:///synthetic", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	boundedCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ctx := metadata.NewOutgoingContext(boundedCtx, metadata.Pairs("x-kodex-rpc-profile", "service-v1", "authorization", "Bearer user-credential"))
	stream, err := cp.NewPlatformCommandServiceClient(conn).DownloadArtifactRevision(ctx, &cp.DownloadArtifactRevisionRequest{ArtifactRef: "art_abcdefghijk", RevisionRef: "arv_abcdefghijk", Purpose: cp.ArtifactDownloadPurpose_ARTIFACT_DOWNLOAD_PURPOSE_DOWNLOAD})
	if err != nil {
		t.Fatal(err)
	}
	first, err := stream.Recv()
	if err != nil || first.GetSizeBytes() != 3 || len(first.GetData()) != 0 {
		t.Fatalf("revision metadata rejected: code=%s", status.Code(err))
	}
	second, err := stream.Recv()
	if err != nil || string(second.GetData()) != "old" {
		t.Fatalf("revision bytes rejected: code=%s", status.Code(err))
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("unexpected stream terminal: code=%s", status.Code(err))
	}
}

func TestArtifactRevisionTrustedClusterRequiresExactCallerAndCredential(t *testing.T) {
	const caller = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
	const target = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
	method := cp.PlatformCommandService_DownloadArtifactRevision_FullMethodName
	for _, name := range []string{"exact", "foreign-caller", "missing-credential", "rejected-credential", "unknown-method"} {
		t.Run(name, func(t *testing.T) {
			auth, err := serviceidentity.NewTrustedCluster(serviceidentity.TrustedClusterProfile, target, []serviceidentity.Binding{{CallerSPIFFEID: caller, FullMethod: method, OperationID: "platform.command.artifact-revisions.download", Permission: "platform.command.artifact-revisions.download", ActorMode: serviceidentity.UserActor}})
			if err != nil {
				t.Fatal(err)
			}
			owner := &streamOwnerFixture{user: name != "rejected-credential"}
			resolver, err := rpcprincipal.New(owner, owner)
			if err != nil {
				t.Fatal(err)
			}
			md := metadata.Pairs("x-kodex-rpc-profile", serviceidentity.TrustedClusterProfile, "x-kodex-trusted-caller", caller, "authorization", "Bearer user-credential")
			info := &grpc.StreamServerInfo{FullMethod: method, IsServerStream: true}
			switch name {
			case "foreign-caller":
				md.Set("x-kodex-trusted-caller", target)
			case "missing-credential":
				md.Delete("authorization")
			case "unknown-method":
				info.FullMethod += "Shadow"
			}
			transport := &streamTransportFixture{ctx: metadata.NewIncomingContext(t.Context(), md)}
			err = ServiceIdentityStream(auth, resolver)(nil, transport, info, func(_ any, stream grpc.ServerStream) error {
				if err := stream.RecvMsg(&cp.DownloadArtifactRevisionRequest{ArtifactRef: "art_abcdefghijk", RevisionRef: "arv_abcdefghijk"}); err != nil {
					return err
				}
				return stream.SendMsg(&cp.DownloadArtifactRevisionResponse{Data: []byte("old")})
			})
			if name == "exact" {
				if err != nil || transport.sent != 1 || len(owner.digest) != 64 {
					t.Fatalf("exact trusted revision stream rejected: code=%s", status.Code(err))
				}
			} else if err == nil || transport.sent != 0 || owner.digest != "" {
				t.Fatal("invalid trusted revision stream reached domain owner")
			}
		})
	}
}
