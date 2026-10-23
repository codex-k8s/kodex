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
	// workerObservationMessage names the safe terminal worker diagnostic record.
	// RU: workerObservationMessage задаёт имя безопасной записи диагностики завершившегося worker.
	workerObservationMessage = "session archive worker terminal observation"
	// archiveAPIFailureMessage names a bounded Kubernetes-operation failure record.
	// RU: archiveAPIFailureMessage задаёт имя ограниченной записи отказа Kubernetes-операции.
	archiveAPIFailureMessage = "session archive Kubernetes operation failed"
	// archiveTaskRefAttribute names the validated task reference log field.
	// RU: archiveTaskRefAttribute задаёт поле журнала проверенной ссылки задания.
	archiveTaskRefAttribute = "task_ref"
	// archiveSessionRefAttribute names the validated session reference log field.
	// RU: archiveSessionRefAttribute задаёт поле журнала проверенной ссылки сессии.
	archiveSessionRefAttribute = "session_ref"
	// archiveTaskKindAttribute names the closed task-kind log field.
	// RU: archiveTaskKindAttribute задаёт поле журнала закрытого вида задания.
	archiveTaskKindAttribute = "task_kind"
	// archiveGenerationAttribute names the claimed content-generation log field.
	// RU: archiveGenerationAttribute задаёт поле журнала полученного поколения содержимого.
	archiveGenerationAttribute = "content_generation"
	// archiveAttemptAttribute names the bounded owner-attempt log field.
	// RU: archiveAttemptAttribute задаёт поле журнала ограниченной попытки владельца.
	archiveAttemptAttribute = "attempt"
	// archiveAPIStageAttribute names the closed Kubernetes API stage log field.
	// RU: archiveAPIStageAttribute задаёт поле журнала закрытого этапа Kubernetes API.
	archiveAPIStageAttribute = "api_stage"
	// archiveAPIReasonAttribute names the allowlisted API reason log field.
	// RU: archiveAPIReasonAttribute задаёт поле журнала причины API из разрешённого списка.
	archiveAPIReasonAttribute = "api_reason"
	// archiveAPIStatusAttribute names the bounded HTTP status-code log field.
	// RU: archiveAPIStatusAttribute задаёт поле журнала ограниченного HTTP-кода.
	archiveAPIStatusAttribute = "api_status_code"
	// archiveFailureStageAttribute names the closed archive failure-stage log field.
	// RU: archiveFailureStageAttribute задаёт поле журнала закрытого этапа отказа архива.
	archiveFailureStageAttribute = "failure_stage"
	// taskRefAnnotation binds worker metadata to the claimed archive task reference.
	// RU: taskRefAnnotation связывает метаданные worker со ссылкой полученного задания архива.
	taskRefAnnotation = "session-archive.kodex.dev/task-ref"
	// taskKindAnnotation pins the worker's expected archive action in its metadata.
	// RU: taskKindAnnotation закрепляет ожидаемое действие архива в метаданных worker.
	taskKindAnnotation = "session-archive.kodex.dev/task-kind"
	// taskGenerationAnnotation pins the source content generation in worker metadata.
	// RU: taskGenerationAnnotation закрепляет поколение исходного содержимого в метаданных worker.
	taskGenerationAnnotation = "session-archive.kodex.dev/content-generation"
	// taskAttemptAnnotation pins the owner-assigned attempt in worker metadata.
	// RU: taskAttemptAnnotation закрепляет назначенную владельцем попытку в метаданных worker.
	taskAttemptAnnotation = "session-archive.kodex.dev/attempt"
)

// archiveAPIStage is the closed set of Kubernetes operations exposed in safe failure diagnostics.
// RU: archiveAPIStage представляет закрытый набор Kubernetes-операций в безопасной диагностике отказа.
type archiveAPIStage string

const (
	// archiveAPICreateInput marks failure while creating the immutable task ConfigMap.
	// RU: archiveAPICreateInput обозначает отказ создания неизменяемой ConfigMap задания.
	archiveAPICreateInput archiveAPIStage = "CREATE_INPUT"
	// archiveAPICreateJob marks failure while creating the worker Job.
	// RU: archiveAPICreateJob обозначает отказ создания Job worker.
	archiveAPICreateJob archiveAPIStage = "CREATE_JOB"
	// archiveAPIObserveJob marks failure while reading worker Job state.
	// RU: archiveAPIObserveJob обозначает отказ чтения состояния Job worker.
	archiveAPIObserveJob archiveAPIStage = "OBSERVE_JOB"
)

// observeAPIFailure logs validated task pins and bounded API status fields without raw messages, details or headers.
// RU: observeAPIFailure записывает проверенные привязки задания и ограниченные поля статуса API без сырых сообщений, подробностей и заголовков.
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

// boundedAPIError reduces API errors and cancellation to an allowlisted reason and an optional 4xx/5xx code.
// RU: boundedAPIError сводит ошибки API и отмену к разрешённой причине и необязательному коду 4xx/5xx.
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

// diagnosticRefPattern limits logged resource identifiers to a bounded safe character set.
// RU: diagnosticRefPattern ограничивает выводимые идентификаторы ресурсов безопасным набором символов и длиной.
var diagnosticRefPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// workerDiagnosticAnnotations records task/generation/attempt and optional PVC UID pins for later identity checks.
// RU: workerDiagnosticAnnotations фиксирует привязки задания, поколения, попытки и необязательного UID PVC для последующей сверки.
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

// validWorkerJob binds observed Job metadata to the already-authorized task and exact source PVC UID.
// RU: validWorkerJob связывает наблюдаемые метаданные Job с уже авторизованным заданием и точным UID исходного PVC.
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

// validWorkerPod requires the exact Job controller owner and the task's sole expected session volume when applicable.
// RU: validWorkerPod требует точного Job-владельца контроллера и единственный ожидаемый том сессии, когда он нужен.
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

// readResult validates Job/Pod lineage before decoding bounded termination JSON and checking terminal status consistency.
// RU: readResult проверяет происхождение Job/Pod до чтения ограниченного JSON завершения и сверки конечного статуса.
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

// diagnosticTerminationReason maps arbitrary container termination text to a closed reason set.
// RU: diagnosticTerminationReason отображает произвольный текст причины завершения контейнера в закрытый набор.
func diagnosticTerminationReason(value string) string {
	switch value {
	case "Completed", "Error", "OOMKilled", "ContainerCannotRun", "DeadlineExceeded":
		return value
	default:
		return "UNKNOWN"
	}
}

// diagnosticTaskKind exposes only recognized worker-backed task kinds and otherwise returns UNKNOWN.
// RU: diagnosticTaskKind раскрывает лишь распознанные виды заданий worker, иначе возвращает UNKNOWN.
func diagnosticTaskKind(value string) string {
	switch value {
	case "SNAPSHOT", "RESTORE", "DELETE_OBJECT":
		return value
	default:
		return "UNKNOWN"
	}
}

// diagnosticSafeErrorCode preserves known safe codes, represents successful emptiness as NONE and bounds other input.
// RU: diagnosticSafeErrorCode сохраняет известные безопасные коды, обозначает успешную пустоту как NONE и ограничивает прочий ввод.
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
