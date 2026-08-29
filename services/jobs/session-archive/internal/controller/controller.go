package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	managedLabel           = "session-archive.kodex.dev/managed"
	restoreInputAnnotation = "session-archive.kodex.dev/restore-input-digest"
)

type Config struct {
	Namespace, Environment, WorkerImage, WorkerServiceAccount, ObjectStorageSecret string
	StorageClass, SessionPVCSize                                                   string
	ObjectStorageEndpoint, ObjectStorageRegion, ObjectStorageBucket                string
	ObjectStorageAllowInsecureLocal                                                bool
	WorkerTimeout                                                                  time.Duration
}

type Controller struct {
	client     kubernetes.Interface
	config     Config
	pvcRequest resource.Quantity
}

func InCluster(config Config) (*Controller, error) {
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.New("load Kubernetes session archive configuration")
	}
	restConfig.Timeout = 5 * time.Second
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, errors.New("create Kubernetes session archive client")
	}
	return New(client, config)
}

func New(client kubernetes.Interface, config Config) (*Controller, error) {
	pvcRequest, quantityErr := resource.ParseQuantity(config.SessionPVCSize)
	if client == nil || config.Namespace == "" || config.Environment == "" || config.WorkerImage == "" ||
		config.WorkerServiceAccount == "" || config.ObjectStorageSecret == "" || config.ObjectStorageEndpoint == "" ||
		config.ObjectStorageRegion == "" || config.ObjectStorageBucket == "" || quantityErr != nil || pvcRequest.Sign() <= 0 ||
		(config.StorageClass != "" && len(utilvalidation.IsDNS1123Subdomain(config.StorageClass)) != 0) ||
		config.WorkerTimeout < time.Minute || config.WorkerTimeout > 15*time.Minute {
		return nil, errors.New("session archive controller configuration is invalid")
	}
	return &Controller{client: client, config: config, pvcRequest: pvcRequest}, nil
}

func (controller *Controller) Check(ctx context.Context) error {
	_, err := controller.client.BatchV1().Jobs(controller.config.Namespace).List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		return errors.New("Kubernetes session archive API is unavailable")
	}
	return nil
}

func (controller *Controller) CleanupStale(ctx context.Context) error {
	selector := labels.Set{managedLabel: "true"}.AsSelector().String()
	propagation := metav1.DeletePropagationForeground
	if err := controller.client.BatchV1().Jobs(controller.config.Namespace).DeleteCollection(ctx,
		metav1.DeleteOptions{PropagationPolicy: &propagation}, metav1.ListOptions{LabelSelector: selector}); err != nil {
		return errors.New("delete stale session archive jobs")
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		jobs, err := controller.client.BatchV1().Jobs(controller.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return errors.New("observe stale session archive jobs")
		}
		if len(jobs.Items) == 0 {
			break
		}
		if time.Now().After(deadline) {
			return errors.New("stale session archive jobs did not terminate")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if err := controller.client.CoreV1().ConfigMaps(controller.config.Namespace).DeleteCollection(ctx,
		metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: selector}); err != nil {
		return errors.New("delete stale session archive task inputs")
	}
	return nil
}

func (controller *Controller) Execute(ctx context.Context, task model.Task, renew func(context.Context) error) (model.Result, error) {
	if task.Kind == "DELETE_PVC" {
		return controller.deletePVC(ctx, task)
	}
	if task.Kind == "RESTORE" {
		if err := controller.ensureRestorePVC(ctx, task); err != nil {
			return model.Result{}, err
		}
	}
	name := workloadName(task.TaskRef, task.ContentGeneration, task.Attempt)
	raw, err := json.Marshal(task)
	if err != nil || len(raw) > model.MaximumTaskBytes {
		return model.Result{}, errors.New("encode session archive task input")
	}
	immutable := true
	configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: controller.config.Namespace,
		Labels: map[string]string{managedLabel: "true"}}, Immutable: &immutable, Data: map[string]string{"task.json": string(raw)}}
	if _, err := controller.client.CoreV1().ConfigMaps(controller.config.Namespace).Create(ctx, configMap, metav1.CreateOptions{}); err != nil {
		return model.Result{}, errors.New("create session archive task input")
	}
	defer controller.cleanup(ctx, name)
	job := controller.job(name, task)
	if _, err := controller.client.BatchV1().Jobs(controller.config.Namespace).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		return model.Result{}, errors.New("create session archive worker job")
	}
	ticker := time.NewTicker(2 * time.Second)
	renewTicker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	defer renewTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return model.Result{}, ctx.Err()
		case <-renewTicker.C:
			if err := renew(ctx); err != nil {
				return model.Result{}, err
			}
		case <-ticker.C:
			job, err := controller.client.BatchV1().Jobs(controller.config.Namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return model.Result{}, errors.New("observe session archive worker job")
			}
			if job.Status.Succeeded == 0 && job.Status.Failed == 0 {
				continue
			}
			return controller.readResult(ctx, name, job.Status.Succeeded > 0)
		}
	}
}

