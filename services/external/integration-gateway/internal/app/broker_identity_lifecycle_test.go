package app

import (
	"context"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"sync"
	"testing"
	"time"
)

// lifecycleConsumerFixture uses the actual shared gate to keep adopted work unjoined while the common app attempts dependency shutdown.
// lifecycleConsumerFixture использует actual shared gate и удерживает adopted work unjoined при попытке common app закрыть dependencies.
type lifecycleConsumerFixture struct {
	brokerProbeFixture
	gate    *serviceruntime.ActivityGate
	failure chan struct{}
}

// Close stops only admission and never releases the test's adopted activity.
// Close закрывает лишь admission и не освобождает adopted activity теста.
func (f *lifecycleConsumerFixture) Close() error { return f.gate.Close() }

// Wait observes the actual shared positive-join boundary.
// Wait наблюдает actual shared positive-join boundary.
func (f *lifecycleConsumerFixture) Wait(ctx context.Context) error { return f.gate.Wait(ctx) }

// Joined keeps timeout distinct from actual release of the adopted activity.
// Joined отличает timeout от actual release adopted activity.
func (f *lifecycleConsumerFixture) Joined() bool { return f.gate.Joined() }

// Failure exposes one fixed lifecycle signal without retaining an error or owner identifiers.
// Failure открывает fixed lifecycle signal без сохранения error либо owner identifiers.
func (f *lifecycleConsumerFixture) Failure() <-chan struct{} { return f.failure }

// TestBrokerIdentityShutdownGuardsUnjoinedDependencies exercises both worker and adopted activity blockers with actual gates and collector channels, then verifies explicit joined recovery permits closure once.
// TestBrokerIdentityShutdownGuardsUnjoinedDependencies проверяет worker и adopted activity blockers через actual gates и collector channels; explicit joined recovery разрешает closure один раз.
func TestBrokerIdentityShutdownGuardsUnjoinedDependencies(t *testing.T) {
	for _, kind := range []string{"worker", "activity"} {
		t.Run(kind, func(t *testing.T) {
			held := make(chan struct{})
			var releaseHeldOnce sync.Once
			releaseHeld := func() { releaseHeldOnce.Do(func() { close(held) }) }
			gate, _ := serviceruntime.NewActivityGate(1)
			consumer := &lifecycleConsumerFixture{gate: gate, failure: make(chan struct{})}
			registry := &brokerIdentityConsumers{SSH: consumer}
			var release func()
			worker := func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
			if kind == "worker" {
				worker = func(context.Context) error { <-held; return nil }
			} else {
				release, _ = gate.Enter()
			}
			workers := serviceruntime.StartWorkers(t.Context(), worker)
			defer func() {
				if kind == "worker" {
					releaseHeld()
				} else {
					release()
				}
				workers.Stop()
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = workers.Wait(ctx)
			}()
			joinedErr := joinIntegrationWork(context.Background(), 10*time.Millisecond, workers, registry)
			count := 0
			operation := serviceruntime.ShutdownOperation{Name: "protected owner clients", Timeout: time.Second, Run: func(context.Context) error { count++; return nil }}
			if !errors.Is(joinedErr, errIntegrationWorkUnjoined) || integrationWorkJoined(workers, registry) {
				t.Fatal("timeout became positive join")
			}
			if !errors.Is(shutdownJoinedIntegrationDependencies(context.Background(), workers, registry, operation), errIntegrationWorkUnjoined) || count != 0 {
				t.Fatal("live work lost dependency")
			}
			if kind == "worker" {
				releaseHeld()
			} else {
				release()
			}
			if err := joinIntegrationWork(context.Background(), time.Second, workers, registry); err != nil {
				t.Fatal(err)
			}
			if err := shutdownJoinedIntegrationDependencies(context.Background(), workers, registry, operation); err != nil || count != 1 {
				t.Fatal("joined recovery did not close dependency exactly once")
			}
		})
	}
}

// TestBrokerIdentityJoinedFailureDoesNotSuppressDependencyClosure preserves a fatal error joined with cancellation while allowing safe dependency closure after actual local join.
// TestBrokerIdentityJoinedFailureDoesNotSuppressDependencyClosure сохраняет fatal error вместе с cancellation и разрешает safe dependency closure после actual local join.
func TestBrokerIdentityJoinedFailureDoesNotSuppressDependencyClosure(t *testing.T) {
	failure := errors.New("synthetic lifecycle failure")
	workers := serviceruntime.StartWorkers(t.Context(), func(context.Context) error { return errors.Join(context.Canceled, failure) })
	if err := joinIntegrationWork(context.Background(), time.Second, workers, nil); !errors.Is(err, failure) || !workers.Joined() {
		t.Fatal("joined failure was masked")
	}
	count := 0
	err := shutdownJoinedIntegrationDependencies(context.Background(), workers, nil, serviceruntime.ShutdownOperation{Name: "protected owner clients", Timeout: time.Second, Run: func(context.Context) error { count++; return nil }})
	if err != nil || count != 1 {
		t.Fatal("joined failure leaked safe dependency")
	}
}

// TestBrokerIdentityFailureCancelsAppBeforeWorkerJoin checks the actual owned monitor signals the app lifecycle before a deliberately stuck peer can join, then retains the fatal result.
// TestBrokerIdentityFailureCancelsAppBeforeWorkerJoin проверяет, что actual owned monitor отменяет app lifecycle до join stuck peer и затем сохраняет fatal result.
func TestBrokerIdentityFailureCancelsAppBeforeWorkerJoin(t *testing.T) {
	lifecycle, stop := context.WithCancel(t.Context())
	defer stop()
	gate, _ := serviceruntime.NewActivityGate(1)
	consumer := &lifecycleConsumerFixture{gate: gate, failure: make(chan struct{})}
	registry := &brokerIdentityConsumers{SSH: consumer}
	held := make(chan struct{})
	workers := serviceruntime.StartWorkers(lifecycle, monitorBrokerIdentityConsumers(registry, stop), func(context.Context) error { <-held; return nil })
	defer func() {
		stop()
		select {
		case <-held:
		default:
			close(held)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = workers.Wait(ctx)
	}()
	close(consumer.failure)
	select {
	case <-lifecycle.Done():
	case <-time.After(time.Second):
		t.Fatal("fatal consumer did not cancel app lifecycle")
	}
	if workers.Joined() {
		t.Fatal("fatal signal became worker join")
	}
	close(held)
	if err := joinIntegrationWork(context.Background(), time.Second, workers, registry); !errors.Is(err, brokerIdentityWorkUnavailable) || !workers.Joined() {
		t.Fatal("fatal monitor result lost after join")
	}
}
