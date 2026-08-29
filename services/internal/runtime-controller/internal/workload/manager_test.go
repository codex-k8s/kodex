package workload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
	providerMounts := make(map[string]string)
	for _, mount := range pod.Spec.Containers[1].VolumeMounts {
		if mount.Name == "provider-auth" {
			if !mount.ReadOnly {
				t.Fatal("provider authentication mount is writable")
			}
			providerMounts[mount.MountPath] = mount.SubPath
		}
	}
	if providerMounts[input.ProviderAuthFile] != "auth.json" ||
		providerMounts[input.ProviderAuthSHA256File] != "auth.sha256" || len(providerMounts) != 2 {
		t.Fatalf("provider credentials are not mounted as exact subPath files: %#v", providerMounts)
	}
	providerSecurity := pod.Spec.Containers[1].SecurityContext
	if providerSecurity == nil || providerSecurity.RunAsUser == nil || *providerSecurity.RunAsUser != 10002 ||
		providerSecurity.AllowPrivilegeEscalation == nil || *providerSecurity.AllowPrivilegeEscalation ||
		providerSecurity.ReadOnlyRootFilesystem == nil || !*providerSecurity.ReadOnlyRootFilesystem ||
		providerSecurity.SeccompProfile == nil || providerSecurity.SeccompProfile.Type != corev1.SeccompProfileTypeUnconfined ||
		providerSecurity.AppArmorProfile == nil || providerSecurity.AppArmorProfile.Type != corev1.AppArmorProfileTypeUnconfined {
		t.Fatalf("provider sandbox security context = %#v", providerSecurity)
	}
	if input.CodexHome != "/workspace/.kodex/state/codex-home" {
		t.Fatalf("provider state path = %q; resumable Codex state must use the session volume", input.CodexHome)
	}
	if len(input.InputArtifacts) != 1 || input.InputArtifacts[0].Ref != "artifact_abcdefgh" || input.InputArtifacts[0].Digest != testArtifactDigest {
		t.Fatalf("runtime artifact catalog = %#v", input.InputArtifacts)
	}
	if input.ProjectRef != "prj_abcdefgh" {
		t.Fatalf("runtime project binding = %q", input.ProjectRef)
	}
	if !hasEnv(pod.Spec.Containers[1], "HTTPS_PROXY", "http://egress-gateway.kodex-system.svc:8080") {
		t.Fatal("provider runtime is not fenced through the egress gateway")
	}
	if !hasEnv(pod.Spec.Containers[0], "OTEL_SDK_DISABLED", "true") ||
		!hasEnv(pod.Spec.Containers[0], "DEPLOYMENT_ENVIRONMENT", "test") {
		t.Fatal("role runtime does not have a valid telemetry identity")
	}
	secret, err := client.CoreV1().Secrets("kodex-system").Get(context.Background(), ticketName(input.LeaseRef), metav1.GetOptions{})
	if err != nil || secret.Immutable == nil || !*secret.Immutable || len(secret.Data[ticketKey]) != 64 {
		t.Fatalf("immutable execution ticket is invalid: err=%v", err)
	}
	if bytes.Contains(secret.Data[inputKey], []byte(binding.Name)) || bytes.Contains(secret.Data[inputKey], []byte(binding.UID)) {
		t.Fatal("Kubernetes provider Secret locator leaked into role-visible runtime input")
	}
	sessionVolumeName, nameErr := runtimecontract.SessionPVCName(input.SessionRef)
	if nameErr != nil {
		t.Fatalf("derive session volume name: %v", nameErr)
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("kodex-system").Get(context.Background(), sessionVolumeName, metav1.GetOptions{}); err != nil {
		t.Fatalf("session volume was not materialized: %v", err)
	}
	pvc, err := client.CoreV1().PersistentVolumeClaims("kodex-system").Get(context.Background(), sessionVolumeName, metav1.GetOptions{})
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

func TestEnsureTurnMaterializesExactEnvironmentSecretOutsideRunnerInput(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	immutable := true
	secretValue := []byte("runtime-environment-secret-fixture")
	digest := sha256.Sum256(secretValue)
	digestHex := hex.EncodeToString(digest[:])
	source := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "runtime-agent-environment-r1", Namespace: "kodex-system",
			UID: "20000000-0000-4000-8000-000000000001", ResourceVersion: "7",
		},
		Immutable: &immutable,
		Data:      map[string][]byte{"token": secretValue},
	}
	if _, err := client.CoreV1().Secrets("kodex-system").Create(context.Background(), source, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create runtime environment Secret fixture: %v", err)
	}

	execution := testExecution(false)
	execution.Revision.EnvironmentValues = []*controlplanev1.RuntimeEnvironmentValue{{Name: "FEATURE_FLAG", Value: "enabled"}}
	execution.Revision.SecretProjections = []*controlplanev1.RuntimeSecretDescriptor{{
		Name: "SERVICE_TOKEN", SecretName: source.Name, SecretKey: "token", SecretUid: string(source.UID),
		SecretResourceVersion: source.ResourceVersion, ContentSha256: digestHex,
	}}
	values := []runtimecontract.RuntimeEnvironmentValue{{Name: "FEATURE_FLAG", Value: "enabled"}}
	projections := []runtimecontract.RuntimeSecretProjection{{
		Name: "SERVICE_TOKEN", SecretName: source.Name, SecretKey: "token", SecretUID: string(source.UID),
		SecretResourceVersion: source.ResourceVersion, ContentSHA256: digestHex,
	}}
	image, tools := runtimeEnvironmentContract(execution.Revision)
	environmentDigest, err := runtimecontract.RuntimeEnvironmentDigest(values, projections, image, tools)
	if err != nil {
		t.Fatalf("RuntimeEnvironmentDigest() error = %v", err)
	}
	execution.Revision.RuntimeEnvironmentDigest = environmentDigest
	input, binding, err := manager.BuildTurnInput(execution)
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	if err := manager.EnsureTurn(context.Background(), input, binding); err != nil {
		t.Fatalf("EnsureTurn() error = %v", err)
	}

	ticket, err := client.CoreV1().Secrets("kodex-system").Get(context.Background(), ticketName(input.LeaseRef), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(runtime ticket Secret) error = %v", err)
	}
	if bytes.Contains(ticket.Data[inputKey], secretValue) {
		t.Fatal("runtime.json contains a Secret value")
	}
	projectionKey := environmentProjectionKey("SERVICE_TOKEN")
	if !bytes.Equal(ticket.Data[projectionKey], secretValue) {
		t.Fatal("execution ticket does not contain the exact verified Secret projection")
	}
	bound, err := runtimecontract.DecodeRunnerInput(ticket.Data[inputKey])
	if err != nil || len(bound.SecretProjections) != 1 || bound.SecretProjections[0] != projections[0] {
		t.Fatalf("runtime.json does not preserve the exact Secret descriptor: input=%#v err=%v", bound.SecretProjections, err)
	}

	pod, err := client.CoreV1().Pods("kodex-system").Get(context.Background(), turnPodName(input.LeaseRef), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(runtime Pod) error = %v", err)
	}
	role := containerByName(t, pod.Spec.Containers, "role-runtime")
	provider := containerByName(t, pod.Spec.Containers, "provider-runtime")
	for _, item := range role.Env {
		if item.ValueFrom != nil && item.ValueFrom.SecretKeyRef != nil {
			t.Fatalf("role runtime received a Secret projection: %#v", item)
		}
	}
	var projected *corev1.SecretKeySelector
	for _, item := range provider.Env {
		if item.Name == "SERVICE_TOKEN" && item.ValueFrom != nil {
			projected = item.ValueFrom.SecretKeyRef
		}
	}
	if projected == nil || projected.Name != ticket.Name || projected.Key != projectionKey ||
		projected.Optional == nil || *projected.Optional {
		t.Fatalf("provider runtime Secret projection is not exact: %#v", projected)
	}
}

