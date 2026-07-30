package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/grpcserver"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/application"
	vaultclient "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/client/vault"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/publisher"
	publisherrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/postgres/publisher"
	sessionrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/postgres/session"
	authoritygrpc "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/transport/grpc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
)

type PublisherConfig struct {
	Listen                       string
	TechnicalListen              string
	TLSCertificateFile           string
	TLSPrivateKeyFile            string
	ClientCAFile                 string
	PostgresDSNFile              string
	PostgresTLSServerName        string
	PostgresExpectedUser         string
	VaultAddress                 string
	VaultTLSServerName           string
	VaultCAFile                  string
	VaultAuthRole                string
	VaultAuthFile                string
	TargetRegistryFile           string
	SignerPrivateJWKFile         string
	SignerSourceRevision         uint64
	SignerSourceDigest           string
	SignerKeySetRevision         uint64
	SignerGeneration             uint64
	ReadbackSignerPrivateJWKFile string
	ReadbackSignerSourceRevision uint64
	ReadbackSignerSourceDigest   string
	ReadbackSignerKeySetRevision uint64
	ReadbackSignerGeneration     uint64
	ControllerGeneration         uint64
	ShutdownTimeout              time.Duration
}

func LoadPublisherConfig() (PublisherConfig, error) {
	parseRevision := func(name string) (uint64, error) {
		value, err := strconv.ParseUint(strings.TrimSpace(os.Getenv(name)), 10, 64)
		if err != nil || value == 0 {
			return 0, errors.New(name + " is invalid")
		}
		return value, nil
	}
	signerSourceRevision, err := parseRevision(
		"INTERNAL_RPC_AUTHORITY_RESTORE_SIGNER_SOURCE_REVISION",
	)
	if err != nil {
		return PublisherConfig{}, err
	}
	signerKeySetRevision, err := parseRevision(
		"INTERNAL_RPC_AUTHORITY_RESTORE_SIGNER_KEY_SET_REVISION",
	)
	if err != nil {
		return PublisherConfig{}, err
	}
	signerGeneration, err := parseRevision(
		"INTERNAL_RPC_AUTHORITY_RESTORE_SIGNER_GENERATION",
	)
	if err != nil {
		return PublisherConfig{}, err
	}
	controllerGeneration, err := parseRevision(
		"INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_GENERATION",
	)
	if err != nil {
		return PublisherConfig{}, err
	}
	readbackSignerSourceRevision, err := parseRevision(
		"INTERNAL_RPC_AUTHORITY_READBACK_SIGNER_SOURCE_REVISION",
	)
	if err != nil {
		return PublisherConfig{}, err
	}
	readbackSignerKeySetRevision, err := parseRevision(
		"INTERNAL_RPC_AUTHORITY_READBACK_SIGNER_KEY_SET_REVISION",
	)
	if err != nil {
		return PublisherConfig{}, err
	}
	readbackSignerGeneration, err := parseRevision(
		"INTERNAL_RPC_AUTHORITY_READBACK_SIGNER_GENERATION",
	)
	if err != nil {
		return PublisherConfig{}, err
	}
	config := PublisherConfig{
		Listen:                envOrDefault("INTERNAL_RPC_AUTHORITY_PUBLISHER_LISTEN", ":8444"),
		TechnicalListen:       envOrDefault("INTERNAL_RPC_AUTHORITY_TECHNICAL_LISTEN", ":9090"),
		TLSCertificateFile:    envOrDefault("INTERNAL_RPC_AUTHORITY_TLS_CERTIFICATE_FILE", "/var/run/secrets/mattercodex/internal-rpc-authority/publisher/tls/tls.crt"),
		TLSPrivateKeyFile:     envOrDefault("INTERNAL_RPC_AUTHORITY_TLS_PRIVATE_KEY_FILE", "/var/run/secrets/mattercodex/internal-rpc-authority/publisher/tls/tls.key"),
		ClientCAFile:          envOrDefault("INTERNAL_RPC_AUTHORITY_CLIENT_CA_FILE", "/var/run/config/mattercodex/internal-rpc-authority/publisher/client-ca.pem"),
		PostgresDSNFile:       envOrDefault("INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE", "/var/run/secrets/mattercodex/internal-rpc-authority/publisher/database/dsn"),
		PostgresTLSServerName: envOrDefault("INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME", "internal-rpc-authority-postgresql.mattercodex-system.svc.cluster.local"),
		PostgresExpectedUser:  strings.TrimSpace(os.Getenv("INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER")),
		VaultAddress:          envOrDefault("INTERNAL_RPC_AUTHORITY_VAULT_ADDRESS", "https://vault.mattercodex-system.svc:8200"),
		VaultTLSServerName:    envOrDefault("INTERNAL_RPC_AUTHORITY_VAULT_TLS_SERVER_NAME", "vault.mattercodex-system.svc.cluster.local"),
		VaultCAFile:           envOrDefault("INTERNAL_RPC_AUTHORITY_VAULT_CA_FILE", "/var/run/config/mattercodex/internal-rpc-authority/publisher/vault-ca.pem"),
		VaultAuthRole:         envOrDefault("INTERNAL_RPC_AUTHORITY_PUBLISHER_VAULT_ROLE", "internal-rpc-authority-publisher"),
		VaultAuthFile:         envOrDefault("INTERNAL_RPC_AUTHORITY_PUBLISHER_VAULT_AUTH_FILE", "/var/run/secrets/tokens/vault/token"),
		TargetRegistryFile:    envOrDefault("INTERNAL_RPC_AUTHORITY_TARGET_REGISTRY_FILE", "/usr/local/share/internal-rpc-authority/bootstrap-key-delivery-targets.yaml"),
		SignerPrivateJWKFile:  envOrDefault("INTERNAL_RPC_AUTHORITY_RESTORE_SIGNER_PRIVATE_JWK_FILE", "/var/run/secrets/mattercodex/internal-rpc-authority/publisher/restore-signer/private.jwk"),
		SignerSourceRevision:  signerSourceRevision,
		SignerSourceDigest:    strings.TrimSpace(os.Getenv("INTERNAL_RPC_AUTHORITY_RESTORE_SIGNER_SOURCE_DIGEST_SHA256")),
		SignerKeySetRevision:  signerKeySetRevision,
		SignerGeneration:      signerGeneration,
		ReadbackSignerPrivateJWKFile: envOrDefault(
			"INTERNAL_RPC_AUTHORITY_READBACK_SIGNER_PRIVATE_JWK_FILE",
			"/var/run/secrets/mattercodex/internal-rpc-authority/publisher/readback-signer/private.jwk",
		),
		ReadbackSignerSourceRevision: readbackSignerSourceRevision,
		ReadbackSignerSourceDigest: strings.TrimSpace(
			os.Getenv("INTERNAL_RPC_AUTHORITY_READBACK_SIGNER_SOURCE_DIGEST_SHA256"),
		),
		ReadbackSignerKeySetRevision: readbackSignerKeySetRevision,
		ReadbackSignerGeneration:     readbackSignerGeneration,
		ControllerGeneration:         controllerGeneration,
		ShutdownTimeout:              10 * time.Second,
	}
	if _, _, err := net.SplitHostPort(config.Listen); err != nil {
		return PublisherConfig{}, errors.New("publisher listen address is invalid")
	}
	if _, _, err := net.SplitHostPort(config.TechnicalListen); err != nil {
		return PublisherConfig{}, errors.New("publisher technical listen address is invalid")
	}
	if config.PostgresExpectedUser == "" ||
		!digestPattern.MatchString(config.SignerSourceDigest) ||
		!digestPattern.MatchString(config.ReadbackSignerSourceDigest) {
		return PublisherConfig{}, errors.New("publisher database or signer identity is invalid")
	}
	return config, nil
}

