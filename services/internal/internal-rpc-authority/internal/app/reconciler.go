package app

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/grpcserver"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	vaultclient "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/client/vault"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
	credentialrollout "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/kubernetes/credentialrollout"
	credentialrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/postgres/credentiallifecycle"
	sessionrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/postgres/session"
	authoritygrpc "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/transport/grpc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

var (
	digestPattern      = regexp.MustCompile(`^[a-f0-9]{64}$`)
	runtimeUUIDPattern = regexp.MustCompile(
		`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
	)
)

// ReconcilerConfig задаёт жизненный цикл учётных данных PostgreSQL и Vault.
type ReconcilerConfig struct {
	HolderID                     string        `env:"POD_UID"`
	Listen                       string        `env:"INTERNAL_RPC_AUTHORITY_RECONCILER_LISTEN"`
	TechnicalListen              string        `env:"INTERNAL_RPC_AUTHORITY_TECHNICAL_LISTEN"`
	AllowedCallerSPIFFEID        string        `env:"INTERNAL_RPC_AUTHORITY_RECONCILER_CALLER_SPIFFE_ID"`
	TLSCertificateFile           string        `env:"INTERNAL_RPC_AUTHORITY_TLS_CERTIFICATE_FILE"`
	TLSPrivateKeyFile            string        `env:"INTERNAL_RPC_AUTHORITY_TLS_PRIVATE_KEY_FILE"`
	ClientCAFile                 string        `env:"INTERNAL_RPC_AUTHORITY_CLIENT_CA_FILE"`
	PostgresDSNFile              string        `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE"`
	PostgresTLSServerName        string        `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME"`
	PostgresExpectedSessionUser  string        `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER"`
	VaultAddress                 string        `env:"INTERNAL_RPC_AUTHORITY_VAULT_ADDRESS"`
	VaultTLSServerName           string        `env:"INTERNAL_RPC_AUTHORITY_VAULT_TLS_SERVER_NAME"`
	VaultCAFile                  string        `env:"INTERNAL_RPC_AUTHORITY_VAULT_CA_FILE"`
	VaultAuthRole                string        `env:"INTERNAL_RPC_AUTHORITY_VAULT_AUTH_ROLE"`
	VaultServiceAccountTokenFile string        `env:"INTERNAL_RPC_AUTHORITY_VAULT_AUTH_FILE"`
	KubernetesAPIAddress         string        `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_API_ADDRESS"`
	KubernetesAPITLSServerName   string        `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_API_TLS_SERVER_NAME"`
	KubernetesAPICAFile          string        `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_API_CA_FILE"`
	KubernetesAPITokenFile       string        `env:"INTERNAL_RPC_AUTHORITY_KUBERNETES_API_TOKEN_FILE"`
	SourceRevision               uint64        `env:"INTERNAL_RPC_AUTHORITY_REGISTERED_SET_SOURCE_REVISION"`
	SourceDigest                 string        `env:"INTERNAL_RPC_AUTHORITY_REGISTERED_SET_SOURCE_DIGEST_SHA256"`
	LeaseDuration                time.Duration `env:"INTERNAL_RPC_AUTHORITY_LEASE_DURATION"`
	ReconcileInterval            time.Duration `env:"INTERNAL_RPC_AUTHORITY_RECONCILE_INTERVAL"`
	ShutdownTimeout              time.Duration `env:"INTERNAL_RPC_AUTHORITY_SHUTDOWN_TIMEOUT"`
}

// LoadReconcilerConfig читает и проверяет окружение согласователя.
func LoadReconcilerConfig() (ReconcilerConfig, error) {
	config := ReconcilerConfig{
		Listen:                       ":8443",
		TechnicalListen:              ":9090",
		TLSCertificateFile:           "/var/run/secrets/mattercodex/internal-rpc-authority/tls/tls.crt",
		TLSPrivateKeyFile:            "/var/run/secrets/mattercodex/internal-rpc-authority/tls/tls.key",
		ClientCAFile:                 "/var/run/config/mattercodex/internal-rpc-authority/tls/client-ca.pem",
		PostgresDSNFile:              "/var/run/secrets/mattercodex/internal-rpc-authority/postgres/dsn",
		PostgresTLSServerName:        "internal-rpc-authority-postgresql.mattercodex-system.svc.cluster.local",
		VaultAddress:                 "https://vault.mattercodex-system.svc:8200",
		VaultTLSServerName:           "vault.mattercodex-system.svc.cluster.local",
		VaultCAFile:                  "/var/run/config/mattercodex/internal-rpc-authority/vault/ca.pem",
		VaultAuthRole:                "internal-rpc-authority-database-credential-reconciler",
		VaultServiceAccountTokenFile: "/var/run/secrets/tokens/vault/token",
		KubernetesAPIAddress:         "https://kubernetes.default.svc:443",
		KubernetesAPITLSServerName:   "kubernetes.default.svc",
		KubernetesAPICAFile:          "/var/run/config/kubernetes.io/serviceaccount/ca.crt",
		KubernetesAPITokenFile:       "/var/run/secrets/tokens/kubernetes-api/token",
		LeaseDuration:                20 * time.Second,
		ReconcileInterval:            10 * time.Second,
		ShutdownTimeout:              10 * time.Second,
	}
	if err := parseEnvironment(&config); err != nil {
		return ReconcilerConfig{}, err
	}
	if !runtimeUUIDPattern.MatchString(config.HolderID) ||
		config.AllowedCallerSPIFFEID == "" ||
		config.SourceRevision == 0 ||
		!digestPattern.MatchString(config.SourceDigest) ||
		config.PostgresExpectedSessionUser == "" {
		return ReconcilerConfig{}, errors.New("database credential reconciler identity or registry is invalid")
	}
	if _, _, err := net.SplitHostPort(config.Listen); err != nil {
		return ReconcilerConfig{}, errors.New("database credential reconciler listen address is invalid")
	}
	if _, _, err := net.SplitHostPort(config.TechnicalListen); err != nil {
		return ReconcilerConfig{}, errors.New("database credential technical listen address is invalid")
	}
	return config, nil
}

// RunDatabaseCredentialReconciler запускает продвижение и отзыв поколений.
func RunDatabaseCredentialReconciler(
	lifecycle context.Context,
	shutdownBase context.Context,
	buildVersion string,
) error {
	config, err := LoadReconcilerConfig()
	if err != nil {
		return err
	}
	telemetry, logger, err := startTelemetry(
		lifecycle,
		"internal_rpc_authority_database_credential_reconciler",
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
	pool, err := openCredentialPostgres(lifecycle, config)
	if err != nil {
		return err
	}
	store, err := credentialrepository.New(pool)
	if err != nil {
		pool.Close()
		return err
	}
	vault, err := vaultclient.NewStaticRoleClient(vaultclient.Config{
		Address:                 config.VaultAddress,
		TLSServerName:           config.VaultTLSServerName,
		CAFile:                  config.VaultCAFile,
		AuthMount:               "kubernetes",
		AuthRole:                config.VaultAuthRole,
		ServiceAccountTokenFile: config.VaultServiceAccountTokenFile,
		Timeout:                 5 * time.Second,
	})
	if err != nil {
		pool.Close()
		return err
	}
	rollout, err := credentialrollout.New(credentialrollout.Config{
		Address:       config.KubernetesAPIAddress,
		TLSServerName: config.KubernetesAPITLSServerName,
		CAFile:        config.KubernetesAPICAFile,
		TokenFile:     config.KubernetesAPITokenFile,
		Namespace:     "mattercodex-system",
		Deployments: []string{
			"internal-rpc-authority-publisher",
			"internal-rpc-authority-readback-attestor",
		},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		vault.Close()
		pool.Close()
		return err
	}
	baseline, registered := credentialRegisteredSets(
		config.SourceRevision,
		config.SourceDigest,
	)
	credentialService, err := service.NewDatabaseCredentialLifecycle(
		config.HolderID,
		config.LeaseDuration,
		baseline,
		registered,
		store,
		vault,
		rollout,
	)
	if err != nil {
		rollout.Close()
		vault.Close()
		pool.Close()
		return err
	}
	serverTLS, err := loadReconcilerServerTLS(config)
	if err != nil {
		pool.Close()
		return err
	}
	methods := map[string]string{
		internalrpcauthorityv1.DatabaseCredentialLifecycleService_ReconcileDatabaseCredentials_FullMethodName: "reconcile_database_credentials",
		internalrpcauthorityv1.DatabaseCredentialLifecycleService_CheckReadiness_FullMethodName:               "check_readiness",
	}
	metrics := observability.NewMetrics(
		"internal_rpc_authority_database_credential_reconciler",
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
			requireExactMTLSPeer(config.AllowedCallerSPIFFEID),
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
					"method", normalizeReconcilerMethod(method),
					"code", code.String(),
				)
			})),
		),
	)
	internalrpcauthorityv1.RegisterDatabaseCredentialLifecycleServiceServer(
		grpcRuntime,
		authoritygrpc.NewDatabaseCredentialLifecycleServer(credentialService),
	)
	grpcListener, err := net.Listen("tcp", config.Listen)
	if err != nil {
		pool.Close()
		return errors.New("listen on database credential reconciler endpoint")
	}
	technicalListener, err := net.Listen("tcp", config.TechnicalListen)
	if err != nil {
		_ = grpcListener.Close()
		pool.Close()
		return errors.New("listen on database credential technical endpoint")
	}
	technicalServer := newCredentialTechnicalServer(
		readiness,
		metrics,
		credentialService,
	)
	workers := serviceruntime.StartWorkers(lifecycle, func(ctx context.Context) error {
		return runCredentialReconciliation(ctx, config, credentialService, readiness, metrics)
	})
	serveErrors := make(chan error, 2)
	go func() {
		if serveErr := grpcRuntime.Serve(grpcListener); serveErr != nil {
			serveErrors <- errors.New("serve database credential gRPC")
		}
	}()
	go func() {
		if serveErr := technicalServer.Serve(technicalListener); serveErr != nil &&
			!errors.Is(serveErr, http.ErrServerClosed) {
			serveErrors <- errors.New("serve database credential technical HTTP")
		}
	}()
	logger.Info("database credential reconciler started")
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
			Name:    "kubernetes-api",
			Timeout: config.ShutdownTimeout,
			Run: func(context.Context) error {
				rollout.Close()
				return nil
			},
		},
		serviceruntime.ShutdownOperation{
			Name:    "vault-http",
			Timeout: config.ShutdownTimeout,
			Run: func(context.Context) error {
				vault.Close()
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
		serviceruntime.ShutdownOperation{
			Name:    "otel-tracing",
			Timeout: config.ShutdownTimeout,
			Run:     telemetry.shutdownTracing,
		},
		serviceruntime.ShutdownOperation{
			Name:    "sentry-flush",
			Timeout: config.ShutdownTimeout,
			Run:     telemetry.flushSentry,
		},
	)
	telemetryFinished = true
	logger.Info("database credential reconciler stopped")
	return errors.Join(runtimeErr, shutdownErr)
}

