package admissioncontroller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const testOrchestrationRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestControllerMaterializesSequentialAdmissionAndPromotion(t *testing.T) {
	client := fake.NewClientset(testPolicy())
	controller, err := New(client, testRenderer{}, testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 22, 12, 30, 0, 0, time.UTC)
	controller.now = func() time.Time { return now }
	ctx := context.Background()
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	workspaces, _ := client.CoreV1().PersistentVolumeClaims(testConfig().Namespace).List(ctx, metav1.ListOptions{})
	if len(workspaces.Items) != 1 {
		t.Fatalf("expected one admission workspace, got %d", len(workspaces.Items))
	}
	id := workspaces.Items[0].Labels[idLabel]
	assertJob(t, client, id, "claim")
	assertJob(t, client, id, "promote")

	for _, phase := range phaseOrder {
		markJobSucceeded(t, client, id, phase)
		if err := controller.Reconcile(ctx); err != nil {
			t.Fatalf("reconcile after %s: %v", phase, err)
		}
		if phase != "admit" {
			next := phaseOrder[indexOf(phaseOrder, phase)+1]
			assertJob(t, client, id, next)
		}
	}
	if _, err := client.CoreV1().PersistentVolumeClaims(testConfig().Namespace).Get(ctx, "mc-admit-"+id, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("terminal admission workspace remains: %v", err)
	}
	jobs, _ := client.BatchV1().Jobs(testConfig().Namespace).List(ctx, metav1.ListOptions{})
	for index := range jobs.Items {
		job := &jobs.Items[index]
		if job.Labels[idLabel] == id && job.Labels[phaseLabel] != "promote" {
			t.Fatalf("terminal admission job remains: %s", job.Name)
		}
		if job.Spec.Template.Spec.AutomountServiceAccountToken == nil || *job.Spec.Template.Spec.AutomountServiceAccountToken {
			t.Fatalf("job %s received an implicit Kubernetes token", job.Name)
		}
	}
}

