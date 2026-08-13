// Package app содержит единственный корень композиции control-plane.
package app

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	sharedcache "github.com/codex-k8s/matter-codex/libs/go/cache"
	"github.com/codex-k8s/matter-codex/libs/go/cache/redisstore"
	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/eventing"
	"github.com/codex-k8s/matter-codex/libs/go/eventing/natsjetstream"
	"github.com/codex-k8s/matter-codex/libs/go/grpcserver"
	"github.com/codex-k8s/matter-codex/libs/go/integrationgatewayauth"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	continuationgrantauth "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization/continuationgrant"
	grantauth "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization/grant"
	mattermosteventauth "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization/mattermostevent"
	oidcauth "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization/oidc"
	authoritypolicy "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization/policy"
	readbackgrantauth "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization/readbackgrant"
	objectclient "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/client/objectstore"
	proofsignerfile "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/client/proofsigner/file"
	domainerrs "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	authorityservice "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/authority"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/resource"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	internalobservability "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/observability"
	cachecontrolplane "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/repository/cache/controlplane"
	postgrescontrolplane "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/repository/postgres/controlplane"
	transportgrpc "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/transport/grpc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	verifierUID = 29002
	verifierGID = 29000
)

type runtimeState struct {
	config      Config
	logger      *slog.Logger
	telemetry   *appTelemetry
	metrics     *observability.Metrics
	readiness   *serviceruntime.Readiness
	workers     *serviceruntime.WorkerGroup
	grpcServer  *stdgrpc.Server
	httpServer  *http.Server
	publisher   *natsjetstream.Publisher
	cache       sharedcache.Store
	oidc        *oidcauth.Verifier
	authority   *authorityclient.LocalConnection
	runtimePool *pgxpool.Pool
	relayPool   *pgxpool.Pool
}

