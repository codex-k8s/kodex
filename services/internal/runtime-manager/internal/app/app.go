// Package app contains runtime-manager process composition and lifecycle.
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
	fleetv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/fleet/v1"
	accessclient "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/clients/access"
	fleetclient "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/clients/fleet"
	runtimeservice "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/service"
	runtimepostgres "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/repository/postgres/runtime"
	runtimegrpc "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/transport/grpc"
	grpcruntime "google.golang.org/grpc"
)

// Run starts runtime-manager process servers and shuts them down with context.
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
	placementResolver, fleetConn, err := newPlacementResolver(cfg)
	if err != nil {
		return err
	}
	defer func() {
		_ = fleetConn.Close()
	}()

	runtimeRepository := runtimepostgres.NewRepository(dbPool)
	slotConfig, err := cfg.SlotServiceConfig()
	if err != nil {
		return err
	}
	slotConfig.Authorizer = authorizer
	slotConfig.PlacementResolver = placementResolver
	runtimeService := runtimeservice.NewWithConfig(runtimeRepository, systemClock{}, uuidGenerator{}, slotConfig)
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           serviceprocess.NewHealthMux(readinessChecks(runtimeRepository, eventLogPool), 2*time.Second),
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcMetrics, err := grpcserver.NewMetrics(nil, grpcserver.MetricsConfig{
		Subsystem:   "runtime_manager_grpc",
		ServiceName: "runtime-manager",
	})
	if err != nil {
		return err
	}
	grpcServer, err := grpcserver.NewServer(cfg.GRPCServerConfig(), grpcserver.Dependencies{
		Logger:        logger,
		Metrics:       grpcMetrics,
		Authenticator: grpcserver.NewSharedTokenAuthenticator(cfg.GRPC.AuthToken),
		UnaryInterceptors: []grpcserver.UnaryInterceptor{
			runtimegrpc.UnaryErrorInterceptor(logger),
		},
	})
	if err != nil {
		return err
	}
	runtimegrpc.RegisterRuntimeManagerService(grpcServer, runtimeService)

	errCh := make(chan error, 3)
	go serviceprocess.StartHTTPServer(httpServer, "runtime-manager", logger, errCh)
	go serviceprocess.StartGRPCServer(grpcServer, "runtime-manager", cfg.GRPCAddr, logger, errCh)
	err = serviceprocess.StartOutboxDispatcher(
		ctx,
		"runtime-manager",
		runtimeRepository,
		outboxEvent,
		serviceprocess.OutboxRuntimeConfig{
			DispatchEnabled:     cfg.Outbox.DispatchEnabled,
			PublisherKind:       cfg.Outbox.PublisherKind,
			AllowLossyPublisher: cfg.Outbox.AllowLossyPublisher,
			EventLogSource:      cfg.Outbox.EventLogSource,
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

type runtimeStore interface {
	Ping(context.Context) error
}

type pingStore interface {
	Ping(context.Context) error
}

func newAuthorizer(cfg Config) (runtimeservice.Authorizer, *grpcruntime.ClientConn, error) {
	if !cfg.Access.CheckEnabled {
		return runtimeservice.AllowAllAuthorizer{}, nil, nil
	}
	accessConfig := accessclient.Config{
		Addr:      cfg.Access.AccessManagerGRPCAddr,
		AuthToken: cfg.Access.AccessManagerAuthToken,
		Timeout:   cfg.Access.CheckTimeout,
	}
	return accessclient.NewConnectedAuthorizer(accessConfig)
}

func newPlacementResolver(cfg Config) (runtimeservice.PlacementResolver, *grpcruntime.ClientConn, error) {
	fleetConfig := fleetclient.Config{
		Addr:      cfg.Fleet.FleetManagerGRPCAddr,
		AuthToken: cfg.Fleet.FleetManagerAuthToken,
		Timeout:   cfg.Fleet.ResolveTimeout,
	}
	conn, err := fleetclient.NewConnection(fleetConfig)
	if err != nil {
		return nil, nil, err
	}
	resolver, err := fleetclient.NewPlacementResolver(fleetv1.NewFleetManagerServiceClient(conn), fleetConfig)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return resolver, conn, nil
}

func readinessChecks(runtime runtimeStore, eventLog pingStore) []serviceprocess.ReadinessCheck {
	checks := []serviceprocess.ReadinessCheck{
		{Name: "runtime database", Check: runtime.Ping},
	}
	if eventLog != nil {
		checks = append(checks, serviceprocess.ReadinessCheck{Name: "event log database", Check: eventLog.Ping})
	}
	return checks
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID {
	return uuid.New()
}