func RunPublisher(
	lifecycle context.Context,
	shutdownBase context.Context,
	buildVersion string,
) error {
	config, err := LoadPublisherConfig()
	if err != nil {
		return err
	}
	telemetry, logger, err := startTelemetry(
		lifecycle,
		"internal_rpc_authority_publisher",
		buildVersion,
	)
	if err != nil {
		return err
	}
	telemetryFinished := false
	defer func() {
		if !telemetryFinished {
			telemetry.cleanupAfterStartupFailure(shutdownBase, config.ShutdownTimeout)
		}
	}()
	startup, cancel := context.WithTimeout(lifecycle, 15*time.Second)
	defer cancel()
	pool, err := openPublisherPostgres(startup, config)
	if err != nil {
		return err
	}
	store, err := publisherrepository.New(pool)
	if err != nil {
		pool.Close()
		return err
	}
	targetRegistry, err := publisher.LoadRegistry(config.TargetRegistryFile)
	if err != nil {
		pool.Close()
		return err
	}
	signerRaw, err := readPrivateFile(config.SignerPrivateJWKFile, 64<<10)
	if err != nil {
		pool.Close()
		return errors.New("read restore credential signer key")
	}
	signerKey, err := internalrpcauth.ParsePrivateJWK(signerRaw)
	if err != nil {
		pool.Close()
		return errors.New("parse restore credential signer key")
	}
	readbackSignerRaw, err := readPrivateFile(
		config.ReadbackSignerPrivateJWKFile,
		64<<10,
	)
	if err != nil {
		pool.Close()
		return errors.New("read readback credential signer key")
	}
	readbackSignerKey, err := internalrpcauth.ParsePrivateJWK(readbackSignerRaw)
	if err != nil {
		pool.Close()
		return errors.New("parse readback credential signer key")
	}
	vault, err := vaultclient.NewStaticRoleClient(vaultclient.Config{
		Address: config.VaultAddress, TLSServerName: config.VaultTLSServerName,
		CAFile: config.VaultCAFile, AuthMount: "kubernetes",
		AuthRole:                config.VaultAuthRole,
		ServiceAccountTokenFile: config.VaultAuthFile,
		Timeout:                 5 * time.Second,
	})
	if err != nil {
		pool.Close()
		return err
	}
	domainService, err := service.NewPublisher(
		targetRegistry,
		service.RestoreCredentialSigner{
			Key: signerKey, SourceRevision: config.SignerSourceRevision,
			SourceDigest:   config.SignerSourceDigest,
			KeySetRevision: config.SignerKeySetRevision,
			Generation:     config.SignerGeneration,
		},
		service.ReadbackCredentialSigner{
			Key:            readbackSignerKey,
			SourceRevision: config.ReadbackSignerSourceRevision,
			SourceDigest:   config.ReadbackSignerSourceDigest,
			KeySetRevision: config.ReadbackSignerKeySetRevision,
			Generation:     config.ReadbackSignerGeneration,
		},
		store,
		vault,
	)
	if err != nil {
		vault.Close()
		pool.Close()
		return err
	}
	publisherApplication := application.NewPublisher(domainService)
	if _, err := publisherApplication.PublishReadbackMaterials(startup); err != nil {
		vault.Close()
		pool.Close()
		return errors.New("publish startup readback materials")
	}
	if err := publisherApplication.Ready(startup); err != nil {
		vault.Close()
		pool.Close()
		return err
	}
	serverTLS, err := loadMTLSServerConfig(
		config.TLSCertificateFile,
		config.TLSPrivateKeyFile,
		config.ClientCAFile,
	)
	if err != nil {
		vault.Close()
		pool.Close()
		return err
	}
	methods := map[string]string{
		internalrpcauthorityv1.RestoreRoleCredentialPublisherService_PublishRoleCredential_FullMethodName: "publish_role_credential",
		internalrpcauthorityv1.RestoreRoleCredentialPublisherService_CheckReadiness_FullMethodName:        "check_readiness",
	}
	metrics := observability.NewMetrics(
		"internal_rpc_authority_publisher",
		buildVersion,
		methods,
	)
	readiness := serviceruntime.NewReadiness()
	readiness.Set(false, "starting")
	metrics.SetReady(false)
	grpcRuntime := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(serverTLS)),
		grpc.ForceServerCodec(grpcserver.StrictProtoCodec()),
		grpc.ChainUnaryInterceptor(
			metrics.UnaryServerInterceptor(),
			telemetry.UnaryServerInterceptor(methods),
			grpcserver.ErrorBoundary(grpcserver.ErrorObserverFunc(func(
				_ context.Context,
				method string,
				code codes.Code,
				_ error,
			) {
				logger.Error(
					"unexpected gRPC failure",
					"method", normalizePublisherMethod(method),
					"code", code.String(),
				)
			})),
		),
	)
	internalrpcauthorityv1.RegisterRestoreRoleCredentialPublisherServiceServer(
		grpcRuntime,
		authoritygrpc.NewRestoreRoleCredentialPublisherServer(
			publisherApplication,
			config.ControllerGeneration,
		),
	)
	grpcListener, err := net.Listen("tcp", config.Listen)
	if err != nil {
		vault.Close()
		pool.Close()
		return errors.New("listen on publisher endpoint")
	}
	technicalListener, err := net.Listen("tcp", config.TechnicalListen)
	if err != nil {
		_ = grpcListener.Close()
		vault.Close()
		pool.Close()
		return errors.New("listen on publisher technical endpoint")
	}
	technicalServer := newPublisherTechnicalServer(
		readiness,
		metrics,
		publisherApplication,
	)
	workers := serviceruntime.StartWorkers(lifecycle, func(ctx context.Context) error {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			if _, publishErr := publisherApplication.PublishReadbackMaterials(ctx); publishErr != nil {
				readiness.Set(false, "readback-publication-failed")
				metrics.SetReady(false)
			} else {
				readiness.Set(true, "ready")
				metrics.SetReady(true)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	})
	serveErrors := make(chan error, 2)
	go func() {
		if serveErr := grpcRuntime.Serve(grpcListener); serveErr != nil {
			serveErrors <- errors.New("serve publisher gRPC")
		}
	}()
	go func() {
		if serveErr := technicalServer.Serve(technicalListener); serveErr != nil &&
			!errors.Is(serveErr, http.ErrServerClosed) {
			serveErrors <- errors.New("serve publisher technical HTTP")
		}
	}()
	readiness.Set(true, "ready")
	metrics.SetReady(true)
	logger.Info("authority publisher started")
	var runtimeErr error
	select {
	case <-lifecycle.Done():
	case runtimeErr = <-serveErrors:
	}
	readiness.Set(false, "shutting-down")
	metrics.SetReady(false)
	shutdownErr := serviceruntime.RunShutdown(
		shutdownBase,
		serviceruntime.ShutdownOperation{
			Name: "workers", Timeout: config.ShutdownTimeout,
			Run: func(ctx context.Context) error {
				workers.Stop()
				return workers.Wait(ctx)
			},
		},
		serviceruntime.ShutdownOperation{
			Name: "grpc-server", Timeout: config.ShutdownTimeout,
			Run: func(ctx context.Context) error { return stopGRPC(ctx, grpcRuntime) },
		},
		serviceruntime.ShutdownOperation{
			Name: "technical-http", Timeout: config.ShutdownTimeout,
			Run: technicalServer.Shutdown,
		},
		serviceruntime.ShutdownOperation{
			Name: "vault-http", Timeout: config.ShutdownTimeout,
			Run: func(context.Context) error {
				vault.Close()
				return nil
			},
		},
		serviceruntime.ShutdownOperation{
			Name: "postgresql", Timeout: config.ShutdownTimeout,
			Run: func(context.Context) error {
				pool.Close()
				return nil
			},
		},
		serviceruntime.ShutdownOperation{
			Name: "otel-tracing", Timeout: config.ShutdownTimeout,
			Run: telemetry.shutdownTracing,
		},
		serviceruntime.ShutdownOperation{
			Name: "sentry-flush", Timeout: config.ShutdownTimeout,
			Run: telemetry.flushSentry,
		},
	)
	telemetryFinished = true
	logger.Info("authority publisher stopped")
	return errors.Join(runtimeErr, shutdownErr)
}

