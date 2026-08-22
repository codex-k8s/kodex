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
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/grpcserver"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/application"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
	readbackrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/postgres/readback"
	sessionrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/postgres/session"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/snapshot"
	authoritygrpc "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/transport/grpc"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
)

// ReadbackConfig задаёт независимый сервис проверки обслуживаемого снимка.
type ReadbackConfig struct {
	Listen                 string        `env:"INTERNAL_RPC_AUTHORITY_READBACK_LISTEN"`
	TechnicalListen        string        `env:"INTERNAL_RPC_AUTHORITY_TECHNICAL_LISTEN"`
	TLSCertificateFile     string        `env:"INTERNAL_RPC_AUTHORITY_TLS_CERTIFICATE_FILE"`
	TLSPrivateKeyFile      string        `env:"INTERNAL_RPC_AUTHORITY_TLS_PRIVATE_KEY_FILE"`
	ClientCAFile           string        `env:"INTERNAL_RPC_AUTHORITY_CLIENT_CA_FILE"`
	PostgresDSNFile        string        `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE"`
	PostgresTLSServerName  string        `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME"`
	PostgresExpectedUser   string        `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER"`
	PodUID                 string        `env:"POD_UID"`
	RootPublicJWKFile      string        `env:"INTERNAL_RPC_AUTHORITY_READBACK_ROOT_PUBLIC_JWK_FILE"`
	RootMetadataFile       string        `env:"INTERNAL_RPC_AUTHORITY_READBACK_ROOT_METADATA_FILE"`
	ManifestBundleJWSFile  string        `env:"INTERNAL_RPC_AUTHORITY_READBACK_MANIFEST_BUNDLE_JWS_FILE"`
	CredentialTrustJWSFile string        `env:"INTERNAL_RPC_AUTHORITY_READBACK_CREDENTIAL_TRUST_JWS_FILE"`
	VerifierGeneration     uint64        `env:"INTERNAL_RPC_AUTHORITY_READBACK_VERIFIER_GENERATION"`
	ShutdownTimeout        time.Duration `env:"INTERNAL_RPC_AUTHORITY_SHUTDOWN_TIMEOUT"`
}

// LoadReadbackConfig читает и проверяет окружение сервиса проверки.
func LoadReadbackConfig() (ReadbackConfig, error) {
	config := ReadbackConfig{
		Listen:                 ":8443",
		TechnicalListen:        ":9090",
		TLSCertificateFile:     "/var/run/secrets/mattercodex/internal-rpc-authority/readback-attestor/tls/tls.crt",
		TLSPrivateKeyFile:      "/var/run/secrets/mattercodex/internal-rpc-authority/readback-attestor/tls/tls.key",
		ClientCAFile:           "/var/run/config/mattercodex/internal-rpc-authority/readback-attestor/client-ca.pem",
		PostgresDSNFile:        "/var/run/secrets/mattercodex/internal-rpc-authority/readback-attestor/database/dsn",
		PostgresTLSServerName:  "internal-rpc-authority-postgresql-rw.mattercodex-system.svc.cluster.local",
		RootPublicJWKFile:      "/usr/local/share/internal-rpc-authority/readback-root/bootstrap-public.jwk",
		RootMetadataFile:       "/usr/local/share/internal-rpc-authority/readback-root/bootstrap-metadata.json",
		ManifestBundleJWSFile:  "/var/run/config/mattercodex/internal-rpc-authority/readback/manifest-root/root.jws",
		CredentialTrustJWSFile: "/var/run/config/mattercodex/internal-rpc-authority/readback/credential-trust/trust.jws",
		ShutdownTimeout:        10 * time.Second,
	}
	if err := parseEnvironment(&config); err != nil {
		return ReadbackConfig{}, err
	}
	if _, _, err := net.SplitHostPort(config.Listen); err != nil {
		return ReadbackConfig{}, errors.New("readback listen address is invalid")
	}
	if _, _, err := net.SplitHostPort(config.TechnicalListen); err != nil {
		return ReadbackConfig{}, errors.New("readback technical listen address is invalid")
	}
	if !runtimeUUIDPattern.MatchString(config.PodUID) ||
		config.PostgresTLSServerName == "" ||
		config.VerifierGeneration == 0 {
		return ReadbackConfig{}, errors.New("readback PostgreSQL identity is invalid")
	}
	return config, nil
}

