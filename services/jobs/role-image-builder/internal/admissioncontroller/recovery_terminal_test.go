package admissioncontroller

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// terminalWorkSource provides a controlled proof/error and after-read mutation
// hook for fake Kubernetes races, counting reads without a real owner connection.
// terminalWorkSource задаёт proof/error и after-read mutation для гонок fake
// Kubernetes, считая чтения без настоящего соединения с владельцем.
type terminalWorkSource struct {
	proof     RecoveryTerminalProof
	err       error
	calls     int
	afterRead func()
}

// GetAvailability advertises only synthetic admission work, keeping independent
// promotion creation out of the terminal-cleanup scenario under test.
// GetAvailability объявляет только искусственную admission-работу, исключая
// независимое создание promotion из проверяемого terminal-cleanup сценария.
func (*terminalWorkSource) GetAvailability(context.Context) (WorkAvailability, error) {
	return WorkAvailability{AdmissionAvailable: true}, nil
}

// GetRecoveryTerminal counts the read, performs the configured race hook and
// returns the supplied proof unchanged, so a deliberately wrong run remains wrong
// rather than being silently replaced by the requested run argument.
// GetRecoveryTerminal считает чтение, выполняет заданный race hook и возвращает
// proof без исправлений: намеренно чужой run не заменяется запрошенным аргументом.
func (s *terminalWorkSource) GetRecoveryTerminal(_ context.Context, runID string) (RecoveryTerminalProof, error) {
	s.calls++
	if s.afterRead != nil {
		s.afterRead()
	}
	return s.proof, s.err
}

