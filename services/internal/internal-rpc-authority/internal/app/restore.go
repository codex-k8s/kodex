package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"log/slog"
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
	publisherclient "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/client/publisher"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/publisher"
	kubernetesrestore "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/kubernetes/restore"
	postgresrestore "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/postgres/restore"
	sessionrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/postgres/session"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/snapshot"
	authoritygrpc "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/transport/grpc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
)

type RestoreControllerConfig struct {
	Listen                   string
	TechnicalListen          string
	TLSCertificateFile       string
	TLSPrivateKeyFile        string
	ClientCAFile             string
	PostgresDSNFile          string
	PostgresTLSServerName    string
	PostgresExpectedUser     string
	DatabaseClusterID        string
	ControllerGeneration     uint64
	TargetRegistryFile       string
	ManifestRootJWKFile      string
	ManifestRootMetadataFile string
	ManifestTrustBundleFile  string
	RestoreRoleTrustFile     string
	PublisherAddress         string
	PublisherTLSServerName   string
	PublisherCAFile          string
	KubernetesAddress        string
	KubernetesTLSServerName  string
	KubernetesCAFile         string
	KubernetesTokenFile      string
	KubernetesNamespace      string
	KubernetesResourceName   string
	ShutdownTimeout          time.Duration
}

func LoadRestoreControllerConfig() (RestoreControllerConfig, error) {
	generation, err := strconv.ParseUint(
		strings.TrimSpace(
			os.Getenv("INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_GENERATION"),
		),
		10,
		64,
	)
	if err != nil || generation == 0 {
		return RestoreControllerConfig{}, errors.New(
			"restore controller generation is invalid",
		)
	}
	config := RestoreControllerConfig{
		Listen:                   envOrDefault("INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_LISTEN", ":8443"),
		TechnicalListen:          envOrDefault("INTERNAL_RPC_AUTHORITY_TECHNICAL_LISTEN", ":9090"),
		TLSCertificateFile:       envOrDefault("INTERNAL_RPC_AUTHORITY_TLS_CERTIFICATE_FILE", "/var/run/secrets/mattercodex/internal-rpc-authority/restore-controller/tls/tls.crt"),
		TLSPrivateKeyFile:        envOrDefault("INTERNAL_RPC_AUTHORITY_TLS_PRIVATE_KEY_FILE", "/var/run/secrets/mattercodex/internal-rpc-authority/restore-controller/tls/tls.key"),
		ClientCAFile:             envOrDefault("INTERNAL_RPC_AUTHORITY_CLIENT_CA_FILE", "/var/run/config/mattercodex/internal-rpc-authority/restore-controller/client-ca.pem"),
		PostgresDSNFile:          envOrDefault("INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE", "/var/run/secrets/mattercodex/internal-rpc-authority/restore-controller/database/dsn"),
		PostgresTLSServerName:    envOrDefault("INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME", "internal-rpc-authority-postgresql.mattercodex-system.svc.cluster.local"),
		PostgresExpectedUser:     strings.TrimSpace(os.Getenv("INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER")),
		DatabaseClusterID:        envOrDefault("INTERNAL_RPC_AUTHORITY_DATABASE_CLUSTER_ID", "internal-rpc-authority-primary"),
		ControllerGeneration:     generation,
		TargetRegistryFile:       envOrDefault("INTERNAL_RPC_AUTHORITY_TARGET_REGISTRY_FILE", "/var/run/config/mattercodex/internal-rpc-authority/restore-controller/key-delivery-targets.yaml"),
		ManifestRootJWKFile:      envOrDefault("INTERNAL_RPC_AUTHORITY_MANIFEST_ROOT_JWK_FILE", "/usr/local/share/internal-rpc-authority/manifest-root/bootstrap-public.jwk"),
		ManifestRootMetadataFile: envOrDefault("INTERNAL_RPC_AUTHORITY_MANIFEST_ROOT_METADATA_FILE", "/usr/local/share/internal-rpc-authority/manifest-root/bootstrap-metadata.json"),
		ManifestTrustBundleFile:  envOrDefault("INTERNAL_RPC_AUTHORITY_MANIFEST_TRUST_BUNDLE_FILE", "/var/run/config/mattercodex/internal-rpc-authority/restore-controller/manifest-trust.jws"),
		RestoreRoleTrustFile:     envOrDefault("INTERNAL_RPC_AUTHORITY_RESTORE_ROLE_TRUST_FILE", "/var/run/config/mattercodex/internal-rpc-authority/restore-controller/restore-role-trust.jws"),
		PublisherAddress:         envOrDefault("INTERNAL_RPC_AUTHORITY_PUBLISHER_ADDRESS", "internal-rpc-authority-publisher.mattercodex-system.svc:8444"),
		PublisherTLSServerName:   envOrDefault("INTERNAL_RPC_AUTHORITY_PUBLISHER_TLS_SERVER_NAME", "internal-rpc-authority-publisher.mattercodex-system.svc"),
		PublisherCAFile:          envOrDefault("INTERNAL_RPC_AUTHORITY_PUBLISHER_CA_FILE", "/var/run/config/mattercodex/internal-rpc-authority/restore-controller/publisher-ca.pem"),
		KubernetesAddress:        envOrDefault("INTERNAL_RPC_AUTHORITY_KUBERNETES_ADDRESS", "https://kubernetes.default.svc:443"),
		KubernetesTLSServerName:  envOrDefault("INTERNAL_RPC_AUTHORITY_KUBERNETES_TLS_SERVER_NAME", "kubernetes.default.svc"),
		KubernetesCAFile:         envOrDefault("INTERNAL_RPC_AUTHORITY_KUBERNETES_CA_FILE", "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"),
		KubernetesTokenFile:      envOrDefault("INTERNAL_RPC_AUTHORITY_KUBERNETES_TOKEN_FILE", "/var/run/secrets/tokens/kubernetes/token"),
		KubernetesNamespace:      "mattercodex-system",
		KubernetesResourceName:   "internal-rpc-authority-restore-coordination",
		ShutdownTimeout:          10 * time.Second,
	}
	if _, _, err := net.SplitHostPort(config.Listen); err != nil {
		return RestoreControllerConfig{}, errors.New(
			"restore controller listen address is invalid",
		)
	}
	if _, _, err := net.SplitHostPort(config.TechnicalListen); err != nil {
		return RestoreControllerConfig{}, errors.New(
			"restore controller technical listen address is invalid",
		)
	}
	if config.PostgresExpectedUser == "" ||
		config.DatabaseClusterID != "internal-rpc-authority-primary" {
		return RestoreControllerConfig{}, errors.New(
			"restore controller database identity is invalid",
		)
	}
	return config, nil
}

