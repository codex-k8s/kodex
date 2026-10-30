package controller

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

// workspaceVolumeObserverFixture retains exact synthetic Kubernetes incarnations for the real
// read-only observer; these objects are neither native execution nor owner acceptance evidence.
// workspaceVolumeObserverFixture хранит exact synthetic Kubernetes incarnations для настоящего
// read-only observer; объекты не являются native execution либо owner acceptance evidence.
type workspaceVolumeObserverFixture struct {
	client     *fake.Clientset
	controller *Controller
	work       runtimecontract.WorkspaceVolumeWork
	input      *corev1.ConfigMap
	job        *batchv1.Job
	pod        *corev1.Pod
	pvc        *corev1.PersistentVolumeClaim
	ready      runtimecontract.WorkspaceVolumeReady
}

// newWorkspaceVolumeObserverFixture builds a complete successful shape from the production
// renderer, positive fixture UIDs and a canonical record while keeping all setup in fake storage.
// newWorkspaceVolumeObserverFixture строит successful shape из production renderer, positive
// fixture UIDs и canonical record; подготовка остаётся целиком в fake storage.
func newWorkspaceVolumeObserverFixture(t *testing.T) workspaceVolumeObserverFixture {
	t.Helper()
	client, controller := workspaceVolumeCreateControllerFixture(t)
	work, input := workspaceVolumePreparationFixture(t, 1, false)
	job, err := controller.workspaceVolumePreparationJob(work, input)
	if err != nil {
		t.Fatal("Job fixture rejected")
	}
	job.UID, job.ResourceVersion = "30000000-0000-4000-8000-000000000001", "1"
	job.Status = batchv1.JobStatus{Succeeded: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}}
	pvc, err := controller.workspaceVolumePVC(work)
	if err != nil {
		t.Fatal("PVC fixture rejected")
	}
	pvc.UID, pvc.ResourceVersion, pvc.Spec.VolumeName, pvc.Status.Phase = "10000000-0000-4000-8000-000000000001", "1", "fixture-pv", corev1.ClaimBound
	preparation, _ := work.Preparation()
	ready := runtimecontract.WorkspaceVolumeReady{Schema: runtimecontract.WorkspaceVolumeReadySchema, Preparation: preparation, Device: 1, Inode: 2}
	raw, err := runtimecontract.EncodeWorkspaceVolumeReady(ready)
	if err != nil {
		t.Fatal("ready fixture rejected")
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "fixture-volume-pod", Namespace: exactWorkerNamespace,
		UID: "40000000-0000-4000-8000-000000000001", ResourceVersion: "1", Labels: map[string]string{"job-name": job.Name},
		Annotations: job.Spec.Template.Annotations, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(job, batchv1.SchemeGroupVersion.WithKind("Job"))}},
		Spec: *job.Spec.Template.Spec.DeepCopy(), Status: corev1.PodStatus{Phase: corev1.PodSucceeded,
			ContainerStatuses: []corev1.ContainerStatus{{Name: "volume", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 0, Message: string(raw)}}}}}}
	return workspaceVolumeObserverFixture{client: client, controller: controller, work: work, input: input, job: job, pod: pod, pvc: pvc, ready: ready}
}

// store installs only fixture API objects without manufacturing persisted platform state or
// claiming a successful effect; the observer itself is still required to inspect every object.
// store помещает только fixture API objects без готовых platform states либо заявления successful
// effect; observer по-прежнему должен проверить каждый объект.
func (fixture workspaceVolumeObserverFixture) store(t *testing.T) {
	t.Helper()
	for _, object := range []runtime.Object{fixture.input, fixture.pvc, fixture.job, fixture.pod} {
		if err := fixture.client.Tracker().Add(object); err != nil {
			t.Fatal("observer API fixture storage failed")
		}
	}
}

// TestWorkspaceVolumeObserverRequiresCompleteJoin checks a valid read-only API join and rejects
// borrowed incarnations, mutated geometry and partial/failed/missing container observations.
// TestWorkspaceVolumeObserverRequiresCompleteJoin проверяет valid read-only API join и отклоняет
// borrowed incarnations, mutated geometry и partial/failed/missing container observations.
func TestWorkspaceVolumeObserverRequiresCompleteJoin(t *testing.T) {
	fixture := newWorkspaceVolumeObserverFixture(t)
	fixture.store(t)
	observed, podUID, err := fixture.controller.observeWorkspaceVolumePreparation(t.Context(), fixture.work, fixture.input.DeepCopy(), fixture.job.DeepCopy())
	if err != nil || observed == nil || *observed != fixture.ready || podUID != string(fixture.pod.UID) {
		t.Fatal("complete exact observer join lost its physical tuple")
	}
	for _, action := range fixture.client.Actions() {
		if action.GetVerb() != "get" && action.GetVerb() != "list" {
			t.Fatal("read-only observation performed a physical mutation")
		}
	}
	for name, mutate := range map[string]func(*workspaceVolumeObserverFixture){
		"borrowed-input-uid": func(f *workspaceVolumeObserverFixture) { f.input.UID = "50000000-0000-4000-8000-000000000001" },
		"borrowed-job-uid":   func(f *workspaceVolumeObserverFixture) { f.job.UID = "50000000-0000-4000-8000-000000000001" },
		"borrowed-pvc":       func(f *workspaceVolumeObserverFixture) { f.pvc.UID = "50000000-0000-4000-8000-000000000001" },
		"lost-pvc":           func(f *workspaceVolumeObserverFixture) { f.pvc.Status.Phase = corev1.ClaimLost },
		"foreign-pod-owner": func(f *workspaceVolumeObserverFixture) {
			f.pod.OwnerReferences[0].UID = "50000000-0000-4000-8000-000000000001"
		},
		"borrowed-input": func(f *workspaceVolumeObserverFixture) {
			f.input.Annotations[workspaceVolumeInputAnnotation] = "foreign"
		},
		"missing-container": func(f *workspaceVolumeObserverFixture) { f.pod.Status.ContainerStatuses = nil },
		"running-pod":       func(f *workspaceVolumeObserverFixture) { f.pod.Status.Phase = corev1.PodRunning },
		"failed-container": func(f *workspaceVolumeObserverFixture) {
			f.pod.Status.ContainerStatuses[0].State.Terminated.ExitCode = 1
		},
		"extra-init-container": func(f *workspaceVolumeObserverFixture) {
			f.pod.Spec.InitContainers = []corev1.Container{{Name: "foreign"}}
		},
		"malformed-record": func(f *workspaceVolumeObserverFixture) {
			f.pod.Status.ContainerStatuses[0].State.Terminated.Message = "{}"
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := newWorkspaceVolumeObserverFixture(t)
			originalInput, originalJob := changed.input.DeepCopy(), changed.job.DeepCopy()
			mutate(&changed)
			changed.store(t)
			ready, uid, err := changed.controller.observeWorkspaceVolumePreparation(t.Context(), changed.work, originalInput, originalJob)
			if ready != nil || uid != "" || !errors.Is(err, errWorkspaceVolumePrepareUnconfirmed) && !errors.Is(err, errWorkspaceVolumePreparePending) {
				t.Fatal("partial or borrowed evidence acquired READY")
			}
		})
	}
}
