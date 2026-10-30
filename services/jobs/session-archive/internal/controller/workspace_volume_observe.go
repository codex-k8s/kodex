package controller

import (
	"context"
	"errors"
	"reflect"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// errWorkspaceVolumePreparePending distinguishes a still-incomplete positive incarnation from
// malformed or replaced evidence; callers may only poll the same already confirmed Job UID.
// errWorkspaceVolumePreparePending отличает незавершённую positive incarnation от malformed или
// replaced evidence; caller может опрашивать только тот же подтверждённый Job UID.
var errWorkspaceVolumePreparePending = errors.New("workspace volume preparation is pending")

// observeWorkspaceVolumePreparation joins the exact positive input/Job/PVC incarnations and
// every declared container before decoding a bounded sealed record. Job phase, a reused name or
// one successful main container alone never establishes readiness. It performs no adoption,
// cleanup, grant renewal or owner transition; the caller retains the returned Pod UID with the fact.
// observeWorkspaceVolumePreparation соединяет exact positive input/Job/PVC incarnations и все
// объявленные containers до decoding bounded sealed record. Job phase, reused name либо один
// успешный main container не доказывают readiness. Проверка не делает adoption, cleanup, grant
// renewal или owner transition; caller сохраняет возвращённый Pod UID вместе с фактом.
func (controller *Controller) observeWorkspaceVolumePreparation(ctx context.Context, work runtimecontract.WorkspaceVolumeWork, input *corev1.ConfigMap, created *batchv1.Job) (*runtimecontract.WorkspaceVolumeReady, string, error) {
	expected, err := controller.workspaceVolumePreparationJob(work, input)
	if err != nil || ctx == nil || created == nil || created.UID == "" || created.ResourceVersion == "" ||
		created.Namespace != exactWorkerNamespace || created.Name != expected.Name {
		return nil, "", errWorkspaceVolumePrepareUnconfirmed
	}
	currentInput, err := controller.client.CoreV1().ConfigMaps(exactWorkerNamespace).Get(ctx, input.Name, metav1.GetOptions{})
	if err != nil || currentInput == nil || currentInput.UID != input.UID {
		return nil, "", errWorkspaceVolumePrepareUnconfirmed
	}
	if _, err := controller.workspaceVolumePreparationJob(work, currentInput); err != nil {
		return nil, "", errWorkspaceVolumePrepareUnconfirmed
	}
	wantedPVC, err := controller.workspaceVolumePVC(work)
	if err != nil {
		return nil, "", errWorkspaceVolumePrepareUnconfirmed
	}
	pvc, err := controller.client.CoreV1().PersistentVolumeClaims(exactWorkerNamespace).Get(ctx, wantedPVC.Name, metav1.GetOptions{})
	if err != nil || !matchingWorkspaceVolumePVC(wantedPVC, pvc) || string(pvc.UID) != work.Volume.PVCUID {
		return nil, "", errWorkspaceVolumePrepareUnconfirmed
	}
	if pvc.Status.Phase != corev1.ClaimBound || pvc.Spec.VolumeName == "" {
		if pvc.Status.Phase == corev1.ClaimPending || pvc.Status.Phase == "" {
			return nil, "", errWorkspaceVolumePreparePending
		}
		return nil, "", errWorkspaceVolumePrepareUnconfirmed
	}
	job, err := controller.client.BatchV1().Jobs(exactWorkerNamespace).Get(ctx, created.Name, metav1.GetOptions{})
	if err != nil || job == nil || job.UID != created.UID || !matchingWorkspaceVolumeJob(expected, job) {
		return nil, "", errWorkspaceVolumePrepareUnconfirmed
	}
	pods, err := controller.client.CoreV1().Pods(exactWorkerNamespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + job.Name, Limit: 2})
	if err != nil || pods == nil || pods.Continue != "" || len(pods.Items) > 1 {
		return nil, "", errWorkspaceVolumePrepareUnconfirmed
	}
	if len(pods.Items) == 0 {
		return nil, "", errWorkspaceVolumePreparePending
	}
	pod := &pods.Items[0]
	if !matchingWorkspaceVolumePod(expected, job, pod) {
		return nil, "", errWorkspaceVolumePrepareUnconfirmed
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return nil, "", errWorkspaceVolumePrepareUnconfirmed
		}
	}
	if !joinedWorkspaceVolumePreparation(job, pod) {
		return nil, "", errWorkspaceVolumePreparePending
	}
	message := pod.Status.ContainerStatuses[0].State.Terminated.Message
	ready, err := runtimecontract.DecodeWorkspaceVolumeReady([]byte(message))
	if err != nil || runtimecontract.ValidateWorkspaceVolumeObservation(work, string(pvc.UID), string(job.UID), string(pod.UID), &ready) != nil {
		return nil, "", errWorkspaceVolumePrepareUnconfirmed
	}
	return &ready, string(pod.UID), nil
}

