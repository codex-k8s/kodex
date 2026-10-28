package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

// workspaceVolumeExecutionFixture prepares an exact fake PVC and assigns positive API identities
// to newly created inputs/Jobs; it does not run a container or write platform database state.
// workspaceVolumeExecutionFixture готовит exact fake PVC и назначает positive API identities новым
// inputs/Jobs; fixture не запускает container и не пишет platform database state.
func workspaceVolumeExecutionFixture(t *testing.T, work runtimecontract.WorkspaceVolumeWork) (*fake.Clientset, *Controller) {
	t.Helper()
	client, controller := workspaceVolumeCreateControllerFixture(t)
	controller.poll = time.Millisecond
	pvc, err := controller.workspaceVolumePVC(work)
	if err != nil {
		t.Fatal("execution PVC fixture rejected")
	}
	pvc.UID, pvc.ResourceVersion, pvc.Spec.VolumeName, pvc.Status.Phase = "10000000-0000-4000-8000-000000000001", "1", "fixture-pv", corev1.ClaimBound
	if err := client.Tracker().Add(pvc); err != nil {
		t.Fatal("execution PVC fixture storage failed")
	}
	client.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		input := action.(ktesting.CreateAction).GetObject().(*corev1.ConfigMap).DeepCopy()
		input.UID, input.ResourceVersion = "20000000-0000-4000-8000-000000000001", "1"
		if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("configmaps"), input, exactWorkerNamespace); err != nil {
			return true, nil, err
		}
		return true, input, nil
	})
	return client, controller
}

// TestWorkspaceVolumeExecutionRetainsInitializerTombstones verifies the full fake-client call
// path, renewal before each Create and Pod-only release after ACK, retaining the original input,
// completed Job and PVC against a delayed first initialization.
// TestWorkspaceVolumeExecutionRetainsInitializerTombstones проверяет полный fake-client call path,
// renewal перед каждым Create и Pod-only release после ACK. Original input, completed Job и PVC
// сохраняются против delayed first initialization.
func TestWorkspaceVolumeExecutionRetainsInitializerTombstones(t *testing.T) {
	work, _ := workspaceVolumePreparationFixture(t, 1, false)
	client, controller := workspaceVolumeExecutionFixture(t, work)
	renewals := 0
	client.PrependReactor("create", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		if renewals < 3 {
			t.Fatal("physical Create occurred without fresh owner revalidation")
		}
		job := action.(ktesting.CreateAction).GetObject().(*batchv1.Job).DeepCopy()
		job.UID, job.ResourceVersion = "30000000-0000-4000-8000-000000000001", "1"
		job.Status = batchv1.JobStatus{Succeeded: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}}
		preparation, _ := work.Preparation()
		raw, err := runtimecontract.EncodeWorkspaceVolumeReady(runtimecontract.WorkspaceVolumeReady{
			Schema: runtimecontract.WorkspaceVolumeReadySchema, Preparation: preparation, Device: 1, Inode: 2})
		if err != nil {
			t.Fatal("execution ready fixture rejected")
		}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "fixture-helper-pod", Namespace: exactWorkerNamespace,
			UID: "40000000-0000-4000-8000-000000000001", ResourceVersion: "1", Labels: map[string]string{"job-name": job.Name},
			Annotations: job.Spec.Template.Annotations, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(job, batchv1.SchemeGroupVersion.WithKind("Job"))}},
			Spec: *job.Spec.Template.Spec.DeepCopy(), Status: corev1.PodStatus{Phase: corev1.PodSucceeded,
				ContainerStatuses: []corev1.ContainerStatus{{Name: "volume", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, Message: string(raw)}}}}}}
		if err := client.Tracker().Add(job); err != nil {
			return true, nil, err
		}
		if err := client.Tracker().Add(pod); err != nil {
			return true, nil, err
		}
		return true, job, nil
	})
	result, retained, err := controller.PrepareWorkspaceVolume(t.Context(), work, func(context.Context) error { renewals++; return nil })
	if err != nil || retained == nil || result.Ready == nil || result.PodUID == "" || result.JobUID == "" {
		t.Fatal("exact physical execution shape did not reach joined observation")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "delete" || action.GetVerb() == "patch" {
			t.Fatal("physical evidence was removed before owner acknowledgement")
		}
	}
	if err := retained.ReleaseAfterAcknowledgement(t.Context()); err != nil {
		t.Fatal("exact joined Pod release failed")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "delete" && action.GetResource().Resource != "pods" {
			t.Fatal("acknowledgement deleted an initializer tombstone or PVC")
		}
	}
	for _, resource := range []string{"jobs", "configmaps"} {
		group := corev1.SchemeGroupVersion.WithResource(resource)
		if resource == "jobs" {
			group = batchv1.SchemeGroupVersion.WithResource(resource)
		}
		if _, err := client.Tracker().Get(group, exactWorkerNamespace, retained.current.input.Name); err != nil {
			t.Fatal("initializer tombstone was lost after acknowledgement")
		}
	}
}

// TestWorkspaceVolumeReconciliationRequiresOriginalInitializer rejects a missing first Job
// without creating another input or initializer, even if the current lease can still renew.
// TestWorkspaceVolumeReconciliationRequiresOriginalInitializer отклоняет missing first Job без
// создания другого input/initializer, даже если current lease ещё можно продлевать.
func TestWorkspaceVolumeReconciliationRequiresOriginalInitializer(t *testing.T) {
	work, _ := workspaceVolumePreparationFixture(t, 2, false)
	client, controller := workspaceVolumeExecutionFixture(t, work)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	result, resources, err := controller.PrepareWorkspaceVolume(ctx, work, func(context.Context) error { return nil })
	if err == nil || result.Ready != nil || resources == nil {
		t.Fatal("missing original initialization became a ready root")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" && action.GetVerb() != "list" {
			t.Fatal("read-only recovery manufactured another physical write")
		}
	}
}

// TestWorkspaceVolumeUnknownJobCreateRetainsInput checks that an uncertain Job response neither
// adopts by name nor deletes its immutable input, PVC or unknown workload.
// TestWorkspaceVolumeUnknownJobCreateRetainsInput проверяет, что unknown Job response не приводит
// к adoption по имени либо удалению immutable input, PVC или неизвестного workload.
func TestWorkspaceVolumeUnknownJobCreateRetainsInput(t *testing.T) {
	work, _ := workspaceVolumePreparationFixture(t, 1, false)
	client, controller := workspaceVolumeExecutionFixture(t, work)
	client.PrependReactor("create", "jobs", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("fixture uncertain Job response")
	})
	result, retained, err := controller.PrepareWorkspaceVolume(t.Context(), work, func(context.Context) error { return nil })
	if err == nil || retained == nil || result.Ready != nil {
		t.Fatal("unknown Job response was accepted")
	}
	input, job := retained.ConfirmedCreates()
	if !input || job {
		t.Fatal("unknown Job was counted as a positive physical response")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "delete" || action.GetVerb() == "patch" {
			t.Fatal("unknown workload was adopted for cleanup")
		}
	}
}