func openCredentialPostgres(
	ctx context.Context,
	config ReconcilerConfig,
) (*pgxpool.Pool, error) {
	raw, err := readPrivateFile(config.PostgresDSNFile, maxDSNFileBytes)
	if err != nil {
		return nil, errors.New("read database credential PostgreSQL DSN file")
	}
	poolConfig, err := pgxpool.ParseConfig(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, errors.New("parse database credential PostgreSQL DSN")
	}
	instrumentPGX(
		poolConfig,
		"internal_rpc_authority_database_credential_reconciler",
	)
	if len(poolConfig.ConnConfig.Fallbacks) != 0 ||
		poolConfig.ConnConfig.Host != config.PostgresTLSServerName ||
		poolConfig.ConnConfig.TLSConfig == nil ||
		poolConfig.ConnConfig.TLSConfig.RootCAs == nil ||
		poolConfig.ConnConfig.TLSConfig.ServerName != config.PostgresTLSServerName ||
		poolConfig.ConnConfig.TLSConfig.InsecureSkipVerify {
		return nil, errors.New("database credential PostgreSQL TLS boundary rejected")
	}
	poolConfig.MaxConns = 4
	poolConfig.ConnConfig.RuntimeParams["application_name"] =
		"internal_rpc_authority_database_credential_reconciler"
	poolConfig.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		return sessionrepository.Configure(
			ctx,
			connection,
			config.PostgresExpectedSessionUser,
			sessionrepository.CapabilityDatabaseCredentialReconciler,
		)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, errors.New("open database credential PostgreSQL pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("verify database credential PostgreSQL connectivity")
	}
	return pool, nil
}

