// Package app собирает web-first control-plane и владеет его lifecycle.
package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/eventing"
	"github.com/codex-k8s/kodex/libs/go/eventing/natsjetstream"
	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/libs/go/httpserver"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/s3store"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/credentialmaterializer"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/authorityproof"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/maintenance/assistantconversationpurge"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/maintenance/projectpurge"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/maintenance/providercredentialcleanup"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/maintenance/providermodelcatalog"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/providercredentialclient"
	platformrepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/repository/postgres/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/skillscanclient"
	platformgrpc "github.com/codex-k8s/kodex/services/internal/control-plane/internal/transport/grpc"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
)

// maximumSecretFileBytes bounds protected key/DSN/CA reads to sixty-four KiB through the common
// nonempty regular-file reader.
// maximumSecretFileBytes ограничивает чтение защищённых key, DSN и CA шестьюдесятью четырьмя KiB
// через общий reader непустого regular file.
const maximumSecretFileBytes = 64 << 10

// Run loads configuration and constructs owner/storage/identity dependencies, bootstraps and
// checks broker/outbox before starting workers; shutdown marks unready, stops workers and attempts
// independently bounded HTTP, gRPC and worker cleanup.
// Run загружает конфигурацию и создаёт owner, storage и identity зависимости, выполняет bootstrap
// и broker/outbox проверки до запуска workers; shutdown снимает readiness, останавливает workers и
// выполняет отдельно ограниченные HTTP, gRPC и worker cleanup.
func Run(lifecycle, shutdownBase context.Context, _ string) error {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	startup, cancelStartup := context.WithTimeout(lifecycle, config.StartupTimeout)
	defer cancelStartup()
	pool, err := openPostgres(startup, config)
	if err != nil {
		return err
	}
	defer pool.Close()
	accessKey, err := readBoundedFile(config.ObjectStorageAccessKeyFile)
	if err != nil {
		return fmt.Errorf("read object storage access key: %w", err)
	}
	secretKey, err := readBoundedFile(config.ObjectStorageSecretKeyFile)
	if err != nil {
		return fmt.Errorf("read object storage secret key: %w", err)
	}
	objects, err := s3store.New(startup, s3store.Config{
		Endpoint: config.ObjectStorageEndpoint, Region: config.ObjectStorageRegion, Bucket: config.ObjectStorageBucket,
		AccessKeyID: strings.TrimSpace(string(accessKey)), SecretKey: strings.TrimSpace(string(secretKey)),
		UsePathStyle: config.ObjectStorageUsePathStyle,
	})
	if err != nil {
		return fmt.Errorf("construct object storage: %w", err)
	}
	archiveObjects, err := s3store.New(startup, s3store.Config{
		Endpoint: config.ObjectStorageEndpoint, Region: config.ObjectStorageRegion, Bucket: config.SessionArchiveStorageBucket,
		AccessKeyID: strings.TrimSpace(string(accessKey)), SecretKey: strings.TrimSpace(string(secretKey)),
		UsePathStyle: config.ObjectStorageUsePathStyle,
	})
	if err != nil {
		return fmt.Errorf("construct session archive storage: %w", err)
	}
	repository, err := platformrepository.New(pool, config.DefaultRuntimeProvider, config.DefaultRuntimeModel, objects)
	if err != nil {
		return fmt.Errorf("construct platform repository: %w", err)
	}
	if err := repository.ConfigureProjectPurgeStorage(config.ObjectStorageEndpoint, config.ObjectStorageRegion, config.ObjectStorageBucket, config.ObjectStorageUsePathStyle); err != nil {
		return fmt.Errorf("configure project purge storage: %w", err)
	}
	if err := repository.ConfigureRPCProfile(config.RPCProfile); err != nil {
		return fmt.Errorf("configure repository RPC profile: %w", err)
	}
	if err := repository.ConfigureRuntimeSecrets(config.RuntimeSecretNamespace); err != nil {
		return fmt.Errorf("configure runtime secrets: %w", err)
	}
	if err := repository.ConfigureRuntimeSecretStaging(config.RuntimeSecretStagingNamespace); err != nil {
		return fmt.Errorf("configure runtime secret staging: %w", err)
	}
	emailProjection, err := initializeEmailProjection(startup, repository, config)
	if err != nil {
		return fmt.Errorf("initialize email projection: %w", err)
	}
	integrationEgressProjection := &integrationEgressProjection{repository: repository,
		baseDigest: config.EmailGatewayPolicyDigest, kubernetesTimeout: config.KubernetesAPITimeout}
	skillScanner, err := skillscanclient.New(config.SkillScannerSocket, config.SkillScannerTimeout)
	if err != nil {
		return fmt.Errorf("construct skill scanner: %w", err)
	}
	if err := repository.ConfigureSkillScanner(skillScanner); err != nil {
		return fmt.Errorf("configure skill scanner: %w", err)
	}
	if err := repository.ConfigureArtifactScanner(skillScanner); err != nil {
		return fmt.Errorf("configure artifact scanner: %w", err)
	}
	if err := repository.ConfigureProviderCredential(platformrepository.ProviderCredentialConfig{
		SecretName: config.DefaultProviderSecretName, SecretUID: config.DefaultProviderSecretUID,
		SecretResourceVersion: config.DefaultProviderSecretVersion,
		ContentSHA256:         config.DefaultProviderCredentialSHA256,
	}); err != nil {
		return fmt.Errorf("configure default provider credential: %w", err)
	}
	leaseSigningKey, err := readBoundedFile(config.LeaseSigningKeyFile)
	if err != nil {
		return fmt.Errorf("read role image lease signing key: %w", err)
	}
	if err := repository.ConfigureRoleImages(platformrepository.RoleImageConfig{
		PolicyRevision: config.ImagePolicyRevision, PolicySHA256: config.ImagePolicySHA256,
		RoleRuntimeContractRevision: config.RoleRuntimeContractRevision, RoleRuntimeContractSHA256: config.RoleRuntimeContractSHA256,
		BuildLeaseDuration: config.ImageBuildLeaseDuration, AdmissionClaimTTL: config.ImageAdmissionClaimTTL,
		PromotionClaimTTL: config.ImagePromotionClaimTTL, MaximumAttempts: config.ImageMaximumAttempts,
		StagingRepository: config.StagingImageRepository, PromotedRepository: config.PromotedImageRepository,
		DefaultImageReference: config.DefaultRoleImageReference, LeaseSigningKey: leaseSigningKey,
	}); err != nil {
		return fmt.Errorf("configure role image lifecycle: %w", err)
	}
	credentialMaterializer, err := credentialmaterializer.InCluster(
		config.IntegrationCredentialNamespace,
		config.IntegrationCredentialSecretName,
		config.KubernetesAPITimeout,
	)
	if err != nil {
		return fmt.Errorf("construct integration credential materializer: %w", err)
	}
	providerControl, err := controlplaneclient.Dial(startup, controlplaneclient.Config{
		RPCProfile: config.RPCProfile, CallerWorkload: "control-plane",
		Target: config.SecretBrokerTarget, TLSServerName: config.SecretBrokerTLSServerName,
		CAFile: config.ClientCAFile, ClientCertificateFile: config.ServerCertificateFile,
		ClientPrivateKeyFile: config.ServerPrivateKeyFile, ApplicationGrantFile: config.ProviderApplicationGrantFile,
		ResolverTarget: config.ProviderResolverTarget, ResolverTLSServerName: config.ProviderResolverTLSServerName,
		ResolverCAFile: config.ProviderResolverCAFile, ExpectedIssuerUID: config.ProviderIssuerUID,
		ExpectedIssuerGID: config.ProviderIssuerGID, DialTimeout: config.ReadinessTimeout,
		Operations: controlplaneclient.ProviderCredentialMaterializerOperations(),
	})
	if err != nil {
		return fmt.Errorf("construct provider credential materializer client: %w", err)
	}
	defer providerControl.Close()
	providerMaterializer, err := providercredentialclient.New(providerControl)
	if err != nil {
		return fmt.Errorf("construct provider credential materializer adapter: %w", err)
	}
	emailCredentials, err := constructEmailCredentialMaterializer(config)
	if err != nil {
		return fmt.Errorf("construct email credential materializer: %w", err)
	}
	service, err := platformservice.New(repository,
		platformservice.WithCredentialMaterializer(credentialMaterializer),
		platformservice.WithEmailCredentialMaterializer(emailCredentials),
		platformservice.WithProviderCredentialMaterializer(providerMaterializer),
	)
	if err != nil {
		return fmt.Errorf("construct platform service: %w", err)
	}
	integrationEgressProjection.ready = service.Ready
	if err := service.Bootstrap(startup); err != nil {
		return fmt.Errorf("bootstrap platform: %w", err)
	}
	roleEnvironmentCatalog, err := loadRoleEnvironmentCatalog(config)
	if err != nil {
		return fmt.Errorf("load role environment catalog: %w", err)
	}
	roleImageService, err := roleimageservice.New(repository, roleEnvironmentCatalog)
	if err != nil {
		return fmt.Errorf("construct role image service: %w", err)
	}
	repository.ConfigureRoleImageCatalog(roleEnvironmentCatalog)
	var proofService *authorityproof.Service
	var verifiedUnary grpc.UnaryServerInterceptor
	var verifiedStream grpc.StreamServerInterceptor
	var refreshOIDC func(context.Context) error
	var transportOptions []grpc.ServerOption
	if config.RPCProfile == "trusted-cluster" {
		var verifier *oidcverifier.Verifier
		verifiedUnary, verifiedStream, verifier, err = trustedClusterReader(startup, config, service)
		if err != nil {
			return fmt.Errorf("construct trusted cluster reader: %w", err)
		}
		defer verifier.Close()
		refreshOIDC = verifier.Refresh
	} else {
		workerGrantTrustFiles := workerGrantTrustFilesFor(config)
		proofService, err = authorityproof.New(startup, service, authorityproof.Config{
			PolicyFile: config.AuthorityPolicyFile, SignerPrivateJWKFile: config.ProofSignerFile,
			SignerTrustFile:          config.ProofSignerTrustFile,
			WorkerGrantTrustFiles:    workerGrantTrustFiles,
			ReadinessWorkerGrantFile: config.ProviderApplicationGrantFile,
			OIDC: oidcverifier.Config{
				Issuer: config.OIDCIssuer, Audience: config.OIDCAudience, JWKSURL: config.OIDCJWKSURL,
				ConnectAddress: config.OIDCConnectAddress, TLSServerName: config.OIDCTLSServerName,
				CAFile: config.OIDCCAFile, Timeout: config.ReadinessTimeout,
			},
		})
		if err != nil {
			return fmt.Errorf("construct authority proof resolver: %w", err)
		}
		defer proofService.Close()
		authority, err := authorityclient.DialLocal(startup, authorityclient.LocalConfig{SocketPath: config.AuthorityVerifierSocket, ExpectedServerUID: config.AuthorityVerifierUID, ExpectedServerGID: config.AuthorityVerifierGID, DialTimeout: 2 * time.Second})
		if err != nil {
			return fmt.Errorf("connect authorization verifier: %w", err)
		}
		defer authority.Close()
		refreshOIDC = proofService.RefreshOIDC
		verifiedUnary = authorityclient.VerifierUnaryServerInterceptor(authority.Verifier())
		verifiedStream = authorityclient.VerifierStreamServerInterceptor(authority.Verifier(), controlplanev1.RuntimeWorkService_StreamExecutionArtifact_FullMethodName)
		verifiedUnary, verifiedStream, err = serviceIdentityReader(service, proofService, verifiedUnary, verifiedStream)
		if err != nil {
			return fmt.Errorf("construct service identity reader: %w", err)
		}
		verifiedUnary = routeResolverUnary(verifiedUnary)
		tlsConfig, err := loadServerTLS(config)
		if err != nil {
			return err
		}
		transportOptions = append(transportOptions, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}
	publisher, err := natsjetstream.New(natsjetstream.Config{
		URL: config.NATSURL, TLSServerName: config.NATSTLSServerName, CAFile: config.NATSCAFile,
		CertificateFile: config.NATSCertificateFile, PrivateKeyFile: config.NATSPrivateKeyFile,
		CredentialsFile: config.NATSCredentialsFile, Stream: config.NATSStream,
		Subjects: []string{"control_plane.run.*.*.events", "control_plane.platform.*.events"},
		Replicas: config.NATSReplicas, MaxMessageBytes: runtimecontract.MaximumControlPlaneStreamMessageBytes, MaxMessages: 10_000_000,
		MaxBytes: config.NATSMaxBytes, MaxPerSubject: 1_000_000, MaxAge: 30 * 24 * time.Hour,
		DuplicateWindow: 2 * time.Minute, ConnectTimeout: 2 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("construct NATS publisher: %w", err)
	}
	defer publisher.Close()
	if err := publisher.Check(startup); err != nil {
		return fmt.Errorf("verify NATS stream: %w", err)
	}
	if err := repository.CheckOutbox(startup); err != nil {
		return fmt.Errorf("verify outbox: %w", err)
	}
	transport, err := platformgrpc.NewServer(service, roleImageService)
	if err != nil {
		return fmt.Errorf("construct gRPC transport: %w", err)
	}
	roleImageTransport, err := platformgrpc.NewRoleImageServer(roleImageService)
	if err != nil {
		return fmt.Errorf("construct role image transport: %w", err)
	}
	serverOptions := append(transportOptions,
		grpc.MaxRecvMsgSize(8<<20),
		grpc.ForceServerCodec(grpcserver.StrictProtoCodec()),
		grpc.ChainUnaryInterceptor(
			grpcserver.ErrorBoundary(grpcserver.ErrorObserverFunc(func(_ context.Context, method string, code codes.Code, _ error) {
				slog.Error("unexpected gRPC failure", "method", method, "code", code.String())
			})),
			verifiedUnary,
			grpcserver.RejectMalformedUnary,
		),
		grpc.ChainStreamInterceptor(
			grpcserver.StreamErrorBoundary(grpcserver.ErrorObserverFunc(logUnexpectedGRPCStreamFailure)),
			verifiedStream,
			grpcserver.RejectMalformedStream,
		),
	)
	grpcServer := grpc.NewServer(serverOptions...)
	controlplanev1.RegisterPlatformQueryServiceServer(grpcServer, transport)
	sttv1.RegisterTranscriptionPolicyProjectionServiceServer(grpcServer, transport)
	if config.RPCProfile == transportprofile.TrustedCluster {
		sttv1.RegisterTranscriptionAuthorityServiceServer(grpcServer, transport)
	}
	controlplanev1.RegisterPlatformCommandServiceServer(grpcServer, transport)
	controlplanev1.RegisterSystemAssistantServiceServer(grpcServer, transport)
	controlplanev1.RegisterRuntimeWorkServiceServer(grpcServer, transport)
	controlplanev1.RegisterManagedConfigurationSourceWorkServiceServer(grpcServer, transport)
	controlplanev1.RegisterManagedConfigurationGitWriteBackWorkServiceServer(grpcServer, transport)
	controlplanev1.RegisterRuntimeSecretWorkServiceServer(grpcServer, transport)
	controlplanev1.RegisterProviderCredentialRenewalWorkServiceServer(grpcServer, transport)
	controlplanev1.RegisterRuntimeSecretDraftWorkServiceServer(grpcServer, transport)
	controlplanev1.RegisterSessionArchiveWorkServiceServer(grpcServer, transport)
	controlplanev1.RegisterInteractionWorkServiceServer(grpcServer, transport)
	controlplanev1.RegisterAccessServiceServer(grpcServer, transport)
	controlplanev1.RegisterRoleImageServiceServer(grpcServer, roleImageTransport)
	if proofService != nil {
		proofTransport, err := platformgrpc.NewAuthorityProofServer(proofService)
		if err != nil {
			return fmt.Errorf("construct authority proof transport: %w", err)
		}
		internalrpcauthorityv1.RegisterAuthorityProofResolverServiceServer(grpcServer, proofTransport)
	}
	cleanupClaimHealth := serviceruntime.NewReadiness()
	cleanupClaimHealth.Set(true, "ready")
	cleanupWorker, err := providercredentialcleanup.New(
		repository,
		providerMaterializer,
		cleanupClaimHealth,
		slog.Default(),
		providercredentialcleanup.Config{
			LeaseOwner: config.InstanceID, BatchSize: config.ProviderCleanupBatchSize,
			PollInterval:     config.ProviderCleanupPollInterval,
			OperationTimeout: config.ProviderCleanupTimeout,
		},
	)
	if err != nil {
		return fmt.Errorf("construct provider credential cleanup worker: %w", err)
	}
	catalogHealth := serviceruntime.NewReadiness()
	catalogWorker, catalogErr := providermodelcatalog.New(repository, providerMaterializer, service.Ready, config.InstanceID, catalogHealth, slog.Default())
	if catalogErr != nil {
		return fmt.Errorf("construct provider model catalog worker: %w", catalogErr)
	}
	renewalWorker, err := providermodelcatalog.NewRenewalWorker(repository, providerMaterializer, service.Ready, config.InstanceID, slog.Default())
	if err != nil {
		return errors.New("construct provider credential renewal worker")
	}
	listener, err := net.Listen("tcp", config.GRPCListen)
	if err != nil {
		return errors.New("listen control-plane gRPC")
	}
	readiness := serviceruntime.NewReadiness()
	technical := technicalServer(config.TechnicalListen, readiness)
	workers := serviceruntime.StartWorkers(lifecycle,
		serveGRPC(grpcServer, listener),
		serveHTTP(technical),
		monitorReadiness(service, repository, publisher, readiness, slog.Default(), config),
		emailProjection.Run,
		integrationEgressProjection.Run,
		monitorOIDCSigningKeys(refreshOIDC, slog.Default(), config),
		runOutboxRelay(repository, publisher, shutdownBase, config),
		cleanupWorker.Run,
		catalogWorker.Run,
		renewalWorker.Run,
		runAgentAvatarCleanup(repository, slog.Default()),
		projectpurge.Run(repository, objects, archiveObjects, config.RuntimeSecretNamespace, slog.Default()),
		assistantconversationpurge.Run(repository, slog.Default()),
	)
	workerDone := make(chan error, 1)
	go func() { workerDone <- workers.Wait(lifecycle) }()
	var workerErr error
	select {
	case <-lifecycle.Done():
	case workerErr = <-workerDone:
	}
	readiness.Set(false, "shutting_down")
	workers.Stop()
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "technical HTTP server", Timeout: config.ShutdownTimeout, Run: technical.Shutdown},
		serviceruntime.ShutdownOperation{Name: "gRPC server", Timeout: config.ShutdownTimeout, Run: func(ctx context.Context) error { return gracefulStop(ctx, grpcServer) }},
		serviceruntime.ShutdownOperation{Name: "workers", Timeout: config.ShutdownTimeout, Run: workers.Wait},
	)
	return errors.Join(workerErr, shutdownErr)
}

