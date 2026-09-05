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
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
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
	credentials := testCredentialProjection(input)
	if err := manager.EnsureTurn(context.Background(), input, binding, credentials); err != nil {
		t.Fatalf("EnsureTurn() error = %v", err)
	}
	pod, err := client.CoreV1().Pods("kodex-runtime").Get(context.Background(), turnPodName(input.LeaseRef), metav1.GetOptions{})
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
	role := containerByName(t, pod.Spec.Containers, "role-runtime")
	provider := containerByName(t, pod.Spec.Containers, "provider-runtime")
	relay := containerByName(t, pod.Spec.Containers, "provider-credential-relay")
	init := containerByName(t, pod.Spec.InitContainers, "workspace-init")
	if relay.Image != testManagerConfig().DefaultRoleImageReference || relay.Image == provider.Image {
		t.Fatalf("provider credential relay image = %q, provider image = %q", relay.Image, provider.Image)
	}
	if hasMount(role, "provider-credential-relay") || hasMount(role, "provider-auth") {
		t.Fatal("role runtime can reach provider credential material")
	}
	if hasMount(provider, "callback-ca") || hasMount(provider, "callback-client") ||
		hasMountAt(provider, "runtime-ticket", input.ExecutionTicketFile) {
		t.Fatal("provider runtime received callback authority")
	}
	if !hasMount(provider, "provider-auth") || !hasMount(provider, "provider-socket") ||
		!hasMount(provider, "provider-credential-relay") {
		t.Fatal("provider runtime is missing an isolated provider or relay socket")
	}
	for _, forbidden := range []string{"provider-auth", "session", "provider-socket", "kube-api-access"} {
		if hasMount(relay, forbidden) {
			t.Fatalf("provider credential relay received forbidden mount %q", forbidden)
		}
	}
	for _, required := range []string{"runtime-input", "runtime-ticket", "callback-ca", "callback-client", "provider-credential-relay"} {
		if !hasMount(relay, required) {
			t.Fatalf("provider credential relay is missing mount %q", required)
		}
	}
	if !hasMountAt(relay, "runtime-ticket", input.ExecutionTicketFile) || relay.SecurityContext == nil ||
		relay.SecurityContext.RunAsUser == nil || *relay.SecurityContext.RunAsUser != 10003 ||
		relay.SecurityContext.SeccompProfile == nil || relay.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatalf("provider credential relay boundary is invalid: %#v", relay)
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
	if providerMounts[input.ProviderAuthFile] != credentials.ProviderAuthKey || len(providerMounts) != 1 ||
		!hasMountAt(provider, "runtime-input", input.ProviderAuthSHA256File) {
		t.Fatalf("provider credentials are not mounted as exact subPath files: %#v", providerMounts)
	}
	providerSecurity := pod.Spec.Containers[1].SecurityContext
	if providerSecurity == nil || providerSecurity.RunAsUser == nil || *providerSecurity.RunAsUser != 10002 ||
		providerSecurity.AllowPrivilegeEscalation == nil || *providerSecurity.AllowPrivilegeEscalation ||
		providerSecurity.ReadOnlyRootFilesystem == nil || !*providerSecurity.ReadOnlyRootFilesystem ||
		providerSecurity.SeccompProfile == nil || providerSecurity.SeccompProfile.Type != corev1.SeccompProfileTypeUnconfined ||
		providerSecurity.AppArmorProfile == nil || providerSecurity.AppArmorProfile.Type != corev1.AppArmorProfileTypeLocalhost ||
		providerSecurity.AppArmorProfile.LocalhostProfile == nil || *providerSecurity.AppArmorProfile.LocalhostProfile != "kodex-provider-runtime" {
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
	secret, err := client.CoreV1().Secrets("kodex-runtime").Get(context.Background(), ticketName(input.LeaseRef), metav1.GetOptions{})
	if err != nil || secret.Immutable == nil || !*secret.Immutable || len(secret.Data[ticketKey]) != 64 {
		t.Fatalf("immutable execution ticket is invalid: err=%v", err)
	}
	if bytes.Contains(secret.Data[inputKey], []byte(binding.Name)) || bytes.Contains(secret.Data[inputKey], []byte(binding.UID)) {
		t.Fatal("Kubernetes provider Secret locator leaked into role-visible runtime input")
	}
	if len(secret.Data) != 2 {
		t.Fatalf("execution ticket contains credential values: keys=%d", len(secret.Data))
	}
	projection, err := client.CoreV1().ConfigMaps("kodex-runtime").Get(context.Background(), runtimeProjectionName(input), metav1.GetOptions{})
	if err != nil || projection.Immutable == nil || !*projection.Immutable || len(projection.BinaryData) != 0 {
		t.Fatalf("immutable runtime projection is invalid: err=%v projection=%#v", err, projection)
	}
	expectedProjectionKeys := []string{inputKey, workspacePolicyKey, inputManifestKey, resultManifestKey, skillManifestKey, memoryManifestKey, mcpManifestKey, callbackManifestKey, providerDigestKey}
	if len(projection.Data) != len(expectedProjectionKeys) {
		t.Fatalf("runtime projection keys = %#v", projection.Data)
	}
	for _, key := range expectedProjectionKeys {
		if projection.Data[key] == "" {
			t.Fatalf("runtime projection key %q is empty", key)
		}
	}
	if projection.Data[providerDigestKey] != input.ProviderCredentialSHA256 ||
		pod.Annotations[credentialProjectionNameAnnotation] != credentials.SecretName ||
		pod.Annotations[credentialProjectionUIDAnnotation] != credentials.SecretUID ||
		pod.Annotations[credentialProjectionVersionAnnotation] != credentials.SecretResourceVersion ||
		pod.Annotations[credentialProjectionDigestAnnotation] != credentials.ContentSHA256 {
		t.Fatal("runtime Pod is not bound to the exact credential projection")
	}
	if projection.Annotations[executionBindingAnnotation] != input.ExecutionBindingDigest ||
		projection.Annotations[mcpBindingAnnotation] != input.MCPBindingDigest ||
		projection.Annotations[organizationHashAnnotation] != shortHash(input.OrganizationRef) ||
		projection.Annotations[projectHashAnnotation] != shortHash(input.ProjectRef) ||
		projection.Annotations[sessionHashAnnotation] != shortHash(input.SessionRef) ||
		projection.Annotations[turnHashAnnotation] != shortHash(input.TurnRef) ||
		projection.Annotations[attemptAnnotation] != "1" {
		t.Fatalf("runtime projection execution binding = %#v", projection.Annotations)
	}
	workspaceVolume := podVolumeByName(t, pod, "workspace")
	if workspaceVolume.EmptyDir == nil || workspaceVolume.EmptyDir.SizeLimit == nil || workspaceVolume.EmptyDir.SizeLimit.Value() != runtimecontract.RuntimeWorkspaceWritableBytes {
		t.Fatalf("workspace quota volume = %#v", workspaceVolume)
	}
	runtimeInputVolume := podVolumeByName(t, pod, "runtime-input")
	if runtimeInputVolume.ConfigMap == nil || runtimeInputVolume.ConfigMap.Name != projection.Name || runtimeInputVolume.ConfigMap.DefaultMode == nil || *runtimeInputVolume.ConfigMap.DefaultMode != 0o440 {
		t.Fatalf("runtime ConfigMap projection volume = %#v", runtimeInputVolume)
	}
	runtimeTicketVolume := podVolumeByName(t, pod, "runtime-ticket")
	if runtimeTicketVolume.Secret == nil || runtimeTicketVolume.Secret.SecretName != secret.Name || runtimeTicketVolume.Secret.DefaultMode == nil || *runtimeTicketVolume.Secret.DefaultMode != 0o440 {
		t.Fatalf("runtime ticket volume = %#v", runtimeTicketVolume)
	}
	if pod.Spec.SecurityContext == nil || pod.Spec.SecurityContext.FSGroup == nil || *pod.Spec.SecurityContext.FSGroup != 29000 ||
		pod.Spec.SecurityContext.FSGroupChangePolicy == nil || *pod.Spec.SecurityContext.FSGroupChangePolicy != corev1.FSGroupChangeOnRootMismatch {
		t.Fatalf("runtime Pod fsGroup matrix = %#v", pod.Spec.SecurityContext)
	}
	for _, volume := range []string{"vfs-input", "vfs-knowledge"} {
		if !mountReadOnly(provider, volume, true) || !mountReadOnly(role, volume, true) {
			t.Fatalf("runtime VFS mount mode %q is invalid", volume)
		}
		if !mountReadOnly(init, volume, false) {
			t.Fatalf("workspace materializer cannot populate %q", volume)
		}
	}
	for _, volume := range []string{"runtime-input", "runtime-ticket", "callback-ca", "callback-client"} {
		if !mountReadOnly(init, volume, true) {
			t.Fatalf("workspace materializer is missing read-only authority mount %q", volume)
		}
	}
	for _, container := range []corev1.Container{init, role, relay} {
		security := container.SecurityContext
		if security == nil || security.RunAsNonRoot == nil || !*security.RunAsNonRoot ||
			security.ReadOnlyRootFilesystem == nil || !*security.ReadOnlyRootFilesystem ||
			security.AllowPrivilegeEscalation == nil || *security.AllowPrivilegeEscalation ||
			security.SeccompProfile == nil || security.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
			t.Fatalf("restricted container security matrix is invalid: name=%q context=%#v", container.Name, security)
		}
	}
	if !hasMountAt(provider, "workspace", "/workspace") || !hasMountAt(provider, "session", "/workspace/.kodex/state") || hasMountAt(provider, "session", "/workspace") {
		t.Fatalf("provider writable roots are not bounded: %#v", provider.VolumeMounts)
	}
	sessionVolumeName, nameErr := runtimecontract.SessionPVCName(input.SessionRef)
	if nameErr != nil {
		t.Fatalf("derive session volume name: %v", nameErr)
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("kodex-runtime").Get(context.Background(), sessionVolumeName, metav1.GetOptions{}); err != nil {
		t.Fatalf("session volume was not materialized: %v", err)
	}
	pvc, err := client.CoreV1().PersistentVolumeClaims("kodex-runtime").Get(context.Background(), sessionVolumeName, metav1.GetOptions{})
	if err != nil || pvc.Spec.StorageClassName != nil {
		t.Fatalf("session volume must use the cluster default StorageClass: storage_class=%v err=%v", pvc.Spec.StorageClassName, err)
	}
}

