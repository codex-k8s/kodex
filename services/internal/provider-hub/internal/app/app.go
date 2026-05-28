// Package app contains provider-hub process composition and lifecycle.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	provideraccess "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/clients/access"
	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
	providerservice "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/service"
	providerclient "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/provider/client"
	providergithub "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/provider/github"
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
	eventLogPool, err := openOutboxEventLogPool(ctx, cfg)
	if err != nil {
		return err
	}
	if eventLogPool != nil {
		defer eventLogPool.Close()
	}
	accessConn, err := provideraccess.NewConnection(provideraccess.Config{Addr: cfg.AccessManagerGRPCAddr})
	if err != nil {
		return err
	}
	defer func() { _ = accessConn.Close() }()
	accountUsage, err := provideraccess.NewExternalAccountUsageResolver(accessaccountsv1.NewAccessManagerServiceClient(accessConn), provideraccess.Config{
		Addr:      cfg.AccessManagerGRPCAddr,
		AuthToken: cfg.AccessManagerGRPCAuthToken,
		Timeout:   cfg.AccessManagerGRPCTimeout,
	})
	if err != nil {
		return err
	}
	secretResolver, err := buildSecretResolver(cfg)
	if err != nil {
		return err
	}

	providerRepository := providerpostgres.NewRepository(dbPool)
	githubAdapter := providergithub.New(providergithub.Config{
		BaseURL:   cfg.GitHubBaseURL,
		UserAgent: cfg.GitHubUserAgent,
	})
	components := processComponents{
		DBPool:         dbPool,
		EventLogDBPool: eventLogPool,
		ProviderService: providerservice.NewWithDependencies(providerservice.Dependencies{
			Repository:                 providerRepository,
			AccountUsageResolver:       accountUsage,
			SecretResolver:             secretResolver,
			WebhookPayloadRetention:    cfg.WebhookPayloadRetention,
			WebhookPayloadCleanupLimit: cfg.WebhookPayloadCleanupLimit,
			ProviderAdapters:           []providerclient.Adapter{githubAdapter},
			ProviderWriteExecutors: []providerclient.WriteExecutor{
				githubAdapter,
			},
			WebhookNormalizers: []providerrepo.WebhookNormalizer{githubAdapter},
		}),
		OutboxStore: providerRepository,
	}
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           serviceprocess.NewHealthMux(readinessChecks(components), 2*time.Second),
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

	errCh := make(chan error, 3)
	go serviceprocess.StartHTTPServer(httpServer, "provider-hub", logger, errCh)
	go serviceprocess.StartGRPCServer(grpcServer, "provider-hub", cfg.GRPCAddr, logger, errCh)
	if err := startOutboxDispatcher(ctx, cfg, logger, eventLogPool, components.OutboxStore, errCh); err != nil {
		return err
	}

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
	DBPool          *pgxpool.Pool
	EventLogDBPool  *pgxpool.Pool
	ProviderService *providerservice.Service
	OutboxStore     serviceOutboxStore
}

func openOutboxEventLogPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	settings, ok := cfg.optionalEventLogDatabasePoolSettings()
	if !ok {
		return nil, nil
	}
	return serviceprocess.OpenEventLogPool(ctx, true, settings)
}

func startOutboxDispatcher(ctx context.Context, cfg Config, logger *slog.Logger, eventLogPool *pgxpool.Pool, store serviceOutboxStore, errCh chan<- error) error {
	return serviceprocess.StartOutboxDispatcher(
		ctx,
		"provider-hub",
		store,
		outboxEvent,
		serviceprocess.OutboxRuntimeConfig{
			DispatchEnabled:     cfg.OutboxDispatchEnabled,
			PublisherKind:       cfg.OutboxPublisherKind,
			AllowLossyPublisher: cfg.OutboxAllowLossyPublisher,
			EventLogSource:      cfg.OutboxEventLogSource,
			Dispatcher:          cfg.OutboxDispatcherConfig(),
		},
		serviceprocess.EventLogAppender(eventLogPool),
		logger,
		errCh,
	)
}

func buildSecretResolver(cfg Config) (secretresolver.Resolver, error) {
	mounted, err := secretresolver.NewMountedKubernetesBackend(secretresolver.MountedKubernetesBackendConfig{
		Root:           cfg.SecretMountedRoot,
		MaxSecretBytes: cfg.SecretMaxBytes,
	})
	if err != nil {
		return nil, err
	}
	backends := map[string]secretresolver.Backend{
		secretresolver.StoreTypeEnv:                     secretresolver.NewEnvBackend(),
		secretresolver.StoreTypeKubernetesMountedSecret: mounted,
	}
	vaultAddr := strings.TrimSpace(cfg.VaultAddr)
	if vaultAddr != "" {
		vaultBackend, err := secretresolver.NewVaultBackendFromClientConfig(secretresolver.VaultClientConfig{
			Addr:      vaultAddr,
			Token:     cfg.VaultToken,
			Namespace: cfg.VaultNamespace,
		})
		if err != nil {
			return nil, err
		}
		backends[secretresolver.StoreTypeVault] = vaultBackend
	}
	return secretresolver.NewMux(backends)
}

func readinessChecks(components processComponents) []serviceprocess.ReadinessCheck {
	return serviceprocess.ServiceDatabaseReadinessChecks(
		"provider service",
		components.ProviderService != nil,
		"provider database",
		components.DBPool,
		components.EventLogDBPool,
	)
}
