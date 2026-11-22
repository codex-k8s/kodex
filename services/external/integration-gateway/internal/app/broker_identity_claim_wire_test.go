package app

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// claimWireCodec injects one duplicate nested singular field into an otherwise valid response; it changes no production transport or owner state.
// claimWireCodec добавляет duplicate nested singular field к корректному ответу без изменения production transport либо owner state.
type claimWireCodec struct{ duplicate bool }

// Name uses the ordinary protocol name so the generated client exercises its real inbound codec rather than a fixture-only format.
// Name использует обычное имя protocol для проверки real inbound codec generated client вместо fixture-only формата.
func (claimWireCodec) Name() string { return "proto" }

// Marshal preserves canonical positive responses and injects duplicate invocation_ref only at the wire boundary after the owner handler has returned.
// Marshal сохраняет canonical positive responses и добавляет duplicate invocation_ref лишь на wire boundary после возврата owner handler.
func (codec claimWireCodec) Marshal(value any) ([]byte, error) {
	response, ok := value.(*cp.ClaimIntegrationInvocationsResponse)
	if !ok || !codec.duplicate {
		return proto.Marshal(value.(proto.Message))
	}
	raw, err := proto.Marshal(response.Claims[0])
	if err != nil {
		return nil, err
	}
	raw = protowire.AppendTag(raw, 1, protowire.BytesType)
	raw = protowire.AppendString(raw, response.Claims[0].InvocationRef)
	return protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), raw), nil
}

// Unmarshal decodes only synthetic requests; production response validation belongs to the actual client codec.
// Unmarshal декодирует только synthetic requests; production response validation выполняет actual client codec.
func (claimWireCodec) Unmarshal(raw []byte, value any) error {
	return proto.Unmarshal(raw, value.(proto.Message))
}

// claimWireOwner returns one immutable synthetic claim and counts actual generated RPC requests, without simulating authorization or SQL qualification.
// claimWireOwner возвращает один immutable synthetic claim и считает actual generated RPC requests без имитации authorization либо SQL qualification.
type claimWireOwner struct {
	cp.UnimplementedRuntimeWorkServiceServer
	claim *cp.IntegrationInvocationClaim
	calls atomic.Int32
}

// ClaimIntegrationInvocations represents the committed owner response before wire damage; no second claim or provider effect is performed.
// ClaimIntegrationInvocations представляет committed owner response до wire damage без второго claim либо provider effect.
func (owner *claimWireOwner) ClaimIntegrationInvocations(context.Context, *cp.ClaimIntegrationInvocationsRequest) (*cp.ClaimIntegrationInvocationsResponse, error) {
	owner.calls.Add(1)
	return &cp.ClaimIntegrationInvocationsResponse{Claims: []*cp.IntegrationInvocationClaim{owner.claim}}, nil
}

// TestBrokerIdentityClaimRejectsDuplicateWire uses the actual generated gRPC client over an in-memory connection; it proves canonical adoption and closed rejection before adoption when duplicate fields would otherwise disappear during protobuf decoding. It makes no TLS/authority readiness claim.
// TestBrokerIdentityClaimRejectsDuplicateWire использует actual generated gRPC client через in-memory connection: canonical adoption проходит, duplicate fields закрыто отклоняются до adoption и до потери сведений при protobuf decode. Проверка не подтверждает TLS либо authority readiness.
func TestBrokerIdentityClaimRejectsDuplicateWire(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(map[bool]string{false: "canonical", true: "duplicate"}[duplicate], func(t *testing.T) {
			consumer := newClaimConsumerFixture(t, 1)
			registry := &brokerIdentityConsumers{Kubernetes: consumer, deliveryContract: func(context.Context) (uint32, error) { return 1, nil }, claimTurn: 1}
			owner := &claimWireOwner{claim: brokerInvocationClaim(t)}
			listener := bufconn.Listen(1 << 20)
			server := grpc.NewServer(grpc.ForceServerCodec(claimWireCodec{duplicate: duplicate}))
			cp.RegisterRuntimeWorkServiceServer(server, owner)
			joined := make(chan struct{})
			go func() { defer close(joined); _ = server.Serve(listener) }()
			t.Cleanup(func() { server.Stop(); _ = listener.Close(); <-joined })
			connection, err := grpc.NewClient("passthrough:///claim-wire", grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatal("create synthetic wire client")
			}
			t.Cleanup(func() { _ = connection.Close() })
			_, adopted, err := claimIntegrationInvocationDelivery(t.Context(), cp.NewRuntimeWorkServiceClient(connection), registry, "worker_fixture", 1, func(brokerIdentityInvocationProgress) { t.Fatal("wire test attempted effect") })
			if owner.calls.Load() != 1 {
				t.Fatal("wire acknowledgement retried owner claim")
			}
			if duplicate {
				if !errors.Is(err, errBrokerIdentityDeliveryUnresolved) || adopted != 0 || len(consumer.adopted) != 0 {
					t.Fatal("duplicate wire fields disappeared before closed rejection")
				}
			} else if err != nil || adopted != 1 || len(consumer.adopted) != 1 {
				t.Fatal("canonical wire response was rejected")
			}
		})
	}
}
