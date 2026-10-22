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

// TestStopTurnPreservesCallbackAuthorityUntilDurableCleanup checks a 150-second
// Pod grace budget, Pod-only Stop deletion and callback-ticket resolution until
// explicit DeleteTurn removes it. Fake readback is not proof of a durable owner
// completion receipt or successful live container shutdown.
// TestStopTurnPreservesCallbackAuthorityUntilDurableCleanup проверяет grace
// 150 секунд, удаление лишь Pod при Stop и доступность callback ticket до явного
// DeleteTurn. Fake readback не доказывает durable owner receipt завершения
// или успешную остановку реальных контейнеров.
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

// TestLeadershipRemainsHeldUntilVoluntaryDrainCompletes coordinates a worker's
// voluntary cancellation and drain using channels, checking the fake Lease stays
// held until join and is then released. It does not simulate a real competing
// controller or API-server lease persistence.
// TestLeadershipRemainsHeldUntilVoluntaryDrainCompletes через каналы согласует
// добровольную отмену и drain worker, проверяя удержание fake Lease до join
// и освобождение после него. Реальный конкурентный controller и устойчивое
// хранение lease в API server не моделируются.
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

// TestLeadershipLossIsDistinctFromVoluntaryShutdown cancels the leadership
// context before running the worker and requires the closed ErrLeadershipLost
// cause rather than conflating leadership loss with normal lifecycle shutdown.
// TestLeadershipLossIsDistinctFromVoluntaryShutdown отменяет leadership context
// до worker и требует закрытую причину ErrLeadershipLost, не смешивая
// потерю лидерства с обычной остановкой lifecycle.
func TestLeadershipLossIsDistinctFromVoluntaryShutdown(t *testing.T) {
	leadership, cancel := context.WithCancel(t.Context())
	cancel()
	err := runWithLeadership(t.Context(), leadership, func(ctx context.Context) error { <-ctx.Done(); return context.Cause(ctx) })
	if !errors.Is(err, ErrLeadershipLost) {
		t.Fatal("leadership loss was not closed separately")
	}
}
