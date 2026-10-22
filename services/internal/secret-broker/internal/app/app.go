// Package app owns the secret-broker composition root and its explicit runtime/security wiring.
// Package app владеет composition root secret-broker и его явными runtime/security связями.
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
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/service/secretdraft"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	businessobservability "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/observability"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/providercredential"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/recovery"
	transportgrpc "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
)

const (
	// serviceName fixes the component identity used for telemetry/runtime configuration, not caller
	// authorization.
	// serviceName фиксирует component identity для telemetry/runtime configuration, не caller authorization.
	serviceName = "secret-broker"
	// metricsSubsystem is the underscore-separated Prometheus subsystem, distinct from the hyphenated service
	// name.
	// metricsSubsystem — Prometheus subsystem с разделителем underscore, отдельно от имени сервиса с дефисом.
	metricsSubsystem = "secret_broker"
	// issuerUID pins the expected application-grant issuer's Unix UID for the control-plane client, not the
	// requesting end user's identity.
	// issuerUID закрепляет ожидаемый Unix UID издателя application grant для control-plane client, не identity
	// конечного пользователя.
	issuerUID = 29001
	// issuerGID pins the expected grant issuer group for local file/identity checks during protected-client
	// composition.
	// issuerGID закрепляет ожидаемую группу издателя grant для локальных file/identity checks при сборке
	// защищённого клиента.
	issuerGID = 29000
)