func RunRestoreController(
	lifecycle context.Context,
	shutdownBase context.Context,
	buildVersion string,
) error {
	config, err := LoadRestoreControllerConfig()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{}))
	startup, cancel := context.WithTimeout(lifecycle, 20*time.Second)
	defer cancel()
	pool, err := openRestorePostgres(startup, config)
	if err != nil {
		return err
	}
	fence, err := postgresrestore.New(pool)
	if err != nil {
		pool.Close()
		return err
	}
	coordination, err := kubernetesrestore.New(kubernetesrestore.Config{
		Address:       config.KubernetesAddress,
		TLSServerName: config.KubernetesTLSServerName,
		CAFile:        config.KubernetesCAFile,
		TokenFile:     config.KubernetesTokenFile,
		Namespace:     config.KubernetesNamespace,
		ResourceName:  config.KubernetesResourceName,
		Timeout:       5 * time.Second,
	})
	if err != nil {
		pool.Close()
		return err
	}
	publisherConnection, err := publisherclient.New(publisherclient.Config{
		Address:               config.PublisherAddress,
		TLSServerName:         config.PublisherTLSServerName,
		CACertificateFile:     config.PublisherCAFile,
		ClientCertificateFile: config.TLSCertificateFile,
		ClientPrivateKeyFile:  config.TLSPrivateKeyFile,
		Timeout:               5 * time.Second,
	})
	if err != nil {
		coordination.Close()
		pool.Close()
		return err
	}
	targetRegistry, err := publisher.LoadRegistry(config.TargetRegistryFile)
	if err != nil {
		_ = publisherConnection.Close()
		coordination.Close()
		pool.Close()
		return err
	}
	roleKeys, roleTrust, err := snapshot.LoadRestoreRoleTrust(
		snapshot.RestoreRoleTrustOptions{
			ManifestRootPublicJWKFile:  config.ManifestRootJWKFile,
			ManifestRootMetadataFile:   config.ManifestRootMetadataFile,
			ManifestTrustBundleJWSFile: config.ManifestTrustBundleFile,
			RestoreRoleTrustJWSFile:    config.RestoreRoleTrustFile,
		},
	)
	if err != nil {
		_ = publisherConnection.Close()
		coordination.Close()
		pool.Close()
		return err
	}
	controllerKey, err := loadControllerSigningKey(
		config.TLSCertificateFile,
		config.TLSPrivateKeyFile,
	)
	if err != nil {
		_ = publisherConnection.Close()
		coordination.Close()
		pool.Close()
		return err
	}
	domain, err := service.NewRestoreController(
		config.DatabaseClusterID,
		controllerKey,
		config.ControllerGeneration,
		targetRegistry,
		roleKeys,
		roleTrust,
		coordination,
		fence,
		publisherConnection,
	)
	if err != nil {
		_ = publisherConnection.Close()
		coordination.Close()
		pool.Close()
		return err
	}
	restoreApplication := application.NewRestoreController(domain)
	if err := restoreApplication.Recover(startup); err != nil {
		_ = publisherConnection.Close()
		coordination.Close()
		pool.Close()
		return err
	}
	serverTLS, err := loadMTLSServerConfig(
		config.TLSCertificateFile,
		config.TLSPrivateKeyFile,
		config.ClientCAFile,
	)
	if err != nil {
		_ = publisherConnection.Close()
		coordination.Close()
		pool.Close()
		return err
	}
	metrics := observability.NewMetrics(
		"internal_rpc_authority_restore_controller",
		buildVersion,
		map[string]string{
			internalrpcauthorityv1.RestoreControllerService_PrepareRestore_FullMethodName:        "prepare_restore",
			internalrpcauthorityv1.RestoreControllerService_GetRestoreDirective_FullMethodName:   "get_restore_directive",
			internalrpcauthorityv1.RestoreControllerService_AcknowledgeQuiescence_FullMethodName: "acknowledge_quiescence",
			internalrpcauthorityv1.RestoreControllerService_CompleteRestore_FullMethodName:       "complete_restore",
			internalrpcauthorityv1.RestoreControllerService_CheckReadiness_FullMethodName:        "check_readiness",
		},
	)
	readiness := serviceruntime.NewReadiness()
	readiness.Set(false, "starting")
	metrics.SetReady(false)
	grpcRuntime := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(serverTLS)),
		grpc.ForceServerCodec(grpcserver.StrictProtoCodec()),
		grpc.ChainUnaryInterceptor(
			metrics.UnaryServerInterceptor(),
			grpcserver.ErrorBoundary(grpcserver.ErrorObserverFunc(func(
				_ context.Context,
				method string,
				code codes.Code,
				_ error,
			) {
				logger.Error(
					"unexpected gRPC failure",
					"method", normalizeRestoreMethod(method),
					"code", code.String(),
				)
			})),
		),
	)
	internalrpcauthorityv1.RegisterRestoreControllerServiceServer(
		grpcRuntime,
		authoritygrpc.NewRestoreControllerServer(restoreApplication),
	)
	grpcListener, err := net.Listen("tcp", config.Listen)
	if err != nil {
		_ = publisherConnection.Close()
		coordination.Close()
		pool.Close()
		return errors.New("listen on restore controller endpoint")
	}
	technicalListener, err := net.Listen("tcp", config.TechnicalListen)
	if err != nil {
		_ = grpcListener.Close()
		_ = publisherConnection.Close()
		coordination.Close()
		pool.Close()
		return errors.New("listen on restore controller technical endpoint")
	}
	technicalServer := newRestoreTechnicalServer(
		readiness,
		metrics,
		restoreApplication,
	)
	serveErrors := make(chan error, 2)
	go func() {
		if serveErr := grpcRuntime.Serve(grpcListener); serveErr != nil {
			serveErrors <- errors.New("serve restore controller gRPC")
		}
	}()
	go func() {
		if serveErr := technicalServer.Serve(technicalListener); serveErr != nil &&
			!errors.Is(serveErr, http.ErrServerClosed) {
			serveErrors <- errors.New("serve restore controller technical HTTP")
		}
	}()
	readiness.Set(true, "ready")
	metrics.SetReady(true)
	logger.Info("restore controller started")
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
			Name:    "grpc-server",
			Timeout: config.ShutdownTimeout,
			Run:     func(ctx context.Context) error { return stopGRPC(ctx, grpcRuntime) },
		},
		serviceruntime.ShutdownOperation{
			Name:    "technical-http",
			Timeout: config.ShutdownTimeout,
			Run:     technicalServer.Shutdown,
		},
		serviceruntime.ShutdownOperation{
			Name:    "publisher-client",
			Timeout: config.ShutdownTimeout,
			Run:     func(context.Context) error { return publisherConnection.Close() },
		},
		serviceruntime.ShutdownOperation{
			Name:    "kubernetes-client",
			Timeout: config.ShutdownTimeout,
			Run: func(context.Context) error {
				coordination.Close()
				return nil
			},
		},
		serviceruntime.ShutdownOperation{
			Name:    "postgresql",
			Timeout: config.ShutdownTimeout,
			Run: func(context.Context) error {
				pool.Close()
				return nil
			},
		},
	)
	logger.Info("restore controller stopped")
	return errors.Join(runtimeErr, shutdownErr)
}

