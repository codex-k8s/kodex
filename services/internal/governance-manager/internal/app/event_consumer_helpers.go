package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	eventconsumer "github.com/codex-k8s/kodex/libs/go/eventconsumer"

	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
)

type eventConsumerDomainError struct {
	target error
	result eventconsumer.Result
}

type eventConsumerPoison struct {
	code    string
	summary string
}

type governanceConsumerRuntime struct {
	Name             string
	LeaseOwner       string
	BatchSize        int
	PollInterval     time.Duration
	LeaseTTL         time.Duration
	HandlerTimeout   time.Duration
	RetryInitial     time.Duration
	RetryMax         time.Duration
	FailureLimit     int
	ConcurrencyLimit int
	MaxAttempts      int
}

func governanceEventConsumerConfig(
	name string,
	leaseOwner string,
	batchSize int,
	pollInterval time.Duration,
	leaseTTL time.Duration,
	handlerTimeout time.Duration,
	retryInitial time.Duration,
	retryMax time.Duration,
	failureLimit int,
	concurrencyLimit int,
	maxAttempts int,
) eventconsumer.Config {
	return eventconsumer.ConfigFromRuntimeValues(name, leaseOwner, batchSize, pollInterval, leaseTTL, handlerTimeout, retryInitial, retryMax, failureLimit, concurrencyLimit, maxAttempts)
}

func startGovernanceEventConsumer(
	ctx context.Context,
	enabled bool,
	logger *slog.Logger,
	errCh chan<- error,
	startMessage string,
	buildRunner func(*slog.Logger) (*eventconsumer.Runner, error),
) error {
	if !enabled {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	runner, err := buildRunner(logger)
	if err != nil {
		return err
	}
	go runGovernanceEventConsumer(ctx, runner, logger, errCh, startMessage)
	return nil
}

func runGovernanceEventConsumer(ctx context.Context, runner *eventconsumer.Runner, logger *slog.Logger, errCh chan<- error, startMessage string) {
	logger.Info(startMessage)
	if err := runner.Run(ctx); err != nil {
		errCh <- err
	}
}

func governanceConsumerError(err error, candidates []eventConsumerDomainError) eventconsumer.Result {
	for _, candidate := range candidates {
		if errors.Is(err, candidate.target) {
			return candidate.result
		}
	}
	return eventconsumer.Retry(err)
}

func governanceConsumerDomainErrors(invalid eventConsumerPoison, conflict eventConsumerPoison, forbidden eventConsumerPoison, unknown eventConsumerPoison, stale eventConsumerPoison) []eventConsumerDomainError {
	return []eventConsumerDomainError{
		governanceConsumerDomainError(errs.ErrInvalidArgument, invalid),
		governanceConsumerDomainError(errs.ErrConflict, conflict),
		governanceConsumerDomainError(errs.ErrForbidden, forbidden),
		governanceConsumerDomainError(errs.ErrNotFound, unknown),
		governanceConsumerDomainError(errs.ErrPreconditionFailed, stale),
	}
}

func governanceConsumerDomainError(target error, poison eventConsumerPoison) eventConsumerDomainError {
	return eventConsumerDomainError{target: target, result: eventconsumer.Poison(poison.code, poison.summary)}
}
