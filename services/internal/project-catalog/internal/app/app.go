// Package app contains project-catalog process composition and lifecycle.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	projectgrpc "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/transport/grpc"
	grpcruntime "google.golang.org/grpc"
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
	components := processComponents{DBPool: dbPool}
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           healthMux(components),
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
	})
	if err != nil {
		return err
	}
	projectgrpc.RegisterProjectCatalogService(grpcServer)

	errCh := make(chan error, 2)
	go serveHTTP(httpServer, cfg.HTTPAddr, logger, errCh)
	go serveGRPC(grpcServer, cfg.GRPCAddr, logger, errCh)

	select {
	case <-ctx.Done():
		return shutdownServers(ctx, httpServer, grpcServer)
	case err := <-errCh:
		grpcServer.Stop()
		_ = httpServer.Close()
		return err
	}
}

func serveHTTP(server *http.Server, addr string, logger *slog.Logger, errCh chan<- error) {
	logger.Info("project-catalog http server starting", "addr", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errCh <- err
	}
}

func serveGRPC(server *grpcruntime.Server, addr string, logger *slog.Logger, errCh chan<- error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		errCh <- err
		return
	}
	logger.Info("project-catalog grpc server starting", "addr", addr)
	if err := server.Serve(listener); err != nil {
		errCh <- err
	}
}

func shutdownServers(ctx context.Context, httpServer *http.Server, grpcServer *grpcruntime.Server) error {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	grpcServer.GracefulStop()
	return httpServer.Shutdown(shutdownCtx)
}

type processComponents struct {
	DBPool *pgxpool.Pool
}

func healthMux(components processComponents) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/health/readyz", func(w http.ResponseWriter, r *http.Request) {
		if components.DBPool == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		readyCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := components.DBPool.Ping(readyCtx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}
