package workload

import (
	"fmt"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// Fake Kubernetes доказывает материализацию и ownership, не live acceptance.
func TestParallelAssistantSessionsMaterializeAndCleanupIndependently(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	const count = 10
	inputs := make([]runtimecontract.RunnerInput, 0, count)
	bindings := make([]ProviderSecretBinding, 0, count)
	credentials := make([]CredentialProjection, 0, count)
	compatibilities := map[string]bool{}
	for index := range count {
		execution := testExecution(true)
		execution.Run.Ref = fmt.Sprintf("run_parallel_%02d", index)
		execution.Run.ProjectRef = ""
		execution.Node.Ref = fmt.Sprintf("node_parallel_%02d", index)
		execution.Revision.Ref = fmt.Sprintf("revision_parallel_%02d", index)
		execution.Revision.SessionRef = fmt.Sprintf("session_parallel_%02d", index)
		execution.Revision.TurnRef = fmt.Sprintf("turn_parallel_%02d", index)
		execution.Lease.Ref = fmt.Sprintf("lease_parallel_%02d", index)
		execution.Lease.Fence = fmt.Sprintf("synthetic-fence-%02d", index)
		execution.Revision.AttachmentSetRef = ""
		execution.Revision.AttachmentSetManifestDigest = ""
		execution.Revision.AttachmentContext = ""
		execution.Revision.AttachmentSets = nil
		execution.Revision.InputArtifacts = nil
		policy, err := runtimeEnvironmentPolicyFromProto(execution.Revision.EnvironmentPolicy)
		if err != nil {
			t.Fatal(err)
		}
		access, err := runtimecontract.RuntimeKubernetesAccessForExecution(policy.KubernetesAccess,
			runtimecontract.RuntimeServiceAccountName(execution.Lease.Ref), runtimecontract.RuntimeTurnPodName(execution.Lease.Ref))
		if err != nil {
			t.Fatal(err)
		}
		execution.Revision.EffectiveKubernetesAccess = testRuntimeKubernetesAccessProto(access)
		sealTestTurnExecution(execution)
		input, binding, err := manager.BuildTurnInput(execution)
		if err != nil {
			t.Fatalf("build isolated session %d: %v", index, err)
		}
		credential := testCredentialProjection(input)
		// Broker проекция также отдельная; общий synthetic provider account не
		// означает общий credential projection или grant исполняющего процесса.
		credential.SecretName = fmt.Sprintf("runtime-credentials-%040d", index)
		credential.SecretUID = fmt.Sprintf("40000000-0000-4000-8000-%012d", index+1)
		if err := manager.EnsureTurn(t.Context(), input, binding, credential); err != nil {
			t.Fatalf("materialize isolated session %d: %v", index, err)
		}
		digest, err := runtimecontract.WarmCompatibilityDigest(input)
		if err != nil || compatibilities[digest] {
			t.Fatalf("warm compatibility shared between distinct sessions: %v", err)
		}
		compatibilities[digest] = true
		inputs, bindings, credentials = append(inputs, input), append(bindings, binding), append(credentials, credential)
	}
	assertResources := func(want int) {
		t.Helper()
		pods, err := client.CoreV1().Pods("kodex-runtime").List(t.Context(), metav1.ListOptions{})
		if err != nil || len(pods.Items) != want {
			t.Fatalf("pod cardinality want=%d got=%d err=%v", want, len(pods.Items), err)
		}
		volumes, err := client.CoreV1().PersistentVolumeClaims("kodex-runtime").List(t.Context(), metav1.ListOptions{})
		if err != nil || len(volumes.Items) != count {
			t.Fatalf("durable session cardinality want=%d got=%d err=%v", count, len(volumes.Items), err)
		}
		for relativeIndex, input := range inputs[count-want:] {
			credential := credentials[count-want+relativeIndex]
			pod, err := client.CoreV1().Pods("kodex-runtime").Get(t.Context(), runtimecontract.RuntimeTurnPodName(input.LeaseRef), metav1.GetOptions{})
			if err != nil || pod.Annotations[sessionHashAnnotation] != shortHash(input.SessionRef) || pod.Annotations[leaseAnnotation] != input.LeaseRef ||
				pod.Annotations[credentialProjectionNameAnnotation] != credential.SecretName || pod.Annotations[credentialProjectionUIDAnnotation] != credential.SecretUID {
				t.Fatalf("pod lost exact session/lease ownership: %v", err)
			}
			volumeName, err := runtimecontract.SessionPVCName(input.SessionRef)
			if err != nil || podVolumeByName(t, pod, "session").PersistentVolumeClaim.ClaimName != volumeName {
				t.Fatal("turn mounted another conversation's session volume")
			}
			if _, err := client.CoreV1().Secrets("kodex-runtime").Get(t.Context(), ticketName(input.LeaseRef), metav1.GetOptions{}); err != nil {
				t.Fatalf("isolated execution ticket was removed: %v", err)
			}
			if _, err := client.CoreV1().ServiceAccounts("kodex-runtime").Get(t.Context(), runtimecontract.RuntimeServiceAccountName(input.LeaseRef), metav1.GetOptions{}); err != nil {
				t.Fatalf("isolated runtime identity was removed: %v", err)
			}
			projection, err := client.CoreV1().ConfigMaps("kodex-runtime").Get(t.Context(), runtimeProjectionName(input), metav1.GetOptions{})
			if err != nil || projection.Annotations[sessionHashAnnotation] != shortHash(input.SessionRef) {
				t.Fatalf("immutable projection crossed a session: %v", err)
			}
		}
	}
	assertResources(count)
	// Новый Manager на том же API state — проверка crash-safe readback и
	// идемпотентного adoption, а не настоящий рестарт controller в кластере.
	restarted, err := New(client, testManagerConfig())
	if err != nil {
		t.Fatal(err)
	}
	for index, input := range inputs {
		if err := restarted.EnsureTurn(t.Context(), input, bindings[index], credentials[index]); err != nil {
			t.Fatalf("adopt isolated session %d: %v", index, err)
		}
	}
	assertResources(count)
	if err := restarted.DeleteTurn(t.Context(), inputs[0].LeaseRef); err != nil {
		t.Fatalf("delete first isolated turn: %v", err)
	}
	assertResources(count - 1)
	for name, read := range map[string]func() error{
		"pod": func() error {
			_, err := client.CoreV1().Pods("kodex-runtime").Get(t.Context(), runtimecontract.RuntimeTurnPodName(inputs[0].LeaseRef), metav1.GetOptions{})
			return err
		},
		"ticket": func() error {
			_, err := client.CoreV1().Secrets("kodex-runtime").Get(t.Context(), ticketName(inputs[0].LeaseRef), metav1.GetOptions{})
			return err
		},
		"projection": func() error {
			_, err := client.CoreV1().ConfigMaps("kodex-runtime").Get(t.Context(), runtimeProjectionName(inputs[0]), metav1.GetOptions{})
			return err
		},
		"identity": func() error {
			_, err := client.CoreV1().ServiceAccounts("kodex-runtime").Get(t.Context(), runtimecontract.RuntimeServiceAccountName(inputs[0].LeaseRef), metav1.GetOptions{})
			return err
		},
	} {
		if err := read(); !apierrors.IsNotFound(err) {
			t.Fatalf("retired %s survived exact cleanup: %v", name, err)
		}
	}
}