func (controller *Controller) ensureRestorePVC(ctx context.Context, task model.Task) error {
	pvcs := controller.client.CoreV1().PersistentVolumeClaims(controller.config.Namespace)
	existing, err := pvcs.Get(ctx, task.PVCName, metav1.GetOptions{})
	if err == nil {
		return controller.validateRestorePVC(existing, task)
	}
	if !apierrors.IsNotFound(err) {
		return errors.New("read restored session PVC")
	}
	var storageClassName *string
	if controller.config.StorageClass != "" {
		storageClassName = &controller.config.StorageClass
	}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: task.PVCName, Namespace: controller.config.Namespace,
		Annotations: map[string]string{restoreInputAnnotation: task.InputDigest}}, Spec: corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, StorageClassName: storageClassName,
		Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: controller.pvcRequest}},
	}}
	created, err := pvcs.Create(ctx, pvc, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		created, err = pvcs.Get(ctx, task.PVCName, metav1.GetOptions{})
	}
	if err != nil {
		return errors.New("create restored session PVC")
	}
	return controller.validateRestorePVC(created, task)
}

func (controller *Controller) validateRestorePVC(pvc *corev1.PersistentVolumeClaim, task model.Task) error {
	wantedClass := controller.config.StorageClass
	actualClass := ""
	if pvc.Spec.StorageClassName != nil {
		actualClass = *pvc.Spec.StorageClassName
	}
	classMatches := wantedClass == "" || actualClass == wantedClass
	requested, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if pvc.DeletionTimestamp != nil || pvc.Annotations[restoreInputAnnotation] != task.InputDigest ||
		!classMatches || !ok || requested.Cmp(controller.pvcRequest) != 0 ||
		len(pvc.Spec.AccessModes) != 1 || pvc.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		return errors.New("restored session PVC conflicts with immutable task input")
	}
	return nil
}