// runAgentAvatarCleanup returns a worker that immediately cleans up to fifty expired uploads and
// repeats each minute, reporting only a fixed failure class and returning on cancellation.
// runAgentAvatarCleanup возвращает worker, сразу очищающий до пятидесяти истёкших uploads и
// повторяющий цикл каждую минуту с фиксированным классом отказа и завершением при отмене.
func runAgentAvatarCleanup(repository interface {
	CleanupExpiredAgentAvatarUploads(context.Context, int32) error
}, logger *slog.Logger) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			if err := repository.CleanupExpiredAgentAvatarUploads(ctx, 50); err != nil && !errors.Is(err, context.Canceled) {
				logger.WarnContext(ctx, "agent avatar cleanup iteration failed", "error_class", "avatar_cleanup")
			}
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	}
}

// routeResolverUnary routes only the exact proof-resolver resolve/readiness RPCs directly to their
// own handler checks, keeping every other unary method behind the protected interceptor.
// routeResolverUnary направляет только точные proof-resolver RPC resolve и readiness прямо к
// собственным проверкам handler, сохраняя protected interceptor для остальных unary методов.
func routeResolverUnary(protected grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		switch info.FullMethod {
		case internalrpcauthorityv1.AuthorityProofResolverService_ResolveAuthorityProof_FullMethodName,
			internalrpcauthorityv1.AuthorityProofResolverService_CheckReadiness_FullMethodName:
			return handler(ctx, request)
		default:
			return protected(ctx, request, info, handler)
		}
	}
}

