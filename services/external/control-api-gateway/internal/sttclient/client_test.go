package sttclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	auth "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	stt "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// catalogOperation pins the model-catalog command expected by the generated speech RPC fixture, not a wildcard permission for all speech methods.
// catalogOperation закрепляет model-catalog command, ожидаемую эталоном generated speech RPC, а не wildcard permission всех speech methods.
const catalogOperation = "platform.stt.model-catalog.get"

// catalogProof captures operation/method/digest and returns a configured error with synthetic proof markers, isolating client composition from a real domain authority resolver.
// catalogProof сохраняет operation, method и digest и возвращает настроенную ошибку с synthetic proof markers, изолируя client composition от настоящего domain authority resolver.
type catalogProof struct {
	operation, method, digest string
	err                       error
}

// AuthorityProof captures exact call identity and bound request digest before returning fixed proof/correlation strings and the configured fixture error unchanged.
// AuthorityProof сохраняет точные call identity и bound request digest до возврата фиксированных proof и correlation strings и настроенной fixture error без изменения.
func (proof *catalogProof) AuthorityProof(ctx context.Context, operation, method string) (string, string, error) {
	proof.operation, proof.method = operation, method
	proof.digest, _ = authorityclient.RequestDigest(ctx)
	return "fixture-admin-proof", "fixture-correlation", proof.err
}

// catalogIssuer embeds unused generated methods and retains a cloned issuance request with optional configured failure; it never signs or persists a production authorization context.
// catalogIssuer встраивает неиспользуемые generated methods и сохраняет клонированный issuance request с необязательным настроенным отказом; production authorization context не подписывается и не сохраняется.
type catalogIssuer struct {
	auth.AuthorizationIssuerServiceClient
	request *auth.IssueAuthorizationContextRequest
	err     error
}

// IssueAuthorizationContext clones the request to retain immutable fixture observations, returns the configured error first and otherwise supplies a fixed issued-context marker.
// IssueAuthorizationContext клонирует request для неизменяемых наблюдений эталона, сначала возвращает настроенную ошибку и иначе выдаёт фиксированный issued-context marker.
func (issuer *catalogIssuer) IssueAuthorizationContext(_ context.Context, request *auth.IssueAuthorizationContextRequest, _ ...grpc.CallOption) (*auth.IssueAuthorizationContextResponse, error) {
	issuer.request = proto.Clone(request).(*auth.IssueAuthorizationContextRequest)
	if issuer.err != nil {
		return nil, issuer.err
	}
	return &auth.IssueAuthorizationContextResponse{CompactJws: "fixture-issued-context"}, nil
}

// catalogServer is a loopback-only generated speech service fake with an atomic call counter and fixed catalog response; it has no audio pipeline or real model-provider client.
// catalogServer — loopback-only fake generated speech service с atomic call counter и фиксированным catalog response без audio pipeline и настоящего model-provider client.
type catalogServer struct {
	stt.UnimplementedSpeechToTextServiceServer
	calls atomic.Int32
}

// GetModelCatalog increments the fixture call counter before requiring one exact authorization metadata marker and returning the synthetic catalog. It does not cryptographically verify the marker.
// GetModelCatalog увеличивает fixture call counter до требования одного точного authorization metadata marker и возврата synthetic catalog. Криптографическая проверка marker не выполняется.
func (server *catalogServer) GetModelCatalog(ctx context.Context, _ *stt.GetModelCatalogRequest) (*stt.GetModelCatalogResponse, error) {
	server.calls.Add(1)
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get(authorityclient.AuthorizationMetadata)
	if len(values) != 1 || values[0] != "fixture-issued-context" {
		return nil, status.Error(codes.Unauthenticated, "issued authorization context is missing")
	}
	return &stt.GetModelCatalogResponse{Catalog: &stt.TranscriptionModelCatalog{Version: "fixture-adapter"}}, nil
}

// TestCatalogGeneratedRPCUsesProductionIssuerComposition uses a loopback gRPC server and the real protected-connection builder to check exact method/digest/proof metadata, local denial before server calls and missing-dependency refusal. Insecure credentials are confined to the fixture; no real STT, provider, local issuer or signed context is used.
// TestCatalogGeneratedRPCUsesProductionIssuerComposition через loopback gRPC server и настоящий protected-connection builder проверяет точные method, digest и proof metadata, local denial до server calls и отказ отсутствующих dependencies. Insecure credentials ограничены эталоном; реальные STT, provider, local issuer и signed context не используются.
func TestCatalogGeneratedRPCUsesProductionIssuerComposition(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	consumer := &catalogServer{}
	stt.RegisterSpeechToTextServiceServer(server, consumer)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); <-done })
	request := &stt.GetModelCatalogRequest{}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	for _, failure := range []string{"", "proof", "issuer"} {
		t.Run(failure, func(t *testing.T) {
			proof, issuer := &catalogProof{}, &catalogIssuer{}
			if failure == "proof" {
				proof.err = status.Error(codes.PermissionDenied, "proof denied")
			}
			if failure == "issuer" {
				issuer.err = status.Error(codes.PermissionDenied, "issuer denied")
			}
			// Только loopback fixture использует insecure; production Dial создаёт exact mTLS credentials.
			connection, err := protectedConnection(listener.Addr().String(), insecure.NewCredentials(), issuer, proof)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = connection.Close() })
			before := consumer.calls.Load()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			response, err := stt.NewSpeechToTextServiceClient(connection).GetModelCatalog(ctx, request)
			if failure != "" {
				if status.Code(err) != codes.Unauthenticated || consumer.calls.Load() != before {
					t.Fatalf("local denial or server-call count changed: %v", err)
				}
				if failure == "proof" && issuer.request != nil {
					t.Fatal("denied proof reached issuer")
				}
				return
			}
			if err != nil || response.GetCatalog().GetVersion() != "fixture-adapter" || consumer.calls.Load() != before+1 {
				t.Fatalf("protected generated call failed: %v", err)
			}
			if proof.operation != catalogOperation || proof.method != stt.SpeechToTextService_GetModelCatalog_FullMethodName || proof.digest != digest ||
				issuer.request.GetOperationId() != catalogOperation || issuer.request.GetRequestDigestSha256() != digest || issuer.request.GetAuthorityProofCompactJws() != "fixture-admin-proof" || issuer.request.GetCorrelationId() != "fixture-correlation" {
				t.Fatal("exact operation, request digest or authority proof was lost")
			}
			before = consumer.calls.Load()
			proof.operation, issuer.request = "", nil
			_, err = stt.NewSpeechToTextServiceClient(connection).CheckReadiness(ctx, &stt.CheckReadinessRequest{})
			if status.Code(err) != codes.PermissionDenied || consumer.calls.Load() != before || proof.operation != "" || issuer.request != nil {
				t.Fatal("unregistered unary method escaped client boundary")
			}
		})
	}
	if connection, err := protectedConnection(listener.Addr().String(), insecure.NewCredentials(), nil, &catalogProof{}); err == nil || connection != nil {
		t.Fatal("missing issuer accepted")
	}
	if connection, err := protectedConnection(listener.Addr().String(), insecure.NewCredentials(), &catalogIssuer{}, nil); err == nil || connection != nil {
		t.Fatal("missing proof provider accepted")
	}
}
