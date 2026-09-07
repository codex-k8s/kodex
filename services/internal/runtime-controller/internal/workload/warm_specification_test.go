package workload

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestWarmSpecificationPreservesProvisioningAndReadyPod(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	revision := testWarmRevision()
	input, binding, err := manager.BuildWarmInput(revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnsureWarm(ctx, input, binding); err != nil {
		t.Fatal(err)
	}
	for _, ready := range []bool{false, false, true, true} {
		pod, err := client.CoreV1().Pods("kodex-runtime").Get(ctx, "system-assistant-warm", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		pod.UID = "stable-pod-identity"
		if ready {
			pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		}
		if _, err := client.CoreV1().Pods("kodex-runtime").Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := manager.EnsureWarm(ctx, input, binding); err != nil {
			t.Fatal(err)
		}
		after, err := client.CoreV1().Pods("kodex-runtime").Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil || after.UID != pod.UID {
			t.Fatal("unchanged specification replaced warm Pod")
		}
	}
	revision.Version++
	revision.Instructions += "\nchanged owner dependency"
	sealTestWarmRevision(revision)
	changed, binding, err := manager.BuildWarmInput(revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnsureWarm(ctx, changed, binding); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Pods("kodex-runtime").Get(ctx, "system-assistant-warm", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("changed specification retained warm Pod")
	}
}
