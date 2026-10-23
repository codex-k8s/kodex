package controller

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

// newArchiveClient assigns fixture identity, enforces UID/version delete preconditions and collects only exact Job-owned Pods.
// RU: newArchiveClient назначает идентичность оснастки, соблюдает UID/version предусловия удаления и очищает лишь Pods точного Job-владельца.
func newArchiveClient(objects ...runtime.Object) *fake.Clientset {
	client := fake.NewSimpleClientset(objects...)
	client.PrependReactor("create", "*", func(action clienttesting.Action) (bool, runtime.Object, error) {
		object := action.(clienttesting.CreateAction).GetObject()
		metadata, _ := meta.Accessor(object)
		metadata.SetUID(types.UID("created-" + metadata.GetName()))
		metadata.SetResourceVersion("1")
		return false, nil, nil
	})
	client.PrependReactor("delete", "*", func(action clienttesting.Action) (bool, runtime.Object, error) {
		deletion := action.(clienttesting.DeleteAction)
		object, err := client.Tracker().Get(action.GetResource(), action.GetNamespace(), deletion.GetName())
		if err != nil {
			return true, nil, err
		}
		metadata, _ := meta.Accessor(object)
		preconditions := deletion.GetDeleteOptions().Preconditions
		if preconditions != nil && ((preconditions.UID != nil && *preconditions.UID != metadata.GetUID()) ||
			(preconditions.ResourceVersion != nil && *preconditions.ResourceVersion != metadata.GetResourceVersion())) {
			return true, nil, apierrors.NewConflict(action.GetResource().GroupResource(), deletion.GetName(), errors.New("fixture identity changed"))
		}
		if action.GetResource().Resource == "jobs" {
			pods, _ := client.Tracker().List(corev1.SchemeGroupVersion.WithResource("pods"), corev1.SchemeGroupVersion.WithKind("Pod"), action.GetNamespace())
			for _, pod := range pods.(*corev1.PodList).Items {
				owner := metav1.GetControllerOf(&pod)
				if owner != nil && owner.Kind == "Job" && owner.UID == metadata.GetUID() {
					_ = client.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), action.GetNamespace(), pod.Name)
				}
			}
		}
		return false, nil, nil
	})
	return client
}

// fullArchiveTask expands the minimal task into consistent owner, runtime, session-path and PVC bindings.
// RU: fullArchiveTask дополняет минимальное задание согласованными привязками владельца, runtime, пути сессии и PVC.
func fullArchiveTask() model.Task {
	task := testSnapshotTask()
	task.OrganizationRef, task.SessionRef = "org_fixture01", "ses_fixture01"
	task.ProviderAccountRef, task.RuntimeRevisionRef, task.RuntimeRevisionVersion = "pva_fixture01", "rrev_fixture01", 1
	task.RuntimeRevisionDigest, task.InputDigest, task.SourceSHA256 = strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	task.CodexSessionID = "00000000-0000-4000-8000-000000000003"
	task.SourceRelativePath = ".kodex/state/codex-home/sessions/2026/08/28/rollout-2026-08-28T23-23-39-" + task.CodexSessionID + ".jsonl"
	task.SourceSizeBytes, task.TargetObjectKey = 1024, "fixture/object-a1.tar"
	task.PVCName, _ = runtimecontract.SessionPVCName(task.SessionRef)
	return task
}

// attemptResources creates validated immutable input and a matching worker Job with explicit fixture UID and version.
// RU: attemptResources создаёт проверенный неизменяемый вход и соответствующую Job worker с явными UID и версией оснастки.
func attemptResources(t *testing.T, controller *Controller, task model.Task) (*corev1.ConfigMap, *batchv1.Job) {
	t.Helper()
	raw, err := json.Marshal(task)
	if err != nil || task.Validate() != nil {
		t.Fatal("invalid fixture task")
	}
	name := workloadName(task.TaskRef, task.ContentGeneration, task.Attempt)
	input := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: exactWorkerNamespace, UID: types.UID("input-" + name), ResourceVersion: "1",
		Labels: map[string]string{managedLabel: "true"}}, Immutable: boolPtr(true), Data: map[string]string{"task.json": string(raw)}}
	job := controller.job(name, task, "source-pvc-uid")
	job.UID, job.ResourceVersion = types.UID("job-"+name), "1"
	return input, job
}