func credentialRegisteredSets(
	sourceRevision uint64,
	sourceDigest string,
) (model.DatabaseCredentialRegisteredSet, model.DatabaseCredentialRegisteredSet) {
	generation := func(
		capability model.DatabaseCredentialCapability,
		number uint64,
		status model.DatabaseCredentialStatus,
		principal string,
		vaultRole string,
	) model.DatabaseCredentialGeneration {
		return model.DatabaseCredentialGeneration{
			Capability:      capability,
			Generation:      number,
			Status:          status,
			Principal:       principal,
			VaultStaticRole: vaultRole,
			SourceRevision:  sourceRevision,
			SourceDigest:    sourceDigest,
		}
	}
	baseline := model.DatabaseCredentialRegisteredSet{
		Version:        model.ContractVersion,
		SourceRevision: sourceRevision,
		SourceDigest:   sourceDigest,
		Generations: []model.DatabaseCredentialGeneration{
			generation(
				model.DatabaseCredentialPublisher,
				1,
				model.DatabaseCredentialRetired,
				"ira_publisher_g1",
				"internal-rpc-authority-publisher-g1",
			),
			generation(
				model.DatabaseCredentialPublisher,
				2,
				model.DatabaseCredentialPrevious,
				"ira_publisher_g2",
				"internal-rpc-authority-publisher-g2",
			),
			generation(
				model.DatabaseCredentialPublisher,
				3,
				model.DatabaseCredentialCurrent,
				"ira_publisher_g3",
				"internal-rpc-authority-publisher-g3",
			),
			generation(
				model.DatabaseCredentialPublisher,
				4,
				model.DatabaseCredentialNext,
				"ira_publisher_g4",
				"internal-rpc-authority-publisher-g4",
			),
			generation(
				model.DatabaseCredentialAttestor,
				1,
				model.DatabaseCredentialRetired,
				"ira_readback_attestor_g1",
				"internal-rpc-authority-readback-attestor-g1",
			),
			generation(
				model.DatabaseCredentialAttestor,
				2,
				model.DatabaseCredentialPrevious,
				"ira_readback_attestor_g2",
				"internal-rpc-authority-readback-attestor-g2",
			),
			generation(
				model.DatabaseCredentialAttestor,
				3,
				model.DatabaseCredentialCurrent,
				"ira_readback_attestor_g3",
				"internal-rpc-authority-readback-attestor-g3",
			),
			generation(
				model.DatabaseCredentialAttestor,
				4,
				model.DatabaseCredentialNext,
				"ira_readback_attestor_g4",
				"internal-rpc-authority-readback-attestor-g4",
			),
		},
	}
	target := model.DatabaseCredentialRegisteredSet{
		Version:        model.ContractVersion,
		SourceRevision: sourceRevision,
		SourceDigest:   sourceDigest,
		Generations: []model.DatabaseCredentialGeneration{
			generation(model.DatabaseCredentialPublisher, 1, model.DatabaseCredentialRetired, "ira_publisher_g1", "internal-rpc-authority-publisher-g1"),
			generation(model.DatabaseCredentialPublisher, 2, model.DatabaseCredentialRetired, "ira_publisher_g2", "internal-rpc-authority-publisher-g2"),
			generation(model.DatabaseCredentialPublisher, 3, model.DatabaseCredentialPrevious, "ira_publisher_g3", "internal-rpc-authority-publisher-g3"),
			generation(model.DatabaseCredentialPublisher, 4, model.DatabaseCredentialCurrent, "ira_publisher_g4", "internal-rpc-authority-publisher-g4"),
			generation(model.DatabaseCredentialPublisher, 5, model.DatabaseCredentialNext, "ira_publisher_g5", "internal-rpc-authority-publisher-g5"),
			generation(model.DatabaseCredentialAttestor, 1, model.DatabaseCredentialRetired, "ira_readback_attestor_g1", "internal-rpc-authority-readback-attestor-g1"),
			generation(model.DatabaseCredentialAttestor, 2, model.DatabaseCredentialRetired, "ira_readback_attestor_g2", "internal-rpc-authority-readback-attestor-g2"),
			generation(model.DatabaseCredentialAttestor, 3, model.DatabaseCredentialPrevious, "ira_readback_attestor_g3", "internal-rpc-authority-readback-attestor-g3"),
			generation(model.DatabaseCredentialAttestor, 4, model.DatabaseCredentialCurrent, "ira_readback_attestor_g4", "internal-rpc-authority-readback-attestor-g4"),
			generation(model.DatabaseCredentialAttestor, 5, model.DatabaseCredentialNext, "ira_readback_attestor_g5", "internal-rpc-authority-readback-attestor-g5"),
		},
	}
	return baseline, target
}

