package platform

import (
	"context"
	"crypto/sha256"
	"errors"
	"net"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/authorization"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/rpcprincipal"
	transport "github.com/codex-k8s/kodex/services/internal/control-plane/internal/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

// personalRuntimeRPCAdmission supplies one synthetic admitted peer for a buffer-only RPC fixture.
// The real profile/hash/principal interceptor and owner database remain active; this fixture
// does not establish certificate validation, a TLS handshake or user JWT acceptance.
// personalRuntimeRPCAdmission задаёт один synthetic admitted peer для buffer-only RPC fixture.
// Настоящие profile/hash/principal interceptor и owner DB работают; fixture не подтверждает
// проверку сертификата, TLS handshake либо пользовательский JWT.
type personalRuntimeRPCAdmission struct{}

// RPCProfile pins the server-selected synthetic peer profile, never a browser-selected profile.
// RPCProfile закрепляет выбранный сервером профиль synthetic peer, не browser-selected profile.
func (personalRuntimeRPCAdmission) RPCProfile() string { return "service-v1" }

// Admit restricts the synthetic peer to exactly the new Begin method and server-owned operation.
// Admit ограничивает synthetic peer ровно новым Begin method и server-owned operation.
func (personalRuntimeRPCAdmission) Admit(_ context.Context, method string) (serviceidentity.Admission, error) {
	if method != cp.PersonalIntegrationCredentialService_BeginPersonalIntegrationInvocation_FullMethodName {
		return serviceidentity.Admission{}, status.Error(codes.PermissionDenied, "synthetic peer method denied")
	}
	return serviceidentity.Admission{RPCProfile: "service-v1", FullMethod: method,
		TargetSPIFFEID: "spiffe://kodex.local/ns/kodex-system/sa/control-plane",
		Peer:           serviceidentity.PeerIdentity{SPIFFEID: "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway", CertificateSHA256: sha256.Sum256([]byte("synthetic personal RPC peer")), NotAfter: time.Now().Add(time.Minute)},
		OperationID:    "platform.personal-integration-calls.begin", Permission: "platform.personal-integration-calls.begin", ActorMode: serviceidentity.ServiceActor}, nil
}

// personalRuntimeRPCNoUserCredential refuses every user-token path in this worker-only fixture.
// personalRuntimeRPCNoUserCredential отвергает любой user-token path в worker-only fixture.
type personalRuntimeRPCNoUserCredential struct{}

// VerifyToken must never grant a worker RPC a user identity or fallback credential.
// VerifyToken не выдаёт worker RPC пользовательскую identity либо fallback credential.
func (personalRuntimeRPCNoUserCredential) VerifyToken(context.Context, string) (oidcverifier.Principal, error) {
	return oidcverifier.Principal{}, errors.New("synthetic worker RPC rejects user credentials")
}

// personalRuntimeRPCClient serves the production Begin handler over actual protobuf/gRPC framing.
// Only the transport-peer admission is synthetic; request-profile hashing, principal resolution,
// RPC caster and owner transaction use production code. Plaintext is confined to bufconn with
// no TCP listener or real credential. Unused role-image service methods are not registered here.
// personalRuntimeRPCClient обслуживает production Begin handler с настоящими protobuf/gRPC frames.
// Синтетичен лишь transport-peer admission; request-profile hashing, principal resolution, RPC
// caster и owner transaction используют production code. Plaintext ограничен bufconn без TCP
// listener и настоящей credential. Неиспользуемые role-image методы здесь не регистрируются.
func personalRuntimeRPCClient(t *testing.T, service *platformservice.Service) cp.PersonalIntegrationCredentialServiceClient {
	t.Helper()
	resolver, err := rpcprincipal.New(service, personalRuntimeRPCNoUserCredential{})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := transport.NewServer(service, &roleimageservice.Service{})
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.UnaryInterceptor(authorization.ServiceIdentityUnary(personalRuntimeRPCAdmission{}, resolver)))
	cp.RegisterPersonalIntegrationCredentialServiceServer(server, owner)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	connection, err := grpc.NewClient("passthrough:///personal-runtime-fixture", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		server.Stop()
		listener.Close()
		<-done
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = connection.Close()
		server.Stop()
		_ = listener.Close()
		if err := <-done; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Error("stop synthetic personal RPC server", err)
		}
	})
	return cp.NewPersonalIntegrationCredentialServiceClient(connection)
}

// personalRuntimeRPCContext supplies only the explicit profile metadata required by the real
// interceptor. It intentionally supplies no user bearer token or request-selected identity.
// personalRuntimeRPCContext задаёт лишь explicit profile metadata для настоящего interceptor.
// User bearer token и request-selected identity намеренно отсутствуют.
func personalRuntimeRPCContext(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "x-kodex-rpc-profile", "service-v1")
}

// assertPersonalRuntimeRPCRefusals proves rejected profile/user/lease requests do not consume
// the valid Begin slot; the caller immediately follows them with the actual positive RPC.
// assertPersonalRuntimeRPCRefusals проверяет, что неверные profile/user/lease запросы не расходуют
// действительный Begin slot; caller сразу после них выполняет настоящий положительный RPC.
func assertPersonalRuntimeRPCRefusals(t *testing.T, ctx context.Context, client cp.PersonalIntegrationCredentialServiceClient, input *cp.BeginPersonalIntegrationInvocationRequest) {
	t.Helper()
	if _, err := client.BeginPersonalIntegrationInvocation(ctx, input); status.Code(err) != codes.Unauthenticated {
		t.Fatal("missing RPC profile reached personal admission", err)
	}
	userCtx := metadata.AppendToOutgoingContext(personalRuntimeRPCContext(ctx), "authorization", "Bearer synthetic-user-fixture")
	if _, err := client.BeginPersonalIntegrationInvocation(userCtx, input); status.Code(err) != codes.PermissionDenied {
		t.Fatal("user bearer became worker admission authority", err)
	}
	for _, change := range []func(*cp.BeginPersonalIntegrationInvocationRequest){
		func(r *cp.BeginPersonalIntegrationInvocationRequest) { r.Generation++ },
		func(r *cp.BeginPersonalIntegrationInvocationRequest) { r.Fence = "fnc_foreign_fixture" },
		func(r *cp.BeginPersonalIntegrationInvocationRequest) { r.WorkloadInstance = "foreign-fixture-worker" },
	} {
		forged := proto.Clone(input).(*cp.BeginPersonalIntegrationInvocationRequest)
		change(forged)
		response, err := client.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), forged)
		if err != nil || response.GetAdmitted() || response.GetCredentialRevision() != nil {
			t.Fatal("foreign lease coordinates obtained a personal descriptor", err)
		}
	}
}
