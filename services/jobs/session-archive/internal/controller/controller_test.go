package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
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
	jobs, err := client.BatchV1().Jobs(testConfig().WorkerNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil || len(jobs.Items) != 0 {
		t.Fatalf("missing PVC created worker jobs: jobs=%d err=%v", len(jobs.Items), err)
	}
	inputs, err := client.CoreV1().ConfigMaps(testConfig().WorkerNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil || len(inputs.Items) != 0 {
		t.Fatalf("missing PVC created task inputs: configmaps=%d err=%v", len(inputs.Items), err)
	}
}

func TestExecuteStopsPendingWorkerWhenSnapshotPVCDisappears(t *testing.T) {
	t.Parallel()

	task := testSnapshotTask()
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: task.PVCName, Namespace: testConfig().WorkerNamespace,
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
	if job.Namespace != exactWorkerNamespace {
		t.Fatalf("created worker Job namespace = %q", job.Namespace)
	}
	input, err := client.CoreV1().ConfigMaps(exactWorkerNamespace).Get(context.Background(), jobName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read worker ConfigMap: %v", err)
	}
	if input.Namespace != exactWorkerNamespace {
		t.Fatalf("created worker ConfigMap namespace = %q", input.Namespace)
	}
	if _, err := client.CoreV1().ConfigMaps("kodex-system").Get(context.Background(), jobName, metav1.GetOptions{}); err == nil {
		t.Fatal("worker ConfigMap leaked into kodex-system")
	}
	if err := client.CoreV1().PersistentVolumeClaims(testConfig().WorkerNamespace).Delete(context.Background(), task.PVCName, metav1.DeleteOptions{}); err != nil {
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
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: task.PVCName, Namespace: testConfig().WorkerNamespace,
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
	if err := client.CoreV1().PersistentVolumeClaims(testConfig().WorkerNamespace).Delete(context.Background(), task.PVCName, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete original PVC: %v", err)
	}
	pvc.UID = types.UID("replacement-pvc-uid")
	pvc.ResourceVersion = ""
	if _, err := client.CoreV1().PersistentVolumeClaims(testConfig().WorkerNamespace).Create(context.Background(), pvc, metav1.CreateOptions{}); err != nil {
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
	task := model.Task{OrganizationRef: "org_fixture01", SessionRef: "ses_fixture01", InputDigest: strings.Repeat("a", 64)}
	task.PVCName, _ = runtimecontract.SessionPVCName(task.SessionRef)
	if err := controller.ensureRestorePVC(context.Background(), task); err != nil {
		t.Fatalf("ensure restore PVC: %v", err)
	}
	pvc, err := client.CoreV1().PersistentVolumeClaims("kodex-runtime").Get(context.Background(), task.PVCName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read restore PVC: %v", err)
	}
	if pvc.Annotations[restoreInputAnnotation] != task.InputDigest || pvc.Spec.Resources.Requests.Storage().String() != "20Gi" {
		t.Fatalf("restore PVC is not bound to the immutable task: %#v", pvc)
	}
	wantedLabels, wantedAnnotations, _ := runtimecontract.SessionVolumeMetadata(task.OrganizationRef, task.ProjectRef, task.SessionRef)
	for _, pair := range []struct{ actual, wanted map[string]string }{{pvc.Labels, wantedLabels}, {pvc.Annotations, wantedAnnotations}} {
		for key, value := range pair.wanted {
			if pair.actual[key] != value {
				t.Fatal("restore PVC cannot pass the runtime managed owner predicate")
			}
		}
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
	if job.Namespace != "kodex-runtime" {
		t.Fatalf("worker namespace = %q", job.Namespace)
	}
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
	sessionMount := pod.Containers[0].VolumeMounts[len(pod.Containers[0].VolumeMounts)-1]
	if sessionMount.Name != "session" || sessionMount.MountPath != "/workspace/.kodex/state" {
		t.Fatalf("worker session PVC mount не совпадает с runtime workspace: %#v", sessionMount)
	}
}

func TestWorkerIdentityKeepsRestoredSourceOwnedByNativeWriter(t *testing.T) {
	t.Parallel()
	controller, err := New(fake.NewSimpleClientset(), testConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		kind string
		uid  int64
	}{{"SNAPSHOT", 10002}, {"RESTORE", 10002}, {"DELETE_OBJECT", 10002}, {"DELETE_PVC", 10002}} {
		t.Run(scenario.kind, func(t *testing.T) {
			job := controller.job("session-archive-test", model.Task{Kind: scenario.kind, PVCName: "runtime-session-0123456789abcdef"}, "pvc-uid")
			pod := job.Spec.Template.Spec
			security := pod.Containers[0].SecurityContext
			if security == nil || security.RunAsUser == nil || *security.RunAsUser != scenario.uid ||
				security.RunAsGroup == nil || *security.RunAsGroup != scenario.uid {
				t.Fatalf("%s worker identity must be UID/GID %d", scenario.kind, scenario.uid)
			}
			if security.RunAsNonRoot == nil || !*security.RunAsNonRoot || security.AllowPrivilegeEscalation == nil || *security.AllowPrivilegeEscalation ||
				security.ReadOnlyRootFilesystem == nil || !*security.ReadOnlyRootFilesystem || security.Privileged != nil && *security.Privileged ||
				security.Capabilities == nil || len(security.Capabilities.Add) != 0 || len(security.Capabilities.Drop) != 1 || security.Capabilities.Drop[0] != "ALL" {
				t.Fatal("worker identity change weakened the restricted security context")
			}
			if pod.SecurityContext.FSGroup == nil || *pod.SecurityContext.FSGroup != 29000 ||
				pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken ||
				len(pod.Containers[0].Command) != 0 || len(pod.Containers[0].Args) != 1 || pod.Containers[0].Args[0] != "worker" ||
				job.Annotations[sourcePVCUIDAnnotation] != "pvc-uid" {
				t.Fatal("worker lost its canonical volume, task entrypoint or source PVC binding")
			}
			if scenario.kind != "RESTORE" {
				if len(pod.InitContainers) != 0 {
					t.Fatal("non-restore task received a restore preparer")
				}
				return
			}
			if len(pod.InitContainers) != 1 {
				t.Fatal("restore task lacks its exact directory preparer")
			}
			prepare := pod.InitContainers[0]
			if prepare.Name != "restore-prepare" || prepare.Image != pod.Containers[0].Image ||
				len(prepare.Args) != 1 || prepare.Args[0] != "prepare-restore" || len(prepare.Command) != 0 ||
				prepare.SecurityContext == nil || *prepare.SecurityContext.RunAsUser != 10001 ||
				*prepare.SecurityContext.RunAsGroup != 10001 || *prepare.SecurityContext.AllowPrivilegeEscalation ||
				!*prepare.SecurityContext.ReadOnlyRootFilesystem || len(prepare.SecurityContext.Capabilities.Add) != 0 ||
				len(prepare.SecurityContext.Capabilities.Drop) != 1 || prepare.SecurityContext.Capabilities.Drop[0] != "ALL" ||
				len(prepare.Env) != 0 || len(prepare.VolumeMounts) != 2 ||
				prepare.VolumeMounts[0].Name != "task" || !prepare.VolumeMounts[0].ReadOnly ||
				prepare.VolumeMounts[1].Name != "session" || prepare.VolumeMounts[1].MountPath != "/workspace/.kodex/state" ||
				len(prepare.Resources.Limits) != 2 {
				t.Fatal("restore preparer widened identity, mounts, credentials or execution bounds")
			}
		})
	}
}

func TestNewRejectsWorkerNamespaceOutsideRuntimeBoundary(t *testing.T) {
	t.Parallel()
	config := testConfig()
	config.WorkerNamespace = "kodex-system"
	if _, err := New(fake.NewSimpleClientset(), config); err == nil {
		t.Fatal("controller accepted a worker namespace outside kodex-runtime")
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
		job, err := client.BatchV1().Jobs(testConfig().WorkerNamespace).Get(context.Background(), name, metav1.GetOptions{})
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
	if _, err := client.BatchV1().Jobs(testConfig().WorkerNamespace).Get(context.Background(), name, metav1.GetOptions{}); err == nil {
		t.Fatalf("worker job %s was not removed", name)
	}
	if _, err := client.CoreV1().ConfigMaps(testConfig().WorkerNamespace).Get(context.Background(), name, metav1.GetOptions{}); err == nil {
		t.Fatalf("task input %s was not removed", name)
	}
}

func testSnapshotTask() model.Task {
	return model.Task{TaskRef: "sat_00000000-0000-4000-8000-000000000001", Kind: "SNAPSHOT",
		ContentGeneration: 1, Attempt: 1, PVCName: "runtime-session-0123456789abcdef"}
}

func testConfig() Config {
	return Config{WorkerNamespace: "kodex-runtime", Environment: "staging", WorkerImage: "example.invalid/session-archive@sha256:" + strings.Repeat("0", 64),
		WorkerServiceAccount: "session-archive-worker", SessionPVCSize: "20Gi", ObjectStorageSecret: "object-storage",
		ObjectStorageEndpoint: "https://s3.example.invalid", ObjectStorageRegion: "us-east-1", ObjectStorageBucket: "archives",
		WorkerTimeout: 8 * time.Minute}
}
