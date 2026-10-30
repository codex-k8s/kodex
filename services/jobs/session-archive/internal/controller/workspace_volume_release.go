package controller

import (
	"context"
	"encoding/json"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ReleaseAfterAcknowledgement removes only positively joined helper Pods after owner acceptance.
// Immutable inputs and terminal Jobs remain as tombstones against delayed first initialization;
// this method never deletes a PVC, a Job, an input or user data and never renews a closed grant.
// ReleaseAfterAcknowledgement удаляет только positively joined helper Pods после принятия owner.
// Immutable inputs и terminal Jobs остаются tombstones против delayed first initialization;
// метод не удаляет PVC, Job, input либо user data и не продлевает закрытый grant.
func (resources *WorkspaceVolumeResources) ReleaseAfterAcknowledgement(lifecycle context.Context) error {
	if resources == nil || resources.controller == nil || resources.current.input == nil || resources.current.job == nil || lifecycle == nil {
		return errWorkspaceVolumePrepareUnconfirmed
	}
	ctx, cancel := context.WithTimeout(lifecycle, archiveCleanupTimeout)
	defer cancel()
	entries := append([]workspaceVolumeAttemptResources{resources.current}, resources.predecessors...)
	for _, entry := range entries {
		input, err := resources.controller.client.CoreV1().ConfigMaps(exactWorkerNamespace).Get(ctx, entry.input.Name, metav1.GetOptions{})
		if err != nil || input == nil || input.UID != entry.input.UID {
			return errWorkspaceVolumePrepareUnconfirmed
		}
		expected, err := resources.controller.workspaceVolumePreparationJob(entry.work, input)
		if err != nil {
			return err
		}
		job, err := resources.controller.client.BatchV1().Jobs(exactWorkerNamespace).Get(ctx, entry.job.Name, metav1.GetOptions{})
		if err != nil || job == nil || job.UID != entry.job.UID || !matchingWorkspaceVolumeJob(expected, job) || !completedWorkspaceVolumeJob(job) {
			return errWorkspaceVolumePrepareUnconfirmed
		}
		pods, err := resources.controller.client.CoreV1().Pods(exactWorkerNamespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + job.Name, Limit: 2})
		if err != nil || pods == nil || len(pods.Items) > 1 || pods.Continue != "" {
			return errWorkspaceVolumePrepareUnconfirmed
		}
		for index := range pods.Items {
			pod := &pods.Items[index]
			if !matchingWorkspaceVolumePod(expected, job, pod) || !joinedWorkspaceVolumePreparation(job, pod) {
				return errWorkspaceVolumePrepareUnconfirmed
			}
			deleteErr := resources.controller.client.CoreV1().Pods(exactWorkerNamespace).Delete(ctx, pod.Name,
				metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &pod.UID, ResourceVersion: &pod.ResourceVersion}})
			// An uncertain delete is inspected, never blindly repeated with a new incarnation.
			// Неопределённое удаление проверяется чтением, не повторяется вслепую с новой incarnation.
			for {
				observed, err := resources.controller.client.CoreV1().Pods(exactWorkerNamespace).Get(ctx, pod.Name, metav1.GetOptions{})
				if apierrors.IsNotFound(err) {
					break
				}
				if err != nil || observed == nil || observed.UID != pod.UID ||
					deleteErr != nil && observed.DeletionTimestamp == nil {
					return errWorkspaceVolumePrepareUnconfirmed
				}
				select {
				case <-ctx.Done():
					return errWorkspaceVolumePrepareUnconfirmed
				case <-time.After(100 * time.Millisecond):
				}
			}
		}
	}
	return nil
}

// suspendAfterFailure stops only this invocation's positively created Job with a UID/RV CAS.
// An ambiguous patch is read back without a retry; Job/input evidence remains. The controller's
// observed suspended condition and disappearance of all owned Pods close the physical envelope.
// suspendAfterFailure останавливает только positively created Job текущего вызова через UID/RV CAS.
// Неопределённый patch проверяется чтением без retry; Job/input evidence сохраняется. Observed
// suspended condition контроллера и исчезновение всех owned Pods закрывают physical envelope.
func (resources *WorkspaceVolumeResources) suspendAfterFailure(lifecycle context.Context) error {
	if resources == nil || resources.controller == nil || resources.current.job == nil || resources.current.input == nil || lifecycle == nil {
		return errWorkspaceVolumePrepareUnconfirmed
	}
	ctx, cancel := context.WithTimeout(lifecycle, archiveCleanupTimeout)
	defer cancel()
	client := resources.controller.client
	original := resources.current.job
	job, err := client.BatchV1().Jobs(exactWorkerNamespace).Get(ctx, original.Name, metav1.GetOptions{})
	if err != nil || job == nil || job.UID != original.UID || job.ResourceVersion == "" {
		return errWorkspaceVolumePrepareUnconfirmed
	}
	if completedWorkspaceVolumeJob(job) {
		return nil
	}
	if job.Spec.Suspend == nil || !*job.Spec.Suspend {
		patch, err := json.Marshal([]map[string]any{
			{"op": "test", "path": "/metadata/uid", "value": string(job.UID)},
			{"op": "test", "path": "/metadata/resourceVersion", "value": job.ResourceVersion},
			{"op": "add", "path": "/spec/suspend", "value": true},
		})
		if err != nil {
			return errWorkspaceVolumePrepareUnconfirmed
		}
		_, _ = client.BatchV1().Jobs(exactWorkerNamespace).Patch(ctx, job.Name, types.JSONPatchType, patch, metav1.PatchOptions{})
	}
	for {
		current, err := client.BatchV1().Jobs(exactWorkerNamespace).Get(ctx, job.Name, metav1.GetOptions{})
		if err != nil || current == nil || current.UID != original.UID || current.Spec.Suspend == nil || !*current.Spec.Suspend {
			return errWorkspaceVolumePrepareUnconfirmed
		}
		suspended := false
		for _, condition := range current.Status.Conditions {
			if condition.Type == batchv1.JobSuspended && condition.Status == corev1.ConditionTrue {
				suspended = true
			}
		}
		pods, err := client.CoreV1().Pods(exactWorkerNamespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + current.Name, Limit: 2})
		if err != nil || pods == nil || pods.Continue != "" || len(pods.Items) > 1 {
			return errWorkspaceVolumePrepareUnconfirmed
		}
		for index := range pods.Items {
			owner := metav1.GetControllerOf(&pods.Items[index])
			if owner == nil || owner.Name != current.Name || owner.UID != current.UID || owner.Kind != "Job" || owner.APIVersion != "batch/v1" {
				return errWorkspaceVolumePrepareUnconfirmed
			}
		}
		if suspended && len(pods.Items) == 0 && current.Status.Active == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return errWorkspaceVolumePrepareUnconfirmed
		case <-time.After(100 * time.Millisecond):
		}
	}
}