func openRestorePostgres(
	ctx context.Context,
	config RestoreControllerConfig,
) (*pgxpool.Pool, error) {
	raw, err := readPrivateFile(config.PostgresDSNFile, maxDSNFileBytes)
	if err != nil {
		return nil, errors.New("read restore controller PostgreSQL DSN file")
	}
	poolConfig, err := pgxpool.ParseConfig(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, errors.New("parse restore controller PostgreSQL DSN")
	}
	if len(poolConfig.ConnConfig.Fallbacks) != 0 ||
		poolConfig.ConnConfig.Host != config.PostgresTLSServerName ||
		poolConfig.ConnConfig.TLSConfig == nil ||
		poolConfig.ConnConfig.TLSConfig.RootCAs == nil ||
		poolConfig.ConnConfig.TLSConfig.ServerName != config.PostgresTLSServerName ||
		poolConfig.ConnConfig.TLSConfig.InsecureSkipVerify {
		return nil, errors.New("restore controller PostgreSQL TLS boundary rejected")
	}
	poolConfig.MaxConns = 4
	poolConfig.ConnConfig.RuntimeParams["application_name"] =
		"internal_rpc_authority_restore_controller"
	poolConfig.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		return sessionrepository.Configure(
			ctx,
			connection,
			config.PostgresExpectedUser,
			sessionrepository.CapabilityRestoreController,
		)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, errors.New("open restore controller PostgreSQL pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("verify restore controller PostgreSQL connectivity")
	}
	return pool, nil
}

