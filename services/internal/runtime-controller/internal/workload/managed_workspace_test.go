package workload

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/client-go/kubernetes/fake"
)

// managedWorkspaceRenderInput selects a synthetic complete local tool profile for render tests.
// It does not reseal an owner revision, issue a credential, contact a model or run a container.
// managedWorkspaceRenderInput выбирает synthetic complete local tool profile для render tests.
// Owner revision не переподписывает, credentials не выдаёт, модель и контейнер не запускает.
func managedWorkspaceRenderInput(input runtimecontract.RunnerInput) runtimecontract.RunnerInput {
	input.Provider = "opencode"
	input.Model = "model-a"
	input.ReasoningMode = runtimecontract.ReasoningUnsupported
	input.EffectiveReasoningEffort = ""
	input.ConfigOverlay = ""
	input.ModelProvider = &runtimecontract.ModelProviderConfiguration{Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git", SourceRevision: strings.Repeat("a", 40), Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp", ProviderID: "test-gateway", BaseURL: "https://gateway.example.invalid/v1", AddressCIDRs: []string{"192.0.2.40/32"}, Models: []runtimecontract.ModelProviderModel{{ID: "model-a", ContextWindowTokens: 32768, MaximumOutputTokens: 1024}}}
	input.EnvironmentPolicy.ManagedWorkspace = true
	input.EnvironmentTools = []runtimecontract.RuntimeEnvironmentTool{{Name: "Go", Command: "go"}, {Name: "Bash", Command: "bash"}}
	return input
}

// TestManagedWorkspaceActualPodRenderSeparatesCredentialsAndPreservesBudget runs the actual Pod
// producer, checks exact secret-free mounts/security, sums both resource partitions and refuses
// injected credentials, shared PID or a writable provider work mount.
// TestManagedWorkspaceActualPodRenderSeparatesCredentialsAndPreservesBudget вызывает actual Pod
// producer, проверяет exact mounts/security без секретов, сумму resource partitions и отказ при
// injected credentials, shared PID либо writable provider work mount.
func TestManagedWorkspaceActualPodRenderSeparatesCredentialsAndPreservesBudget(t *testing.T) {
	manager := newTestManager(t, fake.NewSimpleClientset())
	input, binding, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatal(err)
	}
	original := manager.runtimePod(input, binding, nil, "fixture-ticket", turnPodName(input.LeaseRef), "turn")
	if original == nil {
		t.Fatal("legacy render failed")
	}
	input = managedWorkspaceRenderInput(input)
	pod := manager.runtimePod(input, binding, nil, "fixture-ticket", turnPodName(input.LeaseRef), "turn")
	if pod == nil || len(pod.Spec.Containers) != 4 || !managedWorkspacePodMatches(pod, input) {
		t.Fatal("managed actual render denied")
	}
	executor := containerByName(t, pod.Spec.Containers, workspaceExecutorName)
	provider := containerByName(t, pod.Spec.Containers, "provider-runtime")
	if executor.SecurityContext.RunAsUser == nil || *executor.SecurityContext.RunAsUser != 10002 || executor.SecurityContext.RunAsGroup == nil || *executor.SecurityContext.RunAsGroup != 29000 || len(executor.VolumeMounts) != 3 || len(executor.Env) != 2 || len(executor.EnvFrom) != 0 {
		t.Fatal("executor boundary incomplete")
	}
	for _, name := range []string{"provider-auth", "runtime-input", "runtime-ticket", "callback-ca", "callback-client", "provider-socket", "provider-credential-relay"} {
		if hasMount(executor, name) {
			t.Fatal("credential/private mount reached command container", name)
		}
	}
	if hasMount(provider, workspaceExecutorSocketVolume) || hasMount(pod.Spec.Containers[2], workspaceExecutorSocketVolume) {
		t.Fatal("provider/relay can access command authority socket")
	}
	total := runtimePolicyResourceRequirements(input.EnvironmentPolicy.Resources)
	for _, pair := range []struct{ want, provider, executor corev1.ResourceList }{{total.Requests, provider.Resources.Requests, executor.Resources.Requests}, {total.Limits, provider.Resources.Limits, executor.Resources.Limits}} {
		for name, want := range pair.want {
			sum := pair.provider[name]
			sum.Add(pair.executor[name])
			if sum.Cmp(want) != 0 {
				t.Fatal("queue resource budget changed", name)
			}
		}
	}
	for _, mutate := range []func(*corev1.Pod){
		func(p *corev1.Pod) {
			p.Spec.Containers[3].Env = append(p.Spec.Containers[3].Env, corev1.EnvVar{Name: "UNEXPECTED", Value: "synthetic"})
		},
		func(p *corev1.Pod) {
			p.Spec.Containers[3].VolumeMounts = append(p.Spec.Containers[3].VolumeMounts, corev1.VolumeMount{Name: "provider-auth", MountPath: "/credential"})
		},
		func(p *corev1.Pod) { p.Spec.ShareProcessNamespace = boolPointer(true) },
		func(p *corev1.Pod) { p.Spec.HostPID = true },
		func(p *corev1.Pod) { p.Spec.Containers[1].VolumeMounts[0].ReadOnly = false },
	} {
		candidate := pod.DeepCopy()
		mutate(candidate)
		if managedWorkspacePodMatches(candidate, input) {
			t.Fatal("unsafe managed Pod accepted")
		}
	}
	before := original.DeepCopy()
	legacy := input
	legacy.EnvironmentPolicy.ManagedWorkspace = false
	if !configureManagedWorkspacePod(original, legacy) || !apiequality.Semantic.DeepEqual(original, before) {
		t.Fatal("opt-out changed legacy Pod")
	}
}

// TestManagedWorkspaceV2ObserverIncludesEveryWriter proves the real V2 producer and protected
// storage observer agree on the optional executor. Its still-running status cannot prove join.
// TestManagedWorkspaceV2ObserverIncludesEveryWriter доказывает согласованность actual V2 producer
// и protected storage observer по optional executor. Running status не доказывает join.
func TestManagedWorkspaceV2ObserverIncludesEveryWriter(t *testing.T) {
	manager, _, input, previous := workspaceStoragePodFixture(t)
	input = managedWorkspaceRenderInput(input)
	pod := manager.runtimePod(input, ProviderSecretBinding{Name: "runtime-provider-openai-default-r1"}, nil, "fixture-ticket", turnPodName(input.LeaseRef), "turn")
	if pod == nil {
		t.Fatal("managed V2 render denied")
	}
	pod.UID = previous.UID
	if !manager.workspaceStoragePodMatches(input, pod) {
		t.Fatal("V2 observer omitted managed writer")
	}
	for _, container := range pod.Spec.InitContainers {
		pod.Status.InitContainerStatuses = append(pod.Status.InitContainerStatuses, corev1.ContainerStatus{Name: container.Name, ContainerID: "containerd://synthetic-init", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}})
	}
	for _, container := range pod.Spec.Containers {
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{Name: container.Name, ContainerID: "containerd://synthetic-main", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}})
	}
	if !workspaceAllContainersTerminated(pod) {
		t.Fatal("positive exact all-container stop rejected")
	}
	pod.Status.ContainerStatuses[3].State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	if workspaceAllContainersTerminated(pod) {
		t.Fatal("running executor accepted as stopped")
	}
}
