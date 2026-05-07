package serviceprocess

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	grpcruntime "google.golang.org/grpc"
)

// StartHTTPServer starts an HTTP server and reports unexpected serve errors.
func StartHTTPServer(server *http.Server, serviceName string, logger *slog.Logger, errCh chan<- error) {
	logger.Info(serviceName+" http server starting", "addr", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errCh <- fmt.Errorf("serve %s http: %w", serviceName, err)
	}
}

// StartGRPCServer starts a gRPC server on addr and reports unexpected serve errors.
func StartGRPCServer(server *grpcruntime.Server, serviceName string, addr string, logger *slog.Logger, errCh chan<- error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		errCh <- fmt.Errorf("listen %s grpc: %w", serviceName, err)
		return
	}
	logger.Info(serviceName+" grpc server starting", "addr", addr)
	if err := server.Serve(listener); err != nil {
		errCh <- fmt.Errorf("serve %s grpc: %w", serviceName, err)
	}
}

// ShutdownHTTPAndGRPC gracefully stops gRPC and HTTP servers.
func ShutdownHTTPAndGRPC(ctx context.Context, httpServer *http.Server, grpcServer *grpcruntime.Server, timeout time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	grpcServer.GracefulStop()
	return httpServer.Shutdown(shutdownCtx)
}
