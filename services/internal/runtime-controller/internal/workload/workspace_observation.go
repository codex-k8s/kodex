package workload

import (
	"context"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkspacePodObservation carries only identities observed by the controller, never caller-supplied pins.
// Shape validation is not authority: the owner still performs the exact current writer CAS.
// WorkspacePodObservation содержит только controller-observed identities, не caller-supplied pins.
// Shape validation не является authority: owner отдельно выполняет exact current writer CAS.
type WorkspacePodObservation struct {
	PodUID string
	PVCUID string
}

// ObserveWorkspaceBind proves current owned Pod/PVC storage mounts before any main writer starts.
// RWO alone is insufficient: other live PVC consumers, missing pages or changed identities close admission.
// ObserveWorkspaceBind доказывает current owned Pod/PVC mounts до старта main writer.
// RWO недостаточно: другой live PVC consumer, missing page или changed identity закрывают admission.
func (manager *Manager) ObserveWorkspaceBind(ctx context.Context, input runtimecontract.RunnerInput) (WorkspacePodObservation, error) {
	var empty WorkspacePodObservation
	denied := errors.New("runtime workspace owned storage observation is unavailable")
	if input.WorkspaceStorage == nil || input.WorkspaceStorage.ValidateForInput(input) != nil || manager.validateImage(input) != nil {
		return empty, denied
	}
	name := runtimecontract.RuntimeTurnPodName(input.LeaseRef)
	pod, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || !manager.workspaceStoragePodMatches(input, pod) || pod.DeletionTimestamp != nil || !workspaceMainWritersUnstarted(pod) {
		return empty, denied
	}
	pvcName, err := runtimecontract.SessionPVCName(input.SessionRef)
	if err != nil {
		return empty, denied
	}
	pvc, err := manager.client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Get(ctx, pvcName, metav1.GetOptions{})
	labels, annotations, metadataErr := runtimecontract.SessionVolumeMetadata(input.OrganizationRef, input.ProjectRef, input.SessionRef)
	desired := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: pvcName, Namespace: manager.config.RuntimeNamespace, Labels: labels, Annotations: annotations},
		Spec: corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: manager.pvcRequest}}}}
	if err != nil || metadataErr != nil || !validInitializationPodUID(string(pvc.UID)) ||
		pvc.Name != pvcName || pvc.Namespace != manager.config.RuntimeNamespace || !sessionPVCMatches(pvc, desired, manager.config.StorageClass) {
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
	again, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || again.UID != pod.UID || again.DeletionTimestamp != nil || !manager.workspaceStoragePodMatches(input, again) || !workspaceMainWritersUnstarted(again) {
		return empty, denied
	}
	volumeAgain, err := manager.client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil || volumeAgain.UID != pvc.UID || !sessionPVCMatches(volumeAgain, desired, manager.config.StorageClass) {
		return empty, denied
	}
	return WorkspacePodObservation{PodUID: string(pod.UID), PVCUID: string(pvc.UID)}, nil
}

