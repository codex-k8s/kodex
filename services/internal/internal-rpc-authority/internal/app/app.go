package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/udscred"
	"github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/application"
	readbackclient "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/client/readback"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/client/restoreagent"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/service"
	authorityrepository "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/repository/postgres/authority"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/snapshot"
	authoritygrpc "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/transport/grpc"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	maxDSNFileBytes                 = 16 << 10
	snapshotReadbackRefreshInterval = time.Minute
	logMessageStart                 = "internal-rpc-authority runtime started"
	logMessageStop                  = "internal-rpc-authority runtime stopped"
)

type snapshotMaintenance interface {
	ServedStateReady(context.Context) error
	ActivateSnapshot(context.Context) error
}

type authorityReadinessCondition string

const (
	conditionSnapshot authorityReadinessCondition = "snapshot"
	conditionReplay   authorityReadinessCondition = "replay_cleanup"
)

var authorityReadinessOrder = [...]authorityReadinessCondition{conditionSnapshot, conditionReplay}

type authorityReadiness struct {
	mutex      sync.Mutex
	snapshot   *serviceruntime.Readiness
	metrics    *observability.Metrics
	conditions map[authorityReadinessCondition]bool
}

func newAuthorityReadiness(snapshot *serviceruntime.Readiness, metrics *observability.Metrics) *authorityReadiness {
	conditions := make(map[authorityReadinessCondition]bool, len(authorityReadinessOrder))
	for _, condition := range authorityReadinessOrder {
		conditions[condition] = true
	}
	state := &authorityReadiness{snapshot: snapshot, metrics: metrics, conditions: conditions}
	state.publishLocked()
	return state
}

// Set обновляет одно локальное условие и возвращает true только на его edge.
// Общая готовность является конъюнкцией всех условий, поэтому один worker не
// может скрыть отказ другого успешным tick.
func (state *authorityReadiness) Set(condition authorityReadinessCondition, ready bool) bool {
	state.mutex.Lock()
	defer state.mutex.Unlock()
	previous, exists := state.conditions[condition]
	if !exists || previous == ready {
		return false
	}
	state.conditions[condition] = ready
	state.publishLocked()
	return true
}

func (state *authorityReadiness) publishLocked() {
	ready, reason := true, "ready"
	for _, condition := range authorityReadinessOrder {
		if !state.conditions[condition] {
			ready, reason = false, string(condition)+"_unavailable"
			break
		}
	}
	state.snapshot.Set(ready, reason)
	state.metrics.SetReady(ready)
}

