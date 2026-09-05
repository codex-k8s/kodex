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

const catalogOperation = "platform.stt.model-catalog.get"

type catalogProof struct {
	operation, method, digest string
	err                       error
}

func (proof *catalogProof) AuthorityProof(ctx context.Context, operation, method string) (string, string, error) {
	proof.operation, proof.method = operation, method
	proof.digest, _ = authorityclient.RequestDigest(ctx)
	return "fixture-admin-proof", "fixture-correlation", proof.err
}

type catalogIssuer struct {
	auth.AuthorizationIssuerServiceClient
	request *auth.IssueAuthorizationContextRequest
	err     error
}

func (issuer *catalogIssuer) IssueAuthorizationContext(_ context.Context, request *auth.IssueAuthorizationContextRequest, _ ...grpc.CallOption) (*auth.IssueAuthorizationContextResponse, error) {
	issuer.request = proto.Clone(request).(*auth.IssueAuthorizationContextRequest)
	if issuer.err != nil {
		return nil, issuer.err
	}
	return &auth.IssueAuthorizationContextResponse{CompactJws: "fixture-issued-context"}, nil
}

type catalogServer struct {
	stt.UnimplementedSpeechToTextServiceServer
	calls atomic.Int32
}

func (server *catalogServer) GetModelCatalog(ctx context.Context, _ *stt.GetModelCatalogRequest) (*stt.GetModelCatalogResponse, error) {
	server.calls.Add(1)
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get(authorityclient.AuthorizationMetadata)
	if len(values) != 1 || values[0] != "fixture-issued-context" {
		return nil, status.Error(codes.Unauthenticated, "issued authorization context is missing")
	}
	return &stt.GetModelCatalogResponse{Catalog: &stt.TranscriptionModelCatalog{Version: "fixture-adapter"}}, nil
}

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
