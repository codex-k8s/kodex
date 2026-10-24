package providermodelcatalog

import (
	"context"
	"errors"
	"log/slog"
	"time"

	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
)

// CredentialRenewer provides exact renewal request encoding plus distinct write and readback ports
// whose typed results contain metadata rather than token values.
// CredentialRenewer предоставляет точное кодирование renewal request и отдельные write и readback
// порты с типизированными metadata результатами вместо token values.
type CredentialRenewer interface {
	port.ProviderCredentialRenewalEncoder
	// RenewManagedProviderCredential attempts one owner-issued managed credential renewal and returns
	// its typed outcome or an uncertain transport failure.
	// RenewManagedProviderCredential пытается выполнить одно managed credential renewal по task
	// владельца и возвращает типизированный исход либо неопределённый transport failure.
	RenewManagedProviderCredential(context.Context, port.ProviderCredentialRenewalTask) (port.ProviderCredentialRenewalResult, error)
	// ObserveManagedProviderCredentialRenewal reads the outcome of an existing renewal task without
	// repeating its external OAuth write.
	// ObserveManagedProviderCredentialRenewal читает исход существующей renewal task без повторения её
	// внешней OAuth записи.
	ObserveManagedProviderCredentialRenewal(context.Context, port.ProviderCredentialRenewalTask) (port.ProviderCredentialRenewalResult, error)
}

// RenewalWorker runs owner renewal claims and read-only recovery after explicit owner/renewal
// readiness checks, with bounded independent completion after uncertain writes.
// RenewalWorker исполняет renewal claims владельца и read-only recovery после явных owner и
// renewal readiness проверок с ограниченным независимым completion после неопределённых записей.
type RenewalWorker struct {
	repository port.ProviderCredentialRenewalRepository
	client     CredentialRenewer
	barrier    func(context.Context) error
	claimant   string
	logger     *slog.Logger
}

// NewRenewalWorker requires repository/client/barrier/logger and nonempty claimant dependencies
// before returning an unstarted renewal worker.
// NewRenewalWorker требует repository, client, barrier, logger и непустой claimant до возврата
// незапущенного renewal worker.
func NewRenewalWorker(repository port.ProviderCredentialRenewalRepository, client CredentialRenewer, barrier func(context.Context) error, claimant string, logger *slog.Logger) (*RenewalWorker, error) {
	if repository == nil || client == nil || barrier == nil || claimant == "" || logger == nil {
		return nil, errors.New("provider credential renewal worker configuration is invalid")
	}
	return &RenewalWorker{repository: repository, client: client, barrier: barrier, claimant: claimant, logger: logger}, nil
}

// Run repeats renewal cycles at the shared interval, logs one fixed warning per failed active
// cycle and returns caller cancellation without spawning detached workers.
// Run повторяет renewal циклы с общим интервалом, записывает одно фиксированное предупреждение
// отказавшего активного цикла и возвращает отмену caller без отделённых workers.
func (worker *RenewalWorker) Run(ctx context.Context) error {
	for {
		if err := worker.runCycle(ctx); err != nil && ctx.Err() == nil {
			worker.logger.WarnContext(ctx, "provider credential renewal cycle failed")
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// runCycle checks startup and renewal readiness before claims, bounds writes to thirty-five
// seconds and persists UNKNOWN_OUTCOME on call failure with an independent five-second budget;
// recovery only reads and completes SUCCEEDED results.
// runCycle проверяет startup и renewal readiness до claims, ограничивает write тридцатью пятью
// секундами и сохраняет UNKNOWN_OUTCOME при call failure с независимым пятисекундным бюджетом;
// recovery только читает и завершает SUCCEEDED результаты.
func (worker *RenewalWorker) runCycle(ctx context.Context) error {
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := worker.barrier(check)
	if err == nil {
		err = worker.repository.CheckProviderCredentialRenewalReadiness(check)
	}
	cancel()
	if err != nil {
		return err
	}
	tasks, err := worker.repository.ClaimProviderCredentialRenewalTasks(ctx, worker.claimant, worker.client)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		call, cancel := context.WithTimeout(ctx, 35*time.Second)
		result, callErr := worker.client.RenewManagedProviderCredential(call, task)
		cancel()
		if callErr != nil {
			result = port.ProviderCredentialRenewalResult{State: "UNKNOWN_OUTCOME"}
		}
		finalize, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = worker.repository.CompleteProviderCredentialRenewalTask(finalize, task, result)
		cancel()
		if err != nil {
			return err
		}
	}
	recoveries, err := worker.repository.ListProviderCredentialRenewalRecoveryTasks(ctx)
	if err != nil {
		return err
	}
	for _, task := range recoveries {
		observe, cancel := context.WithTimeout(ctx, 10*time.Second)
		result, err := worker.client.ObserveManagedProviderCredentialRenewal(observe, task)
		cancel()
		if err != nil || result.State != "SUCCEEDED" {
			continue
		}
		finalize, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = worker.repository.CompleteProviderCredentialRenewalTask(finalize, task, result)
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}
