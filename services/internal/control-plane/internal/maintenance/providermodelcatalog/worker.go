// Package providermodelcatalog обновляет account-scoped наблюдения capabilities.
package providermodelcatalog

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
)

const pollInterval = 5 * time.Second
const finalizeReserve = time.Second
const unavailableReason = "provider_model_catalog_observer_unavailable"

type Observer interface {
	platformrepo.ProviderModelCatalogEncoder
	ObserveProviderModelCatalog(context.Context, platformrepo.ProviderModelCatalogTask) (platformrepo.ProviderModelCatalogObservation, error)
}

type Worker struct {
	repository platformrepo.ProviderModelCatalogRepository
	observer   Observer
	barrier    func(context.Context) error
	claimant   string
	health     *serviceruntime.Readiness
	logger     *slog.Logger
}

func New(repository platformrepo.ProviderModelCatalogRepository, observer Observer, barrier func(context.Context) error, claimant string, health *serviceruntime.Readiness, logger *slog.Logger) (*Worker, error) {
	if repository == nil || observer == nil || barrier == nil || claimant == "" || len(claimant) > 128 || health == nil || logger == nil {
		return nil, errors.New("provider model catalog worker configuration is invalid")
	}
	return &Worker{repository: repository, observer: observer, barrier: barrier, claimant: claimant, health: health, logger: logger}, nil
}

func (worker *Worker) Run(ctx context.Context) error {
	for {
		err := worker.runCycle(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if worker.health.Set(false, unavailableReason) {
				worker.logger.WarnContext(ctx, "provider model catalog observer degraded")
			}
		} else {
			worker.health.Set(true, "ready")
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (worker *Worker) runCycle(ctx context.Context) error {
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := worker.barrier(check)
	cancel()
	if err != nil {
		return err
	}
	tasks, err := worker.repository.ClaimProviderModelCatalogTasks(ctx, worker.claimant, 1, worker.observer)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if !task.ExpiresAt.After(time.Now().Add(finalizeReserve)) {
			return errors.New("provider model catalog lease expired")
		}
		observe, cancelObserve := context.WithDeadline(ctx, task.ExpiresAt.Add(-finalizeReserve))
		observation, err := worker.observer.ObserveProviderModelCatalog(observe, task)
		cancelObserve()
		// Transport denial не маскируется provider failure. Durable claim истечёт,
		// следующая попытка получит новый task/ref/proof после owner eligibility.
		if err != nil {
			return err
		}
		complete, cancelComplete := context.WithDeadline(ctx, task.ExpiresAt)
		err = worker.repository.CompleteProviderModelCatalogTask(complete, task, observation)
		cancelComplete()
		if err != nil {
			return err
		}
	}
	return nil
}
