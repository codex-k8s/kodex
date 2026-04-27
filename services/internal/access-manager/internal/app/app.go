package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
)

func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           healthMux(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcServer := grpc.NewServer()

	errCh := make(chan error, 2)
	go func() {
		logger.Info("access-manager http server starting", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	go func() {
		listener, err := net.Listen("tcp", cfg.GRPCAddr)
		if err != nil {
			errCh <- err
			return
		}
		logger.Info("access-manager grpc server starting", "addr", cfg.GRPCAddr)
		if err := grpcServer.Serve(listener); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		grpcServer.GracefulStop()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		grpcServer.Stop()
		_ = httpServer.Close()
		return err
	}
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
