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

// pollInterval sets the five-second interval between catalog and renewal worker cycles without
// making a successful poll proof of provider capability.
// pollInterval задаёт пятисекундный интервал между циклами catalog и renewal workers без признания
// успешного poll доказательством provider capability.
const pollInterval = 5 * time.Second

// finalizeReserve reserves the final second of a catalog task lease for owner completion rather
// than allowing observation to consume the whole lease.
// finalizeReserve резервирует последнюю секунду lease catalog task для owner completion вместо
// расходования всего lease наблюдением.
const finalizeReserve = time.Second

// unavailableReason names catalog observer readiness degradation independently of a persisted
// provider observation or a usable model capability.
// unavailableReason задаёт причину ухудшения readiness catalog observer независимо от сохранённого
// provider observation либо пригодной model capability.
const unavailableReason = "provider_model_catalog_observer_unavailable"

// Observer combines exact catalog-request encoding with the external observation port; transport
// failure must not be converted into invented provider evidence.
// Observer объединяет точное кодирование catalog request с внешним observation портом; transport
// failure не превращается в выдуманное provider свидетельство.
type Observer interface {
	platformrepo.ProviderModelCatalogEncoder
	// ObserveProviderModelCatalog observes one owner-issued catalog task under its bounded context and
	// returns typed provider evidence separately from transport failure.
	// ObserveProviderModelCatalog наблюдает одну catalog task владельца под ограниченным context и
	// возвращает типизированное provider свидетельство отдельно от transport failure.
	ObserveProviderModelCatalog(context.Context, platformrepo.ProviderModelCatalogTask) (platformrepo.ProviderModelCatalogObservation, error)
}

// Worker coordinates owner catalog claims, observation deadlines and completion behind an explicit
// readiness barrier with separate observer health.
// Worker согласует catalog claims владельца, observation deadlines и completion за явным readiness
// barrier с отдельным observer health.
type Worker struct {
	repository platformrepo.ProviderModelCatalogRepository
	observer   Observer
	barrier    func(context.Context) error
	claimant   string
	health     *serviceruntime.Readiness
	logger     *slog.Logger
}

// New validates required repository/observer/barrier/health/logger ports and bounded claimant
// identity without starting background work.
// New проверяет обязательные repository, observer, barrier, health и logger порты и ограниченную
// claimant identity без запуска фоновой работы.
func New(repository platformrepo.ProviderModelCatalogRepository, observer Observer, barrier func(context.Context) error, claimant string, health *serviceruntime.Readiness, logger *slog.Logger) (*Worker, error) {
	if repository == nil || observer == nil || barrier == nil || claimant == "" || len(claimant) > 128 || health == nil || logger == nil {
		return nil, errors.New("provider model catalog worker configuration is invalid")
	}
	return &Worker{repository: repository, observer: observer, barrier: barrier, claimant: claimant, health: health, logger: logger}, nil
}

// Run executes bounded claim/observe cycles, degrades health on cycle failure and restores it
// after a successful cycle, waiting between cycles until caller cancellation.
// Run исполняет ограниченные claim и observe циклы, ухудшает health при отказе цикла и
// восстанавливает после успешного цикла с ожиданием между циклами до отмены caller.
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

// runCycle requires a five-second owner barrier, claims at most one task and reserves lease time
// for completion; transport denial returns without synthesizing an observation or detaching work.
// runCycle требует пятисекундный owner barrier, получает не более одной task и резервирует lease
// время для completion; transport denial возвращается без синтетического observation либо
// отделения работы.
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
