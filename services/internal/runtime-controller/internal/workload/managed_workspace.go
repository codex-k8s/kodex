package workload

import (
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// workspaceExecutorName identifies the only optional secret-free command container.
	// workspaceExecutorName обозначает единственный optional command container без секретов.
	workspaceExecutorName = "workspace-executor"
	// workspaceExecutorSocketVolume is shared only by role and executor, never provider or relay.
	// workspaceExecutorSocketVolume разделяют лишь role и executor, не provider либо relay.
	workspaceExecutorSocketVolume = "workspace-executor-socket"
	// workspaceExecutorTmpVolume holds only disposable command caches under the declared budget.
	// workspaceExecutorTmpVolume хранит лишь disposable command caches в заявленном budget.
	workspaceExecutorTmpVolume = "workspace-executor-tmp"
)

// splitManagedWorkspaceResources partitions the existing environment's total provider/command
// budget. Requests and limits remain additive-exact, so the owner queue does not undercount a
// newly added full-size container or silently double the published resource envelope.
// splitManagedWorkspaceResources делит существующий общий provider/command budget окружения.
// Сумма requests и limits точна: owner queue не пропускает новый полноразмерный container и
// опубликованный resource envelope не удваивается молча.
func splitManagedWorkspaceResources(policy runtimecontract.RuntimeResourcePolicy) (corev1.ResourceRequirements, corev1.ResourceRequirements) {
	total := runtimePolicyResourceRequirements(policy)
	provider := corev1.ResourceRequirements{Requests: corev1.ResourceList{}, Limits: corev1.ResourceList{}}
	executor := corev1.ResourceRequirements{Requests: corev1.ResourceList{}, Limits: corev1.ResourceList{}}
	for _, pair := range []struct{ source, provider, executor corev1.ResourceList }{{total.Requests, provider.Requests, executor.Requests}, {total.Limits, provider.Limits, executor.Limits}} {
		for name, value := range pair.source {
			if name == corev1.ResourceCPU {
				n := value.MilliValue()
				pair.executor[name] = *resource.NewMilliQuantity(n/2, resource.DecimalSI)
				pair.provider[name] = *resource.NewMilliQuantity(n-n/2, resource.DecimalSI)
			} else {
				n := value.Value()
				pair.executor[name] = *resource.NewQuantity(n/2, resource.BinarySI)
				pair.provider[name] = *resource.NewQuantity(n-n/2, resource.BinarySI)
			}
		}
	}
	return provider, executor
}

// workspaceExecutorContainer constructs the closed command sandbox without credentials, runtime
// input, provider state, VFS inputs, callbacks or Kubernetes token mounts. UID reuse with provider
// is safe only with their separate PID namespaces and disjoint writable mounts.
// workspaceExecutorContainer строит закрытый command sandbox без credentials, runtime input,
// provider state, VFS inputs, callback и Kubernetes token mounts. Общий UID с provider допустим
// лишь при отдельных PID namespaces и непересекающихся writable mounts.
func workspaceExecutorContainer(input runtimecontract.RunnerInput) (corev1.Container, bool) {
	work := corev1.VolumeMount{Name: "workspace", MountPath: runtimecontract.RuntimeWorkspaceWorkingRoot, SubPath: "work"}
	if input.WorkspaceStorage != nil {
		tree, err := input.WorkspaceStorage.GenerationSubPath()
		if err != nil {
			return corev1.Container{}, false
		}
		work.Name = "session"
		work.SubPath = tree
	}
	_, resources := splitManagedWorkspaceResources(input.EnvironmentPolicy.Resources)
	security := restrictedSecurityContext(10002)
	security.RunAsGroup = int64Pointer(29000)
	// This dedicated daemon owns PID-1 reaping itself; the normal image init would make it a child.
	// Этот отдельный daemon сам собирает потомков как PID1; штатный init образа сделал бы его child.
	return corev1.Container{Name: workspaceExecutorName, Image: input.ImageReference, ImagePullPolicy: corev1.PullIfNotPresent,
		Command: []string{"/usr/local/bin/kodex-agent-runner"}, Args: []string{runtimecontract.WorkspaceExecutorMode}, WorkingDir: runtimecontract.RuntimeWorkspaceWorkingRoot,
		Env: []corev1.EnvVar{{Name: "HOME", Value: "/tmp"}, {Name: "OTEL_SDK_DISABLED", Value: "true"}}, SecurityContext: security,
		VolumeMounts: []corev1.VolumeMount{work, {Name: workspaceExecutorSocketVolume, MountPath: "/run/kodex/workspace"}, {Name: workspaceExecutorTmpVolume, MountPath: "/tmp"}}, Resources: resources,
		ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"/usr/bin/test", "-S", runtimecontract.WorkspaceExecutorSocket}}}, PeriodSeconds: 2, TimeoutSeconds: 1, FailureThreshold: 30}}, true
}