func TestEnsureTurnRejectsStaleEnvironmentSecretRevision(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	immutable := true
	secretValue := []byte("runtime-environment-secret-fixture")
	digest := sha256.Sum256(secretValue)
	if _, err := client.CoreV1().Secrets("kodex-system").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "runtime-agent-environment-r1", Namespace: "kodex-system",
			UID: "20000000-0000-4000-8000-000000000001", ResourceVersion: "7"},
		Immutable: &immutable, Data: map[string][]byte{"token": secretValue},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create runtime environment Secret fixture: %v", err)
	}
	execution := testExecution(false)
	execution.Revision.SecretProjections = []*controlplanev1.RuntimeSecretDescriptor{{
		Name: "SERVICE_TOKEN", SecretName: "runtime-agent-environment-r1", SecretKey: "token",
		SecretUid: "20000000-0000-4000-8000-000000000001", SecretResourceVersion: "8",
		ContentSha256: hex.EncodeToString(digest[:]),
	}}
	projections := []runtimecontract.RuntimeSecretProjection{{
		Name: "SERVICE_TOKEN", SecretName: "runtime-agent-environment-r1", SecretKey: "token",
		SecretUID: "20000000-0000-4000-8000-000000000001", SecretResourceVersion: "8",
		ContentSHA256: hex.EncodeToString(digest[:]),
	}}
	image, tools := runtimeEnvironmentContract(execution.Revision)
	environmentDigest, err := runtimecontract.RuntimeEnvironmentDigest(nil, projections, image, tools)
	if err != nil {
		t.Fatalf("RuntimeEnvironmentDigest() error = %v", err)
	}
	execution.Revision.RuntimeEnvironmentDigest = environmentDigest
	input, binding, err := manager.BuildTurnInput(execution)
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	if err := manager.EnsureTurn(context.Background(), input, binding); err == nil {
		t.Fatal("EnsureTurn() accepted a stale runtime Secret resourceVersion")
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

func TestBuildTurnInputCarriesExactEnvironmentImageAndSelectedTools(t *testing.T) {
	t.Parallel()
	manager := newTestManager(t, fake.NewSimpleClientset())
	execution := testExecution(false)
	input, _, err := manager.BuildTurnInput(execution)
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	if input.EnvironmentImage.ArtifactRef != execution.Revision.GetRoleImageArtifactRef() ||
		input.EnvironmentImage.RecipeRef != execution.Revision.GetRoleImageRecipeRef() ||
		input.EnvironmentImage.RecipeGeneration != execution.Revision.GetRoleImageRecipeGeneration() ||
		input.EnvironmentImage.Reference != execution.Revision.GetImageReference() ||
		input.EnvironmentImage.Digest != execution.Revision.GetImageManifestDigest() {
		t.Fatalf("runner workload lost exact environment image: %#v", input.EnvironmentImage)
	}
	if len(input.EnvironmentTools) != 1 || input.EnvironmentTools[0].Command != "gh" ||
		input.EnvironmentTools[0].UsageHint != "Используй gh api" {
		t.Fatalf("runner workload lost selected tools: %#v", input.EnvironmentTools)
	}
}

func TestBuildTurnInputSelectsCodexSandboxFromArtifactCapability(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		capabilities []*controlplanev1.PlatformCapability
		want         string
	}{
		{
			name:         "artifact output",
			capabilities: []*controlplanev1.PlatformCapability{{Key: runtimecontract.ArtifactCapability}},
			want:         "workspace-write",
		},
		{name: "no capability", want: "read-only"},
		{
			name:         "unknown workspace capability",
			capabilities: []*controlplanev1.PlatformCapability{{Key: "platform.workspace.write"}},
			want:         "read-only",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			execution := testExecution(false)
			execution.Revision.Capabilities = test.capabilities
			if test.want == "read-only" {
				execution.Revision.AttachmentSetRef = ""
				execution.Revision.AttachmentSetManifestDigest = ""
				execution.Revision.AttachmentContext = ""
				execution.Revision.InputArtifacts = nil
			}
			manager := newTestManager(t, fake.NewSimpleClientset())
			input, _, err := manager.BuildTurnInput(execution)
			if err != nil {
				t.Fatalf("BuildTurnInput() error = %v", err)
			}
			if input.CodexSandbox != test.want {
				t.Fatalf("CodexSandbox = %q, want %q", input.CodexSandbox, test.want)
			}
		})
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
	state, err := manager.TurnPodState(context.Background(), input, true)
	if err != nil {
		t.Fatalf("TurnPodState() error = %v", err)
	}
	if state != "CONFLICT" {
		t.Fatalf("TurnPodState() = %q, want CONFLICT", state)
	}
}

