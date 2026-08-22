package workload

import (
	"context"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testContractDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func TestEnsureTurnMaterializesExactRoleImageAndIsolatesProviderCredential(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	if err := manager.EnsureTurn(context.Background(), input); err != nil {
		t.Fatalf("EnsureTurn() error = %v", err)
	}
	pod, err := client.CoreV1().Pods("mattercodex-system").Get(context.Background(), turnPodName(input.LeaseRef), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(Pod) error = %v", err)
	}
	if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
		t.Fatal("runtime Pod must not mount a Kubernetes service-account token")
	}
	if got := pod.Spec.Containers[0].Image; got != "registry.example/mattercodex/roles@"+testDigest {
		t.Fatalf("role image = %q", got)
	}
	if got := pod.Spec.Containers[1].Image; got != pod.Spec.Containers[0].Image {
		t.Fatalf("provider image = %q, role image = %q", got, pod.Spec.Containers[0].Image)
	}
	if hasMount(pod.Spec.Containers[0], "provider-auth") {
		t.Fatal("role runtime can read provider authentication")
	}
	if !hasMount(pod.Spec.Containers[1], "provider-auth") {
		t.Fatal("provider runtime has no provider authentication mount")
	}
	if input.CodexHome != "/tmp/codex-home" {
		t.Fatalf("provider state path = %q; secret-bearing state must not use the session volume", input.CodexHome)
	}
	if !hasEnv(pod.Spec.Containers[1], "HTTPS_PROXY", "http://egress-gateway.mattercodex-system.svc:8080") {
		t.Fatal("provider runtime is not fenced through the egress gateway")
	}
	secret, err := client.CoreV1().Secrets("mattercodex-system").Get(context.Background(), ticketName(input.LeaseRef), metav1.GetOptions{})
	if err != nil || secret.Immutable == nil || !*secret.Immutable || len(secret.Data[ticketKey]) != 64 {
		t.Fatalf("immutable execution ticket is invalid: err=%v", err)
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("mattercodex-system").Get(context.Background(), sessionPVCName(input.SessionRef), metav1.GetOptions{}); err != nil {
		t.Fatalf("session volume was not materialized: %v", err)
	}
}

func TestTurnPodStateRejectsStaleWarmRevision(t *testing.T) {
	warmPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "system-assistant-warm", Namespace: "mattercodex-system", Annotations: map[string]string{revisionAnnotation: strings.Repeat("c", 64)}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	client := fake.NewSimpleClientset(warmPod)
	manager := newTestManager(t, client)
	input, err := manager.BuildTurnInput(testExecution(true))
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	state, err := manager.TurnPodState(context.Background(), input)
	if err != nil {
		t.Fatalf("TurnPodState() error = %v", err)
	}
	if state != "CONFLICT" {
		t.Fatalf("TurnPodState() = %q, want CONFLICT", state)
	}
}

func TestEnsureTurnRejectsExistingPodFromAnotherRevision(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	conflict := manager.runtimePod(input, ticketName(input.LeaseRef), turnPodName(input.LeaseRef), "turn")
	conflict.Annotations[revisionAnnotation] = strings.Repeat("c", 64)
	if _, err := client.CoreV1().Pods("mattercodex-system").Create(context.Background(), conflict, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Create(conflict Pod) error = %v", err)
	}
	if err := manager.EnsureTurn(context.Background(), input); err == nil {
		t.Fatal("EnsureTurn() accepted a Pod from another immutable revision")
	}
}

func newTestManager(t *testing.T, client *fake.Clientset) *Manager {
	t.Helper()
	manager, err := New(client, Config{
		Environment: "test", Namespace: "mattercodex-system", ControllerPodUID: "controller-pod-uid", ControllerPodIP: "10.0.0.10",
		CallbackTLSServerName:  "runtime-controller-callback.mattercodex-system.svc.cluster.local",
		CallbackClientCASecret: "runtime-execution-client-tls", CallbackClientTLSSecret: "runtime-execution-client-tls",
		ProviderAuthSecret: "runtime-provider-auth", ProviderHTTPSProxy: "http://egress-gateway.mattercodex-system.svc:8080",
		StorageClass: "runtime-session", SessionPVCSize: "20Gi", RunnerServiceAccount: "agent-runner",
		PromotedRoleImageRepository: "registry.example/mattercodex/roles", RoleRuntimeContractRevision: 1,
		RoleRuntimeContractSHA256: testContractDigest, TurnCPUMilli: 2000, TurnMemoryBytes: 4 << 30,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return manager
}

func testExecution(systemAssistant bool) *controlplanev1.ClaimedExecution {
	return &controlplanev1.ClaimedExecution{
		Run: &controlplanev1.Run{Ref: "run_abcdefgh"}, Node: &controlplanev1.RunNode{Ref: "node_abcdefgh"},
		Revision: &controlplanev1.RuntimeRevisionSnapshot{
			Ref: "revision_abcdefgh", Version: 1, SessionRef: "session_abcdefgh", TurnRef: "turn_abcdefgh", Attempt: 1,
			AgentRef: "agent_abcdefgh", Instructions: "Complete the server-owned task.", Runtime: &controlplanev1.RuntimeSelection{Provider: "openai", Model: "codex"},
			RevisionDigest: strings.Repeat("a", 64), SystemAssistant: systemAssistant,
			ImageReference: "registry.example/mattercodex/roles@" + testDigest, ImageManifestDigest: testDigest,
			RoleRuntimeContractRevision: 1, RoleRuntimeContractSha256: testContractDigest,
		},
		Lease: &controlplanev1.WorkLease{Ref: "lease_abcdefgh", Fence: "fence-1", Generation: 1}, Task: "Prepare the result.",
	}
}

func hasMount(container corev1.Container, name string) bool {
	for _, mount := range container.VolumeMounts {
		if mount.Name == name {
			return true
		}
	}
	return false
}

func hasEnv(container corev1.Container, name, value string) bool {
	for _, item := range container.Env {
		if item.Name == name && item.Value == value {
			return true
		}
	}
	return false
}