// workspaceStoragePodMatches checks immutable storage/identity boundaries without trusting phase or labels alone.
// Unrelated provider credential validation remains in the existing guarded Pod materialization path.
// workspaceStoragePodMatches проверяет immutable storage/identity boundary, не доверяя phase или labels отдельно.
// Provider credentials отдельно проверяет существующий guarded Pod materialization path.
func (manager *Manager) workspaceStoragePodMatches(input runtimecontract.RunnerInput, pod *corev1.Pod) bool {
	if pod == nil || !validInitializationPodUID(string(pod.UID)) || pod.Name != runtimecontract.RuntimeTurnPodName(input.LeaseRef) ||
		pod.Namespace != manager.config.RuntimeNamespace || pod.Spec.ServiceAccountName != input.EffectiveKubernetesAccess.ServiceAccountName ||
		pod.Spec.HostNetwork || pod.Spec.HostPID || pod.Spec.HostIPC || pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken ||
		len(pod.Spec.InitContainers) != 2 || len(pod.Spec.Containers) != 3 {
		return false
	}
	expected := metav1.ObjectMeta{Labels: map[string]string{managedLabel: "true", modeLabel: "turn", "app.kubernetes.io/name": "agent-runner", "app.kubernetes.io/component": "role-runtime", "kodex.dev/environment": manager.config.Environment, executionHashLabel: shortHash(input.LeaseRef)}, Annotations: runtimeProjectionAnnotations(input, pod.Name)}
	expected.Annotations[leaseAnnotation] = input.LeaseRef
	if manager.config.RPCProfile == runtimecontract.CallbackProfileTrustedCluster {
		expected.Labels["kodex.dev/security-profile"] = runtimecontract.CallbackProfileTrustedCluster
	}
	if !managedMetadataMatches(pod.ObjectMeta, expected) {
		return false
	}
	template := &corev1.Pod{Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: "workspace-prepare"}, {Name: "workspace-init"}}, Containers: []corev1.Container{{Name: "role-runtime"}, {Name: "provider-runtime"}, {Name: "provider-credential-relay"}}}}
	for _, container := range []*corev1.Container{&template.Spec.InitContainers[1], &template.Spec.Containers[0], &template.Spec.Containers[1]} {
		container.VolumeMounts = []corev1.VolumeMount{{Name: "session", MountPath: "/workspace/.kodex/state"}}
	}
	template.Spec.InitContainers[1].VolumeMounts = append(template.Spec.InitContainers[1].VolumeMounts,
		corev1.VolumeMount{Name: "runtime-input", MountPath: "/var/run/config/kodex/runtime", ReadOnly: true},
		corev1.VolumeMount{Name: "runtime-ticket", MountPath: "/var/run/secrets/kodex/runtime/ticket/token", SubPath: ticketKey, ReadOnly: true})
	if manager.config.RPCProfile != runtimecontract.CallbackProfileTrustedCluster {
		template.Spec.InitContainers[1].VolumeMounts = append(template.Spec.InitContainers[1].VolumeMounts,
			corev1.VolumeMount{Name: "callback-ca", MountPath: "/var/run/config/kodex/runtime/callback", ReadOnly: true},
			corev1.VolumeMount{Name: "callback-client", MountPath: "/var/run/secrets/kodex/runtime/callback-client", ReadOnly: true})
	}
	if !configureWorkspaceStoragePod(template, input) {
		return false
	}
	for index, container := range pod.Spec.InitContainers {
		if container.Name != template.Spec.InitContainers[index].Name || container.Image != input.ImageReference ||
			!apiequality.Semantic.DeepEqual(container.SecurityContext, restrictedSecurityContext(10001)) || len(container.Args) != 1 ||
			(index == 0 && container.Args[0] != "runtime-bind-workspace") || (index == 1 && container.Args[0] != "runtime-init-workspace") ||
			!workspaceStorageMountsMatch(container.VolumeMounts, template.Spec.InitContainers[index].VolumeMounts) {
			return false
		}
	}
	for index, container := range pod.Spec.Containers {
		if container.Name != template.Spec.Containers[index].Name || container.Image != input.ImageReference && container.Name != "provider-credential-relay" ||
			!workspaceStorageMountsMatch(container.VolumeMounts, template.Spec.Containers[index].VolumeMounts) {
			return false
		}
		expectedSecurity := restrictedSecurityContext(10001)
		if container.Name == "provider-runtime" {
			expectedSecurity = providerSandboxSecurityContext(10002, manager.config.ProviderAppArmorProfile)
		} else if container.Name == "provider-credential-relay" {
			expectedSecurity = restrictedSecurityContext(10003)
		}
		if !apiequality.Semantic.DeepEqual(container.SecurityContext, expectedSecurity) {
			return false
		}
	}
	pvcName, err := runtimecontract.SessionPVCName(input.SessionRef)
	if err != nil {
		return false
	}
	seen, rootSeen := 0, 0
	for _, volume := range pod.Spec.Volumes {
		if volume.Name == "workspace" {
			rootSeen++
			if volume.EmptyDir == nil || volume.HostPath != nil || volume.PersistentVolumeClaim != nil {
				return false
			}
		}
		if volume.Name == "session" {
			seen++
			if !apiequality.Semantic.DeepEqual(volume.VolumeSource, corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvcName}}) {
				return false
			}
		}
	}
	return seen == 1 && rootSeen == 1
}

// workspaceStorageMountsMatch requires every assigned mount once and denies alternate PVC/private-work paths.
// workspaceStorageMountsMatch требует каждый assigned mount ровно один раз и запрещает alternate PVC/private-work paths.
func workspaceStorageMountsMatch(actual, required []corev1.VolumeMount) bool {
	for _, expected := range required {
		count := 0
		for _, mount := range actual {
			if mount == expected {
				count++
			}
		}
		if count != 1 {
			return false
		}
	}
	for _, mount := range actual {
		if mount.Name == "session" || mount.MountPath == runtimecontract.WorkspaceStorageRootMount ||
			mount.MountPath == runtimecontract.RuntimeWorkspaceWorkingRoot || strings.HasPrefix(mount.MountPath, runtimecontract.RuntimeWorkspaceWorkingRoot+"/") ||
			strings.HasPrefix(mount.MountPath, "/var/run/config/kodex/workspace") {
			allowed := false
			for _, expected := range required {
				allowed = allowed || mount == expected
			}
			if !allowed {
				return false
			}
		}
	}
	return true
}

// workspaceMainWritersUnstarted forbids BIND after any model/role container has started or terminated.
// workspaceMainWritersUnstarted запрещает BIND после старта либо завершения model/role container.
func workspaceMainWritersUnstarted(pod *corev1.Pod) bool {
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Running != nil || status.State.Terminated != nil || status.LastTerminationState.Terminated != nil || status.RestartCount != 0 {
			return false
		}
	}
	return true
}

// workspaceAllContainersTerminated requires exact positive terminal status for every declared writer container.
// Missing status, waiting containers and phase-only success cannot prove physical quiescence.
// workspaceAllContainersTerminated требует exact positive terminal status каждого declared writer container.
// Missing status, waiting container и одна phase success не доказывают physical quiescence.
func workspaceAllContainersTerminated(pod *corev1.Pod) bool {
	for _, group := range []struct {
		specs    []corev1.Container
		statuses []corev1.ContainerStatus
	}{
		{pod.Spec.InitContainers, pod.Status.InitContainerStatuses}, {pod.Spec.Containers, pod.Status.ContainerStatuses},
	} {
		if len(group.specs) == 0 || len(group.specs) != len(group.statuses) {
			return false
		}
		for _, container := range group.specs {
			count := 0
			for _, status := range group.statuses {
				if status.Name == container.Name && status.State.Terminated != nil && status.ContainerID != "" && status.RestartCount == 0 {
					count++
				}
			}
			if count != 1 {
				return false
			}
		}
	}
	return true
}
