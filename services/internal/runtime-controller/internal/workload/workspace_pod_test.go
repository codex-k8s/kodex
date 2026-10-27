package workload

import (
	"context"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// workspaceStoragePodFixture builds the actual Pod producer with safe V2 scope and fake Kubernetes objects.
// It performs no network and never treats fake UID values as a production ownership proof.
// workspaceStoragePodFixture вызывает actual Pod producer с safe V2 scope и fake Kubernetes objects.
// Сети нет; fake UID не считается production ownership proof.
func workspaceStoragePodFixture(t *testing.T) (*Manager, *fake.Clientset, runtimecontract.RunnerInput, *corev1.Pod) {
	t.Helper()
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, binding, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatal(err)
	}
	input.SessionRef, input.AgentRef = "ses_workspace000", "agt_workspace000"
	manager.config.RoleRuntimeContractRevision = runtimecontract.WorkspaceStorageRuntimeContractRevision
	input.RoleRuntimeContractRevision = runtimecontract.WorkspaceStorageRuntimeContractRevision
	input.WorkspaceStorage = &runtimecontract.WorkspaceStorageBinding{Profile: runtimecontract.WorkspaceStorageProfile,
		Scope:        runtimecontract.WorkspaceStorageScope{OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, SessionRef: input.SessionRef, AgentRef: input.AgentRef},
		WorkspaceRef: "wks_workspace000", Generation: 1}
	credentials := testCredentialProjection(input)
	pod := manager.runtimePod(input, binding, &credentials, ticketName(input.LeaseRef), runtimecontract.RuntimeTurnPodName(input.LeaseRef), "turn")
	if pod == nil {
		t.Fatal("actual V2 Pod producer rejected fixture")
	}
	pod.UID = "20000000-0000-4000-8000-000000000001"
	return manager, client, input, pod
}

// TestWorkspacePodMountsRequireBindBeforePrivateGenerationAccess checks actual producer ordering and isolation.
// Neither provider nor relay receives the full private PVC root or writable proof metadata.
// TestWorkspacePodMountsRequireBindBeforePrivateGenerationAccess проверяет actual producer ordering и isolation.
// Provider и relay не получают полный private PVC root или writable proof metadata.
func TestWorkspacePodMountsRequireBindBeforePrivateGenerationAccess(t *testing.T) {
	manager, _, input, pod := workspaceStoragePodFixture(t)
	if !manager.workspaceStoragePodMatches(input, pod) || pod.Spec.InitContainers[0].Args[0] != "runtime-bind-workspace" {
		t.Fatal("actual V2 Pod failed owned storage validation")
	}
	for _, mount := range pod.Spec.InitContainers[0].VolumeMounts {
		if mount.MountPath == runtimecontract.RuntimeWorkspaceWorkingRoot || mount.MountPath == "/workspace/.kodex/state" {
			t.Fatal("generation/state mounted before authenticated first BIND")
		}
	}
	for _, container := range pod.Spec.Containers {
		for _, mount := range container.VolumeMounts {
			if mount.MountPath == runtimecontract.WorkspaceStorageRootMount || mount.MountPath == runtimecontract.WorkspaceMountProofPath && !mount.ReadOnly ||
				container.Name == "provider-credential-relay" && mount.Name == "session" {
				t.Fatal("private or writable metadata leaked to a runtime consumer")
			}
		}
	}
}

// TestWorkspacePodRejectsStorageDrift proves exact subPath, UID, mode and nested mount violations fail closed.
// TestWorkspacePodRejectsStorageDrift доказывает closed отказ при изменении subPath, UID, mode и nested mount.
func TestWorkspacePodRejectsStorageDrift(t *testing.T) {
	manager, _, input, pod := workspaceStoragePodFixture(t)
	for name, mutate := range map[string]func(*corev1.Pod){
		"foreign generation": func(value *corev1.Pod) {
			value.Spec.Containers[0].VolumeMounts = append(value.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "session", MountPath: "/foreign", SubPath: "workspaces/wks_foreign000/g1/tree"})
		},
		"nested work mount": func(value *corev1.Pod) {
			value.Spec.Containers[0].VolumeMounts = append(value.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "tmp", MountPath: "/workspace/work/nested"})
		},
		"untrusted root": func(value *corev1.Pod) {
			value.Spec.Containers[1].VolumeMounts = append(value.Spec.Containers[1].VolumeMounts, corev1.VolumeMount{Name: "session", MountPath: runtimecontract.WorkspaceStorageRootMount})
		},
		"wrong provider UID":   func(value *corev1.Pod) { value.Spec.Containers[1].SecurityContext.RunAsUser = int64Pointer(10001) },
		"wrong owner metadata": func(value *corev1.Pod) { value.Annotations[executionBindingAnnotation] = "foreign" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := pod.DeepCopy()
			mutate(changed)
			if manager.workspaceStoragePodMatches(input, changed) {
				t.Fatal("storage drift accepted")
			}
		})
	}
}

// TestWorkspaceBindRejectsLiveSecondConsumer proves RWO is not used as single-writer evidence.
// The first observation is read-only and returns exact actual UIDs; adding a consumer closes admission.
// TestWorkspaceBindRejectsLiveSecondConsumer доказывает, что RWO не является single-writer evidence.
// Первая read-only observation возвращает actual UID; новый consumer закрывает admission.
func TestWorkspaceBindRejectsLiveSecondConsumer(t *testing.T) {
	manager, client, input, pod := workspaceStoragePodFixture(t)
	ctx := context.Background()
	if err := manager.ensureSessionPVC(ctx, input); err != nil {
		t.Fatal(err)
	}
	name, _ := runtimecontract.SessionPVCName(input.SessionRef)
	pvc, err := client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pvc.UID = "30000000-0000-4000-8000-000000000001"
	if _, err := client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Update(ctx, pvc, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Pods(manager.config.RuntimeNamespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	observation, err := manager.ObserveWorkspaceBind(ctx, input)
	if err != nil || observation.PodUID != string(pod.UID) || observation.PVCUID != string(pvc.UID) {
		t.Fatalf("owned observation rejected: %v", err)
	}
	other := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "other-writer", Namespace: manager.config.RuntimeNamespace, UID: "40000000-0000-4000-8000-000000000001"},
		Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "work", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: name}}}}}}
	if _, err := client.CoreV1().Pods(manager.config.RuntimeNamespace).Create(ctx, other, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ObserveWorkspaceBind(ctx, input); err == nil {
		t.Fatal("second PVC consumer accepted")
	}
}
