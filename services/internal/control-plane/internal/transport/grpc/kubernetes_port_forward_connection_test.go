package grpc

import (
	"encoding/base64"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// TestPortForwardPublicRPCRequiresExactAuthenticatedMethod refuses unverified and future-shaped requests before a missing owner can be touched.
// TestPortForwardPublicRPCRequiresExactAuthenticatedMethod отклоняет unverified и future-shaped requests до обращения к отсутствующему owner.
func TestPortForwardPublicRPCRequiresExactAuthenticatedMethod(t *testing.T) {
	server := &Server{}
	slot := uint32(0)
	invoke := func(message proto.Message) error {
		switch request := message.(type) {
		case *cp.OpenManagedKubernetesPortForwardConnectionRequest:
			_, err := server.OpenManagedKubernetesPortForwardConnection(t.Context(), request)
			return err
		case *cp.GetManagedKubernetesPortForwardConnectionRequest:
			_, err := server.GetManagedKubernetesPortForwardConnection(t.Context(), request)
			return err
		case *cp.ListManagedKubernetesPortForwardConnectionsRequest:
			_, err := server.ListManagedKubernetesPortForwardConnections(t.Context(), request)
			return err
		case *cp.CloseManagedKubernetesPortForwardConnectionRequest:
			_, err := server.CloseManagedKubernetesPortForwardConnection(t.Context(), request)
			return err
		}
		t.Fatal("unknown fixture method")
		return nil
	}
	for _, message := range []proto.Message{&cp.OpenManagedKubernetesPortForwardConnectionRequest{Slot: &slot}, &cp.GetManagedKubernetesPortForwardConnectionRequest{Selector: &cp.GetManagedKubernetesPortForwardConnectionRequest_ConnectionRef{ConnectionRef: "kpf_" + base64.RawURLEncoding.EncodeToString(make([]byte, 18))}}, &cp.ListManagedKubernetesPortForwardConnectionsRequest{}, &cp.CloseManagedKubernetesPortForwardConnectionRequest{}} {
		if status.Code(invoke(message)) != codes.Unauthenticated {
			t.Fatal("public PF method acquired authority without exact authenticated context")
		}
		message.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
		if status.Code(invoke(message)) != codes.InvalidArgument {
			t.Fatal("future PF field reached semantic interpretation")
		}
	}
	if _, err := server.OpenManagedKubernetesPortForwardConnection(t.Context(), &cp.OpenManagedKubernetesPortForwardConnectionRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatal("omitted slot selected first mapping")
	}
}

// TestPortForwardPublicProjectionSeparatesOwnerClosureAndNativeJoin preserves historical metadata and rejects fake join, future state and unrepresentable JSON versions.
// TestPortForwardPublicProjectionSeparatesOwnerClosureAndNativeJoin сохраняет historical metadata и отклоняет fake join, future state и непредставимые JSON versions.
func TestPortForwardPublicProjectionSeparatesOwnerClosureAndNativeJoin(t *testing.T) {
	base := entity.KubernetesPortForwardConnection{Ref: "kpf_" + base64.RawURLEncoding.EncodeToString(make([]byte, 18)), InvocationRef: "inv_synthetic", StreamRef: "kst_synthetic", StreamGeneration: 1, Generation: 2, InputVersion: 7, State: enum.KubernetesPortForwardConnectionState("CLOSED"), HardDeadline: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
	view, err := kubernetesPortForwardConnectionViewToProto(base)
	if err != nil || view.NativeJoined || view.State != cp.ManagedKubernetesPortForwardConnectionState_MANAGED_KUBERNETES_PORT_FORWARD_CONNECTION_STATE_CLOSED {
		t.Fatal("owner closure invented native join")
	}
	for _, change := range []func(*entity.KubernetesPortForwardConnection){func(v *entity.KubernetesPortForwardConnection) { v.State = "OPEN"; v.NativeJoined = true }, func(v *entity.KubernetesPortForwardConnection) { v.State = "FUTURE" }, func(v *entity.KubernetesPortForwardConnection) { v.Generation = 9007199254740992 }, func(v *entity.KubernetesPortForwardConnection) { v.InputVersion = 0 }, func(v *entity.KubernetesPortForwardConnection) { v.Slot = 16 }, func(v *entity.KubernetesPortForwardConnection) { v.Ref = "kpf_bad" }, func(v *entity.KubernetesPortForwardConnection) { v.ReasonCode = "private provider diagnostic" }} {
		candidate := base
		change(&candidate)
		if result, err := kubernetesPortForwardConnectionViewToProto(candidate); err == nil || result != nil {
			t.Fatal("invalid PF owner metadata escaped projection")
		}
	}
}
