package workload

import (
	"context"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
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
	manager.config.StorageClass = "local-path"
	execution := testExecution(false)
	execution.Revision.SessionRef, execution.Revision.AgentRef = "ses_workspace000", "agt_workspace000"
	execution.Revision.RoleRuntimeContractRevision = runtimecontract.WorkspaceStorageRuntimeContractRevision
	execution.Revision.WorkspaceStorage = &controlplanev1.RuntimeWorkspaceStorage{Profile: runtimecontract.WorkspaceStorageProfile,
		Scope: &controlplanev1.RuntimeWorkspaceScope{OrganizationRef: execution.Revision.OrganizationRef, ProjectRef: execution.Run.ProjectRef,
			SessionRef: execution.Revision.SessionRef, AgentRef: execution.Revision.AgentRef}, WorkspaceRef: "wks_workspace000", Generation: 1}
	storage := execution.Revision.WorkspaceStorage
	storage.VolumeReady = &controlplanev1.RuntimeWorkspaceVolumeReady{Schema: runtimecontract.WorkspaceVolumeReadySchema, Device: 31, Inode: 47,
		Preparation: &controlplanev1.RuntimeWorkspaceVolumePreparation{Scope: storage.Scope,
			WorkspaceRef: storage.WorkspaceRef, TaskRef: "svt_workspace000", PvcUid: "30000000-0000-4000-8000-000000000001", VolumeGeneration: 1}}
	sealTestTurnExecution(execution)
	manager.config.RoleRuntimeContractRevision = runtimecontract.WorkspaceStorageRuntimeContractRevision
	input, binding, err := manager.BuildTurnInput(execution)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimecontract.EncodeRunnerInput(input); err != nil {
		t.Fatalf("V2 fixture is not a valid immutable input: %v", err)
	}
	labels, annotations, err := runtimecontract.SessionVolumeMetadata(input.OrganizationRef, input.ProjectRef, input.SessionRef)
	if err != nil {
		t.Fatal(err)
	}
	annotations["workspace.kodex.dev/workspace-ref"] = storage.WorkspaceRef
	annotations["workspace.kodex.dev/volume-generation"] = "1"
	pvcName, err := runtimecontract.SessionPVCName(input.SessionRef)
	if err != nil {
		t.Fatal(err)
	}
	mode, storageClass := corev1.PersistentVolumeFilesystem, manager.config.StorageClass
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: pvcName, Namespace: manager.config.RuntimeNamespace,
		UID: "30000000-0000-4000-8000-000000000001", ResourceVersion: "1", Labels: labels, Annotations: annotations},
		Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &storageClass, VolumeMode: &mode, VolumeName: "pv-workspace-fixture",
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources:   corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: manager.pvcRequest.DeepCopy()}}},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	if _, err := client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Create(t.Context(), pvc, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
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
				container.Name == "provider-credential-relay" && mount.Name == "session" ||
				mount.Name == "session" && mount.SubPath == "" ||
				mount.MountPath == "/workspace/.kodex/state" && mount.SubPath != runtimecontract.WorkspaceProviderStateSubPath {
				t.Fatal("private or writable metadata leaked to a runtime consumer")
			}
		}
	}
}

// TestWorkspaceStorageWritersKeepPrimaryGroupAfterPrivateDirectoryRestore prevents new files below mode0700 from drifting to the provider's UID group.
// TestWorkspaceStorageWritersKeepPrimaryGroupAfterPrivateDirectoryRestore предотвращает смену group новых файлов под mode0700 на UID group провайдера.
func TestWorkspaceStorageWritersKeepPrimaryGroupAfterPrivateDirectoryRestore(t *testing.T) {
	_, _, _, pod := workspaceStoragePodFixture(t)
	for _, group := range [][]corev1.Container{pod.Spec.InitContainers, pod.Spec.Containers} {
		for _, container := range group {
			want := int64(29000)
			if container.Name == "provider-credential-relay" {
				want = 10003
			}
			if container.SecurityContext == nil || container.SecurityContext.RunAsGroup == nil || *container.SecurityContext.RunAsGroup != want {
				t.Fatal("V2 writer primary group does not preserve restored private directory ownership")
			}
		}
	}
}

// TestWorkspacePodRejectsStorageDrift proves exact subPath, UID, mode and nested mount violations fail closed.
// TestWorkspacePodRejectsStorageDrift доказывает closed отказ при изменении subPath, UID, mode и nested mount.
func TestWorkspacePodRejectsStorageDrift(t *testing.T) {
	manager, _, input, pod := workspaceStoragePodFixture(t)
	for name, mutate := range map[string]func(*corev1.Pod){
		"old provider primary group":    func(value *corev1.Pod) { value.Spec.Containers[1].SecurityContext.RunAsGroup = int64Pointer(10002) },
		"old initializer primary group": func(value *corev1.Pod) { value.Spec.InitContainers[0].SecurityContext.RunAsGroup = int64Pointer(10001) },
		"full PVC through provider state": func(value *corev1.Pod) {
			for index := range value.Spec.Containers[1].VolumeMounts {
				if value.Spec.Containers[1].VolumeMounts[index].MountPath == "/workspace/.kodex/state" {
					value.Spec.Containers[1].VolumeMounts[index].SubPath = ""
				}
			}
		},
		"history points at private generations": func(value *corev1.Pod) {
			for index := range value.Spec.Containers[0].VolumeMounts {
				if value.Spec.Containers[0].VolumeMounts[index].MountPath == "/workspace/.kodex/state" {
					value.Spec.Containers[0].VolumeMounts[index].SubPath = "workspaces"
				}
			}
		},
		"missing pod security": func(value *corev1.Pod) { value.Spec.SecurityContext = nil },
		"recursive group change": func(value *corev1.Pod) {
			value.Spec.SecurityContext.FSGroupChangePolicy = fsGroupChangePolicyPointer(corev1.FSGroupChangeAlways)
		},
		"wrong pod group": func(value *corev1.Pod) { value.Spec.SecurityContext.FSGroup = int64Pointer(0) },
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