// Run запускает стартовые барьеры, gRPC, ретранслятор и ограниченное завершение.
func Run(
	lifecycle context.Context,
	shutdownBase context.Context,
	buildVersion string,
) (resultErr error) {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	state := &runtimeState{
		config:    config,
		readiness: serviceruntime.NewReadiness(),
	}
	defer func() {
		resultErr = errors.Join(
			resultErr,
			state.shutdown(context.WithoutCancel(shutdownBase)),
		)
	}()

	startup, cancelStartup := context.WithTimeout(lifecycle, config.StartupTimeout)
	defer cancelStartup()
	state.telemetry, state.logger, err = startTelemetry(startup, buildVersion)
	if err != nil {
		return err
	}
	methods := observableMethods()
	state.metrics = observability.NewMetrics(serviceName, buildVersion, methods)
	state.metrics.SetReady(false)
	businessMetrics, err := internalobservability.NewBusinessMetrics(state.metrics.Register)
	if err != nil {
		return err
	}

	loadedPolicy, err := authoritypolicy.Load(
		config.AuthorityPolicyFile,
		expectedOperations(),
	)
	if err != nil {
		return err
	}
	state.runtimePool, err = openPostgres(
		startup,
		config.PostgresDSNFile,
		config.PostgresCAFile,
		config.PostgresTLSServerName,
		config.PostgresMaxConnections,
	)
	if err != nil {
		return err
	}
	state.relayPool, err = openPostgres(
		startup,
		config.PostgresRelayDSNFile,
		config.PostgresCAFile,
		config.PostgresTLSServerName,
		4,
	)
	if err != nil {
		return err
	}
	contextSigningKey, err := readSecret(
		config.PostgresContextKeyFile,
		128,
	)
	if err != nil || len(contextSigningKey) < 32 {
		return errors.New("PostgreSQL context signing key is unavailable")
	}
	postgresRepository, err := postgrescontrolplane.New(
		state.runtimePool,
		postgrescontrolplane.Config{
			PrincipalName:       config.PostgresPrincipalName,
			PrincipalGeneration: config.PostgresPrincipalGeneration,
			ContextKeyID:        config.PostgresContextKeyID,
			ContextSigningKey:   contextSigningKey,
			ContextTTL:          5 * time.Second,
		},
	)
	if err != nil {
		return err
	}
	redisPassword, err := readSecret(
		config.RedisPasswordFile,
		maximumSecretFileBytes,
	)
	if err != nil || strings.TrimSpace(string(redisPassword)) == "" {
		return errors.New("redis credential is unavailable")
	}
	redisCache, err := redisstore.New(redisstore.Config{
		Address:       config.RedisAddress,
		TLSServerName: config.RedisTLSServerName,
		CAFile:        config.RedisCAFile,
		Username:      config.RedisUsername,
		Password:      strings.TrimSpace(string(redisPassword)),
		Database:      config.RedisDatabase,
		PoolSize:      config.RedisPoolSize,
		DialTimeout:   config.ReadinessTimeout,
		ReadTimeout:   config.ReadinessTimeout,
		WriteTimeout:  config.ReadinessTimeout,
	})
	if err != nil {
		return err
	}
	state.cache = redisCache
	cachedRepository, err := cachecontrolplane.New(
		postgresRepository,
		state.cache,
		config.CacheTimeout,
		config.CacheTTL,
	)
	if err != nil {
		return err
	}
	leaseKey, err := readSecret(
		config.LeaseSigningKeyFile,
		maximumSecretFileBytes,
	)
	if err != nil {
		return errors.New("turn lease signing key is unavailable")
	}
	decodeRuntimeSigningKey := func(path string) (ed25519.PrivateKey, error) {
		raw, readErr := readSecret(path, maximumSecretFileBytes)
		if readErr != nil {
			return nil, readErr
		}
		decoded, decodeErr := hex.DecodeString(strings.TrimSpace(string(raw)))
		if decodeErr != nil || len(decoded) != ed25519.SeedSize {
			return nil, errors.New("runtime workload signing key is invalid")
		}
		return ed25519.NewKeyFromSeed(decoded), nil
	}
	admissionSigningKey, err := decodeRuntimeSigningKey(config.RuntimeAdmissionSigningKeyFile)
	if err != nil {
		return errors.New("runtime admission signing key is unavailable")
	}
	var archiveSigningKey, restoreSigningKey ed25519.PrivateKey
	if config.RuntimeArchiveRestoreCapability == "enabled" {
		archiveSigningKey, err = decodeRuntimeSigningKey(config.RuntimeArchiveSigningKeyFile)
		if err != nil {
			return errors.New("runtime archive signing key is unavailable")
		}
		restoreSigningKey, err = decodeRuntimeSigningKey(config.RuntimeRestoreSigningKeyFile)
		if err != nil {
			return errors.New("runtime restore signing key is unavailable")
		}
	}
	interactionReadbackSigner, err := readbackgrantauth.New(startup, readbackgrantauth.Config{
		Issuer:     "https://control-plane.mattercodex-system.svc.cluster.local/authority/interaction-delivery-readback",
		Audience:   "urn:mattercodex:interaction-delivery-readback",
		ProducerID: "control-plane.interaction-delivery-readback", Purpose: "INTERACTION_DELIVERY_READBACK_GRANT",
		WorkloadID: "control-plane", CallerSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane",
		Operation: "interaction.delivery.read", Permission: "interaction.delivery.read",
		PrivateJWKFile:   config.InteractionReadbackPrivateJWKFile,
		PublicKeysetFile: config.InteractionReadbackPublicKeysetFile,
		Generation:       config.InteractionReadbackSignerGeneration, MaximumTTL: 15 * time.Minute,
	}, postgresRepository)
	if err != nil {
		return err
	}
	instructionObjects, err := objectclient.New(objectclient.Config{
		Endpoint: config.InstructionS3Endpoint, TLSServerName: config.InstructionS3TLSServerName,
		CAFile: config.InstructionS3CAFile, ClientCertificateFile: config.InstructionS3ClientCertificateFile,
		ClientPrivateKeyFile: config.InstructionS3ClientPrivateKeyFile,
		AccessKeyFile:        config.InstructionS3AccessKeyFile, SecretKeyFile: config.InstructionS3SecretKeyFile,
		SessionTokenFile: config.InstructionS3SessionTokenFile, Bucket: config.InstructionS3Bucket,
		MaximumObjectBytes: 262144, Timeout: config.ReadinessTimeout,
	}, postgresRepository)
	if err != nil {
		return err
	}
	resourceService, err := resource.New(cachedRepository, resource.Config{
		LeaseSigningKey:             leaseKey,
		RuntimeAdmissionSigningKey:  admissionSigningKey,
		ArchiveRestoreEnabled:       config.RuntimeArchiveRestoreCapability == "enabled",
		RuntimeArchiveSigningKey:    archiveSigningKey,
		RuntimeRestoreSigningKey:    restoreSigningKey,
		TurnLeaseDuration:           config.TurnLeaseDuration,
		MaximumScheduleClaims:       config.ScheduleClaimLimit,
		ImagePolicyRevision:         config.ImagePolicyRevision,
		ImagePolicySHA256:           config.ImagePolicySHA256,
		ImageBuildLeaseDuration:     config.ImageBuildLeaseDuration,
		ImageAdmissionClaimTTL:      config.ImageAdmissionClaimTTL,
		ImagePromotionClaimTTL:      config.ImagePromotionClaimTTL,
		ImageMaximumAttempts:        config.ImageMaximumAttempts,
		StagingImageRepository:      config.StagingImageRepository,
		PromotedImageRepository:     config.PromotedImageRepository,
		RoleImageInputRepository:    config.RoleImageInputRepository,
		TrustedRoleBaseRepository:   config.TrustedRoleBaseRepository,
		TrustedRoleBaseDigest:       config.TrustedRoleBaseDigest,
		RoleRuntimeContractRevision: config.RoleRuntimeContractRevision,
		RoleRuntimeContractSHA256:   config.RoleRuntimeContractSHA256,
		ImageBuilderWorkload:        "role-image-builder",
		ImageBuilderSPIFFEID:        "spiffe://mattercodex.local/ns/mattercodex-system/sa/role-image-builder",
		ImageAdmissionWorkload:      "image-admission",
		ImageAdmissionSPIFFEID:      "spiffe://mattercodex.local/ns/mattercodex-system/sa/image-admission",
		ImagePromotionWorkload:      "image-promotion",
		ImagePromotionSPIFFEID:      "spiffe://mattercodex.local/ns/mattercodex-system/sa/image-promotion",
		AuthorityPolicyRevision:     loadedPolicy.Revision,
		AuthorityPolicySHA256:       loadedPolicy.Digest,
		OwnerGateDeliveryWorkload:   "interaction-gateway",
		OwnerGateDeliverySPIFFEID:   "spiffe://mattercodex.local/ns/mattercodex-system/sa/interaction-gateway",
		ScannerWorkload:             "artifact-scanner",
		ScannerSPIFFEID:             "spiffe://mattercodex.local/ns/mattercodex-system/sa/artifact-scanner",
		SchedulerWorkload:           "automation-scheduler",
		SchedulerSPIFFEID:           "spiffe://mattercodex.local/ns/mattercodex-system/sa/automation-scheduler",
		MemoryIndexerWorkload:       "memory-indexer",
		MemoryIndexerSPIFFEID:       "spiffe://mattercodex.local/ns/mattercodex-system/sa/memory-indexer",
		RuntimeControllerWorkload:   "runtime-controller",
		RuntimeControllerSPIFFEID:   "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller",
		ArchiveWorkload:             "runtime-archive",
		ArchiveSPIFFEID:             "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-archive",
		IntegrationGatewayWorkload:  "integration-gateway",
		IntegrationGatewaySPIFFEID:  "spiffe://mattercodex.local/ns/mattercodex-system/sa/integration-gateway",
		RestoreVerifierWorkload:     "runtime-restore-verifier",
		RestoreVerifierSPIFFEID:     "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-restore-verifier",
		CleanupAuthorizerWorkload:   "runtime-cleanup-authorizer",
		CleanupAuthorizerSPIFFEID:   "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-cleanup-authorizer",
		PendingRescheduleDelay:      config.PendingRescheduleDelay,
		InteractionReadbackIssuer:   interactionReadbackIssuer{signer: interactionReadbackSigner},
		Observer:                    businessMetrics,
		InstructionObjects:          instructionObjects,
	})
	if err != nil {
		return err
	}
	signer, err := proofsignerfile.New(proofsignerfile.Config{
		PrivateJWKFile:   config.ProofPrivateJWKFile,
		TrustFile:        config.ProofTrustFile,
		Issuer:           loadedPolicy.Issuer,
		Audience:         loadedPolicy.ProofAudience,
		MaximumClockSkew: 5 * time.Second,
	})
	if err != nil {
		return err
	}
	proofService, err := authorityservice.New(
		cachedRepository,
		signer,
		authorityservice.Config{
			Issuer:                       loadedPolicy.Issuer,
			ProofAudience:                loadedPolicy.ProofAudience,
			AuthorizationContextAudience: loadedPolicy.AuthorizationContextAudience,
			PolicyRevision:               loadedPolicy.Revision,
			PolicyDigest:                 loadedPolicy.Digest,
			Operations:                   loadedPolicy.Operations,
		},
	)
	if err != nil {
		return err
	}
	state.oidc, err = oidcauth.New(startup, oidcauth.Config{
		ProducerID:           loadedPolicy.OIDC.ID,
		Purpose:              loadedPolicy.OIDC.Credential,
		Issuer:               loadedPolicy.OIDC.CredentialIssuer,
		Audience:             loadedPolicy.OIDC.CredentialAudience,
		ConnectAddress:       config.OIDCConnectAddress,
		TLSServerName:        config.OIDCTLSServerName,
		CAFile:               config.OIDCCAFile,
		ExpectedCallerSPIFFE: loadedPolicy.OIDC.CallerSPIFFEID,
		ExpectedWorkload:     loadedPolicy.OIDC.CallerWorkload,
		ClockSkew:            5 * time.Second,
		HTTPTimeout:          config.ReadinessTimeout,
	})
	if err != nil {
		return err
	}
	authenticators := []transportgrpc.ApplicationAuthenticator{state.oidc}
	var continuationProducer authoritypolicy.Producer
	for producerID, producer := range loadedPolicy.Producers {
		if producerID == loadedPolicy.OIDC.ID {
			continue
		}
		if producer.Credential == "MATTERMOST_SIGNED_EVENT" {
			verifier, verifyErr := mattermosteventauth.New(startup, mattermosteventauth.Config{
				ProducerID: producer.ID, Purpose: producer.Credential,
				Issuer: producer.CredentialIssuer, Audience: producer.CredentialAudience,
				WorkloadID: producer.CallerWorkload, CallerSPIFFEID: producer.CallerSPIFFEID,
				PublicKeysetFile: filepath.Join(config.ApplicationGrantTrustDir, producerID+".public-keyset.json"),
				MaximumTTL:       5 * time.Minute,
			}, postgresRepository)
			if verifyErr != nil {
				return verifyErr
			}
			authenticators = append(authenticators, verifier)
			continue
		}
		if producer.Credential == "INTEGRATION_CONTINUATION_GRANT" {
			publicKeysetFile := filepath.Join(config.ApplicationGrantTrustDir, producerID+".public-keyset.json")
			continuationProducer = producer
			verifier, verifyErr := continuationgrantauth.New(continuationgrantauth.Config{
				ProducerID: producer.ID, Purpose: producer.Credential,
				Issuer: producer.CredentialIssuer, Audience: producer.CredentialAudience,
				WorkloadID: producer.CallerWorkload, CallerSPIFFEID: producer.CallerSPIFFEID,
				PublicJWKFile: publicKeysetFile, Generation: config.ContinuationGrantSignerGeneration,
				MaximumTTL: 8 * 24 * time.Hour, ExpectedPurpose: integrationgatewayauth.PurposeTransition,
				CredentialMetadata: producer.CredentialMetadata,
			})
			if verifyErr != nil {
				return verifyErr
			}
			authenticators = append(authenticators, verifier)
			continue
		}
		verifier, err := grantauth.New(grantauth.Config{
			ProducerID:     producer.ID,
			Purpose:        producer.Credential,
			Issuer:         producer.CredentialIssuer,
			Audience:       producer.CredentialAudience,
			WorkloadID:     producer.CallerWorkload,
			CallerSPIFFEID: producer.CallerSPIFFEID,
			PublicJWKFile: filepath.Join(
				config.ApplicationGrantTrustDir,
				producerID+".public.jwk",
			),
		})
		if err != nil {
			return fmt.Errorf("initialize application grant verifier for producer %q: %w", producerID, err)
		}
		authenticators = append(authenticators, verifier)
	}
	if continuationProducer.ID == "" {
		return errors.New("integration continuation grant producer is unavailable")
	}
	continuationPublicJWK := filepath.Join(config.ApplicationGrantTrustDir, continuationProducer.ID+".public-keyset.json")
	transitionSigner, err := integrationgatewayauth.NewSigner(integrationgatewayauth.Config{
		Issuer: continuationProducer.CredentialIssuer, Audience: continuationProducer.CredentialAudience,
		WorkloadID: "integration-gateway", CallerSPIFFEID: continuationProducer.CallerSPIFFEID,
		Generation: config.ContinuationGrantSignerGeneration, MaximumTTL: 8 * 24 * time.Hour,
	}, config.ContinuationGrantPrivateJWKFile, continuationPublicJWK)
	if err != nil {
		return err
	}
	applicationRegistry, err := transportgrpc.NewApplicationRegistry(authenticators)
	if err != nil {
		return err
	}
	state.authority, err = authorityclient.DialLocal(startup, authorityclient.LocalConfig{
		SocketPath:        authorityclient.VerifierSocketPath,
		ExpectedServerUID: verifierUID,
		ExpectedServerGID: verifierGID,
		DialTimeout:       config.ReadinessTimeout,
	})
	if err != nil {
		return err
	}
	outboxStore, err := postgrescontrolplane.NewOutboxStore(state.relayPool, 25)
	if err != nil {
		return err
	}
	state.publisher, err = natsjetstream.New(natsjetstream.Config{
		URL:             config.NATSURL,
		TLSServerName:   config.NATSTLSServerName,
		CAFile:          config.NATSCAFile,
		CredentialsFile: config.NATSCredentialsFile,
		Stream:          config.NATSStream,
		Subjects: []string{
			"control_plane.runtime_configuration_changed",
		},
		Replicas:        config.NATSReplicas,
		MaxMessageBytes: 256 << 10,
		MaxMessages:     10_000_000,
		MaxBytes:        32 << 30,
		MaxPerSubject:   5_000_000,
		MaxAge:          30 * 24 * time.Hour,
		DuplicateWindow: 2 * time.Minute,
		ConnectTimeout:  config.ReadinessTimeout,
	})
	if err != nil {
		return err
	}
	relay, err := eventing.NewRelay(eventing.RelayConfig{
		InstanceID:      config.InstanceID,
		BatchSize:       32,
		PollInterval:    config.RelayPollInterval,
		LeaseDuration:   config.RelayLeaseDuration,
		PublishTimeout:  config.RelayPublishTimeout,
		FinalizeTimeout: config.RelayFinalizeTimeout,
		MaxAttempts:     25,
		InitialBackoff:  time.Second,
		MaximumBackoff:  time.Minute,
	}, outboxStore, state.publisher)
	if err != nil {
		return err
	}
	if err := internalobservability.RegisterInfrastructureMetrics(
		state.metrics.Register,
		state.runtimePool,
		state.relayPool,
		redisCache,
		relay,
		outboxStore,
	); err != nil {
		return err
	}
	checker := &readinessChecker{
		repository:         cachedRepository,
		cache:              state.cache,
		relay:              relay,
		proof:              proofService,
		verifier:           state.authority.Verifier(),
		policyRevision:     loadedPolicy.Revision,
		instructionObjects: instructionObjects,
	}
	if _, err := checker.Check(startup); err != nil {
		return fmt.Errorf("startup barrier: %w", err)
	}
	controlServer, err := transportgrpc.NewServer(resourceService, checker, transitionSigner)
	if err != nil {
		return err
	}
	authorityServer, err := transportgrpc.NewAuthorityServer(
		proofService,
		applicationRegistry,
	)
	if err != nil {
		return err
	}
	transportCredentials, err := serverCredentials(config)
	if err != nil {
		return err
	}
	state.grpcServer = stdgrpc.NewServer(
		stdgrpc.Creds(transportCredentials),
		stdgrpc.ForceServerCodec(grpcserver.StrictProtoCodec()),
		stdgrpc.MaxRecvMsgSize(9<<20),
		stdgrpc.MaxSendMsgSize(9<<20),
		stdgrpc.ChainUnaryInterceptor(
			state.metrics.UnaryServerInterceptor(),
			state.telemetry.UnaryServerInterceptor(methods),
			grpcserver.ErrorBoundary(nil),
			strictRequestInterceptor,
			proofOrAuthorizationInterceptor(state.authority.Verifier()),
		),
	)
	controlplanev1.RegisterControlPlaneServiceServer(state.grpcServer, controlServer)
	internalrpcauthorityv1.RegisterAuthorityProofResolverServiceServer(
		state.grpcServer,
		authorityServer,
	)
	listener, err := net.Listen("tcp", config.GRPCListen)
	if err != nil {
		return errors.New("listen control-plane gRPC")
	}
	state.httpServer = technicalServer(
		config.TechnicalListen,
		state.metrics,
		func() bool {
			ready, _ := state.readiness.Ready()
			return ready
		},
		state.logger,
	)
	serveErrors := make(chan error, 2)
	go func() {
		if err := state.grpcServer.Serve(listener); err != nil &&
			!errors.Is(err, stdgrpc.ErrServerStopped) {
			serveErrors <- fmt.Errorf("serve gRPC: %w", err)
		}
	}()
	go func() {
		if err := state.httpServer.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			serveErrors <- fmt.Errorf("serve technical HTTP: %w", err)
		}
	}()
	state.workers = serviceruntime.StartWorkers(
		lifecycle,
		func(ctx context.Context) error {
			return relay.Run(ctx, context.WithoutCancel(shutdownBase))
		},
		func(ctx context.Context) error {
			return reconcileReadiness(
				ctx,
				checker,
				state.readiness,
				state.metrics,
				config,
			)
		},
		func(ctx context.Context) error {
			return runWorkspaceRecovery(ctx, postgresRepository, resourceService,
				loadedPolicy.Revision, config.RecoveryPollInterval)
		},
	)
	workerResult := make(chan error, 1)
	go func() {
		workerResult <- state.workers.Wait(context.WithoutCancel(shutdownBase))
	}()
	state.readiness.Set(true, "ready")
	state.metrics.SetReady(true)
	state.logger.Info("control-plane started")
	select {
	case <-lifecycle.Done():
		return nil
	case err := <-serveErrors:
		return err
	case err := <-workerResult:
		if err != nil {
			return fmt.Errorf("control-plane worker failed: %w", err)
		}
		if lifecycle.Err() == nil {
			return errors.New("control-plane workers stopped unexpectedly")
		}
		return nil
	}
}

