package controller

import (
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
)

// TestWorkspaceVolumeReadinessRequiresExactOwnPermissions rejects denied, ambiguous and borrowed
// authorization reviews while checking only the controller's fixed namespace/resource/verb set.
// TestWorkspaceVolumeReadinessRequiresExactOwnPermissions отклоняет denied, ambiguous и borrowed
// authorization reviews, проверяя только fixed namespace/resource/verb set самого controller.
func TestWorkspaceVolumeReadinessRequiresExactOwnPermissions(t *testing.T) {
	for name, mutate := range map[string]func(*authorizationv1.SelfSubjectAccessReview){
		"allowed": func(*authorizationv1.SelfSubjectAccessReview) {},
		"denied":  func(review *authorizationv1.SelfSubjectAccessReview) { review.Status.Denied = true },
		"unknown": func(review *authorizationv1.SelfSubjectAccessReview) { review.Status.Allowed = false },
		"evaluation-error": func(review *authorizationv1.SelfSubjectAccessReview) {
			review.Status.EvaluationError = "fixture unresolved authorization"
		},
		"foreign-namespace": func(review *authorizationv1.SelfSubjectAccessReview) {
			review.Spec.ResourceAttributes.Namespace = "foreign"
		},
	} {
		t.Run(name, func(t *testing.T) {
			client, controller := workspaceVolumeCreateControllerFixture(t)
			reviews := 0
			client.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
				review := action.(ktesting.CreateAction).GetObject().(*authorizationv1.SelfSubjectAccessReview).DeepCopy()
				if review.Spec.ResourceAttributes == nil || review.Spec.ResourceAttributes.Namespace != exactWorkerNamespace ||
					review.Spec.NonResourceAttributes != nil || review.Spec.ResourceAttributes.Resource == "secrets" ||
					review.Spec.ResourceAttributes.Verb == "*" {
					t.Fatal("readiness expanded its own authorization scope")
				}
				reviews++
				review.Status.Allowed = true
				mutate(review)
				return true, review, nil
			})
			err := controller.CheckWorkspaceVolume(t.Context())
			if (err == nil) != (name == "allowed") || name == "allowed" && reviews != 11 {
				t.Fatal("physical readiness accepted incomplete or borrowed authority")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() == "create" && action.GetResource().Resource != "selfsubjectaccessreviews" {
					t.Fatal("readiness materialized a physical workload")
				}
			}
		})
	}
}
