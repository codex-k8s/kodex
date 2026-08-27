package workload

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testDefaultDigest = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
const testContractDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const testProviderDigest = "004ab004093ba6916de2d7fa718d1e1539157f24f04e747d0346e86e0a87556c"
const testArtifactDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

func TestRunAsLeaderHasCompleteClientGoCallbacks(t *testing.T) {
	t.Parallel()
	manager := newTestManager(t, fake.NewSimpleClientset())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := manager.RunAsLeader(ctx, func(context.Context) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("leader election did not preserve canceled lifecycle: %v", err)
	}
}

func TestAllowsLastKnownGoodObservationOnlyForTransientAPIFailure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "deadline", err: fmt.Errorf("list: %w", context.DeadlineExceeded), want: true},
		{name: "server unavailable", err: fmt.Errorf("list: %w", apierrors.NewServiceUnavailable("temporarily unavailable")), want: true},
		{name: "rate limited", err: fmt.Errorf("list: %w", apierrors.NewTooManyRequests("retry", 1)), want: true},
		{name: "forbidden", err: fmt.Errorf("list: %w", apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("denied"))), want: false},
		{name: "unknown integrity failure", err: errors.New("certificate signature rejected"), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := AllowsLastKnownGoodObservation(test.err); got != test.want {
				t.Fatalf("AllowsLastKnownGoodObservation() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestEnsureTurnMaterializesExactRoleImageAndIsolatesProviderCredential(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, binding, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	if err := manager.EnsureTurn(context.Background(), input, binding); err != nil {
		t.Fatalf("EnsureTurn() error = %v", err)
	}
	pod, err := client.CoreV1().Pods("kodex-system").Get(context.Background(), turnPodName(input.LeaseRef), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(Pod) error = %v", err)
	}
	if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
		t.Fatal("runtime Pod must not mount a Kubernetes service-account token")
	}
	if got := pod.Spec.Containers[0].Image; got != "registry.example/kodex/roles@"+testDigest {
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
	if len(input.InputArtifacts) != 1 || input.InputArtifacts[0].Ref != "artifact_abcdefgh" || input.InputArtifacts[0].Digest != testArtifactDigest {
		t.Fatalf("runtime artifact catalog = %#v", input.InputArtifacts)
	}
	if !hasEnv(pod.Spec.Containers[1], "HTTPS_PROXY", "http://egress-gateway.kodex-system.svc:8080") {
		t.Fatal("provider runtime is not fenced through the egress gateway")
	}
	secret, err := client.CoreV1().Secrets("kodex-system").Get(context.Background(), ticketName(input.LeaseRef), metav1.GetOptions{})
	if err != nil || secret.Immutable == nil || !*secret.Immutable || len(secret.Data[ticketKey]) != 64 {
		t.Fatalf("immutable execution ticket is invalid: err=%v", err)
	}
	if bytes.Contains(secret.Data[inputKey], []byte(binding.Name)) || bytes.Contains(secret.Data[inputKey], []byte(binding.UID)) {
		t.Fatal("Kubernetes provider Secret locator leaked into role-visible runtime input")
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("kodex-system").Get(context.Background(), sessionPVCName(input.SessionRef), metav1.GetOptions{}); err != nil {
		t.Fatalf("session volume was not materialized: %v", err)
	}
	pvc, err := client.CoreV1().PersistentVolumeClaims("kodex-system").Get(context.Background(), sessionPVCName(input.SessionRef), metav1.GetOptions{})
	if err != nil || pvc.Spec.StorageClassName != nil {
		t.Fatalf("session volume must use the cluster default StorageClass: storage_class=%v err=%v", pvc.Spec.StorageClassName, err)
	}
}

func TestManagerAcceptsOnlyDefaultOrValidExplicitStorageClass(t *testing.T) {
	t.Parallel()
	config := testManagerConfig()
	config.StorageClass = "fast.storage.example"
	if _, err := New(fake.NewSimpleClientset(), config); err != nil {
		t.Fatalf("valid explicit StorageClass was rejected: %v", err)
	}
	config.StorageClass = "invalid/storage-class"
	if _, err := New(fake.NewSimpleClientset(), config); err == nil {
		t.Fatal("invalid explicit StorageClass was accepted")
	}
}

func TestEnsureTurnRejectsProviderCredentialOutsideRuntimeRevision(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, binding, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	binding.ResourceVersion = "2"
	if err := manager.EnsureTurn(context.Background(), input, binding); err == nil {
		t.Fatal("EnsureTurn() accepted a provider Secret outside the immutable credential revision")
	}
}

func TestValidateImageAcceptsOnlyPromotedOrExactReleaseDefault(t *testing.T) {
	t.Parallel()
	manager := newTestManager(t, fake.NewSimpleClientset())
	input, _, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	if err := manager.validateImage(input); err != nil {
		t.Fatalf("promoted role image was rejected: %v", err)
	}
	input.ImageReference = manager.config.DefaultRoleImageReference
	input.ImageManifestDigest = testDefaultDigest
	if err := manager.validateImage(input); err != nil {
		t.Fatalf("exact release default image was rejected: %v", err)
	}
	input.ImageReference = "registry.example/kodex/other@" + testDefaultDigest
	if err := manager.validateImage(input); err == nil {
		t.Fatal("arbitrary pinned image was accepted")
	}
}

func TestTurnPodStateRejectsStaleWarmRevision(t *testing.T) {
	warmPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "system-assistant-warm", Namespace: "kodex-system", Annotations: map[string]string{revisionAnnotation: strings.Repeat("c", 64)}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	client := fake.NewSimpleClientset(warmPod)
	manager := newTestManager(t, client)
	input, _, err := manager.BuildTurnInput(testExecution(true))
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

func TestEnsureWarmRecreatesTerminalPod(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, binding, err := manager.BuildWarmInput(testExecution(true).GetRevision())
	if err != nil {
		t.Fatalf("BuildWarmInput() error = %v", err)
	}
	terminal := manager.runtimePod(input, binding, ticketName("warm-"+input.RuntimeRevisionRef), "system-assistant-warm", "warm")
	terminal.Status.Phase = corev1.PodFailed
	if _, err := client.CoreV1().Pods("kodex-system").Create(context.Background(), terminal, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Create(terminal warm Pod) error = %v", err)
	}
	ready, err := manager.EnsureWarm(context.Background(), input, binding)
	if err != nil {
		t.Fatalf("EnsureWarm() error = %v", err)
	}
	if ready {
		t.Fatal("new warm Pod cannot be ready before Kubernetes observation")
	}
	pod, err := client.CoreV1().Pods("kodex-system").Get(context.Background(), "system-assistant-warm", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(recreated warm Pod) error = %v", err)
	}
	if pod.Status.Phase == corev1.PodFailed || pod.Annotations[revisionAnnotation] != input.RuntimeRevisionDigest {
		t.Fatalf("terminal warm Pod was not recreated: phase=%q", pod.Status.Phase)
	}
}

func TestEnsureWarmReplacesTicketFromPreviousControllerInstance(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, binding, err := manager.BuildWarmInput(testExecution(true).GetRevision())
	if err != nil {
		t.Fatalf("BuildWarmInput() error = %v", err)
	}
	staleInput := input
	staleInput.WorkloadInstance = "previous-controller"
	raw, err := runtimecontract.EncodeRunnerInput(staleInput)
	if err != nil {
		t.Fatalf("EncodeRunnerInput() error = %v", err)
	}
	immutable := true
	secretName := ticketName("warm-" + input.RuntimeRevisionRef)
	_, err = client.CoreV1().Secrets("kodex-system").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "kodex-system",
			Annotations: map[string]string{revisionAnnotation: input.RuntimeRevisionDigest, controllerAnnotation: "previous-controller"}},
		Immutable: &immutable, Data: map[string][]byte{inputKey: raw, ticketKey: []byte(strings.Repeat("a", 64))},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create(stale warm ticket) error = %v", err)
	}
	if _, err := manager.EnsureWarm(context.Background(), input, binding); err != nil {
		t.Fatalf("EnsureWarm() error = %v", err)
	}
	current, err := client.CoreV1().Secrets("kodex-system").Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(current warm ticket) error = %v", err)
	}
	bound, err := runtimecontract.DecodeRunnerInput(current.Data[inputKey])
	if err != nil || bound.WorkloadInstance != "controller-pod-uid" || current.Annotations[controllerAnnotation] != "controller-pod-uid" {
		t.Fatalf("warm ticket still belongs to previous controller: input=%#v err=%v", bound, err)
	}
}

