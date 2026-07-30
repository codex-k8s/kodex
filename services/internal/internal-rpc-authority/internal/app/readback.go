package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/grpcserver"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/application"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
	readbackrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/postgres/readback"
	sessionrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/postgres/session"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/snapshot"
	authoritygrpc "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/transport/grpc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
)

type ReadbackConfig struct {
	Listen                 string
	TechnicalListen        string
	TLSCertificateFile     string
	TLSPrivateKeyFile      string
	ClientCAFile           string
	PostgresDSNFile        string
	PostgresTLSServerName  string
	PostgresExpectedUser   string
	RootPublicJWKFile      string
	RootMetadataFile       string
	ManifestBundleJWSFile  string
	CredentialTrustJWSFile string
	VerifierGeneration     uint64
	ShutdownTimeout        time.Duration
}

func LoadReadbackConfig() (ReadbackConfig, error) {
	verifierGeneration, err := strconv.ParseUint(
		strings.TrimSpace(os.Getenv("INTERNAL_RPC_AUTHORITY_READBACK_VERIFIER_GENERATION")),
		10,
		64,
	)
	if err != nil || verifierGeneration == 0 {
		return ReadbackConfig{}, errors.New("readback verifier generation is invalid")
	}
	config := ReadbackConfig{
		Listen:                 envOrDefault("INTERNAL_RPC_AUTHORITY_READBACK_LISTEN", ":8443"),
		TechnicalListen:        envOrDefault("INTERNAL_RPC_AUTHORITY_TECHNICAL_LISTEN", ":9090"),
		TLSCertificateFile:     envOrDefault("INTERNAL_RPC_AUTHORITY_TLS_CERTIFICATE_FILE", "/var/run/secrets/mattercodex/internal-rpc-authority/readback-attestor/tls/tls.crt"),
		TLSPrivateKeyFile:      envOrDefault("INTERNAL_RPC_AUTHORITY_TLS_PRIVATE_KEY_FILE", "/var/run/secrets/mattercodex/internal-rpc-authority/readback-attestor/tls/tls.key"),
		ClientCAFile:           envOrDefault("INTERNAL_RPC_AUTHORITY_CLIENT_CA_FILE", "/var/run/config/mattercodex/internal-rpc-authority/readback-attestor/client-ca.pem"),
		PostgresDSNFile:        envOrDefault("INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE", "/var/run/secrets/mattercodex/internal-rpc-authority/readback-attestor/database/dsn"),
		PostgresTLSServerName:  envOrDefault("INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME", "internal-rpc-authority-postgresql.mattercodex-system.svc.cluster.local"),
		PostgresExpectedUser:   strings.TrimSpace(os.Getenv("INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER")),
		RootPublicJWKFile:      envOrDefault("INTERNAL_RPC_AUTHORITY_READBACK_ROOT_PUBLIC_JWK_FILE", "/usr/local/share/internal-rpc-authority/readback-root/bootstrap-public.jwk"),
		RootMetadataFile:       envOrDefault("INTERNAL_RPC_AUTHORITY_READBACK_ROOT_METADATA_FILE", "/usr/local/share/internal-rpc-authority/readback-root/bootstrap-metadata.json"),
		ManifestBundleJWSFile:  envOrDefault("INTERNAL_RPC_AUTHORITY_READBACK_MANIFEST_BUNDLE_JWS_FILE", "/var/run/config/mattercodex/internal-rpc-authority/readback/manifest-root/root.jws"),
		CredentialTrustJWSFile: envOrDefault("INTERNAL_RPC_AUTHORITY_READBACK_CREDENTIAL_TRUST_JWS_FILE", "/var/run/config/mattercodex/internal-rpc-authority/readback/credential-trust/trust.jws"),
		VerifierGeneration:     verifierGeneration,
		ShutdownTimeout:        10 * time.Second,
	}
	if _, _, err := net.SplitHostPort(config.Listen); err != nil {
		return ReadbackConfig{}, errors.New("readback listen address is invalid")
	}
	if _, _, err := net.SplitHostPort(config.TechnicalListen); err != nil {
		return ReadbackConfig{}, errors.New("readback technical listen address is invalid")
	}
	if config.PostgresExpectedUser == "" ||
		config.PostgresTLSServerName == "" {
		return ReadbackConfig{}, errors.New("readback PostgreSQL identity is invalid")
	}
	return config, nil
}

