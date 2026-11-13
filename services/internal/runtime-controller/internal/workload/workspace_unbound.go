package workload

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// workspaceUnboundEvidence requires a retained deleting terminal Pod and complete never-started
// status for every consumer; only the first, non-writing initializer may have failed once.
// workspaceUnboundEvidence требует retained deleting terminal Pod и complete never-started status
// каждого consumer; единожды завершиться с ошибкой может лишь первый non-writing initializer.
func workspaceUnboundEvidence(pod *corev1.Pod) string {
	if pod == nil || pod.DeletionTimestamp == nil || pod.Status.Phase != corev1.PodFailed || pod.Status.Reason == "NodeLost" || pod.Status.Reason == "Shutdown" ||
		pod.Spec.RestartPolicy != corev1.RestartPolicyNever || pod.Spec.NodeName == "" || len(pod.Spec.InitContainers) != 2 ||
		len(pod.Status.InitContainerStatuses) != 2 || len(pod.Status.ContainerStatuses) != len(pod.Spec.Containers) {
		return ""
	}
	evidence := "ALL_UNSTARTED"
	for groupIndex, group := range []struct {
		spec   []corev1.Container
		status []corev1.ContainerStatus
	}{{pod.Spec.InitContainers, pod.Status.InitContainerStatuses}, {pod.Spec.Containers, pod.Status.ContainerStatuses}} {
		for index, container := range group.spec {
			matches := 0
			for _, state := range group.status {
				if state.Name != container.Name {
					continue
				}
				matches++
				if state.RestartCount != 0 || state.LastTerminationState.Terminated != nil || state.LastTerminationState.Running != nil || state.LastTerminationState.Waiting != nil {
					return ""
				}
				firstFailure := groupIndex == 0 && index == 0 && state.State.Terminated != nil && state.State.Running == nil && state.State.Waiting == nil && state.State.Terminated.ExitCode != 0 && state.ContainerID != "" && !state.State.Terminated.FinishedAt.IsZero()
				if firstFailure {
					evidence = "FIRST_INIT_FAILED"
					continue
				}
				if state.State.Waiting == nil || state.State.Running != nil || state.State.Terminated != nil || state.ContainerID != "" || state.Started != nil && *state.Started {
					return ""
				}
			}
			if matches != 1 {
				return ""
			}
		}
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Status == corev1.ConditionUnknown {
			return ""
		}
	}
	return evidence
}

// ObserveWorkspaceUnboundStop obtains positive before-BIND evidence from the exact retained Pod,
// its original ready node and sealed PVC. A node created after this Pod cannot inherit its proof.
// ObserveWorkspaceUnboundStop получает positive before-BIND evidence от exact retained Pod,
// его исходного ready node и sealed PVC. Узел, созданный после Pod, не наследует доказательство.
func (manager *Manager) ObserveWorkspaceUnboundStop(ctx context.Context, input runtimecontract.RunnerInput) (runtimecontract.WorkspaceUnboundStop, error) {
	var empty runtimecontract.WorkspaceUnboundStop
	denied := errors.New("runtime workspace unbound stop is unconfirmed")
	if input.WorkspaceStorage == nil || input.WorkspaceStorage.ValidateForInput(input) != nil || manager.validateImage(input) != nil {
		return empty, denied
	}
	pod, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, runtimecontract.RuntimeTurnPodName(input.LeaseRef), metav1.GetOptions{})
	if err != nil || !manager.workspaceStoragePodMatches(input, pod) || !slices.Contains(pod.Finalizers, workspaceWriterFinalizer) {
		return empty, denied
	}
	first := pod.Spec.InitContainers[0]
	expectedMounts := 4
	if manager.config.RPCProfile != runtimecontract.CallbackProfileTrustedCluster {
		expectedMounts = 6
	}
	if len(first.Command) != 0 || first.WorkingDir != "/" || len(first.EnvFrom) != 0 || len(first.Env) != 1 ||
		first.Env[0] != (corev1.EnvVar{Name: "KODEX_RUNTIME_REVISION_FILE", Value: "/var/run/config/kodex/runtime/runtime.json"}) ||
		first.Lifecycle != nil || first.StartupProbe != nil || first.LivenessProbe != nil || first.ReadinessProbe != nil || first.RestartPolicy != nil ||
		len(first.VolumeMounts) != expectedMounts || len(first.VolumeDevices) != 0 || first.Stdin || first.TTY || first.StdinOnce {
		return empty, denied
	}
	evidence := workspaceUnboundEvidence(pod)
	if evidence == "FIRST_INIT_FAILED" {
		for _, state := range pod.Status.InitContainerStatuses {
			if state.Name == first.Name && state.ImageID != input.ImageManifestDigest && !strings.HasSuffix(state.ImageID, "@"+input.ImageManifestDigest) {
				return empty, denied
			}
		}
	}
	if evidence == "" || pod.CreationTimestamp.IsZero() {
		return empty, denied
	}
	node, err := manager.client.CoreV1().Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{})
	if err != nil || !validInitializationPodUID(string(node.UID)) || node.CreationTimestamp.IsZero() || node.CreationTimestamp.After(pod.CreationTimestamp.Time) || node.DeletionTimestamp != nil {
		return empty, denied
	}
	ready := false
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
			ready = true
		}
	}
	if !ready {
		return empty, denied
	}
	pvcName, _ := runtimecontract.SessionPVCName(input.SessionRef)
	pvc, err := manager.client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil || input.WorkspaceStorage.VolumeReady == nil || string(pvc.UID) != input.WorkspaceStorage.VolumeReady.Preparation.PVCUID {
		return empty, denied
	}
	// The existing BIND observer checks exact PVC preparation, consumer exclusion and rereads.
	// Existing BIND observer rejects terminal Pods, so repeat its immutable volume checks here.
	// Existing BIND observer проверяет exact PVC preparation, consumer exclusion и rereads.
	// Terminal Pod он отклоняет, поэтому immutable volume checks повторяются здесь.
	labels, annotations, metadataErr := runtimecontract.SessionVolumeMetadata(input.OrganizationRef, input.ProjectRef, input.SessionRef)
	desired := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: pvcName, Namespace: manager.config.RuntimeNamespace, Labels: labels, Annotations: annotations}, Spec: corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: manager.pvcRequest}}}}
	if metadataErr != nil || !preparedWorkspacePVCMatches(input, pvc, desired, manager.config.StorageClass) {
		return empty, denied
	}
	consumers, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{Limit: 128})
	if err != nil || consumers.Continue != "" {
		return empty, denied
	}
	for _, other := range consumers.Items {
		if other.UID == pod.UID {
			continue
		}
		for _, volume := range other.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == pvcName && !workspaceAllContainersTerminated(&other) {
				return empty, denied
			}
		}
	}
	again, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil || again.UID != pod.UID || again.Spec.NodeName != node.Name || workspaceUnboundEvidence(again) != evidence || !slices.Contains(again.Finalizers, workspaceWriterFinalizer) || !manager.workspaceStoragePodMatches(input, again) {
		return empty, denied
	}
	nodeAgain, err := manager.client.CoreV1().Nodes().Get(ctx, node.Name, metav1.GetOptions{})
	ready = false
	if err == nil {
		for _, condition := range nodeAgain.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				ready = true
			}
		}
	}
	if err != nil || nodeAgain.UID != node.UID || nodeAgain.DeletionTimestamp != nil || !ready {
		return empty, denied
	}
	volumeAgain, err := manager.client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil || volumeAgain.UID != pvc.UID || !preparedWorkspacePVCMatches(input, volumeAgain, desired, manager.config.StorageClass) {
		return empty, denied
	}
	binding := input.WorkspaceStorage
	record := runtimecontract.WorkspaceUnboundStop{Schema: runtimecontract.WorkspaceUnboundStopSchema, Scope: binding.Scope, WorkspaceRef: binding.WorkspaceRef, Generation: binding.Generation,
		NodeName: node.Name, NodeUID: string(node.UID), Evidence: evidence, Execution: runtimecontract.WorkspaceWriterCommitment{RunRef: input.RunRef, NodeRef: input.NodeRef, TurnRef: input.TurnRef, RuntimeRevisionRef: input.RuntimeRevisionRef, RuntimeRevisionVersion: input.RuntimeRevisionVersion, RuntimeRevisionDigest: input.RuntimeRevisionDigest, ExecutionBindingDigest: input.ExecutionBindingDigest, InputDigest: input.InputDigest, LeaseRef: input.LeaseRef, LeaseGeneration: input.LeaseGeneration, Attempt: input.Attempt, PodUID: string(pod.UID), PVCUID: string(pvc.UID)}}
	if !record.Matches(input) {
		return empty, denied
	}
	return record, nil
}