func TestEnsureTurnRejectsExistingPodFromAnotherRevision(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, binding, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	conflict := manager.runtimePod(input, binding, ticketName(input.LeaseRef), turnPodName(input.LeaseRef), "turn")
	conflict.Annotations[revisionAnnotation] = strings.Repeat("c", 64)
	if _, err := client.CoreV1().Pods("kodex-system").Create(context.Background(), conflict, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Create(conflict Pod) error = %v", err)
	}
	if err := manager.EnsureTurn(context.Background(), input, binding); err == nil {
		t.Fatal("EnsureTurn() accepted a Pod from another immutable revision")
	}
}

func newTestManager(t *testing.T, client *fake.Clientset) *Manager {
	t.Helper()
	manager, err := New(client, testManagerConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	immutable := true
	_, err = client.CoreV1().Secrets("kodex-system").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "runtime-provider-openai-default-r1", Namespace: "kodex-system",
			UID: "10000000-0000-4000-8000-000000000001", ResourceVersion: "1"},
		Immutable: &immutable,
		Data:      map[string][]byte{"auth.json": []byte(`{"auth":"fixture"}`), "auth.sha256": []byte(testProviderDigest)},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create provider credential fixture: %v", err)
	}
	return manager
}

func testManagerConfig() Config {
	return Config{
		Environment: "test", Namespace: "kodex-system", ControllerPodUID: "controller-pod-uid", ControllerPodIP: "10.0.0.10",
		CallbackTLSServerName:  "runtime-controller-callback.kodex-system.svc.cluster.local",
		CallbackClientCASecret: "runtime-execution-client-tls", CallbackClientTLSSecret: "runtime-execution-client-tls",
		ProviderHTTPSProxy: "http://egress-gateway.kodex-system.svc:8080",
		StorageClass:       "", SessionPVCSize: "20Gi", RunnerServiceAccount: "agent-runner",
		PromotedRoleImageRepository: "registry.example/kodex/roles",
		DefaultRoleImageReference:   "registry.example/kodex/agent-runner@" + testDefaultDigest,
		RoleRuntimeContractRevision: 1,
		RoleRuntimeContractSHA256:   testContractDigest, TurnCPUMilli: 2000, TurnMemoryBytes: 4 << 30,
	}
}