// Run запускает локальный issuer или verifier и управляет его жизненным циклом.
func Run(
	lifecycle context.Context,
	shutdownBase context.Context,
	mode Mode,
	buildVersion string,
) (runErr error) {
	stage := stageConfiguration
	defer func() {
		if runErr != nil {
			runErr = &runtimeStageError{stage: stage, cause: runErr}
		}
	}()
	config, err := LoadConfig(mode)
	if err != nil {
		return fmt.Errorf("load runtime configuration: %w", err)
	}
	stage = stageObservability
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
	stage = stagePostgres
	pool, err := openPostgres(startupCtx, config)
	if err != nil {
		return err
	}
	readinessReservationKind := repository.ReservationAuthorizationContext
	if config.Mode == ModeIssuer {
		readinessReservationKind = repository.ReservationAuthorityProof
	}
	store, err := authorityrepository.New(
		pool,
		config.WorkloadID,
		readinessReservationKind,
	)
	if err != nil {
		pool.Close()
		return fmt.Errorf("construct authority repository: %w", err)
	}
	stage = stageSnapshotLoad
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
	stage = stageAuthorityConstruction
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
	stage = stageReadbackClient
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
	snapshotAttestor, err := readbackclient.NewSecretAttestor(readbackclient.SecretConfig{
		Address: config.ReadbackAttestorAddress, TLS: readbackTLS,
		CredentialPath:          config.ReadbackCredentialSecret,
		PossessionPath:          config.ReadbackPossessionSecret,
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
	readbackAttestors := []*readbackclient.SecretAttestor{snapshotAttestor}
	closeReadbackAttestors := func() error {
		var closeErrors []error
		for _, attestor := range readbackAttestors {
			if closeErr := attestor.Close(); closeErr != nil {
				closeErrors = append(closeErrors, closeErr)
			}
		}
		readbackAttestors = nil
		return errors.Join(closeErrors...)
	}
	defer func() {
		_ = closeReadbackAttestors()
	}()
	var activationAttestor repository.SnapshotAttestor = snapshotAttestor
	if config.Mode == ModeVerifier && config.ResolverEnabled {
		resolverAttestor, resolverErr := readbackclient.NewSecretAttestor(
			readbackclient.SecretConfig{
				Address: config.ReadbackAttestorAddress, TLS: readbackTLS,
				CredentialPath:          config.ResolverReadbackCredentialSecret,
				PossessionPath:          config.ResolverReadbackPossessionSecret,
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
		readbackAttestors = append(readbackAttestors, resolverAttestor)
		activationAttestor = &proofResolverAttestor{
			primary:              snapshotAttestor,
			resolver:             resolverAttestor,
			privateJWKFile:       config.ResolverProofPrivateJWKFile,
			signerGenerationFile: config.ResolverProofSignerGenerationFile,
			proofTrustFile:       config.ResolverProofTrustJWKFile,
			issuer:               config.WorkloadSPIFFEID,
			now:                  time.Now,
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
	stage = stageRestoreClient
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
		RoleCredentialSecret:       config.RestoreRoleCredentialSecret,
		ACKPrivateJWKSecret:        config.RestoreACKSecret,
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
	stage = stageRestoreVerify
	if err := restoreWorkloadAgent.VerifyStartup(
		startupCtx,
		authorityApplication,
	); err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("verify external restore startup barrier: %w", err)
	}
	stage = stageSnapshotActivation
	if err := activateSnapshotUntilReady(startupCtx, authorityApplication); err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("activate served authority snapshot: %w", err)
	}
	stage = stageRestoreOpen
	if err := restoreWorkloadAgent.Poll(
		startupCtx,
		authorityApplication,
	); err != nil {
		delivery.Close()
		store.Close()
		return fmt.Errorf("open external restore startup barrier: %w", err)
	}
	stage = stageLocalServers
	metrics := observability.NewMetrics(
		config.ServiceName,
		buildVersion,
		allowedMethods(mode),
	)
	readiness := serviceruntime.NewReadiness()
	readiness.Set(false, "starting")
	metrics.SetReady(false)
	localReadiness := newAuthorityReadiness(readiness, metrics)
	errorObserver := grpcserver.ErrorObserverFunc(func(
		_ context.Context,
		method string,
		code codes.Code,
		err error,
	) {
		logger.Error(
			"unexpected gRPC failure",
			"method", normalizedMethod(mode, method),
			"code", code.String(),
			"grpc_message", status.Convert(err).Message(),
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
	stage = stageRuntime
	workers := serviceruntime.StartWorkers(
		lifecycle,
		func(ctx context.Context) error {
			runSnapshotReload(
				ctx,
				config,
				store,
				authorityApplication,
				localReadiness,
				logger,
			)
			return nil
		},
		func(ctx context.Context) error {
			runReplayCleanup(ctx, config, store, localReadiness, logger)
			return nil
		},
		func(ctx context.Context) error {
			runRestoreWorkloadAgent(
				ctx,
				config,
				restoreWorkloadAgent,
				authorityApplication,
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
			Name:    "readback-attestors",
			Timeout: config.ShutdownTimeout,
			Run: func(context.Context) error {
				return closeReadbackAttestors()
			},
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
	logger *slog.Logger,
) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	available := true
	for {
		pollCtx, cancel := context.WithTimeout(ctx, config.ReadinessTimeout)
		err := agent.Poll(pollCtx, authorityApplication)
		cancel()
		if err != nil {
			authorityApplication.SetRestoreBlocked(true)
			if available {
				logger.Error("restore coordination unavailable", "error_class", "restore_controller")
			}
			available = false
		} else {
			if !available {
				logger.Info("restore coordination restored")
			}
			available = true
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
	readiness *authorityReadiness,
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
			if readiness.Set(conditionReplay, false) {
				logger.Error("replay reservation cleanup unavailable", "error_class", "postgresql")
			}
		} else if readiness.Set(conditionReplay, true) {
			logger.Info("replay reservation cleanup restored")
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
	const maximumRetryInterval = 10 * time.Second
	retryInterval := time.Second
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
		retryInterval = min(retryInterval*2, maximumRetryInterval)
	}
}

func runSnapshotReload(
	ctx context.Context,
	config Config,
	store *authorityrepository.Store,
	authorityApplication *application.Authority,
	readiness *authorityReadiness,
	logger *slog.Logger,
) {
	ticker := time.NewTicker(config.SnapshotReloadInterval)
	defer ticker.Stop()
	lastReadbackRefresh := time.Now()
	readbackRefreshDeferred := false
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
			if readiness.Set(conditionSnapshot, false) {
				logger.Error("authority snapshot reload rejected", "error_class", "snapshot")
			}
			continue
		}
		if loaded.Policy.SourceRevision == current.SourceRevision &&
			loaded.Policy.SourceDigestSHA256 == current.SourceDigestSHA256 &&
			loaded.Policy.KeySetRevision == current.KeySetRevision &&
			loaded.Policy.PolicyRevision == current.PolicyRevision &&
			loaded.Policy.SignerGeneration == current.SignerGeneration {
			probeCtx, probeCancel := context.WithTimeout(ctx, config.ReadinessTimeout)
			previousReadbackRefresh := lastReadbackRefresh
			lastReadbackRefresh, err = maintainServedSnapshot(
				probeCtx,
				authorityApplication,
				lastReadbackRefresh,
				time.Now(),
			)
			probeCancel()
			if err == nil {
				authorityApplication.SetAvailable(true)
				if readiness.Set(conditionSnapshot, true) {
					logger.Info("authority snapshot readiness restored")
				}
				if lastReadbackRefresh.Equal(previousReadbackRefresh) &&
					time.Since(previousReadbackRefresh) >= snapshotReadbackRefreshInterval {
					if !readbackRefreshDeferred {
						readbackRefreshDeferred = true
						logger.Warn("served snapshot readback refresh deferred", "error_class", "readback_attestor")
					}
				} else if readbackRefreshDeferred {
					readbackRefreshDeferred = false
					logger.Info("served snapshot readback refresh restored")
				}
			} else {
				authorityApplication.SetAvailable(false)
				if readiness.Set(conditionSnapshot, false) {
					logger.Error("served snapshot readback unavailable", "error_class", "readback_attestor")
				}
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
			if readiness.Set(conditionSnapshot, false) {
				logger.Error("authority snapshot activation rejected", "error_class", "snapshot")
			}
			continue
		}
		lastReadbackRefresh = time.Now()
		readbackRefreshDeferred = false
		readiness.Set(conditionSnapshot, true)
		logger.Info(
			"authority snapshot activated",
			"source_revision", loaded.Policy.SourceRevision,
			"key_set_revision", loaded.Policy.KeySetRevision,
			"policy_revision", loaded.Policy.PolicyRevision,
			"signer_generation", loaded.Policy.SignerGeneration,
		)
	}
}

func maintainServedSnapshot(
	ctx context.Context,
	runtime snapshotMaintenance,
	lastRefresh time.Time,
	now time.Time,
) (time.Time, error) {
	servedStateChecked := false
	var servedStateErr error
	if now.Sub(lastRefresh) < snapshotReadbackRefreshInterval {
		servedStateChecked = true
		servedStateErr = runtime.ServedStateReady(ctx)
		if servedStateErr == nil {
			return lastRefresh, nil
		}
	}
	if err := runtime.ActivateSnapshot(ctx); err != nil {
		if !allowsReadbackLastKnownGood(err) {
			return lastRefresh, fmt.Errorf(
				"refresh served snapshot readback: %w",
				err,
			)
		}
		if !servedStateChecked {
			servedStateErr = runtime.ServedStateReady(ctx)
		}
		if servedStateErr == nil {
			return lastRefresh, nil
		}
		return lastRefresh, errors.Join(
			fmt.Errorf("refresh served snapshot readback: %w", err),
			fmt.Errorf("validate current served snapshot readback: %w", servedStateErr),
		)
	}
	return now, nil
}

func allowsReadbackLastKnownGood(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) &&
		(networkError.Timeout() || networkError.Temporary())
}

func openPostgres(ctx context.Context, config Config) (*pgxpool.Pool, error) {
	return openCapabilityPostgres(ctx, postgresConnectionConfig{
		DSNFile: config.PostgresDSNFile, TLSServerName: config.PostgresTLSServerName,
		ExpectedUser: config.PostgresExpectedSessionUser, Capability: config.DatabaseCapabilityRole,
		ApplicationName: config.ServiceName, MaxConnections: config.PostgresMaxConnections,
	})
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
	mux.HandleFunc("/healthz", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/readyz", func(response http.ResponseWriter, _ *http.Request) {
		if ready, _ := readiness.Ready(); !ready {
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
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
	value, err := securefile.Read(path, limit)
	if err != nil {
		return nil, errors.New("secret file type, mode or size is invalid")
	}
	return value, nil
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