func loadControllerSigningKey(
	certificateFile string,
	privateKeyFile string,
) (internalrpcauth.ES256Key, error) {
	pair, err := tls.LoadX509KeyPair(certificateFile, privateKeyFile)
	if err != nil || len(pair.Certificate) != 1 {
		return internalrpcauth.ES256Key{}, errors.New(
			"load restore controller signing certificate",
		)
	}
	certificate, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return internalrpcauth.ES256Key{}, errors.New(
			"parse restore controller signing certificate",
		)
	}
	privateKey, ok := pair.PrivateKey.(*ecdsa.PrivateKey)
	publicKey, publicOK := certificate.PublicKey.(*ecdsa.PublicKey)
	if !ok ||
		!publicOK ||
		privateKey.Curve != elliptic.P256() ||
		publicKey.Curve != elliptic.P256() ||
		privateKey.PublicKey.X.Cmp(publicKey.X) != 0 ||
		privateKey.PublicKey.Y.Cmp(publicKey.Y) != 0 {
		return internalrpcauth.ES256Key{}, errors.New(
			"restore controller TLS key is not exact ES256",
		)
	}
	digest := sha256.Sum256(certificate.Raw)
	return internalrpcauth.ES256Key{
		KeyID:   "controller-tls-" + hex.EncodeToString(digest[:12]),
		Public:  publicKey,
		Private: privateKey,
	}, nil
}

func newRestoreTechnicalServer(
	readiness *serviceruntime.Readiness,
	metrics *observability.Metrics,
	restoreApplication *application.RestoreController,
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
		ctx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
		defer cancel()
		if _, err := restoreApplication.Ready(ctx); err != nil {
			metrics.SetReady(false)
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
		metrics.SetReady(true)
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

func normalizeRestoreMethod(method string) string {
	switch method {
	case internalrpcauthorityv1.RestoreControllerService_PrepareRestore_FullMethodName:
		return "prepare_restore"
	case internalrpcauthorityv1.RestoreControllerService_GetRestoreDirective_FullMethodName:
		return "get_restore_directive"
	case internalrpcauthorityv1.RestoreControllerService_AcknowledgeQuiescence_FullMethodName:
		return "acknowledge_quiescence"
	case internalrpcauthorityv1.RestoreControllerService_CompleteRestore_FullMethodName:
		return "complete_restore"
	case internalrpcauthorityv1.RestoreControllerService_CheckReadiness_FullMethodName:
		return "check_readiness"
	default:
		return "unknown"
	}
}