// monitorOIDCSigningKeys periodically refreshes signing keys under a bounded child context and
// logs only transitions into or out of degraded refresh, returning the caller cancellation error.
// monitorOIDCSigningKeys периодически обновляет signing keys под ограниченным дочерним context и
// записывает лишь переходы в degraded refresh и обратно с возвратом ошибки отмены caller.
func monitorOIDCSigningKeys(refresh func(context.Context) error, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.OIDCRefreshInterval)
		defer ticker.Stop()
		degraded := false
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				probe, cancel := context.WithTimeout(ctx, config.ReadinessTimeout)
				err := refresh(probe)
				cancel()
				if err != nil && !degraded {
					logger.Warn("OIDC signing-key refresh degraded")
					degraded = true
				} else if err == nil && degraded {
					logger.Info("OIDC signing-key refresh restored")
					degraded = false
				}
			}
		}
	}
}

// readBoundedFile reads one nonempty regular protected file using the fixed secret-file byte limit
// and safe errors without exposing its contents.
// readBoundedFile читает один непустой regular protected file с фиксированным байтовым лимитом
// секретного файла и безопасными ошибками без раскрытия содержимого.
func readBoundedFile(path string) ([]byte, error) {
	return readBoundedFileLimit(path, maximumSecretFileBytes)
}

