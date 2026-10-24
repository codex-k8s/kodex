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

// fixture counts synthetic claims/observations/completions with optional empty claims and a
// transport denial for one configured catalog task.
// fixture считает синтетические claims, observations и completions с необязательными пустыми
// claims и transport denial одной заданной catalog task.
type fixture struct {
	calls, observed, completed int
	denial                     error
	task                       platformrepo.ProviderModelCatalogTask
	empty                      bool
}

// ModelCatalogRequestDigest returns a fixed synthetic request commitment used only by the hermetic
// catalog-worker fixture.
// ModelCatalogRequestDigest возвращает фиксированный синтетический request commitment только для
// герметичной оснастки catalog worker.
func (f *fixture) ModelCatalogRequestDigest(platformrepo.ProviderModelCatalogTask) (string, error) {
	return "digest", nil
}

// ClaimProviderModelCatalogTasks counts the synthetic claim and returns either no work or the
// configured task without manufacturing a completed observation.
// ClaimProviderModelCatalogTasks считает синтетический claim и возвращает отсутствие работы либо
// заданную task без выдуманного completed observation.
func (f *fixture) ClaimProviderModelCatalogTasks(context.Context, string, int32, platformrepo.ProviderModelCatalogEncoder) ([]platformrepo.ProviderModelCatalogTask, error) {
	f.calls++
	if f.empty {
		return nil, nil
	}
	return []platformrepo.ProviderModelCatalogTask{f.task}, nil
}

// TestCatalogBrokerDownPendingAndEmptyCyclesDoNotCompleteObservation checks broker transport
// denial and a later empty pending claim never create completion, and a new claim still retries
// observation while the broker remains unavailable.
// TestCatalogBrokerDownPendingAndEmptyCyclesDoNotCompleteObservation проверяет отсутствие
// completion при broker transport denial и последующем пустом pending claim и повтор observation
// новым claim при сохраняющейся недоступности broker.
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

// CompleteProviderModelCatalogTask counts an owner completion in the synthetic fixture without
// persisting live catalog data.
// CompleteProviderModelCatalogTask считает owner completion в синтетической оснастке без
// сохранения живых catalog данных.
func (f *fixture) CompleteProviderModelCatalogTask(context.Context, platformrepo.ProviderModelCatalogTask, platformrepo.ProviderModelCatalogObservation) error {
	f.completed++
	return nil
}

// ObserveProviderModelCatalog checks the observation deadline leaves the exact finalization
// reserve and returns typed synthetic provider failure separately from optional transport denial.
// ObserveProviderModelCatalog проверяет observation deadline с точным finalization reserve и
// возвращает типизированный синтетический provider failure отдельно от необязательного transport
// denial.
func (f *fixture) ObserveProviderModelCatalog(ctx context.Context, task platformrepo.ProviderModelCatalogTask) (platformrepo.ProviderModelCatalogObservation, error) {
	f.observed++
	deadline, ok := ctx.Deadline()
	if !ok || !deadline.Equal(task.ExpiresAt.Add(-finalizeReserve)) {
		return platformrepo.ProviderModelCatalogObservation{}, errors.New("deadline mismatch")
	}
	return platformrepo.ProviderModelCatalogObservation{Failure: "UNAVAILABLE"}, f.denial
}

// TestCatalogWorkerBarrierAndTransportDenialDoNotInventObservation checks startup rejection
// prevents claims, transport rejection prevents completion, valid typed provider failure can
// complete and an expired task never reaches the broker.
// TestCatalogWorkerBarrierAndTransportDenialDoNotInventObservation проверяет запрет claim при
// startup отказе, запрет completion при transport отказе, допуск completion правильного typed
// provider failure и отсутствие обращения к broker истёкшей task.
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

// TestCatalogWorkerCancellationReturnsWithoutDetachedWork checks an already canceled worker exits
// with context cancellation before claiming or observing a task.
// TestCatalogWorkerCancellationReturnsWithoutDetachedWork проверяет выход уже отменённого worker с
// context cancellation до claim либо observation task.
func TestCatalogWorkerCancellationReturnsWithoutDetachedWork(t *testing.T) {
	f := &fixture{}
	worker, _ := New(f, f, func(ctx context.Context) error { return ctx.Err() }, "worker", serviceruntime.NewReadiness(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := worker.Run(ctx); !errors.Is(err, context.Canceled) || f.calls != 0 {
		t.Fatalf("cancelled worker continued: %v", err)
	}
}
