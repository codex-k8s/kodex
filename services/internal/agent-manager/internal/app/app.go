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
	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	packagehubclient "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/packagehub"
	projectcatalogclient "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/projectcatalog"
	providerhubclient "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/providerhub"
	runtimeclient "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/runtime"
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
	workspacePolicyResolver, projectCatalogConn, err := newWorkspacePolicyResolver(cfg)
	if err != nil {
		return err
	}
	if projectCatalogConn != nil {
		defer func() { _ = projectCatalogConn.Close() }()
	}
	runtimePreparer, runtimeManagerConn, err := newRuntimePreparer(cfg)
	if err != nil {
		return err
	}
	if runtimeManagerConn != nil {
		defer func() { _ = runtimeManagerConn.Close() }()
	}
	providerFollowUpDispatcher, providerHubConn, err := newProviderFollowUpDispatcher(cfg)
	if err != nil {
		return err
	}
	if providerHubConn != nil {
		defer func() { _ = providerHubConn.Close() }()
	}
	agentRepository := agentpostgres.NewRepository(dbPool)
	agentService := agentservice.New(agentservice.Config{
		Repository:                 agentRepository,
		Clock:                      systemClock{},
		IDGenerator:                uuidGenerator{},
		GuidanceResolver:           guidanceResolver,
		WorkspacePolicyResolver:    workspacePolicyResolver,
		RuntimePreparer:            runtimePreparer,
		ProviderFollowUpDispatcher: providerFollowUpDispatcher,
		RuntimePreparationEnabled:  cfg.RuntimePreparationEnabled,
		EventPublisher:             agentservice.DisabledEventPublisher{},
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

func newWorkspacePolicyResolver(cfg Config) (agentservice.WorkspacePolicyResolver, *grpcruntime.ClientConn, error) {
	if cfg.RuntimePreparationEnabled {
		return connectOwnerService[agentservice.WorkspacePolicyResolver](projectcatalogclient.Config{
			Addr:      cfg.ProjectCatalogGRPCAddr,
			AuthToken: cfg.ProjectCatalogGRPCAuthToken,
			Timeout:   cfg.ProjectCatalogReadTimeout,
		}, projectcatalogclient.NewConnection, func(conn *grpcruntime.ClientConn, clientConfig projectcatalogclient.Config) (agentservice.WorkspacePolicyResolver, error) {
			return projectcatalogclient.NewWorkspacePolicyResolver(projectsv1.NewProjectCatalogServiceClient(conn), clientConfig)
		})
	}
	return agentservice.DisabledWorkspacePolicyResolver{}, nil, nil
}

func newRuntimePreparer(cfg Config) (agentservice.RuntimePreparer, *grpcruntime.ClientConn, error) {
	if !cfg.RuntimePreparationEnabled {
		disabled := agentservice.DisabledRuntimePreparer{}
		return disabled, nil, nil
	}
	clientConfig := runtimeclient.Config{
		Addr:      cfg.RuntimeManagerGRPCAddr,
		AuthToken: cfg.RuntimeManagerGRPCAuthToken,
		Timeout:   cfg.RuntimeManagerPrepareTimeout,
	}
	return connectOwnerService[agentservice.RuntimePreparer](clientConfig, runtimeclient.NewConnection, func(conn *grpcruntime.ClientConn, clientConfig runtimeclient.Config) (agentservice.RuntimePreparer, error) {
		return runtimeclient.NewPreparer(runtimev1.NewRuntimeManagerServiceClient(conn), clientConfig)
	})
}

func newProviderFollowUpDispatcher(cfg Config) (agentservice.ProviderFollowUpDispatcher, *grpcruntime.ClientConn, error) {
	if !cfg.ProviderHubWriteEnabled {
		disabled := agentservice.DisabledProviderFollowUpDispatcher{}
		return disabled, nil, nil
	}
	return connectProviderFollowUpDispatcher(providerhubclient.Config{
		Addr:      cfg.ProviderHubGRPCAddr,
		AuthToken: cfg.ProviderHubGRPCAuthToken,
		Timeout:   cfg.ProviderHubWriteTimeout,
	})
}

func connectProviderFollowUpDispatcher(clientConfig providerhubclient.Config) (agentservice.ProviderFollowUpDispatcher, *grpcruntime.ClientConn, error) {
	return connectOwnerService[agentservice.ProviderFollowUpDispatcher](clientConfig, providerhubclient.NewConnection, func(conn *grpcruntime.ClientConn, clientConfig providerhubclient.Config) (agentservice.ProviderFollowUpDispatcher, error) {
		return providerhubclient.NewFollowUpDispatcher(providersv1.NewProviderHubServiceClient(conn), clientConfig)
	})
}

func connectOwnerService[T any, C any](
	clientConfig C,
	newConnection func(C) (*grpcruntime.ClientConn, error),
	newAdapter func(*grpcruntime.ClientConn, C) (T, error),
) (T, *grpcruntime.ClientConn, error) {
	var zero T
	conn, err := newConnection(clientConfig)
	if err != nil {
		return zero, nil, err
	}
	adapter, err := newAdapter(conn, clientConfig)
	if err != nil {
		_ = conn.Close()
		return zero, nil, err
	}
	return adapter, conn, nil
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
