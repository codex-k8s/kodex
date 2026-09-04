// Package app содержит единственный composition root secret-broker.
package app

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	controlowner "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/controlplane"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/providercredential"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/recovery"
	transportgrpc "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
)

const (
	serviceName      = "secret-broker"
	metricsSubsystem = "secret_broker"
	issuerUID        = 29001
	issuerGID        = 29000
)

func Run(lifecycle, shutdownBase context.Context, buildVersion string) (resultErr error) {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	startup, cancelStartup := context.WithTimeout(lifecycle, 10*time.Second)
	defer cancelStartup()
	telemetryConfig, err := sharedobservability.RuntimeConfigFromEnv(serviceName, buildVersion)
	if err != nil {
		return err
	}
	telemetry, err := sharedobservability.NewRuntime(startup, telemetryConfig)
	if err != nil {
		return err
	}
	logger := telemetry.Logger(os.Stdout)
	methods := map[string]string{
		secretbrokerv1.SecretBrokerService_CreateSecret_FullMethodName:                                                   "create",
		secretbrokerv1.SecretBrokerService_RotateSecret_FullMethodName:                                                   "rotate",
		secretbrokerv1.SecretBrokerService_RevealSecret_FullMethodName:                                                   "reveal",
		secretbrokerv1.SecretBrokerService_RevokeSecret_FullMethodName:                                                   "revoke",
		secretbrokerv1.SecretBrokerService_CheckReadiness_FullMethodName:                                                 "readiness",
		secretbrokerv1.RuntimeCredentialProjectionService_MaterializeRuntimeCredentials_FullMethodName:                   "runtime_credentials_materialize",
		secretbrokerv1.RuntimeCredentialProjectionService_CheckRuntimeCredentialProjectionReadiness_FullMethodName:       "runtime_credentials_readiness",
		sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName:                     "stt_credential_project",
		controlplanev1.ProviderCredentialMaterializerService_CheckProviderCredentialMaterializerReadiness_FullMethodName: "provider_readiness",
		controlplanev1.ProviderCredentialMaterializerService_StartDeviceAuthorization_FullMethodName:                     "provider_device_start",
		controlplanev1.ProviderCredentialMaterializerService_ObserveDeviceAuthorization_FullMethodName:                   "provider_device_observe",
		controlplanev1.ProviderCredentialMaterializerService_MaterializeAPIKey_FullMethodName:                            "provider_api_key",
		controlplanev1.ProviderCredentialMaterializerService_DiscardProviderCredentialMaterialization_FullMethodName:     "provider_discard",
		controlplanev1.ProviderCredentialMaterializerService_CleanupProviderCredential_FullMethodName:                    "provider_cleanup",
	}
	metrics := sharedobservability.NewMetrics(metricsSubsystem, buildVersion, methods)
	recoveryMetrics := recovery.NewMetrics()
	if err := metrics.Register(recoveryMetrics.Collectors()...); err != nil {
		return errors.New("register secret broker recovery metrics")
	}
	defer func() {
		trace, cancelTrace := context.WithTimeout(shutdownBase, 5*time.Second)
		resultErr = errors.Join(resultErr, telemetry.ShutdownTracing(trace))
		cancelTrace()
		sentry, cancelSentry := context.WithTimeout(shutdownBase, 5*time.Second)
		resultErr = errors.Join(resultErr, telemetry.FlushSentry(sentry))
		cancelSentry()
	}()
	control, err := controlplaneclient.Dial(startup, controlplaneclient.Config{
		Target: config.ControlPlaneTarget, TLSServerName: config.ControlPlaneTLSServerName,
		CAFile: config.ControlPlaneCAFile, ClientCertificateFile: config.ControlPlaneCertificateFile,
		ClientPrivateKeyFile: config.ControlPlanePrivateKeyFile, ApplicationGrantFile: config.ApplicationGrantFile,
		ExpectedIssuerUID: issuerUID, ExpectedIssuerGID: issuerGID, DialTimeout: config.RequestTimeout,
		Operations: secretBrokerOperations(),
	})
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, control.Close()) }()
	owner, err := controlowner.New(control, config.ClaimantID)
	if err != nil {
		return err
	}
	store, err := kubernetesstore.InCluster(config.RuntimeNamespace)
	if err != nil {
		return err
	}
	reconciler, err := recovery.New(owner, store, config.RecoveryInterval, config.RecoveryTimeout, logger, recoveryMetrics)
	if err != nil {
		return err
	}
	if err := reconciler.EnableExpiredClaimRecovery(owner, store, config.RuntimeNamespace); err != nil {
		return err
	}
	if err := reconciler.EnableCredentialProjectionRecovery(owner, store); err != nil {
		return err
	}
	appServer, err := providercredential.NewAppServerProcess(config.CodexBinary, config.ProviderAuthorizationRoot)
	if err != nil {
		return err
	}
	providerCredentials, err := providercredential.New(lifecycle, store, appServer, config.ProviderDeviceAuthTTL)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, providerCredentials.Close()) }()
	verifier, err := authorityclient.DialLocal(startup, authorityclient.LocalConfig{
		SocketPath: config.AuthorityVerifierSocket, ExpectedServerUID: config.AuthorityVerifierUID,
		ExpectedServerGID: config.AuthorityVerifierGID, DialTimeout: config.RequestTimeout,
	})
	if err != nil {
		return errors.New("connect provider credential authorization verifier")
	}
	defer func() { resultErr = errors.Join(resultErr, verifier.Close()) }()
	handler, err := transportgrpc.New(owner, store, reconciler, config.MaximumSecretBytes,
		transportgrpc.WithProviderCredentialMaterializer(providerCredentials))
	if err != nil {
		return err
	}
	tlsConfig, err := serverTLS(config)
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsConfig)),
		grpc.ForceServerCodec(grpcserver.StrictProtoCodec()),
		grpc.ChainUnaryInterceptor(
			metrics.UnaryServerInterceptor(),
			telemetry.UnaryServerInterceptor(methods),
			routeProtectedUnary(authorityclient.VerifierUnaryServerInterceptor(verifier.Verifier())),
			grpcserver.ErrorBoundary(grpcserver.ErrorObserverFunc(func(
				_ context.Context,
				method string,
				code codes.Code,
				_ error,
			) {
				logger.Error(
					"unexpected gRPC failure",
					"method", normalizeMethod(methods, method),
					"code", code.String(),
				)
			})),
		),
		grpc.MaxRecvMsgSize(config.MaximumSecretBytes+(64<<10)), grpc.MaxSendMsgSize(config.MaximumSecretBytes+(64<<10)),
	)
	secretbrokerv1.RegisterSecretBrokerServiceServer(grpcServer, handler)
	secretbrokerv1.RegisterRuntimeCredentialProjectionServiceServer(grpcServer, handler)
	sttv1.RegisterTranscriptionCredentialProjectionServiceServer(grpcServer, handler)
	controlplanev1.RegisterProviderCredentialMaterializerServiceServer(grpcServer, handler)
	listener, err := net.Listen("tcp", config.GRPCListen)
	if err != nil {
		return errors.New("listen for secret broker gRPC")
	}
	readiness := serviceruntime.NewReadiness()
	readiness.Set(false, "infrastructure_starting")
	metrics.SetReady(false)
	technical := technicalServer(lifecycle, config, readiness, metrics)
	verifierReadiness := providerVerifierReadiness{client: verifier.Verifier()}
	if err := errors.Join(owner.Check(startup), store.Check(startup), reconciler.ReconcileOnce(startup),
		providerCredentials.Check(startup), verifierReadiness.Check(startup)); err != nil {
		_ = listener.Close()
		return errors.Join(errors.New("secret broker startup barrier failed"), err)
	}
	readiness.Set(true, "ready")
	metrics.SetReady(true)
	workers := serviceruntime.StartWorkers(lifecycle,
		serveGRPC(grpcServer, listener),
		serveHTTP(technical),
		monitorReadiness(readiness, metrics, logger, config.RequestTimeout, owner, store, reconciler, providerCredentials, verifierReadiness),
		reconciler.Worker(),
	)
	err = workers.Wait(context.WithoutCancel(lifecycle))
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "secret broker gRPC server", Timeout: config.ShutdownTimeout, Run: func(context.Context) error { grpcServer.GracefulStop(); return nil }},
		serviceruntime.ShutdownOperation{Name: "secret broker technical HTTP server", Timeout: config.ShutdownTimeout, Run: technical.Shutdown},
		serviceruntime.ShutdownOperation{Name: "secret broker workers", Timeout: config.ShutdownTimeout, Run: workers.Wait},
	)
	return errors.Join(err, shutdownErr)
}