func hasMountAt(container corev1.Container, volumeName, path string) bool {
	for _, mount := range container.VolumeMounts {
		if mount.Name == volumeName && mount.MountPath == path {
			return true
		}
	}
	return false
}

func mountReadOnly(container corev1.Container, volumeName string, expected bool) bool {
	for _, mount := range container.VolumeMounts {
		if mount.Name == volumeName {
			return mount.ReadOnly == expected
		}
	}
	return false
}

func podVolumeByName(t *testing.T, pod *corev1.Pod, name string) corev1.Volume {
	t.Helper()
	for _, volume := range pod.Spec.Volumes {
		if volume.Name == name {
			return volume
		}
	}
	t.Fatalf("Pod volume %q is missing", name)
	return corev1.Volume{}
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

func TestManagerRejectsSharedControlAndRuntimeNamespace(t *testing.T) {
	t.Parallel()
	config := testManagerConfig()
	config.RuntimeNamespace = config.ControlNamespace
	if _, err := New(fake.NewSimpleClientset(), config); err == nil {
		t.Fatal("shared control and runtime namespace was accepted")
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
	if err := manager.EnsureTurn(context.Background(), input, binding, testCredentialProjection(input)); err == nil {
		t.Fatal("EnsureTurn() accepted a provider Secret outside the immutable credential revision")
	}
}

func TestMaterializeProviderCredentialRefreshCreatesImmutableRevisionIdempotently(t *testing.T) {
	client, manager, input, payload := managedOAuthRefreshFixture(t)
	client.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
		created := action.(k8stesting.CreateAction).GetObject().(*corev1.Secret)
		if strings.HasPrefix(created.Name, "runtime-provider-refresh-") {
			created.UID = "30000000-0000-4000-8000-000000000001"
			created.ResourceVersion = "17"
		}
		return false, nil, nil
	})

	first, err := manager.MaterializeProviderCredentialRefresh(context.Background(), input, payload)
	if err != nil {
		t.Fatalf("MaterializeProviderCredentialRefresh() error = %v", err)
	}
	second, err := manager.MaterializeProviderCredentialRefresh(context.Background(), input, payload)
	if err != nil {
		t.Fatalf("MaterializeProviderCredentialRefresh() repeat error = %v", err)
	}
	if first != second || first.UID == "" || first.ResourceVersion == "" || first.ContentSHA256 == input.ProviderCredentialSHA256 {
		t.Fatalf("materialized binding is not stable: first=%#v second=%#v", first, second)
	}
	secret, err := client.CoreV1().Secrets("kodex-runtime").Get(context.Background(), first.Name, metav1.GetOptions{})
	if err != nil || secret.Immutable == nil || !*secret.Immutable || len(secret.Data) != 2 ||
		!bytes.Equal(secret.Data["auth.json"], payload.Authentication) ||
		string(secret.Data["auth.sha256"]) != first.ContentSHA256 {
		t.Fatalf("materialized Secret readback is invalid: name=%q uid=%q resource_version=%q err=%v",
			first.Name, first.UID, first.ResourceVersion, err)
	}
	listed, err := client.CoreV1().Secrets("kodex-runtime").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List(Secrets) error = %v", err)
	}
	count := 0
	for _, item := range listed.Items {
		if strings.HasPrefix(item.Name, "runtime-provider-refresh-") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("refresh Secret count = %d, want 1", count)
	}
}

func TestMaterializeProviderCredentialRefreshRejectsInvalidLineage(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fake.Clientset, runtimecontract.RunnerInput, *runtimecontract.RunnerProviderCredentialRefreshRequest)
	}{
		{name: "different account", mutate: func(_ *fake.Clientset, _ runtimecontract.RunnerInput, request *runtimecontract.RunnerProviderCredentialRefreshRequest) {
			request.Authentication = managedOAuthAuthentication("account-other", "access-new", "refresh-new")
		}},
		{name: "API key", mutate: func(_ *fake.Clientset, _ runtimecontract.RunnerInput, request *runtimecontract.RunnerProviderCredentialRefreshRequest) {
			request.Authentication = []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"test-only"}`)
		}},
		{name: "duplicate security field", mutate: func(_ *fake.Clientset, _ runtimecontract.RunnerInput, request *runtimecontract.RunnerProviderCredentialRefreshRequest) {
			request.Authentication = []byte(`{"auth_mode":"apikey","auth_mode":"chatgpt","tokens":{"account_id":"account-1","access_token":"access-new","refresh_token":"refresh-new"}}`)
		}},
		{name: "unchanged snapshot", mutate: func(_ *fake.Clientset, _ runtimecontract.RunnerInput, request *runtimecontract.RunnerProviderCredentialRefreshRequest) {
			request.Authentication = managedOAuthAuthentication("account-1", "access-old", "refresh-old")
		}},
		{name: "stale previous revision", mutate: func(_ *fake.Clientset, _ runtimecontract.RunnerInput, request *runtimecontract.RunnerProviderCredentialRefreshRequest) {
			request.PreviousCredentialRevisionRef = "pcr_stale123"
		}},
		{name: "missing pinned Secret", mutate: func(client *fake.Clientset, _ runtimecontract.RunnerInput, _ *runtimecontract.RunnerProviderCredentialRefreshRequest) {
			_ = client.CoreV1().Secrets("kodex-runtime").Delete(context.Background(), "runtime-provider-openai-default-r1", metav1.DeleteOptions{})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, manager, input, payload := managedOAuthRefreshFixture(t)
			test.mutate(client, input, &payload)
			if _, err := manager.MaterializeProviderCredentialRefresh(context.Background(), input, payload); !errors.Is(err, ErrProviderCredentialRefreshRejected) {
				t.Fatalf("MaterializeProviderCredentialRefresh() error = %v, want rejected", err)
			}
		})
	}
}

func managedOAuthRefreshFixture(t *testing.T) (*fake.Clientset, *Manager, runtimecontract.RunnerInput, runtimecontract.RunnerProviderCredentialRefreshRequest) {
	t.Helper()
	client := fake.NewSimpleClientset()
	manager, err := New(client, testManagerConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	oldAuthentication := managedOAuthAuthentication("account-1", "access-old", "refresh-old")
	oldDigest := sha256.Sum256(oldAuthentication)
	oldDigestHex := hex.EncodeToString(oldDigest[:])
	immutable := true
	_, err = client.CoreV1().Secrets("kodex-runtime").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "runtime-provider-openai-default-r1", Namespace: "kodex-runtime",
			UID: "10000000-0000-4000-8000-000000000001", ResourceVersion: "1"},
		Immutable: &immutable,
		Data:      map[string][]byte{"auth.json": oldAuthentication, "auth.sha256": []byte(oldDigestHex)},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create managed OAuth fixture: %v", err)
	}
	execution := testExecution(false)
	execution.Revision.ProviderCredential.ContentSha256 = oldDigestHex
	sealTestTurnExecution(execution)
	input, binding, err := manager.BuildTurnInput(execution)
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	if err := manager.EnsureTurn(context.Background(), input, binding, testCredentialProjection(input)); err != nil {
		t.Fatalf("EnsureTurn() error = %v", err)
	}
	return client, manager, input, runtimecontract.RunnerProviderCredentialRefreshRequest{
		RuntimeRevisionDigest:         input.RuntimeRevisionDigest,
		PreviousCredentialRevisionRef: input.ProviderCredentialRef,
		PreviousContentSHA256:         input.ProviderCredentialSHA256,
		Authentication:                managedOAuthAuthentication("account-1", "access-new", "refresh-new"),
	}
}

func managedOAuthAuthentication(accountID, accessToken, refreshToken string) []byte {
	return []byte(fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"account_id":%q,"access_token":%q,"refresh_token":%q}}`, accountID, accessToken, refreshToken))
}

