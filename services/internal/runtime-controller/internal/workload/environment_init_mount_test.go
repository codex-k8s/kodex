package workload

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestRuntimeInitializerHasExactDeclaredEnvironmentMounts inspects turn/warm
// Pod-generation modes for one exact writable mount per declared volume in
// initializer/role/provider, no aliases/propagation and no preparation/relay mount.
// It changes fixture policy without claiming a new authoritative runtime seal.
// TestRuntimeInitializerHasExactDeclaredEnvironmentMounts проверяет modes генерации
// turn/warm Pod: по одному точному writable mount каждого тома у initializer,
// role и provider, без aliases/propagation и mounts у preparation/relay.
// Изменение fixture policy не объявляется новым авторитетным runtime seal.
func TestRuntimeInitializerHasExactDeclaredEnvironmentMounts(t *testing.T) {
	for _, mode := range []string{"turn", "warm"} {
		t.Run(mode, func(t *testing.T) {
			manager := newTestManager(t, fake.NewSimpleClientset())
			input, binding, err := manager.BuildTurnInput(testExecution(false))
			if err != nil {
				t.Fatal(err)
			}
			policy := input.EnvironmentPolicy
			policy.Volumes = []runtimecontract.RuntimeVolume{
				{Name: "build-cache", Kind: runtimecontract.RuntimeVolumeEphemeralDisk, SizeMiB: 8192},
				{Name: "scratch", Kind: runtimecontract.RuntimeVolumeEphemeralMemory, SizeMiB: 32},
			}
			input.EnvironmentPolicy, err = runtimecontract.NormalizeRuntimeEnvironmentPolicy(policy)
			if err != nil {
				t.Fatal(err)
			}
			pod := manager.runtimePod(input, binding, nil, "ticket-fixture", "runtime-fixture", mode)
			if pod == nil {
				t.Fatal("runtime Pod was not materialized")
			}
			init := containerByName(t, pod.Spec.InitContainers, "workspace-init")
			role := containerByName(t, pod.Spec.Containers, "role-runtime")
			provider := containerByName(t, pod.Spec.Containers, "provider-runtime")
			for _, volume := range input.EnvironmentPolicy.Volumes {
				name := "environment-" + shortHash(volume.Name)
				for _, container := range []corev1.Container{init, role, provider} {
					count := 0
					for _, mount := range container.VolumeMounts {
						if mount.Name != name {
							continue
						}
						count++
						if mount.MountPath != volume.MountPath || mount.ReadOnly || mount.SubPath != "" || mount.SubPathExpr != "" || mount.MountPropagation != nil {
							t.Fatal("environment canary received an invalid mount boundary")
						}
					}
					if count != 1 {
						t.Fatalf("exact declared mount missing from %s", container.Name)
					}
				}
				for _, container := range []corev1.Container{pod.Spec.InitContainers[0], containerByName(t, pod.Spec.Containers, "provider-credential-relay")} {
					if hasMount(container, name) {
						t.Fatal("environment volume leaked to an unrelated container")
					}
				}
			}
		})
	}
}