func runWorkspaceRecovery(
	ctx context.Context,
	repository domainrepo.WorkspaceRecoveryCandidateRepository,
	service *resource.Service,
	policyRevision uint64,
	pollInterval time.Duration,
) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		candidate, err := repository.NextWorkspaceRecoveryCandidate(ctx)
		if err == nil {
			permission := "controlplane.workspace_backup.terminal"
			if candidate.Kind == enum.KindWorkspaceRestore {
				permission = "controlplane.workspace_restore.terminal"
			}
			authorityDigest := workspaceRecoveryIntentDigest(candidate.ResourceID,
				candidate.Version, candidate.Outcome, candidate.TerminalReasonCode)
			principal := value.Principal{
				ActorID: candidate.OwnerActorID, OrganizationID: candidate.OrganizationID,
				ProjectID: candidate.ProjectID, Permission: permission,
				CorrelationID: uuid.NewString(), PolicyRevision: policyRevision,
				AuthorityGeneration: candidate.Generation,
				CallerWorkload:      "control-plane-recovery-reconciler",
				CallerSPIFFEID:      "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane-recovery-reconciler",
				AuthoritySource:     "DOMAIN_STATE", AuthorityReference: candidate.ResourceID,
				AuthorityRevision: candidate.Version,
				AuthorityDigest:   authorityDigest,
			}
			input := resource.ReconcileWorkspaceRecoveryInput{
				Principal: principal, IdempotencyKey: "workspace-recovery-" + authorityDigest,
				ResourceID: candidate.ResourceID, ExpectedVersion: candidate.Version,
				ExpectedAttempt: candidate.Attempt, ExpectedGeneration: candidate.Generation,
				Outcome: candidate.Outcome, TerminalReasonCode: candidate.TerminalReasonCode,
			}
			if candidate.Kind == enum.KindWorkspaceBackup {
				_, err = service.ReconcileWorkspaceBackupTerminal(ctx, input)
			} else {
				_, err = service.ReconcileWorkspaceRestoreTerminal(ctx, input)
			}
			if errors.Is(err, domainerrs.ErrStateConflict) && candidate.Outcome == "complete" {
				// Exact terminal readback расходится с immutable snapshot. Оставлять
				// такой candidate вечным predecessor нельзя: один bounded fallback
				// закрывает весь envelope как FAILED. Конкурентный winner всё равно
				// победит через version/attempt/generation OCC.
				input.Outcome = "fail"
				input.TerminalReasonCode = "recovery_readback_mismatch"
				input.Principal.AuthorityDigest = workspaceRecoveryIntentDigest(input.ResourceID,
					input.ExpectedVersion, input.Outcome, input.TerminalReasonCode)
				input.IdempotencyKey = "workspace-recovery-" + input.Principal.AuthorityDigest
				if candidate.Kind == enum.KindWorkspaceBackup {
					_, err = service.ReconcileWorkspaceBackupTerminal(ctx, input)
				} else {
					_, err = service.ReconcileWorkspaceRestoreTerminal(ctx, input)
				}
			}
		}
		if err != nil && !errors.Is(err, domainerrs.ErrNotFound) &&
			!errors.Is(err, domainerrs.ErrVersionMismatch) &&
			!errors.Is(err, domainerrs.ErrStateConflict) {
			return fmt.Errorf("workspace recovery reconcile: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func workspaceRecoveryIntentDigest(resourceID string, version uint64, outcome, reason string) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		resourceID, fmt.Sprint(version), outcome, reason,
	}, "\x00")))
	return fmt.Sprintf("%x", digest[:])
}

