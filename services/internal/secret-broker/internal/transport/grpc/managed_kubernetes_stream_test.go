package grpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// exchangeRPCFixture captures exact input and returns an independently allocated bounded output copy.
// exchangeRPCFixture фиксирует exact input и возвращает отдельно выделенную bounded output copy.
type exchangeRPCFixture struct {
	calls   int
	binding managedkubernetes.Binding
	input   *value.KubernetesStreamFrame
	ref     string
	ack     uint64
	result  value.KubernetesStreamExchange
}

// Exchange exposes whether malformed or unadmitted transport input reached the private domain lane.
// Exchange показывает, достиг ли malformed/unadmitted transport input private domain lane.
func (fixture *exchangeRPCFixture) Exchange(_ context.Context, binding managedkubernetes.Binding, ref string, ack uint64, input *value.KubernetesStreamFrame) (value.KubernetesStreamExchange, error) {
	fixture.calls++
	fixture.binding, fixture.ref, fixture.ack, fixture.input = binding, ref, ack, input
	return fixture.result, nil
}

// invokeKubernetesExchange applies the real trusted-profile interceptor independently of caller metadata.
// invokeKubernetesExchange применяет real trusted-profile interceptor независимо от caller metadata.
func invokeKubernetesExchange(t *testing.T, server *Server, request *sb.ExchangeManagedKubernetesStreamRequest, admitted bool, operation string) (*sb.ExchangeManagedKubernetesStreamResponse, error) {
	t.Helper()
	caller := "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway"
	method := sb.ManagedKubernetesService_ExchangeManagedKubernetesStream_FullMethodName
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster, serviceidentity.CallerMetadataKey, caller))
	if !admitted {
		return server.ExchangeManagedKubernetesStream(ctx, request)
	}
	authorizer, err := serviceidentity.NewTrustedCluster(transportprofile.TrustedCluster, secretBrokerSPIFFEID, []serviceidentity.Binding{{CallerSPIFFEID: caller, FullMethod: method, OperationID: operation, Permission: operation, ActorMode: serviceidentity.ServiceActor}})
	if err != nil {
		t.Fatal(err)
	}
	var response *sb.ExchangeManagedKubernetesStreamResponse
	_, err = authorizer.UnaryServerInterceptor()(ctx, request, &grpcgo.UnaryServerInfo{FullMethod: method}, func(ctx context.Context, _ any) (any, error) {
		var callErr error
		response, callErr = server.ExchangeManagedKubernetesStream(ctx, request)
		return response, callErr
	})
	return response, err
}