// TestSecondArchiveReplicaCheckNeverDeletesActiveWork proves a replica's readiness probe only lists resources.
// RU: TestSecondArchiveReplicaCheckNeverDeletesActiveWork доказывает, что проверка готовности реплики лишь читает список ресурсов.
func TestSecondArchiveReplicaCheckNeverDeletesActiveWork(t *testing.T) {
	t.Parallel()
	client := newArchiveClient()
	first, _ := New(client, testConfig())
	input, job := attemptResources(t, first, fullArchiveTask())
	_ = client.Tracker().Add(input)
	_ = client.Tracker().Add(job)
	second, _ := New(client, testConfig())
	if err := second.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "list" {
			t.Fatal("startup mutated active work")
		}
	}
}

// TestArchiveRetryCleansOnlyProvenPreviousAttempt requires fresh authority and exact deletion while preserving another task.
// RU: TestArchiveRetryCleansOnlyProvenPreviousAttempt требует свежих полномочий и точного удаления, сохраняя другое задание.
func TestArchiveRetryCleansOnlyProvenPreviousAttempt(t *testing.T) {
	t.Parallel()
	client := newArchiveClient()
	controller, _ := New(client, testConfig())
	previous := fullArchiveTask()
	input, job := attemptResources(t, controller, previous)
	_ = client.Tracker().Add(input)
	_ = client.Tracker().Add(job)
	other := previous
	other.TaskRef = "sat_00000000-0000-4000-8000-000000000002"
	otherInput, otherJob := attemptResources(t, controller, other)
	_ = client.Tracker().Add(otherInput)
	_ = client.Tracker().Add(otherJob)
	current := previous
	current.Attempt, current.TargetObjectKey, current.InputDigest = 2, "fixture/object-a2.tar", strings.Repeat("d", 64)
	var renewals int
	if err := controller.cleanupPredecessors(t.Context(), current, func(context.Context) error { renewals++; return nil }); err != nil {
		t.Fatal(err)
	}
	assertWorkerResourcesRemoved(t, client, job.Name)
	if _, err := client.BatchV1().Jobs(exactWorkerNamespace).Get(t.Context(), otherJob.Name, metav1.GetOptions{}); err != nil || renewals < 3 {
		t.Fatal("retry lost unrelated job or fresh authority checks")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "delete-collection" {
			t.Fatal("namespace-wide cleanup reached API")
		}
		if deletion, ok := action.(clienttesting.DeleteAction); ok {
			options := deletion.GetDeleteOptions()
			if options.Preconditions == nil || options.Preconditions.UID == nil || options.Preconditions.ResourceVersion == nil {
				t.Fatal("cleanup is not exact UID/RV fenced")
			}
		}
	}
}