func reconcileReadiness(
	ctx context.Context,
	checker *readinessChecker,
	readiness *serviceruntime.Readiness,
	metrics *observability.Metrics,
	config Config,
) error {
	ticker := time.NewTicker(config.ReadinessInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			checkCtx, cancel := context.WithTimeout(ctx, config.ReadinessTimeout)
			_, err := checker.Check(checkCtx)
			cancel()
			if err != nil {
				readiness.Set(false, "dependency")
				metrics.SetReady(false)
				continue
			}
			readiness.Set(true, "ready")
			metrics.SetReady(true)
		}
	}
}

func proofOrAuthorizationInterceptor(
	verifier internalrpcauthorityv1.AuthorizationVerifierServiceClient,
) stdgrpc.UnaryServerInterceptor {
	authorization := authorityclient.VerifierUnaryServerInterceptor(verifier)
	return func(
		ctx context.Context,
		request any,
		info *stdgrpc.UnaryServerInfo,
		handler stdgrpc.UnaryHandler,
	) (any, error) {
		if strings.HasPrefix(
			info.FullMethod,
			"/internalrpcauthority.v1.AuthorityProofResolverService/",
		) {
			return handler(ctx, request)
		}
		return authorization(ctx, request, info, handler)
	}
}

func strictRequestInterceptor(
	ctx context.Context,
	request any,
	_ *stdgrpc.UnaryServerInfo,
	handler stdgrpc.UnaryHandler,
) (any, error) {
	if grpcserver.HasMalformedProto(request) {
		return nil, status.Error(codes.InvalidArgument, "request protobuf is invalid")
	}
	return handler(ctx, request)
}

