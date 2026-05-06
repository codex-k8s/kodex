// Package app contains project-catalog process composition and lifecycle.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	grpcruntime "google.golang.org/grpc"

	eventlog "github.com/codex-k8s/kodex/libs/go/eventlog"
	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	accessclient "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/clients/access"
	projectservice "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/service"
	projectpostgres "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/repository/postgres/project"
	projectgrpc "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/transport/grpc"
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
	eventLogPool, err := openOutboxEventLogPool(ctx, cfg)
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

	projectRepository := projectpostgres.NewRepository(dbPool)
	projectService := projectservice.NewWithConfig(projectRepository, systemClock{}, uuidGenerator{}, projectservice.Config{Authorizer: authorizer})
	components := processComponents{
		DBPool:         dbPool,
		EventLogDBPool: eventLogPool,
		ProjectService: projectService,
		OutboxStore:    projectRepository,
	}
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
		UnaryInterceptors: []grpcserver.UnaryInterceptor{
			projectgrpc.UnaryErrorInterceptor(logger),
		},
	})
	if err != nil {
		return err
	}
	projectgrpc.RegisterProjectCatalogService(grpcServer, components.ProjectService)

	errCh := make(chan error, 3)
	go serveHTTP(httpServer, cfg.HTTPAddr, logger, errCh)
	go serveGRPC(grpcServer, cfg.GRPCAddr, logger, errCh)
	if cfg.OutboxDispatchEnabled {
		publisher, err := newOutboxPublisher(cfg, outboxEventLogAppender(eventLogPool), logger)
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
			logger.Info("project-catalog outbox dispatcher starting")
			if err := dispatcher.Run(ctx); err != nil {
				errCh <- err
			}
		}()
	}

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
	DBPool         *pgxpool.Pool
	EventLogDBPool *pgxpool.Pool
	ProjectService *projectservice.Service
	OutboxStore    outboxStore
}

func openOutboxEventLogPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	if !cfg.needsEventLogDatabase() {
		return nil, nil
	}
	pool, err := postgreslib.OpenPool(ctx, cfg.EventLogDatabasePoolSettings())
	if err != nil {
		return nil, fmt.Errorf("open platform event log database pool: %w", err)
	}
	return pool, nil
}

func outboxEventLogAppender(pool *pgxpool.Pool) eventLogAppender {
	if pool == nil {
		return nil
	}
	return eventlog.NewStore(pool)
}

func newAuthorizer(cfg Config) (projectservice.Authorizer, *grpcruntime.ClientConn, error) {
	if !cfg.AccessCheckEnabled {
		return projectservice.AllowAllAuthorizer{}, nil, nil
	}
	accessConfig := accessclient.Config{
		Addr:      cfg.AccessManagerGRPCAddr,
		AuthToken: cfg.AccessManagerGRPCAuthToken,
		Timeout:   cfg.AccessManagerCheckTimeout,
	}
	conn, err := accessclient.NewConnection(accessConfig)
	if err != nil {
		return nil, nil, err
	}
	authorizer, err := accessclient.NewAuthorizer(accessaccountsv1.NewAccessManagerServiceClient(conn), accessConfig)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return authorizer, conn, nil
}

func healthMux(components processComponents) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/health/readyz", func(w http.ResponseWriter, r *http.Request) {
		if components.ProjectService == nil || components.DBPool == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		readyCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := components.DBPool.Ping(readyCtx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if components.EventLogDBPool != nil {
			if err := components.EventLogDBPool.Ping(readyCtx); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
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
