// Package app contains package-hub process composition and lifecycle.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	packageservice "github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/service"
	packagegrpc "github.com/codex-k8s/kodex/services/internal/package-hub/internal/transport/grpc"
)

const serviceName = "package-hub"

// Run starts package-hub process servers and shuts them down with context.
func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	packageService := packageservice.New()
	components := processComponents{
		PackageService: packageService,
	}
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           serviceprocess.NewHealthMux(readinessChecks(components.PackageService), 2*time.Second),
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
	})
	if err != nil {
		return err
	}
	packagegrpc.RegisterPackageHubService(grpcServer, components.PackageService)

	errCh := make(chan error, 2)
	go serviceprocess.StartHTTPServer(httpServer, serviceName, logger, errCh)
	go serviceprocess.StartGRPCServer(grpcServer, serviceName, cfg.GRPCAddr, logger, errCh)

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

func readinessChecks(packageService *packageservice.Service) []serviceprocess.ReadinessCheck {
	return []serviceprocess.ReadinessCheck{
		serviceprocess.StaticReadinessCheck("package service", packageService != nil),
	}
}