func TestControllerDropsFailedWorkspaceAndBacksOff(t *testing.T) {
	client := fake.NewClientset(testPolicy())
	controller, err := New(client, testRenderer{}, testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 22, 12, 30, 0, 0, time.UTC)
	controller.now = func() time.Time { return now }
	ctx := context.Background()
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	workspaces, _ := client.CoreV1().PersistentVolumeClaims(testConfig().Namespace).List(ctx, metav1.ListOptions{})
	id := workspaces.Items[0].Labels[idLabel]
	job, _ := client.BatchV1().Jobs(testConfig().Namespace).Get(ctx, "mc-admit-"+id+"-claim", metav1.GetOptions{})
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
	if _, err := client.BatchV1().Jobs(testConfig().Namespace).UpdateStatus(ctx, job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	remaining, _ := client.CoreV1().PersistentVolumeClaims(testConfig().Namespace).List(ctx, metav1.ListOptions{})
	if len(remaining.Items) != 0 {
		t.Fatal("failed admission workspace was not removed")
	}
	jobs, _ := client.BatchV1().Jobs(testConfig().Namespace).List(ctx, metav1.ListOptions{})
	for index := range jobs.Items {
		if jobs.Items[index].Labels[idLabel] == id && jobs.Items[index].Labels[phaseLabel] != "promote" {
			t.Fatalf("failed admission job remains: %s", jobs.Items[index].Name)
		}
	}
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	remaining, _ = client.CoreV1().PersistentVolumeClaims(testConfig().Namespace).List(ctx, metav1.ListOptions{})
	if len(remaining.Items) != 0 {
		t.Fatal("controller ignored retry backoff")
	}
}

func TestControllerRoutesTechnicalScanFailureThroughAdmissionRecord(t *testing.T) {
	client := fake.NewClientset(testPolicy())
	controller, err := New(client, testRenderer{}, testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	controller.now = func() time.Time { return time.Date(2026, 8, 22, 12, 30, 0, 0, time.UTC) }
	ctx := context.Background()
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	workspaces, _ := client.CoreV1().PersistentVolumeClaims(testConfig().Namespace).List(ctx, metav1.ListOptions{})
	id := workspaces.Items[0].Labels[idLabel]
	markJobSucceeded(t, client, id, "claim")
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	markJobFailed(t, client, id, "scan")
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	assertJob(t, client, id, "admit")
	if _, err := client.BatchV1().Jobs(testConfig().Namespace).Get(ctx, "mc-admit-"+id+"-sign", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("sign job was created after a technical scan rejection: %v", err)
	}
	markJobSucceeded(t, client, id, "admit")
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().PersistentVolumeClaims(testConfig().Namespace).Get(ctx, "mc-admit-"+id, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("recorded technical rejection workspace remains: %v", err)
	}
}

func TestControllerRejectsRendererPrivilegeExpansion(t *testing.T) {
	client := fake.NewClientset(testPolicy())
	controller, err := New(client, invalidRenderer{}, testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	controller.now = func() time.Time { return time.Date(2026, 8, 22, 12, 30, 0, 0, time.UTC) }
	if err := controller.Reconcile(context.Background()); err == nil || !strings.Contains(err.Error(), "rendered image admission job is invalid") {
		t.Fatalf("expected fail-closed renderer validation, got %v", err)
	}
}

func TestControllerRejectsConcurrentPromotionJobs(t *testing.T) {
	client := fake.NewClientset(testPolicy())
	controller, err := New(client, testRenderer{}, testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 22, 12, 30, 0, 0, time.UTC)
	controller.now = func() time.Time { return now }
	ctx := context.Background()
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	secondRunID := makeRunID(now.Add(time.Second), testOrchestrationRevision)
	second, err := (testRenderer{}).Render(ctx, testPolicy(), "production", secondRunID, "promote")
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareRendered(second, testConfig().Namespace, secondRunID, "promote"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.BatchV1().Jobs(testConfig().Namespace).Create(ctx, second.Job, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(ctx); err == nil || !strings.Contains(err.Error(), "multiple active image promotion jobs") {
		t.Fatalf("expected concurrent promotion rejection, got %v", err)
	}
}

func TestControllerDoesNotMaterializePhaseForDeletingWorkspace(t *testing.T) {
	client := fake.NewClientset(testPolicy())
	controller, err := New(client, testRenderer{}, testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 22, 12, 30, 0, 0, time.UTC)
	controller.now = func() time.Time { return now }
	ctx := context.Background()
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	workspaces, err := client.CoreV1().PersistentVolumeClaims(testConfig().Namespace).List(ctx, metav1.ListOptions{})
	if err != nil || len(workspaces.Items) != 1 {
		t.Fatalf("read admission workspace: count=%d err=%v", len(workspaces.Items), err)
	}
	workspace := workspaces.Items[0].DeepCopy()
	id := workspace.Labels[idLabel]
	deletionTimestamp := metav1.NewTime(now)
	workspace.DeletionTimestamp = &deletionTimestamp
	workspace.Finalizers = []string{"test.kodex.dev/retain-deleting-workspace"}
	if _, err := client.CoreV1().PersistentVolumeClaims(testConfig().Namespace).Update(ctx, workspace, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := client.BatchV1().Jobs(testConfig().Namespace).Delete(ctx, "mc-admit-"+id+"-claim", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	client.ClearActions()

	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.BatchV1().Jobs(testConfig().Namespace).Get(ctx, "mc-admit-"+id+"-claim", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("claim job was recreated for deleting workspace: %v", err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "create" && action.GetResource().Resource == "jobs" {
			t.Fatalf("job was materialized for deleting workspace: %#v", action)
		}
	}
}

func TestControllerDeletesOnlyManagedOrphanAdmissionJobWithUIDPrecondition(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 22, 12, 30, 0, 0, time.UTC)
	runID := makeRunID(now, testOrchestrationRevision)
	claim, err := (testRenderer{}).Render(ctx, testPolicy(), testConfig().Environment, runID, "claim")
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareRendered(claim, testConfig().Namespace, runID, "claim"); err != nil {
		t.Fatal(err)
	}
	promote, err := (testRenderer{}).Render(ctx, testPolicy(), testConfig().Environment, runID, "promote")
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareRendered(promote, testConfig().Namespace, runID, "promote"); err != nil {
		t.Fatal(err)
	}
	unmanaged := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "unmanaged-image-job", Namespace: testConfig().Namespace}}
	client := fake.NewClientset(testPolicy(), claim.Job, promote.Job, unmanaged)
	controller, err := New(client, testRenderer{}, testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	controller.now = func() time.Time { return now }

	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.BatchV1().Jobs(testConfig().Namespace).Get(ctx, claim.Job.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("managed orphan admission job remains: %v", err)
	}
	if _, err := client.BatchV1().Jobs(testConfig().Namespace).Get(ctx, promote.Job.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("promotion job was deleted: %v", err)
	}
	if _, err := client.BatchV1().Jobs(testConfig().Namespace).Get(ctx, unmanaged.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("unmanaged job was deleted: %v", err)
	}
	assertJobDeleteUIDPrecondition(t, client, claim.Job.Name, claim.Job.UID)
	workspaces, err := client.CoreV1().PersistentVolumeClaims(testConfig().Namespace).List(ctx, metav1.ListOptions{})
	if err != nil || len(workspaces.Items) != 0 {
		t.Fatalf("new workspace was materialized during orphan cleanup: count=%d err=%v", len(workspaces.Items), err)
	}
}

type testRenderer struct{}

func (testRenderer) Render(_ context.Context, _ *corev1.ConfigMap, environment, runID, phase string) (Rendered, error) {
	id := testRenderID(environment, runID)
	automount := false
	job := &batchv1.Job{TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"}, ObjectMeta: metav1.ObjectMeta{
		Name: "mc-admit-" + id + "-" + phase, Namespace: testConfig().Namespace,
		Labels: map[string]string{idLabel: id, phaseLabel: phase}, UID: types.UID("job-" + id + "-" + phase),
	}, Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
		ServiceAccountName: phaseAccounts[phase], AutomountServiceAccountToken: &automount,
		RestartPolicy: corev1.RestartPolicyNever,
		Containers:    []corev1.Container{{Name: phase, Command: []string{"/bin/sh", "/opt/kodex/image-admission.sh", phase}}},
	}}}}
	result := Rendered{Job: job}
	if phase == "claim" {
		result.PVC = &corev1.PersistentVolumeClaim{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolumeClaim"},
			ObjectMeta: metav1.ObjectMeta{Name: "mc-admit-" + id, Namespace: testConfig().Namespace, Labels: map[string]string{idLabel: id}, UID: types.UID("workspace-" + id)},
			Spec: corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("2Gi")}}}}
	}
	return result, nil
}