// matchingWorkspaceVolumeJob validates only immutable geometry and input pins; callers must
// independently prove the UID source and current grant before any physical action or owner ACK.
// matchingWorkspaceVolumeJob проверяет только immutable geometry и input pins; caller отдельно
// доказывает источник UID и current grant до physical action либо owner ACK.
func matchingWorkspaceVolumeJob(expected, job *batchv1.Job) bool {
	if expected == nil || job == nil || job.Namespace != exactWorkerNamespace || job.Name != expected.Name || job.UID == "" || job.ResourceVersion == "" || job.DeletionTimestamp != nil ||
		job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 ||
		job.Spec.Parallelism == nil || *job.Spec.Parallelism != 1 || job.Spec.Completions == nil || *job.Spec.Completions != 1 ||
		job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != *expected.Spec.ActiveDeadlineSeconds ||
		job.Spec.PodReplacementPolicy == nil || *job.Spec.PodReplacementPolicy != batchv1.Failed ||
		job.Spec.ManagedBy != nil && *job.Spec.ManagedBy != "kubernetes.io/job-controller" ||
		job.Spec.Suspend != nil && *job.Spec.Suspend || job.Spec.ManualSelector != nil && *job.Spec.ManualSelector ||
		job.Spec.TTLSecondsAfterFinished != nil ||
		!validWorkspaceHelperPod(expected.Spec.Template.Spec, job.Spec.Template.Spec) ||
		!reflect.DeepEqual(job.Spec.Template.Spec.SecurityContext.FSGroupChangePolicy, expected.Spec.Template.Spec.SecurityContext.FSGroupChangePolicy) {
		return false
	}
	for key, value := range expected.Annotations {
		if job.Annotations[key] != value || job.Spec.Template.Annotations[key] != value {
			return false
		}
	}
	return true
}

// matchingWorkspaceVolumePod checks a sole exact Job controller, input pins and the expected
// credential-free execution geometry without treating a label as physical ownership.
// matchingWorkspaceVolumePod проверяет sole exact Job controller, input pins и expected
// credential-free execution geometry, не объявляя label physical ownership.
func matchingWorkspaceVolumePod(expected, job *batchv1.Job, pod *corev1.Pod) bool {
	if expected == nil || job == nil || pod == nil {
		return false
	}
	owner := metav1.GetControllerOf(pod)
	if pod.Namespace != exactWorkerNamespace || pod.UID == "" || pod.ResourceVersion == "" || pod.DeletionTimestamp != nil || len(pod.OwnerReferences) != 1 ||
		owner == nil || owner.UID != job.UID || owner.Name != job.Name || owner.APIVersion != "batch/v1" || owner.Kind != "Job" ||
		!validWorkspaceHelperPod(expected.Spec.Template.Spec, pod.Spec) ||
		!reflect.DeepEqual(pod.Spec.SecurityContext.FSGroupChangePolicy, expected.Spec.Template.Spec.SecurityContext.FSGroupChangePolicy) {
		return false
	}
	for key, value := range expected.Annotations {
		if pod.Annotations[key] != value {
			return false
		}
	}
	return true
}

// joinedWorkspaceVolumePreparation requires the terminal Job and every exact declared
// container to have succeeded; incomplete or failed observations never prove a completed writer.
// joinedWorkspaceVolumePreparation требует terminal Job и успешного завершения каждого exact
// declared container; incomplete либо failed observations не доказывают завершение writer.
func joinedWorkspaceVolumePreparation(job *batchv1.Job, pod *corev1.Pod) bool {
	return completedWorkspaceVolumeJob(job) && pod != nil &&
		pod.Status.Phase == corev1.PodSucceeded && len(pod.Status.EphemeralContainerStatuses) == 0 &&
		workspaceContainersJoined(pod.Spec.Containers, pod.Status.ContainerStatuses) &&
		workspaceContainersJoined(pod.Spec.InitContainers, pod.Status.InitContainerStatuses)
}

// completedWorkspaceVolumeJob checks the retained terminal Job barrier independently of Pod
// retention; it never substitutes for a new attempt's positive all-container observation.
// completedWorkspaceVolumeJob проверяет retained terminal Job barrier независимо от сохранности
// Pod; проверка не заменяет positive all-container observation новой attempt.
func completedWorkspaceVolumeJob(job *batchv1.Job) bool {
	if job == nil || job.Status.Succeeded != 1 || job.Status.Active != 0 || job.Status.Failed != 0 {
		return false
	}
	completed := false
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return false
		}
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			completed = true
		}
	}
	return completed
}
