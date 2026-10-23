package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// archiveCleanupTimeout bounds predecessor joining and detached attempt cleanup.
	// RU: archiveCleanupTimeout ограничивает ожидание предшественников и независимую очистку попытки.
	archiveCleanupTimeout = 30 * time.Second
	// archiveRenewTimeout bounds each owner-authority renewal before a cleanup effect.
	// RU: archiveRenewTimeout ограничивает каждое продление полномочий владельца перед эффектом очистки.
	archiveRenewTimeout = 10 * time.Second
	// maximumArchiveAttempt bounds the predecessor scan for one archive task.
	// RU: maximumArchiveAttempt ограничивает обход предшествующих попыток одного задания архива.
	maximumArchiveAttempt = 5
)

// renewAuthority requires a live context and a successful bounded renewal before further task effects.
// RU: renewAuthority требует живой контекст и успешное ограниченное продление до следующих эффектов задания.
func renewAuthority(ctx context.Context, renew func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if renew == nil {
		return errors.New("session archive authority renewal is missing")
	}
	call, cancel := context.WithTimeout(ctx, archiveRenewTimeout)
	defer cancel()
	if err := renew(call); err != nil {
		return err
	}
	return call.Err()
}

// cleanupPredecessors uses the current owner claim to validate and join only earlier attempts of this task generation.
// RU: cleanupPredecessors использует текущий claim владельца для проверки и завершения лишь прежних попыток этого поколения задания.
func (controller *Controller) cleanupPredecessors(ctx context.Context, task model.Task, renew func(context.Context) error) error {
	if task.Attempt < 1 || task.Attempt > maximumArchiveAttempt {
		return errors.New("session archive attempt is invalid")
	}
	if err := renewAuthority(ctx, renew); err != nil {
		return err
	}
	cleanup, cancel := context.WithTimeout(ctx, archiveCleanupTimeout)
	defer cancel()
	for attempt := int32(1); attempt < task.Attempt; attempt++ {
		name := workloadName(task.TaskRef, task.ContentGeneration, attempt)
		input, err := controller.client.CoreV1().ConfigMaps(controller.config.WorkerNamespace).Get(cleanup, name, metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return errors.New("read predecessor session archive input")
		}
		job, jobErr := controller.client.BatchV1().Jobs(controller.config.WorkerNamespace).Get(cleanup, name, metav1.GetOptions{})
		if jobErr != nil && !apierrors.IsNotFound(jobErr) {
			return errors.New("read predecessor session archive job")
		}
		if apierrors.IsNotFound(jobErr) {
			pods, podErr := controller.client.CoreV1().Pods(controller.config.WorkerNamespace).List(cleanup,
				metav1.ListOptions{LabelSelector: "job-name=" + name})
			if podErr != nil || len(pods.Items) != 0 {
				return errors.New("predecessor session archive pods have no job binding")
			}
		}
		if apierrors.IsNotFound(err) && apierrors.IsNotFound(jobErr) {
			continue
		}
		if apierrors.IsNotFound(err) || !validPredecessorInput(task, attempt, input) {
			return errors.New("predecessor session archive binding is invalid")
		}
		if apierrors.IsNotFound(jobErr) {
			job = nil
		} else {
			previous, decodeErr := decodeAttemptInput(input)
			if decodeErr != nil || !validWorkerJob(previous, job, types.UID(job.Annotations[sourcePVCUIDAnnotation])) || !jobUsesInput(job, name) {
				return errors.New("predecessor session archive job binding is invalid")
			}
		}
		if err := controller.cleanupAttempt(cleanup, input, job, renew); err != nil {
			return err
		}
	}
	return renewAuthority(ctx, renew)
}