type invalidRenderer struct{ testRenderer }

func (invalidRenderer) Render(ctx context.Context, policy *corev1.ConfigMap, environment, runID, phase string) (Rendered, error) {
	result, err := (testRenderer{}).Render(ctx, policy, environment, runID, phase)
	result.Job.Spec.Template.Spec.ServiceAccountName = "cluster-admin"
	return result, err
}

func testPolicy() *corev1.ConfigMap {
	immutable := true
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: policyName, Namespace: testConfig().Namespace,
		Labels: map[string]string{"kodex.dev/owner-intent": "true"}}, Immutable: &immutable,
		Data: map[string]string{"orchestrationRevision": testOrchestrationRevision}}
}

func testConfig() Config {
	return Config{Environment: "production", Namespace: "kodex-system", PolicyConfigMap: policyName,
		RendererPath: "/opt/kodex/render-image-admission-job.sh", TechnicalListen: ":9090",
		ReconcileInterval: 5 * time.Second, RetryInterval: 30 * time.Second,
		InfrastructureCheck: 10 * time.Second, RequestTimeout: 5 * time.Second}
}

func testRenderID(environment, runID string) string {
	digest := sha256.Sum256([]byte(environment + "\x00" + runID))
	return hex.EncodeToString(digest[:16])
}

func assertJob(t *testing.T, client *fake.Clientset, id, phase string) {
	t.Helper()
	job, err := client.BatchV1().Jobs(testConfig().Namespace).Get(context.Background(), "mc-admit-"+id+"-"+phase, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read %s job: %v", phase, err)
	}
	if !validManagedJob(job, testConfig().Namespace, phase) {
		t.Fatalf("%s job does not retain bounded contract", phase)
	}
}

func markJobSucceeded(t *testing.T, client *fake.Clientset, id, phase string) {
	t.Helper()
	job, err := client.BatchV1().Jobs(testConfig().Namespace).Get(context.Background(), "mc-admit-"+id+"-"+phase, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	job.Status.Succeeded = 1
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	if _, err := client.BatchV1().Jobs(testConfig().Namespace).UpdateStatus(context.Background(), job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func markJobFailed(t *testing.T, client *fake.Clientset, id, phase string) {
	t.Helper()
	job, err := client.BatchV1().Jobs(testConfig().Namespace).Get(context.Background(), "mc-admit-"+id+"-"+phase, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	job.Status.Failed = 1
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
	if _, err := client.BatchV1().Jobs(testConfig().Namespace).UpdateStatus(context.Background(), job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func assertJobDeleteUIDPrecondition(t *testing.T, client *fake.Clientset, name string, uid types.UID) {
	t.Helper()
	for _, action := range client.Actions() {
		deleteAction, ok := action.(k8stesting.DeleteAction)
		if !ok || action.GetResource().Resource != "jobs" || deleteAction.GetName() != name {
			continue
		}
		preconditions := deleteAction.GetDeleteOptions().Preconditions
		if preconditions == nil || preconditions.UID == nil || *preconditions.UID != uid {
			t.Fatalf("unexpected admission Job delete preconditions: %#v", preconditions)
		}
		return
	}
	t.Fatal("admission Job delete action was not recorded")
}

func indexOf(values []string, value string) int {
	for index, candidate := range values {
		if candidate == value {
			return index
		}
	}
	return -1
}
