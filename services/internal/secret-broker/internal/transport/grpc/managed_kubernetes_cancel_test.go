package grpc

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// cancelRPCFixture separates local transport completion from the original invocation's remote outcome.
// cancelRPCFixture отделяет local transport completion от remote outcome исходного invocation.
type cancelRPCFixture struct {
	exchangeRPCFixture
	cancels int
	joined  bool
	err     error
}

// Cancel records one exact cleanup request without reopening or replaying native execution.
// Cancel фиксирует exact cleanup request без reopen либо replay native execution.
func (fixture *cancelRPCFixture) Cancel(_ context.Context, binding managedkubernetes.Binding, ref string) (value.KubernetesStreamExchange, bool, error) {
	fixture.cancels++
	fixture.binding, fixture.ref = binding, ref
	return fixture.result, fixture.joined, fixture.err
}

// invokeKubernetesCancel verifies the actual per-method admission before calling the cleanup RPC.
// invokeKubernetesCancel проверяет actual per-method admission до вызова cleanup RPC.
func invokeKubernetesCancel(t *testing.T, server *Server, request *sb.CancelManagedKubernetesOperationRequest, admitted bool, operation string) (*sb.CancelManagedKubernetesOperationResponse, error) {
	t.Helper()
	caller := "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway"
	method := sb.ManagedKubernetesService_CancelManagedKubernetesOperation_FullMethodName
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster, serviceidentity.CallerMetadataKey, caller))
	if !admitted {
		return server.CancelManagedKubernetesOperation(ctx, request)
	}
	authorizer, err := serviceidentity.NewTrustedCluster(transportprofile.TrustedCluster, secretBrokerSPIFFEID, []serviceidentity.Binding{{CallerSPIFFEID: caller, FullMethod: method, OperationID: operation, Permission: operation, ActorMode: serviceidentity.ServiceActor}})
	if err != nil {
		t.Fatal(err)
	}
	var response *sb.CancelManagedKubernetesOperationResponse
	_, err = authorizer.UnaryServerInterceptor()(ctx, request, &grpcgo.UnaryServerInfo{FullMethod: method}, func(ctx context.Context, _ any) (any, error) {
		var callErr error
		response, callErr = server.CancelManagedKubernetesOperation(ctx, request)
		return response, callErr
	})
	return response, err
}

// TestManagedKubernetesCancelBoundary rejects forged cleanup authority and never fabricates a joined remote exit.
// TestManagedKubernetesCancelBoundary отвергает forged cleanup authority и не выдумывает joined remote exit.
func TestManagedKubernetesCancelBoundary(t *testing.T) {
	for _, variant := range []string{"exact", "unknown-join", "raw-metadata", "wrong-operation", "unknown-request", "unknown-binding", "health-binding", "unwired", "private-error", "active", "known-exit-unjoined", "output"} {
		t.Run(variant, func(t *testing.T) {
			execute, _ := kubernetesOperationRPCFixture()
			request := &sb.CancelManagedKubernetesOperationRequest{Binding: execute.Binding, StreamRef: "stream_synthetic"}
			fixture := &cancelRPCFixture{joined: true, exchangeRPCFixture: exchangeRPCFixture{result: value.KubernetesStreamExchange{State: "CANCELED", RemoteOutcome: "UNKNOWN", ReasonCode: "CANCELED"}}}
			server := &Server{}
			WithKubernetesStreams(fixture)(server)
			operation, want := "kubernetes.cancel", codes.OK
			switch variant {
			case "unknown-join":
				fixture.joined = false
			case "raw-metadata":
				want = codes.PermissionDenied
			case "wrong-operation":
				operation, want = "kubernetes.exchange", codes.PermissionDenied
			case "unknown-request":
				request.ProtoReflect().SetUnknown([]byte{0x78, 1})
				want = codes.InvalidArgument
			case "unknown-binding":
				request.Binding.ProtoReflect().SetUnknown([]byte{0x78, 1})
				want = codes.InvalidArgument
			case "health-binding":
				request.Binding.InvocationRef, request.Binding.ConnectionTestRef = "", "test_synthetic"
				want = codes.InvalidArgument
			case "unwired":
				server.kubernetesStreams = &exchangeRPCFixture{}
				want = codes.Unavailable
			case "private-error":
				fixture.err = errors.New("PRIVATE_SYNTHETIC_DIAGNOSTIC")
				want = codes.Unavailable
			case "active":
				fixture.result.State, fixture.result.RemoteOutcome = "ACTIVE", "RUNNING"
				want = codes.Unavailable
			case "known-exit-unjoined":
				fixture.joined, fixture.result.RemoteOutcome = false, "SUCCEEDED"
				want = codes.Unavailable
			case "output":
				fixture.result.Output = []value.KubernetesStreamFrame{{Payload: []byte("PRIVATE_SYNTHETIC_OUTPUT")}}
				want = codes.Unavailable
			}
			response, err := invokeKubernetesCancel(t, server, request, variant != "raw-metadata", operation)
			if status.Code(err) != want || strings.Contains(status.Convert(err).Message(), "PRIVATE_SYNTHETIC") {
				t.Fatal("wrong cancellation RPC boundary", status.Code(err))
			}
			if want == codes.OK {
				if response.GetTransportJoined() != fixture.joined || response.GetRemoteOutcome() != sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_UNKNOWN || fixture.binding.Fence != request.Binding.Fence || fixture.ref != request.StreamRef {
					t.Fatal("cleanup changed original binding or fabricated exit/join")
				}
			}
			if (want == codes.PermissionDenied || want == codes.InvalidArgument || variant == "unwired") && fixture.cancels != 0 {
				t.Fatal("unadmitted cleanup reached private domain")
			}
			if variant == "output" {
				for _, b := range fixture.result.Output[0].Payload {
					if b != 0 {
						t.Fatal("rejected cleanup output was retained")
					}
				}
			}
		})
	}
}
