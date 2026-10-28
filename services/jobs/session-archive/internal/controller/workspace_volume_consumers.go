package controller

import (
	"context"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// workspaceVolumePredecessor checks a bounded earlier typed intent in the same exact physical
// incarnation. It proves quiescence only, not an old positive Create, fresh grant or owner READY.
// A missing original initializer cannot authorize a read-only successor: its late Create would
// otherwise be able to initialize after that successor had admitted private business data.
// workspaceVolumePredecessor проверяет bounded earlier typed intent в той же exact physical
// incarnation. Доказывает только quiescence, не old positive Create, fresh grant либо owner READY.
// Отсутствующий original initializer не разрешает read-only successor: его late Create иначе
// мог бы выполнить initialization после допуска private business data successor-ом.
func (controller *Controller) workspaceVolumePredecessor(ctx context.Context, current runtimecontract.WorkspaceVolumeWork, reference string, attempt int32) (*workspaceVolumeAttemptResources, error) {
	name := workloadName(reference, current.Volume.VolumeGeneration, attempt)
	input, inputErr := controller.client.CoreV1().ConfigMaps(exactWorkerNamespace).Get(ctx, name, metav1.GetOptions{})
	job, jobErr := controller.client.BatchV1().Jobs(exactWorkerNamespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(inputErr) && apierrors.IsNotFound(jobErr) {
		if reference == current.Volume.PreparationTaskRef && attempt == 1 {
			return nil, errWorkspaceVolumePreparePending
		}
		return nil, nil
	}
	if inputErr != nil || jobErr != nil || input == nil || job == nil {
		if apierrors.IsNotFound(inputErr) || apierrors.IsNotFound(jobErr) {
			return nil, errWorkspaceVolumePreparePending
		}
		return nil, errWorkspaceVolumePrepareUnconfirmed
	}
	previous, err := runtimecontract.DecodeWorkspaceVolumeWork([]byte(input.Data["work.json"]))
	if err != nil || previous.TaskRef != reference || previous.Attempt != attempt || previous.Kind != runtimecontract.WorkspaceVolumePrepare {
		return nil, errWorkspaceVolumePrepareUnconfirmed
	}
	before, after := previous.Volume, current.Volume
	before.ReadOnly, after.ReadOnly = false, false
	if before != after || reference != current.TaskRef && reference != current.Volume.PreparationTaskRef ||
		reference == current.TaskRef && attempt >= current.Attempt {
		return nil, errWorkspaceVolumePrepareUnconfirmed
	}
	expected, err := controller.workspaceVolumePreparationJob(previous, input)
	if err != nil || !matchingWorkspaceVolumeJob(expected, job) {
		return nil, errWorkspaceVolumePrepareUnconfirmed
	}
	pods, err := controller.client.CoreV1().Pods(exactWorkerNamespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + name, Limit: 2})
	if err != nil || pods == nil || pods.Continue != "" || len(pods.Items) > 1 {
		return nil, errWorkspaceVolumePrepareUnconfirmed
	}
	if len(pods.Items) == 0 {
		// Only an owner-selected recovery of an already accepted root may rely on its retained
		// terminal tombstone after the original Pod has been positively released.
		// Только owner-selected recovery уже accepted root может использовать retained terminal
		// tombstone после подтверждённого освобождения исходного Pod.
		if !current.Volume.ReadOnly || !completedWorkspaceVolumeJob(job) {
			return nil, errWorkspaceVolumePreparePending
		}
	} else {
		pod := &pods.Items[0]
		if !matchingWorkspaceVolumePod(expected, job, pod) {
			return nil, errWorkspaceVolumePrepareUnconfirmed
		}
		if !joinedWorkspaceVolumePreparation(job, pod) {
			return nil, errWorkspaceVolumePreparePending
		}
	}
	return &workspaceVolumeAttemptResources{work: previous, input: input, job: job}, nil
}

// workspaceVolumeConsumerBarrier requires the pinned PVC and every known prior attempt to be
// quiescent before another helper runs. Namespace-wide consumer inspection rejects unknown Pods;
// labels alone never admit a competing writer, and no predecessor object is deleted or adopted.
// workspaceVolumeConsumerBarrier требует pinned PVC и quiescence каждой известной prior attempt
// до запуска следующего helper. Namespace-wide проверка отклоняет unknown Pods; labels сами по
// себе не допускают competing writer. Predecessor objects не удаляются и не усыновляются.
func (controller *Controller) workspaceVolumeConsumerBarrier(ctx context.Context, current runtimecontract.WorkspaceVolumeWork, own *batchv1.Job) ([]workspaceVolumeAttemptResources, error) {
	wanted, err := controller.workspaceVolumePVC(current)
	if err != nil || ctx == nil || current.Kind != runtimecontract.WorkspaceVolumePrepare {
		return nil, errWorkspaceVolumePrepareUnconfirmed
	}
	pvc, err := controller.client.CoreV1().PersistentVolumeClaims(exactWorkerNamespace).Get(ctx, wanted.Name, metav1.GetOptions{})
	if err != nil || !matchingWorkspaceVolumePVC(wanted, pvc) || string(pvc.UID) != current.Volume.PVCUID {
		return nil, errWorkspaceVolumePrepareUnconfirmed
	}
	var previous []workspaceVolumeAttemptResources
	known := make(map[string]workspaceVolumeAttemptResources)
	if current.ReconcileOnly {
		refs := []string{current.Volume.PreparationTaskRef}
		if current.TaskRef != current.Volume.PreparationTaskRef {
			refs = append(refs, current.TaskRef)
		}
		for _, ref := range refs {
			maximum := int32(5)
			if ref == current.TaskRef {
				maximum = current.Attempt - 1
			}
			for attempt := int32(1); attempt <= maximum; attempt++ {
				resource, err := controller.workspaceVolumePredecessor(ctx, current, ref, attempt)
				if err != nil {
					return nil, err
				}
				if resource != nil {
					previous = append(previous, *resource)
					known[string(resource.job.UID)] = *resource
				}
			}
		}
	}
	continuation := ""
	for page := 0; page < 10; page++ {
		pods, err := controller.client.CoreV1().Pods(exactWorkerNamespace).List(ctx, metav1.ListOptions{Limit: 100, Continue: continuation})
		if err != nil || pods == nil {
			return nil, errWorkspaceVolumePrepareUnconfirmed
		}
		for index := range pods.Items {
			pod := &pods.Items[index]
			for _, volume := range pod.Spec.Volumes {
				if volume.PersistentVolumeClaim == nil || volume.PersistentVolumeClaim.ClaimName != wanted.Name {
					continue
				}
				owner := metav1.GetControllerOf(pod)
				if owner == nil || owner.Kind != "Job" || owner.APIVersion != "batch/v1" || len(pod.OwnerReferences) != 1 {
					return nil, errWorkspaceVolumePrepareUnconfirmed
				}
				if own != nil && owner.UID == own.UID && owner.Name == own.Name {
					continue
				}
				resource, exists := known[string(owner.UID)]
				if !exists || owner.Name != resource.job.Name {
					return nil, errWorkspaceVolumePrepareUnconfirmed
				}
				expected, err := controller.workspaceVolumePreparationJob(resource.work, resource.input)
				if err != nil || !matchingWorkspaceVolumePod(expected, resource.job, pod) || !joinedWorkspaceVolumePreparation(resource.job, pod) {
					return nil, errWorkspaceVolumePrepareUnconfirmed
				}
			}
		}
		if pods.Continue == "" {
			return previous, nil
		}
		if pods.Continue == continuation {
			return nil, errWorkspaceVolumePrepareUnconfirmed
		}
		continuation = pods.Continue
	}
	return nil, errWorkspaceVolumePrepareUnconfirmed
}