// TestControllerOriginalOwnerTerminalProofClosesStaleWorkspace creates a durable
// failed-predecessor cursor before reading a synthetic owner proof. Exact/TTL-cleaned
// terminal cases remove the old workspace/Jobs and permit a later fresh claim;
// denial, wrong proof, live Jobs and post-read UID/version/cursor races preserve
// the workspace. Fake reactors require delete UID/version wiring, not live API
// enforcement or a real owner-issued credential.
// TestControllerOriginalOwnerTerminalProofClosesStaleWorkspace создаёт устойчивый
// failed-predecessor cursor до чтения искусственного owner proof. Точный и TTL-
// очищенный terminal удаляют старые workspace/Jobs и допускают последующий claim;
// отказ, чужой proof, живые Jobs и post-read UID/version/cursor гонки сохраняют
// workspace. Fake reactors требуют mapping UID/version удаления, не настоящее
// применение API-проверки либо реквизиты, выданные владельцем.
func TestControllerOriginalOwnerTerminalProofClosesStaleWorkspace(t *testing.T) {
	for _, scenario := range []string{"valid", "valid-ttl", "denied", "unknown", "wrong-run", "bad-fence", "active-job", "workspace-changed", "workspace-version-changed", "job-changed", "job-uid-changed", "job-version-changed"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := testConfig()
			cfg.PauseNewRuns = true
			now := time.Date(2026, 8, 22, 12, 30, 0, 0, time.UTC)
			runID := makeRunID(now, testOrchestrationRevision)
			claim, _ := (testRenderer{}).Render(t.Context(), testPolicy(), cfg.Environment, runID, "claim")
			if prepareRendered(claim, cfg.Namespace, runID, "claim") != nil {
				t.Fatal("render fixture")
			}
			claim.PVC.CreationTimestamp = metav1.NewTime(now.Add(-time.Hour))
			claim.PVC.ResourceVersion = "11"
			objects := []runtime.Object{testPolicy(), claim.PVC}
			for _, phase := range phaseOrder {
				rendered, _ := (testRenderer{}).Render(t.Context(), testPolicy(), cfg.Environment, runID, phase)
				if prepareRendered(rendered, cfg.Namespace, runID, phase) != nil {
					t.Fatal("render phase")
				}
				rendered.Job.ResourceVersion = "12"
				kind := batchv1.JobComplete
				if phase == "admit" {
					kind = batchv1.JobFailed
				}
				rendered.Job.Status.Conditions = []batchv1.JobCondition{{Type: kind, Status: corev1.ConditionTrue}}
				objects = append(objects, rendered.Job)
			}
			client := fake.NewClientset(objects...)
			source := &terminalWorkSource{proof: RecoveryTerminalProof{RunID: runID, State: "CANCELLED", ArtifactRef: "imgart_original", BuildRef: "imgbld_original", AttemptRef: "imgadm_original", Attempt: 1, ClaimVersion: 2, ClaimFence: 1, ClaimGeneration: 7, TerminalVersion: 3, TerminalFence: 2, TerminalAttemptVersion: 3}}
			cfg.WorkSource = source
			controller, err := New(client, testRenderer{}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			controller.now = func() time.Time { return now }
			if err := controller.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if source.calls != 0 {
				t.Fatal("owner proof read before durable failed predecessor cursor")
			}
			before, _ := client.CoreV1().PersistentVolumeClaims(cfg.Namespace).Get(t.Context(), claim.PVC.Name, metav1.GetOptions{})
			if before.Annotations[recoveryUIDAnnotation] == "" {
				t.Fatal("durable recovery cursor absent")
			}
			switch scenario {
			case "valid-ttl":
				jobs, _ := client.BatchV1().Jobs(cfg.Namespace).List(t.Context(), metav1.ListOptions{})
				for _, job := range jobs.Items {
					if err := client.BatchV1().Jobs(cfg.Namespace).Delete(t.Context(), job.Name, metav1.DeleteOptions{}); err != nil {
						t.Fatal(err)
					}
				}
			case "denied":
				source.err = errors.New("owner permission denied")
			case "unknown":
				source.proof.State = "CLAIMED"
			case "wrong-run":
				source.proof.RunID = "foreign-run"
			case "bad-fence":
				source.proof.TerminalFence = 0
			case "active-job":
				job, _ := client.BatchV1().Jobs(cfg.Namespace).Get(t.Context(), claim.Job.Name, metav1.GetOptions{})
				job.Status.Conditions = nil
				job.Status.Active = 1
				client.BatchV1().Jobs(cfg.Namespace).Update(t.Context(), job, metav1.UpdateOptions{})
			case "workspace-changed":
				source.afterRead = func() {
					p := before.DeepCopy()
					p.Annotations[recoveryUIDAnnotation] = "dddddddd-dddd-dddd-dddd-dddddddddddd"
					client.CoreV1().PersistentVolumeClaims(cfg.Namespace).Update(t.Context(), p, metav1.UpdateOptions{})
				}
			case "job-changed":
				source.afterRead = func() {
					j, _ := client.BatchV1().Jobs(cfg.Namespace).Get(t.Context(), claim.Job.Name, metav1.GetOptions{})
					j.Annotations[runIDAnnotation] = "foreign-run"
					client.BatchV1().Jobs(cfg.Namespace).Update(t.Context(), j, metav1.UpdateOptions{})
				}
			case "workspace-version-changed":
				source.afterRead = func() {
					p := before.DeepCopy()
					p.ResourceVersion = "13"
					client.CoreV1().PersistentVolumeClaims(cfg.Namespace).Update(t.Context(), p, metav1.UpdateOptions{})
				}
			case "job-uid-changed", "job-version-changed":
				source.afterRead = func() {
					j, _ := client.BatchV1().Jobs(cfg.Namespace).Get(t.Context(), claim.Job.Name, metav1.GetOptions{})
					if scenario == "job-uid-changed" {
						j.UID = "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
					} else {
						j.ResourceVersion = "13"
					}
					client.BatchV1().Jobs(cfg.Namespace).Update(t.Context(), j, metav1.UpdateOptions{})
				}
			}
			// Delete должен иметь не только UID, но и свежий resourceVersion.
			client.PrependReactor("delete", "*", func(a k8stesting.Action) (bool, runtime.Object, error) {
				p := a.(k8stesting.DeleteAction).GetDeleteOptions().Preconditions
				if p == nil || p.UID == nil || *p.UID == "" || p.ResourceVersion == nil || *p.ResourceVersion == "" {
					t.Fatal("terminal recovery delete lost exact UID/version fence")
				}
				return false, nil, nil
			})
			controller.now = func() time.Time { return now.Add(cfg.RetryInterval) }
			err = controller.Reconcile(t.Context())
			_, readErr := client.CoreV1().PersistentVolumeClaims(cfg.Namespace).Get(t.Context(), claim.PVC.Name, metav1.GetOptions{})
			if scenario != "valid" && scenario != "valid-ttl" {
				if readErr != nil {
					t.Fatal("denial or incomplete proof deleted original workspace")
				}
				return
			}
			if err != nil || source.calls != 1 || !apierrors.IsNotFound(readErr) {
				t.Fatalf("exact terminal proof did not close stale workspace: %v", err)
			}
			jobs, _ := client.BatchV1().Jobs(cfg.Namespace).List(t.Context(), metav1.ListOptions{})
			if len(jobs.Items) != 0 {
				t.Fatal("old phase jobs remained")
			}
			controller.config.PauseNewRuns = false
			controller.now = func() time.Time { return now.Add(2 * cfg.RetryInterval) }
			if err := controller.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			jobs, _ = client.BatchV1().Jobs(cfg.Namespace).List(t.Context(), metav1.ListOptions{})
			if len(jobs.Items) != 1 || jobs.Items[0].Labels[phaseLabel] != "claim" || jobs.Items[0].Annotations[runIDAnnotation] == runID {
				t.Fatal("fresh B2 claim is unreachable")
			}
		})
	}
}
