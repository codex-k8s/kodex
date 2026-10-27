package controller

import (
	"context"
	"errors"
	"reflect"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/archive"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// RetainedAttempt carries exact Kubernetes identities, never a name-only deletion capability or owner authority.
// The application may release it only after a successful terminal owner RPC; transport uncertainty retains it.
// RetainedAttempt хранит точные Kubernetes identities, не name-only deletion capability или owner authority.
// Application освобождает его только после успешного terminal owner RPC; transport uncertainty сохраняет его.
type RetainedAttempt struct {
	controller *Controller
	input      *corev1.ConfigMap
	job        *batchv1.Job
	task       model.Task
}

// validWorkspaceWorkerPod checks the actual consumer geometry against the configured producer, allowing API defaults only outside authority fields.
// validWorkspaceWorkerPod сверяет actual consumer geometry с configured producer; API defaults допустимы только вне authority fields.
func (controller *Controller) validWorkspaceWorkerPod(task model.Task, job *batchv1.Job, pod *corev1.Pod) bool {
	expected := controller.job(job.Name, task, types.UID(task.Workspace.PVCUID)).Spec.Template.Spec
	actual := pod.Spec
	if actual.SecurityContext == nil || actual.SecurityContext.FSGroup != nil ||
		!reflect.DeepEqual(actual.SecurityContext.SupplementalGroups, expected.SecurityContext.SupplementalGroups) ||
		!reflect.DeepEqual(actual.SecurityContext.SeccompProfile, expected.SecurityContext.SeccompProfile) ||
		actual.HostNetwork || actual.HostPID || actual.HostIPC || actual.ShareProcessNamespace != nil && *actual.ShareProcessNamespace ||
		actual.ServiceAccountName != expected.ServiceAccountName || actual.AutomountServiceAccountToken == nil || *actual.AutomountServiceAccountToken ||
		actual.RestartPolicy != corev1.RestartPolicyNever || len(actual.EphemeralContainers) != 0 || len(actual.Volumes) != len(expected.Volumes) {
		return false
	}
	for index, wanted := range expected.Volumes {
		observed := actual.Volumes[index].DeepCopy()
		// API defaultMode не меняет источник; все прочие поля и порядок остаются точными.
		if observed.ConfigMap != nil && observed.ConfigMap.DefaultMode != nil && *observed.ConfigMap.DefaultMode == 0o644 {
			observed.ConfigMap.DefaultMode = nil
		}
		if observed.Secret != nil && observed.Secret.DefaultMode != nil && *observed.Secret.DefaultMode == 0o644 {
			observed.Secret.DefaultMode = nil
		}
		if !reflect.DeepEqual(*observed, wanted) {
			return false
		}
	}
	for _, pair := range []struct{ actual, expected []corev1.Container }{{actual.Containers, expected.Containers}, {actual.InitContainers, expected.InitContainers}} {
		if len(pair.actual) != len(pair.expected) {
			return false
		}
		for index, wanted := range pair.expected {
			observed := pair.actual[index]
			if observed.Name != wanted.Name || observed.Image != wanted.Image || !reflect.DeepEqual(observed.Command, wanted.Command) ||
				!reflect.DeepEqual(observed.Args, wanted.Args) || !reflect.DeepEqual(observed.Env, wanted.Env) || len(observed.EnvFrom) != 0 ||
				!reflect.DeepEqual(observed.VolumeMounts, wanted.VolumeMounts) || len(observed.VolumeDevices) != 0 ||
				!reflect.DeepEqual(observed.SecurityContext, wanted.SecurityContext) || !reflect.DeepEqual(observed.Resources, wanted.Resources) ||
				observed.Lifecycle != nil || observed.Stdin || observed.TTY {
				return false
			}
		}
	}
	return true
}

// ExecuteRetained returns V2 resources separately from worker outcome so owner commit precedes cleanup.
// Unknown Create results do not manufacture an exact identity and are left for a later owner-fenced attempt.
// ExecuteRetained возвращает V2 resources отдельно от результата, чтобы owner commit предшествовал очистке.
// Unknown Create не выдумывает exact identity и остаётся для следующей owner-fenced attempt.
func (controller *Controller) ExecuteRetained(ctx context.Context, task model.Task, renew func(context.Context) error) (model.Result, *RetainedAttempt, error) {
	retained := &RetainedAttempt{task: task}
	result, err := controller.execute(ctx, task, renew, retained)
	if retained.input == nil {
		return result, nil, err
	}
	return result, retained, err
}

// ReleaseAfterAcknowledgement joins only this attempt's exact Job/Pods before deleting its immutable input.
// It does not delete PVCs or objects, renew completed authority, or reinterpret a cleanup failure as owner failure.
// ReleaseAfterAcknowledgement завершает только exact Job/Pods попытки до удаления её immutable input.
// PVC/objects не удаляются, завершённые полномочия не продлеваются, cleanup failure не меняет owner outcome.
func (attempt *RetainedAttempt) ReleaseAfterAcknowledgement(ctx context.Context) error {
	if attempt == nil {
		return nil
	}
	if attempt.controller == nil || attempt.input == nil {
		return errors.New("session workspace cleanup identity is missing")
	}
	cleanup, cancel := context.WithTimeout(ctx, archiveCleanupTimeout)
	defer cancel()
	if err := attempt.controller.cleanupAttempt(cleanup, attempt.input, attempt.job, nil); err != nil {
		return err
	}
	for previous := int32(1); previous < attempt.task.Attempt; previous++ {
		name := workloadName(attempt.task.TaskRef, attempt.task.ContentGeneration, previous)
		input, err := attempt.controller.client.CoreV1().ConfigMaps(exactWorkerNamespace).Get(cleanup, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil || !validPredecessorInput(attempt.task, previous, input) {
			return errors.New("session workspace retained input binding changed")
		}
		_, err = attempt.controller.client.BatchV1().Jobs(exactWorkerNamespace).Get(cleanup, name, metav1.GetOptions{})
		pods, podErr := attempt.controller.client.CoreV1().Pods(exactWorkerNamespace).List(cleanup, metav1.ListOptions{LabelSelector: "job-name=" + name})
		if !apierrors.IsNotFound(err) || podErr != nil || len(pods.Items) != 0 {
			return errors.New("session workspace predecessor cleanup is unconfirmed")
		}
		if err := attempt.controller.cleanupAttempt(cleanup, input, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// checkWorkspaceSource binds the exact frozen PVC and excludes every other Pod using it, including terminal Pods.
// A positive owner quiescence record remains required; absence alone grants no snapshot authority.
// checkWorkspaceSource связывает exact frozen PVC и исключает все остальные Pod-потребители, включая terminal Pods.
// Positive owner quiescence record остаётся обязательным; одно отсутствие не разрешает snapshot.
func (controller *Controller) checkWorkspaceSource(ctx context.Context, task model.Task, uid, workerUID types.UID) error {
	if task.Workspace == nil || task.Validate() != nil || task.Kind != "SNAPSHOT" || uid == "" || string(uid) != task.Workspace.PVCUID {
		return errors.New("session workspace source binding is invalid")
	}
	pvc, err := controller.client.CoreV1().PersistentVolumeClaims(exactWorkerNamespace).Get(ctx, task.PVCName, metav1.GetOptions{})
	if err != nil || pvc.UID != uid || pvc.DeletionTimestamp != nil {
		return errors.New("session workspace source binding changed")
	}
	continuation := ""
	for {
		pods, err := controller.client.CoreV1().Pods(exactWorkerNamespace).List(ctx, metav1.ListOptions{Limit: 100, Continue: continuation})
		if err != nil {
			return errors.New("session workspace consumer observation failed")
		}
		for _, pod := range pods.Items {
			for _, volume := range pod.Spec.Volumes {
				if volume.PersistentVolumeClaim == nil || volume.PersistentVolumeClaim.ClaimName != task.PVCName {
					continue
				}
				owner := metav1.GetControllerOf(&pod)
				if workerUID == "" || owner == nil || owner.UID != workerUID || owner.Kind != "Job" || owner.APIVersion != "batch/v1" ||
					owner.Name != workloadName(task.TaskRef, task.ContentGeneration, task.Attempt) {
					return errors.New("session workspace source has another consumer")
				}
			}
		}
		if pods.Continue == "" {
			return nil
		}
		if pods.Continue == continuation {
			return errors.New("session workspace consumer cursor repeated")
		}
		continuation = pods.Continue
	}
}

// configureWorkspaceSnapshot restricts both readers to one read-only generation and isolates storage credentials.
// No FSGroup is assigned: recursive kubelet ownership changes must never alter archived mode0600 source files.
// configureWorkspaceSnapshot ограничивает readers одной read-only generation и изолирует storage credentials.
// FSGroup не назначается: recursive kubelet ownership changes не должны менять mode0600 исходников архива.
func (controller *Controller) configureWorkspaceSnapshot(job *batchv1.Job, task model.Task) {
	subpath, err := task.Workspace.Target.GenerationSubPath()
	if err != nil || task.Kind != "SNAPSHOT" {
		return
	}
	job.Spec.TTLSecondsAfterFinished = nil
	pod := &job.Spec.Template.Spec
	pod.SecurityContext.FSGroup = nil
	pod.SecurityContext.SupplementalGroups = []int64{29000}
	for index := range pod.Volumes {
		if pod.Volumes[index].Name == "session" {
			pod.Volumes[index].PersistentVolumeClaim.ReadOnly = true
		}
	}
	main := &pod.Containers[0]
	main.SecurityContext = restricted(10001)
	main.SecurityContext.RunAsGroup = int64Ptr(29000)
	main.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: *quantity("90m"), corev1.ResourceMemory: *quantity("64Mi")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: *quantity("900m"), corev1.ResourceMemory: *quantity("224Mi")},
	}
	workMount := corev1.VolumeMount{Name: "session", MountPath: archive.WorkspaceSnapshotRoot, SubPath: subpath, ReadOnly: true}
	socketMount := corev1.VolumeMount{Name: "tmp", MountPath: archive.WorkspaceReaderSocketDirectory, SubPath: "workspace-reader"}
	for index := range main.VolumeMounts {
		if main.VolumeMounts[index].Name == "session" {
			main.VolumeMounts[index] = workMount
		}
	}
	main.VolumeMounts = append(main.VolumeMounts, socketMount)
	prepare := corev1.Container{Name: "reader-prepare", Image: controller.config.WorkerImage,
		Command: []string{"/usr/local/bin/workspace-reader"}, Args: []string{"prepare"}, SecurityContext: restricted(10001),
		VolumeMounts: []corev1.VolumeMount{{Name: "tmp", MountPath: "/tmp"}},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: *quantity("10m"), corev1.ResourceMemory: *quantity("16Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: *quantity("100m"), corev1.ResourceMemory: *quantity("32Mi")},
		},
	}
	prepare.SecurityContext.RunAsGroup = int64Ptr(29000)
	pod.InitContainers = []corev1.Container{prepare}
	reader := corev1.Container{Name: "workspace-reader", Image: controller.config.WorkerImage,
		Command: []string{"/usr/local/bin/workspace-reader"}, Args: []string{"read"}, SecurityContext: restricted(10002),
		Env:          []corev1.EnvVar{{Name: "SESSION_ARCHIVE_WORKER_TIMEOUT", Value: controller.config.WorkerTimeout.String()}},
		VolumeMounts: []corev1.VolumeMount{workMount, socketMount, {Name: "task", MountPath: "/var/run/config/kodex/session-archive", ReadOnly: true}},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: *quantity("10m"), corev1.ResourceMemory: *quantity("16Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: *quantity("100m"), corev1.ResourceMemory: *quantity("32Mi")},
		},
	}
	reader.SecurityContext.RunAsGroup = int64Ptr(29000)
	pod.Containers = append(pod.Containers, reader)
}
