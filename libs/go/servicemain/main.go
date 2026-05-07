package servicemain

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// LoadConfigFunc loads a typed service configuration.
type LoadConfigFunc[C any] func() (C, error)

// RunFunc starts a service with a typed configuration.
type RunFunc[C any] func(context.Context, C, *slog.Logger) error

// Run starts a service with the shared signal, logging and exit-code behavior.
func Run[C any](serviceName string, loadConfig LoadConfigFunc[C], runService RunFunc[C]) {
	os.Exit(run(context.Background(), serviceName, slog.New(slog.NewJSONHandler(os.Stdout, nil)), loadConfig, runService))
}

func run[C any](
	parentCtx context.Context,
	serviceName string,
	logger *slog.Logger,
	loadConfig LoadConfigFunc[C],
	runService RunFunc[C],
) int {
	ctx, stop := signal.NotifyContext(parentCtx, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	defer stop()

	cfg, err := loadConfig()
	if err != nil {
		logger.Error(serviceName+" config failed", "error", err)
		return 1
	}
	if err := runService(ctx, cfg, logger); err != nil {
		logger.Error(serviceName+" stopped", "error", err)
		return 1
	}
	return 0
}
