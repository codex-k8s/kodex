package controller

import (
	"errors"
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// errWorkspaceVolumePrepareUnconfirmed closes preparation failures without embedding physical
// metadata, private lease proofs, termination output or raw Kubernetes diagnostics.
// errWorkspaceVolumePrepareUnconfirmed замыкает preparation failures без physical metadata,
// private lease proofs, termination output либо raw Kubernetes diagnostics.
var errWorkspaceVolumePrepareUnconfirmed = errors.New("workspace volume preparation is unconfirmed")

const (
	// workspaceVolumeTaskAnnotation binds resources to the current logical owner task, not the original sealed preparation.
	// workspaceVolumeTaskAnnotation связывает resources с current logical owner task, не original sealed preparation.
	workspaceVolumeTaskAnnotation = "workspace.kodex.dev/task-ref"
	// workspaceVolumeInputAnnotation pins the complete immutable effective attempt without carrying a private lease fence.
	// workspaceVolumeInputAnnotation закрепляет complete immutable effective attempt без private lease fence.
	workspaceVolumeInputAnnotation = "workspace.kodex.dev/input-digest"
	// workspaceVolumeInputUIDAnnotation retains the positive ConfigMap incarnation used by the helper Job.
	// workspaceVolumeInputUIDAnnotation сохраняет positive ConfigMap incarnation, используемую helper Job.
	workspaceVolumeInputUIDAnnotation = "workspace.kodex.dev/input-uid"
	// workspaceVolumePVCUIDAnnotation separates the observed physical volume from a reusable PVC name.
	// workspaceVolumePVCUIDAnnotation отделяет observed physical volume от повторно используемого имени PVC.
	workspaceVolumePVCUIDAnnotation = "workspace.kodex.dev/pvc-uid"
	// workspaceVolumeAttemptAnnotation pins the bounded physical attempt independently of model execution generations.
	// workspaceVolumeAttemptAnnotation закрепляет bounded physical attempt отдельно от model execution generations.
	workspaceVolumeAttemptAnnotation = "workspace.kodex.dev/attempt"
)

// workspaceVolumePreparationJob renders one credential-free helper only after the caller has a
// positive immutable input UID. Read-only reconciliation never receives FSGroup or write mounts;
// neither mode receives a model revision, service-account token or object-storage credentials.
// The caller still owns fresh grant validation, exact PVC observation, consumer exclusion and
// positive Job/Pod/all-container join before reporting readiness to the owner.
// workspaceVolumePreparationJob строит один credential-free helper только после получения caller
// positive immutable input UID. Read-only reconciliation не получает FSGroup либо write mounts;
// оба режима не получают model revision, service-account token или object-storage credentials.
// Caller отдельно проверяет fresh grant, exact PVC, отсутствие других consumers и positive
// Job/Pod/all-container join перед отправкой readiness владельцу.
func (controller *Controller) workspaceVolumePreparationJob(work runtimecontract.WorkspaceVolumeWork, input *corev1.ConfigMap) (*batchv1.Job, error) {
	if _, err := controller.workspaceVolumePVC(work); err != nil || work.Kind != runtimecontract.WorkspaceVolumePrepare {
		return nil, errWorkspaceVolumePrepareUnconfirmed
	}
	preparation, err := work.Preparation()
	if err != nil {
		return nil, errWorkspaceVolumePrepareUnconfirmed
	}
	raw, err := runtimecontract.EncodeWorkspaceVolumePreparation(preparation)
	name := workloadName(work.TaskRef, work.Volume.VolumeGeneration, work.Attempt)
	if err != nil || input == nil || input.UID == "" || input.ResourceVersion == "" || input.Name != name ||
		input.Namespace != exactWorkerNamespace || input.Immutable == nil || !*input.Immutable || input.DeletionTimestamp != nil ||
		len(input.OwnerReferences) != 0 || input.Labels[managedLabel] != "true" ||
		input.Annotations[workspaceVolumeTaskAnnotation] != work.TaskRef || input.Annotations[workspaceVolumeInputAnnotation] != work.InputDigest ||
		input.Annotations[workspaceVolumePVCUIDAnnotation] != work.Volume.PVCUID ||
		input.Annotations[workspaceVolumeAttemptAnnotation] != strconv.FormatInt(int64(work.Attempt), 10) ||
		len(input.Data) != 1 || len(input.BinaryData) != 0 || input.Data["volume.json"] != string(raw) {
		return nil, errWorkspaceVolumePrepareUnconfirmed
	}
	pvc, _ := runtimecontract.SessionPVCName(work.Volume.Scope.SessionRef)
	mode := "volume"
	if work.ReconcileOnly {
		mode = "volume-readback"
	}
	mounts := []corev1.VolumeMount{{Name: "task", MountPath: "/var/run/config/kodex/session-archive", ReadOnly: true},
		{Name: "session", MountPath: runtimecontract.WorkspaceStorageRootMount, ReadOnly: work.ReconcileOnly},
		{Name: "tmp", MountPath: "/tmp"}}
	helper := controller.workspaceWriterContainer("volume", mode, 10001, mounts)
	helper.TerminationMessagePath = "/dev/termination-log"
	helper.TerminationMessagePolicy = corev1.TerminationMessageReadFile
	zero, one, falseValue := int32(0), int32(1), false
	deadline := int64(controller.config.WorkerTimeout.Seconds())
	replacement := batchv1.Failed
	security := &corev1.PodSecurityContext{RunAsNonRoot: boolPtr(true),
		SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}
	if !work.ReconcileOnly {
		group, policy := int64(29000), corev1.FSGroupChangeAlways
		security.FSGroup, security.FSGroupChangePolicy = &group, &policy
	}
	annotations := map[string]string{workspaceVolumeTaskAnnotation: work.TaskRef,
		workspaceVolumeInputAnnotation: work.InputDigest, workspaceVolumeInputUIDAnnotation: string(input.UID),
		workspaceVolumePVCUIDAnnotation: work.Volume.PVCUID, workspaceVolumeAttemptAnnotation: strconv.FormatInt(int64(work.Attempt), 10)}
	return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: exactWorkerNamespace,
		Labels: map[string]string{managedLabel: "true"}, Annotations: annotations},
		Spec: batchv1.JobSpec{BackoffLimit: &zero, Parallelism: &one, Completions: &one, ActiveDeadlineSeconds: &deadline,
			PodReplacementPolicy: &replacement, Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{managedLabel: "true"}, Annotations: annotations},
				Spec: corev1.PodSpec{ServiceAccountName: controller.config.WorkerServiceAccount, AutomountServiceAccountToken: &falseValue,
					EnableServiceLinks: &falseValue, RestartPolicy: corev1.RestartPolicyNever, SecurityContext: security,
					Containers: []corev1.Container{helper}, Volumes: []corev1.Volume{
						{Name: "task", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: input.Name}}}},
						{Name: "session", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvc, ReadOnly: work.ReconcileOnly}}},
						{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantity("4Mi")}}},
					}},
			}}}, nil
}
