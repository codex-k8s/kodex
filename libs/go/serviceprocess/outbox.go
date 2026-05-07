package serviceprocess

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	eventlog "github.com/codex-k8s/kodex/libs/go/eventlog"
	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
)

// OpenEventLogPool opens the shared event-log DB pool only when a service needs it.
func OpenEventLogPool(ctx context.Context, enabled bool, settings postgreslib.PoolSettings) (*pgxpool.Pool, error) {
	if !enabled {
		return nil, nil
	}
	pool, err := postgreslib.OpenPool(ctx, settings)
	if err != nil {
		return nil, fmt.Errorf("open platform event log database pool: %w", err)
	}
	return pool, nil
}

// EventLogAppender returns an appender for a configured event-log pool.
func EventLogAppender(pool *pgxpool.Pool) eventlog.Appender {
	if pool == nil {
		return nil
	}
	return eventlog.NewStore(pool)
}

// OutboxRuntimeConfig contains process-level outbox publisher settings.
type OutboxRuntimeConfig struct {
	PublisherKind       string
	AllowLossyPublisher bool
	EventLogSource      string
	Dispatcher          outboxlib.Config
}

// StartOutboxDispatcher creates and starts a shared outbox dispatcher.
func StartOutboxDispatcher[E any](
	ctx context.Context,
	serviceName string,
	store outboxlib.EntityStore[E],
	eventMapper func(E) outboxlib.Event,
	cfg OutboxRuntimeConfig,
	appender eventlog.Appender,
	logger *slog.Logger,
	errCh chan<- error,
) error {
	publisher, err := eventlog.NewOutboxPublisher(
		cfg.PublisherKind,
		cfg.AllowLossyPublisher,
		cfg.EventLogSource,
		appender,
		logger,
		serviceName,
	)
	if err != nil {
		return err
	}
	dispatcher := outboxlib.NewDispatcher(
		outboxlib.NewStoreAdapter(store, eventMapper),
		publisher,
		cfg.Dispatcher,
		logger,
		serviceName,
	)
	go func() {
		logger.Info(serviceName + " outbox dispatcher starting")
		if err := dispatcher.Run(ctx); err != nil {
			errCh <- err
		}
	}()
	return nil
}