func RunReadbackAttestor(
	lifecycle context.Context,
	shutdownBase context.Context,
	buildVersion string,
) error {
	config, err := LoadReadbackConfig()
	if err != nil {
		return err
	}
	telemetry, logger, err := startTelemetry(
		lifecycle,
		"internal_rpc_authority_readback_attestor",
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
	pool, err := openReadbackPostgres(startup, config)
	if err != nil {
		return err
	}
	store, err := readbackrepository.New(pool)
	if err != nil {
		pool.Close()
		return err
	}
	trust, trustMetadata, err := snapshot.LoadReadbackTrust(snapshot.ReadbackTrustOptions{
		RootPublicJWKFile:      config.RootPublicJWKFile,
		RootMetadataFile:       config.RootMetadataFile,
		ManifestBundleJWSFile:  config.ManifestBundleJWSFile,
		CredentialTrustJWSFile: config.CredentialTrustJWSFile,
		Now:                    time.Now(),
	})
	if err != nil {
		pool.Close()
		return fmt.Errorf("load independent readback trust: %w", err)
	}
	domainService, err := service.NewReadbackAttestor(
		trust,
		store,
		config.VerifierGeneration,
	)
	if err != nil {
		pool.Close()
		return err
	}
	readbackApplication := application.NewReadbackAttestor(domainService)
	if err := readbackApplication.Ready(startup); err != nil {
		pool.Close()
		return fmt.Errorf("verify readback startup path: %w", err)
	}
	serverTLS, err := loadMTLSServerConfig(
		config.TLSCertificateFile,
		config.TLSPrivateKeyFile,
		config.ClientCAFile,
	)
	if err != nil {
		pool.Close()
		return err
	}
	methods := map[string]string{
		internalrpcauthorityv1.AuthorityReadbackAttestorService_IssueAttestationChallenge_FullMethodName: "issue_attestation_challenge",
		internalrpcauthorityv1.AuthorityReadbackAttestorService_AttestServedState_FullMethodName:         "attest_served_state",
		internalrpcauthorityv1.AuthorityReadbackAttestorService_CheckReadiness_FullMethodName:            "check_readiness",
	}
	metrics := observability.NewMetrics(
		"internal_rpc_authority_readback_attestor",
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
					"method", normalizeReadbackMethod(method),
					"code", code.String(),
				)
			})),
		),
	)
	internalrpcauthorityv1.RegisterAuthorityReadbackAttestorServiceServer(
		grpcRuntime,
		authoritygrpc.NewAuthorityReadbackAttestorServer(
			readbackApplication,
			trustMetadata,
			config.VerifierGeneration,
		),
	)
	grpcListener, err := net.Listen("tcp", config.Listen)
	if err != nil {
		pool.Close()
		return errors.New("listen on readback attestor endpoint")
	}
	technicalListener, err := net.Listen("tcp", config.TechnicalListen)
	if err != nil {
		_ = grpcListener.Close()
		pool.Close()
		return errors.New("listen on readback technical endpoint")
	}
	technicalServer := newReadbackTechnicalServer(
		readiness,
		metrics,
		readbackApplication,
	)
	serveErrors := make(chan error, 2)
	go func() {
		if serveErr := grpcRuntime.Serve(grpcListener); serveErr != nil {
			serveErrors <- errors.New("serve readback gRPC")
		}
	}()
	go func() {
		if serveErr := technicalServer.Serve(technicalListener); serveErr != nil &&
			!errors.Is(serveErr, http.ErrServerClosed) {
			serveErrors <- errors.New("serve readback technical HTTP")
		}
	}()
	readiness.Set(true, "ready")
	metrics.SetReady(true)
	logger.Info("readback attestor started")
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
			Name: "grpc-server", Timeout: config.ShutdownTimeout,
			Run: func(ctx context.Context) error { return stopGRPC(ctx, grpcRuntime) },
		},
		serviceruntime.ShutdownOperation{
			Name: "technical-http", Timeout: config.ShutdownTimeout,
			Run: technicalServer.Shutdown,
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
	logger.Info("readback attestor stopped")
	return errors.Join(runtimeErr, shutdownErr)
}

func openReadbackPostgres(
	ctx context.Context,
	config ReadbackConfig,
) (*pgxpool.Pool, error) {
	raw, err := readPrivateFile(config.PostgresDSNFile, maxDSNFileBytes)
	if err != nil {
		return nil, errors.New("read readback PostgreSQL DSN file")
	}
	poolConfig, err := pgxpool.ParseConfig(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, errors.New("parse readback PostgreSQL DSN")
	}
	instrumentPGX(poolConfig, "internal_rpc_authority_readback_attestor")
	if len(poolConfig.ConnConfig.Fallbacks) != 0 ||
		poolConfig.ConnConfig.Host != config.PostgresTLSServerName ||
		poolConfig.ConnConfig.TLSConfig == nil ||
		poolConfig.ConnConfig.TLSConfig.RootCAs == nil ||
		poolConfig.ConnConfig.TLSConfig.ServerName != config.PostgresTLSServerName ||
		poolConfig.ConnConfig.TLSConfig.InsecureSkipVerify {
		return nil, errors.New("readback PostgreSQL TLS boundary rejected")
	}
	poolConfig.MaxConns = 8
	poolConfig.ConnConfig.RuntimeParams["application_name"] =
		"internal_rpc_authority_readback_attestor"
	poolConfig.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		return sessionrepository.Configure(
			ctx,
			connection,
			config.PostgresExpectedUser,
			sessionrepository.CapabilityReadbackAttestor,
		)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, errors.New("open readback PostgreSQL pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("verify readback PostgreSQL connectivity")
	}
	return pool, nil
}

func loadMTLSServerConfig(certificateFile, privateKeyFile, clientCAFile string) (
	*tls.Config,
	error,
) {
	certificate, err := tls.LoadX509KeyPair(certificateFile, privateKeyFile)
	if err != nil {
		return nil, errors.New("load mTLS server certificate")
	}
	caRaw, err := os.ReadFile(clientCAFile)
	if err != nil {
		return nil, errors.New("read mTLS client CA")
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("mTLS client CA is invalid")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
	}, nil
}

func newReadbackTechnicalServer(
	readiness *serviceruntime.Readiness,
	metrics *observability.Metrics,
	readbackApplication *application.ReadbackAttestor,
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
		if err := readbackApplication.Ready(ctx); err != nil {
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

func normalizeReadbackMethod(method string) string {
	switch method {
	case internalrpcauthorityv1.AuthorityReadbackAttestorService_IssueAttestationChallenge_FullMethodName:
		return "issue_attestation_challenge"
	case internalrpcauthorityv1.AuthorityReadbackAttestorService_AttestServedState_FullMethodName:
		return "attest_served_state"
	case internalrpcauthorityv1.AuthorityReadbackAttestorService_CheckReadiness_FullMethodName:
		return "check_readiness"
	default:
		return "unknown"
	}
}
