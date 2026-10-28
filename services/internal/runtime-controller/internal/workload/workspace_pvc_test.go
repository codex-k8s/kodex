package workload

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestPreparedWorkspacePVCNeverCreatesOrAdopts exercises the real materialization entrypoint:
// missing, recycled or altered custody fails with GET only, while legacy creation stays separate.
// TestPreparedWorkspacePVCNeverCreatesOrAdopts проверяет actual materialization entrypoint:
// missing, recycled либо altered custody отклоняется с GET only; legacy creation остаётся отдельно.
func TestPreparedWorkspacePVCNeverCreatesOrAdopts(t *testing.T) {
	manager, client, input, _ := workspaceStoragePodFixture(t)
	name, _ := runtimecontract.SessionPVCName(input.SessionRef)
	accepted, err := client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Get(t.Context(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*corev1.PersistentVolumeClaim, *runtimecontract.RunnerInput)
		absent bool
		valid  bool
	}{
		{name: "exact", valid: true},
		{name: "missing", absent: true},
		{name: "recreated_uid", mutate: func(pvc *corev1.PersistentVolumeClaim, _ *runtimecontract.RunnerInput) {
			pvc.UID = "40000000-0000-4000-8000-000000000001"
		}},
		{name: "unknown_resource_version", mutate: func(pvc *corev1.PersistentVolumeClaim, _ *runtimecontract.RunnerInput) { pvc.ResourceVersion = "" }},
		{name: "unbound", mutate: func(pvc *corev1.PersistentVolumeClaim, _ *runtimecontract.RunnerInput) {
			pvc.Status.Phase = corev1.ClaimPending
		}},
		{name: "foreign_workspace", mutate: func(pvc *corev1.PersistentVolumeClaim, _ *runtimecontract.RunnerInput) {
			pvc.Annotations["workspace.kodex.dev/workspace-ref"] = "wks_foreign000"
		}},
		{name: "foreign_incarnation", mutate: func(pvc *corev1.PersistentVolumeClaim, _ *runtimecontract.RunnerInput) {
			pvc.Annotations["workspace.kodex.dev/volume-generation"] = "2"
		}},
		{name: "unknown_ready", mutate: func(_ *corev1.PersistentVolumeClaim, value *runtimecontract.RunnerInput) {
			value.WorkspaceStorage.VolumeReady = nil
		}},
		{name: "missing_pv", mutate: func(pvc *corev1.PersistentVolumeClaim, _ *runtimecontract.RunnerInput) { pvc.Spec.VolumeName = "" }},
		{name: "wrong_geometry", mutate: func(pvc *corev1.PersistentVolumeClaim, _ *runtimecontract.RunnerInput) {
			mode := corev1.PersistentVolumeBlock
			pvc.Spec.VolumeMode = &mode
		}},
		{name: "terminating", mutate: func(pvc *corev1.PersistentVolumeClaim, _ *runtimecontract.RunnerInput) {
			stamp := metav1.Now()
			pvc.DeletionTimestamp = &stamp
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			pvc := accepted.DeepCopy()
			candidate := input
			binding := *input.WorkspaceStorage
			candidate.WorkspaceStorage = &binding
			if test.mutate != nil {
				test.mutate(pvc, &candidate)
			}
			isolated := fake.NewSimpleClientset()
			if !test.absent {
				if err := isolated.Tracker().Add(pvc); err != nil {
					t.Fatal(err)
				}
			}
			worker := newTestManager(t, isolated)
			worker.config.StorageClass = "local-path"
			err := worker.ensureSessionPVC(t.Context(), candidate)
			if (err == nil) != test.valid {
				t.Fatalf("unexpected prepared volume admission: valid=%v err=%v", test.valid, err)
			}
			for _, action := range isolated.Actions() {
				if action.GetVerb() != "get" || action.GetResource().Resource != "persistentvolumeclaims" {
					t.Fatal("prepared volume admission performed a write or unrelated action")
				}
			}
		})
	}
}
