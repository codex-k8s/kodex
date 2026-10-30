package controller

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// workspaceVolumeRenewInterval bounds owner revalidation during read-only waiting; every new
// Kubernetes Create still requires a separate immediate renewal regardless of this interval.
// workspaceVolumeRenewInterval ограничивает интервал owner revalidation при read-only ожидании;
// каждый новый Kubernetes Create всё равно требует отдельного немедленного renewal.
const workspaceVolumeRenewInterval = 20 * time.Second

// WorkspaceVolumePreparationResult carries only a positively joined physical fact; it contains
// no model lease, grant fence or owner-selected business state.
// WorkspaceVolumePreparationResult содержит только positively joined physical fact, без model
// lease, grant fence либо owner-selected business state.
type WorkspaceVolumePreparationResult struct {
	Ready          *runtimecontract.WorkspaceVolumeReady
	JobUID, PodUID string
}

// workspaceVolumeAttemptResources binds one immutable input and Job to the exact typed attempt;
// predecessor entries establish quiescence only and do not recreate their old grants.
// workspaceVolumeAttemptResources связывает immutable input и Job с exact typed attempt;
// predecessor entries доказывают только quiescence и не восстанавливают старые grants.
type workspaceVolumeAttemptResources struct {
	work  runtimecontract.WorkspaceVolumeWork
	input *corev1.ConfigMap
	job   *batchv1.Job
}

// WorkspaceVolumeResources retains positive current Create identities and validated predecessor
// envelopes until owner acceptance. Completed Job/input tombstones remain to reject delayed first
// initialization under the same names; pod release never deletes the PVC or user bytes.
// WorkspaceVolumeResources хранит positive current Create identities и validated predecessor
// envelopes до принятия owner. Completed Job/input tombstones сохраняются, отклоняя delayed first
// initialization с прежними именами; освобождение Pod не удаляет PVC либо пользовательские bytes.
type WorkspaceVolumeResources struct {
	controller   *Controller
	current      workspaceVolumeAttemptResources
	predecessors []workspaceVolumeAttemptResources
}

// ConfirmedCreates reports this invocation's positive input/Job API responses independently of
// its final outcome; read-only predecessor observations are not counted as new external effects.
// ConfirmedCreates сообщает positive input/Job API responses текущего вызова независимо от final
// outcome; read-only predecessor observations не считаются новыми external effects.
func (resources *WorkspaceVolumeResources) ConfirmedCreates() (bool, bool) {
	return resources != nil && resources.current.input != nil, resources != nil && resources.current.job != nil
}