func TestEnsureTurnMountsExactBrokerEnvironmentSecretOutsideTicket(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	immutable := true
	secretValue := []byte("runtime-environment-secret-fixture")
	digest := sha256.Sum256(secretValue)
	digestHex := hex.EncodeToString(digest[:])
	source := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "runtime-agent-environment-r1", Namespace: "kodex-runtime",
			UID: "20000000-0000-4000-8000-000000000001", ResourceVersion: "7",
		},
		Immutable: &immutable,
		Data:      map[string][]byte{"token": secretValue},
	}
	if _, err := client.CoreV1().Secrets("kodex-runtime").Create(context.Background(), source, metav1.CreateOptions{}); err != nil {
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
	policy, policyErr := runtimeEnvironmentPolicyFromProto(execution.Revision.GetEnvironmentPolicy())
	if policyErr != nil {
		t.Fatalf("runtimeEnvironmentPolicyFromProto() error = %v", policyErr)
	}
	environmentDigest, err := runtimecontract.RuntimeEnvironmentDigest(values, projections, image, tools, policy)
	if err != nil {
		t.Fatalf("RuntimeEnvironmentDigest() error = %v", err)
	}
	execution.Revision.RuntimeEnvironmentDigest = environmentDigest
	sealTestTurnExecution(execution)
	input, binding, err := manager.BuildTurnInput(execution)
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	credentials := testCredentialProjection(input)
	if err := manager.EnsureTurn(context.Background(), input, binding, credentials); err != nil {
		t.Fatalf("EnsureTurn() error = %v", err)
	}

	ticket, err := client.CoreV1().Secrets("kodex-runtime").Get(context.Background(), ticketName(input.LeaseRef), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(runtime ticket Secret) error = %v", err)
	}
	if bytes.Contains(ticket.Data[inputKey], secretValue) {
		t.Fatal("runtime.json contains a Secret value")
	}
	if len(ticket.Data) != 2 {
		t.Fatalf("execution ticket contains copied Secret values: %#v", ticket.Data)
	}
	bound, err := runtimecontract.DecodeRunnerInput(ticket.Data[inputKey])
	if err != nil || len(bound.SecretProjections) != 1 || bound.SecretProjections[0] != projections[0] {
		t.Fatalf("runtime.json does not preserve the exact Secret descriptor: input=%#v err=%v", bound.SecretProjections, err)
	}

	pod, err := client.CoreV1().Pods("kodex-runtime").Get(context.Background(), turnPodName(input.LeaseRef), metav1.GetOptions{})
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
	if projected == nil || projected.Name != credentials.SecretName || projected.Key != "SERVICE_TOKEN" ||
		projected.Optional == nil || *projected.Optional {
		t.Fatalf("provider runtime Secret projection is not exact: %#v", projected)
	}
}

func TestEnsureTurnRejectsCredentialProjectionKeySetMismatch(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	immutable := true
	secretValue := []byte("runtime-environment-secret-fixture")
	digest := sha256.Sum256(secretValue)
	if _, err := client.CoreV1().Secrets("kodex-runtime").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "runtime-agent-environment-r1", Namespace: "kodex-runtime",
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
	policy, policyErr := runtimeEnvironmentPolicyFromProto(execution.Revision.GetEnvironmentPolicy())
	if policyErr != nil {
		t.Fatalf("runtimeEnvironmentPolicyFromProto() error = %v", policyErr)
	}
	environmentDigest, err := runtimecontract.RuntimeEnvironmentDigest(nil, projections, image, tools, policy)
	if err != nil {
		t.Fatalf("RuntimeEnvironmentDigest() error = %v", err)
	}
	execution.Revision.RuntimeEnvironmentDigest = environmentDigest
	sealTestTurnExecution(execution)
	input, binding, err := manager.BuildTurnInput(execution)
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	credentials := testCredentialProjection(input)
	credentials.RuntimeSecretKeys["SERVICE_TOKEN"] = "OTHER_TOKEN"
	if err := manager.EnsureTurn(context.Background(), input, binding, credentials); err == nil {
		t.Fatal("EnsureTurn() accepted a credential projection with another key set")
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

func TestBuildTurnInputCarriesExactAttachmentProvenance(t *testing.T) {
	t.Parallel()
	manager := newTestManager(t, fake.NewSimpleClientset())
	execution := testExecution(false)
	input, _, err := manager.BuildTurnInput(execution)
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	if len(input.AttachmentSets) != 1 || len(input.InputArtifacts) != 1 {
		t.Fatalf("runner attachment catalog sizes = %d sets, %d artifacts", len(input.AttachmentSets), len(input.InputArtifacts))
	}
	set, artifact := input.AttachmentSets[0], input.InputArtifacts[0]
	if set.Ref != input.AttachmentSetRef || set.ManifestDigest != input.AttachmentSetManifestDigest ||
		set.Purpose != input.AttachmentContext || set.Scope != runtimecontract.AttachmentScopeInput ||
		set.Provenance != "CURRENT_TURN" || set.TurnRef != input.TurnRef {
		t.Fatalf("runner attachment set lost exact provenance: %#v", set)
	}
	if artifact.AttachmentSetRef != set.Ref || artifact.AttachmentPurpose != set.Purpose ||
		artifact.Provenance != set.Provenance || artifact.Revision != 1 || artifact.Version != 1 ||
		artifact.Source != "CONTROL_CENTER" || artifact.Scope != runtimecontract.AttachmentScopeInput {
		t.Fatalf("runner artifact lost exact provenance: %#v", artifact)
	}
	workspace, err := runtimecontract.BuildWorkspaceAttachmentManifest(input.AttachmentSets, input.InputArtifacts)
	if err != nil {
		t.Fatalf("BuildWorkspaceAttachmentManifest() error = %v", err)
	}
	if len(workspace.Manifest.Files) != 1 {
		t.Fatalf("workspace manifest files = %d, want 1", len(workspace.Manifest.Files))
	}
	file := workspace.Manifest.Files[0]
	if file.ArtifactRef != artifact.Ref || file.Revision != artifact.Revision || file.Version != artifact.Version ||
		file.Source != artifact.Source || file.Scope != artifact.Scope || file.Purpose != artifact.AttachmentPurpose ||
		file.Path != "/workspace/input/aset_abcdefgh/files/0001-brief.txt" {
		t.Fatalf("workspace manifest lost exact artifact identity: %#v", file)
	}
}

func TestBuildTurnInputCarriesSessionHistoryTurnLineage(t *testing.T) {
	t.Parallel()
	execution := testExecution(false)
	historySetRef := "aset_history1"
	historyPurpose := "SESSION_TURN"
	historyArtifact := &controlplanev1.RuntimeInputArtifact{
		Artifact: &controlplanev1.Artifact{Ref: "artifact_history1", FileName: "prior.txt", MediaType: "text/plain", SizeBytes: 24, Digest: testArtifactDigest, Revision: 2, Version: 3, Source: controlplanev1.ArtifactSource_ARTIFACT_SOURCE_INTERACTION_ATTACHMENT},
		Scope:    runtimecontract.AttachmentScopeSession, Position: 1, AttachmentSetRef: historySetRef,
		AttachmentPurpose: historyPurpose, Provenance: "SESSION_HISTORY",
	}
	historyManifest, err := runtimecontract.BuildAttachmentManifest(historySetRef, historyPurpose, []runtimecontract.RunnerInputArtifact{{
		Ref: historyArtifact.Artifact.Ref, FileName: historyArtifact.Artifact.FileName, MediaType: historyArtifact.Artifact.MediaType,
		Digest: historyArtifact.Artifact.Digest, SizeBytes: historyArtifact.Artifact.SizeBytes, Revision: int64(historyArtifact.Artifact.Revision),
		Version: historyArtifact.Artifact.Version, Scope: runtimecontract.AttachmentScopeInput,
		Position: historyArtifact.Position, Source: "INTERACTION_ATTACHMENT",
	}})
	if err != nil {
		t.Fatalf("BuildAttachmentManifest(history) error = %v", err)
	}
	execution.Revision.AttachmentSets = append(execution.Revision.AttachmentSets, &controlplanev1.RuntimeAttachmentSet{
		Ref: historySetRef, ManifestDigest: historyManifest.Digest, Purpose: historyPurpose,
		Scope: runtimecontract.AttachmentScopeSession, Provenance: "SESSION_HISTORY", TurnRef: "turn_history1",
	})
	execution.Revision.InputArtifacts = append(execution.Revision.InputArtifacts, historyArtifact)
	sealTestTurnExecution(execution)

	manager := newTestManager(t, fake.NewSimpleClientset())
	input, _, err := manager.BuildTurnInput(execution)
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	if len(input.AttachmentSets) != 2 || len(input.InputArtifacts) != 2 {
		t.Fatalf("runner attachment catalog sizes = %d sets, %d artifacts", len(input.AttachmentSets), len(input.InputArtifacts))
	}
	historySet, history := input.AttachmentSets[1], input.InputArtifacts[1]
	if historySet.Ref != historySetRef || historySet.ManifestDigest != historyManifest.Digest ||
		historySet.Scope != runtimecontract.AttachmentScopeSession || historySet.Provenance != "SESSION_HISTORY" ||
		historySet.TurnRef != "turn_history1" || history.AttachmentSetRef != historySet.Ref ||
		history.Scope != historySet.Scope || history.Provenance != historySet.Provenance {
		t.Fatalf("runner session history lost exact turn lineage: set=%#v artifact=%#v", historySet, history)
	}
	workspace, err := runtimecontract.BuildWorkspaceAttachmentManifest(input.AttachmentSets, input.InputArtifacts)
	if err != nil {
		t.Fatalf("BuildWorkspaceAttachmentManifest() error = %v", err)
	}
	if got := workspace.Manifest.Files[1].Path; got != "/workspace/input/aset_history1/files/0001-prior.txt" {
		t.Fatalf("history workspace path = %q", got)
	}
}

func TestBuildTurnInputRejectsAttachmentProvenanceDrift(t *testing.T) {
	t.Parallel()
	tests := map[string]func(*controlplanev1.RuntimeRevisionSnapshot){
		"manifest digest": func(revision *controlplanev1.RuntimeRevisionSnapshot) {
			revision.AttachmentSets[0].ManifestDigest = strings.Repeat("f", 64)
			revision.AttachmentSetManifestDigest = strings.Repeat("f", 64)
		},
		"artifact revision": func(revision *controlplanev1.RuntimeRevisionSnapshot) {
			revision.InputArtifacts[0].Artifact.Revision++
		},
		"artifact version": func(revision *controlplanev1.RuntimeRevisionSnapshot) {
			revision.InputArtifacts[0].Artifact.Version++
		},
		"artifact source": func(revision *controlplanev1.RuntimeRevisionSnapshot) {
			revision.InputArtifacts[0].Artifact.Source = controlplanev1.ArtifactSource_ARTIFACT_SOURCE_AGENT_RESULT
		},
		"artifact scope": func(revision *controlplanev1.RuntimeRevisionSnapshot) {
			revision.InputArtifacts[0].Scope = runtimecontract.AttachmentScopeSession
		},
		"artifact path": func(revision *controlplanev1.RuntimeRevisionSnapshot) {
			revision.InputArtifacts[0].Artifact.FileName = "renamed.txt"
		},
		"attachment set": func(revision *controlplanev1.RuntimeRevisionSnapshot) {
			revision.InputArtifacts[0].AttachmentSetRef = "aset_ijklmnop"
		},
		"turn lineage": func(revision *controlplanev1.RuntimeRevisionSnapshot) {
			revision.AttachmentSets[0].TurnRef = ""
		},
		"wrong current turn lineage": func(revision *controlplanev1.RuntimeRevisionSnapshot) {
			revision.AttachmentSets[0].TurnRef = "turn_ijklmnop"
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			execution := testExecution(false)
			mutate(execution.Revision)
			manager := newTestManager(t, fake.NewSimpleClientset())
			if _, _, err := manager.BuildTurnInput(execution); err == nil {
				t.Fatal("BuildTurnInput() accepted attachment provenance drift")
			}
		})
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
				execution.Revision.AttachmentSets = nil
				execution.Revision.InputArtifacts = nil
			}
			sealTestTurnExecution(execution)
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
	warmPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "system-assistant-warm", Namespace: "kodex-runtime", Annotations: map[string]string{revisionAnnotation: strings.Repeat("c", 64)}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
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
	if err := manager.EnsureTurn(context.Background(), input, binding, testCredentialProjection(input)); err != nil {
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
		wantDiag   string
	}{
		{
			name: "role terminated while provider is running",
			statuses: []corev1.ContainerStatus{
				{Name: "role-runtime", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}},
				{Name: "provider-runtime", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			},
			want:     "FAILED",
			wantDiag: "ROLE_RUNTIME_EXITED_ZERO",
		},
		{
			name: "provider terminated while role is running",
			statuses: []corev1.ContainerStatus{
				{Name: "role-runtime", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
				{Name: "provider-runtime", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}},
			},
			want:     "FAILED",
			wantDiag: "PROVIDER_RUNTIME_EXITED",
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
			credentials := testCredentialProjection(input)
			pod := manager.runtimePod(input, binding, &credentials, ticketName(input.LeaseRef), turnPodName(input.LeaseRef), "turn")
			pod.Status = corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: test.statuses, Conditions: test.conditions}
			if _, err := client.CoreV1().Pods("kodex-runtime").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
				t.Fatalf("Create(cold runtime Pod) error = %v", err)
			}
			observation, err := manager.ObserveTurnPod(context.Background(), input, false)
			if err != nil {
				t.Fatalf("ObserveTurnPod() error = %v", err)
			}
			if observation.State != test.want || observation.DiagnosticCode != test.wantDiag {
				t.Fatalf("ObserveTurnPod() = %#v, want state=%q diagnostic=%q", observation, test.want, test.wantDiag)
			}
		})
	}
}

