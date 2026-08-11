package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/grpcserver"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/application"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/publisher"
	snapshotdelivery "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/kubernetes/snapshot"
	publisherrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/postgres/publisher"
	sessionrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/postgres/session"
	authoritygrpc "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/transport/grpc"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
)

// PublisherConfig задаёт соединения, ключи и реестр publisher.
type PublisherConfig struct {
	Listen                       string        `env:"INTERNAL_RPC_AUTHORITY_PUBLISHER_LISTEN"`
	TechnicalListen              string        `env:"INTERNAL_RPC_AUTHORITY_TECHNICAL_LISTEN"`
	TLSCertificateFile           string        `env:"INTERNAL_RPC_AUTHORITY_TLS_CERTIFICATE_FILE"`
	TLSPrivateKeyFile            string        `env:"INTERNAL_RPC_AUTHORITY_TLS_PRIVATE_KEY_FILE"`
	ClientCAFile                 string        `env:"INTERNAL_RPC_AUTHORITY_CLIENT_CA_FILE"`
	PostgresDSNFile              string        `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE"`
	PostgresTLSServerName        string        `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME"`
	PostgresExpectedUser         string        `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER"`
	PodUID                       string        `env:"POD_UID"`
	SecretBackend                string        `env:"INTERNAL_RPC_AUTHORITY_SECRET_BACKEND"`
	DeploymentProfile            string        `env:"INTERNAL_RPC_AUTHORITY_DEPLOYMENT_PROFILE"`
	VaultAddress                 string        `env:"INTERNAL_RPC_AUTHORITY_VAULT_ADDRESS"`
	VaultTLSServerName           string        `env:"INTERNAL_RPC_AUTHORITY_VAULT_TLS_SERVER_NAME"`
	VaultCAFile                  string        `env:"INTERNAL_RPC_AUTHORITY_VAULT_CA_FILE"`
	VaultAuthRole                string        `env:"INTERNAL_RPC_AUTHORITY_PUBLISHER_VAULT_ROLE"`
	VaultAuthFile                string        `env:"INTERNAL_RPC_AUTHORITY_PUBLISHER_VAULT_AUTH_FILE"`
	TargetRegistryFile           string        `env:"INTERNAL_RPC_AUTHORITY_TARGET_REGISTRY_FILE"`
	SignerPrivateJWKFile         string        `env:"INTERNAL_RPC_AUTHORITY_RESTORE_SIGNER_PRIVATE_JWK_FILE"`
	SignerSourceRevision         uint64        `env:"INTERNAL_RPC_AUTHORITY_RESTORE_SIGNER_SOURCE_REVISION"`
	SignerSourceDigest           string        `env:"INTERNAL_RPC_AUTHORITY_RESTORE_SIGNER_SOURCE_DIGEST_SHA256"`
	SignerKeySetRevision         uint64        `env:"INTERNAL_RPC_AUTHORITY_RESTORE_SIGNER_KEY_SET_REVISION"`
	SignerGeneration             uint64        `env:"INTERNAL_RPC_AUTHORITY_RESTORE_SIGNER_GENERATION"`
	ReadbackSignerPrivateJWKFile string        `env:"INTERNAL_RPC_AUTHORITY_READBACK_SIGNER_PRIVATE_JWK_FILE"`
	ReadbackSignerSourceRevision uint64        `env:"INTERNAL_RPC_AUTHORITY_READBACK_SIGNER_SOURCE_REVISION"`
	ReadbackSignerSourceDigest   string        `env:"INTERNAL_RPC_AUTHORITY_READBACK_SIGNER_SOURCE_DIGEST_SHA256"`
	ReadbackSignerKeySetRevision uint64        `env:"INTERNAL_RPC_AUTHORITY_READBACK_SIGNER_KEY_SET_REVISION"`
	ReadbackSignerGeneration     uint64        `env:"INTERNAL_RPC_AUTHORITY_READBACK_SIGNER_GENERATION"`
	ManifestSignerPrivateJWKFile string        `env:"INTERNAL_RPC_AUTHORITY_MANIFEST_SIGNER_PRIVATE_JWK_FILE"`
	ManifestSignerGeneration     uint64        `env:"INTERNAL_RPC_AUTHORITY_MANIFEST_SIGNER_GENERATION"`
	ManifestRootPublicJWKFile    string        `env:"INTERNAL_RPC_AUTHORITY_MANIFEST_ROOT_PUBLIC_JWK_FILE"`
	ManifestRootMetadataFile     string        `env:"INTERNAL_RPC_AUTHORITY_MANIFEST_ROOT_METADATA_FILE"`
	ManifestTrustBundleJWSFile   string        `env:"INTERNAL_RPC_AUTHORITY_MANIFEST_TRUST_BUNDLE_JWS_FILE"`
	AuthorityPolicyFile          string        `env:"INTERNAL_RPC_AUTHORITY_PUBLISHER_POLICY_FILE"`
	KubernetesAPIAddress         string        `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_API_ADDRESS"`
	KubernetesAPITLSServerName   string        `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_API_TLS_SERVER_NAME"`
	KubernetesAPICAFile          string        `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_API_CA_FILE"`
	KubernetesAPITokenFile       string        `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_API_TOKEN_FILE"`
	ControllerGeneration         uint64        `env:"INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_GENERATION"`
	ShutdownTimeout              time.Duration `env:"INTERNAL_RPC_AUTHORITY_SHUTDOWN_TIMEOUT"`
}

