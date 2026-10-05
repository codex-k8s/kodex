package admissioncontroller

import (
	"io"
	"log/slog"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestAdmissionFailedReceiptRecoverySurvivesRestart(t *testing.T) {
	cfg := testConfig()
	now := time.Date(2026, 8, 22, 12, 30, 0, 0, time.UTC)
	runID := makeRunID(now, testOrchestrationRevision)
	claim, err := (testRenderer{}).Render(t.Context(), testPolicy(), cfg.Environment, runID, "claim")
	if err != nil || prepareRendered(claim, cfg.Namespace, runID, "claim") != nil {
		t.Fatal("render claim")
	}
	claim.PVC.CreationTimestamp = metav1.NewTime(now.Add(-time.Hour))
	objects := []runtime.Object{testPolicy(), claim.PVC}
	for _, phase := range phaseOrder {
		rendered, err := (testRenderer{}).Render(t.Context(), testPolicy(), cfg.Environment, runID, phase)
		if err != nil || prepareRendered(rendered, cfg.Namespace, runID, phase) != nil {
			t.Fatal("render phase")
		}
		kind := batchv1.JobComplete
		if phase == "admit" {
			kind = batchv1.JobFailed
		}
		rendered.Job.Status.Conditions = []batchv1.JobCondition{{Type: kind, Status: corev1.ConditionTrue}}
		objects = append(objects, rendered.Job)
	}
	client := fake.NewClientset(objects...)
	controller, err := New(client, testRenderer{}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	controller.now = func() time.Time { return now }
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	workspace, err := client.CoreV1().PersistentVolumeClaims(cfg.Namespace).Get(t.Context(), claim.PVC.Name, metav1.GetOptions{})
	if err != nil || workspace.Annotations[recoveryUIDAnnotation] == "" {
		t.Fatal("failed callback lost durable workspace cursor")
	}
	restarted, err := New(client, testRenderer{}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = func() time.Time { return now.Add(cfg.RetryInterval) }
	if err := restarted.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	job, err := client.BatchV1().Jobs(cfg.Namespace).Get(t.Context(), claim.Job.Name[:len(claim.Job.Name)-len("claim")]+"admit", metav1.GetOptions{})
	if err != nil || job.Annotations[failedPredecessorUID] != workspace.Annotations[recoveryUIDAnnotation] || job.Spec.Template.Spec.Containers[0].Command[3] != "ADMIT_PREDECESSOR_FAILED" {
		t.Fatal("recovery did not pin exact predecessor")
	}
	job.UID = types.UID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
	if _, err := client.BatchV1().Jobs(cfg.Namespace).Update(t.Context(), job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	workspace, _ = client.CoreV1().PersistentVolumeClaims(cfg.Namespace).Get(t.Context(), claim.PVC.Name, metav1.GetOptions{})
	if workspace.Annotations[recoveryUIDAnnotation] != string(job.UID) {
		t.Fatal("callback outage cursor not advanced atomically")
	}
	restarted.now = func() time.Time { return now.Add(2 * cfg.RetryInterval) }
	if err := restarted.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	job, _ = client.BatchV1().Jobs(cfg.Namespace).Get(t.Context(), job.Name, metav1.GetOptions{})
	job.UID = types.UID("cccccccc-cccc-cccc-cccc-cccccccccccc")
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	if _, err := client.BatchV1().Jobs(cfg.Namespace).Update(t.Context(), job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	// Crash посреди cleanup уже мог удалить предыдущую фазу.
	if err := client.BatchV1().Jobs(cfg.Namespace).Delete(t.Context(), claim.Job.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	workspaces, _ := client.CoreV1().PersistentVolumeClaims(cfg.Namespace).List(t.Context(), metav1.ListOptions{})
	if len(workspaces.Items) != 0 {
		t.Fatal("durable owner receipt did not unblock cleanup")
	}
}

func TestRecoveryWorkspaceCELAllowsOnlyClosedCursor(t *testing.T) {
	policies, err := readAdmissionPolicies()
	if err != nil {
		t.Fatal(err)
	}
	var policy admissionPolicyDocument
	for _, p := range policies {
		if p.Metadata.Name == "kodex-image-admission-controller-workspaces" {
			policy = p
		}
	}
	renderer, err := NewScriptRenderer(repositoryRoot() + "/tools/render-image-admission-job.sh")
	if err != nil {
		t.Fatal(err)
	}
	runID := "v20260822120000-" + testOrchestrationRevision
	rendered, err := renderer.Render(t.Context(), completeTestPolicy(), "production", runID, "claim")
	if err != nil || prepareRendered(rendered, "kodex-system", runID, "claim") != nil {
		t.Fatal("fixture render")
	}
	rendered.PVC.CreationTimestamp = metav1.NewTime(time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC))
	rendered.PVC.Spec.VolumeName = "synthetic-bound-volume"
	old, err := runtime.DefaultUnstructuredConverter.ToUnstructured(rendered.PVC)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"valid", "resize", "identity", "other_annotation", "owner_refs", "finalizers", "invalid_uid", "outside_budget", "foreign_actor", "pod_actor", "nonmanaged"} {
		t.Run(scenario, func(t *testing.T) {
			updated := rendered.PVC.DeepCopy()
			updated.Annotations[recoveryUIDAnnotation] = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
			updated.Annotations[recoveryAfterAnnotation] = "2026-08-22T12:30:00Z"
			switch scenario {
			case "resize":
				updated.Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("4Gi")
			case "identity":
				updated.Labels[idLabel] = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			case "other_annotation":
				updated.Annotations[runIDAnnotation] = "v20260822120001-" + testOrchestrationRevision
			case "owner_refs":
				updated.OwnerReferences = []metav1.OwnerReference{{Name: "foreign"}}
			case "finalizers":
				updated.Finalizers = []string{"foreign"}
			case "invalid_uid":
				updated.Annotations[recoveryUIDAnnotation] = "not-a-uid"
			case "outside_budget":
				updated.Annotations[recoveryAfterAnnotation] = "2026-08-24T12:30:00Z"
			case "nonmanaged":
				updated.Labels[orchestratedLabel] = "false"
			}
			object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(updated)
			if err != nil {
				t.Fatal(err)
			}
			actor := "system:serviceaccount:kodex-system:image-admission-controller"
			if scenario == "foreign_actor" {
				actor = "system:serviceaccount:kodex-system:image-promotion"
			}
			if scenario == "pod_actor" {
				actor = "system:serviceaccount:kodex-system:image-admission"
			}
			accepted, err := evaluateAdmissionPolicyForActor(policy, object, old, completeTestPolicy(), actor)
			if err != nil {
				t.Fatal(err)
			}
			if accepted != (scenario == "valid") {
				t.Fatal("workspace recovery policy disagrees with closed cursor contract")
			}
		})
	}
}
