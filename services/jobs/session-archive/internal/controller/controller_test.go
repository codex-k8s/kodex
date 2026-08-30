package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestExecuteMissingSnapshotPVCIsIdempotentAndCreatesNoWorkerResources(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset()
	controller, err := New(client, testConfig())
	if err != nil {
		t.Fatalf("create controller: %v", err)
	}
	task := testSnapshotTask()
	for attempt := 0; attempt < 2; attempt++ {
		result, err := controller.Execute(context.Background(), task, func(context.Context) error { return nil })
		if err != nil {
			t.Fatalf("execute missing PVC attempt %d: %v", attempt+1, err)
		}
		if result.Success || result.SafeErrorCode != model.SafeErrorPVCMissing {
			t.Fatalf("missing PVC result attempt %d = %#v", attempt+1, result)
		}
	}
	jobs, err := client.BatchV1().Jobs(testConfig().Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil || len(jobs.Items) != 0 {
		t.Fatalf("missing PVC created worker jobs: jobs=%d err=%v", len(jobs.Items), err)
	}
	inputs, err := client.CoreV1().ConfigMaps(testConfig().Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil || len(inputs.Items) != 0 {
		t.Fatalf("missing PVC created task inputs: configmaps=%d err=%v", len(inputs.Items), err)
	}
}

func TestExecuteStopsPendingWorkerWhenSnapshotPVCDisappears(t *testing.T) {
	t.Parallel()

	task := testSnapshotTask()
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: task.PVCName, Namespace: testConfig().Namespace,
		UID: types.UID("source-pvc-uid")}}
	client := fake.NewSimpleClientset(pvc)
	controller, err := New(client, testConfig())
	if err != nil {
		t.Fatalf("create controller: %v", err)
	}
	controller.poll = 5 * time.Millisecond
	result := executeAsync(controller, task)
	jobName := workloadName(task.TaskRef, task.ContentGeneration, task.Attempt)
	waitForJob(t, client, jobName)
	if err := client.CoreV1().PersistentVolumeClaims(testConfig().Namespace).Delete(context.Background(), task.PVCName, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete source PVC: %v", err)
	}
	completed := waitForResult(t, result)
	if completed.err != nil || completed.result.Success || completed.result.SafeErrorCode != model.SafeErrorPVCMissing {
		t.Fatalf("disappeared PVC result = %#v err=%v", completed.result, completed.err)
	}
	assertWorkerResourcesRemoved(t, client, jobName)
}

func TestExecuteRejectsSnapshotPVCReplacement(t *testing.T) {
	t.Parallel()

	task := testSnapshotTask()
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: task.PVCName, Namespace: testConfig().Namespace,
		UID: types.UID("source-pvc-uid")}}
	client := fake.NewSimpleClientset(pvc)
	controller, err := New(client, testConfig())
	if err != nil {
		t.Fatalf("create controller: %v", err)
	}
	controller.poll = 5 * time.Millisecond
	result := executeAsync(controller, task)
	jobName := workloadName(task.TaskRef, task.ContentGeneration, task.Attempt)
	job := waitForJob(t, client, jobName)
	if job.Annotations[sourcePVCUIDAnnotation] != "source-pvc-uid" {
		t.Fatalf("job source PVC binding = %#v", job.Annotations)
	}
	if err := client.CoreV1().PersistentVolumeClaims(testConfig().Namespace).Delete(context.Background(), task.PVCName, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete original PVC: %v", err)
	}
	pvc.UID = types.UID("replacement-pvc-uid")
	pvc.ResourceVersion = ""
	if _, err := client.CoreV1().PersistentVolumeClaims(testConfig().Namespace).Create(context.Background(), pvc, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create replacement PVC: %v", err)
	}
	completed := waitForResult(t, result)
	if completed.err != nil || completed.result.Success || completed.result.SafeErrorCode != model.SafeErrorPVCReplaced {
		t.Fatalf("replacement PVC result = %#v err=%v", completed.result, completed.err)
	}
	assertWorkerResourcesRemoved(t, client, jobName)
}