// readBoundedFileLimit checks an opened file is regular and within the positive size bound, then
// limits reading to maximum plus one; it does not enforce ownership/mode or reject symlink
// traversal.
// readBoundedFileLimit проверяет regular тип открытого файла и размер в положительном лимите,
// затем ограничивает чтение maximum плюс одним байтом; владение, mode и запрет прохода symlink не
// проверяются.
func readBoundedFileLimit(path string, maximum int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open protected file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maximum {
		return nil, errors.New("protected file is invalid")
	}
	value, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || len(value) == 0 || int64(len(value)) > maximum {
		return nil, errors.New("read protected file")
	}
	return value, nil
}

// openPostgres loads the bounded DSN/CA, wipes the raw DSN byte slice after parsing, configures
// exact TLS13 roots/SNI and pool size, and closes a pool whose initial ping fails.
// openPostgres загружает ограниченные DSN и CA, очищает raw DSN срез после parsing, задаёт точные
// TLS13 roots и SNI и размер pool и закрывает pool при отказе начального ping.
func openPostgres(ctx context.Context, config Config) (*pgxpool.Pool, error) {
	rawDSN, err := readBoundedFile(config.PostgresDSNFile)
	if err != nil {
		return nil, errors.New("load PostgreSQL configuration")
	}
	poolConfig, err := pgxpool.ParseConfig(strings.TrimSpace(string(rawDSN)))
	for index := range rawDSN {
		rawDSN[index] = 0
	}
	if err != nil {
		return nil, errors.New("parse PostgreSQL configuration")
	}
	ca, err := readBoundedFile(config.PostgresCAFile)
	if err != nil {
		return nil, errors.New("load PostgreSQL CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("parse PostgreSQL CA")
	}
	poolConfig.ConnConfig.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: config.PostgresTLSServerName}
	poolConfig.MaxConns = config.PostgresMaxConnections
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, errors.New("open PostgreSQL pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("verify PostgreSQL connection")
	}
	return pool, nil
}