// Run composes telemetry, protected owner clients, local stores and provider/draft services, then starts
// the worker group after its explicit local startup barrier. It passes caller-owned lifecycle/shutdown
// contexts and joins errors; readiness does not claim remote owner or completed draft recovery, and
// context budgets cannot preempt callbacks that ignore them.
// Run собирает telemetry, защищённые owner clients, локальные stores и provider/draft services, затем
// запускает worker group после явного local startup barrier. Передаёт caller-owned lifecycle/shutdown
// contexts и объединяет ошибки; readiness не доказывает готовность remote owner и завершение draft
// recovery, а context budgets не прерывают игнорирующие их callbacks.
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
		secretbrokerv1.SecretBrokerService_SaveSecretDraft_FullMethodName:                                                "draft_save",
		secretbrokerv1.SecretBrokerService_ValidateSecretDraft_FullMethodName:                                            "draft_validate",
		secretbrokerv1.SecretBrokerService_PublishSecretDraft_FullMethodName:                                             "draft_publish",
		secretbrokerv1.SecretBrokerService_DiscardSecretDraft_FullMethodName:                                             "draft_discard",
		secretbrokerv1.SecretBrokerService_CheckSecretDraftReadiness_FullMethodName:                                      "draft_readiness",
		secretbrokerv1.RuntimeCredentialProjectionService_MaterializeRuntimeCredentials_FullMethodName:                   "runtime_credentials_materialize",
		secretbrokerv1.RuntimeCredentialProjectionService_MaterializeSystemAssistantCredentials_FullMethodName:           "assistant_credentials_materialize",
		secretbrokerv1.RuntimeCredentialProjectionService_CheckRuntimeCredentialProjectionReadiness_FullMethodName:       "runtime_credentials_readiness",
		sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName:                     "stt_credential_project",
		controlplanev1.ProviderCredentialMaterializerService_CheckProviderCredentialMaterializerReadiness_FullMethodName: "provider_readiness",
		controlplanev1.ProviderCredentialMaterializerService_StartDeviceAuthorization_FullMethodName:                     "provider_device_start",
		controlplanev1.ProviderCredentialMaterializerService_ObserveDeviceAuthorization_FullMethodName:                   "provider_device_observe",
		controlplanev1.ProviderCredentialMaterializerService_ObserveProviderModelCatalog_FullMethodName:                  "provider_catalog_observe",
		controlplanev1.ProviderCredentialMaterializerService_MaterializeAPIKey_FullMethodName:                            "provider_api_key",
		controlplanev1.ProviderCredentialMaterializerService_DiscardProviderCredentialMaterialization_FullMethodName:     "provider_discard",
		controlplanev1.ProviderCredentialMaterializerService_CleanupProviderCredential_FullMethodName:                    "provider_cleanup",
		controlplanev1.ProviderCredentialMaterializerService_RenewManagedProviderCredential_FullMethodName:               "provider_credential_renew",
		controlplanev1.ProviderCredentialMaterializerService_ObserveManagedProviderCredentialRenewal_FullMethodName:      "provider_credential_renewal_observe",
	}
	metrics := sharedobservability.NewMetrics(metricsSubsystem, buildVersion, methods)
	recoveryMetrics := recovery.NewMetrics()
	draftMetrics := businessobservability.NewSecretDrafts()
	if err := metrics.Register(recoveryMetrics.Collectors()...); err != nil {
		return errors.New("register secret broker recovery metrics")
	}
	if err := metrics.Register(draftMetrics.Collectors()...); err != nil {
		return errors.New("register secret draft recovery metrics")
	}
	defer func() {
		trace, cancelTrace := context.WithTimeout(shutdownBase, 5*time.Second)
		resultErr = errors.Join(resultErr, telemetry.ShutdownTracing(trace))
		cancelTrace()
		sentry, cancelSentry := context.WithTimeout(shutdownBase, 5*time.Second)
		resultErr = errors.Join(resultErr, telemetry.FlushSentry(sentry))
		cancelSentry()
	}()
	control, err := controlplaneclient.Dial(startup, controlplaneclient.Config{ServiceIdentity: true,
		RPCProfile: config.RPCProfile, CallerWorkload: "secret-broker",
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
	drafts, err := newSecretDrafts(config, control.RuntimeSecretDrafts, store, draftMetrics)
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
	appServer, err := providercredential.NewAppServerProcess(
		config.CodexBinary,
		config.ProviderAuthorizationRoot,
		config.Environment == "staging",
	)
	if err != nil {
		return err
	}
	providerCredentials, err := providercredential.New(lifecycle, store, appServer, config.ProviderDeviceAuthTTL)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, providerCredentials.Close()) }()
	var admission grpc.UnaryServerInterceptor
	var transportOptions []grpc.ServerOption
	var securityCheckers []checker
	if config.RPCProfile == "trusted-cluster" {
		admission, err = trustedClusterAdmission(config.RPCProfile)
		if err != nil {
			return err
		}
	} else {
		verifier, err := authorityclient.DialLocal(startup, authorityclient.LocalConfig{
			SocketPath: config.AuthorityVerifierSocket, ExpectedServerUID: config.AuthorityVerifierUID,
			ExpectedServerGID: config.AuthorityVerifierGID, DialTimeout: config.RequestTimeout,
		})
		if err != nil {
			return errors.New("connect provider credential authorization verifier")
		}
		defer func() { resultErr = errors.Join(resultErr, verifier.Close()) }()
		admission = routeProtectedUnary(authorityclient.VerifierUnaryServerInterceptor(verifier.Verifier()))
		securityCheckers = append(securityCheckers, providerVerifierReadiness{client: verifier.Verifier()})
		tlsConfig, err := serverTLS(config)
		if err != nil {
			return err
		}
		transportOptions = append(transportOptions, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}
	handler, err := transportgrpc.New(owner, store, reconciler, config.MaximumSecretBytes,
		transportgrpc.WithProviderCredentialMaterializer(providerCredentials), transportgrpc.WithDraftCommands(drafts),
		transportgrpc.WithProviderRenewalOwner(owner),
		transportgrpc.WithCatalogLogger(logger))
	if err != nil {
		return err
	}
	serverOptions := append(transportOptions,
		grpc.ForceServerCodec(grpcserver.StrictProtoCodec()),
		grpc.ChainUnaryInterceptor(
			metrics.UnaryServerInterceptor(),
			telemetry.UnaryServerInterceptor(methods),
			admission,
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
	grpcServer := grpc.NewServer(serverOptions...)
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
	for _, security := range securityCheckers {
		if err := security.Check(startup); err != nil {
			_ = listener.Close()
			return err
		}
	}
	if err := errors.Join(store.Check(startup), providerCredentials.Check(startup)); err != nil {
		_ = listener.Close()
		return errors.Join(errors.New("secret broker local infrastructure startup barrier failed"), err)
	}
	readiness.Set(true, "ready")
	metrics.SetReady(true)
	readinessCheckers := append([]checker{store, providerCredentials}, securityCheckers...)
	workers := serviceruntime.StartWorkers(lifecycle,
		serveGRPC(grpcServer, listener),
		serveHTTP(technical),
		monitorReadiness(readiness, metrics, logger, config.RequestTimeout, readinessCheckers...),
		reconciler.Worker(),
		drafts.Worker(config.RecoveryInterval, config.RecoveryTimeout, func(err error) {
			if err != nil {
				logger.Warn("secret draft recovery cycle failed", "error_class", secretdraft.RecoveryFailureClass(err), "stage", secretdraft.RecoveryFailureStage(err))
			}
		}),
	)
	err = workers.Wait(context.WithoutCancel(lifecycle))
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "secret broker gRPC server", Timeout: config.ShutdownTimeout, Run: func(context.Context) error { grpcServer.GracefulStop(); return nil }},
		serviceruntime.ShutdownOperation{Name: "secret broker technical HTTP server", Timeout: config.ShutdownTimeout, Run: technical.Shutdown},
		serviceruntime.ShutdownOperation{Name: "secret broker workers", Timeout: config.ShutdownTimeout, Run: workers.Wait},
	)
	return errors.Join(err, shutdownErr)
}

