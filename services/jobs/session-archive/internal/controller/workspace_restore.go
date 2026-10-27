package controller

import (
	"path"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/archive"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

// configureWorkspaceArchiveMain fixes the trusted role identity and its bounded resource share without recursive PVC ownership changes.
// It preserves immutable Job/input evidence until a separate successful owner acknowledgement.
// configureWorkspaceArchiveMain фиксирует trusted role identity и bounded resource share без recursive PVC ownership changes.
// Immutable Job/input evidence сохраняются до отдельного successful owner acknowledgement.
func configureWorkspaceArchiveMain(job *batchv1.Job) *corev1.PodSpec {
	job.Spec.TTLSecondsAfterFinished = nil
	pod := &job.Spec.Template.Spec
	pod.SecurityContext.FSGroup = nil
	pod.SecurityContext.SupplementalGroups = []int64{29000}
	main := &pod.Containers[0]
	main.SecurityContext = restricted(10001)
	main.SecurityContext.RunAsGroup = int64Ptr(29000)
	main.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: *quantity("90m"), corev1.ResourceMemory: *quantity("64Mi")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: *quantity("900m"), corev1.ResourceMemory: *quantity("224Mi")},
	}
	return pod
}

// workspaceWriterContainer gives a fixed helper only its explicitly selected data/task/IPC mounts and no storage or API credentials.
// workspaceWriterContainer передаёт fixed helper только explicit data/task/IPC mounts без storage/API credentials.
func (controller *Controller) workspaceWriterContainer(name, mode string, uid int64, mounts []corev1.VolumeMount) corev1.Container {
	container := corev1.Container{Name: name, Image: controller.config.WorkerImage,
		Command: []string{"/usr/local/bin/workspace-writer"}, Args: []string{mode}, SecurityContext: restricted(uid),
		Env:          []corev1.EnvVar{{Name: "SESSION_ARCHIVE_WORKER_TIMEOUT", Value: controller.config.WorkerTimeout.String()}},
		VolumeMounts: mounts,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: *quantity("10m"), corev1.ResourceMemory: *quantity("16Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: *quantity("100m"), corev1.ResourceMemory: *quantity("32Mi")},
		},
	}
	container.SecurityContext.RunAsGroup = int64Ptr(29000)
	return container
}

// configureWorkspaceRestore renders only an already validated frozen task's exact generation and separates init, writer and reconciliation privileges.
// This geometry does not enable controller admission: the complete owner restore lifecycle must be delivered before Execute permits RESTORE.
// configureWorkspaceRestore строит exact generation уже validated frozen task, разделяя init/writer/reconciliation privileges.
// Геометрия не включает controller admission: Execute допускает RESTORE только после полного owner restore lifecycle.
func (controller *Controller) configureWorkspaceRestore(job *batchv1.Job, task model.Task) {
	subpath, _ := task.Workspace.Target.GenerationSubPath()
	generation := path.Dir(subpath)
	reconcile := task.Workspace.ReconcileOnly
	dataSubpath := path.Join(generation, ".restore-tree")
	if reconcile {
		dataSubpath = subpath
	}
	pod := configureWorkspaceArchiveMain(job)
	for index := range pod.Volumes {
		if pod.Volumes[index].Name == "session" {
			pod.Volumes[index].PersistentVolumeClaim.ReadOnly = reconcile
		}
	}
	main := &pod.Containers[0]
	data := corev1.VolumeMount{Name: "session", MountPath: archive.WorkspaceRestoreRoot, SubPath: dataSubpath, ReadOnly: reconcile}
	for index := range main.VolumeMounts {
		if main.VolumeMounts[index].Name == "session" {
			main.VolumeMounts[index] = data
		}
	}
	main.VolumeMounts = append(main.VolumeMounts, corev1.VolumeMount{Name: "session", MountPath: archive.WorkspaceRestoreGenerationMount,
		SubPath: generation, ReadOnly: reconcile})
	pod.InitContainers = nil
	if reconcile {
		return
	}
	taskMount := corev1.VolumeMount{Name: "task", MountPath: "/var/run/config/kodex/session-archive", ReadOnly: true}
	socket := corev1.VolumeMount{Name: "tmp", MountPath: archive.WorkspaceWriterSocketDirectory, SubPath: "workspace-writer"}
	main.VolumeMounts = append(main.VolumeMounts, socket)
	initialize := controller.workspaceWriterContainer("generation-prepare", "generation", 10001, []corev1.VolumeMount{taskMount,
		{Name: "session", MountPath: runtimecontract.WorkspaceStorageRootMount}})
	initialize.Resources.Limits = corev1.ResourceList{corev1.ResourceCPU: *quantity("200m"), corev1.ResourceMemory: *quantity("128Mi")}
	prepare := controller.workspaceWriterContainer("writer-prepare", "prepare", 10001,
		[]corev1.VolumeMount{{Name: "tmp", MountPath: "/tmp"}})
	pod.InitContainers = []corev1.Container{initialize, prepare}
	pod.Containers = append(pod.Containers, controller.workspaceWriterContainer("workspace-writer", "write", 10002,
		[]corev1.VolumeMount{data, socket, taskMount}))
}

// confirmedWorkspaceWorkerResult requires exact result metadata and positive terminal observations of every expected physical container.
// A protocol EOF, successful worker or Job counter alone cannot prove that a provider or initializer has exited.
// confirmedWorkspaceWorkerResult требует exact result metadata и positive terminal observations всех expected physical containers.
// Одного protocol EOF, successful worker либо Job counter недостаточно для доказательства завершения provider/initializer.
func confirmedWorkspaceWorkerResult(task model.Task, pod *corev1.Pod, result model.Result) bool {
	if task.Workspace == nil || pod == nil || pod.Status.Phase != corev1.PodSucceeded ||
		!workspaceContainersJoined(pod.Spec.Containers, pod.Status.ContainerStatuses) ||
		!workspaceContainersJoined(pod.Spec.InitContainers, pod.Status.InitContainerStatuses) {
		return false
	}
	switch task.Kind {
	case "SNAPSHOT":
		_, err := result.WorkspaceCheckpoint(task)
		return err == nil
	case "RESTORE":
		return result.MatchesWorkspaceRestore(task)
	default:
		return false
	}
}

// workspaceContainersJoined rejects missing, duplicate, foreign, restarted, running and failed container observations.
// workspaceContainersJoined отклоняет missing/duplicate/foreign/restarted/running/failed container observations.
func workspaceContainersJoined(expected []corev1.Container, statuses []corev1.ContainerStatus) bool {
	if len(expected) != len(statuses) {
		return false
	}
	seen := make(map[string]bool, len(statuses))
	for _, status := range statuses {
		if seen[status.Name] || status.RestartCount != 0 || status.State.Terminated == nil ||
			status.State.Running != nil || status.State.Waiting != nil || status.State.Terminated.ExitCode != 0 {
			return false
		}
		seen[status.Name] = true
	}
	for _, container := range expected {
		if !seen[container.Name] {
			return false
		}
	}
	return true
}
