// Package app contains package-hub process composition and lifecycle.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	grpcruntime "google.golang.org/grpc"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
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
		Handler:           healthMux(components),
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

	servers := processServers{
		httpServer: httpServer,
		grpcServer: grpcServer,
		grpcAddr:   cfg.GRPCAddr,
		logger:     logger,
	}
	errCh := make(chan error, 2)
	go servers.runHTTP(errCh)
	go servers.runGRPC(errCh)

	select {
	case <-ctx.Done():
		return servers.shutdown(ctx)
	case err := <-errCh:
		grpcServer.Stop()
		_ = httpServer.Close()
		return err
	}
}

type processServers struct {
	httpServer *http.Server
	grpcServer *grpcruntime.Server
	grpcAddr   string
	logger     *slog.Logger
}

func (servers processServers) runHTTP(errCh chan<- error) {
	servers.logger.Info(serviceName+" http server starting", "addr", servers.httpServer.Addr)
	err := servers.httpServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		errCh <- err
	}
}

func (servers processServers) runGRPC(errCh chan<- error) {
	listener, err := net.Listen("tcp", servers.grpcAddr)
	if err != nil {
		errCh <- err
		return
	}
	servers.logger.Info(serviceName+" grpc server starting", "addr", servers.grpcAddr)
	if err := servers.grpcServer.Serve(listener); err != nil {
		errCh <- err
	}
}

func (servers processServers) shutdown(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	servers.grpcServer.GracefulStop()
	return servers.httpServer.Shutdown(shutdownCtx)
}

type processComponents struct {
	PackageService *packageservice.Service
}

func healthMux(components processComponents) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/health/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if components.PackageService == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}
