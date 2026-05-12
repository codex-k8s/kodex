package app

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	vaultapi "github.com/hashicorp/vault/api"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	accessclient "github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/clients/access"
	fleetkubernetes "github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/clients/kubernetes"
	fleetservice "github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/service"
	fleetpostgres "github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/repository/postgres/fleet"
	fleetgrpc "github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/transport/grpc"
	grpcruntime "google.golang.org/grpc"
)

// Run starts fleet-manager process servers and shuts them down with context.
func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	dbPool, err := postgreslib.OpenPool(ctx, cfg.DatabasePoolSettings())
	if err != nil {
		return err
	}
	defer dbPool.Close()
	eventLogPool, err := serviceprocess.OpenEventLogPool(ctx, cfg.needsEventLogDatabase(), cfg.EventLogDatabasePoolSettings())
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

	fleetRepository := fleetpostgres.NewRepository(dbPool)
	seed, err := cfg.PlatformDefaultSeed()
	if err != nil {
		return err
	}
	secretResolver, err := newSecretResolver(cfg.SecretResolver)
	if err != nil {
		return err
	}
	checker, err := fleetkubernetes.NewChecker(secretResolver, cfg.Connectivity.CheckTimeout)
	if err != nil {
		return err
	}
	fleetService := fleetservice.NewWithConfig(fleetRepository, systemClock{}, uuidGenerator{}, fleetservice.Config{
		Authorizer:          authorizer,
		ConnectivityChecker: checker,
		PlatformDefaultSeed: seed,
	})
	if cfg.Bootstrap.SeedEnabled {
		if err := fleetService.EnsurePlatformDefaultSeed(ctx); err != nil {
			return err
		}
	}
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           serviceprocess.NewHealthMux(readinessChecks(fleetRepository, eventLogPool), 2*time.Second),
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcMetrics, err := grpcserver.NewMetrics(nil, grpcserver.MetricsConfig{
		Subsystem:   "fleet_manager_grpc",
		ServiceName: "fleet-manager",
	})
	if err != nil {
		return err
	}
	grpcServer, err := grpcserver.NewServer(cfg.GRPCServerConfig(), grpcserver.Dependencies{
		Logger:        logger,
		Metrics:       grpcMetrics,
		Authenticator: grpcserver.NewSharedTokenAuthenticator(cfg.GRPC.AuthToken),
		UnaryInterceptors: []grpcserver.UnaryInterceptor{
			fleetgrpc.UnaryErrorInterceptor(logger),
		},
	})
	if err != nil {
		return err
	}
	fleetgrpc.RegisterFleetManagerService(grpcServer, fleetService)

	errCh := make(chan error, 3)
	go serviceprocess.StartHTTPServer(httpServer, "fleet-manager", logger, errCh)
	go serviceprocess.StartGRPCServer(grpcServer, "fleet-manager", cfg.GRPCAddr, logger, errCh)
	outboxRuntime := serviceprocess.OutboxRuntimeConfig{
		Dispatcher:          cfg.OutboxDispatcherConfig(),
		EventLogSource:      cfg.Outbox.EventLogSource,
		PublisherKind:       cfg.Outbox.PublisherKind,
		DispatchEnabled:     cfg.Outbox.DispatchEnabled,
		AllowLossyPublisher: cfg.Outbox.AllowLossyPublisher,
	}
	err = serviceprocess.StartOutboxDispatcher(
		ctx,
		"fleet-manager",
		fleetRepository,
		outboxEvent,
		outboxRuntime,
		serviceprocess.EventLogAppender(eventLogPool),
		logger,
		errCh,
	)
	if err != nil {
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

func readinessChecks(fleet serviceprocess.PingStore, eventLog serviceprocess.PingStore) []serviceprocess.ReadinessCheck {
	return serviceprocess.ServiceDatabaseReadinessChecks("fleet service", true, "fleet database", fleet, eventLog)
}

func newAuthorizer(cfg Config) (fleetservice.Authorizer, *grpcruntime.ClientConn, error) {
	accessConfig := accessclient.Config{
		Addr:      cfg.Access.AccessManagerGRPCAddr,
		AuthToken: cfg.Access.AccessManagerAuthToken,
		Timeout:   cfg.Access.CheckTimeout,
	}
	return connectAuthorizer(cfg.Access.CheckEnabled, accessConfig)
}

func connectAuthorizer(enabled bool, accessConfig accessclient.Config) (fleetservice.Authorizer, *grpcruntime.ClientConn, error) {
	if !enabled {
		return fleetservice.AllowAllAuthorizer{}, nil, nil
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

func newSecretResolver(cfg FleetSecretConfig) (secretresolver.Resolver, error) {
	backends := make(map[string]secretresolver.Backend)
	if cfg.EnvEnabled {
		backends[secretresolver.StoreTypeEnv] = secretresolver.NewEnvBackend()
	}
	if cfg.MountedKubernetesRoot != "" {
		backend, err := secretresolver.NewMountedKubernetesBackend(secretresolver.MountedKubernetesBackendConfig{
			Root:           cfg.MountedKubernetesRoot,
			MaxSecretBytes: cfg.MountedKubernetesMaxBytes,
		})
		if err != nil {
			return nil, err
		}
		backends[secretresolver.StoreTypeKubernetesMountedSecret] = backend
	}
	if strings.TrimSpace(cfg.VaultAddr) != "" {
		vaultConfig := vaultapi.DefaultConfig()
		vaultConfig.Address = strings.TrimSpace(cfg.VaultAddr)
		vaultClient, err := vaultapi.NewClient(vaultConfig)
		if err != nil {
			return nil, err
		}
		vaultClient.SetToken(strings.TrimSpace(cfg.VaultToken))
		if namespace := strings.TrimSpace(cfg.VaultNamespace); namespace != "" {
			vaultClient.SetNamespace(namespace)
		}
		backend, err := secretresolver.NewVaultBackend(secretresolver.VaultBackendConfig{Client: vaultClient})
		if err != nil {
			return nil, err
		}
		backends[secretresolver.StoreTypeVault] = backend
	}
	return secretresolver.NewMux(backends)
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID {
	return uuid.New()
}