func TestWarmCompatibilityIgnoresTurnIdentityButRejectsRuntimeDrift(t *testing.T) {
	t.Parallel()
	manager := newTestManager(t, fake.NewSimpleClientset())
	warm, _, err := manager.BuildWarmInput(testWarmRevision())
	if err != nil {
		t.Fatalf("BuildWarmInput() error = %v", err)
	}
	execution := testExecution(true)
	execution.Run.ProjectRef = ""
	sealTestTurnExecution(execution)
	turn, _, err := manager.BuildTurnInput(execution)
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	turn.RuntimeRevisionRef = "revision_turn1234"
	turn.RuntimeRevisionDigest = strings.Repeat("e", 64)
	turn.SessionRef = warm.SessionRef
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
	input, binding, err := manager.BuildWarmInput(testWarmRevision())
	if err != nil {
		t.Fatalf("BuildWarmInput() error = %v", err)
	}
	terminal := manager.runtimePod(input, binding, nil, manager.warmTicketName(input.RuntimeRevisionRef, input.RuntimeRevisionDigest), "system-assistant-warm", "warm")
	terminal.Status.Phase = corev1.PodFailed
	if _, err := client.CoreV1().Pods("kodex-runtime").Create(context.Background(), terminal, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Create(terminal warm Pod) error = %v", err)
	}
	ready, err := manager.EnsureWarm(context.Background(), input, binding)
	if err != nil {
		t.Fatalf("EnsureWarm() error = %v", err)
	}
	if ready {
		t.Fatal("deleted warm Pod cannot be ready")
	}
	if _, err := client.CoreV1().Pods("kodex-runtime").Get(context.Background(), "system-assistant-warm", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("terminal warm Pod was not deleted before replacement: %v", err)
	}
	ready, err = manager.EnsureWarm(context.Background(), input, binding)
	if err != nil {
		t.Fatalf("second EnsureWarm() error = %v", err)
	}
	if ready {
		t.Fatal("new warm Pod cannot be ready before Kubernetes observation")
	}
	pod, err := client.CoreV1().Pods("kodex-runtime").Get(context.Background(), "system-assistant-warm", metav1.GetOptions{})
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
	input, binding, err := manager.BuildWarmInput(testWarmRevision())
	if err != nil {
		t.Fatalf("BuildWarmInput() error = %v", err)
	}
	terminal := manager.runtimePod(input, binding, nil, manager.warmTicketName(input.RuntimeRevisionRef, input.RuntimeRevisionDigest), "system-assistant-warm", "warm")
	terminal.Status.Phase = corev1.PodRunning
	terminal.Status.ContainerStatuses = []corev1.ContainerStatus{
		{Name: "role-runtime", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}},
		{Name: "provider-runtime", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
	}
	if _, err := client.CoreV1().Pods("kodex-runtime").Create(context.Background(), terminal, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Create(terminal warm Pod) error = %v", err)
	}
	ready, err := manager.EnsureWarm(context.Background(), input, binding)
	if err != nil {
		t.Fatalf("EnsureWarm() error = %v", err)
	}
	if ready {
		t.Fatal("deleted warm Pod cannot be ready")
	}
	if _, err := client.CoreV1().Pods("kodex-runtime").Get(context.Background(), "system-assistant-warm", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("terminated warm Pod was not deleted before replacement: %v", err)
	}
	ready, err = manager.EnsureWarm(context.Background(), input, binding)
	if err != nil {
		t.Fatalf("second EnsureWarm() error = %v", err)
	}
	if ready {
		t.Fatal("recreated warm Pod cannot be ready before Kubernetes observation")
	}
	pod, err := client.CoreV1().Pods("kodex-runtime").Get(context.Background(), "system-assistant-warm", metav1.GetOptions{})
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
	input, binding, err := manager.BuildWarmInput(testWarmRevision())
	if err != nil {
		t.Fatalf("BuildWarmInput() error = %v", err)
	}
	secretName := manager.warmTicketName(input.RuntimeRevisionRef, input.RuntimeRevisionDigest)
	oldToken := strings.Repeat("a", 64)
	if err := manager.ensureTicket(context.Background(), secretName, "system-assistant-warm", "warm", input, oldToken, nil, &binding); err != nil {
		t.Fatalf("ensureTicket() error = %v", err)
	}
	staleInput := input
	staleInput.RuntimeRevisionRef = "runtime_revision_stale"
	staleInput.RuntimeRevisionDigest = strings.Repeat("d", 64)
	if err := manager.ensureTicket(context.Background(), manager.warmTicketName(staleInput.RuntimeRevisionRef, staleInput.RuntimeRevisionDigest), "system-assistant-warm", "warm", staleInput, strings.Repeat("b", 64), nil, &binding); err != nil {
		t.Fatalf("ensure stale ticket error = %v", err)
	}
	terminal := manager.runtimePod(input, binding, nil, secretName, "system-assistant-warm", "warm")
	terminal.Status.Phase = corev1.PodFailed
	if _, err := client.CoreV1().Pods("kodex-runtime").Create(context.Background(), terminal, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Create(terminal warm Pod) error = %v", err)
	}
	if _, err := manager.EnsureWarm(context.Background(), input, binding); err != nil {
		t.Fatalf("EnsureWarm() error = %v", err)
	}
	if _, err := client.CoreV1().Secrets("kodex-runtime").Get(context.Background(), secretName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("terminal warm ticket was not deleted before replacement: %v", err)
	}
	if _, err := manager.EnsureWarm(context.Background(), input, binding); err != nil {
		t.Fatalf("second EnsureWarm() error = %v", err)
	}
	current, err := client.CoreV1().Secrets("kodex-runtime").Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(current warm ticket) error = %v", err)
	}
	if bytes.Equal(current.Data[ticketKey], []byte(oldToken)) {
		t.Fatal("terminal warm Pod reused its execution ticket")
	}
	items, err := client.CoreV1().Secrets("kodex-runtime").List(context.Background(), metav1.ListOptions{
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
	input, binding, err := manager.BuildWarmInput(testWarmRevision())
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
	_, err = client.CoreV1().Secrets("kodex-runtime").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "kodex-runtime",
			Annotations: map[string]string{revisionAnnotation: input.RuntimeRevisionDigest, controllerAnnotation: "previous-controller"}},
		Immutable: &immutable, Data: map[string][]byte{inputKey: raw, ticketKey: []byte(strings.Repeat("a", 64))},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create(stale warm ticket) error = %v", err)
	}
	if _, err := manager.EnsureWarm(context.Background(), input, binding); err != nil {
		t.Fatalf("EnsureWarm() error = %v", err)
	}
	current, err := client.CoreV1().Secrets("kodex-runtime").Get(context.Background(), manager.warmTicketName(input.RuntimeRevisionRef, input.RuntimeRevisionDigest), metav1.GetOptions{})
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
	input, binding, err := manager.BuildWarmInput(testWarmRevision())
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
	_, err = client.CoreV1().Secrets("kodex-runtime").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "kodex-runtime",
			Annotations: map[string]string{revisionAnnotation: input.RuntimeRevisionDigest, controllerAnnotation: manager.config.ControllerPodUID}},
		Immutable: &immutable, Data: map[string][]byte{inputKey: raw, ticketKey: []byte(strings.Repeat("a", 64))},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create(stale warm ticket) error = %v", err)
	}
	if _, err := manager.EnsureWarm(context.Background(), input, binding); err != nil {
		t.Fatalf("EnsureWarm() error = %v", err)
	}
	current, err := client.CoreV1().Secrets("kodex-runtime").Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(current warm ticket) error = %v", err)
	}
	bound, err := runtimecontract.DecodeRunnerInput(current.Data[inputKey])
	if err != nil || bound.CallbackURL != input.CallbackURL {
		t.Fatalf("warm ticket retained stale callback address: input=%#v err=%v", bound, err)
	}
}

