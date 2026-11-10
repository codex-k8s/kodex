package workload

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestManagedWorkspaceExportAdmissionFixtures feeds synthetic Pods from the real producer to
// the separate Kubernetes CEL contour; no private configuration or live credentials are read.
// TestManagedWorkspaceExportAdmissionFixtures передаёт synthetic Pods реального producer в
// отдельный Kubernetes CEL-контур; приватная конфигурация и живые credentials не читаются.
func TestManagedWorkspaceExportAdmissionFixtures(t *testing.T) {
	path := os.Getenv("KODEX_TEST_RUNTIME_ADMISSION_FIXTURES")
	if path == "" {
		return
	}
	type fixture struct {
		Name   string         `json:"name"`
		Pod    *corev1.Pod    `json:"pod"`
		Params map[string]any `json:"params"`
	}
	var fixtures []fixture
	add := func(name string, manager *Manager, input runtimecontract.RunnerInput, binding ProviderSecretBinding) {
		t.Helper()
		credentials := testCredentialProjection(input)
		pod := manager.runtimePod(input, binding, &credentials, ticketName(input.LeaseRef), turnPodName(input.LeaseRef), "turn")
		if pod == nil {
			t.Fatal("actual producer rejected synthetic fixture", name)
		}
		profile := ""
		for _, c := range pod.Spec.Containers {
			if c.Name == "provider-runtime" && c.SecurityContext.AppArmorProfile != nil && c.SecurityContext.AppArmorProfile.LocalhostProfile != nil {
				profile = *c.SecurityContext.AppArmorProfile.LocalhostProfile
			}
		}
		fixtures = append(fixtures, fixture{name, pod, map[string]any{"data": map[string]string{"nodeReadbackImage": manager.config.DefaultRoleImageReference, "providerAppArmorProfile": profile, "roleRuntimeContractRevision": strconv.Itoa(int(runtimecontract.WorkspaceStorageRuntimeContractRevision))}}})
	}
	manager := newTestManager(t, fake.NewSimpleClientset())
	input, binding, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatal(err)
	}
	add("legacy", manager, input, binding)
	managed := managedWorkspaceRenderInput(input)
	add("commands", manager, managed, binding)
	minimum := managed
	minimum.EnvironmentPolicy.Resources.CPURequestMilli = 100
	minimum.EnvironmentPolicy.Resources.MemoryRequestMiB = 128
	minimum.EnvironmentPolicy.Resources.EphemeralStorageRequestMiB = 256
	add("commands-minimum", manager, minimum, binding)
	odd := managed
	odd.EnvironmentPolicy.Resources.CPURequestMilli = 251
	add("commands-odd-cpu", manager, odd, binding)
	managed.EnvironmentTools = nil
	add("files", manager, managed, binding)
	manager, _, input, _ = workspaceStoragePodFixture(t)
	managed = managedWorkspaceRenderInput(input)
	add("commands-v2", manager, managed, ProviderSecretBinding{Name: "runtime-provider-openai-default-r1"})
	managed.EnvironmentTools = nil
	add("files-v2", manager, managed, ProviderSecretBinding{Name: "runtime-provider-openai-default-r1"})
	raw, err := json.Marshal(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
}