func testExecution(systemAssistant bool) *controlplanev1.ClaimedExecution {
	return &controlplanev1.ClaimedExecution{
		Run: &controlplanev1.Run{Ref: "run_abcdefgh"}, Node: &controlplanev1.RunNode{Ref: "node_abcdefgh"},
		Revision: &controlplanev1.RuntimeRevisionSnapshot{
			Ref: "revision_abcdefgh", Version: 1, SessionRef: "session_abcdefgh", TurnRef: "turn_abcdefgh", Attempt: 1,
			AgentRef: "agent_abcdefgh", Instructions: "Complete the server-owned task.", Runtime: &controlplanev1.RuntimeSelection{Provider: "openai", Model: "codex"},
			RevisionDigest: strings.Repeat("a", 64), SystemAssistant: systemAssistant,
			ImageReference: "registry.example/kodex/roles@" + testDigest, ImageManifestDigest: testDigest,
			RoleRuntimeContractRevision: 1, RoleRuntimeContractSha256: testContractDigest,
			Artifacts: []*controlplanev1.Artifact{{Ref: "artifact_abcdefgh", FileName: "brief.txt", MediaType: "text/plain", SizeBytes: 12, Digest: testArtifactDigest, Revision: 1, Version: 1}},
			ProviderCredential: &controlplanev1.ProviderCredentialBinding{
				AccountRef: "pacc_abcdefgh", CredentialRevisionRef: "pcr_abcdefgh", CredentialRevision: 1,
				SecretName: "runtime-provider-openai-default-r1", SecretUid: "10000000-0000-4000-8000-000000000001",
				SecretResourceVersion: "1", ContentSha256: testProviderDigest,
			},
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