func TestEnsureWarmReplacesTicketWithStaleProviderSecretBinding(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, binding, err := manager.BuildWarmInput(testWarmRevision())
	if err != nil {
		t.Fatalf("BuildWarmInput() error = %v", err)
	}
	staleBinding := binding
	staleBinding.ResourceVersion = "previous-resource-version"
	secretName := manager.warmTicketName(input.RuntimeRevisionRef, input.RuntimeRevisionDigest)
	if err := manager.ensureTicket(context.Background(), secretName, "system-assistant-warm", "warm", input,
		strings.Repeat("a", 64), nil, &staleBinding); err != nil {
		t.Fatalf("ensure stale warm ticket error = %v", err)
	}
	if _, err := manager.EnsureWarm(context.Background(), input, binding); err != nil {
		t.Fatalf("EnsureWarm() error = %v", err)
	}
	current, err := client.CoreV1().Secrets("kodex-runtime").Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(current warm ticket) error = %v", err)
	}
	if !providerTicketBindingMatches(current, input, &binding) {
		t.Fatalf("warm ticket retained stale provider binding: annotations=%#v", current.Annotations)
	}
}

func TestEnsureTurnRejectsExistingPodFromAnotherRevision(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, binding, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	credentials := testCredentialProjection(input)
	conflict := manager.runtimePod(input, binding, &credentials, ticketName(input.LeaseRef), turnPodName(input.LeaseRef), "turn")
	conflict.Annotations[revisionAnnotation] = strings.Repeat("c", 64)
	if _, err := client.CoreV1().Pods("kodex-runtime").Create(context.Background(), conflict, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Create(conflict Pod) error = %v", err)
	}
	if err := manager.EnsureTurn(context.Background(), input, binding, testCredentialProjection(input)); err == nil {
		t.Fatal("EnsureTurn() accepted a Pod from another immutable revision")
	}
}

func TestBuildTurnRejectsRuntimeRevisionDigestMismatch(t *testing.T) {
	t.Parallel()
	tests := map[string]func(*controlplanev1.RuntimeRevisionSnapshot){
		"missing digest": func(revision *controlplanev1.RuntimeRevisionSnapshot) { revision.RevisionDigest = "" },
		"revision":       func(revision *controlplanev1.RuntimeRevisionSnapshot) { revision.Ref = "rrev_ijklmnop" },
		"image":          func(revision *controlplanev1.RuntimeRevisionSnapshot) { revision.RoleImageRecipeGeneration++ },
		"environment": func(revision *controlplanev1.RuntimeRevisionSnapshot) {
			revision.EnvironmentTools[0].UsageHint = "changed"
		},
		"provider": func(revision *controlplanev1.RuntimeRevisionSnapshot) {
			revision.ProviderCredential.CredentialRevision++
		},
		"role": func(revision *controlplanev1.RuntimeRevisionSnapshot) { revision.RoleRuntimeContractRevision++ },
		"prompt": func(revision *controlplanev1.RuntimeRevisionSnapshot) {
			revision.PromptTemplateDigest = strings.Repeat("e", 64)
		},
		"workspace": func(revision *controlplanev1.RuntimeRevisionSnapshot) { revision.WorkspacePolicy.MaximumFileCount-- },
		"MCP grant": func(revision *controlplanev1.RuntimeRevisionSnapshot) {
			revision.IntegrationGrants = append(revision.IntegrationGrants, &controlplanev1.IntegrationGrant{
				Ref: "grant_abcdefgh", ConnectionRef: "conn_abcdefgh", DefinitionKey: "calendar",
				ConnectionName: "Calendar", CapabilityKey: "calendar.read", CapabilityName: "Read calendar", Enabled: true,
			})
		},
		"STT": func(revision *controlplanev1.RuntimeRevisionSnapshot) {
			revision.Capabilities = append(revision.Capabilities, &controlplanev1.PlatformCapability{Key: "platform.stt.use"})
			revision.SystemSttConfigurationRef = "sttcfg_abcdefgh"
			revision.SystemSttConfigurationRevisionRef = "sttrev_abcdefgh"
			revision.SystemSttConfigurationVersion = 1
			revision.SystemSttConfigurationDigest = strings.Repeat("9", 64)
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			execution := testExecution(false)
			mutate(execution.Revision)
			manager := newTestManager(t, fake.NewSimpleClientset())
			if _, _, err := manager.BuildTurnInput(execution); err == nil {
				t.Fatalf("BuildTurnInput() error = %v", err)
			}
		})
	}
}

func TestEnsureTurnRejectsStaleImmutableProjection(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, binding, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	data, err := runtimeProjectionData(input)
	if err != nil {
		t.Fatalf("runtimeProjectionData() error = %v", err)
	}
	data[mcpManifestKey] = `{"binding_digest":"stale"}`
	immutable := true
	projection := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: runtimeProjectionName(input), Namespace: "kodex-runtime",
		Labels: map[string]string{managedLabel: "true", modeLabel: "turn"}, Annotations: runtimeProjectionAnnotations(input, runtimecontract.RuntimeTurnPodName(input.LeaseRef))},
		Immutable: &immutable, Data: data}
	if _, err := client.CoreV1().ConfigMaps("kodex-runtime").Create(context.Background(), projection, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Create(stale projection) error = %v", err)
	}
	if err := manager.EnsureTurn(context.Background(), input, binding, testCredentialProjection(input)); err == nil || !strings.Contains(err.Error(), "conflicts with immutable revision") {
		t.Fatalf("EnsureTurn(stale projection) error = %v", err)
	}
	if _, err := client.CoreV1().Pods("kodex-runtime").Get(context.Background(), runtimecontract.RuntimeTurnPodName(input.LeaseRef), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("Pod was created from a stale projection: %v", err)
	}
}

