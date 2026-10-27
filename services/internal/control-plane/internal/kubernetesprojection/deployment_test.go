package kubernetesprojection

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestDeploymentReadyExactCounters rejects missing identity and every incomplete rollout counter.
// TestDeploymentReadyExactCounters отклоняет отсутствие identity и каждый незавершённый rollout counter.
func TestDeploymentReadyExactCounters(t *testing.T) {
	replicas := int32(2)
	valid := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{UID: "synthetic", ResourceVersion: "1", Generation: 2}, Spec: appsv1.DeploymentSpec{Replicas: &replicas}, Status: appsv1.DeploymentStatus{ObservedGeneration: 2, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 2}}
	if !DeploymentReady(&valid) {
		t.Fatal("complete rollout rejected")
	}
	for name, change := range map[string]func(*appsv1.Deployment){
		"uid": func(d *appsv1.Deployment) { d.UID = "" }, "version": func(d *appsv1.Deployment) { d.ResourceVersion = "" }, "generation": func(d *appsv1.Deployment) { d.Generation = 0 },
		"nil replicas": func(d *appsv1.Deployment) { d.Spec.Replicas = nil }, "zero replicas": func(d *appsv1.Deployment) { zero := int32(0); d.Spec.Replicas = &zero },
		"observed": func(d *appsv1.Deployment) { d.Status.ObservedGeneration = 1 }, "replicas": func(d *appsv1.Deployment) { d.Status.Replicas = 1 }, "updated": func(d *appsv1.Deployment) { d.Status.UpdatedReplicas = 1 },
		"ready": func(d *appsv1.Deployment) { d.Status.ReadyReplicas = 1 }, "available": func(d *appsv1.Deployment) { d.Status.AvailableReplicas = 1 }, "unavailable": func(d *appsv1.Deployment) { d.Status.UnavailableReplicas = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			d := valid.DeepCopy()
			change(d)
			if DeploymentReady(d) {
				t.Fatal("incomplete rollout accepted")
			}
		})
	}
}