func observableMethods() map[string]string {
	methods := map[string]string{
		internalrpcauthorityv1.AuthorityProofResolverService_ResolveAuthorityProof_FullMethodName: "authority.resolve",
		internalrpcauthorityv1.AuthorityProofResolverService_CheckReadiness_FullMethodName:        "authority.readiness",
	}
	for _, descriptor := range controlplanev1.ControlPlaneService_ServiceDesc.Methods {
		methods["/controlplane.v1.ControlPlaneService/"+descriptor.MethodName] =
			"controlplane." + strings.ToLower(descriptor.MethodName)
	}
	return methods
}

func (state *runtimeState) shutdown(background context.Context) error {
	if state.readiness != nil {
		state.readiness.Set(false, "stopping")
	}
	if state.metrics != nil {
		state.metrics.SetReady(false)
	}
	var operations []serviceruntime.ShutdownOperation
	if state.workers != nil {
		state.workers.Stop()
		operations = append(operations, serviceruntime.ShutdownOperation{
			Name:    "join workers",
			Timeout: state.config.ShutdownTimeout,
			Run:     state.workers.Wait,
		})
	}
	if state.grpcServer != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{
			Name:    "stop gRPC",
			Timeout: state.config.ShutdownTimeout,
			Run: func(ctx context.Context) error {
				done := make(chan struct{})
				go func() {
					state.grpcServer.GracefulStop()
					close(done)
				}()
				select {
				case <-done:
					return nil
				case <-ctx.Done():
					state.grpcServer.Stop()
					return ctx.Err()
				}
			},
		})
	}
	if state.httpServer != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{
			Name:    "stop technical HTTP",
			Timeout: state.config.ShutdownTimeout,
			Run:     state.httpServer.Shutdown,
		})
	}
	if state.publisher != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{
			Name:    "drain NATS",
			Timeout: state.config.ShutdownTimeout,
			Run: func(context.Context) error {
				return state.publisher.Close()
			},
		})
	}
	if state.cache != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{
			Name:    "close Redis",
			Timeout: state.config.ShutdownTimeout,
			Run: func(context.Context) error {
				return state.cache.Close()
			},
		})
	}
	if state.oidc != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{
			Name:    "close OIDC",
			Timeout: state.config.ShutdownTimeout,
			Run: func(context.Context) error {
				state.oidc.Close()
				return nil
			},
		})
	}
	if state.authority != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{
			Name:    "close authority client",
			Timeout: state.config.ShutdownTimeout,
			Run: func(context.Context) error {
				return state.authority.Close()
			},
		})
	}
	if state.runtimePool != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{
			Name:    "close runtime PostgreSQL",
			Timeout: state.config.ShutdownTimeout,
			Run: func(context.Context) error {
				state.runtimePool.Close()
				return nil
			},
		})
	}
	if state.relayPool != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{
			Name:    "close relay PostgreSQL",
			Timeout: state.config.ShutdownTimeout,
			Run: func(context.Context) error {
				state.relayPool.Close()
				return nil
			},
		})
	}
	if state.telemetry != nil {
		operations = append(
			operations,
			serviceruntime.ShutdownOperation{
				Name:    "shutdown tracing",
				Timeout: state.config.ShutdownTimeout,
				Run:     state.telemetry.shutdownTracing,
			},
			serviceruntime.ShutdownOperation{
				Name:    "flush Sentry",
				Timeout: state.config.ShutdownTimeout,
				Run:     state.telemetry.flushSentry,
			},
		)
	}
	return serviceruntime.RunShutdown(background, operations...)
}