func TestKubernetesMaterializationPayloadLimitFailsClosed(t *testing.T) {
	t.Parallel()
	oversized := strings.Repeat("x", maximumKubernetesProjectionBytes)
	if stringDataSize(map[string]string{"runtime.json": oversized}) <= maximumKubernetesProjectionBytes {
		t.Fatal("oversized ConfigMap projection was accepted")
	}
	if byteDataSize(map[string][]byte{"runtime.json": []byte(oversized)}) <= maximumKubernetesProjectionBytes {
		t.Fatal("oversized Secret projection was accepted")
	}
}

func TestSessionPVCRejectsCrossTenantAndProjectReuse(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, _, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	if err := manager.ensureSessionPVC(context.Background(), input); err != nil {
		t.Fatalf("ensureSessionPVC(first) error = %v", err)
	}
	for name, mutate := range map[string]func(*runtimecontract.RunnerInput){
		"organization": func(value *runtimecontract.RunnerInput) { value.OrganizationRef = "org_ijklmnop" },
		"project":      func(value *runtimecontract.RunnerInput) { value.ProjectRef = "prj_ijklmnop" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := input
			mutate(&candidate)
			if err := manager.ensureSessionPVC(context.Background(), candidate); err == nil || !strings.Contains(err.Error(), "exact session binding") {
				t.Fatalf("cross-boundary PVC reuse error = %v", err)
			}
		})
	}
}

func TestRetryMaterializesNewRevisionAndCleanupKeepsNewAttempt(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	firstExecution := testExecution(false)
	testContextRevision(firstExecution.Revision)
	sealTestTurnExecution(firstExecution)
	first, firstBinding, err := manager.BuildTurnInput(firstExecution)
	if err != nil || manager.EnsureTurn(context.Background(), first, firstBinding, testCredentialProjection(first)) != nil {
		t.Fatalf("materialize first attempt: %v", err)
	}
	retryExecution := testExecution(false)
	retryExecution.Revision.Ref = "revision_ijklmnop"
	retryExecution.Revision.Version = 2
	retryExecution.Revision.Attempt = 2
	retryExecution.Lease = &controlplanev1.WorkLease{Ref: "lease_ijklmnop", Fence: "fence-2", Generation: 2}
	policy, _ := runtimeEnvironmentPolicyFromProto(retryExecution.Revision.EnvironmentPolicy)
	access, _ := runtimecontract.RuntimeKubernetesAccessForExecution(policy.KubernetesAccess,
		runtimecontract.RuntimeServiceAccountName(retryExecution.Lease.Ref), runtimecontract.RuntimeTurnPodName(retryExecution.Lease.Ref))
	retryExecution.Revision.EffectiveKubernetesAccess = testRuntimeKubernetesAccessProto(access)
	sealTestTurnExecution(retryExecution)
	retry, retryBinding, err := manager.BuildTurnInput(retryExecution)
	if err != nil || manager.EnsureTurn(context.Background(), retry, retryBinding, testCredentialProjection(retry)) != nil {
		t.Fatalf("materialize retry attempt: %v", err)
	}
	if first.RuntimeRevisionDigest == retry.RuntimeRevisionDigest || first.ExecutionBindingDigest == retry.ExecutionBindingDigest || runtimeProjectionName(first) == runtimeProjectionName(retry) {
		t.Fatal("retry reused an immutable runtime binding")
	}
	if first.ContextSnapshot.Digest == retry.ContextSnapshot.Digest || len(retry.ContextSnapshot.Skills) != 0 || len(retry.ContextSnapshot.Memories) != 0 {
		t.Fatal("retry retained removed context")
	}
	if err := manager.DeleteTurn(context.Background(), first.LeaseRef); err != nil {
		t.Fatalf("DeleteTurn(first) error = %v", err)
	}
	for _, read := range []func() error{
		func() error {
			_, err := client.CoreV1().Pods("kodex-runtime").Get(context.Background(), runtimecontract.RuntimeTurnPodName(first.LeaseRef), metav1.GetOptions{})
			return err
		},
		func() error {
			_, err := client.CoreV1().Secrets("kodex-runtime").Get(context.Background(), ticketName(first.LeaseRef), metav1.GetOptions{})
			return err
		},
		func() error {
			_, err := client.CoreV1().ConfigMaps("kodex-runtime").Get(context.Background(), runtimeProjectionName(first), metav1.GetOptions{})
			return err
		},
		func() error {
			_, err := client.CoreV1().ServiceAccounts("kodex-runtime").Get(context.Background(), runtimecontract.RuntimeServiceAccountName(first.LeaseRef), metav1.GetOptions{})
			return err
		},
	} {
		if err := read(); !apierrors.IsNotFound(err) {
			t.Fatalf("old attempt resource survived cleanup: %v", err)
		}
	}
	if _, err := client.CoreV1().Pods("kodex-runtime").Get(context.Background(), runtimecontract.RuntimeTurnPodName(retry.LeaseRef), metav1.GetOptions{}); err != nil {
		t.Fatalf("new retry Pod was removed with predecessor: %v", err)
	}
	if _, err := client.CoreV1().ConfigMaps("kodex-runtime").Get(context.Background(), runtimeProjectionName(retry), metav1.GetOptions{}); err != nil {
		t.Fatalf("new retry projection was removed with predecessor: %v", err)
	}
}

func TestSessionPVCCreateRaceChecksWinnerBeforePod(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(fmt.Sprintf("foreign-%t", foreign), func(t *testing.T) {
			client := fake.NewSimpleClientset()
			manager := newTestManager(t, client)
			input, binding, err := manager.BuildTurnInput(testExecution(false))
			if err != nil {
				t.Fatal(err)
			}
			client.PrependReactor("create", "persistentvolumeclaims", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
				pvc := action.(k8stesting.CreateAction).GetObject().(*corev1.PersistentVolumeClaim).DeepCopy()
				if foreign {
					pvc.Annotations[projectHashAnnotation] = "foreign-project"
				}
				if err := client.Tracker().Add(pvc); err != nil {
					t.Fatal(err)
				}
				return true, nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "persistentvolumeclaims"}, pvc.Name)
			})
			err = manager.EnsureTurn(t.Context(), input, binding, testCredentialProjection(input))
			if (err != nil) != foreign {
				t.Fatalf("EnsureTurn() error = %v, foreign = %t", err, foreign)
			}
			pods, err := client.CoreV1().Pods("kodex-runtime").List(t.Context(), metav1.ListOptions{})
			if err != nil || foreign && len(pods.Items) != 0 || !foreign && len(pods.Items) != 1 {
				t.Fatal("Pod creation did not follow exact PVC readback")
			}
		})
	}
}

func TestExistingRuntimePodRejectsSecurityDrift(t *testing.T) {
	for name, mutate := range map[string]func(*corev1.Pod){
		"fsGroup": func(pod *corev1.Pod) { pod.Spec.SecurityContext.FSGroup = int64Pointer(0) },
		"seccomp": func(pod *corev1.Pod) {
			pod.Spec.SecurityContext.SeccompProfile.Type = corev1.SeccompProfileTypeUnconfined
		},
		"host network":  func(pod *corev1.Pod) { pod.Spec.HostNetwork = true },
		"host PID":      func(pod *corev1.Pod) { pod.Spec.HostPID = true },
		"host IPC":      func(pod *corev1.Pod) { pod.Spec.HostIPC = true },
		"service links": func(pod *corev1.Pod) { pod.Spec.EnableServiceLinks = boolPointer(true) },
	} {
		t.Run(name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			manager := newTestManager(t, client)
			input, binding, err := manager.BuildTurnInput(testExecution(false))
			if err != nil {
				t.Fatal(err)
			}
			if err := manager.EnsureTurn(t.Context(), input, binding, testCredentialProjection(input)); err != nil {
				t.Fatal(err)
			}
			pod, err := client.CoreV1().Pods("kodex-runtime").Get(t.Context(), turnPodName(input.LeaseRef), metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			mutate(pod)
			if _, err := client.CoreV1().Pods("kodex-runtime").Update(t.Context(), pod, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := manager.EnsureTurn(t.Context(), input, binding, testCredentialProjection(input)); err == nil {
				t.Fatal("security drift accepted")
			}
		})
	}
}

func TestEnsureTurnAcceptsAPIServerContainerDefaults(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, binding, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	if err := manager.EnsureTurn(context.Background(), input, binding, testCredentialProjection(input)); err != nil {
		t.Fatalf("EnsureTurn(first) error = %v", err)
	}
	pod, err := client.CoreV1().Pods("kodex-runtime").Get(context.Background(), turnPodName(input.LeaseRef), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(runtime Pod) error = %v", err)
	}
	applyDefaults := func(containers []corev1.Container) {
		for index := range containers {
			containers[index].TerminationMessagePath = "/dev/termination-log"
			containers[index].TerminationMessagePolicy = corev1.TerminationMessageReadFile
			for portIndex := range containers[index].Ports {
				containers[index].Ports[portIndex].Protocol = corev1.ProtocolTCP
			}
			for _, probe := range []*corev1.Probe{containers[index].StartupProbe, containers[index].ReadinessProbe, containers[index].LivenessProbe} {
				if probe != nil {
					probe.SuccessThreshold = 1
				}
			}
		}
	}
	applyDefaults(pod.Spec.InitContainers)
	applyDefaults(pod.Spec.Containers)
	if _, err := client.CoreV1().Pods("kodex-runtime").Update(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("Update(defaulted runtime Pod) error = %v", err)
	}
	if err := manager.EnsureTurn(context.Background(), input, binding, testCredentialProjection(input)); err != nil {
		t.Fatalf("EnsureTurn(defaulted existing Pod) error = %v", err)
	}
}

func TestCleanupStaleTurnsRemovesOrphanedExecutionPolicy(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, _, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatalf("BuildTurnInput() error = %v", err)
	}
	if err := manager.ensureExecutionPolicy(context.Background(), input, runtimecontract.RuntimeTurnPodName(input.LeaseRef)); err != nil {
		t.Fatalf("ensureExecutionPolicy() error = %v", err)
	}
	if err := manager.ensureProjection(context.Background(), runtimecontract.RuntimeTurnPodName(input.LeaseRef), "turn", input); err != nil {
		t.Fatalf("ensureProjection() error = %v", err)
	}
	if err := manager.CleanupStaleTurns(context.Background()); err != nil {
		t.Fatalf("CleanupStaleTurns() error = %v", err)
	}
	if _, err := client.CoreV1().ServiceAccounts("kodex-runtime").Get(context.Background(), input.EffectiveKubernetesAccess.ServiceAccountName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("orphaned execution ServiceAccount survived cleanup: %v", err)
	}
	if _, err := client.NetworkingV1().NetworkPolicies("kodex-runtime").Get(context.Background(), runtimecontract.RuntimeNetworkPolicyName(input.LeaseRef), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("orphaned execution NetworkPolicy survived cleanup: %v", err)
	}
	if _, err := client.CoreV1().ConfigMaps("kodex-runtime").Get(context.Background(), runtimeProjectionName(input), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("orphaned runtime projection survived cleanup: %v", err)
	}
}

