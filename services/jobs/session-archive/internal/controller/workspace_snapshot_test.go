package controller

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// workspaceTaskFixture creates a closed owner-shaped snapshot for fake-API boundary tests, not live authorization.
// workspaceTaskFixture создаёт closed owner-shaped snapshot для fake-API boundary tests, не live authorization.
func workspaceTaskFixture(t *testing.T) model.Task {
	t.Helper()
	scope := runtimecontract.WorkspaceStorageScope{OrganizationRef: "org_fixture000", ProjectRef: "prj_fixture000", SessionRef: "ses_fixture000", AgentRef: "agt_fixture000"}
	writer := runtimecontract.WorkspaceWriterCommitment{RunRef: "run_fixture000", NodeRef: "node_fixture000", TurnRef: "turn_fixture000",
		RuntimeRevisionRef: "rrev_fixture000", RuntimeRevisionVersion: 1, RuntimeRevisionDigest: strings.Repeat("a", 64),
		ExecutionBindingDigest: strings.Repeat("b", 64), InputDigest: strings.Repeat("c", 64), LeaseRef: "lease_fixture000",
		LeaseGeneration: 1, Attempt: 1, PodUID: "pod-fixture", PVCUID: "source-pvc-uid"}
	joined, err := runtimecontract.EncodeWorkspaceQuiescence(runtimecontract.WorkspaceQuiescence{Schema: runtimecontract.WorkspaceQuiescenceSchema,
		Scope: scope, WorkspaceRef: "wks_fixture000", Generation: 2, Joined: true, Writer: writer})
	if err != nil {
		t.Fatal(err)
	}
	pvc, err := runtimecontract.SessionPVCName(scope.SessionRef)
	if err != nil {
		t.Fatal(err)
	}
	task := model.Task{TaskRef: "sat_fixture000", Kind: "SNAPSHOT", OrganizationRef: scope.OrganizationRef, ProjectRef: scope.ProjectRef,
		SessionRef: scope.SessionRef, RuntimeRevisionRef: writer.RuntimeRevisionRef, RuntimeRevisionVersion: writer.RuntimeRevisionVersion,
		RuntimeRevisionDigest: writer.RuntimeRevisionDigest, ContentGeneration: 7, PVCName: pvc, InputDigest: strings.Repeat("d", 64), Attempt: 1,
		Workspace: &model.WorkspaceArchiveTask{Target: runtimecontract.WorkspaceStorageBinding{Profile: runtimecontract.WorkspaceStorageProfile,
			Scope: scope, WorkspaceRef: "wks_fixture000", Generation: 2}, CheckpointRef: "wcp_fixture000", Quiescence: string(joined),
			PVCUID: writer.PVCUID, Policy: runtimecontract.RuntimeWorkspacePolicyV1()}}
	identity, err := task.Workspace.SnapshotIdentity()
	if err != nil {
		t.Fatal(err)
	}
	task.TargetObjectKey = identity.ObjectPrefix() + "manifest.json"
	if task.Validate() != nil {
		t.Fatal("invalid snapshot fixture")
	}
	return task
}