// TestArchiveRetryRejectsExpiredAuthorityAndForeignInput forbids mutations for invalid leases, owner pins or predecessor resources.
// RU: TestArchiveRetryRejectsExpiredAuthorityAndForeignInput запрещает изменения при неверных lease, привязках владельца или ресурсах предшественника.
func TestArchiveRetryRejectsExpiredAuthorityAndForeignInput(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"expired", "renew lost", "foreign owner", "same attempt", "foreign job", "missing input"} {
		t.Run(scenario, func(t *testing.T) {
			client := newArchiveClient()
			controller, _ := New(client, testConfig())
			previous := fullArchiveTask()
			current := previous
			current.Attempt = 2
			if scenario == "foreign owner" {
				previous.OrganizationRef = "org_foreign01"
			}
			if scenario == "same attempt" {
				previous.Attempt = 2
			}
			input, job := attemptResources(t, controller, previous)
			if scenario == "same attempt" {
				input.Name = workloadName(current.TaskRef, current.ContentGeneration, 1)
				job.Name = input.Name
			}
			if scenario == "foreign job" {
				job.Annotations[taskRefAnnotation] = "sat_foreign01"
			}
			if scenario != "missing input" {
				_ = client.Tracker().Add(input)
			}
			_ = client.Tracker().Add(job)
			renewals := 0
			renew := func(context.Context) error {
				renewals++
				if scenario == "expired" || (scenario == "renew lost" && renewals > 1) {
					return errors.New("fixture lease expired")
				}
				return nil
			}
			if err := controller.cleanupPredecessors(t.Context(), current, renew); err == nil {
				t.Fatal("invalid predecessor or authority was accepted")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() == "delete" || action.GetVerb() == "create" || action.GetVerb() == "delete-collection" {
					t.Fatal("rejected predecessor mutated Kubernetes")
				}
			}
		})
	}
}

// TestArchiveCleanupWaitsForPodJoinAndPreservesReplacedUID keeps task input when Pods remain or resource identity changed.
// RU: TestArchiveCleanupWaitsForPodJoinAndPreservesReplacedUID сохраняет вход задания при оставшихся Pods либо смене идентичности ресурса.
func TestArchiveCleanupWaitsForPodJoinAndPreservesReplacedUID(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"pending pod", "replaced input", "replaced job"} {
		t.Run(scenario, func(t *testing.T) {
			client := newArchiveClient()
			controller, _ := New(client, testConfig())
			input, job := attemptResources(t, controller, fullArchiveTask())
			_ = client.Tracker().Add(input)
			_ = client.Tracker().Add(job)
			if scenario == "pending pod" {
				pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-pending", Namespace: exactWorkerNamespace,
					UID: "pending-pod", Labels: map[string]string{"job-name": job.Name},
					OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID, Controller: boolPtr(true)}}}}
				_ = client.Tracker().Add(pod)
				client.PrependReactor("delete", "jobs", func(action clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, client.Tracker().Delete(action.GetResource(), action.GetNamespace(), job.Name)
				})
			} else {
				resource, object := corev1.SchemeGroupVersion.WithResource("configmaps"), runtime.Object(input.DeepCopy())
				if scenario == "replaced job" {
					resource, object = batchv1.SchemeGroupVersion.WithResource("jobs"), job.DeepCopy()
				}
				metadata, _ := meta.Accessor(object)
				metadata.SetUID("replacement-uid")
				_ = client.Tracker().Update(resource, object, exactWorkerNamespace)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
			defer cancel()
			if err := controller.cleanupAttempt(ctx, input, job, nil); err == nil {
				t.Fatal("pending pod or replaced UID was accepted")
			}
			if _, err := client.CoreV1().ConfigMaps(exactWorkerNamespace).Get(t.Context(), input.Name, metav1.GetOptions{}); err != nil {
				t.Fatal("cleanup removed input before pod join or despite UID replacement")
			}
		})
	}
}

// TestArchiveExpiredClaimHasNoPVCOrWorkerEffects rejects every task kind before contacting Kubernetes after lease failure.
// RU: TestArchiveExpiredClaimHasNoPVCOrWorkerEffects отклоняет любой вид задания до обращения к Kubernetes при отказе lease.
func TestArchiveExpiredClaimHasNoPVCOrWorkerEffects(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"SNAPSHOT", "RESTORE", "DELETE_PVC", "DELETE_OBJECT"} {
		t.Run(kind, func(t *testing.T) {
			client := newArchiveClient()
			controller, _ := New(client, testConfig())
			task := fullArchiveTask()
			task.Kind = kind
			_, err := controller.Execute(t.Context(), task, func(context.Context) error { return errors.New("fixture expired") })
			if err == nil || len(client.Actions()) != 0 {
				t.Fatal("expired claim reached Kubernetes")
			}
		})
	}
}

