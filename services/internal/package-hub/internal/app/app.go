// Package app contains package-hub process composition and lifecycle.
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
	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	accessclient "github.com/codex-k8s/kodex/services/internal/package-hub/internal/clients/access"
	packageservice "github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/service"
	packagepostgres "github.com/codex-k8s/kodex/services/internal/package-hub/internal/repository/postgres/catalog"
	packagegrpc "github.com/codex-k8s/kodex/services/internal/package-hub/internal/transport/grpc"
)

const serviceName = "package-hub"

// Run starts package-hub process servers and shuts them down with context.
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
	accessDependencies, accessConn, err := newAccessDependencies(cfg)
	if err != nil {
		return err
	}
	if accessConn != nil {
		defer func() { _ = accessConn.Close() }()
	}
	secretChecker, err := newSecretChecker(cfg)
	if err != nil {
		return err
	}
	packageRepository := packagepostgres.NewRepository(dbPool)
	packageService := packageservice.NewWithConfig(packageRepository, systemClock{}, uuidGenerator{}, packageservice.Config{
		Authorizer:      accessDependencies.Authorizer,
		SecretRefReader: accessDependencies.SecretRefReader,
		SecretChecker:   secretChecker,
	})
	components := processComponents{PackageService: packageService}
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           serviceprocess.NewHealthMux(readinessChecks(packageService, dbPool, eventLogPool), 2*time.Second),
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcMetrics, err := grpcserver.NewMetrics(nil, grpcserver.MetricsConfig{
		Subsystem:   "package_hub_grpc",
		ServiceName: "package-hub",
	})
	if err != nil {
		return err
	}
	grpcServer, err := grpcserver.NewServer(cfg.GRPCServerConfig(), grpcserver.Dependencies{
		Logger:        logger,
		Metrics:       grpcMetrics,
		Authenticator: grpcserver.NewSharedTokenAuthenticator(cfg.GRPCAuthToken),
		UnaryInterceptors: []grpcserver.UnaryInterceptor{
			packagegrpc.UnaryErrorInterceptor(logger),
		},
	})
	if err != nil {
		return err
	}
	packagegrpc.RegisterPackageHubService(grpcServer, components.PackageService)

	errCh := make(chan error, 3)
	go serviceprocess.StartHTTPServer(httpServer, serviceName, logger, errCh)
	go serviceprocess.StartGRPCServer(grpcServer, serviceName, cfg.GRPCAddr, logger, errCh)
	err = serviceprocess.StartOutboxDispatcher(
		ctx,
		serviceName,
		packageRepository,
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

type processComponents struct {
	PackageService *packageservice.Service
}

type accessDependencies struct {
	Authorizer      packageservice.Authorizer
	SecretRefReader packageservice.SecretRefReader
}

func newAccessDependencies(cfg Config) (accessDependencies, *grpcruntime.ClientConn, error) {
	if cfg.AccessCheckEnabled {
		return connectAccessDependencies(accessclient.Config{
			Addr:      cfg.AccessManagerGRPCAddr,
			AuthToken: cfg.AccessManagerGRPCAuthToken,
			Timeout:   cfg.AccessManagerCheckTimeout,
		})
	}
	return accessDependencies{Authorizer: packageservice.AllowAllAuthorizer{}}, nil, nil
}

func connectAccessDependencies(accessConfig accessclient.Config) (accessDependencies, *grpcruntime.ClientConn, error) {
	conn, err := accessclient.NewConnection(accessConfig)
	if err != nil {
		return accessDependencies{}, nil, err
	}
	client := accessaccountsv1.NewAccessManagerServiceClient(conn)
	authorizer, err := accessclient.NewAuthorizer(client, accessConfig)
	if err != nil {
		_ = conn.Close()
		return accessDependencies{}, nil, err
	}
	secretRefs, err := accessclient.NewPackageSecretRefReader(client, accessConfig)
	if err != nil {
		_ = conn.Close()
		return accessDependencies{}, nil, err
	}
	return accessDependencies{Authorizer: authorizer, SecretRefReader: secretRefs}, conn, nil
}

func readinessChecks(packageService *packageservice.Service, packageDB serviceprocess.PingStore, eventLogDB serviceprocess.PingStore) []serviceprocess.ReadinessCheck {
	return serviceprocess.ServiceDatabaseReadinessChecks(
		"package service",
		packageService != nil,
		"package database",
		packageDB,
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