// decodeAttemptInput strictly decodes one bounded immutable task document before validating its owner pins.
// RU: decodeAttemptInput строго декодирует ограниченный неизменяемый документ задания до проверки его привязок владельца.
func decodeAttemptInput(input *corev1.ConfigMap) (model.Task, error) {
	// wireTask bypasses Task.UnmarshalJSON so the outer decoder can enforce unknown-field rejection.
	// RU: wireTask обходит Task.UnmarshalJSON, позволяя внешнему decoder отклонять неизвестные поля.
	type wireTask model.Task
	var decoded wireTask
	raw := input.Data["task.json"]
	if len(raw) == 0 || len(raw) > model.MaximumTaskBytes {
		return model.Task{}, errors.New("session archive input size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return model.Task{}, errors.New("session archive input is invalid")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return model.Task{}, errors.New("session archive input has trailing data")
	}
	task := model.Task(decoded)
	return task, task.Validate()
}

// validPredecessorInput checks exact ConfigMap identity and equal owner pins, allowing only server-reassigned attempt fields.
// RU: validPredecessorInput проверяет точную идентичность ConfigMap и равенство привязок владельца, допуская лишь переназначенные поля попытки.
func validPredecessorInput(current model.Task, attempt int32, input *corev1.ConfigMap) bool {
	if input == nil || input.Namespace != exactWorkerNamespace || input.UID == "" || input.ResourceVersion == "" ||
		input.Name != workloadName(current.TaskRef, current.ContentGeneration, attempt) ||
		input.Labels[managedLabel] != "true" || input.Immutable == nil || !*input.Immutable ||
		len(input.Data) != 1 || len(input.BinaryData) != 0 {
		return false
	}
	previous, err := decodeAttemptInput(input)
	if err != nil || previous.Attempt != attempt || current.Validate() != nil {
		return false
	}
	// CP заново назначает input digest каждой attempt; SNAPSHOT получает
	// отдельный object key. Все остальные immutable owner pins должны совпасть.
	previous.Attempt, previous.InputDigest = current.Attempt, current.InputDigest
	if current.Kind == "SNAPSHOT" {
		previous.TargetObjectKey = current.TargetObjectKey
	}
	return reflect.DeepEqual(previous, current)
}

// jobUsesInput requires exactly one task volume referring to the expected immutable ConfigMap name.
// RU: jobUsesInput требует ровно один том задания со ссылкой на ожидаемое имя неизменяемой ConfigMap.
func jobUsesInput(job *batchv1.Job, name string) bool {
	count := 0
	for _, volume := range job.Spec.Template.Spec.Volumes {
		if volume.Name == "task" && volume.ConfigMap != nil && volume.ConfigMap.Name == name {
			count++
		}
	}
	return count == 1
}

// cleanupAttempt deletes with UID/resourceVersion preconditions and joins the exact Job and owned Pods before removing input.
// RU: cleanupAttempt удаляет с предусловиями UID/resourceVersion и дожидается точной Job и принадлежащих ей Pods до удаления входа.
func (controller *Controller) cleanupAttempt(ctx context.Context, input *corev1.ConfigMap, job *batchv1.Job, renew func(context.Context) error) error {
	if input == nil || input.UID == "" || input.ResourceVersion == "" {
		return errors.New("session archive cleanup input identity is missing")
	}
	check := func() error {
		if renew == nil {
			return ctx.Err()
		}
		return renewAuthority(ctx, renew)
	}
	if job != nil {
		if job.UID == "" || job.ResourceVersion == "" || job.Name != input.Name {
			return errors.New("session archive cleanup job identity is missing")
		}
		// Job status штатно меняет resourceVersion после Create. Свежий read
		// разрешён только при прежнем UID; replacement не усыновляется.
		current, err := controller.client.BatchV1().Jobs(controller.config.WorkerNamespace).Get(ctx, job.Name, metav1.GetOptions{})
		if err == nil {
			if current.UID != job.UID || current.ResourceVersion == "" {
				return errors.New("session archive job was replaced before cleanup")
			}
			job = current
		} else if !apierrors.IsNotFound(err) {
			return errors.New("read exact session archive cleanup job")
		}
		if err := check(); err != nil {
			return err
		}
		foreground := metav1.DeletePropagationForeground
		if err := controller.client.BatchV1().Jobs(controller.config.WorkerNamespace).Delete(ctx, job.Name,
			metav1.DeleteOptions{PropagationPolicy: &foreground, Preconditions: &metav1.Preconditions{UID: &job.UID, ResourceVersion: &job.ResourceVersion}}); err != nil && !apierrors.IsNotFound(err) {
			return errors.New("delete exact session archive job")
		}
		for {
			current, err := controller.client.BatchV1().Jobs(controller.config.WorkerNamespace).Get(ctx, job.Name, metav1.GetOptions{})
			if err == nil && current.UID != job.UID {
				return errors.New("session archive job was replaced during cleanup")
			}
			if err != nil && !apierrors.IsNotFound(err) {
				return errors.New("observe session archive job cleanup")
			}
			pods, podErr := controller.client.CoreV1().Pods(controller.config.WorkerNamespace).List(ctx,
				metav1.ListOptions{LabelSelector: "job-name=" + job.Name})
			if podErr != nil {
				return errors.New("observe session archive pod cleanup")
			}
			for _, pod := range pods.Items {
				owner := metav1.GetControllerOf(&pod)
				if owner == nil || owner.Kind != "Job" || owner.APIVersion != "batch/v1" || owner.UID != job.UID || owner.Name != job.Name {
					return errors.New("session archive cleanup pod binding is invalid")
				}
			}
			if apierrors.IsNotFound(err) && len(pods.Items) == 0 {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if err := check(); err != nil {
		return err
	}
	if err := controller.client.CoreV1().ConfigMaps(controller.config.WorkerNamespace).Delete(ctx, input.Name,
		metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &input.UID, ResourceVersion: &input.ResourceVersion}}); err != nil && !apierrors.IsNotFound(err) {
		return errors.New("delete exact session archive task input")
	}
	return nil
}
