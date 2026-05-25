package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	grpcruntime "google.golang.org/grpc"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	packagesv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/packages/v1"
	packagehubclient "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/packagehub"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	agentpostgres "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/repository/postgres/agent"
	agentgrpc "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/transport/grpc"
)

const serviceName = "agent-manager"

// Run starts agent-manager process servers and shuts them down with context.
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
	guidanceResolver, packageHubConn, err := newGuidanceResolver(cfg)
	if err != nil {
		return err
	}
	if packageHubConn != nil {
		defer func() { _ = packageHubConn.Close() }()
	}
	agentRepository := agentpostgres.NewRepository(dbPool)
	agentService := agentservice.New(agentservice.Config{
		Repository:       agentRepository,
		Clock:            systemClock{},
		IDGenerator:      uuidGenerator{},
		GuidanceResolver: guidanceResolver,
		EventPublisher:   agentservice.DisabledEventPublisher{},
	})
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           serviceprocess.NewHealthMux(readinessChecks(agentService, dbPool, eventLogPool), 2*time.Second),
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcMetrics, err := grpcserver.NewMetrics(nil, grpcserver.MetricsConfig{
		Subsystem:   "agent_manager_grpc",
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
			agentgrpc.UnaryErrorInterceptor(logger),
		},
	})
	if err != nil {
		return err
	}
	agentgrpc.RegisterAgentManagerService(grpcServer, agentService)

	errCh := make(chan error, 3)
	go serviceprocess.StartHTTPServer(httpServer, serviceName, logger, errCh)
	go serviceprocess.StartGRPCServer(grpcServer, serviceName, cfg.GRPCAddr, logger, errCh)
	err = serviceprocess.StartOutboxDispatcher(
		ctx,
		serviceName,
		agentRepository,
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

func newGuidanceResolver(cfg Config) (agentservice.GuidanceResolver, *grpcruntime.ClientConn, error) {
	if !cfg.PackageHubEnabled {
		return agentservice.DisabledGuidanceResolver{}, nil, nil
	}
	return connectPackageHubGuidance(packagehubclient.Config{
		Addr:      cfg.PackageHubGRPCAddr,
		AuthToken: cfg.PackageHubGRPCAuthToken,
		Timeout:   cfg.PackageHubReadTimeout,
	})
}

func connectPackageHubGuidance(clientConfig packagehubclient.Config) (agentservice.GuidanceResolver, *grpcruntime.ClientConn, error) {
	conn, err := packagehubclient.NewConnection(clientConfig)
	if err != nil {
		return nil, nil, err
	}
	resolver, err := packagehubclient.NewGuidanceResolver(packagesv1.NewPackageHubServiceClient(conn), clientConfig)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return resolver, conn, nil
}

func readinessChecks(agentService *agentservice.Service, agentDB serviceprocess.PingStore, eventLogDB serviceprocess.PingStore) []serviceprocess.ReadinessCheck {
	return serviceprocess.ServiceDatabaseReadinessChecks(
		"agent service",
		agentService != nil && agentService.Ready(),
		"agent database",
		agentDB,
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
