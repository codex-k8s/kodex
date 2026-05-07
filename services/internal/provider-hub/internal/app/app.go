// Package app contains provider-hub process composition and lifecycle.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	providerservice "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/service"
	providerpostgres "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/repository/postgres/provider"
	providergrpc "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/transport/grpc"
)

// Run starts provider-hub process servers and shuts them down with context.
func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	dbPool, err := postgreslib.OpenPool(ctx, cfg.DatabasePoolSettings())
	if err != nil {
		return err
	}
	defer dbPool.Close()

	providerRepository := providerpostgres.NewRepository(dbPool)
	components := processComponents{
		ProviderService: providerservice.New(providerRepository),
	}
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           serviceprocess.NewHealthMux(readinessChecks(components.ProviderService), 2*time.Second),
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcMetrics, err := grpcserver.NewMetrics(nil, grpcserver.MetricsConfig{
		Subsystem:   "provider_hub_grpc",
		ServiceName: "provider-hub",
	})
	if err != nil {
		return err
	}
	grpcServer, err := grpcserver.NewServer(cfg.GRPCServerConfig(), grpcserver.Dependencies{
		Logger:        logger,
		Metrics:       grpcMetrics,
		Authenticator: grpcserver.NewSharedTokenAuthenticator(cfg.GRPCAuthToken),
		UnaryInterceptors: []grpcserver.UnaryInterceptor{
			providergrpc.UnaryErrorInterceptor(logger),
		},
	})
	if err != nil {
		return err
	}
	providergrpc.RegisterProviderHubService(grpcServer, components.ProviderService)

	errCh := make(chan error, 2)
	go serviceprocess.StartHTTPServer(httpServer, "provider-hub", logger, errCh)
	go serviceprocess.StartGRPCServer(grpcServer, "provider-hub", cfg.GRPCAddr, logger, errCh)

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
	ProviderService *providerservice.Service
}

func readinessChecks(providerService *providerservice.Service) []serviceprocess.ReadinessCheck {
	checks := []serviceprocess.ReadinessCheck{
		serviceprocess.StaticReadinessCheck("provider service", providerService != nil),
	}
	if providerService != nil {
		checks = append(checks, serviceprocess.ReadinessCheck{Name: "provider database", Check: providerService.Ping})
	}
	return checks
}