type providerVerifierReadiness struct {
	client internalrpcauthorityv1.AuthorizationVerifierServiceClient
}

func (checker providerVerifierReadiness) Check(ctx context.Context) error {
	response, err := checker.client.CheckReadiness(ctx, &internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessRequest{})
	if err != nil || !response.GetReady() {
		return errors.New("provider credential authorization verifier is not ready")
	}
	return nil
}

func routeProtectedUnary(protected grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		switch info.FullMethod {
		case controlplanev1.ProviderCredentialMaterializerService_CheckProviderCredentialMaterializerReadiness_FullMethodName,
			controlplanev1.ProviderCredentialMaterializerService_StartDeviceAuthorization_FullMethodName,
			controlplanev1.ProviderCredentialMaterializerService_ObserveDeviceAuthorization_FullMethodName,
			controlplanev1.ProviderCredentialMaterializerService_MaterializeAPIKey_FullMethodName,
			controlplanev1.ProviderCredentialMaterializerService_DiscardProviderCredentialMaterialization_FullMethodName,
			controlplanev1.ProviderCredentialMaterializerService_CleanupProviderCredential_FullMethodName,
			secretbrokerv1.RuntimeCredentialProjectionService_MaterializeRuntimeCredentials_FullMethodName,
			secretbrokerv1.RuntimeCredentialProjectionService_CheckRuntimeCredentialProjectionReadiness_FullMethodName,
			sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName:
			return protected(ctx, request, info, handler)
		default:
			return handler(ctx, request)
		}
	}
}

