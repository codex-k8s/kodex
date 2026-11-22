package grpc

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// managedKubernetesPortForwardWitnessFromProto validates a broker-only OUTPUT witness before narrowing integers or consulting owner state.
// managedKubernetesPortForwardWitnessFromProto проверяет broker-only OUTPUT witness до сужения integers либо обращения к owner state.
func managedKubernetesPortForwardWitnessFromProto(input *cp.ManagedKubernetesPortForwardFrameBinding, count uint32) (*managedkubernetes.PortForwardConnectionWitness, bool) {
	if input == nil {
		return nil, true
	}
	if len(input.ProtoReflect().GetUnknown()) != 0 || input.GetGeneration() < 1 || input.GetSlot() >= managedkubernetes.MaximumForwardPorts {
		return nil, false
	}
	switch input.GetAction() {
	case cp.ManagedKubernetesPortForwardFrameAction_MANAGED_KUBERNETES_PORT_FORWARD_FRAME_ACTION_DATA, cp.ManagedKubernetesPortForwardFrameAction_MANAGED_KUBERNETES_PORT_FORWARD_FRAME_ACTION_HALF_CLOSE, cp.ManagedKubernetesPortForwardFrameAction_MANAGED_KUBERNETES_PORT_FORWARD_FRAME_ACTION_CLOSE:
	default:
		return nil, false
	}
	witness := managedkubernetes.PortForwardConnectionWitness{ConnectionRef: input.GetConnectionRef(), Generation: uint64(input.GetGeneration()), Slot: byte(input.GetSlot()), Action: managedkubernetes.PortForwardConnectionAction(input.GetAction()), NativeOrdinal: input.GetNativeOrdinal()}
	if witness.ValidateOutput(count, managedkubernetes.MaximumForwardPorts) != nil {
		return nil, false
	}
	return &witness, true
}