func TestTurnPodStateUsesColdPodForSystemAssistantFallback(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, binding, err := manager.BuildTurnInput(testExecution(true))
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	if err := manager.EnsureTurn(context.Background(), input, binding); err != nil {
		t.Fatalf("EnsureTurn() error = %v", err)
	}
	state, err := manager.TurnPodState(context.Background(), input, false)
	if err != nil {
		t.Fatalf("TurnPodState() error = %v", err)
	}
	if state != "UNKNOWN" {
		t.Fatalf("TurnPodState() = %q, want UNKNOWN for fake cold Pod", state)
	}
}

func TestTurnPodStateClassifiesColdRuntimeContainers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		statuses   []corev1.ContainerStatus
		conditions []corev1.PodCondition
		want       string
	}{
		{
			name: "role terminated while provider is running",
			statuses: []corev1.ContainerStatus{
				{Name: "role-runtime", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}},
				{Name: "provider-runtime", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			},
			want: "FAILED",
		},
		{
			name: "provider terminated while role is running",
			statuses: []corev1.ContainerStatus{
				{Name: "role-runtime", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
				{Name: "provider-runtime", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}},
			},
			want: "FAILED",
		},
		{
			name: "both running but pod is not ready",
			statuses: []corev1.ContainerStatus{
				{Name: "role-runtime", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
				{Name: "provider-runtime", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			},
			want: "STARTING",
		},
		{
			name: "both running and pod is ready",
			statuses: []corev1.ContainerStatus{
				{Name: "role-runtime", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
				{Name: "provider-runtime", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			},
			conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			want:       "READY",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := fake.NewSimpleClientset()
			manager := newTestManager(t, client)
			input, binding, err := manager.BuildTurnInput(testExecution(false))
			if err != nil {
				t.Fatalf("BuildTurnInput() error = %v", err)
			}
			pod := manager.runtimePod(input, binding, ticketName(input.LeaseRef), turnPodName(input.LeaseRef), "turn")
			pod.Status = corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: test.statuses, Conditions: test.conditions}
			if _, err := client.CoreV1().Pods("kodex-system").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
				t.Fatalf("Create(cold runtime Pod) error = %v", err)
			}
			state, err := manager.TurnPodState(context.Background(), input, false)
			if err != nil {
				t.Fatalf("TurnPodState() error = %v", err)
			}
			if state != test.want {
				t.Fatalf("TurnPodState() = %q, want %q", state, test.want)
			}
		})
	}
}

