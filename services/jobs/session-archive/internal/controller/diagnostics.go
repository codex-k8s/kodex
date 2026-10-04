package controller

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	workerObservationMessage = "session archive worker terminal observation"
	taskRefAnnotation        = "session-archive.kodex.dev/task-ref"
	taskKindAnnotation       = "session-archive.kodex.dev/task-kind"
	taskGenerationAnnotation = "session-archive.kodex.dev/content-generation"
	taskAttemptAnnotation    = "session-archive.kodex.dev/attempt"
)

var diagnosticRefPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

func workerDiagnosticAnnotations(task model.Task, uid types.UID) map[string]string {
	result := pvcBindingAnnotation(uid)
	if result == nil {
		result = make(map[string]string)
	}
	result[taskRefAnnotation] = task.TaskRef
	result[taskKindAnnotation] = task.Kind
	result[taskGenerationAnnotation] = strconv.FormatInt(task.ContentGeneration, 10)
	result[taskAttemptAnnotation] = strconv.FormatInt(int64(task.Attempt), 10)
	return result
}

// Metadata связывает результат с уже проверенным owner claim, не создаёт authority.
func validWorkerJob(task model.Task, job *batchv1.Job, uid types.UID) bool {
	if (task.Kind == "SNAPSHOT" || task.Kind == "RESTORE") && uid == "" {
		return false
	}
	if job == nil || task.Validate() != nil || job.UID == "" || job.Name != workloadName(task.TaskRef, task.ContentGeneration, task.Attempt) || job.Namespace != exactWorkerNamespace || job.Labels[managedLabel] != "true" {
		return false
	}
	for key, value := range workerDiagnosticAnnotations(task, uid) {
		if job.Annotations[key] != value {
			return false
		}
	}
	return true
}

func validWorkerPod(task model.Task, job *batchv1.Job, pod *corev1.Pod) bool {
	if pod.Namespace != exactWorkerNamespace || pod.UID == "" || pod.Labels["job-name"] != job.Name || len(pod.OwnerReferences) != 1 {
		return false
	}
	owner := pod.OwnerReferences[0]
	if owner.Kind != "Job" || owner.APIVersion != "batch/v1" || owner.Name != job.Name || owner.UID != job.UID || owner.Controller == nil || !*owner.Controller {
		return false
	}
	if task.Kind == "SNAPSHOT" || task.Kind == "RESTORE" {
		found := false
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil {
				if found || volume.PersistentVolumeClaim.ClaimName != task.PVCName {
					return false
				}
				found = true
			}
		}
		return found
	}
	return true
}

func (controller *Controller) readResult(ctx context.Context, task model.Task, job *batchv1.Job, uid types.UID) (model.Result, error) {
	stage, reason, code, exit := "JOB_BINDING_INVALID", "UNKNOWN", "UNKNOWN", int32(-1)
	podRef := "UNKNOWN"
	jobRef := "UNKNOWN"
	if job != nil {
		jobRef = job.Name
	}
	defer func() {
		if controller.config.Logger == nil {
			return
		}
		ref := func(value string) string {
			if diagnosticRefPattern.MatchString(value) {
				return value
			}
			return "UNKNOWN"
		}
		controller.config.Logger.InfoContext(ctx, workerObservationMessage,
			"task_ref", ref(task.TaskRef), "session_ref", ref(task.SessionRef), "task_kind", diagnosticTaskKind(task.Kind),
			"content_generation", task.ContentGeneration, "attempt", task.Attempt, "job_ref", ref(jobRef), "pod_ref", ref(podRef),
			"stage", stage, "termination_reason", reason, "exit_code", exit, "safe_error_code", code)
	}()
	if !validWorkerJob(task, job, uid) {
		return model.Result{}, errors.New("session archive worker job binding is invalid")
	}
	pods, err := controller.client.CoreV1().Pods(exactWorkerNamespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + job.Name})
	stage = "POD_LOOKUP_FAILED"
	if err != nil || len(pods.Items) != 1 {
		return model.Result{}, errors.New("read session archive worker pod")
	}
	pod := &pods.Items[0]
	stage = "POD_BINDING_INVALID"
	if !validWorkerPod(task, job, pod) {
		return model.Result{}, errors.New("session archive worker pod binding is invalid")
	}
	podRef = pod.Name
	stage = "RESULT_MISSING"
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name != "worker" || status.State.Terminated == nil {
			continue
		}
		termination := status.State.Terminated
		reason = diagnosticTerminationReason(termination.Reason)
		if termination.ExitCode >= 0 && termination.ExitCode <= 255 {
			exit = termination.ExitCode
		}
		stage = "RESULT_DECODE_INVALID"
		var result model.Result
		if len(termination.Message) > 4096 || json.Unmarshal([]byte(termination.Message), &result) != nil {
			return model.Result{}, errors.New("decode session archive worker result")
		}
		code = diagnosticSafeErrorCode(result.SafeErrorCode, result.Success)
		stage = "RESULT_STATUS_CONFLICT"
		if (job.Status.Succeeded > 0) != result.Success {
			return model.Result{}, errors.New("session archive worker status conflicts")
		}
		stage = "WORKER_REPORTED_FAILURE"
		if result.Success {
			stage = "WORKER_REPORTED_SUCCESS"
		}
		return result, nil
	}
	return model.Result{}, errors.New("session archive worker result is missing")
}

func diagnosticTerminationReason(value string) string {
	switch value {
	case "Completed", "Error", "OOMKilled", "ContainerCannotRun", "DeadlineExceeded":
		return value
	default:
		return "UNKNOWN"
	}
}
func diagnosticTaskKind(value string) string {
	switch value {
	case "SNAPSHOT", "RESTORE", "DELETE_OBJECT":
		return value
	default:
		return "UNKNOWN"
	}
}
func diagnosticSafeErrorCode(value string, success bool) string {
	if value == "" && success {
		return "NONE"
	}
	switch value {
	case "SESSION_ARCHIVE_SOURCE_INVALID", "SESSION_ARCHIVE_OBJECT_WRITE_FAILED", "SESSION_ARCHIVE_OBJECT_READBACK_FAILED", "SESSION_ARCHIVE_OBJECT_DELETE_FAILED", "SESSION_ARCHIVE_RESTORE_INVALID", "SESSION_ARCHIVE_PVC_BUSY", "SESSION_ARCHIVE_PVC_MISSING", "SESSION_ARCHIVE_PVC_REPLACED", "SESSION_ARCHIVE_KUBERNETES_UNAVAILABLE", "SESSION_ARCHIVE_WORKER_FAILED", "SESSION_ARCHIVE_TIMEOUT", "SESSION_ARCHIVE_LEASE_EXPIRED", "SESSION_BECAME_ACTIVE":
		return value
	default:
		return "UNKNOWN"
	}
}