// TestParallelArchiveReplicasKeepOtherSessionAlive isolates worker cleanup and completion across two concurrent sessions.
// RU: TestParallelArchiveReplicasKeepOtherSessionAlive изолирует очистку worker и завершение двух параллельных сессий.
func TestParallelArchiveReplicasKeepOtherSessionAlive(t *testing.T) {
	t.Parallel()
	client := newArchiveClient()
	first, _ := New(client, testConfig())
	second, _ := New(client, testConfig())
	first.poll, second.poll = time.Millisecond, time.Millisecond
	one := fullArchiveTask()
	two := one
	two.TaskRef, two.SessionRef = "sat_00000000-0000-4000-8000-000000000002", "ses_fixture02"
	two.PVCName, _ = runtimecontract.SessionPVCName(two.SessionRef)
	for _, task := range []model.Task{one, two} {
		_ = client.Tracker().Add(&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: task.PVCName, Namespace: exactWorkerNamespace, UID: types.UID("pvc-" + task.SessionRef)}})
	}
	firstResult, secondResult := executeAsync(first, one), executeAsync(second, two)
	firstName, secondName := workloadName(one.TaskRef, one.ContentGeneration, 1), workloadName(two.TaskRef, two.ContentGeneration, 1)
	waitForJob(t, client, firstName)
	waitForJob(t, client, secondName)
	if err := client.CoreV1().PersistentVolumeClaims(exactWorkerNamespace).Delete(t.Context(), one.PVCName, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	completed := waitForResult(t, firstResult)
	if completed.err != nil || completed.result.SafeErrorCode != model.SafeErrorPVCMissing {
		t.Fatal("first session did not stop independently")
	}
	assertWorkerResourcesRemoved(t, client, firstName)
	if _, err := client.BatchV1().Jobs(exactWorkerNamespace).Get(t.Context(), secondName, metav1.GetOptions{}); err != nil {
		t.Fatal("first replica deleted second session job")
	}
	if err := client.CoreV1().PersistentVolumeClaims(exactWorkerNamespace).Delete(t.Context(), two.PVCName, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if completed := waitForResult(t, secondResult); completed.err != nil || completed.result.SafeErrorCode != model.SafeErrorPVCMissing {
		t.Fatal("second session could not finish independently")
	}
}

// TestUnknownArchiveJobCreateIsPreservedUntilFencedRetry preserves an accepted Job after a lost response until a proven retry.
// RU: TestUnknownArchiveJobCreateIsPreservedUntilFencedRetry сохраняет принятую Job после потери ответа до доказанного повтора.
func TestUnknownArchiveJobCreateIsPreservedUntilFencedRetry(t *testing.T) {
	t.Parallel()
	task := fullArchiveTask()
	client := newArchiveClient(&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
		Name: task.PVCName, Namespace: exactWorkerNamespace, UID: "source-pvc-uid"}})
	controller, _ := New(client, testConfig())
	client.PrependReactor("create", "jobs", func(action clienttesting.Action) (bool, runtime.Object, error) {
		job := action.(clienttesting.CreateAction).GetObject().(*batchv1.Job).DeepCopy()
		job.UID, job.ResourceVersion = "accepted-job-with-lost-response", "1"
		if err := client.Tracker().Create(action.GetResource(), job, exactWorkerNamespace); err != nil {
			t.Fatal(err)
		}
		return true, nil, apierrors.NewTimeoutError("fixture lost create response", 1)
	})
	_, err := controller.Execute(t.Context(), task, func(context.Context) error { return nil })
	if err == nil {
		t.Fatal("unknown create became success")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "delete" {
			t.Fatal("unknown create was deleted without a proven identity")
		}
	}
	task.Attempt, task.InputDigest, task.TargetObjectKey = 2, strings.Repeat("d", 64), "fixture/object-a2.tar"
	if err := controller.cleanupPredecessors(t.Context(), task, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	assertWorkerResourcesRemoved(t, client, workloadName(task.TaskRef, task.ContentGeneration, 1))
}