// PrepareWorkspaceVolume materializes one new credential-free attempt after fresh owner renewal
// and a complete predecessor/consumer barrier. It never adopts an unknown Create response and
// returns retained identities even on partial failure. Failure may suspend its own Job to stop
// Pods; Job/input/PVC evidence is retained before and after owner ACK.
// PrepareWorkspaceVolume создаёт одну новую credential-free attempt после fresh owner renewal и
// полного predecessor/consumer barrier. Не усыновляет unknown Create response и возвращает retained
// identities даже при partial failure. При отказе свой Job может быть suspended для остановки
// Pods; Job/input/PVC evidence сохраняется до и после owner ACK.
func (controller *Controller) PrepareWorkspaceVolume(lifecycle context.Context, work runtimecontract.WorkspaceVolumeWork, renew func(context.Context) error) (result WorkspaceVolumePreparationResult, resources *WorkspaceVolumeResources, resultErr error) {
	if lifecycle == nil || lifecycle.Err() != nil || work.Kind != runtimecontract.WorkspaceVolumePrepare {
		return WorkspaceVolumePreparationResult{}, nil, errWorkspaceVolumePrepareUnconfirmed
	}
	if _, err := controller.workspaceVolumePVC(work); err != nil {
		return WorkspaceVolumePreparationResult{}, nil, errWorkspaceVolumePrepareUnconfirmed
	}
	wanted, err := workspaceVolumePreparationInput(work)
	if err != nil {
		return WorkspaceVolumePreparationResult{}, nil, err
	}
	ctx, cancel := context.WithTimeout(lifecycle, controller.config.WorkerTimeout)
	defer cancel()
	resources = &WorkspaceVolumeResources{controller: controller, current: workspaceVolumeAttemptResources{work: work}}
	defer func() {
		if resultErr == nil || resources.current.job == nil {
			return
		}
		// Preserve a completed late physical fact before stopping a cancelled helper. Owner ACK
		// independently decides whether this fact is historical or may admit current READY.
		// Сохраняем completed late physical fact до остановки отменённого helper. Owner ACK
		// отдельно решает, является ли факт historical либо может допустить current READY.
		read, stop := context.WithTimeout(context.WithoutCancel(lifecycle), 5*time.Second)
		previous, barrierErr := controller.workspaceVolumeConsumerBarrier(read, work, resources.current.job)
		if barrierErr == nil {
			ready, podUID, err := controller.observeWorkspaceVolumePreparation(read, work, resources.current.input, resources.current.job)
			if err == nil {
				resources.predecessors = previous
				result, resultErr = WorkspaceVolumePreparationResult{Ready: ready, JobUID: string(resources.current.job.UID), PodUID: podUID}, nil
				stop()
				return
			}
		}
		stop()
		if err := resources.suspendAfterFailure(context.WithoutCancel(lifecycle)); err != nil {
			resultErr = errWorkspaceVolumePrepareUnconfirmed
		}
	}()
	lastRenewal := time.Time{}
	refresh := func(force bool) error {
		if force || lastRenewal.IsZero() || time.Since(lastRenewal) >= workspaceVolumeRenewInterval {
			if err := renewAuthority(ctx, renew); err != nil {
				return err
			}
			lastRenewal = time.Now()
		}
		return ctx.Err()
	}
	ticker := time.NewTicker(controller.poll)
	defer ticker.Stop()
	for {
		if err := refresh(false); err != nil {
			return WorkspaceVolumePreparationResult{}, resources, errWorkspaceVolumePrepareUnconfirmed
		}
		previous, err := controller.workspaceVolumeConsumerBarrier(ctx, work, nil)
		if err == nil {
			resources.predecessors = previous
			break
		}
		if !errors.Is(err, errWorkspaceVolumePreparePending) {
			return WorkspaceVolumePreparationResult{}, resources, err
		}
		select {
		case <-ctx.Done():
			return WorkspaceVolumePreparationResult{}, resources, errWorkspaceVolumePrepareUnconfirmed
		case <-ticker.C:
		}
	}
	if err := refresh(true); err != nil {
		return WorkspaceVolumePreparationResult{}, resources, errWorkspaceVolumePrepareUnconfirmed
	}
	input, err := controller.client.CoreV1().ConfigMaps(exactWorkerNamespace).Create(ctx, wanted, metav1.CreateOptions{})
	if err != nil || input == nil || input.UID == "" || input.ResourceVersion == "" || input.Name != wanted.Name || input.Namespace != exactWorkerNamespace {
		return WorkspaceVolumePreparationResult{}, resources, errWorkspaceVolumePrepareUnconfirmed
	}
	resources.current.input = input
	job, err := controller.workspaceVolumePreparationJob(work, input)
	if err != nil || refresh(true) != nil {
		return WorkspaceVolumePreparationResult{}, resources, errWorkspaceVolumePrepareUnconfirmed
	}
	previous, err := controller.workspaceVolumeConsumerBarrier(ctx, work, nil)
	if err != nil {
		return WorkspaceVolumePreparationResult{}, resources, err
	}
	resources.predecessors = previous
	if err := refresh(true); err != nil {
		return WorkspaceVolumePreparationResult{}, resources, errWorkspaceVolumePrepareUnconfirmed
	}
	created, err := controller.client.BatchV1().Jobs(exactWorkerNamespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil || created == nil || created.UID == "" || created.ResourceVersion == "" || created.Name != job.Name || created.Namespace != exactWorkerNamespace {
		return WorkspaceVolumePreparationResult{}, resources, errWorkspaceVolumePrepareUnconfirmed
	}
	resources.current.job = created
	if !matchingWorkspaceVolumeJob(job, created) {
		return WorkspaceVolumePreparationResult{}, resources, errWorkspaceVolumePrepareUnconfirmed
	}
	for {
		if err := refresh(false); err != nil {
			return WorkspaceVolumePreparationResult{}, resources, errWorkspaceVolumePrepareUnconfirmed
		}
		previous, err := controller.workspaceVolumeConsumerBarrier(ctx, work, created)
		if err != nil && !errors.Is(err, errWorkspaceVolumePreparePending) {
			return WorkspaceVolumePreparationResult{}, resources, err
		}
		if err == nil {
			resources.predecessors = previous
			ready, podUID, observeErr := controller.observeWorkspaceVolumePreparation(ctx, work, input, created)
			if observeErr == nil {
				return WorkspaceVolumePreparationResult{Ready: ready, JobUID: string(created.UID), PodUID: podUID}, resources, nil
			}
			if !errors.Is(observeErr, errWorkspaceVolumePreparePending) {
				return WorkspaceVolumePreparationResult{}, resources, observeErr
			}
		}
		select {
		case <-ctx.Done():
			return WorkspaceVolumePreparationResult{}, resources, errWorkspaceVolumePrepareUnconfirmed
		case <-ticker.C:
		}
	}
}
