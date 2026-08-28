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
	"github.com/codex-k8s/kodex/libs/go/eventing"
	"github.com/codex-k8s/kodex/libs/go/eventing/natsjetstream"
	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/s3store"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/authorityproof"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	platformrepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/repository/postgres/platform"
	platformgrpc "github.com/codex-k8s/kodex/services/internal/control-plane/internal/transport/grpc"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
)

const maximumSecretFileBytes = 64 << 10

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
	if err := objects.Check(startup); err != nil {
		return fmt.Errorf("verify object storage: %w", err)
	}
	repository, err := platformrepository.New(pool, config.DefaultRuntimeProvider, config.DefaultRuntimeModel, objects)
	if err != nil {
		return fmt.Errorf("construct platform repository: %w", err)
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
	service, err := platformservice.New(repository)
	if err != nil {
		return fmt.Errorf("construct platform service: %w", err)
	}
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
	workerGrantTrustFiles := map[string]string{
		"automation-scheduler": config.AutomationGrantTrustFile,
		"integration-gateway":  config.IntegrationGrantTrustFile,
		"runtime-controller":   config.RuntimeGrantTrustFile,
		"role-image-builder":   config.RoleImageBuilderGrantTrustFile,
		"image-admission":      config.ImageAdmissionGrantTrustFile,
		"image-promotion":      config.ImagePromotionGrantTrustFile,
	}
	if config.InteractionGrantTrustFile != "" {
		workerGrantTrustFiles["interaction-gateway"] = config.InteractionGrantTrustFile
	}
	proofService, err := authorityproof.New(startup, service, authorityproof.Config{
		PolicyFile: config.AuthorityPolicyFile, SignerPrivateJWKFile: config.ProofSignerFile,
		SignerTrustFile:       config.ProofSignerTrustFile,
		WorkerGrantTrustFiles: workerGrantTrustFiles,
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
	publisher, err := natsjetstream.New(natsjetstream.Config{
		URL: config.NATSURL, TLSServerName: config.NATSTLSServerName, CAFile: config.NATSCAFile,
		CertificateFile: config.NATSCertificateFile, PrivateKeyFile: config.NATSPrivateKeyFile,
		CredentialsFile: config.NATSCredentialsFile, Stream: config.NATSStream,
		Subjects: []string{"control_plane.run.*.*.events", "control_plane.platform.*.events"},
		Replicas: config.NATSReplicas, MaxMessageBytes: 64 << 10, MaxMessages: 10_000_000,
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
	transport, err := platformgrpc.NewServer(service)
	if err != nil {
		return fmt.Errorf("construct gRPC transport: %w", err)
	}
	proofTransport, err := platformgrpc.NewAuthorityProofServer(proofService)
	if err != nil {
		return fmt.Errorf("construct authority proof transport: %w", err)
	}
	roleImageTransport, err := platformgrpc.NewRoleImageServer(roleImageService)
	if err != nil {
		return fmt.Errorf("construct role image transport: %w", err)
	}
	tlsConfig, err := loadServerTLS(config)
	if err != nil {
		return err
	}
	verifiedUnary := authorityclient.VerifierUnaryServerInterceptor(authority.Verifier())
	grpcServer := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsConfig)),
		grpc.ForceServerCodec(grpcserver.StrictProtoCodec()),
		grpc.ChainUnaryInterceptor(
			grpcserver.ErrorBoundary(grpcserver.ErrorObserverFunc(func(_ context.Context, method string, code codes.Code, _ error) {
				slog.Error("unexpected gRPC failure", "method", method, "code", code.String())
			})),
			routeResolverUnary(verifiedUnary),
			grpcserver.RejectMalformedUnary,
		),
		grpc.ChainStreamInterceptor(
			grpcserver.StreamErrorBoundary(grpcserver.ErrorObserverFunc(func(_ context.Context, method string, code codes.Code, _ error) {
				slog.Error("unexpected gRPC stream failure", "method", method, "code", code.String())
			})),
			authorityclient.VerifierStreamServerInterceptor(authority.Verifier()),
			grpcserver.RejectMalformedStream,
		),
	)
	controlplanev1.RegisterPlatformQueryServiceServer(grpcServer, transport)
	controlplanev1.RegisterPlatformCommandServiceServer(grpcServer, transport)
	controlplanev1.RegisterSystemAssistantServiceServer(grpcServer, transport)
	controlplanev1.RegisterRuntimeWorkServiceServer(grpcServer, transport)
	controlplanev1.RegisterInteractionWorkServiceServer(grpcServer, transport)
	controlplanev1.RegisterRoleImageServiceServer(grpcServer, roleImageTransport)
	internalrpcauthorityv1.RegisterAuthorityProofResolverServiceServer(grpcServer, proofTransport)
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
		monitorOIDCSigningKeys(proofService, slog.Default(), config),
		runOutboxRelay(repository, publisher, shutdownBase, config),
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

func monitorOIDCSigningKeys(service *authorityproof.Service, logger *slog.Logger, config Config) serviceruntime.Worker {
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
				err := service.RefreshOIDC(probe)
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

func readBoundedFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open protected file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maximumSecretFileBytes {
		return nil, errors.New("protected file is invalid")
	}
	value, err := io.ReadAll(io.LimitReader(file, maximumSecretFileBytes+1))
	if err != nil || len(value) == 0 || len(value) > maximumSecretFileBytes {
		return nil, errors.New("read protected file")
	}
	return value, nil
}

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

func technicalServer(address string, readiness *serviceruntime.Readiness) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/livez", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/startupz", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/readyz", func(response http.ResponseWriter, _ *http.Request) {
		ready, reason := readiness.Ready()
		if !ready {
			http.Error(response, reason, http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	})
	return &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 30 * time.Second}
}

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

type readinessStore interface {
	CheckOutbox(context.Context) error
}

type readinessPublisher interface {
	Check(context.Context) error
}

func monitorReadiness(service *platformservice.Service, store readinessStore, publisher readinessPublisher, readiness *serviceruntime.Readiness, logger *slog.Logger, config Config) serviceruntime.Worker {
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
				if readiness.Set(false, "direct_infrastructure_unavailable") {
					logger.WarnContext(ctx, "control-plane readiness lost", "error_class", "direct_infrastructure")
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

type outboxStore interface {
	ClaimOutbox(context.Context, string, int, time.Duration) ([]platformrepository.OutboxItem, error)
	MarkOutboxPublished(context.Context, platformrepository.OutboxItem, eventing.PublishReceipt) error
	MarkOutboxFailed(context.Context, platformrepository.OutboxItem, time.Duration) error
}

type rawPublisher interface {
	PublishRaw(context.Context, string, string, []byte) (eventing.PublishReceipt, error)
}

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
