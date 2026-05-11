package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	fleetpostgres "github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/repository/postgres/fleet"
	fleetgrpc "github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/transport/grpc"
)

// Run starts fleet-manager process servers and shuts them down with context.
func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	dbPool, err := postgreslib.OpenPool(ctx, cfg.DatabasePoolSettings())
	if err != nil {
		return err
	}
	defer dbPool.Close()
	eventLogPool, err := serviceprocess.OpenEventLogPool(ctx, cfg.needsEventLogDatabase(), cfg.EventLogDatabasePoolSettings())
	if err != nil {
		return err
	}
	if eventLogPool != nil {
		defer eventLogPool.Close()
	}

	fleetRepository := fleetpostgres.NewRepository(dbPool)
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           serviceprocess.NewHealthMux(readinessChecks(fleetRepository, eventLogPool), 2*time.Second),
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcMetrics, err := grpcserver.NewMetrics(nil, grpcserver.MetricsConfig{
		Subsystem:   "fleet_manager_grpc",
		ServiceName: "fleet-manager",
	})
	if err != nil {
		return err
	}
	grpcServer, err := grpcserver.NewServer(cfg.GRPCServerConfig(), grpcserver.Dependencies{
		Logger:        logger,
		Metrics:       grpcMetrics,
		Authenticator: grpcserver.NewSharedTokenAuthenticator(cfg.GRPC.AuthToken),
	})
	if err != nil {
		return err
	}
	fleetgrpc.RegisterFleetManagerService(grpcServer)

	errCh := make(chan error, 3)
	go serviceprocess.StartHTTPServer(httpServer, "fleet-manager", logger, errCh)
	go serviceprocess.StartGRPCServer(grpcServer, "fleet-manager", cfg.GRPCAddr, logger, errCh)
	outboxRuntime := serviceprocess.OutboxRuntimeConfig{
		Dispatcher:          cfg.OutboxDispatcherConfig(),
		EventLogSource:      cfg.Outbox.EventLogSource,
		PublisherKind:       cfg.Outbox.PublisherKind,
		DispatchEnabled:     cfg.Outbox.DispatchEnabled,
		AllowLossyPublisher: cfg.Outbox.AllowLossyPublisher,
	}
	err = serviceprocess.StartOutboxDispatcher(
		ctx,
		"fleet-manager",
		fleetRepository,
		outboxEvent,
		outboxRuntime,
		serviceprocess.EventLogAppender(eventLogPool),
		logger,
		errCh,
	)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return serviceprocess.ShutdownHTTPAndGRPC(ctx, httpServer, grpcServer, 10*time.Second)
	case err := <-errCh:
		grpcServer.Stop()
		_ = httpServer.Close()
		return err
	}
}

func readinessChecks(fleet serviceprocess.PingStore, eventLog serviceprocess.PingStore) []serviceprocess.ReadinessCheck {
	return serviceprocess.ServiceDatabaseReadinessChecks("fleet service", true, "fleet database", fleet, eventLog)
}