func newTestManager(t *testing.T, client *fake.Clientset) *Manager {
	t.Helper()
	manager, err := New(client, testManagerConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	immutable := true
	_, err = client.CoreV1().Secrets("kodex-runtime").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "runtime-provider-openai-default-r1", Namespace: "kodex-runtime",
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
		Environment: "test", ControlNamespace: "kodex-system", RuntimeNamespace: "kodex-runtime",
		ControllerPodUID: "controller-pod-uid", ControllerPodIP: "10.0.0.10",
		CallbackTLSServerName:  "runtime-controller-callback.kodex-system.svc.cluster.local",
		CallbackClientCASecret: "runtime-execution-client-tls", CallbackClientTLSSecret: "runtime-execution-client-tls",
		ProviderHTTPSProxy:      "http://egress-gateway.kodex-system.svc:8080",
		ProviderAppArmorProfile: "kodex-provider-runtime",
		KubernetesAPIServiceIP:  "10.43.0.1",
		StorageClass:            "", SessionPVCSize: "20Gi", RunnerServiceAccount: "agent-runner",
		PromotedRoleImageRepository: "registry.example/kodex/roles",
		DefaultRoleImageReference:   "registry.example/kodex/agent-runner@" + testDefaultDigest,
		RoleRuntimeContractRevision: 1,
		RoleRuntimeContractSHA256:   testContractDigest,
	}
}

func testCredentialProjection(input runtimecontract.RunnerInput) CredentialProjection {
	keys := make(map[string]string, len(input.SecretProjections))
	for _, item := range input.SecretProjections {
		keys[item.Name] = item.Name
	}
	return CredentialProjection{
		Namespace: "kodex-runtime", SecretName: "runtime-credentials-0123456789abcdef0123456789abcdef01234567",
		SecretUID: "40000000-0000-4000-8000-000000000001", SecretResourceVersion: "19",
		ContentSHA256: strings.Repeat("c", 64), ProviderAuthKey: "provider-auth.json", RuntimeSecretKeys: keys,
	}
}

func TestProviderSandboxSecurityContextDoesNotAssumeNodeLocalAppArmor(t *testing.T) {
	securityContext := providerSandboxSecurityContext(10002, "")
	if securityContext.AppArmorProfile != nil {
		t.Fatalf("base provider AppArmor profile = %#v", securityContext.AppArmorProfile)
	}
	if securityContext.SeccompProfile == nil || securityContext.SeccompProfile.Type != corev1.SeccompProfileTypeUnconfined {
		t.Fatalf("base provider seccomp profile = %#v", securityContext.SeccompProfile)
	}
}

