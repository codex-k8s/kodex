package workload

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestTrustedRuntimePodOmitsCallbackKeysButRetainsTicket(t *testing.T) {
	config := testManagerConfig()
	config.RPCProfile = runtimecontract.CallbackProfileTrustedCluster
	config.CallbackTLSServerName, config.CallbackClientCASecret, config.CallbackClientTLSSecret = "", "", ""
	manager, err := New(fake.NewSimpleClientset(), config)
	if err != nil {
		t.Fatal(err)
	}
	input, binding, err := manager.BuildTurnInput(testExecution(false))
	if err != nil || input.ValidateCallbackTransport() != nil || input.CallbackTLS.Profile != config.RPCProfile {
		t.Fatalf("trusted runtime input: %v", err)
	}
	credentials := testCredentialProjection(input)
	pod := manager.runtimePod(input, binding, &credentials, "runtime-ticket-fixture", "fixture", "turn")
	if pod.Labels["kodex.dev/security-profile"] != config.RPCProfile {
		t.Fatal("trusted runtime profile label missing")
	}
	for _, volume := range pod.Spec.Volumes {
		if volume.Name == "callback-ca" || volume.Name == "callback-client" {
			t.Fatal("callback key material remains in trusted Pod")
		}
	}
	for _, container := range append(append([]corev1.Container{}, pod.Spec.Containers...), pod.Spec.InitContainers...) {
		for _, mount := range container.VolumeMounts {
			if mount.Name == "callback-ca" || mount.Name == "callback-client" {
				t.Fatal("callback key mount remains in trusted Pod")
			}
		}
	}
	if !hasMount(pod.Spec.Containers[0], "runtime-ticket") || hasMount(pod.Spec.Containers[1], "runtime-ticket") {
		t.Fatal("execution ticket isolation changed")
	}
}
