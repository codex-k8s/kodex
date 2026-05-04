// Package app contains access-manager process composition and lifecycle.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	accessservice "github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/service"
	accesspostgres "github.com/codex-k8s/kodex/services/internal/access-manager/internal/repository/postgres/access"
	accessgrpc "github.com/codex-k8s/kodex/services/internal/access-manager/internal/transport/grpc"
)

// Run starts access-manager process servers and shuts them down with context.
func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	dbPool, err := postgreslib.OpenPool(ctx, cfg.DatabasePoolSettings())
	if err != nil {
		return err
	}
	defer dbPool.Close()

	accessRepository := accesspostgres.NewRepository(dbPool)
	components := processComponents{
		DBPool:        dbPool,
		AccessService: accessservice.New(accessRepository, systemClock{}, uuidGenerator{}),
		OutboxStore:   accessRepository,
	}
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           healthMux(components),
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcMetrics, err := grpcserver.NewMetrics(nil, grpcserver.MetricsConfig{
		Subsystem:   "access_manager_grpc",
		ServiceName: "access-manager",
	})
	if err != nil {
		return err
	}
	grpcServer, err := grpcserver.NewServer(cfg.GRPCServerConfig(), grpcserver.Dependencies{
		Logger:        logger,
		Metrics:       grpcMetrics,
		Authenticator: grpcserver.NewSharedTokenAuthenticator(cfg.GRPCAuthToken),
		UnaryInterceptors: []grpcserver.UnaryInterceptor{
			accessgrpc.UnaryErrorInterceptor(logger),
		},
	})
	if err != nil {
		return err
	}
	accessgrpc.RegisterAccessManagerService(grpcServer, components.AccessService)

	errCh := make(chan error, 3)
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
	if cfg.OutboxDispatchEnabled {
		publisher, err := newOutboxPublisher(cfg, logger)
		if err != nil {
			return err
		}
		dispatcher := newOutboxDispatcher(
			components.OutboxStore,
			publisher,
			cfg.OutboxDispatcherConfig(),
			logger,
		)
		go func() {
			logger.Info("access-manager outbox dispatcher starting")
			if err := dispatcher.Run(ctx); err != nil {
				errCh <- err
			}
		}()
	}

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

type processComponents struct {
	DBPool        *pgxpool.Pool
	AccessService *accessservice.Service
	OutboxStore   outboxStore
}

func healthMux(components processComponents) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/health/readyz", func(w http.ResponseWriter, r *http.Request) {
		if components.AccessService == nil {
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

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID {
	return uuid.New()
}
