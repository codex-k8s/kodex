package controller

import (
	"context"
	"errors"
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// errWorkspaceVolumeCreateUnconfirmed preserves a closed diagnostic for any missing positive
// Create response; raw API errors, credentials and an existing name never become ownership proof.
// errWorkspaceVolumeCreateUnconfirmed сохраняет закрытую диагностику при отсутствии positive
// Create response; raw API errors, credentials и существующее имя не становятся ownership proof.
var errWorkspaceVolumeCreateUnconfirmed = errors.New("workspace volume create is unconfirmed")

// workspaceVolumePVC renders only the qualified local-path intent with the unchanged twenty-GiB
// request. Caller-selected data sources, PV names, owner references and arbitrary storage profiles
// cannot enter this object; the returned metadata is an expectation, not authority.
// workspaceVolumePVC строит только qualified local-path intent с прежним запросом двадцати ГиБ.
// Caller-selected data sources, PV names, owner references и произвольные storage profiles
// не попадают в объект; возвращённые metadata являются ожиданием, не полномочиями.
func (controller *Controller) workspaceVolumePVC(work runtimecontract.WorkspaceVolumeWork) (*corev1.PersistentVolumeClaim, error) {
	if controller == nil || controller.client == nil || work.Validate() != nil ||
		controller.config.WorkerNamespace != exactWorkerNamespace || controller.config.StorageClass != "local-path" ||
		controller.pvcRequest.Value() != runtimecontract.WorkspaceVolumeRequestedBytes {
		return nil, errWorkspaceVolumeCreateUnconfirmed
	}
	input := work.Volume
	name, err := runtimecontract.SessionPVCName(input.Scope.SessionRef)
	if err != nil {
		return nil, errWorkspaceVolumeCreateUnconfirmed
	}
	labels, annotations, err := runtimecontract.SessionVolumeMetadata(input.Scope.OrganizationRef, input.Scope.ProjectRef, input.Scope.SessionRef)
	if err != nil {
		return nil, errWorkspaceVolumeCreateUnconfirmed
	}
	annotations["workspace.kodex.dev/workspace-ref"] = input.WorkspaceRef
	annotations["workspace.kodex.dev/volume-generation"] = strconv.FormatInt(input.VolumeGeneration, 10)
	annotations["workspace.kodex.dev/create-task-ref"] = input.CreateTaskRef
	storageClass, mode := controller.config.StorageClass, corev1.PersistentVolumeFilesystem
	return &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: exactWorkerNamespace,
		Labels: labels, Annotations: annotations}, Spec: corev1.PersistentVolumeClaimSpec{
		StorageClassName: &storageClass, VolumeMode: &mode, AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: controller.pvcRequest.DeepCopy()}},
	}}, nil
}

// CreateWorkspaceVolume performs one owner-renewed first-attempt Create and returns only its
// positive API UID. AlreadyExists, transport uncertainty and reconciliation never cause a GET,
// adoption, second Create or deletion; the owner retains uncertain custody for diagnosis.
// CreateWorkspaceVolume выполняет один owner-renewed Create первой attempt и возвращает только
// positive API UID. AlreadyExists, transport uncertainty и reconciliation не вызывают GET,
// adoption, второй Create либо удаление; owner сохраняет uncertain custody для диагностики.
func (controller *Controller) CreateWorkspaceVolume(ctx context.Context, work runtimecontract.WorkspaceVolumeWork, renew func(context.Context) error) (string, error) {
	if work.Kind != runtimecontract.WorkspaceVolumeCreate || work.Attempt != 1 || work.ReconcileOnly {
		return "", errWorkspaceVolumeCreateUnconfirmed
	}
	wanted, err := controller.workspaceVolumePVC(work)
	if err != nil || ctx == nil || renewAuthority(ctx, renew) != nil {
		return "", errWorkspaceVolumeCreateUnconfirmed
	}
	created, err := controller.client.CoreV1().PersistentVolumeClaims(exactWorkerNamespace).Create(ctx, wanted, metav1.CreateOptions{})
	if err != nil || !matchingWorkspaceVolumePVC(wanted, created) ||
		runtimecontract.ValidateWorkspaceVolumeObservation(work, string(created.UID), "", "", nil) != nil {
		return "", errWorkspaceVolumeCreateUnconfirmed
	}
	return string(created.UID), nil
}

// matchingWorkspaceVolumePVC checks the qualified physical geometry and owner metadata without
// adopting a name or assigning authority; callers separately prove a positive Create or pinned UID.
// matchingWorkspaceVolumePVC проверяет qualified physical geometry и owner metadata без adoption
// имени либо назначения полномочий; caller отдельно доказывает positive Create или pinned UID.
func matchingWorkspaceVolumePVC(wanted, created *corev1.PersistentVolumeClaim) bool {
	if wanted == nil || created == nil || created.UID == "" || created.Namespace != wanted.Namespace || created.Name != wanted.Name ||
		created.DeletionTimestamp != nil || len(created.OwnerReferences) != 0 ||
		created.Spec.StorageClassName == nil || *created.Spec.StorageClassName != "local-path" ||
		created.Spec.VolumeMode == nil || *created.Spec.VolumeMode != corev1.PersistentVolumeFilesystem ||
		len(created.Spec.AccessModes) != 1 || created.Spec.AccessModes[0] != corev1.ReadWriteOnce ||
		created.Spec.DataSource != nil || created.Spec.DataSourceRef != nil ||
		created.Spec.Resources.Requests.Storage().Value() != runtimecontract.WorkspaceVolumeRequestedBytes {
		return false
	}
	for key, value := range wanted.Labels {
		if created.Labels[key] != value {
			return false
		}
	}
	for key, value := range wanted.Annotations {
		if created.Annotations[key] != value {
			return false
		}
	}
	return true
}
