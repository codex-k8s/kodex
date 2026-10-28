package controller

import (
	"strconv"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// workspaceVolumePreparationFixture supplies an already acknowledged input incarnation for
// renderer tests; it does not simulate the missing physical Create, owner grant or native join.
// workspaceVolumePreparationFixture задаёт подтверждённую input incarnation для renderer tests;
// fixture не имитирует отсутствующие physical Create, owner grant либо native join.
func workspaceVolumePreparationFixture(t *testing.T, attempt int32, recovery bool) (runtimecontract.WorkspaceVolumeWork, *corev1.ConfigMap) {
	t.Helper()
	work := workspaceVolumeCreateFixture()
	work.Kind, work.TaskRef, work.Attempt = runtimecontract.WorkspaceVolumePrepare, "svt_prepare000", attempt
	work.Volume.PreparationTaskRef, work.Volume.PVCUID = work.TaskRef, "10000000-0000-4000-8000-000000000001"
	if recovery {
		work.TaskRef, work.Volume.ReadOnly = "svt_recovery00", true
	}
	work.ReconcileOnly = recovery || attempt > 1
	work.InputDigest, _ = work.Digest()
	preparation, err := work.Preparation()
	if err != nil {
		t.Fatal("preparation fixture rejected")
	}
	raw, err := runtimecontract.EncodeWorkspaceVolumePreparation(preparation)
	if err != nil {
		t.Fatal("preparation encoding failed")
	}
	immutable := true
	input := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: exactWorkerNamespace,
		Name: workloadName(work.TaskRef, work.Volume.VolumeGeneration, work.Attempt), UID: "20000000-0000-4000-8000-000000000001", ResourceVersion: "1",
		Labels: map[string]string{managedLabel: "true"}, Annotations: map[string]string{
			workspaceVolumeTaskAnnotation: work.TaskRef, workspaceVolumeInputAnnotation: work.InputDigest,
			workspaceVolumePVCUIDAnnotation: work.Volume.PVCUID, workspaceVolumeAttemptAnnotation: strconv.FormatInt(int64(work.Attempt), 10)}},
		Immutable: &immutable, Data: map[string]string{"volume.json": string(raw)}}
	return work, input
}

// TestWorkspaceVolumePreparationSeparatesFirstWriteFromReconciliation pins FSGroup to the first
// empty-root initialization and denies write mounts to every retry or separate recovery intent.
// TestWorkspaceVolumePreparationSeparatesFirstWriteFromReconciliation закрепляет FSGroup только
// за first empty-root initialization и запрещает write mounts каждой retry/recovery attempt.
func TestWorkspaceVolumePreparationSeparatesFirstWriteFromReconciliation(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		for attempt := int32(1); attempt <= 5; attempt++ {
			client, controller := workspaceVolumeCreateControllerFixture(t)
			work, input := workspaceVolumePreparationFixture(t, attempt, recovery)
			job, err := controller.workspaceVolumePreparationJob(work, input)
			if err != nil || job == nil || len(client.Actions()) != 0 {
				t.Fatal("pure renderer rejected a valid intent or performed an API effect")
			}
			pod := job.Spec.Template.Spec
			if len(pod.Containers) != 1 || len(pod.InitContainers) != 0 || len(pod.Volumes) != 3 ||
				pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken ||
				job.Spec.TTLSecondsAfterFinished != nil || job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
				t.Fatal("helper received credentials, automatic retries or cleanup before acknowledgement")
			}
			if work.ReconcileOnly {
				if pod.SecurityContext.FSGroup != nil || pod.SecurityContext.FSGroupChangePolicy != nil ||
					!pod.Volumes[1].PersistentVolumeClaim.ReadOnly || !pod.Containers[0].VolumeMounts[1].ReadOnly ||
					pod.Containers[0].Args[0] != "volume-readback" {
					t.Fatal("reconciliation acquired recursive FSGroup or root write access")
				}
			} else if pod.SecurityContext.FSGroup == nil || *pod.SecurityContext.FSGroup != 29000 ||
				pod.SecurityContext.FSGroupChangePolicy == nil || *pod.SecurityContext.FSGroupChangePolicy != corev1.FSGroupChangeAlways ||
				pod.Containers[0].Args[0] != "volume" {
				t.Fatal("first preparation lost its qualified root geometry")
			}
			if job.Annotations[workspaceVolumeInputUIDAnnotation] != string(input.UID) ||
				job.Annotations[workspaceVolumeInputAnnotation] != work.InputDigest {
				t.Fatal("Job lost the positive input incarnation or effective attempt digest")
			}
			if !validWorkspaceHelperPod(pod, *pod.DeepCopy()) {
				t.Fatal("exact helper geometry rejected")
			}
			changed := pod.DeepCopy()
			changed.Containers[0].VolumeMounts[1].ReadOnly = !work.ReconcileOnly
			if validWorkspaceHelperPod(pod, *changed) {
				t.Fatal("observed helper changed its write boundary")
			}
		}
	}
}

// TestWorkspaceVolumePreparationRejectsBorrowedInput prevents a reused name, mutable content or
// mismatched attempt from selecting the preparation; no Kubernetes resources are materialized.
// TestWorkspaceVolumePreparationRejectsBorrowedInput не допускает выбор preparation по reused name,
// mutable content либо mismatched attempt; Kubernetes resources не создаются.
func TestWorkspaceVolumePreparationRejectsBorrowedInput(t *testing.T) {
	for name, mutate := range map[string]func(*corev1.ConfigMap){
		"no-positive-uid": func(input *corev1.ConfigMap) { input.UID = "" },
		"mutable":         func(input *corev1.ConfigMap) { input.Immutable = nil },
		"other-attempt": func(input *corev1.ConfigMap) {
			input.Annotations[workspaceVolumeAttemptAnnotation] = "2"
		},
		"other-snapshot": func(input *corev1.ConfigMap) {
			input.Annotations[workspaceVolumeInputAnnotation] = "foreign"
		},
		"extra-content": func(input *corev1.ConfigMap) { input.Data["other"] = "unexpected" },
	} {
		t.Run(name, func(t *testing.T) {
			client, controller := workspaceVolumeCreateControllerFixture(t)
			work, input := workspaceVolumePreparationFixture(t, 1, false)
			mutate(input)
			if job, err := controller.workspaceVolumePreparationJob(work, input); err == nil || job != nil || len(client.Actions()) != 0 {
				t.Fatal("borrowed input rendered physical preparation")
			}
		})
	}
}