func TestWarmCompatibilityIgnoresTurnIdentityButRejectsRuntimeDrift(t *testing.T) {
	t.Parallel()
	manager := newTestManager(t, fake.NewSimpleClientset())
	warm, _, err := manager.BuildWarmInput(testExecution(true).GetRevision())
	if err != nil {
		t.Fatalf("BuildWarmInput() error = %v", err)
	}
	turn, _, err := manager.BuildTurnInput(testExecution(true))
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	turn.RuntimeRevisionRef = "revision_turn1234"
	turn.RuntimeRevisionDigest = strings.Repeat("e", 64)
	turn.SessionRef = "session_turn1234"
	turn.Task = "A different bounded turn input."
	warmDigest, err := runtimecontract.WarmCompatibilityDigest(warm)
	if err != nil {
		t.Fatalf("WarmCompatibilityDigest(warm) error = %v", err)
	}
	turnDigest, err := runtimecontract.WarmCompatibilityDigest(turn)
	if err != nil {
		t.Fatalf("WarmCompatibilityDigest(turn) error = %v", err)
	}
	if warmDigest != turnDigest {
		t.Fatalf("turn identity changed warm compatibility: warm=%s turn=%s", warmDigest, turnDigest)
	}
	turn.Model = "different-model"
	drifted, err := runtimecontract.WarmCompatibilityDigest(turn)
	if err != nil {
		t.Fatalf("WarmCompatibilityDigest(drifted turn) error = %v", err)
	}
	if drifted == warmDigest {
		t.Fatal("model drift preserved warm compatibility")
	}
}

