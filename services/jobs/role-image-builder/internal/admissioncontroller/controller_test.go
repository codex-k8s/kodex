package admissioncontroller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

// testOrchestrationRevision is the fixed synthetic revision used to bind fixture
// run IDs and immutable policy data; it does not identify a deployed image build.
// testOrchestrationRevision — фиксированная искусственная ревизия для run ID
// и immutable policy fixtures, а не идентификатор развёрнутой сборки образа.
const testOrchestrationRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// TestControllerMaterializesSequentialAdmissionAndPromotion advances admission
// phases beside an independent promotion, checks workspace/job cleanup and requires
// explicit disabled token automount on retained fake Jobs, without starting Pods.
// TestControllerMaterializesSequentialAdmissionAndPromotion продвигает admission
// рядом с независимым promotion, проверяет удаление workspace/jobs и явный запрет
// automount токена у оставшихся fake Jobs, без запуска Pod.
func TestControllerMaterializesSequentialAdmissionAndPromotion(t *testing.T) {
	client := fake.NewClientset(testPolicy())
	controller := newTestController(t, client, testRenderer{}, testConfig())
	now := time.Date(2026, 8, 22, 12, 30, 0, 0, time.UTC)
	ctx := reconcileTestAt(t, controller, now)
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

// TestControllerHoldsExactProofJobsAcrossRestart retains suspended claim/promotion
// Jobs with exact attempt/reservation bindings across a new controller instance
// using the same fake client, without duplicating the two durable fixture Jobs.
// TestControllerHoldsExactProofJobsAcrossRestart сохраняет suspended claim/promotion
// с точными attempt/reservation при новом экземпляре контроллера и том же fake client,
// не дублируя два сохранённых в fixture Job.
func TestControllerHoldsExactProofJobsAcrossRestart(t *testing.T) {
	cfg := testConfig()
	cfg.HoldProofJobs = true
	client := fake.NewClientset(testPolicy())
	now := time.Date(2026, 9, 9, 12, 30, 0, 0, time.UTC)
	cfg.ProofHoldUntil = now.Add(20 * time.Minute)
	controller := newTestController(t, client, testRenderer{}, cfg)
	controller.now = func() time.Time { return now }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"claim", "promote"} {
		jobs, err := client.BatchV1().Jobs(cfg.Namespace).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		var found *batchv1.Job
		for index := range jobs.Items {
			if jobs.Items[index].Labels[phaseLabel] == phase {
				found = &jobs.Items[index]
			}
		}
		if found == nil || found.Spec.Suspend == nil || !*found.Spec.Suspend ||
			found.Labels[proofHoldLabel] != "true" || found.Annotations[proofAttempt] != "1" ||
			found.Annotations[proofReservation] != proofReservationDigest(found.Annotations[runIDAnnotation], found.Labels[idLabel], phase, "1") {
			t.Fatalf("%s proof reservation is incomplete: %#v", phase, found)
		}
	}
	restarted := newTestController(t, client, testRenderer{}, cfg)
	restarted.now = func() time.Time { return now.Add(time.Second) }
	if err := restarted.Reconcile(context.Background()); err != nil {
		t.Fatalf("restart rejected durable reservations: %v", err)
	}
	jobs, _ := client.BatchV1().Jobs(cfg.Namespace).List(context.Background(), metav1.ListOptions{})
	if len(jobs.Items) != 2 {
		t.Fatalf("restart duplicated held jobs: %d", len(jobs.Items))
	}
}