// ReleaseWorkspaceUnboundStop removes only the retained finalizer after an exact durable owner
// receipt and fresh physical proof. It never removes PVCs, files or another attempt's dependencies.
// ReleaseWorkspaceUnboundStop удаляет лишь retained finalizer после exact durable owner receipt
// и fresh physical proof. PVC, files и dependencies другой attempt не удаляются.
func (manager *Manager) ReleaseWorkspaceUnboundStop(ctx context.Context, input runtimecontract.RunnerInput, record runtimecontract.WorkspaceUnboundStop) error {
	observed, err := manager.ObserveWorkspaceUnboundStop(ctx, input)
	if err != nil || observed != record {
		return errors.New("runtime workspace unbound stop receipt changed")
	}
	pod, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, runtimecontract.RuntimeTurnPodName(input.LeaseRef), metav1.GetOptions{})
	if err != nil || string(pod.UID) != record.Execution.PodUID {
		return errors.New("runtime workspace unbound Pod changed")
	}
	next := pod.DeepCopy()
	next.Finalizers = slices.DeleteFunc(next.Finalizers, func(value string) bool { return value == workspaceWriterFinalizer })
	if _, err = manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Update(ctx, next, metav1.UpdateOptions{}); err != nil {
		return errors.New("release runtime workspace unbound Pod")
	}
	return nil
}

// CleanupWorkspaceUnboundDependencies requires a durable exact stop receipt and absent/released
// old Pod. A replacement Pod or retained finalizer prevents removal of its supporting resources.
// CleanupWorkspaceUnboundDependencies требует durable exact stop receipt и absent/released
// old Pod. Replacement Pod либо retained finalizer запрещают удаление supporting resources.
func (manager *Manager) CleanupWorkspaceUnboundDependencies(ctx context.Context, input runtimecontract.RunnerInput, record runtimecontract.WorkspaceUnboundStop) error {
	if !record.Matches(input) {
		return errors.New("runtime workspace unbound receipt is invalid")
	}
	pod, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, runtimecontract.RuntimeTurnPodName(input.LeaseRef), metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return errors.New("runtime workspace unbound cleanup readback is unavailable")
	}
	if err == nil && (string(pod.UID) != record.Execution.PodUID || slices.Contains(pod.Finalizers, workspaceWriterFinalizer) || workspaceUnboundEvidence(pod) == "" || !manager.workspaceStoragePodMatches(input, pod)) {
		return errors.New("runtime workspace unbound Pod is not released")
	}
	return manager.cleanupReleasedWorkspaceDependencies(ctx, input)
}
