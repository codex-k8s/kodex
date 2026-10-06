package admissioncontroller

import (
	"context"
	"errors"
	batchv1 "k8s.io/api/batch/v1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Proof относится к original claim receipt и завершённой owner attempt,
// а не к текущему latest build или к отказу устаревшего callback.
type RecoveryTerminalProof struct {
	RunID, State, ArtifactRef, BuildRef, AttemptRef        string
	Attempt                                                uint32
	ClaimVersion, ClaimFence, ClaimGeneration              uint64
	TerminalVersion, TerminalFence, TerminalAttemptVersion uint64
}

type RecoveryTerminalSource interface {
	GetRecoveryTerminal(context.Context, string) (RecoveryTerminalProof, error)
}

func validRecoveryTerminal(proof RecoveryTerminalProof, runID string) bool {
	return proof.RunID == runID && slicesContains([]string{"ACCEPTED", "REJECTED", "FAILED", "CANCELLED"}, proof.State) &&
		proof.ArtifactRef != "" && proof.BuildRef != "" && proof.AttemptRef != "" && proof.Attempt > 0 && proof.ClaimVersion > 0 && proof.ClaimFence > 0 && proof.ClaimGeneration > 0 &&
		proof.TerminalVersion > proof.ClaimVersion && proof.TerminalFence >= proof.ClaimFence && proof.TerminalAttemptVersion > 1
}

func (controller *Controller) cleanupOwnerTerminalRecovery(ctx context.Context, workspace *corev1.PersistentVolumeClaim, runID string, originalJobs []batchv1.Job) (bool, error) {
	source, available := controller.config.WorkSource.(RecoveryTerminalSource)
	if !available {
		return false, nil
	}
	proof, err := source.GetRecoveryTerminal(ctx, runID)
	// Ни denial, ни отсутствие/повреждение ответа не разрешают delete.
	if err != nil {
		return false, err
	}
	if !validRecoveryTerminal(proof, runID) {
		return false, errors.New("image admission recovery terminal proof is invalid")
	}
	fresh, err := controller.client.CoreV1().PersistentVolumeClaims(controller.config.Namespace).Get(ctx, workspace.Name, metav1.GetOptions{})
	if err != nil || fresh.UID != workspace.UID || fresh.ResourceVersion != workspace.ResourceVersion || !validManagedWorkspace(fresh, controller.config.Namespace) || fresh.Annotations[runIDAnnotation] != runID ||
		fresh.Annotations[recoveryUIDAnnotation] != workspace.Annotations[recoveryUIDAnnotation] || fresh.Annotations[recoveryAfterAnnotation] != workspace.Annotations[recoveryAfterAnnotation] || fresh.ResourceVersion == "" {
		return false, errors.New("image admission recovery workspace conflicts")
	}
	jobs, err := controller.client.BatchV1().Jobs(controller.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: orchestratedSelector(), Limit: 512})
	if err != nil || jobs.Continue != "" {
		return false, errors.New("read image admission recovery terminal inventory")
	}
	original := make(map[string]batchv1.Job)
	for i := range originalJobs {
		job := originalJobs[i]
		if job.Labels[idLabel] == workspace.Labels[idLabel] && job.Labels[phaseLabel] != "promote" {
			original[job.Name] = job
		}
	}
	for i := range jobs.Items {
		job := &jobs.Items[i]
		if job.Labels[idLabel] != workspace.Labels[idLabel] || job.Labels[phaseLabel] == "promote" {
			continue
		}
		before, exists := original[job.Name]
		if !exists || before.UID != job.UID || before.ResourceVersion != job.ResourceVersion {
			return false, errors.New("image admission recovery terminal job changed")
		}
		if !validManagedJob(job, controller.config.Namespace, job.Labels[phaseLabel]) || job.Annotations[runIDAnnotation] != runID || !jobTerminal(job) || job.ResourceVersion == "" {
			return false, errors.New("image admission recovery terminal inventory conflicts")
		}
		if job.Labels[phaseLabel] == "admit" && string(job.UID) != workspace.Annotations[recoveryUIDAnnotation] && job.Annotations[failedPredecessorUID] != workspace.Annotations[recoveryUIDAnnotation] {
			return false, errors.New("image admission recovery terminal lineage conflicts")
		}
	}
	for i := range jobs.Items {
		job := &jobs.Items[i]
		if job.Labels[idLabel] != workspace.Labels[idLabel] || job.Labels[phaseLabel] == "promote" {
			continue
		}
		uid, version := job.UID, job.ResourceVersion
		propagation := metav1.DeletePropagationBackground
		if err := controller.client.BatchV1().Jobs(controller.config.Namespace).Delete(ctx, job.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}, PropagationPolicy: &propagation}); err != nil && !apierrors.IsNotFound(err) {
			return false, errors.New("delete owner terminal image admission job")
		}
	}
	uid, version := fresh.UID, fresh.ResourceVersion
	if err := controller.client.CoreV1().PersistentVolumeClaims(controller.config.Namespace).Delete(ctx, fresh.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}}); err != nil && !apierrors.IsNotFound(err) {
		return false, errors.New("delete owner terminal image admission workspace")
	}
	controller.lastAdmissionAttempt = controller.now().UTC()
	return true, nil
}
