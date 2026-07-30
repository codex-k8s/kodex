package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/grpcserver"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/udscred"
	"github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/application"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
	authorityrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/postgres/authority"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/snapshot"
	authoritygrpc "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/transport/grpc"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

const (
	maxDSNFileBytes = 16 << 10
	logMessageStart = "internal-rpc-authority runtime started"
	logMessageStop  = "internal-rpc-authority runtime stopped"
)

func Run(
	lifecycle context.Context,
	shutdownBase context.Context,
	mode Mode,
	buildVersion string,
) error {
	config, err := LoadConfig(mode)
	if err != nil {
		return fmt.Errorf("load runtime configuration: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{}))
	startupCtx, startupCancel := context.WithTimeout(lifecycle, config.StartupTimeout)
	defer startupCancel()
	pool, err := openPostgres(startupCtx, config)
	if err != nil {
		return err
	}
	store, err := authorityrepository.New(pool, config.WorkloadID)
	if err != nil {
		pool.Close()
		return fmt.Errorf("construct authority repository: %w", err)
	}
	loaded, err := snapshot.Load(snapshot.LoadOptions{
		Role:                   snapshot.Role(config.Mode),
		WorkloadID:             config.WorkloadID,
		SnapshotJWSFile:        config.SnapshotJWSFile,
		ManifestPublicJWKFile:  config.ManifestPublicJWKFile,
		ContextPrivateJWKFile:  config.ContextPrivateJWKFile,
		ProofTrustJWKFile:      config.ProofTrustJWKFile,
		ReadbackPrivateJWKFile: config.ReadbackPrivateJWKFile,
		Now:                    time.Now(),
	})
	if err != nil {
		store.Close()
		return fmt.Errorf("load served authority snapshot: %w", err)
	}
	domainService, err := service.NewAuthority(loaded.Policy, loaded.Keys, store)
	if err != nil {
		store.Close()
		return fmt.Errorf("construct authority domain service: %w", err)
	}
	authorityApplication := application.NewAuthority(domainService)
	if err := authorityApplication.ActivateSnapshot(startupCtx); err != nil {
		store.Close()
		return fmt.Errorf("activate served authority snapshot: %w", err)
	}
	metrics := observability.NewMetrics(
		config.ServiceName,
		buildVersion,
		allowedMethods(mode),
	)
	readiness := serviceruntime.NewReadiness()
	readiness.Set(false, "starting")
	metrics.SetReady(false)
	errorObserver := grpcserver.ErrorObserverFunc(func(
		_ context.Context,
		method string,
		code codes.Code,
		_ error,
	) {
		logger.Error(
			"unexpected gRPC failure",
			"method", normalizedMethod(mode, method),
			"code", code.String(),
		)
	})
	grpcRuntime := grpc.NewServer(
		grpc.Creds(udscred.New(config.ExpectedPeerUID, config.ExpectedPeerGID)),
		grpc.ChainUnaryInterceptor(
			metrics.UnaryServerInterceptor(),
			grpcserver.ErrorBoundary(errorObserver),
		),
	)
	switch mode {
	case ModeIssuer:
		internalrpcauthorityv1.RegisterAuthorizationIssuerServiceServer(
			grpcRuntime,
			authoritygrpc.NewIssuerServer(authorityApplication),
		)
	case ModeVerifier:
		internalrpcauthorityv1.RegisterAuthorizationVerifierServiceServer(
			grpcRuntime,
			authoritygrpc.NewVerifierServer(authorityApplication),
		)
	}
	unixListener, err := listenUnix(config)
	if err != nil {
		store.Close()
		return err
	}
	technicalListener, err := net.Listen("tcp", config.TechnicalListen)
	if err != nil {
		_ = unixListener.Close()
		store.Close()
		return fmt.Errorf("listen on technical HTTP endpoint: %w", err)
	}
	technicalServer := newTechnicalServer(
		config,
		readiness,
		metrics,
		authorityApplication,
	)
	runCtx, cancel := context.WithCancel(lifecycle)
	defer cancel()
	serveErrors := make(chan error, 2)
	go func() {
		if serveErr := grpcRuntime.Serve(unixListener); serveErr != nil {
			serveErrors <- fmt.Errorf("serve authority gRPC: %w", serveErr)
		}
	}()
	go func() {
		if serveErr := technicalServer.Serve(technicalListener); serveErr != nil &&
			!errors.Is(serveErr, http.ErrServerClosed) {
			serveErrors <- fmt.Errorf("serve technical HTTP: %w", serveErr)
		}
	}()
	workers := serviceruntime.StartWorkers(runCtx, func(workerCtx context.Context) error {
		return runReplayCleanup(workerCtx, config, store)
	})
	readiness.Set(true, "ready")
	metrics.SetReady(true)
	logger.Info(logMessageStart, "mode", string(mode), "workload", config.WorkloadID)
	var runtimeErr error
	select {
	case <-lifecycle.Done():
	case runtimeErr = <-serveErrors:
		cancel()
	}
	readiness.Set(false, "shutting-down")
	metrics.SetReady(false)
	shutdownErr := serviceruntime.RunShutdown(
		shutdownBase,
		serviceruntime.ShutdownOperation{
			Name:    "grpc-server",
			Timeout: config.ShutdownTimeout,
			Run: func(ctx context.Context) error {
				return stopGRPC(ctx, grpcRuntime)
			},
		},
		serviceruntime.ShutdownOperation{
			Name:    "workers",
			Timeout: config.ShutdownTimeout,
			Run: func(ctx context.Context) error {
				workers.Stop()
				return workers.Wait(ctx)
			},
		},
		serviceruntime.ShutdownOperation{
			Name:    "technical-http",
			Timeout: config.ShutdownTimeout,
			Run:     technicalServer.Shutdown,
		},
		serviceruntime.ShutdownOperation{
			Name:    "postgresql",
			Timeout: config.ShutdownTimeout,
			Run: func(context.Context) error {
				store.Close()
				return nil
			},
		},
	)
	logger.Info(logMessageStop, "mode", string(mode))
	return errors.Join(runtimeErr, shutdownErr)
}

func openPostgres(ctx context.Context, config Config) (*pgxpool.Pool, error) {
	raw, err := readPrivateFile(config.PostgresDSNFile, maxDSNFileBytes)
	if err != nil {
		return nil, fmt.Errorf("read PostgreSQL DSN file: %w", err)
	}
	poolConfig, err := pgxpool.ParseConfig(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, errors.New("parse PostgreSQL DSN")
	}
	if poolConfig.ConnConfig.Host != config.PostgresTLSServerName ||
		poolConfig.ConnConfig.TLSConfig == nil ||
		poolConfig.ConnConfig.TLSConfig.ServerName != config.PostgresTLSServerName ||
		poolConfig.ConnConfig.TLSConfig.InsecureSkipVerify {
		return nil, errors.New("PostgreSQL DSN must use verify-full TLS with exact server name")
	}
	poolConfig.MaxConns = config.PostgresMaxConnections
	poolConfig.ConnConfig.RuntimeParams["application_name"] = config.ServiceName
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, errors.New("open PostgreSQL pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("verify PostgreSQL connectivity")
	}
	return pool, nil
}

func newTechnicalServer(
	config Config,
	readiness *serviceruntime.Readiness,
	metrics *observability.Metrics,
	authorityApplication *application.Authority,
) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.PrometheusHandler())
	mux.HandleFunc("/livez", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = response.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(response http.ResponseWriter, request *http.Request) {
		if ready, _ := readiness.Ready(); !ready {
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), config.ReadinessTimeout)
		defer cancel()
		if err := authorityApplication.Ready(ctx); err != nil {
			metrics.SetReady(false)
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
		metrics.SetReady(true)
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = response.Write([]byte("ready\n"))
	})
	return &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
}

