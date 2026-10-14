package providermodelcatalog

import (
	"context"
	"errors"
	"log/slog"
	"time"

	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
)

type CredentialRenewer interface {
	port.ProviderCredentialRenewalEncoder
	RenewManagedProviderCredential(context.Context, port.ProviderCredentialRenewalTask) (port.ProviderCredentialRenewalResult, error)
	ObserveManagedProviderCredentialRenewal(context.Context, port.ProviderCredentialRenewalTask) (port.ProviderCredentialRenewalResult, error)
}

type RenewalWorker struct {
	repository port.ProviderCredentialRenewalRepository
	client     CredentialRenewer
	barrier    func(context.Context) error
	claimant   string
	logger     *slog.Logger
}

func NewRenewalWorker(repository port.ProviderCredentialRenewalRepository, client CredentialRenewer, barrier func(context.Context) error, claimant string, logger *slog.Logger) (*RenewalWorker, error) {
	if repository == nil || client == nil || barrier == nil || claimant == "" || logger == nil {
		return nil, errors.New("provider credential renewal worker configuration is invalid")
	}
	return &RenewalWorker{repository: repository, client: client, barrier: barrier, claimant: claimant, logger: logger}, nil
}

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
