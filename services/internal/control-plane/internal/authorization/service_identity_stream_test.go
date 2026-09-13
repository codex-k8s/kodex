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

type streamOwnerFixture struct{ digest string }

func (owner *streamOwnerFixture) ResolveProofAuthority(_ context.Context, input platformrepo.ProofPrincipalInput) (platformrepo.ProofAuthority, error) {
	owner.digest = input.RequestDigestSHA256
	return platformrepo.ProofAuthority{ActorID: "resolved-actor", OrganizationID: "resolved-org"}, nil
}
func (*streamOwnerFixture) ResolveServiceCredentialGeneration(context.Context, string) (uint64, error) {
	return 7, nil
}
func (*streamOwnerFixture) VerifyToken(context.Context, string) (oidcverifier.Principal, error) {
	return oidcverifier.Principal{}, errors.New("unexpected user verification")
}

type streamRevocations struct{ revoked bool }

func (state *streamRevocations) CheckPeer(context.Context, serviceidentity.PeerIdentity) error {
	if state.revoked {
		return errors.New("revoked")
	}
	return nil
}

type streamTransportFixture struct {
	grpc.ServerStream
	ctx  context.Context
	sent int
}

func (stream *streamTransportFixture) Context() context.Context { return stream.ctx }
func (*streamTransportFixture) RecvMsg(any) error               { return nil }
func (stream *streamTransportFixture) SendMsg(any) error        { stream.sent++; return nil }

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
	uri, _ := url.Parse(caller)
	cert := &x509.Certificate{Raw: []byte("synthetic certificate"), URIs: []*url.URL{uri}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Minute), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	ctx := peer.NewContext(t.Context(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}}})
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
