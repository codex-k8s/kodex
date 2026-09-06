package workload

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestStopTurnPreservesCallbackAuthorityUntilDurableCleanup(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	execution := testExecution(false)
	sealTestTurnExecution(execution)
	input, binding, err := manager.BuildTurnInput(execution)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.EnsureTurn(t.Context(), input, binding, testCredentialProjection(input)); err != nil {
		t.Fatal(err)
	}
	pod, err := client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(t.Context(), runtimecontract.RuntimeTurnPodName(input.LeaseRef), metav1.GetOptions{})
	if err != nil || pod.Spec.TerminationGracePeriodSeconds == nil || *pod.Spec.TerminationGracePeriodSeconds != 150 {
		t.Fatal("runtime shutdown budget was not materialized")
	}
	secret, err := client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(t.Context(), ticketName(input.LeaseRef), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	client.ClearActions()
	if err := manager.StopTurn(t.Context(), input.LeaseRef); err != nil {
		t.Fatal(err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "delete" {
			continue
		}
		if action.GetResource().Resource != "pods" {
			t.Fatal("stop removed callback material before receipt")
		}
		if value := action.(ktesting.DeleteAction).GetDeleteOptions().GracePeriodSeconds; value != nil && *value != 150 {
			t.Fatal("stop bypassed pod grace")
		}
	}
	resolved, err := manager.ResolveTurn(t.Context(), input.LeaseRef, string(secret.Data[ticketKey]))
	if err != nil || resolved.RuntimeRevisionDigest != input.RuntimeRevisionDigest {
		t.Fatal("callback binding lost during graceful stop")
	}
	if err := manager.DeleteTurn(t.Context(), input.LeaseRef); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ResolveTurn(t.Context(), input.LeaseRef, string(secret.Data[ticketKey])); err == nil {
		t.Fatal("terminal cleanup retained callback ticket")
	}
}

func TestLeadershipRemainsHeldUntilVoluntaryDrainCompletes(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	lifecycle, cancel := context.WithCancel(t.Context())
	started, release := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- manager.RunAsLeader(lifecycle, func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			if errors.Is(context.Cause(ctx), ErrLeadershipLost) {
				return errors.New("voluntary shutdown lost leadership")
			}
			<-release
			return ctx.Err()
		})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("leader did not start")
	}
	cancel()
	lease, err := client.CoordinationV1().Leases(manager.config.ControlNamespace).Get(t.Context(), "runtime-controller-leader", metav1.GetOptions{})
	if err != nil || lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity != manager.config.ControllerPodUID {
		close(release)
		t.Fatal("leader released before drain")
	}
	close(release)
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("leader did not join after drain")
	}
	lease, err = client.CoordinationV1().Leases(manager.config.ControlNamespace).Get(t.Context(), "runtime-controller-leader", metav1.GetOptions{})
	if err != nil || lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity != "" {
		t.Fatal("leader retained lease after drain")
	}
}

func TestLeadershipLossIsDistinctFromVoluntaryShutdown(t *testing.T) {
	leadership, cancel := context.WithCancel(t.Context())
	cancel()
	err := runWithLeadership(t.Context(), leadership, func(ctx context.Context) error { <-ctx.Done(); return context.Cause(ctx) })
	if !errors.Is(err, ErrLeadershipLost) {
		t.Fatal("leadership loss was not closed separately")
	}
}