// RunReadbackAttestor запускает независимую проверку обслуживаемого снимка.
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
	if err := readbackApplication.ActivateTrust(
		startup,
		trust,
		readbackTrustState(trustMetadata, time.Now()),
	); err != nil {
		pool.Close()
		return fmt.Errorf("activate durable readback trust: %w", err)
	}
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
	workers := serviceruntime.StartWorkers(lifecycle, func(ctx context.Context) error {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
			nextTrust, nextMetadata, loadErr := snapshot.LoadReadbackTrust(
				snapshot.ReadbackTrustOptions{
					RootPublicJWKFile:      config.RootPublicJWKFile,
					RootMetadataFile:       config.RootMetadataFile,
					ManifestBundleJWSFile:  config.ManifestBundleJWSFile,
					CredentialTrustJWSFile: config.CredentialTrustJWSFile,
					Now:                    time.Now(),
				},
			)
			if loadErr != nil {
				if readiness.Set(false, "readback-trust-reload-failed") {
					logger.Error("readback trust reload unavailable", "error_class", "snapshot")
				}
				metrics.SetReady(false)
				continue
			}
			if activateErr := readbackApplication.ActivateTrust(
				ctx,
				nextTrust,
				readbackTrustState(nextMetadata, time.Now()),
			); activateErr != nil {
				if readiness.Set(false, "readback-trust-watermark-rejected") {
					logger.Error("readback trust activation rejected", "error_class", "watermark")
				}
				metrics.SetReady(false)
				continue
			}
			if readiness.Set(true, "ready") {
				logger.Info("readback trust readiness restored")
			}
			metrics.SetReady(true)
		}
	})
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

func readbackTrustState(
	metadata snapshot.ReadbackTrustMetadata,
	now time.Time,
) model.ReadbackTrustState {
	return model.ReadbackTrustState{
		RootID:                       metadata.RootID,
		RootFingerprintSHA256:        metadata.RootFingerprintSHA256,
		ManifestBundleRevision:       metadata.ManifestBundleRevision,
		ManifestBundleDigestSHA256:   metadata.ManifestBundleDigest,
		TrustSourceRevision:          metadata.TrustSourceRevision,
		TrustSetDigestSHA256:         metadata.TrustSetDigest,
		TrustKeySetRevision:          metadata.TrustKeySetRevision,
		SignerGeneration:             metadata.ManifestSignerGeneration,
		PredecessorStateDigestSHA256: metadata.PredecessorStateDigest,
		ServedStateDigestSHA256:      metadata.ServedStateDigest,
		ServedAt:                     now.UTC().Truncate(time.Second),
	}
}

func openReadbackPostgres(
	ctx context.Context,
	config ReadbackConfig,
) (*pgxpool.Pool, error) {
	return openRotatingPostgres(ctx, rotatingPostgresConfig{
		DSNTemplateFile: config.PostgresDSNFile,
		TLSServerName:   config.PostgresTLSServerName,
		Capability:      sessionrepository.CapabilityReadbackAttestor,
		ApplicationName: "internal_rpc_authority_readback_attestor",
		PodUID:          config.PodUID,
		Candidates: []databaseCredentialCandidate{
			{Role: "internal-rpc-authority-readback-attestor-g3", Principal: "ira_readback_attestor_g3", Directory: "/var/run/secrets/mattercodex/internal-rpc-authority/readback-attestor/database/g3"},
			{Role: "internal-rpc-authority-readback-attestor-g4", Principal: "ira_readback_attestor_g4", Directory: "/var/run/secrets/mattercodex/internal-rpc-authority/readback-attestor/database/g4"},
			{Role: "internal-rpc-authority-readback-attestor-g5", Principal: "ira_readback_attestor_g5", Directory: "/var/run/secrets/mattercodex/internal-rpc-authority/readback-attestor/database/g5"},
		},
	})
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
	mux.HandleFunc("/healthz", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/readyz", func(response http.ResponseWriter, _ *http.Request) {
		if ready, _ := readiness.Ready(); !ready {
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
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