// TestWorkspaceSnapshotJobSeparatesReadersAndRetainsEvidence rejects mount/identity drift and keeps total worker limits.
// TestWorkspaceSnapshotJobSeparatesReadersAndRetainsEvidence отклоняет mount/identity drift и сохраняет общий лимит worker.
func TestWorkspaceSnapshotJobSeparatesReadersAndRetainsEvidence(t *testing.T) {
	task := workspaceTaskFixture(t)
	controller, _ := New(newArchiveClient(), testConfig())
	job := controller.job(workloadName(task.TaskRef, task.ContentGeneration, task.Attempt), task, types.UID(task.Workspace.PVCUID))
	pod := &corev1.Pod{Spec: *job.Spec.Template.Spec.DeepCopy()}
	if job.Spec.TTLSecondsAfterFinished != nil || len(pod.Spec.Containers) != 2 || len(pod.Spec.InitContainers) != 1 ||
		pod.Spec.SecurityContext.FSGroup != nil || !controller.validWorkspaceWorkerPod(task, job, pod) {
		t.Fatal("snapshot job did not retain evidence and exact reader geometry")
	}
	var memory, cpu int64
	for _, container := range pod.Spec.Containers {
		memory += container.Resources.Limits.Memory().Value()
		cpu += container.Resources.Limits.Cpu().MilliValue()
	}
	if memory != 256<<20 || cpu != 1000 {
		t.Fatal("combined worker limits expanded")
	}
	reader := pod.Spec.Containers[1]
	for _, mount := range reader.VolumeMounts {
		if mount.Name == "object-storage" || mount.MountPath == "/tmp" || mount.Name == "session" && (!mount.ReadOnly || mount.SubPath == "" || mount.MountPath != "/workspace/work") {
			t.Fatal("reader received storage credentials, shared tmp root or writable source")
		}
	}
	for name, mutate := range map[string]func(*corev1.Pod){
		"recursive ownership": func(p *corev1.Pod) { p.Spec.SecurityContext.FSGroup = int64Ptr(29000) },
		"parent mount":        func(p *corev1.Pod) { p.Spec.Containers[1].VolumeMounts[0].SubPath = "" },
		"writable source":     func(p *corev1.Pod) { p.Spec.Containers[1].VolumeMounts[0].ReadOnly = false },
		"reader credentials":  func(p *corev1.Pod) { p.Spec.Containers[1].EnvFrom = []corev1.EnvFromSource{{Prefix: "FOREIGN"}} },
		"root reader":         func(p *corev1.Pod) { p.Spec.Containers[1].SecurityContext.RunAsUser = int64Ptr(0) },
		"foreign image":       func(p *corev1.Pod) { p.Spec.Containers[1].Image = "foreign" },
		"private source":      func(p *corev1.Pod) { p.Spec.Volumes[3].PersistentVolumeClaim.ClaimName = "foreign" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := pod.DeepCopy()
			mutate(changed)
			if controller.validWorkspaceWorkerPod(task, job, changed) {
				t.Fatal("altered worker boundary accepted")
			}
		})
	}
}

// TestWorkspaceSourceRequiresExactPVCAndNoOtherConsumers rejects replacement and terminal Pods before creating a worker.
// TestWorkspaceSourceRequiresExactPVCAndNoOtherConsumers отклоняет replacement и terminal Pods до создания worker.
func TestWorkspaceSourceRequiresExactPVCAndNoOtherConsumers(t *testing.T) {
	for _, scenario := range []string{"replacement", "terminal consumer", "unavailable consumer"} {
		t.Run(scenario, func(t *testing.T) {
			task := workspaceTaskFixture(t)
			pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: task.PVCName, Namespace: exactWorkerNamespace, UID: "source-pvc-uid"}}
			if scenario == "replacement" {
				pvc.UID = "replacement"
			}
			client := newArchiveClient(pvc)
			if scenario != "replacement" {
				pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "previous-writer", Namespace: exactWorkerNamespace, UID: "previous"},
					Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "work", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: task.PVCName}}}}}}
				if scenario == "terminal consumer" {
					pod.Status.Phase = corev1.PodSucceeded
				}
				if err := client.Tracker().Add(pod); err != nil {
					t.Fatal(err)
				}
			}
			controller, _ := New(client, testConfig())
			if _, _, err := controller.ExecuteRetained(t.Context(), task, func(context.Context) error { return nil }); err == nil {
				t.Fatal("unconfirmed source accepted")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() == "create" || action.GetVerb() == "delete" {
					t.Fatal("source rejection changed resources")
				}
			}
		})
	}
}