func normalizeMethod(methods map[string]string, method string) string {
	if operation, ok := methods[method]; ok {
		return operation
	}
	return "unknown"
}

type checker interface{ Check(context.Context) error }

func monitorReadiness(readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, logger *slog.Logger, timeout time.Duration, checkers ...checker) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			check, cancel := context.WithTimeout(ctx, timeout)
			var err error
			for _, dependency := range checkers {
				err = errors.Join(err, dependency.Check(check))
			}
			cancel()
			if err == nil {
				metrics.SetReady(true)
				if readiness.Set(true, "ready") {
					logger.InfoContext(ctx, "secret broker readiness restored")
				}
			} else {
				metrics.SetReady(false)
				if readiness.Set(false, "dependency_unavailable") {
					logger.WarnContext(ctx, "secret broker readiness lost", "error_class", "dependency")
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

func secretBrokerOperations() map[string]string {
	return controlplaneclient.SecretBrokerOperations()
}

func technicalServer(lifecycle context.Context, config Config, readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, _ *http.Request) {
		ready, reason := readiness.Ready()
		if !ready {
			http.Error(writer, reason, http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/metrics", metrics.PrometheusHandler())
	return &http.Server{Addr: config.TechnicalListen, Handler: mux, BaseContext: func(net.Listener) context.Context { return lifecycle },
		ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
}

func serveGRPC(server *grpc.Server, listener net.Listener) serviceruntime.Worker {
	return func(ctx context.Context) error {
		done := make(chan error, 1)
		go func() { done <- server.Serve(listener) }()
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			server.GracefulStop()
			return <-done
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
			_ = server.Close()
			return nil
		}
	}
}

func serverTLS(config Config) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(config.ServerCertificateFile, config.ServerPrivateKeyFile)
	if err != nil {
		return nil, errors.New("load secret broker server identity")
	}
	ca, err := os.ReadFile(config.ClientCAFile)
	if err != nil || len(ca) == 0 || len(ca) > 1<<20 {
		return nil, errors.New("read secret broker client CA")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, errors.New("parse secret broker client CA")
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 {
				return errors.New("secret broker client certificate is unverified")
			}
			for _, identity := range state.VerifiedChains[0][0].URIs {
				for _, expected := range config.ExpectedClientSPIFFEIDs {
					if subtle.ConstantTimeCompare([]byte(identity.String()), []byte(expected)) == 1 {
						return nil
					}
				}
			}
			return errors.New("secret broker client identity is invalid")
		},
	}, nil
}