func TestEnsureWarmRecreatesTerminalPod(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, binding, err := manager.BuildWarmInput(testExecution(true).GetRevision())
	if err != nil {
		t.Fatalf("BuildWarmInput() error = %v", err)
	}
	terminal := manager.runtimePod(input, binding, manager.warmTicketName(input.RuntimeRevisionRef, input.RuntimeRevisionDigest), "system-assistant-warm", "warm")
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

func TestEnsureWarmRecreatesRunningPodWithTerminatedRuntime(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, binding, err := manager.BuildWarmInput(testExecution(true).GetRevision())
	if err != nil {
		t.Fatalf("BuildWarmInput() error = %v", err)
	}
	terminal := manager.runtimePod(input, binding, manager.warmTicketName(input.RuntimeRevisionRef, input.RuntimeRevisionDigest), "system-assistant-warm", "warm")
	terminal.Status.Phase = corev1.PodRunning
	terminal.Status.ContainerStatuses = []corev1.ContainerStatus{
		{Name: "role-runtime", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}},
		{Name: "provider-runtime", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
	}
	if _, err := client.CoreV1().Pods("kodex-system").Create(context.Background(), terminal, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Create(terminal warm Pod) error = %v", err)
	}
	ready, err := manager.EnsureWarm(context.Background(), input, binding)
	if err != nil {
		t.Fatalf("EnsureWarm() error = %v", err)
	}
	if ready {
		t.Fatal("recreated warm Pod cannot be ready before Kubernetes observation")
	}
	pod, err := client.CoreV1().Pods("kodex-system").Get(context.Background(), "system-assistant-warm", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(recreated warm Pod) error = %v", err)
	}
	if runtimePodTerminal(pod) {
		t.Fatalf("running warm Pod with a terminated runtime was not recreated: %#v", pod.Status.ContainerStatuses)
	}
}

func TestEnsureWarmRotatesTerminalTicketAndDeletesStaleWarmTickets(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, binding, err := manager.BuildWarmInput(testExecution(true).GetRevision())
	if err != nil {
		t.Fatalf("BuildWarmInput() error = %v", err)
	}
	secretName := manager.warmTicketName(input.RuntimeRevisionRef, input.RuntimeRevisionDigest)
	oldToken := strings.Repeat("a", 64)
	if err := manager.ensureTicket(context.Background(), secretName, "system-assistant-warm", "warm", input, oldToken, nil); err != nil {
		t.Fatalf("ensureTicket() error = %v", err)
	}
	staleInput := input
	staleInput.RuntimeRevisionRef = "runtime_revision_stale"
	staleInput.RuntimeRevisionDigest = strings.Repeat("d", 64)
	if err := manager.ensureTicket(context.Background(), manager.warmTicketName(staleInput.RuntimeRevisionRef, staleInput.RuntimeRevisionDigest), "system-assistant-warm", "warm", staleInput, strings.Repeat("b", 64), nil); err != nil {
		t.Fatalf("ensure stale ticket error = %v", err)
	}
	terminal := manager.runtimePod(input, binding, secretName, "system-assistant-warm", "warm")
	terminal.Status.Phase = corev1.PodFailed
	if _, err := client.CoreV1().Pods("kodex-system").Create(context.Background(), terminal, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Create(terminal warm Pod) error = %v", err)
	}
	if _, err := manager.EnsureWarm(context.Background(), input, binding); err != nil {
		t.Fatalf("EnsureWarm() error = %v", err)
	}
	current, err := client.CoreV1().Secrets("kodex-system").Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(current warm ticket) error = %v", err)
	}
	if bytes.Equal(current.Data[ticketKey], []byte(oldToken)) {
		t.Fatal("terminal warm Pod reused its execution ticket")
	}
	items, err := client.CoreV1().Secrets("kodex-system").List(context.Background(), metav1.ListOptions{
		LabelSelector: labels.Set{managedLabel: "true", modeLabel: "warm"}.AsSelector().String(),
	})
	if err != nil {
		t.Fatalf("List(warm tickets) error = %v", err)
	}
	if len(items.Items) != 1 || items.Items[0].Name != secretName {
		t.Fatalf("warm tickets after reconciliation = %#v", items.Items)
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
	secretName := ticketName("warm-legacy-" + input.RuntimeRevisionRef)
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
	current, err := client.CoreV1().Secrets("kodex-system").Get(context.Background(), manager.warmTicketName(input.RuntimeRevisionRef, input.RuntimeRevisionDigest), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(current warm ticket) error = %v", err)
	}
	bound, err := runtimecontract.DecodeRunnerInput(current.Data[inputKey])
	if err != nil || bound.WorkloadInstance != "controller-pod-uid" || current.Annotations[controllerAnnotation] != "controller-pod-uid" {
		t.Fatalf("warm ticket still belongs to previous controller: input=%#v err=%v", bound, err)
	}
}

func TestEnsureWarmReplacesTicketWithStaleCallbackAddress(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, binding, err := manager.BuildWarmInput(testExecution(true).GetRevision())
	if err != nil {
		t.Fatalf("BuildWarmInput() error = %v", err)
	}
	staleInput := input
	staleInput.CallbackURL = "https://10.0.0.9:8444"
	raw, err := runtimecontract.EncodeRunnerInput(staleInput)
	if err != nil {
		t.Fatalf("EncodeRunnerInput() error = %v", err)
	}
	immutable := true
	secretName := manager.warmTicketName(input.RuntimeRevisionRef, input.RuntimeRevisionDigest)
	_, err = client.CoreV1().Secrets("kodex-system").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "kodex-system",
			Annotations: map[string]string{revisionAnnotation: input.RuntimeRevisionDigest, controllerAnnotation: manager.config.ControllerPodUID}},
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
	if err != nil || bound.CallbackURL != input.CallbackURL {
		t.Fatalf("warm ticket retained stale callback address: input=%#v err=%v", bound, err)
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
	execution := &controlplanev1.ClaimedExecution{
		Run: &controlplanev1.Run{Ref: "run_abcdefgh", ProjectRef: "prj_abcdefgh"}, Node: &controlplanev1.RunNode{Ref: "node_abcdefgh"},
		Revision: &controlplanev1.RuntimeRevisionSnapshot{
			Ref: "revision_abcdefgh", Version: 1, SessionRef: "session_abcdefgh", TurnRef: "turn_abcdefgh", Attempt: 1,
			AgentRef: "agent_abcdefgh", Instructions: "Complete the server-owned task.", Runtime: &controlplanev1.RuntimeSelection{Provider: "openai", Model: "codex"},
			RevisionDigest: strings.Repeat("a", 64), SystemAssistant: systemAssistant,
			ImageReference: "registry.example/kodex/roles@" + testDigest, ImageManifestDigest: testDigest,
			RoleRuntimeContractRevision: 1, RoleRuntimeContractSha256: testContractDigest,
			Capabilities:     []*controlplanev1.PlatformCapability{{Key: runtimecontract.ArtifactCapability}},
			AttachmentSetRef: "aset_abcdefgh", AttachmentSetManifestDigest: strings.Repeat("4", 64), AttachmentContext: "RUN_INPUT",
			InputArtifacts: []*controlplanev1.RuntimeInputArtifact{{
				Artifact: &controlplanev1.Artifact{Ref: "artifact_abcdefgh", FileName: "brief.txt", MediaType: "text/plain", SizeBytes: 12, Digest: testArtifactDigest, Revision: 1, Version: 1, Source: controlplanev1.ArtifactSource_ARTIFACT_SOURCE_CONTROL_CENTER},
				Scope:    "INPUT", Position: 1,
			}},
			ProviderCredential: &controlplanev1.ProviderCredentialBinding{
				AccountRef: "pacc_abcdefgh", CredentialRevisionRef: "pcr_abcdefgh", CredentialRevision: 1,
				SecretName: "runtime-provider-openai-default-r1", SecretUid: "10000000-0000-4000-8000-000000000001",
				SecretResourceVersion: "1", ContentSha256: testProviderDigest,
			},
			RuntimeConfigRef: "rconf_abcdefgh", RuntimeConfigVersion: 1, RuntimeConfigDigest: strings.Repeat("1", 64),
			ProviderPolicyRef: "ppol_abcdefgh", ProviderPolicyVersion: 1, ProviderPolicyDigest: strings.Repeat("2", 64),
			ConfigOverlayRef: "cover_abcdefgh", ConfigOverlayVersion: 1,
			ConfigOverlayDigest:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			RuntimeEnvironmentRef: "renv_abcdefgh", RuntimeEnvironmentVersion: 1,
			RuntimeEnvironmentDigest: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			EnvironmentBindingRef:    "aenv_abcdefgh", EnvironmentBindingVersion: 1, EnvironmentBindingDigest: strings.Repeat("3", 64),
		},
		Lease: &controlplanev1.WorkLease{Ref: "lease_abcdefgh", Fence: "fence-1", Generation: 1}, Task: "Prepare the result.",
	}
	if !systemAssistant {
		execution.Revision.RoleImageRecipeRef = "imgrec_abcdefgh"
		execution.Revision.RoleImageArtifactRef = "imgart_abcdefgh"
		execution.Revision.RoleImageRecipeGeneration = 1
		execution.Revision.EnvironmentTools = []*controlplanev1.RuntimeEnvironmentTool{{
			Name: "GitHub CLI", Command: "gh", Description: "Работа с GitHub", UsageHint: "Используй gh api",
		}}
	}
	image, tools := runtimeEnvironmentContract(execution.Revision)
	execution.Revision.RuntimeEnvironmentDigest, _ = runtimecontract.RuntimeEnvironmentDigest(nil, nil, image, tools)
	return execution
}

func runtimeEnvironmentContract(revision *controlplanev1.RuntimeRevisionSnapshot) (runtimecontract.RuntimeEnvironmentImage, []runtimecontract.RuntimeEnvironmentTool) {
	image := runtimecontract.RuntimeEnvironmentImage{
		ArtifactRef: revision.GetRoleImageArtifactRef(), RecipeRef: revision.GetRoleImageRecipeRef(),
		RecipeGeneration: revision.GetRoleImageRecipeGeneration(), Reference: revision.GetImageReference(),
		Digest: revision.GetImageManifestDigest(),
	}
	tools := make([]runtimecontract.RuntimeEnvironmentTool, 0, len(revision.GetEnvironmentTools()))
	for _, tool := range revision.GetEnvironmentTools() {
		tools = append(tools, runtimecontract.RuntimeEnvironmentTool{
			Name: tool.GetName(), Command: tool.GetCommand(), Description: tool.GetDescription(), UsageHint: tool.GetUsageHint(),
		})
	}
	return image, tools
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

func containerByName(t *testing.T, containers []corev1.Container, name string) corev1.Container {
	t.Helper()
	for _, container := range containers {
		if container.Name == name {
			return container
		}
	}
	t.Fatalf("container %q is absent", name)
	return corev1.Container{}
}