// TestWorkspaceCancellationRetainsResourcesUntilAcknowledgement exercises the actual Execute defer and exact cleanup.
// TestWorkspaceCancellationRetainsResourcesUntilAcknowledgement проверяет actual Execute defer и exact cleanup.
func TestWorkspaceCancellationRetainsResourcesUntilAcknowledgement(t *testing.T) {
	task := workspaceTaskFixture(t)
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: task.PVCName, Namespace: exactWorkerNamespace, UID: "source-pvc-uid"}}
	client := newArchiveClient(pvc)
	controller, _ := New(client, testConfig())
	controller.poll = time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	retained := make(chan *RetainedAttempt, 1)
	go func() {
		_, attempt, _ := controller.ExecuteRetained(ctx, task, func(context.Context) error { return nil })
		retained <- attempt
	}()
	name := workloadName(task.TaskRef, task.ContentGeneration, task.Attempt)
	waitForJob(t, client, name)
	cancel()
	var attempt *RetainedAttempt
	select {
	case attempt = <-retained:
	case <-time.After(time.Second):
		t.Fatal("cancel did not join controller")
	}
	if attempt == nil {
		t.Fatal("cancel lost exact cleanup identity")
	}
	if _, err := client.BatchV1().Jobs(exactWorkerNamespace).Get(t.Context(), name, metav1.GetOptions{}); err != nil {
		t.Fatal("Job removed before owner ACK")
	}
	if _, err := client.CoreV1().ConfigMaps(exactWorkerNamespace).Get(t.Context(), name, metav1.GetOptions{}); err != nil {
		t.Fatal("input removed before owner ACK")
	}
	if err := attempt.ReleaseAfterAcknowledgement(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertWorkerResourcesRemoved(t, client, name)
	if _, err := client.CoreV1().PersistentVolumeClaims(exactWorkerNamespace).Get(t.Context(), task.PVCName, metav1.GetOptions{}); err != nil {
		t.Fatal("cleanup deleted source PVC")
	}
}

// TestWorkspaceRetryJoinsWorkloadButKeepsInputAndRootKey preserves custody until the successor ACK without new PUT keys.
// TestWorkspaceRetryJoinsWorkloadButKeepsInputAndRootKey сохраняет custody до successor ACK без новых PUT keys.
func TestWorkspaceRetryJoinsWorkloadButKeepsInputAndRootKey(t *testing.T) {
	previous := workspaceTaskFixture(t)
	client := newArchiveClient()
	controller, _ := New(client, testConfig())
	input, job := attemptResources(t, controller, previous)
	if err := client.Tracker().Add(input); err != nil {
		t.Fatal(err)
	}
	if err := client.Tracker().Add(job); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(previous)
	current, err := model.DecodeTask(raw)
	if err != nil {
		t.Fatal(err)
	}
	current.Attempt, current.InputDigest, current.Workspace.ReconcileOnly = 2, strings.Repeat("e", 64), true
	if current.Validate() != nil || !validPredecessorInput(current, 1, input) {
		t.Fatal("valid read-only retry rejected")
	}
	changed := current
	changed.TargetObjectKey += "other"
	if validPredecessorInput(changed, 1, input) {
		t.Fatal("retry changed root object custody")
	}
	if err := controller.cleanupPredecessors(t.Context(), current, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := client.BatchV1().Jobs(exactWorkerNamespace).Get(t.Context(), job.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("old worker not joined")
	}
	if _, err := client.CoreV1().ConfigMaps(exactWorkerNamespace).Get(t.Context(), input.Name, metav1.GetOptions{}); err != nil {
		t.Fatal("old input removed before successor ACK")
	}
}

// TestUnsupportedWorkspaceActionsCannotFallThroughToLegacy prevents destructive legacy behavior before full consumers exist.
// TestUnsupportedWorkspaceActionsCannotFallThroughToLegacy запрещает destructive legacy behavior до появления полных consumers.
func TestUnsupportedWorkspaceActionsCannotFallThroughToLegacy(t *testing.T) {
	for _, kind := range []string{"RESTORE", "DELETE_PVC", "DELETE_OBJECT"} {
		t.Run(kind, func(t *testing.T) {
			client := newArchiveClient()
			controller, _ := New(client, testConfig())
			task := workspaceTaskFixture(t)
			task.Kind = kind
			if _, _, err := controller.ExecuteRetained(t.Context(), task, func(context.Context) error { t.Fatal("unsupported action renewed authority"); return nil }); err == nil {
				t.Fatal("unsupported action accepted")
			}
			if len(client.Actions()) != 0 {
				t.Fatal("unsupported action reached Kubernetes")
			}
		})
	}
}