func openPublisherPostgres(
	ctx context.Context,
	config PublisherConfig,
) (*pgxpool.Pool, error) {
	raw, err := readPrivateFile(config.PostgresDSNFile, maxDSNFileBytes)
	if err != nil {
		return nil, errors.New("read publisher PostgreSQL DSN file")
	}
	poolConfig, err := pgxpool.ParseConfig(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, errors.New("parse publisher PostgreSQL DSN")
	}
	instrumentPGX(poolConfig, "internal_rpc_authority_publisher")
	if len(poolConfig.ConnConfig.Fallbacks) != 0 ||
		poolConfig.ConnConfig.Host != config.PostgresTLSServerName ||
		poolConfig.ConnConfig.TLSConfig == nil ||
		poolConfig.ConnConfig.TLSConfig.RootCAs == nil ||
		poolConfig.ConnConfig.TLSConfig.ServerName != config.PostgresTLSServerName ||
		poolConfig.ConnConfig.TLSConfig.InsecureSkipVerify {
		return nil, errors.New("publisher PostgreSQL TLS boundary rejected")
	}
	poolConfig.MaxConns = 8
	poolConfig.ConnConfig.RuntimeParams["application_name"] =
		"internal_rpc_authority_publisher"
	poolConfig.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		return sessionrepository.Configure(
			ctx,
			connection,
			config.PostgresExpectedUser,
			sessionrepository.CapabilityPublisher,
		)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, errors.New("open publisher PostgreSQL pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("verify publisher PostgreSQL connectivity")
	}
	return pool, nil
}

func newPublisherTechnicalServer(
	readiness *serviceruntime.Readiness,
	metrics *observability.Metrics,
	publisherApplication *application.Publisher,
) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.PrometheusHandler())
	mux.HandleFunc("/livez", func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(response http.ResponseWriter, request *http.Request) {
		if ready, _ := readiness.Ready(); !ready {
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := publisherApplication.Ready(ctx); err != nil {
			metrics.SetReady(false)
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
		metrics.SetReady(true)
		_, _ = response.Write([]byte("ready\n"))
	})
	return &http.Server{
		Handler: mux, ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
		IdleTimeout: 30 * time.Second,
	}
}

func normalizePublisherMethod(method string) string {
	switch method {
	case internalrpcauthorityv1.RestoreRoleCredentialPublisherService_PublishRoleCredential_FullMethodName:
		return "publish_role_credential"
	case internalrpcauthorityv1.RestoreRoleCredentialPublisherService_CheckReadiness_FullMethodName:
		return "check_readiness"
	default:
		return "unknown"
	}
}
