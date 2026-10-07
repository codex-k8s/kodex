package workload

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEnsureTurnGuardDenialDoesNotPublishPod(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, binding, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatal(err)
	}
	denied := errors.New("lease revoked")
	err = manager.EnsureTurnGuarded(t.Context(), input, binding, testCredentialProjection(input), func(func(context.Context) error) error { return denied })
	if !errors.Is(err, denied) {
		t.Fatal("publication guard denial was lost")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "create" && action.GetResource().Resource == "pods" {
			t.Fatal("denied guard published a Pod")
		}
	}
}

func TestEnsureTurnGuardUsesCancellablePublicationContext(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "cancelled"}[cancelled], func(t *testing.T) {
			client := fake.NewSimpleClientset()
			manager := newTestManager(t, client)
			input, binding, err := manager.BuildTurnInput(testExecution(false))
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			err = manager.EnsureTurnGuarded(t.Context(), input, binding, testCredentialProjection(input), func(publish func(context.Context) error) error {
				calls++
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if cancelled {
					cancel()
				}
				return publish(ctx)
			})
			if calls != 1 || cancelled && !errors.Is(err, context.Canceled) || !cancelled && err != nil {
				t.Fatal("publication did not use the guard context exactly once")
			}
			pods, getErr := client.CoreV1().Pods(manager.config.RuntimeNamespace).List(t.Context(), metav1.ListOptions{})
			if getErr != nil || len(pods.Items) != map[bool]int{false: 1, true: 0}[cancelled] {
				t.Fatal("publication escaped the cancellation fence")
			}
		})
	}
}