// providerVerifierReadiness adapts verifier readiness to the local checker interface without issuing or
// verifying an application authorization context.
// providerVerifierReadiness адаптирует verifier readiness к local checker interface без выдачи и проверки
// application authorization context.
type providerVerifierReadiness struct {
	client internalrpcauthorityv1.AuthorizationVerifierServiceClient
}

// Check requires a successful Ready reply from the configured verifier and returns a fixed error for
// transport failure or false readiness.
// Check требует успешный Ready reply настроенного verifier и возвращает фиксированную ошибку при transport
// failure либо false readiness.
func (checker providerVerifierReadiness) Check(ctx context.Context) error {
	response, err := checker.client.CheckReadiness(ctx, &internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessRequest{})
	if err != nil || !response.GetReady() {
		return errors.New("provider credential authorization verifier is not ready")
	}
	return nil
}

// routeProtectedUnary applies the supplied verifier interceptor only to the enumerated
// provider/draft/projection methods, forwarding all others directly; it is not a universal default-deny
// policy or a replacement for domain grants.
// routeProtectedUnary применяет переданный verifier interceptor лишь к перечисленным
// provider/draft/projection methods, остальные передаёт напрямую; это не универсальная default-deny policy
// и не замена domain grants.
func routeProtectedUnary(protected grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		switch info.FullMethod {
		case controlplanev1.ProviderCredentialMaterializerService_CheckProviderCredentialMaterializerReadiness_FullMethodName,
			controlplanev1.ProviderCredentialMaterializerService_RenewManagedProviderCredential_FullMethodName,
			controlplanev1.ProviderCredentialMaterializerService_ObserveManagedProviderCredentialRenewal_FullMethodName,
			secretbrokerv1.SecretBrokerService_SaveSecretDraft_FullMethodName,
			secretbrokerv1.SecretBrokerService_ValidateSecretDraft_FullMethodName,
			secretbrokerv1.SecretBrokerService_PublishSecretDraft_FullMethodName,
			secretbrokerv1.SecretBrokerService_DiscardSecretDraft_FullMethodName,
			secretbrokerv1.SecretBrokerService_CheckSecretDraftReadiness_FullMethodName,
			controlplanev1.ProviderCredentialMaterializerService_StartDeviceAuthorization_FullMethodName,
			controlplanev1.ProviderCredentialMaterializerService_ObserveDeviceAuthorization_FullMethodName,
			controlplanev1.ProviderCredentialMaterializerService_ObserveProviderModelCatalog_FullMethodName,
			controlplanev1.ProviderCredentialMaterializerService_MaterializeAPIKey_FullMethodName,
			controlplanev1.ProviderCredentialMaterializerService_DiscardProviderCredentialMaterialization_FullMethodName,
			controlplanev1.ProviderCredentialMaterializerService_CleanupProviderCredential_FullMethodName,
			secretbrokerv1.RuntimeCredentialProjectionService_MaterializeRuntimeCredentials_FullMethodName,
			secretbrokerv1.RuntimeCredentialProjectionService_MaterializeSystemAssistantCredentials_FullMethodName,
			secretbrokerv1.RuntimeCredentialProjectionService_CheckRuntimeCredentialProjectionReadiness_FullMethodName,
			sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName:
			return protected(ctx, request, info, handler)
		default:
			return handler(ctx, request)
		}
	}
}

