// Package providercredentialcleanup выполняет ограниченную фоновую очистку
// заменённого provider credential material.
package providercredentialcleanup

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	platformrepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	SafeCodeUnavailable = "PROVIDER_CREDENTIAL_CLEANUP_UNAVAILABLE"
	SafeCodeRejected    = "PROVIDER_CREDENTIAL_CLEANUP_REJECTED"
	SafeCodeTimeout     = "PROVIDER_CREDENTIAL_CLEANUP_TIMEOUT"
	SafeCodeFailed      = "PROVIDER_CREDENTIAL_CLEANUP_FAILED"

	claimUnavailableReason  = "provider_credential_cleanup_claim_unavailable"
	minimumPollInterval     = 50 * time.Millisecond
	maximumPollInterval     = time.Minute
	minimumOperationTimeout = 100 * time.Millisecond
	maximumOperationTimeout = 30 * time.Second
	maximumIdleInterval     = 5 * time.Second
)

type Repository interface {
	ClaimProviderCredentialCleanupTasks(context.Context, string, int32) ([]platformrepository.ProviderCredentialCleanupTask, error)
	CompleteProviderCredentialCleanupTask(context.Context, string, string, int64, string) (platformrepository.ProviderCredentialCleanupResult, error)
	FailProviderCredentialCleanupTask(context.Context, string, string, int64, string) (platformrepository.ProviderCredentialCleanupResult, error)
}

type Materializer interface {
	CleanupProviderCredential(context.Context, string, string, int64, entity.ProviderCredentialDescriptor) (string, error)
}

type Config struct {
	LeaseOwner       string
	BatchSize        int32
	PollInterval     time.Duration
	OperationTimeout time.Duration
}

type Worker struct {
	repository   Repository
	materializer Materializer
	health       *serviceruntime.Readiness
	logger       *slog.Logger
	config       Config
}

func New(
	repository Repository,
	materializer Materializer,
	health *serviceruntime.Readiness,
	logger *slog.Logger,
	config Config,
) (*Worker, error) {
	if repository == nil || materializer == nil || health == nil || logger == nil ||
		config.LeaseOwner == "" || len(config.LeaseOwner) > 128 ||
		config.BatchSize < 1 || config.BatchSize > 16 ||
		config.PollInterval < minimumPollInterval || config.PollInterval > maximumPollInterval ||
		config.OperationTimeout < minimumOperationTimeout || config.OperationTimeout > maximumOperationTimeout {
		return nil, errors.New("provider credential cleanup worker configuration is invalid")
	}
	return &Worker{
		repository: repository, materializer: materializer, health: health,
		logger: logger, config: config,
	}, nil
}

func (worker *Worker) Run(ctx context.Context) error {
	idleBackoff := serviceruntime.NewIdleBackoff(worker.config.PollInterval, maximumIdleInterval)
	for {
		processed, err := worker.runCycle(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if worker.health.Set(false, claimUnavailableReason) {
				worker.logger.WarnContext(ctx, "provider credential cleanup claim degraded", "error_class", "postgresql")
			}
		} else if worker.health.Set(true, "ready") {
			worker.logger.InfoContext(ctx, "provider credential cleanup claim restored")
		}

		timer := time.NewTimer(idleBackoff.Next(err == nil && processed > 0))
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

func (worker *Worker) runCycle(ctx context.Context) (int, error) {
	claimCtx, cancelClaim := context.WithTimeout(ctx, worker.config.OperationTimeout)
	tasks, err := worker.repository.ClaimProviderCredentialCleanupTasks(
		claimCtx,
		worker.config.LeaseOwner,
		worker.config.BatchSize,
	)
	cancelClaim()
	if err != nil {
		return 0, err
	}

	var wait sync.WaitGroup
	wait.Add(len(tasks))
	for _, task := range tasks {
		task := task
		go func() {
			defer wait.Done()
			if stage := worker.processTask(ctx, task); stage != "" && ctx.Err() == nil {
				worker.logger.WarnContext(ctx, "provider credential cleanup task finalization failed", "stage", stage)
			}
		}()
	}
	wait.Wait()
	return len(tasks), nil
}

func (worker *Worker) processTask(ctx context.Context, task platformrepository.ProviderCredentialCleanupTask) string {
	cleanupCtx, cancelCleanup := context.WithTimeout(ctx, worker.config.OperationTimeout)
	receipt, cleanupErr := worker.materializer.CleanupProviderCredential(
		cleanupCtx,
		task.Ref,
		task.AccountRef,
		task.Generation,
		task.Credential,
	)
	cancelCleanup()
	if ctx.Err() != nil {
		return ""
	}

	finalizeCtx, cancelFinalize := context.WithTimeout(ctx, worker.config.OperationTimeout)
	defer cancelFinalize()
	if cleanupErr != nil {
		_, err := worker.repository.FailProviderCredentialCleanupTask(
			finalizeCtx,
			task.Ref,
			worker.config.LeaseOwner,
			task.Generation,
			safeErrorCode(cleanupErr),
		)
		if err != nil {
			return "fail"
		}
		return ""
	}
	if _, err := worker.repository.CompleteProviderCredentialCleanupTask(
		finalizeCtx,
		task.Ref,
		worker.config.LeaseOwner,
		task.Generation,
		receipt,
	); err != nil {
		return "complete"
	}
	return ""
}

func safeErrorCode(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || status.Code(err) == codes.DeadlineExceeded {
		return SafeCodeTimeout
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.ResourceExhausted:
		return SafeCodeUnavailable
	case codes.InvalidArgument, codes.Unauthenticated, codes.PermissionDenied,
		codes.NotFound, codes.AlreadyExists, codes.FailedPrecondition, codes.Aborted:
		return SafeCodeRejected
	default:
		return SafeCodeFailed
	}
}