// LoadPublisherConfig читает и проверяет типизированное окружение publisher.
func LoadPublisherConfig() (PublisherConfig, error) {
	config := PublisherConfig{
		Listen:                       ":8444",
		TechnicalListen:              ":9090",
		TLSCertificateFile:           "/var/run/secrets/mattercodex/internal-rpc-authority/publisher/tls/tls.crt",
		TLSPrivateKeyFile:            "/var/run/secrets/mattercodex/internal-rpc-authority/publisher/tls/tls.key",
		ClientCAFile:                 "/var/run/config/mattercodex/internal-rpc-authority/publisher/client-ca.pem",
		PostgresDSNFile:              "/var/run/secrets/mattercodex/internal-rpc-authority/publisher/database/dsn",
		PostgresTLSServerName:        "internal-rpc-authority-postgresql-rw.mattercodex-system.svc.cluster.local",
		SecretBackend:                string(secretBackendVault),
		VaultAddress:                 "https://vault.mattercodex-system.svc:8200",
		VaultTLSServerName:           "vault.mattercodex-system.svc.cluster.local",
		VaultCAFile:                  "/var/run/config/mattercodex/internal-rpc-authority/publisher/vault-ca.pem",
		VaultAuthRole:                "internal-rpc-authority-publisher",
		VaultAuthFile:                "/var/run/secrets/tokens/vault/token",
		TargetRegistryFile:           "/usr/local/share/internal-rpc-authority/bootstrap-key-delivery-targets.yaml",
		SignerPrivateJWKFile:         "/var/run/secrets/mattercodex/internal-rpc-authority/publisher/restore-signer/private.jwk",
		ReadbackSignerPrivateJWKFile: "/var/run/secrets/mattercodex/internal-rpc-authority/publisher/readback-signer/private.jwk",
		ManifestSignerPrivateJWKFile: "/var/run/secrets/mattercodex/internal-rpc-authority/publisher/manifest-signer/private.jwk",
		ManifestRootPublicJWKFile:    "/usr/local/share/internal-rpc-authority/manifest-root/bootstrap-public.jwk",
		ManifestRootMetadataFile:     "/usr/local/share/internal-rpc-authority/manifest-root/bootstrap-metadata.json",
		ManifestTrustBundleJWSFile:   "/var/run/config/mattercodex/internal-rpc-authority/publisher/manifest-trust/manifest-trust.jws",
		AuthorityPolicyFile:          "/var/run/config/mattercodex/internal-rpc-authority/publisher-targets/authority-policy.json",
		KubernetesAPIAddress:         "https://kubernetes.default.svc:443",
		KubernetesAPITLSServerName:   "kubernetes.default.svc",
		KubernetesAPICAFile:          "/var/run/config/kubernetes.io/serviceaccount/ca.crt",
		KubernetesAPITokenFile:       "/var/run/secrets/tokens/kubernetes-api/token",
		ShutdownTimeout:              10 * time.Second,
	}
	if err := parseEnvironment(&config); err != nil {
		return PublisherConfig{}, err
	}
	backend, err := selectSecretBackend(config.SecretBackend, config.DeploymentProfile)
	if err != nil {
		return PublisherConfig{}, err
	}
	if backend == secretBackendDirectProductionPrototype {
		if err := validatePrototypeKubernetesBoundary(
			config.KubernetesAPIAddress,
			config.KubernetesAPITLSServerName,
			config.KubernetesAPICAFile,
			config.KubernetesAPITokenFile,
		); err != nil {
			return PublisherConfig{}, err
		}
	}
	if _, _, err := net.SplitHostPort(config.Listen); err != nil {
		return PublisherConfig{}, errors.New("publisher listen address is invalid")
	}
	if _, _, err := net.SplitHostPort(config.TechnicalListen); err != nil {
		return PublisherConfig{}, errors.New("publisher technical listen address is invalid")
	}
	if !runtimeUUIDPattern.MatchString(config.PodUID) ||
		config.SignerSourceRevision == 0 ||
		config.SignerKeySetRevision == 0 ||
		config.SignerGeneration == 0 ||
		config.ReadbackSignerSourceRevision == 0 ||
		config.ReadbackSignerKeySetRevision == 0 ||
		config.ReadbackSignerGeneration == 0 ||
		config.ManifestSignerGeneration == 0 ||
		config.ControllerGeneration == 0 ||
		!digestPattern.MatchString(config.SignerSourceDigest) ||
		!digestPattern.MatchString(config.ReadbackSignerSourceDigest) {
		return PublisherConfig{}, errors.New("publisher database or signer identity is invalid")
	}
	return config, nil
}

