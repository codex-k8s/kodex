// Package app contains project-catalog process composition and lifecycle.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	projectgrpc "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/transport/grpc"
	grpcruntime "google.golang.org/grpc"
)

// Run starts project-catalog process servers and shuts them down with context.
func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           healthMux(),
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
		return shutdownServers(httpServer, grpcServer)
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

func shutdownServers(httpServer *http.Server, grpcServer *grpcruntime.Server) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	grpcServer.GracefulStop()
	return httpServer.Shutdown(shutdownCtx)
}

func healthMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/health/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}