// loadServerTLS loads the configured server key pair and client CA into TLS13 mutual-client
// verification without changing certificate paths or trust policy on failure.
// loadServerTLS загружает заданные server key pair и client CA в TLS13 проверку client
// certificates без изменения certificate путей либо trust policy при отказе.
func loadServerTLS(config Config) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(config.ServerCertificateFile, config.ServerPrivateKeyFile)
	if err != nil {
		return nil, errors.New("load control-plane server certificate")
	}
	ca, err := readBoundedFile(config.ClientCAFile)
	if err != nil {
		return nil, errors.New("load internal client CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("parse internal client CA")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert}, nil
}

// technicalServer builds the existing technical probe mux with snapshot readiness and the
// control-plane header/read/write/idle budgets without opening a listener.
// technicalServer создаёт прежний technical probe mux с snapshot readiness и control-plane
// бюджетами header, read, write и idle без открытия listener.
func technicalServer(address string, readiness *serviceruntime.Readiness) *http.Server {
	mux := httpserver.ProbeMux(readiness.Ready)
	return &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 30 * time.Second}
}

// serveGRPC returns a worker that waits for Serve or caller cancellation; server shutdown and
// joining the serving goroutine remain the composition-root responsibility.
// serveGRPC возвращает worker ожидания Serve либо отмены caller; shutdown сервера и ожидание
// serving goroutine остаются ответственностью composition root.
func serveGRPC(server *grpc.Server, listener net.Listener) serviceruntime.Worker {
	return func(ctx context.Context) error {
		done := make(chan error, 1)
		go func() { done <- server.Serve(listener) }()
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// serveHTTP returns a worker that treats normal HTTP server closure as success or returns
// Serve/cancellation errors, leaving actual shutdown to the owner.
// serveHTTP возвращает worker, считающий штатное закрытие HTTP сервера успехом либо возвращающий
// ошибки Serve и отмены; фактический shutdown остаётся владельцу.
func serveHTTP(server *http.Server) serviceruntime.Worker {
	return func(ctx context.Context) error {
		done := make(chan error, 1)
		go func() { done <- server.ListenAndServe() }()
		select {
		case err := <-done:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// readinessStore exposes only authoritative outbox infrastructure readiness for the root readiness
// loop without adding optional projection dependencies.
// readinessStore предоставляет только авторитетную readiness инфраструктуры outbox для root
// readiness цикла без добавления optional projection зависимостей.
type readinessStore interface {
	// CheckOutbox checks the owned durable outbox path under the supplied readiness budget without
	// claiming or publishing work.
	// CheckOutbox проверяет собственный durable outbox путь под переданным readiness бюджетом без
	// claim либо публикации работы.
	CheckOutbox(context.Context) error
}

// readinessPublisher exposes the broker readiness check used with owned PostgreSQL/outbox health
// by the root process readiness loop.
// readinessPublisher предоставляет проверку broker readiness, используемую вместе с owned
// PostgreSQL и outbox health root циклом готовности процесса.
type readinessPublisher interface {
	// Check checks broker durability/readiness through the configured publisher rather than inferring
	// health from a prior successful publication.
	// Check проверяет broker durability и readiness через заданный publisher вместо вывода здоровья из
	// прежней успешной публикации.
	Check(context.Context) error
}

// readinessOwner supplies owned-state service readiness independently of external optional
// adapters, projections or cleanup workers.
// readinessOwner предоставляет service readiness собственного состояния независимо от внешних
// optional adapters, проекций либо cleanup workers.
type readinessOwner interface {
	// Ready checks the owner service readiness within the supplied context without declaring unrelated
	// worker capabilities healthy.
	// Ready проверяет readiness service владельца в переданном context без объявления исправными
	// несвязанных worker capabilities.
	Ready(context.Context) error
}

// monitorReadiness immediately and periodically evaluates owned service, outbox and broker checks
// under one budget, updates the snapshot and logs only state transitions; optional paths retain
// separate health.
// monitorReadiness сразу и периодически проверяет owned service, outbox и broker под единым
// бюджетом, обновляет snapshot и записывает лишь переходы состояния; optional пути сохраняют
// отдельное здоровье.
func monitorReadiness(service readinessOwner, store readinessStore, publisher readinessPublisher, readiness *serviceruntime.Readiness, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.ReadinessInterval)
		defer ticker.Stop()
		for {
			check, cancel := context.WithTimeout(ctx, config.ReadinessTimeout)
			err := errors.Join(service.Ready(check), store.CheckOutbox(check), publisher.Check(check))
			cancel()
			if err == nil {
				if readiness.Set(true, "ready") {
					logger.InfoContext(ctx, "control-plane readiness restored")
				}
			} else {
				if readiness.Set(false, "primary_infrastructure_unavailable") {
					logger.WarnContext(ctx, "control-plane readiness lost", "error_class", "postgresql_or_nats")
				}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

// outboxStore owns leased outbox claims and durable publication/failure finalization, keeping
// business persistence outside the relay worker.
// outboxStore владеет leased outbox claims и durable фиксацией публикации либо отказа с
// сохранением business persistence вне relay worker.
type outboxStore interface {
	// ClaimOutbox claims a bounded batch for the exact instance and lease duration through the owner
	// transaction.
	// ClaimOutbox получает ограниченный пакет claim для точных instance и lease duration через
	// транзакцию владельца.
	ClaimOutbox(context.Context, string, int, time.Duration) ([]platformrepository.OutboxItem, error)
	// MarkOutboxPublished records the exact leased outbox item and durable broker receipt after
	// publication without recreating the business event.
	// MarkOutboxPublished фиксирует точный leased outbox элемент и durable broker receipt после
	// публикации без повторного создания business event.
	MarkOutboxPublished(context.Context, platformrepository.OutboxItem, eventing.PublishReceipt) error
	// MarkOutboxFailed records failure/backoff for the exact leased outbox item, leaving retry
	// eligibility and ordering to the owner.
	// MarkOutboxFailed фиксирует failure и backoff точного leased outbox элемента, сохраняя retry
	// eligibility и ordering у владельца.
	MarkOutboxFailed(context.Context, platformrepository.OutboxItem, time.Duration) error
}

// rawPublisher publishes an existing serialized event using its exact subject and event ID and
// returns a durable broker receipt.
// rawPublisher публикует существующее сериализованное событие с точными subject и event ID и
// возвращает durable broker receipt.
type rawPublisher interface {
	// PublishRaw publishes the supplied event bytes and idempotent event identity under the caller
	// context without constructing domain state.
	// PublishRaw публикует переданные event bytes и идемпотентную identity события под caller context
	// без построения доменного состояния.
	PublishRaw(context.Context, string, string, []byte) (eventing.PublishReceipt, error)
}

// runOutboxRelay claims batches of up to sixty-four, bounds each publish and independently
// finalizes receipt or capped exponential failure backoff using the supplied noncancelled base,
// stopping on owner errors.
// runOutboxRelay получает claim пакетами до шестидесяти четырёх, ограничивает publish и независимо
// фиксирует receipt либо ограниченный exponential failure backoff через переданную неотменённую
// base с остановкой при owner ошибках.
func runOutboxRelay(store outboxStore, publisher rawPublisher, finalizeBase context.Context, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				items, err := store.ClaimOutbox(ctx, config.InstanceID, 64, config.RelayLeaseDuration)
				if err != nil {
					return err
				}
				for _, item := range items {
					publishCtx, cancelPublish := context.WithTimeout(ctx, config.RelayPublishTimeout)
					receipt, publishErr := publisher.PublishRaw(publishCtx, item.Subject, item.EventID, item.Payload)
					cancelPublish()
					finalizeCtx, cancelFinalize := context.WithTimeout(finalizeBase, config.RelayFinalizeTimeout)
					if publishErr == nil {
						err = store.MarkOutboxPublished(finalizeCtx, item, receipt)
					} else {
						backoff := time.Second << min(item.Attempts, 6)
						err = store.MarkOutboxFailed(finalizeCtx, item, backoff)
					}
					cancelFinalize()
					if err != nil {
						return err
					}
				}
				timer.Reset(config.RelayPollInterval)
			}
		}
	}
}

// gracefulStop attempts gRPC graceful shutdown until the caller budget expires, then forces Stop
// and returns the context error.
// gracefulStop пытается выполнить graceful shutdown gRPC до истечения бюджета caller, затем
// принудительно вызывает Stop и возвращает ошибку context.
func gracefulStop(ctx context.Context, server *grpc.Server) error {
	done := make(chan struct{})
	go func() { server.GracefulStop(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		server.Stop()
		return ctx.Err()
	}
}