// RunPublisher запускает доставку ключей и сервер выдачи ролей восстановления.
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
	manifestSignerRaw, err := readPrivateFile(
		config.ManifestSignerPrivateJWKFile,
		64<<10,
	)
	if err != nil {
		pool.Close()
		return errors.New("read authority manifest signer key")
	}
	manifestSignerKey, err := internalrpcauth.ParsePrivateJWK(manifestSignerRaw)
	if err != nil {
		pool.Close()
		return errors.New("parse authority manifest signer key")
	}
	delivery, err := newPublisherSecretDelivery(config, targetRegistry)
	if err != nil {
		pool.Close()
		return err
	}
	snapshotDelivery, err := snapshotdelivery.New(snapshotdelivery.Config{
		Address:       config.KubernetesAPIAddress,
		TLSServerName: config.KubernetesAPITLSServerName,
		CAFile:        config.KubernetesAPICAFile,
		TokenFile:     config.KubernetesAPITokenFile,
		Namespace:     "mattercodex-system",
		SecretName:    "internal-rpc-authority-snapshot",
		Timeout:       5 * time.Second,
	})
	if err != nil {
		delivery.Close()
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
		delivery,
	)
	if err != nil {
		snapshotDelivery.Close()
		delivery.Close()
		pool.Close()
		return err
	}
	graph, err := publisher.NewGraph(publisher.GraphConfig{
		Registry:                   targetRegistry,
		Store:                      store,
		Vault:                      delivery,
		Snapshot:                   snapshotDelivery,
		ManifestSigner:             manifestSignerKey,
		ManifestSignerGeneration:   config.ManifestSignerGeneration,
		ManifestRootPublicJWKFile:  config.ManifestRootPublicJWKFile,
		ManifestRootMetadataFile:   config.ManifestRootMetadataFile,
		ManifestTrustBundleJWSFile: config.ManifestTrustBundleJWSFile,
		PolicyFile:                 config.AuthorityPolicyFile,
	})
	if err != nil {
		snapshotDelivery.Close()
		delivery.Close()
		pool.Close()
		return err
	}
	if err := domainService.AttachAuthorityGraph(graph); err != nil {
		snapshotDelivery.Close()
		delivery.Close()
		pool.Close()
		return err
	}
	publisherApplication := application.NewPublisher(domainService)
	if _, err := publisherApplication.PublishAuthorityGraph(startup); err != nil {
		snapshotDelivery.Close()
		delivery.Close()
		pool.Close()
		return fmt.Errorf("publish startup authority graph: %w", err)
	}
	if _, err := publisherApplication.PublishReadbackMaterials(startup); err != nil {
		snapshotDelivery.Close()
		delivery.Close()
		pool.Close()
		return fmt.Errorf("publish startup readback materials: %w", err)
	}
	serverTLS, err := loadMTLSServerConfig(
		config.TLSCertificateFile,
		config.TLSPrivateKeyFile,
		config.ClientCAFile,
	)
	if err != nil {
		snapshotDelivery.Close()
		delivery.Close()
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
		snapshotDelivery.Close()
		delivery.Close()
		pool.Close()
		return errors.New("listen on publisher endpoint")
	}
	technicalListener, err := net.Listen("tcp", config.TechnicalListen)
	if err != nil {
		snapshotDelivery.Close()
		_ = grpcListener.Close()
		delivery.Close()
		pool.Close()
		return errors.New("listen on publisher technical endpoint")
	}
	technicalServer := newPublisherTechnicalServer(
		readiness,
		metrics,
		publisherApplication,
	)
	readiness.Set(false, "waiting-for-authority-graph-readback")
	metrics.SetReady(false)
	workers := serviceruntime.StartWorkers(lifecycle, func(ctx context.Context) error {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			_, graphErr := publisherApplication.PublishAuthorityGraph(ctx)
			_, publishErr := publisherApplication.PublishReadbackMaterials(ctx)
			readyErr := publisherApplication.Ready(ctx)
			if graphErr != nil || publishErr != nil || readyErr != nil {
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
			Name: "kubernetes-api", Timeout: config.ShutdownTimeout,
			Run: func(context.Context) error {
				snapshotDelivery.Close()
				return nil
			},
		},
		serviceruntime.ShutdownOperation{
			Name: "secret-delivery", Timeout: config.ShutdownTimeout,
			Run: func(context.Context) error {
				delivery.Close()
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
	return openRotatingPostgres(ctx, rotatingPostgresConfig{
		DSNTemplateFile: config.PostgresDSNFile,
		TLSServerName:   config.PostgresTLSServerName,
		Capability:      sessionrepository.CapabilityPublisher,
		ApplicationName: "internal_rpc_authority_publisher",
		PodUID:          config.PodUID,
		Candidates: []databaseCredentialCandidate{
			{Role: "internal-rpc-authority-publisher-g3", Principal: "ira_publisher_g3", Directory: "/var/run/secrets/mattercodex/internal-rpc-authority/publisher/database/g3"},
			{Role: "internal-rpc-authority-publisher-g4", Principal: "ira_publisher_g4", Directory: "/var/run/secrets/mattercodex/internal-rpc-authority/publisher/database/g4"},
			{Role: "internal-rpc-authority-publisher-g5", Principal: "ira_publisher_g5", Directory: "/var/run/secrets/mattercodex/internal-rpc-authority/publisher/database/g5"},
		},
	})
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
