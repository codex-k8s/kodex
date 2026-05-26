package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	governanceservice "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/service"
	governancepostgres "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/repository/postgres/governance"
	governancegrpc "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/transport/grpc"
)

const serviceName = "governance-manager"

// Run starts governance-manager process servers and shuts them down with context.
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

	governanceRepository := governancepostgres.NewRepository(dbPool)
	governanceService := governanceservice.NewWithConfig(governanceservice.Config{
		Repository:  governanceRepository,
		Clock:       systemClock{},
		IDGenerator: uuidGenerator{},
	})
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           serviceprocess.NewHealthMux(readinessChecks(governanceService, dbPool, eventLogPool), 2*time.Second),
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcMetrics, err := grpcserver.NewMetrics(nil, grpcserver.MetricsConfig{
		Subsystem:   "governance_manager_grpc",
		ServiceName: serviceName,
	})
	if err != nil {
		return err
	}
	grpcServer, err := grpcserver.NewServer(cfg.GRPCServerConfig(), grpcserver.Dependencies{
		Logger:        logger,
		Metrics:       grpcMetrics,
		Authenticator: grpcserver.NewSharedTokenAuthenticator(cfg.GRPCAuthToken),
		UnaryInterceptors: []grpcserver.UnaryInterceptor{
			governancegrpc.UnaryErrorInterceptor(logger),
		},
	})
	if err != nil {
		return err
	}
	governancegrpc.RegisterGovernanceManagerService(grpcServer, governanceService)

	errCh := make(chan error, 3)
	go serviceprocess.StartHTTPServer(httpServer, serviceName, logger, errCh)
	go serviceprocess.StartGRPCServer(grpcServer, serviceName, cfg.GRPCAddr, logger, errCh)
	err = serviceprocess.StartOutboxDispatcher(
		ctx,
		serviceName,
		governanceRepository,
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

type readyService interface {
	Ready() bool
}

func readinessChecks(governanceService readyService, governanceDB serviceprocess.PingStore, eventLogDB serviceprocess.PingStore) []serviceprocess.ReadinessCheck {
	return serviceprocess.ServiceDatabaseReadinessChecks(
		"governance service",
		governanceService != nil && governanceService.Ready(),
		"governance database",
		governanceDB,
		eventLogDB,
	)
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID {
	return uuid.New()
}