// normalizeMethod maps a method through the caller's registered operation map and returns one fixed
// unknown value otherwise; callers keep that map bounded.
// normalizeMethod отображает method через зарегистрированную operation map caller, иначе возвращает
// фиксированное unknown; ограниченность map обеспечивает caller.
func normalizeMethod(methods map[string]string, method string) string {
	if operation, ok := methods[method]; ok {
		return operation
	}
	return "unknown"
}

// checker is a context-aware dependency health port, not an authority issuer or background worker
// lifecycle.
// checker — context-aware port проверки dependency health, не источник authority и не lifecycle фонового
// worker.
type checker interface {
	// Check probes the configured dependency and must honor the shared caller deadline; a successful probe
	// does not authorize later commands.
	// Check проверяет настроенную dependency и должен соблюдать общий caller deadline; успешная проверка не
	// разрешает последующие команды.
	Check(context.Context) error
}

// monitorReadiness returns a caller-owned worker that immediately checks every dependency under one shared
// timeout, then repeats on ten-second ticks. It updates readiness/metrics and logs only transitions,
// returning context error on cancellation; dependencies must honor context themselves.
// monitorReadiness возвращает caller-owned worker, сразу проверяющий все dependencies под общим timeout,
// затем по десятисекундным ticks. Обновляет readiness/metrics и логирует лишь transitions, возвращая
// context error при отмене; dependencies сами должны соблюдать context.
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
				if readiness.Set(false, "local_infrastructure_unavailable") {
					logger.WarnContext(ctx, "secret broker readiness lost", "error_class", "kubernetes_or_local_runtime")
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

// secretBrokerOperations delegates to the shared closed operation-to-method registry rather than
// maintaining a second local source of authority mappings.
// secretBrokerOperations использует общий закрытый operation-to-method registry вместо второго локального
// источника authority mappings.
func secretBrokerOperations() map[string]string {
	return controlplaneclient.SecretBrokerOperations()
}

// technicalServer constructs but does not start the health/readiness/metrics HTTP server with fixed
// request/header limits and the supplied lifecycle base; these probes are not business RPC authorization.
// technicalServer создаёт, но не запускает health/readiness/metrics HTTP server с фиксированными
// request/header limits и переданной lifecycle base; эти probes не заменяют business RPC authorization.
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

// serveGRPC wraps Serve in a caller-owned worker and waits for its result; on cancellation it calls
// GracefulStop and joins Serve, so a nonreturning graceful stop is not forcibly bounded here.
// serveGRPC оборачивает Serve в caller-owned worker и ждёт результата; при отмене вызывает GracefulStop и
// присоединяет Serve, поэтому зависший graceful stop здесь принудительно не ограничивается.
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

// serveHTTP starts ListenAndServe, treats ErrServerClosed as success and closes the server on
// cancellation; that cancellation branch returns without explicitly waiting on its Serve-result channel.
// serveHTTP запускает ListenAndServe, считает ErrServerClosed успехом и закрывает сервер при отмене; ветка
// отмены возвращается без явного ожидания Serve-result channel.
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

// serverTLS loads server keypair and client CA, requires TLS 1.3 mutual certificate verification and an
// exact allowed SPIFFE URI in the verified leaf. CA size is checked after reading; it does not provide
// bearer/domain-grant checks or live certificate reload.
// serverTLS загружает server keypair и client CA, требует TLS 1.3 mutual certificate verification и точный
// разрешённый SPIFFE URI в verified leaf. Размер CA проверяет после чтения; bearer/domain-grant checks и
// live certificate reload не обеспечивает.
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