func TestEnsureRestorePVCCreatesBoundedCanonicalVolume(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset()
	controller, err := New(client, testConfig())
	if err != nil {
		t.Fatalf("create controller: %v", err)
	}
	task := model.Task{PVCName: "runtime-session-0123456789abcdef", InputDigest: strings.Repeat("a", 64)}
	if err := controller.ensureRestorePVC(context.Background(), task); err != nil {
		t.Fatalf("ensure restore PVC: %v", err)
	}
	pvc, err := client.CoreV1().PersistentVolumeClaims("kodex-system").Get(context.Background(), task.PVCName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read restore PVC: %v", err)
	}
	if pvc.Annotations[restoreInputAnnotation] != task.InputDigest || pvc.Spec.Resources.Requests.Storage().String() != "20Gi" {
		t.Fatalf("restore PVC is not bound to the immutable task: %#v", pvc)
	}

	conflict := task
	conflict.InputDigest = strings.Repeat("b", 64)
	if err := controller.ensureRestorePVC(context.Background(), conflict); err == nil {
		t.Fatal("controller accepted a PVC from another restore input")
	}
}

func TestWorkerJobUsesSessionVolumeGroupWithoutServiceAccountToken(t *testing.T) {
	t.Parallel()

	controller, err := New(fake.NewSimpleClientset(), testConfig())
	if err != nil {
		t.Fatalf("create controller: %v", err)
	}
	job := controller.job("session-archive-test", model.Task{Kind: "SNAPSHOT", PVCName: "runtime-session-0123456789abcdef"}, "pvc-uid")
	pod := job.Spec.Template.Spec
	if pod.SecurityContext == nil || pod.SecurityContext.FSGroup == nil || *pod.SecurityContext.FSGroup != 29000 {
		t.Fatalf("worker cannot read runner-owned session files: %#v", pod.SecurityContext)
	}
	if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
		t.Fatal("worker received a Kubernetes service account token")
	}
	assertEnvironmentValue(t, pod.Containers[0].Env, "SESSION_ARCHIVE_OBJECT_STORAGE_ALLOW_INSECURE_LOCAL", "false")
	if job.Annotations[sourcePVCUIDAnnotation] != "pvc-uid" {
		t.Fatalf("worker job lost the exact source PVC binding: %#v", job.Annotations)
	}
}

func assertEnvironmentValue(t *testing.T, values []corev1.EnvVar, name, expected string) {
	t.Helper()
	for _, value := range values {
		if value.Name == name {
			if value.Value != expected {
				t.Fatalf("environment %s = %q, expected %q", name, value.Value, expected)
			}
			return
		}
	}
	t.Fatalf("environment %s is absent", name)
}

type executionResult struct {
	result model.Result
	err    error
}

func executeAsync(controller *Controller, task model.Task) <-chan executionResult {
	result := make(chan executionResult, 1)
	go func() {
		value, err := controller.Execute(context.Background(), task, func(context.Context) error { return nil })
		result <- executionResult{result: value, err: err}
	}()
	return result
}

func waitForJob(t *testing.T, client *fake.Clientset, name string) *batchv1.Job {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		job, err := client.BatchV1().Jobs(testConfig().Namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err == nil {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("worker job %s was not created", name)
	return nil
}

func waitForResult(t *testing.T, result <-chan executionResult) executionResult {
	t.Helper()
	select {
	case completed := <-result:
		return completed
	case <-time.After(time.Second):
		t.Fatal("session archive execution did not stop within the test budget")
		return executionResult{}
	}
}

func assertWorkerResourcesRemoved(t *testing.T, client *fake.Clientset, name string) {
	t.Helper()
	if _, err := client.BatchV1().Jobs(testConfig().Namespace).Get(context.Background(), name, metav1.GetOptions{}); err == nil {
		t.Fatalf("worker job %s was not removed", name)
	}
	if _, err := client.CoreV1().ConfigMaps(testConfig().Namespace).Get(context.Background(), name, metav1.GetOptions{}); err == nil {
		t.Fatalf("task input %s was not removed", name)
	}
}

func testSnapshotTask() model.Task {
	return model.Task{TaskRef: "sat_00000000-0000-4000-8000-000000000001", Kind: "SNAPSHOT",
		ContentGeneration: 1, Attempt: 1, PVCName: "runtime-session-0123456789abcdef"}
}

func testConfig() Config {
	return Config{Namespace: "kodex-system", Environment: "staging", WorkerImage: "example.invalid/session-archive@sha256:" + strings.Repeat("0", 64),
		WorkerServiceAccount: "session-archive-worker", SessionPVCSize: "20Gi", ObjectStorageSecret: "object-storage",
		ObjectStorageEndpoint: "https://s3.example.invalid", ObjectStorageRegion: "us-east-1", ObjectStorageBucket: "archives",
		WorkerTimeout: 8 * time.Minute}
}
