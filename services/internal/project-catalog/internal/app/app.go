// Package app contains project-catalog process composition and lifecycle.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"github.com/google/uuid"
	grpcruntime "google.golang.org/grpc"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	accessclient "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/clients/access"
	providerhubclient "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/clients/providerhub"
	projectservice "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/service"
	projectpostgres "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/repository/postgres/project"
	projectgrpc "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/transport/grpc"
)

// Run starts project-catalog process servers and shuts them down with context.
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
	authorizer, accessConn, err := newAuthorizer(cfg)
	if err != nil {
		return err
	}
	if accessConn != nil {
		defer func() {
			_ = accessConn.Close()
		}()
	}
	bootstrapProvider, providerConn, err := newBootstrapProvider(cfg)
	if err != nil {
		return err
	}
	if providerConn != nil {
		defer func() {
			_ = providerConn.Close()
		}()
	}

	projectRepository := projectpostgres.NewRepository(dbPool)
	projectService := projectservice.NewWithConfig(projectRepository, systemClock{}, uuidGenerator{}, projectservice.Config{
		Authorizer:              authorizer,
		BootstrapProvider:       bootstrapProvider,
		RepositoryChangeSignals: bootstrapProvider,
	})
	components := processComponents{
		ProjectService: projectService,
	}
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           serviceprocess.NewHealthMux(readinessChecks(projectService, dbPool, eventLogPool), 2*time.Second),
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcMetrics, err := grpcserver.NewMetrics(nil, grpcserver.MetricsConfig{
		Subsystem:   "project_catalog_grpc",
		ServiceName: "project-catalog",
	})
	if err != nil {
		return err
	}
	grpcServer, err := grpcserver.NewServer(cfg.GRPCServerConfig(), grpcserver.Dependencies{
		Logger:        logger,
		Metrics:       grpcMetrics,
		Authenticator: grpcserver.NewSharedTokenAuthenticator(cfg.GRPCAuthToken),
		UnaryInterceptors: []grpcserver.UnaryInterceptor{
			projectgrpc.UnaryErrorInterceptor(logger),
		},
	})
	if err != nil {
		return err
	}
	projectgrpc.RegisterProjectCatalogService(grpcServer, components.ProjectService)

	errCh := make(chan error, 5)
	go serviceprocess.StartHTTPServer(httpServer, "project-catalog", logger, errCh)
	go serviceprocess.StartGRPCServer(grpcServer, "project-catalog", cfg.GRPCAddr, logger, errCh)
	err = serviceprocess.StartOutboxDispatcher(
		ctx,
		"project-catalog",
		projectRepository,
		outboxEvent,
		serviceprocess.OutboxRuntimeConfig{
			DispatchEnabled:     cfg.OutboxDispatchEnabled,
			PublisherKind:       cfg.OutboxPublisherKind,
			AllowLossyPublisher: cfg.OutboxAllowLossyPublisher,
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
	if err := startProviderBootstrapMergeConsumer(ctx, cfg, eventLogPool, projectService, logger, errCh); err != nil {
		return err
	}
	if err := startProviderAdoptionMergeConsumer(ctx, cfg, eventLogPool, projectService, logger, errCh); err != nil {
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

type processComponents struct {
	ProjectService *projectservice.Service
}

func newAuthorizer(cfg Config) (projectservice.Authorizer, *grpcruntime.ClientConn, error) {
	if !cfg.AccessCheckEnabled {
		return projectservice.AllowAllAuthorizer{}, nil, nil
	}
	accessConfig := accessclient.Config{
		Addr:      cfg.AccessManagerGRPCAddr,
		AuthToken: cfg.AccessManagerGRPCAuthToken,
		Timeout:   cfg.AccessManagerCheckTimeout,
	}
	return accessclient.NewConnectedAuthorizer(accessConfig)
}

func newBootstrapProvider(cfg Config) (*providerhubclient.Bootstrapper, *grpcruntime.ClientConn, error) {
	if !cfg.ProviderHubBootstrapEnabled {
		return nil, nil, nil
	}
	providerConfig := providerhubclient.Config{
		Addr:      cfg.ProviderHubGRPCAddr,
		AuthToken: cfg.ProviderHubGRPCAuthToken,
		Timeout:   cfg.ProviderHubRequestTimeout,
	}
	conn, err := providerhubclient.NewConnection(providerConfig)
	if err != nil {
		return nil, nil, err
	}
	bootstrapper, err := providerhubclient.NewBootstrapper(providersv1.NewProviderHubServiceClient(conn), providerConfig)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return bootstrapper, conn, nil
}

func readinessChecks(projectService *projectservice.Service, projectDB serviceprocess.PingStore, eventLogDB serviceprocess.PingStore) []serviceprocess.ReadinessCheck {
	return serviceprocess.ServiceDatabaseReadinessChecks(
		"project service",
		projectService != nil,
		"project database",
		projectDB,
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
