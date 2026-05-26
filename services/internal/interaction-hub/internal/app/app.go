package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	interactionservice "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/service"
	interactionpostgres "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/repository/postgres/interaction"
	interactiongrpc "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/transport/grpc"
)

const serviceName = "interaction-hub"

// Run starts interaction-hub process servers and shuts them down with context.
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
	interactionRepository := interactionpostgres.NewRepository(dbPool)
	interactionService := interactionservice.New(interactionRepository)
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           serviceprocess.NewHealthMux(readinessChecks(interactionService, dbPool, eventLogPool), 2*time.Second),
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcMetrics, err := grpcserver.NewMetrics(nil, grpcserver.MetricsConfig{
		Subsystem:   "interaction_hub_grpc",
		ServiceName: serviceName,
	})
	if err != nil {
		return err
	}
	grpcServer, err := grpcserver.NewServer(cfg.GRPCServerConfig(), grpcserver.Dependencies{
		Logger:        logger,
		Metrics:       grpcMetrics,
		Authenticator: grpcserver.NewSharedTokenAuthenticator(cfg.GRPC.AuthToken),
		UnaryInterceptors: []grpcserver.UnaryInterceptor{
			interactiongrpc.UnaryErrorInterceptor(logger),
		},
	})
	if err != nil {
		return err
	}
	interactiongrpc.RegisterInteractionHubService(grpcServer, interactionService)

	errCh := make(chan error, 3)
	go serviceprocess.StartHTTPServer(httpServer, serviceName, logger, errCh)
	go serviceprocess.StartGRPCServer(grpcServer, serviceName, cfg.GRPCAddr, logger, errCh)
	err = serviceprocess.StartOutboxDispatcher(
		ctx,
		serviceName,
		interactionRepository,
		outboxEvent,
		serviceprocess.OutboxRuntimeConfig{
			DispatchEnabled:     cfg.OutboxDispatchEnabled,
			PublisherKind:       cfg.OutboxPublisherKind,
			AllowLossyPublisher: cfg.OutboxAllowLossy,
			EventLogSource:      cfg.OutboxEventLogSource,
			Dispatcher:          cfg.OutboxDispatcherConfig(),
		},
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

func readinessChecks(interactionService *interactionservice.Service, interactionDB serviceprocess.PingStore, eventLogDB serviceprocess.PingStore) []serviceprocess.ReadinessCheck {
	return serviceprocess.ServiceDatabaseReadinessChecks(
		"interaction service",
		interactionService != nil && interactionService.Ready(),
		"interaction database",
		interactionDB,
		eventLogDB,
	)
}