func runReplayCleanup(
	ctx context.Context,
	config Config,
	store *authorityrepository.Store,
) error {
	ticker := time.NewTicker(config.ReplayCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			if err := store.DeleteExpired(
				ctx,
				now.UTC().Add(-config.ReplayRetentionAfterExpiry),
			); err != nil {
				return err
			}
		}
	}
}

func stopGRPC(ctx context.Context, server *grpc.Server) error {
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		server.Stop()
		return ctx.Err()
	}
}

func readPrivateFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() ||
		info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0o007 != 0 ||
		info.Size() <= 0 ||
		info.Size() > limit {
		return nil, errors.New("secret file type, mode or size is invalid")
	}
	return os.ReadFile(path)
}

func allowedMethods(mode Mode) map[string]string {
	if mode == ModeIssuer {
		return map[string]string{
			internalrpcauthorityv1.AuthorizationIssuerService_IssueAuthorizationContext_FullMethodName: "issue_authorization_context",
			internalrpcauthorityv1.AuthorizationIssuerService_CheckReadiness_FullMethodName:            "check_readiness",
		}
	}
	return map[string]string{
		internalrpcauthorityv1.AuthorizationVerifierService_VerifyAuthorizationContext_FullMethodName: "verify_authorization_context",
		internalrpcauthorityv1.AuthorizationVerifierService_CheckReadiness_FullMethodName:             "check_readiness",
	}
}

func normalizedMethod(mode Mode, fullMethod string) string {
	if operation, ok := allowedMethods(mode)[fullMethod]; ok {
		return operation
	}
	return "unknown"
}