// TestControllerRejectsCorruptedProofReservation changes one fake Job reservation
// after materialization and requires closed reconciliation failure rather than
// trusting the existing managed label or replacing the corrupted reservation.
// TestControllerRejectsCorruptedProofReservation меняет reservation одного fake Job
// после создания и требует закрытого отказа reconcile, а не доверия managed label
// либо замены повреждённой reservation.
func TestControllerRejectsCorruptedProofReservation(t *testing.T) {
	cfg := testConfig()
	cfg.HoldProofJobs = true
	now := time.Date(2026, 9, 9, 12, 30, 0, 0, time.UTC)
	cfg.ProofHoldUntil = now.Add(20 * time.Minute)
	client := fake.NewClientset(testPolicy())
	controller := newTestController(t, client, testRenderer{}, cfg)
	controller.now = func() time.Time { return now }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	jobs, _ := client.BatchV1().Jobs(cfg.Namespace).List(context.Background(), metav1.ListOptions{})
	job := jobs.Items[0].DeepCopy()
	job.Annotations[proofReservation] = strings.Repeat("0", 64)
	if _, err := client.BatchV1().Jobs(cfg.Namespace).Update(context.Background(), job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(context.Background()); err == nil || !strings.Contains(err.Error(), "managed image admission job is invalid") {
		t.Fatalf("expected corrupted reservation rejection, got %v", err)
	}
}

// TestControllerFailsClosedAfterProofHoldDeadline advances the injected clock past
// the configured hold deadline and requires failure before any executable Job is
// materialized in the fake Kubernetes store.
// TestControllerFailsClosedAfterProofHoldDeadline переводит заданные часы за срок
// proof hold и требует отказа до создания исполняемого Job в fake-хранилище Kubernetes.
func TestControllerFailsClosedAfterProofHoldDeadline(t *testing.T) {
	cfg := testConfig()
	now := time.Date(2026, 9, 9, 12, 30, 0, 0, time.UTC)
	cfg.HoldProofJobs = true
	cfg.ProofHoldUntil = now.Add(time.Minute)
	client := fake.NewClientset(testPolicy())
	controller := newTestController(t, client, testRenderer{}, cfg)
	controller.now = func() time.Time { return now.Add(2 * time.Minute) }
	if err := controller.Reconcile(context.Background()); err == nil || !strings.Contains(err.Error(), "proof hold deadline") {
		t.Fatalf("expected expired hold rejection, got %v", err)
	}
	jobs, _ := client.BatchV1().Jobs(cfg.Namespace).List(context.Background(), metav1.ListOptions{})
	if len(jobs.Items) != 0 {
		t.Fatalf("expired hold created executable jobs: %d", len(jobs.Items))
	}
}

// TestControllerDropsFailedWorkspaceAndBacksOff marks the fake claim Job failed,
// verifies its admission workspace/non-promotion Jobs are removed and checks that
// immediate reconciliation at the same clock does not bypass the retry backoff.
// TestControllerDropsFailedWorkspaceAndBacksOff помечает fake claim Job ошибочным,
// проверяет удаление admission workspace/jobs кроме promotion и отсутствие обхода
// retry backoff при немедленном reconcile с теми же часами.
func TestControllerDropsFailedWorkspaceAndBacksOff(t *testing.T) {
	client := fake.NewClientset(testPolicy())
	controller := newTestController(t, client, testRenderer{}, testConfig())
	now := time.Date(2026, 8, 22, 12, 30, 0, 0, time.UTC)
	ctx := reconcileTestAt(t, controller, now)
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

// TestControllerRoutesTechnicalScanFailureThroughAdmissionRecord requires a failed
// synthetic scan to create exact UID-bound recovery admit instead of sign, then
// cleans the workspace after the test marks that recovery Job successful. This is
// controller routing proof, not a real scanner/admission receipt or image approval.
// TestControllerRoutesTechnicalScanFailureThroughAdmissionRecord требует после
// ошибки искусственного scan точный UID-bound recovery admit вместо sign и удаление
// workspace после заданного тестом успеха recovery Job. Это проверка маршрута
// контроллера, не настоящая квитанция сканера/допуска и не approval образа.
func TestControllerRoutesTechnicalScanFailureThroughAdmissionRecord(t *testing.T) {
	client := fake.NewClientset(testPolicy())
	controller := newTestController(t, client, testRenderer{}, testConfig())
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
	admit, _ := client.BatchV1().Jobs(testConfig().Namespace).Get(ctx, "mc-admit-"+id+"-admit", metav1.GetOptions{})
	scan, _ := client.BatchV1().Jobs(testConfig().Namespace).Get(ctx, "mc-admit-"+id+"-scan", metav1.GetOptions{})
	if len(admit.Spec.Template.Spec.Containers[0].Command) != 4 ||
		admit.Spec.Template.Spec.Containers[0].Command[3] != "SCAN_PREDECESSOR_FAILED" ||
		admit.Annotations[failedPredecessorUID] != string(scan.UID) {
		t.Fatal("failed scan did not receive exact bounded recovery")
	}
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

// TestControllerRejectsRendererPrivilegeExpansion supplies a fake renderer that
// chooses cluster-admin and requires local rendered-resource validation to reject
// it before the controller can accept the expanded service-account selection.
// TestControllerRejectsRendererPrivilegeExpansion передаёт fake renderer с выбором
// cluster-admin и требует отказа локальной проверки ресурса до принятия расширенного
// выбора service account контроллером.
func TestControllerRejectsRendererPrivilegeExpansion(t *testing.T) {
	client := fake.NewClientset(testPolicy())
	controller := newTestController(t, client, invalidRenderer{}, testConfig())
	controller.now = func() time.Time { return time.Date(2026, 8, 22, 12, 30, 0, 0, time.UTC) }
	if err := controller.Reconcile(context.Background()); err == nil || !strings.Contains(err.Error(), "rendered image admission job is invalid") {
		t.Fatalf("expected fail-closed renderer validation, got %v", err)
	}
}

// TestControllerRejectsConcurrentPromotionJobs inserts a second correctly prepared
// synthetic promotion for another run and requires rejection of concurrent active
// promotion Jobs instead of choosing one by list order.
// TestControllerRejectsConcurrentPromotionJobs добавляет второй подготовленный
// искусственный promotion другого запуска и требует отказа при двух активных
// promotion Jobs, а не выбора по порядку списка.
func TestControllerRejectsConcurrentPromotionJobs(t *testing.T) {
	client := fake.NewClientset(testPolicy())
	controller := newTestController(t, client, testRenderer{}, testConfig())
	now := time.Date(2026, 8, 22, 12, 30, 0, 0, time.UTC)
	ctx := reconcileTestAt(t, controller, now)
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

// TestControllerDoesNotMaterializePhaseForDeletingWorkspace retains a fake PVC with
// deletion timestamp/finalizer, removes its claim Job and asserts reconciliation
// neither recreates that phase nor records any other Job create action.
// TestControllerDoesNotMaterializePhaseForDeletingWorkspace сохраняет fake PVC
// с deletion timestamp/finalizer, удаляет claim Job и проверяет, что reconcile
// не воссоздаёт фазу и не записывает других действий создания Job.
func TestControllerDoesNotMaterializePhaseForDeletingWorkspace(t *testing.T) {
	client := fake.NewClientset(testPolicy())
	controller := newTestController(t, client, testRenderer{}, testConfig())
	now := time.Date(2026, 8, 22, 12, 30, 0, 0, time.UTC)
	ctx := reconcileTestAt(t, controller, now)
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

// TestControllerDeletesOnlyManagedOrphanAdmissionJobWithUIDPrecondition checks orphan
// cleanup preserves promotion/unmanaged Jobs, targets the managed admission UID and
// does not start a new workspace in the same cycle. Fake action inspection proves
// DeleteOptions wiring, not enforcement by a real Kubernetes API server.
// TestControllerDeletesOnlyManagedOrphanAdmissionJobWithUIDPrecondition проверяет
// сохранность promotion/unmanaged Jobs, удаление точного managed admission UID
// и отсутствие нового workspace в том же цикле. Fake actions доказывают mapping
// DeleteOptions, а не применение проверки настоящим Kubernetes API server.
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
	controller := newTestController(t, client, testRenderer{}, testConfig())
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

// testRenderer creates minimal synthetic phase Jobs and claim PVCs with deterministic
// IDs; it replaces the production shell renderer and never executes the workloads.
// testRenderer создаёт минимальные искусственные phase Jobs и claim PVC
// с детерминированными ID вместо shell renderer и не исполняет эти workloads.
type testRenderer struct{}

// Render derives fixture name/UID from environment and run, assigns the closed phase
// service account, disables token automount and adds a 2Gi RWO PVC only for claim.
// The policy argument is intentionally unused: policy parsing is tested elsewhere.
// Render получает имя/UID из environment/run, назначает закрытый phase account,
// запрещает automount токена и добавляет 2Gi RWO PVC только для claim. Аргумент policy
// намеренно не используется: разбор политики проверяется отдельно.
func (testRenderer) Render(_ context.Context, _ *corev1.ConfigMap, environment, runID, phase string) (Rendered, error) {
	id := testRenderID(environment, runID)
	automount := false
	job := &batchv1.Job{TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"}, ObjectMeta: metav1.ObjectMeta{
		Name: "mc-admit-" + id + "-" + phase, Namespace: testConfig().Namespace,
		Labels: map[string]string{idLabel: id, phaseLabel: phase}, UID: types.UID(id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]),
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

// invalidRenderer changes only the synthetic Job's service account to exercise
// rejection of renderer privilege expansion without deploying a privileged Pod.
// invalidRenderer меняет только service account искусственного Job для проверки
// отказа расширению прав renderer, без развёртывания привилегированного Pod.
type invalidRenderer struct{ testRenderer }

// Render delegates fixture materialization then substitutes cluster-admin; it
// preserves the underlying renderer's error and performs no cluster operation.
// Render создаёт fixture исходным renderer и подставляет cluster-admin, сохраняя
// его ошибку и не выполняя операций кластера.
func (invalidRenderer) Render(ctx context.Context, policy *corev1.ConfigMap, environment, runID, phase string) (Rendered, error) {
	result, err := (testRenderer{}).Render(ctx, policy, environment, runID, phase)
	result.Job.Spec.Template.Spec.ServiceAccountName = "cluster-admin"
	return result, err
}

// testPolicy returns a fresh immutable synthetic policy with owner-intent label
// and the test revision. That label is fixture data, not a server-owned approval.
// testPolicy возвращает новую immutable искусственную политику с owner-intent
// и тестовой ревизией. Label — данные fixture, не серверное подтверждение владельца.
func testPolicy() *corev1.ConfigMap {
	immutable := true
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: policyName, Namespace: testConfig().Namespace,
		Labels: map[string]string{"kodex.dev/owner-intent": "true"}}, Immutable: &immutable,
		Data: map[string]string{"orchestrationRevision": testOrchestrationRevision}}
}

// testConfig returns bounded fixture timing/identity and an available fake work
// source; its production environment string does not select a real live cluster.
// testConfig возвращает ограниченные интервалы/identity и доступный fake work source;
// строка production внутри fixture не выбирает настоящий живой кластер.
func testConfig() Config {
	return Config{WorkSource: testWorkSource{availability: WorkAvailability{AdmissionAvailable: true, PromotionAvailable: true}},
		Environment: "production", Namespace: "kodex-system", PolicyConfigMap: policyName,
		RendererPath: "/opt/kodex/render-image-admission-job.sh", TechnicalListen: ":9090",
		ReconcileInterval: 5 * time.Second, RetryInterval: 30 * time.Second,
		InfrastructureCheck: 10 * time.Second, RequestTimeout: 5 * time.Second}
}

// testWorkSource carries controlled availability or an error; pointer-backed use
// lets lifecycle tests change future reads without changing controller identity.
// testWorkSource хранит заданные availability либо ошибку; использование указателя
// позволяет менять будущие чтения без замены идентичности контроллера.
type testWorkSource struct {
	availability WorkAvailability
	err          error
}

// GetAvailability returns the supplied synthetic values without external I/O,
// filtering or inference from existing Kubernetes Jobs.
// GetAvailability возвращает заданные искусственные значения без внешнего I/O,
// фильтрации либо вывода доступности по существующим Kubernetes Jobs.
func (source testWorkSource) GetAvailability(context.Context) (WorkAvailability, error) {
	return source.availability, source.err
}

// TestControllerDoesNotCreateJobsWithoutAuthoritativeWork supplies no available
// work and requires successful idle reconciliation with no fake Jobs or PVCs.
// TestControllerDoesNotCreateJobsWithoutAuthoritativeWork задаёт отсутствие работы
// и требует успешного idle reconcile без fake Jobs и PVC.
func TestControllerDoesNotCreateJobsWithoutAuthoritativeWork(t *testing.T) {
	cfg := testConfig()
	cfg.WorkSource = testWorkSource{}
	client := fake.NewClientset(testPolicy())
	controller := newTestController(t, client, testRenderer{}, cfg)
	controller.now = func() time.Time { return time.Date(2026, 9, 17, 12, 30, 0, 0, time.UTC) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	jobs, err := client.BatchV1().Jobs(cfg.Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil || len(jobs.Items) != 0 {
		t.Fatalf("idle reconciliation created jobs: count=%d err=%v", len(jobs.Items), err)
	}
	workspaces, err := client.CoreV1().PersistentVolumeClaims(cfg.Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil || len(workspaces.Items) != 0 {
		t.Fatalf("idle reconciliation created workspaces: count=%d err=%v", len(workspaces.Items), err)
	}
}

// TestControllerFailsClosedWhenAuthoritativeWorkIsUnavailable returns an explicit
// work-source error and requires closed reconciliation failure without Job creation.
// TestControllerFailsClosedWhenAuthoritativeWorkIsUnavailable задаёт явную ошибку
// work source и требует закрытого отказа reconcile без создания Job.
func TestControllerFailsClosedWhenAuthoritativeWorkIsUnavailable(t *testing.T) {
	cfg := testConfig()
	cfg.WorkSource = testWorkSource{err: errors.New("control-plane unavailable")}
	client := fake.NewClientset(testPolicy())
	controller := newTestController(t, client, testRenderer{}, cfg)
	if err := controller.Reconcile(context.Background()); err == nil || !strings.Contains(err.Error(), "work availability") {
		t.Fatalf("expected closed availability failure, got %v", err)
	}
	jobs, _ := client.BatchV1().Jobs(cfg.Namespace).List(context.Background(), metav1.ListOptions{})
	if len(jobs.Items) != 0 {
		t.Fatalf("availability failure created jobs: %d", len(jobs.Items))
	}
}

// TestControllerCompletesActiveLifecycleWhenAvailabilityReadFails creates a claim,
// marks it successful and makes later availability reads fail. The active chain must
// still advance to scan while surfacing the independent idle-promotion preflight error.
// TestControllerCompletesActiveLifecycleWhenAvailabilityReadFails создаёт claim,
// помечает его успешным и ломает последующие availability reads. Активная цепочка
// должна перейти к scan, одновременно вернув ошибку независимого idle-promotion preflight.
func TestControllerCompletesActiveLifecycleWhenAvailabilityReadFails(t *testing.T) {
	source := &testWorkSource{availability: WorkAvailability{AdmissionAvailable: true, PromotionAvailable: false}}
	cfg := testConfig()
	cfg.WorkSource = source
	client := fake.NewClientset(testPolicy())
	controller := newTestController(t, client, testRenderer{}, cfg)
	controller.now = func() time.Time { return time.Date(2026, 9, 17, 12, 30, 0, 0, time.UTC) }
	ctx := context.Background()
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	workspaces, _ := client.CoreV1().PersistentVolumeClaims(cfg.Namespace).List(ctx, metav1.ListOptions{})
	id := workspaces.Items[0].Labels[idLabel]
	markJobSucceeded(t, client, id, "claim")
	source.err = errors.New("control-plane unavailable")
	if err := controller.Reconcile(ctx); err == nil || !strings.Contains(err.Error(), "work availability") {
		t.Fatalf("independent idle promotion preflight did not surface its failure: %v", err)
	}
	assertJob(t, client, id, "scan")
}

// testRenderID hex-encodes the first 16 SHA-256 bytes of the NUL-separated fixture
// environment/run pair, keeping repeated renders stable without random identities.
// testRenderID кодирует первые 16 SHA-256 байтов пары environment/run с NUL-разделителем,
// сохраняя стабильность повторного render без случайной идентичности.
func testRenderID(environment, runID string) string {
	digest := sha256.Sum256([]byte(environment + "\x00" + runID))
	return hex.EncodeToString(digest[:16])
}

// assertJob retrieves the exact fixture Job and applies validManagedJob to its
// namespace/phase contract; missing Jobs and malformed managed resources fail the test.
// assertJob читает точный fixture Job и применяет validManagedJob к namespace/phase;
// отсутствие Job и повреждённый managed resource завершают тест ошибкой.
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

// markJobSucceeded simulates success by setting only Succeeded=1 and a true Complete
// condition through the shared fake status updater, leaving other counters untouched.
// markJobSucceeded моделирует успех, задавая лишь Succeeded=1 и true Complete через
// общий fake status updater, не меняя остальные счётчики.
func markJobSucceeded(t *testing.T, client *fake.Clientset, id, phase string) {
	t.Helper()
	mutateJobStatus(t, client, id, phase, func(status *batchv1.JobStatus) {
		status.Succeeded = 1
		status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	})
}

// markJobFailed simulates failure by setting only Failed=1 and a true Failed
// condition through the shared fake status updater, leaving other counters untouched.
// markJobFailed моделирует ошибку, задавая лишь Failed=1 и true Failed через
// общий fake status updater, не меняя остальные счётчики.
func markJobFailed(t *testing.T, client *fake.Clientset, id, phase string) {
	t.Helper()
	mutateJobStatus(t, client, id, phase, func(status *batchv1.JobStatus) {
		status.Failed = 1
		status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
	})
}

// mutateJobStatus reads the exact fake Job, applies only the caller's status edits
// and updates its status subresource. Existing counters/metadata/spec are retained;
// the helper never reconciles a controller or contacts a Kubernetes cluster.
// mutateJobStatus читает точный искусственный Job, применяет только заданные правки
// статуса и обновляет status subresource. Остальные счётчики/metadata/spec сохраняются;
// helper не запускает reconcile и не обращается к кластеру Kubernetes.
func mutateJobStatus(t *testing.T, client *fake.Clientset, id, phase string, mutate func(*batchv1.JobStatus)) {
	t.Helper()
	job, err := client.BatchV1().Jobs(testConfig().Namespace).Get(context.Background(), "mc-admit-"+id+"-"+phase, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mutate(&job.Status)
	if _, err := client.BatchV1().Jobs(testConfig().Namespace).UpdateStatus(context.Background(), job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}

// newTestController centralizes only construction and its fatal error check using
// the same discard logger. The exact fake client, renderer and config remain caller
// inputs; clock overrides, work-source changes and reconcile order stay in each test.
// newTestController объединяет только конструктор и проверку его ошибки с прежним
// discard logger. Точные fake client, renderer и config задаёт вызывающий тест;
// часы, изменения work source и порядок reconcile остаются в самих сценариях.
func newTestController(t *testing.T, client *fake.Clientset, renderer Renderer, config Config) *Controller {
	t.Helper()
	controller, err := New(client, renderer, config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return controller
}

// reconcileTestAt fixes a caller-selected instant and performs exactly one explicit successful fake reconciliation.
// It returns the same background test context used by the original callers; tests that advance captured clocks must not use this helper.
// reconcileTestAt закрепляет выбранный момент и выполняет ровно одно явное успешное искусственное согласование.
// Возвращается прежний фоновый тестовый контекст; тесты с продвижением захваченных часов не должны использовать этот помощник.
func reconcileTestAt(t *testing.T, controller *Controller, now time.Time) context.Context {
	t.Helper()
	controller.now = func() time.Time { return now }
	ctx := context.Background()
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx
}

// TestReconcileTestAtPreservesExplicitClockAndSingleCycle checks clock/context identity and one availability read per idle work kind.
// Only the expected synthetic claim/promotion pair and one workspace may be created; it neither starts workers nor performs another cycle.
// TestReconcileTestAtPreservesExplicitClockAndSingleCycle проверяет часы, идентичность контекста и одно чтение доступности каждого вида новой работы.
// Допускаются только ожидаемые искусственные claim и promotion и один рабочий том; workers и дополнительный цикл не запускаются.
func TestReconcileTestAtPreservesExplicitClockAndSingleCycle(t *testing.T) {
	client := fake.NewClientset(testPolicy())
	config := testConfig()
	calls := 0
	config.WorkSource = workAvailabilityFunc(func(ctx context.Context) (WorkAvailability, error) {
		calls++
		if ctx != context.Background() {
			t.Fatal("reconcile context changed")
		}
		return WorkAvailability{AdmissionAvailable: true, PromotionAvailable: true}, nil
	})
	controller := newTestController(t, client, testRenderer{}, config)
	now := time.Date(2026, 8, 22, 12, 30, 0, 0, time.UTC)
	ctx := reconcileTestAt(t, controller, now)
	if ctx != context.Background() || !controller.now().Equal(now) || calls != 2 {
		t.Fatal("fixed clock helper changed context, time or reconcile count")
	}
	jobs, err := client.BatchV1().Jobs(config.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil || len(jobs.Items) != 2 {
		t.Fatal("fixed clock helper created unexpected jobs")
	}
	seen := map[string]bool{}
	for _, job := range jobs.Items {
		seen[job.Labels[phaseLabel]] = true
		if job.Annotations[runIDAnnotation] != makeRunID(now, testOrchestrationRevision) {
			t.Fatal("fixed clock helper changed run binding")
		}
	}
	workspaces, err := client.CoreV1().PersistentVolumeClaims(config.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil || len(workspaces.Items) != 1 || !seen["claim"] || !seen["promote"] {
		t.Fatal("fixed clock helper changed admission/promotion materialization")
	}
}

// TestNewTestControllerPreservesCallerInputs checks pointer/config identity and
// absence of fake Kubernetes actions, proving the constructor helper does not hide
// reconciliation or reset caller-owned work source, pause and timing choices.
// TestNewTestControllerPreservesCallerInputs проверяет identity указателей/config
// и отсутствие fake Kubernetes actions: helper не скрывает reconcile и не сбрасывает
// заданные work source, pause и интервалы.
func TestNewTestControllerPreservesCallerInputs(t *testing.T) {
	client := fake.NewClientset(testPolicy())
	renderer := &testRenderer{}
	source := &testWorkSource{availability: WorkAvailability{PromotionAvailable: true}}
	config := testConfig()
	config.WorkSource, config.PauseNewRuns, config.RetryInterval = source, true, time.Minute
	controller := newTestController(t, client, renderer, config)
	if controller.client != client || controller.renderer != renderer || controller.config != config ||
		controller.logger == nil || controller.now == nil || !controller.lastAdmissionAttempt.IsZero() || !controller.lastPromotionAttempt.IsZero() {
		t.Fatal("test constructor changed caller inputs or initial state")
	}
	if len(client.Actions()) != 0 {
		t.Fatal("test constructor performed hidden Kubernetes actions")
	}
}

// TestTerminalJobMarkersPreserveUnrelatedState checks success then failure on one
// fake Job with preexisting counters, metadata and spec. Each marker changes only
// its counter and exact terminal condition; the opposite counter remains intact.
// TestTerminalJobMarkersPreserveUnrelatedState проверяет success, затем failure
// одного fake Job с существующими counters/metadata/spec. Каждый marker меняет лишь
// свой счётчик и точное terminal condition, сохраняя противоположный счётчик.
func TestTerminalJobMarkersPreserveUnrelatedState(t *testing.T) {
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: "mc-admit-fixture-claim", Namespace: testConfig().Namespace,
		UID: "retained-uid", Annotations: map[string]string{"retained": "metadata"},
	}, Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{ServiceAccountName: "retained-account"}}},
		Status: batchv1.JobStatus{Active: 3, Failed: 7}}
	client := fake.NewClientset(job)
	markJobSucceeded(t, client, "fixture", "claim")
	succeeded, err := client.BatchV1().Jobs(testConfig().Namespace).Get(t.Context(), job.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if succeeded.Status.Active != 3 || succeeded.Status.Failed != 7 || succeeded.Status.Succeeded != 1 ||
		len(succeeded.Status.Conditions) != 1 || succeeded.Status.Conditions[0].Type != batchv1.JobComplete || succeeded.Status.Conditions[0].Status != corev1.ConditionTrue {
		t.Fatal("success marker replaced unrelated status")
	}
	markJobFailed(t, client, "fixture", "claim")
	failed, err := client.BatchV1().Jobs(testConfig().Namespace).Get(t.Context(), job.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status.Active != 3 || failed.Status.Succeeded != 1 || failed.Status.Failed != 1 ||
		len(failed.Status.Conditions) != 1 || failed.Status.Conditions[0].Type != batchv1.JobFailed || failed.Status.Conditions[0].Status != corev1.ConditionTrue {
		t.Fatal("failure marker replaced unrelated status")
	}
	if failed.UID != job.UID || failed.Annotations["retained"] != "metadata" || failed.Spec.Template.Spec.ServiceAccountName != "retained-account" {
		t.Fatal("status helper replaced Job metadata or spec")
	}
}

// assertJobDeleteUIDPrecondition finds the named Job delete action in the fake
// client's history and requires its exact nonnil UID precondition; it does not
// assert resource-version preconditions or real API-server enforcement.
// assertJobDeleteUIDPrecondition находит удаление именованного Job в fake history
// и требует точное ненулевое UID precondition; resource version и настоящее
// применение проверки API-сервером здесь не проверяются.
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

// indexOf returns the first exact string match or -1, letting the phase-order test
// select a successor without mutating the shared phase slice.
// indexOf возвращает первый точный индекс строки либо -1, позволяя тесту выбрать
// следующую фазу без изменения общего среза phaseOrder.
func indexOf(values []string, value string) int {
	for index, candidate := range values {
		if candidate == value {
			return index
		}
	}
	return -1
}