// TestManagedKubernetesExchangeBoundary preserves exact bindings and refuses malformed frames before domain I/O.
// TestManagedKubernetesExchangeBoundary сохраняет exact bindings и отвергает malformed frames до domain I/O.
func TestManagedKubernetesExchangeBoundary(t *testing.T) {
	registered := ""
	for operation, method := range controlplaneclient.ManagedKubernetesBrokerOperations() {
		if method == sb.ManagedKubernetesService_ExchangeManagedKubernetesStream_FullMethodName {
			if registered != "" {
				t.Fatal("duplicate stream method registration")
			}
			registered = operation
		}
	}
	if registered == "" {
		t.Fatal("stream method is not registered")
	}
	for _, variant := range []string{"exact", "raw-metadata", "wrong-method-policy", "obsolete-operation", "unknown-request", "unknown-binding", "health-binding", "unknown-frame", "input-delivery-ordinal", "channel", "digest", "oversized", "unwired", "private-reason", "invalid-output"} {
		t.Run(variant, func(t *testing.T) {
			execute, _ := kubernetesOperationRPCFixture()
			inputBytes, outputBytes := []byte("synthetic-input"), []byte("synthetic-output")
			inputDigest, outputDigest := sha256.Sum256(inputBytes), sha256.Sum256(outputBytes)
			request := &sb.ExchangeManagedKubernetesStreamRequest{Binding: execute.Binding, StreamRef: "stream_synthetic", AcknowledgedOutputSequence: 3, Input: &sb.ManagedKubernetesFrame{Sequence: 2, Channel: sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDIN, Payload: inputBytes, PayloadDigest: hex.EncodeToString(inputDigest[:])}}
			fixture := &exchangeRPCFixture{result: value.KubernetesStreamExchange{AcknowledgedInput: 2, State: "ACTIVE", RemoteOutcome: "RUNNING", Output: []value.KubernetesStreamFrame{{Sequence: 4, Channel: "STDOUT", Payload: outputBytes, PayloadDigest: hex.EncodeToString(outputDigest[:]), DeliveryOrdinal: 1, AuthorizedUntil: time.Now().Add(time.Second)}}}}
			server := &Server{}
			WithKubernetesStreams(fixture)(server)
			operation := registered
			want := codes.InvalidArgument
			switch variant {
			case "exact":
				want = codes.OK
			case "raw-metadata":
				want = codes.PermissionDenied
			case "wrong-method-policy":
				operation, want = "kubernetes.execute", codes.PermissionDenied
			case "obsolete-operation":
				operation, want = "kubernetes.exchange", codes.PermissionDenied
			case "unknown-request":
				request.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "unknown-binding":
				request.Binding.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "health-binding":
				request.Binding.InvocationRef, request.Binding.ConnectionTestRef = "", "test_synthetic"
			case "unknown-frame":
				request.Input.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "input-delivery-ordinal":
				request.Input.DeliveryOrdinal = 1
			case "channel":
				request.Input.Channel = sb.ManagedKubernetesChannel(100)
			case "digest":
				request.Input.PayloadDigest = strings.Repeat("f", 64)
			case "oversized":
				request.Input.Payload = bytes.Repeat([]byte("a"), managedkubernetes.MaximumFrameBytes+1)
			case "unwired":
				server.kubernetesStreams, want = nil, codes.Unavailable
			case "private-reason":
				fixture.result.ReasonCode, want = "PRIVATE_SYNTHETIC_DIAGNOSTIC", codes.Unavailable
			case "invalid-output":
				fixture.result.Output[0].PayloadDigest, want = strings.Repeat("f", 64), codes.Unavailable
			}
			response, err := invokeKubernetesExchange(t, server, request, variant != "raw-metadata", operation)
			if status.Code(err) != want || strings.Contains(status.Convert(err).Message(), "PRIVATE_SYNTHETIC") {
				t.Fatal("wrong stream RPC boundary", status.Code(err))
			}
			called := variant == "exact" || variant == "private-reason" || variant == "invalid-output"
			if (fixture.calls == 1) != called {
				t.Fatal("malformed or unadmitted frame reached domain")
			}
			if called {
				if fixture.binding.Fence != request.Binding.Fence || fixture.ref != request.StreamRef || fixture.ack != 3 || !bytes.Equal(fixture.input.Payload, make([]byte, len(inputBytes))) || !bytes.Equal(outputBytes, make([]byte, len(outputBytes))) {
					t.Fatal("pins or private buffer cleanup lost")
				}
			}
			if variant == "exact" && (response.AcknowledgedInputSequence != 2 || len(response.Output) != 1 || string(response.Output[0].Payload) != "synthetic-output") {
				t.Fatal("admitted output lost or aliased")
			}
		})
	}
}

// TestManagedKubernetesDescriptorRejectsMixedCredit excludes gates, historical effects and expired or foreign grants.
// TestManagedKubernetesDescriptorRejectsMixedCredit исключает gates, historical effects и expired/foreign grants.
func TestManagedKubernetesDescriptorRejectsMixedCredit(t *testing.T) {
	for _, variant := range []string{"exact", "gate", "history", "result", "generation", "expiry", "channel", "effect"} {
		t.Run(variant, func(t *testing.T) {
			request, fixture := kubernetesOperationRPCFixture()
			binding, _ := kubernetesInvocationBinding(request.Binding)
			accepted := fixture.result.Result
			fixture.result.Result, fixture.result.AuthorizedUntil = nil, time.Time{}
			grant := &value.KubernetesStreamGrant{Ref: "stream_synthetic", Generation: binding.Generation, ExpiresAt: time.Now().Add(time.Minute), MaximumFrameBytes: 1024, MaximumTotalBytes: 4096, InputChannels: []string{"STDIN"}, OutputChannels: []string{"STDOUT"}}
			result := targetrepo.OperationExecution{Effect: fixture.result, Stream: grant}
			switch variant {
			case "gate":
				result.Effect.GateRef = "gate_synthetic"
			case "history":
				result.Effect.ReconcileOnly = true
			case "result":
				result.Effect.Result = accepted
			case "generation":
				grant.Generation++
			case "expiry":
				grant.ExpiresAt = time.Now().Add(-time.Second)
			case "channel":
				grant.OutputChannels[0] = "arbitrary"
			case "effect":
				result.Effect.EffectGeneration = 0
			}
			response, err := managedKubernetesStreamResponse(t.Context(), binding, result)
			if (err == nil) != (variant == "exact") {
				t.Fatal("mixed or stale descriptor accepted")
			}
			if variant == "exact" && (response.GetStream().GetInvocationRef() != binding.InvocationRef || response.GetStream().GetGeneration() != binding.Generation) {
				t.Fatal("descriptor lost owner binding")
			}
		})
	}
}
