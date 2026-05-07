// Package app contains provider-hub process composition and lifecycle.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
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
		DBPool:          dbPool,
		ProviderService: providerservice.New(providerRepository),
	}
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           healthMux(components),
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
	go func() {
		logger.Info("provider-hub http server starting", "addr", cfg.HTTPAddr)
		if serveErr := httpServer.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- fmt.Errorf("serve provider-hub http: %w", serveErr)
		}
	}()
	go func() {
		listener, listenErr := net.Listen("tcp", cfg.GRPCAddr)
		if listenErr != nil {
			errCh <- fmt.Errorf("listen provider-hub grpc: %w", listenErr)
			return
		}
		logger.Info("provider-hub grpc server starting", "addr", cfg.GRPCAddr)
		if serveErr := grpcServer.Serve(listener); serveErr != nil {
			errCh <- fmt.Errorf("serve provider-hub grpc: %w", serveErr)
		}
	}()

	if err := waitUntilStopped(ctx, errCh); err != nil {
		grpcServer.Stop()
		_ = httpServer.Close()
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	grpcServer.GracefulStop()
	return httpServer.Shutdown(shutdownCtx)
}

func waitUntilStopped(ctx context.Context, errCh <-chan error) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

type processComponents struct {
	DBPool          *pgxpool.Pool
	ProviderService *providerservice.Service
}

func healthMux(components processComponents) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/health/readyz", func(w http.ResponseWriter, r *http.Request) {
		if components.ProviderService == nil || components.DBPool == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		readyCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := components.ProviderService.Ping(readyCtx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}