func (controller *Controller) deletePVC(ctx context.Context, task model.Task) (model.Result, error) {
	if task.Archive == nil {
		return model.Result{}, errors.New("PVC deletion lacks an archive receipt")
	}
	pods, err := controller.client.CoreV1().Pods(controller.config.Namespace).List(ctx, metav1.ListOptions{Limit: 500})
	if err != nil {
		return model.Result{}, errors.New("list PVC consumers")
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == task.PVCName {
				return model.Result{Success: false, SafeErrorCode: "SESSION_ARCHIVE_PVC_BUSY"}, nil
			}
		}
	}
	pvc, err := controller.client.CoreV1().PersistentVolumeClaims(controller.config.Namespace).Get(ctx, task.PVCName, metav1.GetOptions{})
	if err == nil {
		uid := pvc.UID
		if err := controller.client.CoreV1().PersistentVolumeClaims(controller.config.Namespace).Delete(ctx, task.PVCName,
			metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
			return model.Result{}, errors.New("delete archived session PVC")
		}
	} else if !apierrors.IsNotFound(err) {
		return model.Result{}, errors.New("read archived session PVC")
	}
	for {
		_, err := controller.client.CoreV1().PersistentVolumeClaims(controller.config.Namespace).Get(ctx, task.PVCName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return model.Result{Success: true}, nil
		}
		if err != nil {
			return model.Result{}, errors.New("observe archived session PVC deletion")
		}
		select {
		case <-ctx.Done():
			return model.Result{}, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (controller *Controller) readResult(ctx context.Context, jobName string, succeeded bool) (model.Result, error) {
	pods, err := controller.client.CoreV1().Pods(controller.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + jobName})
	if err != nil || len(pods.Items) != 1 {
		return model.Result{}, errors.New("read session archive worker pod")
	}
	for _, status := range pods.Items[0].Status.ContainerStatuses {
		if status.Name == "worker" && status.State.Terminated != nil {
			var result model.Result
			if json.Unmarshal([]byte(status.State.Terminated.Message), &result) != nil {
				return model.Result{}, errors.New("decode session archive worker result")
			}
			if succeeded != result.Success {
				return model.Result{}, errors.New("session archive worker status conflicts")
			}
			return result, nil
		}
	}
	return model.Result{}, errors.New("session archive worker result is missing")
}

func (controller *Controller) job(name string, task model.Task) *batchv1.Job {
	zero, deadline, ttl := int32(0), int64(controller.config.WorkerTimeout/time.Second), int32(300)
	falseValue := false
	volumes := []corev1.Volume{
		{Name: "task", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: name}}}},
		{Name: "object-storage", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: controller.config.ObjectStorageSecret}}},
		{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantity("136Mi")}}},
	}
	mounts := []corev1.VolumeMount{{Name: "task", MountPath: "/var/run/config/kodex/session-archive", ReadOnly: true},
		{Name: "object-storage", MountPath: "/var/run/secrets/kodex/session-archive/object-storage", ReadOnly: true}, {Name: "tmp", MountPath: "/tmp"}}
	if task.Kind == "SNAPSHOT" || task.Kind == "RESTORE" {
		volumes = append(volumes, corev1.Volume{Name: "session", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: task.PVCName}}})
		mounts = append(mounts, corev1.VolumeMount{Name: "session", MountPath: "/workspace"})
	}
	container := corev1.Container{Name: "worker", Image: controller.config.WorkerImage, Args: []string{"worker"},
		Env: []corev1.EnvVar{{Name: "DEPLOYMENT_ENVIRONMENT", Value: controller.config.Environment},
			{Name: "SESSION_ARCHIVE_OBJECT_STORAGE_ENDPOINT", Value: controller.config.ObjectStorageEndpoint},
			{Name: "SESSION_ARCHIVE_OBJECT_STORAGE_REGION", Value: controller.config.ObjectStorageRegion},
			{Name: "SESSION_ARCHIVE_OBJECT_STORAGE_BUCKET", Value: controller.config.ObjectStorageBucket},
			{Name: "SESSION_ARCHIVE_OBJECT_STORAGE_ALLOW_INSECURE_LOCAL", Value: strconv.FormatBool(controller.config.ObjectStorageAllowInsecureLocal)},
			{Name: "SESSION_ARCHIVE_OBJECT_STORAGE_USE_PATH_STYLE", Value: "true"},
			{Name: "SESSION_ARCHIVE_WORKER_TIMEOUT", Value: controller.config.WorkerTimeout.String()}},
		VolumeMounts: mounts, TerminationMessagePath: "/dev/termination-log", TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		SecurityContext: restricted(10002)}
	return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: controller.config.Namespace, Labels: map[string]string{managedLabel: "true"}},
		Spec: batchv1.JobSpec{BackoffLimit: &zero, ActiveDeadlineSeconds: &deadline, TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{managedLabel: "true"}}, Spec: corev1.PodSpec{
				ServiceAccountName: controller.config.WorkerServiceAccount, AutomountServiceAccountToken: &falseValue,
				RestartPolicy: corev1.RestartPolicyNever, EnableServiceLinks: &falseValue, Containers: []corev1.Container{container}, Volumes: volumes,
				SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: boolPtr(true), FSGroup: int64Ptr(29000), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
			}}}}
}

func (controller *Controller) cleanup(ctx context.Context, name string) {
	background, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	propagation := metav1.DeletePropagationBackground
	_ = controller.client.BatchV1().Jobs(controller.config.Namespace).Delete(background, name, metav1.DeleteOptions{PropagationPolicy: &propagation})
	_ = controller.client.CoreV1().ConfigMaps(controller.config.Namespace).Delete(background, name, metav1.DeleteOptions{})
}

func workloadName(ref string, generation int64, attempt int32) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d", ref, generation, attempt)))
	return "session-archive-" + hex.EncodeToString(digest[:8])
}
func restricted(uid int64) *corev1.SecurityContext {
	return &corev1.SecurityContext{RunAsNonRoot: boolPtr(true), RunAsUser: &uid, RunAsGroup: &uid, AllowPrivilegeEscalation: boolPtr(false), ReadOnlyRootFilesystem: boolPtr(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}
}
func boolPtr(value bool) *bool                 { return &value }
func int64Ptr(value int64) *int64              { return &value }
func quantity(value string) *resource.Quantity { result := resource.MustParse(value); return &result }