// configureManagedWorkspacePod adds the optional command container only for a qualified ordinary
// OpenCode turn, keeps provider work read-only, and leaves all legacy profiles unchanged.
// configureManagedWorkspacePod добавляет optional command container лишь для qualified ordinary
// OpenCode turn, сохраняет provider work read-only и не меняет legacy profiles.
func configureManagedWorkspacePod(pod *corev1.Pod, input runtimecontract.RunnerInput) bool {
	if !input.EnvironmentPolicy.ManagedWorkspace {
		return true
	}
	if pod == nil || !runtimecontract.RuntimeWorkspaceToolsAvailable(input) || len(pod.Spec.Containers) != 3 {
		return false
	}
	pod.Spec.ShareProcessNamespace = boolPointer(false)
	for index := range pod.Spec.Containers {
		container := &pod.Spec.Containers[index]
		if container.Name == "provider-runtime" {
			for i := range container.VolumeMounts {
				mount := &container.VolumeMounts[i]
				if mount.MountPath == "/workspace" || mount.MountPath == runtimecontract.RuntimeWorkspaceWorkingRoot {
					mount.ReadOnly = true
				}
			}
			if len(input.EnvironmentTools) > 0 {
				container.Resources, _ = splitManagedWorkspaceResources(input.EnvironmentPolicy.Resources)
			}
		}
	}
	if len(input.EnvironmentTools) == 0 {
		return true
	}
	executor, ok := workspaceExecutorContainer(input)
	if !ok {
		return false
	}
	pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: workspaceExecutorSocketVolume, MountPath: "/run/kodex/workspace"})
	pod.Spec.Containers = append(pod.Spec.Containers, executor)
	pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{Name: workspaceExecutorSocketVolume, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("8Mi"))}}},
		corev1.Volume{Name: workspaceExecutorTmpVolume, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("512Mi"))}}})
	return true
}

// managedWorkspacePodMatches validates the exact executor authority boundary after known API
// default normalization. A missing/writable provider work mount or any injected container env,
// secret mount, host/shared PID, capabilities or command endpoint closes admission.
// managedWorkspacePodMatches проверяет exact executor authority boundary с учётом известных API
// defaults. Missing/writable provider work mount, injected env/secret mount, host/shared PID,
// capabilities либо command endpoint закрывают admission.
func managedWorkspacePodMatches(pod *corev1.Pod, input runtimecontract.RunnerInput) bool {
	if !input.EnvironmentPolicy.ManagedWorkspace {
		return true
	}
	if pod == nil || pod.Spec.HostPID || pod.Spec.ShareProcessNamespace == nil || *pod.Spec.ShareProcessNamespace {
		return false
	}
	providerRoot := false
	for _, container := range pod.Spec.Containers {
		if container.Name == "provider-runtime" {
			for _, mount := range container.VolumeMounts {
				if mount.MountPath == "/workspace" {
					providerRoot = mount.ReadOnly
				}
				if mount.MountPath == runtimecontract.RuntimeWorkspaceWorkingRoot && !mount.ReadOnly {
					return false
				}
			}
		}
	}
	if !providerRoot {
		return false
	}
	if len(input.EnvironmentTools) == 0 {
		return len(pod.Spec.Containers) == 3
	}
	if len(pod.Spec.Containers) != 4 {
		return false
	}
	expected, ok := workspaceExecutorContainer(input)
	if !ok {
		return false
	}
	return apiequality.Semantic.DeepEqual(canonicalRuntimeContainers([]corev1.Container{pod.Spec.Containers[3]}), canonicalRuntimeContainers([]corev1.Container{expected}))
}

// workspaceContainerCount binds the optional container count to the immutable execution profile.
// workspaceContainerCount связывает optional container count с immutable execution profile.
func workspaceContainerCount(input runtimecontract.RunnerInput) int {
	if runtimecontract.RuntimeWorkspaceToolsAvailable(input) && len(input.EnvironmentTools) > 0 {
		return 4
	}
	return 3
}
