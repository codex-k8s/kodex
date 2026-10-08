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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	workerObservationMessage     = "session archive worker terminal observation"
	archiveAPIFailureMessage     = "session archive Kubernetes operation failed"
	archiveTaskRefAttribute      = "task_ref"
	archiveSessionRefAttribute   = "session_ref"
	archiveTaskKindAttribute     = "task_kind"
	archiveGenerationAttribute   = "content_generation"
	archiveAttemptAttribute      = "attempt"
	archiveAPIStageAttribute     = "api_stage"
	archiveAPIReasonAttribute    = "api_reason"
	archiveAPIStatusAttribute    = "api_status_code"
	archiveFailureStageAttribute = "failure_stage"
	taskRefAnnotation            = "session-archive.kodex.dev/task-ref"
	taskKindAnnotation           = "session-archive.kodex.dev/task-kind"
	taskGenerationAnnotation     = "session-archive.kodex.dev/content-generation"
	taskAttemptAnnotation        = "session-archive.kodex.dev/attempt"
)

type archiveAPIStage string

const (
	archiveAPICreateInput archiveAPIStage = "CREATE_INPUT"
	archiveAPICreateJob   archiveAPIStage = "CREATE_JOB"
	archiveAPIObserveJob  archiveAPIStage = "OBSERVE_JOB"
)

// Диагностика использует только проверенный claim и закрытые поля API status.
// Message, Details, headers и произвольный текст причины не выдаются.
func (controller *Controller) observeAPIFailure(ctx context.Context, task model.Task, stage archiveAPIStage, err error) {
	if err == nil || controller.config.Logger == nil || task.Validate() != nil || task.Attempt > 5 {
		return
	}
	safeStage := "UNKNOWN"
	switch stage {
	case archiveAPICreateInput, archiveAPICreateJob, archiveAPIObserveJob:
		safeStage = string(stage)
	}
	reason, code := boundedAPIError(err)
	ref := func(value string) string {
		if diagnosticRefPattern.MatchString(value) {
			return value
		}
		return "UNKNOWN"
	}
	controller.config.Logger.InfoContext(ctx, archiveAPIFailureMessage,
		archiveTaskRefAttribute, ref(task.TaskRef), archiveSessionRefAttribute, ref(task.SessionRef),
		archiveTaskKindAttribute, diagnosticTaskKind(task.Kind), archiveGenerationAttribute, task.ContentGeneration,
		archiveAttemptAttribute, task.Attempt, archiveAPIStageAttribute, safeStage,
		archiveAPIReasonAttribute, reason, archiveAPIStatusAttribute, code)
}

func boundedAPIError(err error) (string, int32) {
	reason := "UNKNOWN"
	switch value := apierrors.ReasonForError(err); value {
	case metav1.StatusReasonUnauthorized, metav1.StatusReasonForbidden, metav1.StatusReasonNotFound,
		metav1.StatusReasonAlreadyExists, metav1.StatusReasonConflict, metav1.StatusReasonGone,
		metav1.StatusReasonInvalid, metav1.StatusReasonServerTimeout, metav1.StatusReasonTimeout,
		metav1.StatusReasonTooManyRequests, metav1.StatusReasonBadRequest,
		metav1.StatusReasonMethodNotAllowed, metav1.StatusReasonNotAcceptable,
		metav1.StatusReasonRequestEntityTooLarge, metav1.StatusReasonUnsupportedMediaType,
		metav1.StatusReasonInternalError, metav1.StatusReasonExpired, metav1.StatusReasonServiceUnavailable:
		reason = string(value)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		reason = "CLIENT_TIMEOUT"
	} else if errors.Is(err, context.Canceled) {
		reason = "CLIENT_CANCELLED"
	}
	var apiStatus apierrors.APIStatus
	var code int32
	if errors.As(err, &apiStatus) {
		statusCode := apiStatus.Status().Code
		if statusCode >= 400 && statusCode <= 599 {
			code = statusCode
		}
	}
	return reason, code
}

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
	failureStage := model.FailureStageUnknown
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
			archiveTaskRefAttribute, ref(task.TaskRef), archiveSessionRefAttribute, ref(task.SessionRef), archiveTaskKindAttribute, diagnosticTaskKind(task.Kind),
			archiveGenerationAttribute, task.ContentGeneration, archiveAttemptAttribute, task.Attempt, "job_ref", ref(jobRef), "pod_ref", ref(podRef),
			"stage", stage, "termination_reason", reason, "exit_code", exit, "safe_error_code", code, archiveFailureStageAttribute, string(failureStage))
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
		} else {
			failureStage = model.NormalizeFailureStage(result.FailureStage)
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