func testExecution(systemAssistant bool) *controlplanev1.ClaimedExecution {
	attachmentSetRef := "aset_abcdefgh"
	attachmentPurpose := "RUN_INPUT"
	artifact := &controlplanev1.RuntimeInputArtifact{
		Artifact: &controlplanev1.Artifact{Ref: "artifact_abcdefgh", FileName: "brief.txt", MediaType: "text/plain", SizeBytes: 12, Digest: testArtifactDigest, Revision: 1, Version: 1, Source: controlplanev1.ArtifactSource_ARTIFACT_SOURCE_CONTROL_CENTER},
		Scope:    runtimecontract.AttachmentScopeInput, Position: 1, AttachmentSetRef: attachmentSetRef,
		AttachmentPurpose: attachmentPurpose, Provenance: "CURRENT_TURN",
	}
	manifest, err := runtimecontract.BuildAttachmentManifest(attachmentSetRef, attachmentPurpose, []runtimecontract.RunnerInputArtifact{{
		Ref: artifact.Artifact.Ref, FileName: artifact.Artifact.FileName, MediaType: artifact.Artifact.MediaType,
		Digest: artifact.Artifact.Digest, SizeBytes: artifact.Artifact.SizeBytes, Revision: int64(artifact.Artifact.Revision),
		Version: artifact.Artifact.Version, Scope: artifact.Scope, Position: artifact.Position, Source: "CONTROL_CENTER",
	}})
	if err != nil {
		panic(err)
	}
	execution := &controlplanev1.ClaimedExecution{
		Run: &controlplanev1.Run{Ref: "run_abcdefgh", ProjectRef: "prj_abcdefgh"}, Node: &controlplanev1.RunNode{Ref: "node_abcdefgh"},
		Revision: &controlplanev1.RuntimeRevisionSnapshot{
			Ref: "revision_abcdefgh", Version: 1, OrganizationRef: "org_abcdefgh", SessionRef: "session_abcdefgh", TurnRef: "turn_abcdefgh", Attempt: 1,
			AgentRef: "agent_abcdefgh", Instructions: "Complete the server-owned task.", Runtime: &controlplanev1.RuntimeSelection{Ref: "runtime_abcdefgh", Revision: "revision-1", Provider: "openai", Model: "codex"},
			RevisionDigest: strings.Repeat("a", 64), SystemAssistant: systemAssistant,
			RoleDefinitionRef: "roledef_abcdefgh", InstructionRef: "instruction_abcdefgh", InstructionDigest: strings.Repeat("4", 64),
			PromptTemplateRef: "prompt_abcdefgh", PromptTemplateDigest: strings.Repeat("5", 64), PromptMaterializationDigest: strings.Repeat("6", 64),
			ImageReference: "registry.example/kodex/roles@" + testDigest, ImageManifestDigest: testDigest,
			RoleRuntimeContractRevision: 1, RoleRuntimeContractSha256: testContractDigest,
			Capabilities:     []*controlplanev1.PlatformCapability{{Key: runtimecontract.ArtifactCapability}},
			AttachmentSetRef: attachmentSetRef, AttachmentSetManifestDigest: manifest.Digest, AttachmentContext: attachmentPurpose,
			AttachmentSets: []*controlplanev1.RuntimeAttachmentSet{{
				Ref: attachmentSetRef, ManifestDigest: manifest.Digest, Purpose: attachmentPurpose,
				Scope: runtimecontract.AttachmentScopeInput, Provenance: "CURRENT_TURN", TurnRef: "turn_abcdefgh",
			}},
			InputArtifacts: []*controlplanev1.RuntimeInputArtifact{artifact},
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
	policy := runtimecontract.DefaultRuntimeEnvironmentPolicy()
	serviceAccountName := runtimecontract.RuntimeServiceAccountName(execution.Lease.Ref)
	podName := runtimecontract.RuntimeTurnPodName(execution.Lease.Ref)
	access, _ := runtimecontract.RuntimeKubernetesAccessForExecution(policy.KubernetesAccess, serviceAccountName, podName)
	execution.Revision.EnvironmentPolicy = testRuntimeEnvironmentPolicyProto(policy)
	execution.Revision.EffectiveKubernetesAccess = testRuntimeKubernetesAccessProto(access)
	workspace := runtimecontract.RuntimeWorkspacePolicyV1()
	execution.Revision.WorkspacePolicy = &controlplanev1.RuntimeWorkspacePolicy{Revision: workspace.Revision, Root: workspace.Root, MaximumWritableBytes: workspace.MaximumWritableBytes, MaximumFileCount: workspace.MaximumFileCount, Digest: workspace.Digest,
		DenialReasons: []controlplanev1.RuntimeWorkspaceDenialReason{controlplanev1.RuntimeWorkspaceDenialReason_RUNTIME_WORKSPACE_DENIAL_REASON_READ_ONLY, controlplanev1.RuntimeWorkspaceDenialReason_RUNTIME_WORKSPACE_DENIAL_REASON_QUOTA_EXCEEDED, controlplanev1.RuntimeWorkspaceDenialReason_RUNTIME_WORKSPACE_DENIAL_REASON_PATH_OUTSIDE_WORKSPACE, controlplanev1.RuntimeWorkspaceDenialReason_RUNTIME_WORKSPACE_DENIAL_REASON_RUNTIME_IO_ERROR}}
	for _, rule := range workspace.Rules {
		access := controlplanev1.RuntimeWorkspaceAccess_RUNTIME_WORKSPACE_ACCESS_READ_ONLY
		if rule.Access == runtimecontract.RuntimeWorkspaceWritable {
			access = controlplanev1.RuntimeWorkspaceAccess_RUNTIME_WORKSPACE_ACCESS_WRITABLE
		}
		execution.Revision.WorkspacePolicy.Rules = append(execution.Revision.WorkspacePolicy.Rules, &controlplanev1.RuntimeWorkspacePathRule{Path: rule.Path, Access: access})
	}
	image, tools := runtimeEnvironmentContract(execution.Revision)
	execution.Revision.RuntimeEnvironmentDigest, _ = runtimecontract.RuntimeEnvironmentDigest(nil, nil, image, tools, policy)
	execution.Revision.InputDigest, _ = runtimecontract.RuntimeBoundedInputDigest(map[string]any{})
	sealTestTurnExecution(execution)
	return execution
}

func testWarmRevision() *controlplanev1.RuntimeRevisionSnapshot {
	execution := testExecution(true)
	execution.Revision.AttachmentSetRef = ""
	execution.Revision.AttachmentSetManifestDigest = ""
	execution.Revision.AttachmentContext = ""
	execution.Revision.AttachmentSets = nil
	execution.Revision.InputArtifacts = nil
	policy, err := runtimeEnvironmentPolicyFromProto(execution.Revision.GetEnvironmentPolicy())
	if err != nil {
		panic(err)
	}
	access, err := runtimecontract.RuntimeKubernetesAccessForExecution(policy.KubernetesAccess, "agent-runner", "system-assistant-warm")
	if err != nil {
		panic(err)
	}
	execution.Revision.EffectiveKubernetesAccess = testRuntimeKubernetesAccessProto(access)
	sealTestWarmRevision(execution.Revision)
	return execution.Revision
}

func sealTestTurnExecution(execution *controlplanev1.ClaimedExecution) {
	manager := &Manager{config: testManagerConfig()}
	input, err := manager.baseInput(execution.Revision, runtimecontract.RunnerModeTurn)
	if err != nil {
		panic(err)
	}
	input.RunRef, input.NodeRef, input.SessionRef, input.TurnRef = execution.Run.Ref, execution.Node.Ref, execution.Revision.SessionRef, execution.Revision.TurnRef
	input.ProjectRef, input.AgentRef, input.Attempt = execution.Run.ProjectRef, execution.Revision.AgentRef, execution.Revision.Attempt
	input.InputDigest = execution.Revision.InputDigest
	input.LeaseRef, input.LeaseFence, input.LeaseGeneration = execution.Lease.Ref, execution.Lease.Fence, execution.Lease.Generation
	input.Task, input.BoundedInput = execution.Task, map[string]any{}
	if execution.Revision.BoundedInput != nil {
		input.BoundedInput = execution.Revision.BoundedInput.AsMap()
	}
	manager.addCatalog(&input, execution.Revision)
	if err := hydrateRuntimeContext(&input, execution.Revision); err != nil {
		panic(err)
	}
	binding, err := providerSecretBinding(execution.Revision)
	if err != nil {
		panic(err)
	}
	digest, err := runtimecontract.RuntimeRevisionDigest(input, runtimecontract.RuntimeRevisionCredentialSource{
		SecretName: binding.Name, SecretUID: binding.UID, SecretResourceVersion: binding.ResourceVersion,
	})
	if err != nil {
		panic(err)
	}
	execution.Revision.RevisionDigest = digest
}

func sealTestWarmRevision(revision *controlplanev1.RuntimeRevisionSnapshot) {
	manager := &Manager{config: testManagerConfig()}
	input, err := manager.baseInput(revision, runtimecontract.RunnerModeWarm)
	if err != nil {
		panic(err)
	}
	input.SessionRef, input.AgentRef = revision.SessionRef, revision.AgentRef
	manager.addCatalog(&input, revision)
	if err := hydrateRuntimeContext(&input, revision); err != nil {
		panic(err)
	}
	binding, err := providerSecretBinding(revision)
	if err != nil {
		panic(err)
	}
	digest, err := runtimecontract.RuntimeRevisionDigest(input, runtimecontract.RuntimeRevisionCredentialSource{
		SecretName: binding.Name, SecretUID: binding.UID, SecretResourceVersion: binding.ResourceVersion,
	})
	if err != nil {
		panic(err)
	}
	revision.RevisionDigest = digest
}

func testRuntimeEnvironmentPolicyProto(policy runtimecontract.RuntimeEnvironmentPolicy) *controlplanev1.RuntimeEnvironmentPolicy {
	result := &controlplanev1.RuntimeEnvironmentPolicy{
		Resources: &controlplanev1.RuntimeResourcePolicy{
			CpuRequestMilli: policy.Resources.CPURequestMilli, CpuLimitMilli: policy.Resources.CPULimitMilli,
			MemoryRequestMib: policy.Resources.MemoryRequestMiB, MemoryLimitMib: policy.Resources.MemoryLimitMiB,
			EphemeralStorageRequestMib: policy.Resources.EphemeralStorageRequestMiB,
			EphemeralStorageLimitMib:   policy.Resources.EphemeralStorageLimitMiB,
		},
		Network: &controlplanev1.RuntimeNetworkPolicy{DenyByDefault: policy.Network.DenyByDefault},
		KubernetesAccess: &controlplanev1.RuntimeKubernetesAccessProfile{
			Kind: controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_NONE, Namespace: policy.KubernetesAccess.Namespace,
		},
		ResourcesDigest: policy.ResourcesDigest, VolumesDigest: policy.VolumesDigest,
		NetworkDigest: policy.NetworkDigest, RbacDigest: policy.RBACDigest,
	}
	if policy.KubernetesAccess.Kind == runtimecontract.RuntimeKubernetesAccessReadOwnExecution {
		result.KubernetesAccess.Kind = controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_READ_OWN_EXECUTION
	}
	for _, volume := range policy.Volumes {
		kind := controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_DISK
		if volume.Kind == runtimecontract.RuntimeVolumeEphemeralMemory {
			kind = controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_MEMORY
		}
		result.Volumes = append(result.Volumes, &controlplanev1.RuntimeVolume{Name: volume.Name, Kind: kind, SizeMib: volume.SizeMiB, MountPath: volume.MountPath})
	}
	for _, egress := range policy.Network.Egress {
		destination := map[string]controlplanev1.RuntimeNetworkDestination{
			runtimecontract.RuntimeEgressDNS:             controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_DNS,
			runtimecontract.RuntimeEgressRuntimeCallback: controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_RUNTIME_CALLBACK,
			runtimecontract.RuntimeEgressProviderProxy:   controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_PROVIDER_PROXY,
			runtimecontract.RuntimeEgressKubernetesAPI:   controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_KUBERNETES_API,
		}[egress.Destination]
		protocol := controlplanev1.RuntimeNetworkProtocol_RUNTIME_NETWORK_PROTOCOL_TCP
		if egress.Protocol == runtimecontract.RuntimeProtocolUDP {
			protocol = controlplanev1.RuntimeNetworkProtocol_RUNTIME_NETWORK_PROTOCOL_UDP
		}
		result.Network.Egress = append(result.Network.Egress, &controlplanev1.RuntimeNetworkEgress{Destination: destination, Protocol: protocol, Port: egress.Port})
	}
	return result
}

func testRuntimeKubernetesAccessProto(access runtimecontract.RuntimeKubernetesAccess) *controlplanev1.RuntimeKubernetesAccess {
	profileKind := controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_NONE
	if access.Profile.Kind == runtimecontract.RuntimeKubernetesAccessReadOwnExecution {
		profileKind = controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_READ_OWN_EXECUTION
	}
	result := &controlplanev1.RuntimeKubernetesAccess{Profile: &controlplanev1.RuntimeKubernetesAccessProfile{
		Kind: profileKind, Namespace: access.Profile.Namespace,
	}, ServiceAccountName: access.ServiceAccountName, Digest: access.Digest}
	for _, rule := range access.Rules {
		result.Rules = append(result.Rules, &controlplanev1.RuntimeKubernetesRule{ApiGroup: rule.APIGroup, Resource: rule.Resource,
			Verbs: append([]string(nil), rule.Verbs...), ResourceNames: append([]string(nil), rule.ResourceNames...)})
	}
	return result
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
