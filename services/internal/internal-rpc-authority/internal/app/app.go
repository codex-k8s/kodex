package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/grpcserver"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/udscred"
	"github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/application"
	readbackclient "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/client/readback"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/client/restoreagent"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
	authorityrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/postgres/authority"
	sessionrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/postgres/session"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/snapshot"
	authoritygrpc "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/transport/grpc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

const (
	maxDSNFileBytes = 16 << 10
	logMessageStart = "internal-rpc-authority runtime started"
	logMessageStop  = "internal-rpc-authority runtime stopped"
)

// Run запускает локальный issuer или verifier и управляет его жизненным циклом.
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
	telemetry, logger, err := startTelemetry(
		lifecycle,
		config.ServiceName,
		buildVersion,
	)
	if err != nil {
		return fmt.Errorf("start observability runtime: %w", err)
	}
	telemetryFinished := false
	defer func() {
		if !telemetryFinished {
			telemetry.cleanupAfterStartupFailure(shutdownBase, config.ShutdownTimeout)
		}
	}()
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
		Role:                       snapshot.Role(config.Mode),
		WorkloadID:                 config.WorkloadID,
		SnapshotJWSFile:            config.SnapshotJWSFile,
		ManifestRootPublicJWKFile:  config.ManifestRootPublicJWKFile,
		ManifestRootMetadataFile:   config.ManifestRootMetadataFile,
		ManifestTrustBundleJWSFile: config.ManifestTrustBundleJWSFile,
		ContextPrivateJWKFile:      config.ContextPrivateJWKFile,
		ProofTrustJWKFile:          config.ProofTrustJWKFile,
		Now:                        time.Now(),
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
	delivery, err := newRuntimeSecretDelivery(config)
	if err != nil {
		store.Close()
		return fmt.Errorf("construct authority material delivery: %w", err)
	}
	readbackTLS, err := loadRestoreClientTLS(
		config.ReadbackAttestorCAFile,
		config.WorkloadCertificateFile,
		config.WorkloadPrivateKeyFile,
		config.ReadbackAttestorTLSServerName,
	)
	if err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("load readback attestor mTLS client: %w", err)
	}
	snapshotAttestor, err := readbackclient.NewVaultAttestor(readbackclient.VaultConfig{
		Address: config.ReadbackAttestorAddress, TLS: readbackTLS,
		CredentialPath:          config.ReadbackCredentialVaultPath,
		PossessionPath:          config.ReadbackPossessionVaultPath,
		Delivery:                delivery,
		WorkloadID:              config.WorkloadID,
		WorkloadSPIFFEID:        config.WorkloadSPIFFEID,
		Role:                    config.ReadbackRole,
		WorkloadGeneration:      config.WorkloadGeneration,
		CredentialGeneration:    config.CredentialGeneration,
		PossessionKeyGeneration: config.PossessionKeyGeneration,
		UnaryInterceptor: telemetry.UnaryClientInterceptor(map[string]string{
			internalrpcauthorityv1.AuthorityReadbackAttestorService_IssueAttestationChallenge_FullMethodName: "issue_attestation_challenge",
			internalrpcauthorityv1.AuthorityReadbackAttestorService_AttestServedState_FullMethodName:         "attest_served_state",
		}),
	})
	if err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("construct readback attestor client: %w", err)
	}
	var activationAttestor repository.SnapshotAttestor = snapshotAttestor
	if config.Mode == ModeVerifier && config.ResolverEnabled {
		resolverAttestor, resolverErr := readbackclient.NewVaultAttestor(
			readbackclient.VaultConfig{
				Address: config.ReadbackAttestorAddress, TLS: readbackTLS,
				CredentialPath:          config.ResolverReadbackCredentialPath,
				PossessionPath:          config.ResolverReadbackPossessionPath,
				Delivery:                delivery,
				WorkloadID:              config.WorkloadID,
				WorkloadSPIFFEID:        config.WorkloadSPIFFEID,
				Role:                    "AUTHORITY_PROOF_RESOLVER",
				WorkloadGeneration:      config.WorkloadGeneration,
				CredentialGeneration:    config.ResolverCredentialGeneration,
				PossessionKeyGeneration: config.ResolverPossessionKeyGeneration,
				UnaryInterceptor: telemetry.UnaryClientInterceptor(map[string]string{
					internalrpcauthorityv1.AuthorityReadbackAttestorService_IssueAttestationChallenge_FullMethodName: "issue_attestation_challenge",
					internalrpcauthorityv1.AuthorityReadbackAttestorService_AttestServedState_FullMethodName:         "attest_served_state",
				}),
			},
		)
		if resolverErr != nil {
			delivery.Close()
			store.Close()
			return fmt.Errorf(
				"construct authority proof resolver readback client: %w",
				resolverErr,
			)
		}
		activationAttestor = &proofResolverAttestor{
			primary:          snapshotAttestor,
			resolver:         resolverAttestor,
			privateJWKFile:   config.ResolverProofPrivateJWKFile,
			proofTrustFile:   config.ResolverProofTrustJWKFile,
			issuer:           config.WorkloadSPIFFEID,
			signerGeneration: config.ResolverProofSignerGeneration,
			now:              time.Now,
		}
	}
	authorityApplication, err := application.NewAuthority(
		domainService,
		activationAttestor,
	)
	if err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("construct authority application: %w", err)
	}
	restoreTLS, err := loadRestoreClientTLS(
		config.RestoreControllerCAFile,
		config.WorkloadCertificateFile,
		config.WorkloadPrivateKeyFile,
		config.RestoreControllerTLSServerName,
	)
	if err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("load restore controller mTLS client: %w", err)
	}
	restoreWorkloadAgent, err := restoreagent.New(restoreagent.Config{
		Address: config.RestoreControllerAddress, TLS: restoreTLS,
		RoleCredentialVaultPath:    config.RestoreRoleCredentialVaultPath,
		ACKPrivateJWKVaultPath:     config.RestoreACKVaultPath,
		Delivery:                   delivery,
		ControllerCertificateFile:  config.RestoreControllerCertificateFile,
		ManifestRootPublicJWKFile:  config.ManifestRootPublicJWKFile,
		ManifestRootMetadataFile:   config.ManifestRootMetadataFile,
		ManifestTrustBundleJWSFile: config.ManifestTrustBundleJWSFile,
		RestoreRoleTrustJWSFile:    config.RestoreRoleTrustJWSFile,
		WorkloadID:                 config.WorkloadID, WorkloadSPIFFEID: config.WorkloadSPIFFEID,
		Role: config.ReadbackRole, WorkloadGeneration: config.WorkloadGeneration,
		CredentialGeneration: config.CredentialGeneration,
		ACKKeyGeneration:     config.RestoreACKKeyGeneration,
		UnaryInterceptor: telemetry.UnaryClientInterceptor(map[string]string{
			internalrpcauthorityv1.RestoreControllerService_GetRestoreDirective_FullMethodName:   "get_restore_directive",
			internalrpcauthorityv1.RestoreControllerService_AcknowledgeQuiescence_FullMethodName: "acknowledge_quiescence",
		}),
	})
	if err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("construct restore workload agent: %w", err)
	}
	if err := restoreWorkloadAgent.VerifyStartup(
		startupCtx,
		authorityApplication,
	); err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("verify external restore startup barrier: %w", err)
	}
	if err := activateSnapshotUntilReady(startupCtx, authorityApplication); err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("activate served authority snapshot: %w", err)
	}
	if err := restoreWorkloadAgent.Poll(
		startupCtx,
		authorityApplication,
	); err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("open external restore startup barrier: %w", err)
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
		grpc.ForceServerCodec(grpcserver.StrictProtoCodec()),
		grpc.ChainUnaryInterceptor(
			metrics.UnaryServerInterceptor(),
			telemetry.UnaryServerInterceptor(allowedMethods(mode)),
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
		delivery.Close()
		return err
	}
	technicalListener, err := net.Listen("tcp", config.TechnicalListen)
	if err != nil {
		_ = unixListener.Close()
		store.Close()
		delivery.Close()
		return fmt.Errorf("listen on technical HTTP endpoint: %w", err)
	}
	technicalServer := newTechnicalServer(
		config,
		readiness,
		metrics,
		authorityApplication,
	)
	workers := serviceruntime.StartWorkers(
		lifecycle,
		func(ctx context.Context) error {
			runSnapshotReload(
				ctx,
				config,
				store,
				authorityApplication,
				readiness,
				metrics,
				logger,
			)
			return nil
		},
		func(ctx context.Context) error {
			runReplayCleanup(ctx, config, store, readiness, metrics, logger)
			return nil
		},
		func(ctx context.Context) error {
			runRestoreWorkloadAgent(
				ctx,
				config,
				restoreWorkloadAgent,
				authorityApplication,
				readiness,
				metrics,
				logger,
			)
			return nil
		},
	)
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
	readiness.Set(true, "ready")
	metrics.SetReady(true)
	logger.Info(logMessageStart, "mode", string(mode), "workload", config.WorkloadID)
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
			Name:    "secret-delivery",
			Timeout: config.ShutdownTimeout,
			Run: func(context.Context) error {
				delivery.Close()
				return nil
			},
		},
		serviceruntime.ShutdownOperation{
			Name:    "postgresql",
			Timeout: config.ShutdownTimeout,
			Run: func(context.Context) error {
				store.Close()
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
	logger.Info(logMessageStop, "mode", string(mode))
	return errors.Join(runtimeErr, shutdownErr)
}

type reservationCleaner interface {
	DeleteExpired(context.Context, repository.ReservationKind, time.Time) error
}

func runRestoreWorkloadAgent(
	ctx context.Context,
	config Config,
	agent *restoreagent.Agent,
	authorityApplication *application.Authority,
	readiness *serviceruntime.Readiness,
	metrics *observability.Metrics,
	logger *slog.Logger,
) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		pollCtx, cancel := context.WithTimeout(ctx, config.ReadinessTimeout)
		err := agent.Poll(pollCtx, authorityApplication)
		cancel()
		if err != nil {
			authorityApplication.SetRestoreBlocked(true)
			readiness.Set(false, "restore-coordination-failed")
			metrics.SetReady(false)
			logger.Error("restore coordination poll failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runReplayCleanup(
	ctx context.Context,
	config Config,
	cleaner reservationCleaner,
	readiness *serviceruntime.Readiness,
	metrics *observability.Metrics,
	logger *slog.Logger,
) {
	kind := repository.ReservationAuthorizationContext
	if config.Mode == ModeIssuer {
		kind = repository.ReservationAuthorityProof
	}
	ticker := time.NewTicker(config.ReplayCleanupInterval)
	defer ticker.Stop()
	for {
		cleanupCtx, cancel := context.WithTimeout(ctx, config.ReadinessTimeout)
		err := cleaner.DeleteExpired(
			cleanupCtx,
			kind,
			time.Now().UTC().Add(-config.ReplayRetentionAfterExpiry),
		)
		cancel()
		if err != nil {
			readiness.Set(false, "replay-cleanup-failed")
			metrics.SetReady(false)
			logger.Error("replay reservation cleanup failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func activateSnapshotUntilReady(
	ctx context.Context,
	authorityApplication *application.Authority,
) error {
	const retryInterval = 250 * time.Millisecond
	for {
		err := authorityApplication.ActivateSnapshot(ctx)
		if err == nil {
			return nil
		}
		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(err, ctx.Err())
		case <-timer.C:
		}
	}
}

func runSnapshotReload(
	ctx context.Context,
	config Config,
	store *authorityrepository.Store,
	authorityApplication *application.Authority,
	readiness *serviceruntime.Readiness,
	metrics *observability.Metrics,
	logger *slog.Logger,
) {
	ticker := time.NewTicker(config.SnapshotReloadInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		current := authorityApplication.SnapshotState()
		loaded, err := snapshot.Load(snapshot.LoadOptions{
			Role:                       snapshot.Role(config.Mode),
			WorkloadID:                 config.WorkloadID,
			SnapshotJWSFile:            config.SnapshotJWSFile,
			ManifestRootPublicJWKFile:  config.ManifestRootPublicJWKFile,
			ManifestRootMetadataFile:   config.ManifestRootMetadataFile,
			ManifestTrustBundleJWSFile: config.ManifestTrustBundleJWSFile,
			ContextPrivateJWKFile:      config.ContextPrivateJWKFile,
			ProofTrustJWKFile:          config.ProofTrustJWKFile,
			Now:                        time.Now(),
		})
		if err != nil {
			authorityApplication.SetAvailable(false)
			readiness.Set(false, "snapshot-reload-rejected")
			metrics.SetReady(false)
			logger.Error("authority snapshot reload rejected")
			continue
		}
		if loaded.Policy.SourceRevision == current.SourceRevision &&
			loaded.Policy.SourceDigestSHA256 == current.SourceDigestSHA256 &&
			loaded.Policy.KeySetRevision == current.KeySetRevision &&
			loaded.Policy.PolicyRevision == current.PolicyRevision &&
			loaded.Policy.SignerGeneration == current.SignerGeneration {
			probeCtx, probeCancel := context.WithTimeout(ctx, config.ReadinessTimeout)
			readyErr := authorityApplication.ServedStateReady(probeCtx)
			probeCancel()
			if readyErr == nil {
				authorityApplication.SetAvailable(true)
				readiness.Set(true, "ready")
				metrics.SetReady(true)
			} else {
				authorityApplication.SetAvailable(false)
				readiness.Set(false, "served-snapshot-readback-failed")
				metrics.SetReady(false)
			}
			continue
		}
		next, err := service.NewAuthority(loaded.Policy, loaded.Keys, store)
		activationCtx, activationCancel := context.WithTimeout(
			ctx,
			config.ReadinessTimeout,
		)
		if err == nil {
			err = authorityApplication.ActivateReplacement(activationCtx, next)
		}
		activationCancel()
		if err != nil {
			authorityApplication.SetAvailable(false)
			readiness.Set(false, "snapshot-reload-rejected")
			metrics.SetReady(false)
			logger.Error("authority snapshot activation rejected")
			continue
		}
		readiness.Set(true, "ready")
		metrics.SetReady(true)
		logger.Info(
			"authority snapshot activated",
			"source_revision", loaded.Policy.SourceRevision,
			"key_set_revision", loaded.Policy.KeySetRevision,
			"policy_revision", loaded.Policy.PolicyRevision,
			"signer_generation", loaded.Policy.SignerGeneration,
		)
	}
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
	instrumentPGX(poolConfig, config.ServiceName)
	if len(poolConfig.ConnConfig.Fallbacks) != 0 ||
		poolConfig.ConnConfig.Host != config.PostgresTLSServerName ||
		poolConfig.ConnConfig.TLSConfig == nil ||
		poolConfig.ConnConfig.TLSConfig.RootCAs == nil ||
		poolConfig.ConnConfig.TLSConfig.ServerName != config.PostgresTLSServerName ||
		poolConfig.ConnConfig.TLSConfig.InsecureSkipVerify {
		return nil, errors.New("PostgreSQL DSN must use verify-full TLS with exact server name")
	}
	poolConfig.MaxConns = config.PostgresMaxConnections
	poolConfig.ConnConfig.RuntimeParams["application_name"] = config.ServiceName
	poolConfig.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		return sessionrepository.Configure(
			ctx,
			connection,
			config.PostgresExpectedSessionUser,
			config.DatabaseCapabilityRole,
		)
	}
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
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(filepath.Dir(path), resolved)
	if err != nil || relative == ".." || filepath.IsAbs(relative) ||
		len(relative) >= 3 && relative[:3] == "../" {
		return nil, errors.New("secret file symlink escapes its mounted directory")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() ||
		info.Mode().Perm()&0o007 != 0 ||
		info.Size() <= 0 ||
		info.Size() > limit {
		return nil, errors.New("secret file type, mode or size is invalid")
	}
	return os.ReadFile(resolved)
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
