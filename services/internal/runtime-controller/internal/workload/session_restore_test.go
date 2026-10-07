package workload

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSessionPVCAcceptsCanonicalRestoreMetadataAndRejectsForeignOwner(t *testing.T) {
	for _, projectRef := range []string{"", "prj_abcdefgh"} {
		t.Run(projectRef, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			manager := newTestManager(t, client)
			input := runtimecontract.RunnerInput{OrganizationRef: "org_abcdefgh", ProjectRef: projectRef, SessionRef: "ses_abcdefgh"}
			name, _ := runtimecontract.SessionPVCName(input.SessionRef)
			labels, annotations, err := runtimecontract.SessionVolumeMetadata(input.OrganizationRef, input.ProjectRef, input.SessionRef)
			if err != nil {
				t.Fatal(err)
			}
			annotations["session-archive.kodex.dev/restore-input-digest"] = "synthetic-exact-task"
			pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: manager.config.RuntimeNamespace, Labels: labels, Annotations: annotations}, Spec: corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: manager.pvcRequest}}}}
			if manager.config.StorageClass != "" {
				pvc.Spec.StorageClassName = &manager.config.StorageClass
			}
			if _, err := client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Create(t.Context(), pvc, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := manager.ensureSessionPVC(t.Context(), input); err != nil {
				t.Fatalf("archive producer metadata rejected by runtime: %v", err)
			}
			input.OrganizationRef = "org_other000"
			if err := manager.ensureSessionPVC(t.Context(), input); err == nil {
				t.Fatal("foreign archive owner adopted")
			}
		})
	}
}