func runCredentialReconciliation(
	ctx context.Context,
	config ReconcilerConfig,
	credentialService *service.DatabaseCredentialLifecycle,
	readiness *serviceruntime.Readiness,
	metrics *observability.Metrics,
) error {
	ticker := time.NewTicker(config.ReconcileInterval)
	defer ticker.Stop()
	for {
		idempotencyKey := deterministicLifecycleRequestID(config.SourceDigest)
		_, err := credentialService.Reconcile(ctx, idempotencyKey)
		if err != nil {
			_, err = credentialService.Ready(ctx)
		}
		if err == nil {
			readiness.Set(true, "ready")
			metrics.SetReady(true)
		} else {
			readiness.Set(false, "reconciliation-failed")
			metrics.SetReady(false)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func deterministicLifecycleRequestID(sourceDigest string) string {
	digest := sha256.Sum256([]byte("database-credential-lifecycle:" + sourceDigest))
	value := digest[:16]
	value[6] = (value[6] & 0x0f) | 0x50
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" +
		encoded[16:20] + "-" + encoded[20:32]
}

func loadReconcilerServerTLS(config ReconcilerConfig) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(
		config.TLSCertificateFile,
		config.TLSPrivateKeyFile,
	)
	if err != nil {
		return nil, errors.New("load database credential server certificate")
	}
	caRaw, err := os.ReadFile(config.ClientCAFile)
	if err != nil {
		return nil, errors.New("read database credential client CA")
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("database credential client CA is invalid")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
	}, nil
}

func requireExactMTLSPeer(expectedSPIFFEID string) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		peerValue, ok := peer.FromContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "mTLS client identity required")
		}
		tlsInfo, ok := peerValue.AuthInfo.(credentials.TLSInfo)
		if !ok || len(tlsInfo.State.VerifiedChains) == 0 ||
			len(tlsInfo.State.PeerCertificates) == 0 {
			return nil, status.Error(codes.Unauthenticated, "mTLS client identity required")
		}
		uris := tlsInfo.State.PeerCertificates[0].URIs
		if len(uris) != 1 || uris[0].String() != expectedSPIFFEID {
			return nil, status.Error(codes.PermissionDenied, "mTLS client identity rejected")
		}
		return handler(ctx, request)
	}
}

func newCredentialTechnicalServer(
	readiness *serviceruntime.Readiness,
	metrics *observability.Metrics,
	credentialService *service.DatabaseCredentialLifecycle,
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
		ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
		defer cancel()
		if _, err := credentialService.Ready(ctx); err != nil {
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

func normalizeReconcilerMethod(method string) string {
	switch method {
	case internalrpcauthorityv1.DatabaseCredentialLifecycleService_ReconcileDatabaseCredentials_FullMethodName:
		return "reconcile_database_credentials"
	case internalrpcauthorityv1.DatabaseCredentialLifecycleService_CheckReadiness_FullMethodName:
		return "check_readiness"
	default:
		return "unknown"
	}
}
