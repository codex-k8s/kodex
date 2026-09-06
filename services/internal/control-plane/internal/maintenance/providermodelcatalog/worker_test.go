package providermodelcatalog

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
)

type fixture struct {
	calls, observed, completed int
	denial                     error
	task                       platformrepo.ProviderModelCatalogTask
	empty                      bool
}

func (f *fixture) ModelCatalogRequestDigest(platformrepo.ProviderModelCatalogTask) (string, error) {
	return "digest", nil
}
func (f *fixture) ClaimProviderModelCatalogTasks(context.Context, string, int32, platformrepo.ProviderModelCatalogEncoder) ([]platformrepo.ProviderModelCatalogTask, error) {
	f.calls++
	if f.empty {
		return nil, nil
	}
	return []platformrepo.ProviderModelCatalogTask{f.task}, nil
}

func TestCatalogBrokerDownPendingAndEmptyCyclesDoNotCompleteObservation(t *testing.T) {
	f := &fixture{task: platformrepo.ProviderModelCatalogTask{ExpiresAt: time.Now().Add(15 * time.Second)}, denial: errors.New("broker unavailable")}
	worker, err := New(f, f, func(context.Context) error { return nil }, "worker", serviceruntime.NewReadiness(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if worker.runCycle(t.Context()) == nil || f.observed != 1 || f.completed != 0 {
		t.Fatal("broker denial created a catalog observation")
	}
	// Пока предыдущая task принадлежит lease, claim пуст. Это не подтверждение
	// capability: completion по-прежнему отсутствует, следующее получение повторит RPC.
	f.empty = true
	if err := worker.runCycle(t.Context()); err != nil || f.observed != 1 || f.completed != 0 {
		t.Fatal("pending empty claim invented an observation")
	}
	f.empty = false
	if worker.runCycle(t.Context()) == nil || f.observed != 2 || f.completed != 0 {
		t.Fatal("broker remained unavailable but catalog became usable")
	}
}
func (f *fixture) CompleteProviderModelCatalogTask(context.Context, platformrepo.ProviderModelCatalogTask, platformrepo.ProviderModelCatalogObservation) error {
	f.completed++
	return nil
}
func (f *fixture) ObserveProviderModelCatalog(ctx context.Context, task platformrepo.ProviderModelCatalogTask) (platformrepo.ProviderModelCatalogObservation, error) {
	f.observed++
	deadline, ok := ctx.Deadline()
	if !ok || !deadline.Equal(task.ExpiresAt.Add(-finalizeReserve)) {
		return platformrepo.ProviderModelCatalogObservation{}, errors.New("deadline mismatch")
	}
	return platformrepo.ProviderModelCatalogObservation{Failure: "UNAVAILABLE"}, f.denial
}

func TestCatalogWorkerBarrierAndTransportDenialDoNotInventObservation(t *testing.T) {
	f := &fixture{task: platformrepo.ProviderModelCatalogTask{ExpiresAt: time.Now().Add(15 * time.Second)}}
	barrierErr := errors.New("not ready")
	worker, err := New(f, f, func(context.Context) error { return barrierErr }, "worker", serviceruntime.NewReadiness(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if worker.runCycle(t.Context()) == nil || f.calls != 0 || f.observed != 0 {
		t.Fatal("startup barrier bypassed")
	}
	barrierErr = nil
	f.denial = errors.New("proof rejected")
	if worker.runCycle(t.Context()) == nil || f.completed != 0 {
		t.Fatal("transport denial became observation")
	}
	f.denial = nil
	if err := worker.runCycle(t.Context()); err != nil || f.completed != 1 {
		t.Fatalf("safe provider failure not persisted: %v", err)
	}
	f.task.ExpiresAt = time.Now().Add(-time.Second)
	previous := f.observed
	if worker.runCycle(t.Context()) == nil || f.observed != previous {
		t.Fatal("expired task reached broker")
	}
}

func TestCatalogWorkerCancellationReturnsWithoutDetachedWork(t *testing.T) {
	f := &fixture{}
	worker, _ := New(f, f, func(ctx context.Context) error { return ctx.Err() }, "worker", serviceruntime.NewReadiness(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := worker.Run(ctx); !errors.Is(err, context.Canceled) || f.calls != 0 {
		t.Fatalf("cancelled worker continued: %v", err)
	}
}
