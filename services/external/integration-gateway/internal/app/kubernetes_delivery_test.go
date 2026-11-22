package app

import (
	"context"
	"net"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// TestKubernetesDeliveryAdoptsActualConsumer proves the common dispatcher hands the original claim to the actual bounded Kubernetes consumer.
// The activity runner is isolated; this proves adoption and cancellation lifetime, not an external effect or owner transaction.
// TestKubernetesDeliveryAdoptsActualConsumer проверяет передачу original claim из common dispatcher в actual bounded Kubernetes consumer.
// Activity runner изолирован: доказываются adoption и cancellation lifetime, не external effect либо owner transaction.
func TestKubernetesDeliveryAdoptsActualConsumer(t *testing.T) {
	entered := make(chan kubernetesActivityInput, 1)
	consumer := newKubernetesConsumerFixture(t, 1, func(ctx context.Context, input kubernetesActivityInput) error {
		entered <- input
		<-ctx.Done()
		return nil
	})
	claim := brokerInvocationClaim(t)
	claim.BoundedInput = kubernetesConsumerClaim().BoundedInput
	owner := &claimOwnerFixture{reply: &cp.ClaimIntegrationInvocationsResponse{Claims: []*cp.IntegrationInvocationClaim{claim}}}
	registry := &brokerIdentityConsumers{Kubernetes: consumer, claimTurn: 1, deliveryContract: func(context.Context) (uint32, error) { return 1, nil }}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ordinary, adopted, err := claimIntegrationInvocationDelivery(ctx, owner, registry, consumer.instance, 1, func(brokerIdentityInvocationProgress) { t.Error("adoption reported an external effect") })
	cancel()
	if err != nil || adopted != 1 || len(ordinary) != 0 || owner.calls != 1 || owner.request.Capacity.Kubernetes != 1 {
		t.Fatal("actual consumer rejected original claim", err)
	}
	select {
	case input := <-entered:
		if input.binding.InvocationRef != claim.InvocationRef || input.binding.Generation != claim.Lease.Generation || input.binding.Fence != claim.Lease.Fence || input.binding.WorkloadInstance != consumer.instance {
			t.Fatal("common delivery changed original binding")
		}
	case <-time.After(time.Second):
		t.Fatal("adopted activity never entered")
	}
	if slot, available := consumer.ReserveClaim(); available || slot != nil {
		t.Fatal("unary cancellation released active consumer capacity")
	}
	_ = consumer.Close()
	joined, finish := context.WithTimeout(t.Context(), time.Second)
	defer finish()
	if consumer.Wait(joined) != nil || !consumer.Joined() {
		t.Fatal("actual consumer failed to join")
	}
}

// deliveryWireOwner exposes synthetic readiness through the real generated client without asserting TLS or owner authority.
// deliveryWireOwner передаёт synthetic readiness через real generated client без утверждений о TLS либо owner authority.
type deliveryWireOwner struct {
	cp.UnimplementedManagedKubernetesOwnerServiceServer
	revision uint32
	ready    bool
}

// CheckManagedKubernetesOwnerReadiness accepts only the common-delivery probe without enabling configured target operations.
// CheckManagedKubernetesOwnerReadiness принимает только common-delivery probe без включения configured target operations.
func (owner *deliveryWireOwner) CheckManagedKubernetesOwnerReadiness(_ context.Context, request *cp.CheckManagedKubernetesOwnerReadinessRequest) (*cp.CheckManagedKubernetesOwnerReadinessResponse, error) {
	if !request.DeliveryContractOnly {
		return nil, brokerIdentityWorkUnavailable
	}
	return &cp.CheckManagedKubernetesOwnerReadinessResponse{Ready: owner.ready, DeliveryContractRevision: owner.revision}, nil
}

// deliveryWireCodec injects malformed successful replies after server serialization to prove inbound strict decoding.
// deliveryWireCodec добавляет malformed successful replies после server serialization для проверки inbound strict decoding.
type deliveryWireCodec struct{ mode string }

// Name retains the standard wire protocol name.
// Name сохраняет стандартное имя wire protocol.
func (deliveryWireCodec) Name() string { return "proto" }

// Marshal keeps valid fields and optionally adds a duplicate revision or an unknown response field.
// Marshal сохраняет valid fields и опционально добавляет duplicate revision либо unknown response field.
func (codec deliveryWireCodec) Marshal(message any) ([]byte, error) {
	raw, err := proto.Marshal(message.(proto.Message))
	if _, ok := message.(*cp.CheckManagedKubernetesOwnerReadinessResponse); ok && err == nil {
		if codec.mode == "duplicate" {
			raw = protowire.AppendVarint(protowire.AppendTag(raw, 2, protowire.VarintType), 1)
		}
		if codec.mode == "unknown" {
			raw = protowire.AppendVarint(protowire.AppendTag(raw, 99, protowire.VarintType), 1)
		}
	}
	return raw, err
}

// Unmarshal handles only requests sent to the synthetic server.
// Unmarshal обрабатывает лишь requests к synthetic server.
func (deliveryWireCodec) Unmarshal(raw []byte, message any) error {
	return proto.Unmarshal(raw, message.(proto.Message))
}

// TestKubernetesDeliveryContractUsesActualTypedWire distinguishes optional target readiness from the required delivery revision.
// Duplicate, unknown and unsupported replies close startup even when the owner otherwise reports ready.
// TestKubernetesDeliveryContractUsesActualTypedWire различает optional target readiness и required delivery revision.
// Duplicate, unknown и unsupported replies закрывают startup даже при owner ready.
func TestKubernetesDeliveryContractUsesActualTypedWire(t *testing.T) {
	for _, mode := range []string{"target unavailable", "target ready", "unsupported", "duplicate", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			owner := &deliveryWireOwner{revision: 1, ready: mode != "target unavailable"}
			if mode == "unsupported" {
				owner.revision = 2
			}
			listener := bufconn.Listen(1 << 20)
			server := grpc.NewServer(grpc.ForceServerCodec(deliveryWireCodec{mode: mode}))
			cp.RegisterManagedKubernetesOwnerServiceServer(server, owner)
			joined := make(chan struct{})
			go func() { defer close(joined); _ = server.Serve(listener) }()
			t.Cleanup(func() { server.Stop(); _ = listener.Close(); <-joined })
			connection, err := grpc.NewClient("passthrough:///delivery-wire", grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = connection.Close() })
			registry := &brokerIdentityConsumers{deliveryContract: kubernetesDeliveryContract(cp.NewManagedKubernetesOwnerServiceClient(connection))}
			err = checkBrokerIdentityDelivery(t.Context(), registry)
			valid := mode == "target unavailable" || mode == "target ready"
			if (err == nil) != valid {
				t.Fatal("delivery compatibility decision changed", err)
			}
		})
	}
}
