package app

import (
	"context"
	"log/slog"

	eventconsumer "github.com/codex-k8s/kodex/libs/go/eventconsumer"
)

type managedEventConsumerStarter struct {
	enabled  bool
	logger   *slog.Logger
	errCh    chan<- error
	validate func() error
	runner   func(*slog.Logger) (*eventconsumer.Runner, error)
	run      func(context.Context, *eventconsumer.Runner, *slog.Logger, chan<- error)
}

func startManagedEventConsumer(ctx context.Context, starter managedEventConsumerStarter) error {
	if !starter.enabled {
		return nil
	}
	logger := starter.logger
	if logger == nil {
		logger = slog.Default()
	}
	if err := starter.validate(); err != nil {
		return err
	}
	runner, err := starter.runner(logger)
	if err != nil {
		return err
	}
	go starter.run(ctx, runner, logger, starter.errCh)
	return nil
}

func startManagedEventConsumerWithParts(ctx context.Context, enabled bool, logger *slog.Logger, errCh chan<- error, validate func() error, runner func(*slog.Logger) (*eventconsumer.Runner, error), run func(context.Context, *eventconsumer.Runner, *slog.Logger, chan<- error)) error {
	return startManagedEventConsumer(ctx, managedEventConsumerStarter{
		enabled:  enabled,
		logger:   logger,
		errCh:    errCh,
		validate: validate,
		runner:   runner,
		run:      run,
	})
}
