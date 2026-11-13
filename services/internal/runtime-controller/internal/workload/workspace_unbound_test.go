package workload

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"slices"
)

// TestWorkspaceUnboundStopRequiresExactTerminalNeverStartedProof exercises the actual qualified
// Pod producer and rejects active/later init, missing status, node replacement and changed identity.
// TestWorkspaceUnboundStopRequiresExactTerminalNeverStartedProof проверяет actual qualified
// Pod producer и отказ active/later init, missing status, node replacement и changed identity.
func TestWorkspaceUnboundStopRequiresExactTerminalNeverStartedProof(t *testing.T) {
	for _, variant := range []string{"unstarted", "first-failed", "active", "later-init", "missing-status", "node-replaced", "node-unready", "foreign-pod", "old-start", "node-lost", "restart", "command", "environment", "mount", "lifecycle", "image-id"} {
		t.Run(variant, func(t *testing.T) {
			manager, client, input, pod := workspaceStoragePodFixture(t)
			created := metav1.NewTime(time.Unix(1000, 0))
			deleted := metav1.NewTime(time.Unix(2000, 0))
			pod.CreationTimestamp = created
			pod.DeletionTimestamp = &deleted
			pod.ResourceVersion = "1"
			pod.Spec.NodeName = "synthetic-node"
			pod.Status.Phase = corev1.PodFailed
			waiting := func(name string) corev1.ContainerStatus {
				return corev1.ContainerStatus{Name: name, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "PodInitializing"}}}
			}
			for _, c := range pod.Spec.InitContainers {
				pod.Status.InitContainerStatuses = append(pod.Status.InitContainerStatuses, waiting(c.Name))
			}
			for _, c := range pod.Spec.Containers {
				pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, waiting(c.Name))
			}
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: pod.Spec.NodeName, UID: "40000000-0000-4000-8000-000000000001", CreationTimestamp: metav1.NewTime(time.Unix(500, 0))}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}}
			switch variant {
			case "first-failed":
				pod.Status.InitContainerStatuses[0].State = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, FinishedAt: deleted}}
				pod.Status.InitContainerStatuses[0].ContainerID = "containerd://synthetic"
				pod.Status.InitContainerStatuses[0].ImageID = input.ImageReference
			case "command":
				pod.Spec.InitContainers[0].Command = []string{"/bin/sh"}
			case "environment":
				pod.Spec.InitContainers[0].Env = append(pod.Spec.InitContainers[0].Env, corev1.EnvVar{Name: "LD_PRELOAD", Value: "/untrusted"})
			case "mount":
				pod.Spec.InitContainers[0].VolumeMounts = append(pod.Spec.InitContainers[0].VolumeMounts, corev1.VolumeMount{Name: "workspace", MountPath: "/usr/local/bin"})
			case "lifecycle":
				pod.Spec.InitContainers[0].Lifecycle = &corev1.Lifecycle{PostStart: &corev1.LifecycleHandler{Exec: &corev1.ExecAction{Command: []string{"/bin/sh"}}}}
			case "image-id":
				pod.Status.InitContainerStatuses[0].State = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, FinishedAt: deleted}}
				pod.Status.InitContainerStatuses[0].ContainerID = "containerd://synthetic"
				pod.Status.InitContainerStatuses[0].ImageID = "sha256:foreign"
			case "active":
				pod.Status.ContainerStatuses[0].State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: created}}
			case "later-init":
				pod.Status.InitContainerStatuses[1].State = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, FinishedAt: deleted}}
				pod.Status.InitContainerStatuses[1].ContainerID = "containerd://later"
			case "missing-status":
				pod.Status.ContainerStatuses = pod.Status.ContainerStatuses[:1]
			case "node-replaced":
				node.CreationTimestamp = metav1.NewTime(time.Unix(1500, 0))
			case "node-unready":
				node.Status.Conditions[0].Status = corev1.ConditionUnknown
			case "foreign-pod":
				pod.Spec.InitContainers[0].Args = []string{"other"}
			case "old-start":
				pod.Status.ContainerStatuses[0].ContainerID = "containerd://old"
			case "node-lost":
				pod.Status.Reason = "NodeLost"
			case "restart":
				pod.Status.InitContainerStatuses[0].RestartCount = 1
			}
			if _, err := client.CoreV1().Nodes().Create(t.Context(), node, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := client.CoreV1().Pods(manager.config.RuntimeNamespace).Create(t.Context(), pod, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
			record, err := manager.ObserveWorkspaceUnboundStop(t.Context(), input)
			allowed := variant == "unstarted" || variant == "first-failed"
			if (err == nil) != allowed {
				t.Fatalf("unexpected stopped proof for %s", variant)
			}
			if allowed && (!record.Matches(input) || record.NodeUID != string(node.UID) || record.Execution.PodUID != string(pod.UID)) {
				t.Fatal("proof lost exact pins")
			}
			if allowed {
				foreign := record
				foreign.Execution.PodUID = "50000000-0000-4000-8000-000000000001"
				if manager.ReleaseWorkspaceUnboundStop(t.Context(), input, foreign) == nil {
					t.Fatal("foreign receipt released old Pod")
				}
				if err := manager.ReleaseWorkspaceUnboundStop(t.Context(), input, record); err != nil {
					t.Fatal("exact receipt did not release old finalizer", err)
				}
				released, err := client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(t.Context(), pod.Name, metav1.GetOptions{})
				if err != nil || slices.Contains(released.Finalizers, workspaceWriterFinalizer) {
					t.Fatal("retained finalizer was not released", err)
				}
				if err := manager.CleanupWorkspaceUnboundDependencies(t.Context(), input, record); err != nil {
					t.Fatal("released old resources were not cleanable", err)
				}
				pvcName, _ := runtimecontract.SessionPVCName(input.SessionRef)
				if _, err := client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Get(t.Context(), pvcName, metav1.GetOptions{}); err != nil {
					t.Fatal("cleanup removed prepared workspace PVC", err)
				}
				released.UID = "50000000-0000-4000-8000-000000000001"
				if _, err := client.CoreV1().Pods(manager.config.RuntimeNamespace).Update(t.Context(), released, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
				if manager.CleanupWorkspaceUnboundDependencies(t.Context(), input, record) == nil {
					t.Fatal("old receipt cleaned replacement Pod dependencies")
				}
			}
		})
	}
}
