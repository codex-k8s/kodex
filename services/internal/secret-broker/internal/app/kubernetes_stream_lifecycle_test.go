package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/service/kubernetesoperation"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// blockedStreamOwner holds an admitted owner read after cancellation to expose the real dependency boundary.
// blockedStreamOwner удерживает admitted owner read после cancellation для проверки dependency boundary.
type blockedStreamOwner struct {
	targetrepo.StreamPlanOwner
	started, canceled, release chan struct{}
}

// ResolveKubernetesOperation models a slow dependency cleanup without issuing target or stream authority.
// ResolveKubernetesOperation моделирует slow dependency cleanup без выдачи target либо stream authority.
func (owner *blockedStreamOwner) ResolveKubernetesOperation(ctx context.Context, _ managedkubernetes.Binding) (value.KubernetesOperationSnapshot, error) {
	close(owner.started)
	<-ctx.Done()
	close(owner.canceled)
	<-owner.release
	return value.KubernetesOperationSnapshot{}, ctx.Err()
}

// unusedStreamPreparation exposes no executable native path in the cancellation-only fixture.
// unusedStreamPreparation не предоставляет executable native path в cancellation-only fixture.
type unusedStreamPreparation struct{ targetrepo.PodStreamPreparer }

// TestKubernetesStreamWorkerKeepsDependenciesAfterTimeout proves cancellation does not equal positive join.
// A later actual handler return permits cleanup without losing the original worker failure.
// TestKubernetesStreamWorkerKeepsDependenciesAfterTimeout доказывает, что cancellation не равна positive join.
// Последующий actual handler return допускает cleanup, не отменяя исходную ошибку worker.
func TestKubernetesStreamWorkerKeepsDependenciesAfterTimeout(t *testing.T) {
	owner := &blockedStreamOwner{started: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{})}
	streams, err := kubernetesoperation.NewStreamOperations(t.Context(), owner, unusedStreamPreparation{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	upload := &kubernetesIdentityUpload{streams: streams, shutdown: 25 * time.Millisecond}
	binding := managedkubernetes.Binding{WorkloadInstance: "gateway_synthetic", InvocationRef: "inv_synthetic", LeaseRef: "lease_synthetic", Fence: "synthetic-fence", Generation: 1, InputDigest: strings.Repeat("a", 64)}
	done := make(chan error, 1)
	go func() { _, err := streams.Execute(t.Context(), binding); done <- err }()
	select {
	case <-owner.started:
	case <-time.After(time.Second):
		t.Fatal("owner read did not start")
	}
	workerCtx, cancelWorker := context.WithCancel(t.Context())
	workerDone := make(chan error, 1)
	go func() { workerDone <- upload.streamWorker(context.Background())(workerCtx) }()
	cancelWorker()
	select {
	case <-owner.canceled:
	case <-time.After(time.Second):
		t.Fatal("worker cancellation did not reach owner read")
	}
	select {
	case err := <-workerDone:
		if err == nil {
			t.Fatal("blocked request fabricated joined worker")
		}
	case <-time.After(time.Second):
		t.Fatal("worker ignored shutdown budget")
	}
	closes := 0
	closeDependency := func() error { closes++; return nil }
	if upload.joined() || closeBrokerDependency(closeDependency, upload) == nil || closes != 0 || streams.Available() {
		t.Fatal("dependency closed before admitted request joined")
	}
	close(owner.release)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled owner read succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("released owner read did not join")
	}
	stop, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := streams.Close(stop); err != nil || !upload.joined() || closeBrokerDependency(closeDependency, upload) != nil || closes != 1 {
		t.Fatal("positive join did not permit dependency cleanup")
	}
}

// TestKubernetesStreamsStartupCleanup removes owned callbacks even when no worker or target operation started.
// TestKubernetesStreamsStartupCleanup удаляет owned callbacks также до запуска worker либо target operation.
func TestKubernetesStreamsStartupCleanup(t *testing.T) {
	owner := &blockedStreamOwner{}
	streams, err := kubernetesoperation.NewStreamOperations(t.Context(), owner, unusedStreamPreparation{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	upload := &kubernetesIdentityUpload{streams: streams}
	if upload.joined() {
		t.Fatal("unclosed lifecycle callback counted as joined")
	}
	stop, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if streams.Close(stop) != nil || !upload.joined() {
		t.Fatal("startup failure leaked owned stream lifecycle")
	}
}
