// Package app содержит единственный composition root integration-gateway.
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	sharedobservability "github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	controlclient "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/clients/controlplane"
	domainservice "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/service/gateway"
	managementservice "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/service/management"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/integration/credential"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/integration/definition"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/integration/gitsource"
	providerregistry "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/integration/provider"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/integration/provider/codexappserver"
	providerhttp "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/integration/provider/http"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/integration/schema"
	internalobservability "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/observability"
	postgresgateway "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/repository/postgres/gateway"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/security/effectreceipt"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/security/payloadcipher"
	managementgrpc "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/transport/grpc/management"
	resultgrpc "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/transport/grpc/result"
	apihttp "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/transport/http/api"
	mcphttp "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/transport/http/mcp"
	"github.com/jackc/pgx/v5/pgxpool"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type state struct {
	config            Config
	logger            *slog.Logger
	telemetry         *sharedobservability.Runtime
	metrics           *sharedobservability.Metrics
	business          *internalobservability.Metrics
	readiness         *serviceruntime.Readiness
	workers           *serviceruntime.WorkerGroup
	publicServer      *http.Server
	technicalServer   *http.Server
	resultServer      *stdgrpc.Server
	publicListener    net.Listener
	technicalListener net.Listener
	resultListener    net.Listener
	pool              *pgxpool.Pool
	control           *controlplaneclient.Client
	managementControl *controlplaneclient.Client
	authority         *authorityclient.LocalConnection
	secretBoundaries  []managedSecretStore
}

func Run(lifecycle context.Context, shutdownBase context.Context, version string) (resultErr error) {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	current := &state{config: config, readiness: serviceruntime.NewReadiness()}
	defer func() { resultErr = errors.Join(resultErr, current.shutdown(context.WithoutCancel(shutdownBase))) }()
	startup, cancelStartup := context.WithTimeout(lifecycle, config.StartupTimeout)
	defer cancelStartup()
	telemetryConfig, err := sharedobservability.RuntimeConfigFromEnv(serviceName, version)
	if err != nil {
		return err
	}
	current.telemetry, err = sharedobservability.NewRuntime(startup, telemetryConfig)
	if err != nil {
		return err
	}
	current.logger = current.telemetry.Logger(os.Stdout)
	current.metrics = sharedobservability.NewMetrics(serviceName, version, map[string]string{})
	current.business, err = internalobservability.New(current.metrics.Register)
	if err != nil {
		return err
	}
	current.metrics.SetReady(false)
	current.pool, err = openPostgres(startup, config)
	if err != nil {
		return err
	}
	contextKey, err := readRuntimeFile(config.PostgresContextKeyFile, 128)
	if err != nil || len(contextKey) < 32 {
		return errors.New("PostgreSQL context signing key is unavailable")
	}
	repository, err := postgresgateway.New(current.pool, postgresgateway.Config{
		PrincipalName: config.PostgresPrincipalName, PrincipalGeneration: config.PostgresPrincipalGeneration,
		ContextKeyID: config.PostgresContextKeyID, ContextSigningKey: contextKey, ContextTTL: 5 * time.Second,
		CleanupBase: shutdownBase, CleanupTimeout: config.ShutdownTimeout,
	})
	if err != nil {
		return err
	}
	cipher, err := payloadcipher.New(config.PayloadKeysetFile)
	if err != nil {
		return err
	}
	credentialSource, err := credential.NewFileSource(config.CredentialDirectory)
	if err != nil {
		return err
	}
	provider, err := providerhttp.New(providerhttp.Config{
		ProxyURL:        config.ProviderProxyURL,
		ProxyServerName: config.ProviderProxyTLSServerName, CAFile: config.ProviderProxyCAFile,
		ClientCertificateFile: config.TLSCertificateFile, ClientPrivateKeyFile: config.TLSPrivateKeyFile,
		MaximumConnections: config.MaximumGlobalConcurrency,
	})
	if err != nil {
		return err
	}
	catalog := controlclient.NewCatalog()
	current.control, err = controlplaneclient.Dial(startup, controlplaneclient.Config{
		Target: config.ControlPlaneTarget, TLSServerName: config.ControlPlaneTLSServerName,
		CAFile: config.ControlPlaneCAFile, ClientCertificateFile: config.ControlPlaneClientCertificateFile,
		ClientPrivateKeyFile: config.ControlPlaneClientPrivateKeyFile, ApplicationGrantFile: config.ControlPlaneApplicationGrantFile,
		ExpectedIssuerUID: 29001, ExpectedIssuerGID: 29000, DialTimeout: 2 * time.Second,
		Operations: controlplaneclient.IntegrationGatewayOperations(),
	})
	if err != nil {
		return err
	}
	current.managementControl, err = controlplaneclient.Dial(startup, controlplaneclient.Config{
		Target: config.ControlPlaneTarget, TLSServerName: config.ControlPlaneTLSServerName,
		CAFile: config.ControlPlaneCAFile, ClientCertificateFile: config.ControlPlaneClientCertificateFile,
		ClientPrivateKeyFile: config.ControlPlaneClientPrivateKeyFile, ApplicationGrantFile: config.ControlPlaneApplicationGrantFile,
		ExpectedIssuerUID: 29001, ExpectedIssuerGID: 29000, DialTimeout: 2 * time.Second,
		Operations: controlplaneclient.IntegrationGatewayManagementOperations(),
	})
	if err != nil {
		return err
	}
	control, err := controlclient.New(current.control, catalog, config.SessionTTL)
	if err != nil {
		return err
	}
	service, err := domainservice.New(repository, cipher, provider, credentialSource, schema.Validator{}, control, domainservice.Config{
		SessionTTL: config.SessionTTL, InvocationTTL: config.InvocationTTL, FinalizationTimeout: config.ShutdownTimeout,
		MaximumSessionRequests: config.MaximumSessionRequests, MaximumConcurrentRequests: config.MaximumSessionConcurrency,
	})
	if err != nil {
		return err
	}
	if err := loadDefinitions(startup, config.DefinitionDirectory, service, catalog); err != nil {
		return err
	}
	providerCatalog, err := providerregistry.LoadCatalog(config.ProviderCatalogFile)
	if err != nil {
		return err
	}
	gitCatalog, err := gitsource.NewCatalog(config.GitSourceCatalogFile)
	if err != nil {
		return err
	}
	for _, directory := range []string{config.CodexTemporaryRoot, config.GitTemporaryRoot} {
		if err = preparePrivateDirectory(directory); err != nil {
			return err
		}
	}
	secretBoundary, gitSecretBoundary, err := newSecretStores(config, gitCatalog)
	if err != nil {
		return err
	}
	current.secretBoundaries = []managedSecretStore{secretBoundary, gitSecretBoundary}
	authorizer, err := codexappserver.New(codexappserver.Config{
		Executable: config.CodexExecutable, TemporaryRoot: config.CodexTemporaryRoot,
		SSLCertificateFile: config.CodexCAFile, HTTPSProxy: config.ManagementEgressProxyURL, NoProxy: config.ManagementEgressNoProxy,
		Timeout: config.ProviderAuthorizationTimeout, PollInterval: config.ProviderAuthorizationPollInterval,
	})
	if err != nil {
		return err
	}
	receiptSigner, err := effectreceipt.New(effectreceipt.Config{
		ProviderIssuer: config.ProviderReceiptIssuer, ProviderPrivateJWKFile: config.ProviderReceiptPrivateJWKFile,
		GitIssuer: config.GitReceiptIssuer, GitPrivateJWKFile: config.GitReceiptPrivateJWKFile,
		MaximumTTL: config.EffectReceiptTTL,
	})
	if err != nil {
		return err
	}
	managementEffects, err := controlclient.NewManagementClient(current.managementControl, receiptSigner)
	if err != nil {
		return err
	}
	gitFetcher, err := gitsource.NewFetcher(gitCatalog, gitSecretBoundary, gitsource.FetcherConfig{
		GitExecutable: config.GitExecutable, TemporaryRoot: config.GitTemporaryRoot, CAFile: config.GitCAFile,
		HTTPSProxy: config.ManagementEgressProxyURL, NoProxy: config.ManagementEgressNoProxy, Timeout: config.GitFetchTimeout,
	})
	if err != nil {
		return err
	}
	managementService, err := managementservice.New(repository, cipher, catalog, gitCatalog, service, managementservice.Config{
		Providers: providerCatalog.Providers(), AuthorizationTTL: config.AuthorizationTTL, MaximumPageSize: 100,
	})
	if err != nil {
		return err
	}
	if err = managementService.ConfigureWorker(managementservice.WorkerDependencies{
		Authorizer: authorizer, Provider: provider, Secrets: secretBoundary, GitSecrets: gitSecretBoundary, Effects: managementEffects, Git: gitFetcher,
		SecretPathPrefix: config.VaultCredentialPathPrefix, LeaseDuration: config.ManagementLeaseDuration,
	}); err != nil {
		return err
	}
	current.authority, err = authorityclient.DialLocal(startup, authorityclient.LocalConfig{
		SocketPath: authorityclient.VerifierSocketPath, ExpectedServerUID: config.AuthorityVerifierUID,
		ExpectedServerGID: config.AuthorityVerifierGID, DialTimeout: 2 * time.Second,
	})
	if err != nil {
		return err
	}
	authorityChecker := authorityVerifierChecker{client: current.authority.Verifier()}
	if err := authorityChecker.Check(startup); err != nil {
		return fmt.Errorf("startup barrier: %w", err)
	}
	resultTransport, err := resultgrpc.New(managementPostgresChecker{repository}, control)
	if err != nil {
		return err
	}
	managementTransport, err := managementgrpc.New(managementService)
	if err != nil {
		return err
	}
	oidcVerifier, err := newOIDCVerifier(lifecycle, config)
	if err != nil {
		return err
	}
	mcpHandler, err := mcphttp.New(service, control, mcphttp.Config{
		MaximumBodyBytes: config.MaximumBodyBytes,
		RequestDeadline:  config.RequestDeadline, MaximumGlobalConcurrency: config.MaximumGlobalConcurrency,
	})
	if err != nil {
		return err
	}
	apiHandler, err := apihttp.New(service, oidcVerifier)
	if err != nil {
		return err
	}
	if err := repository.Check(startup); err != nil {
		return fmt.Errorf("startup barrier: %w", err)
	}
	if err := current.control.Check(startup); err != nil {
		return fmt.Errorf("startup barrier: %w", err)
	}
	if err := provider.Check(startup); err != nil {
		return fmt.Errorf("startup barrier: %w", err)
	}
	for _, dependency := range []checker{managementPostgresChecker{repository}, secretBoundary, gitSecretBoundary, oidcVerifier, authorizer, gitFetcher, providerCatalog, gitCatalog, managementEffects} {
		if err := dependency.Check(startup); err != nil {
			return fmt.Errorf("startup barrier: %w", err)
		}
	}
	publicMux := http.NewServeMux()
	publicMux.Handle("/mcp/v1", observeHTTP(current.business, "mcp", mcpHandler.HTTPHandler()))
	publicMux.Handle("/api/v1/", observeHTTP(current.business, "api",
		boundedHTTP(apiHandler, config.MaximumGlobalConcurrency, config.RequestDeadline)))
	publicTLS, err := loadPublicTLSConfig(config)
	if err != nil {
		return err
	}
	current.publicServer = &http.Server{
		Addr: config.HTTPListen, Handler: publicMux,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: config.RequestDeadline + time.Second,
		WriteTimeout: config.RequestDeadline + time.Second, IdleTimeout: 30 * time.Second,
		MaxHeaderBytes: 16 << 10, TLSConfig: publicTLS,
	}
	resultTLS, err := loadResultTLSConfig(config)
	if err != nil {
		return err
	}
	current.resultServer = stdgrpc.NewServer(
		stdgrpc.Creds(credentials.NewTLS(resultTLS)),
		stdgrpc.MaxRecvMsgSize(64<<10), stdgrpc.MaxSendMsgSize(512<<10),
		stdgrpc.ChainUnaryInterceptor(authorityclient.VerifierUnaryServerInterceptor(current.authority.Verifier())),
	)
	integrationgatewayv1.RegisterIntegrationResultServiceServer(current.resultServer, resultTransport)
	integrationgatewayv1.RegisterIntegrationManagementServiceServer(current.resultServer, managementTransport)
	technicalMux := http.NewServeMux()
	technicalMux.HandleFunc("/livez", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) })
	technicalMux.HandleFunc("/readyz", func(response http.ResponseWriter, _ *http.Request) {
		ready, _ := current.readiness.Ready()
		if !ready {
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	})
	technicalMux.Handle("/metrics", current.metrics.PrometheusHandler())
	current.technicalServer = &http.Server{
		Addr: config.TechnicalListen, Handler: technicalMux,
		ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 8 << 10,
	}
	current.publicListener, err = net.Listen("tcp", config.HTTPListen)
	if err != nil {
		return errors.New("bind HTTPS listener")
	}
	current.publicListener = tls.NewListener(current.publicListener, publicTLS)
	current.resultListener, err = net.Listen("tcp", config.ResultRPCListen)
	if err != nil {
		_ = current.publicListener.Close()
		return errors.New("bind integration result gRPC listener")
	}
	current.technicalListener, err = net.Listen("tcp", config.TechnicalListen)
	if err != nil {
		_ = current.publicListener.Close()
		_ = current.resultListener.Close()
		return errors.New("bind technical HTTP listener")
	}
	current.readiness.Set(true, "ready")
	current.metrics.SetReady(true)
	current.workers = serviceruntime.StartWorkers(
		lifecycle,
		serverWorker(current.publicServer, current.publicListener),
		grpcServerWorker(current.resultServer, current.resultListener),
		serverWorker(current.technicalServer, current.technicalListener),
		executionWorker(service, current.business, current.logger, config.WorkerInterval, shutdownBase),
		continuationWorker(service, current.business, current.logger, config.WorkerInterval),
		lifecycleWorker(service, current.business, current.logger, config.WorkerInterval),
		managementWorker(managementService, current.business, current.logger, config.WorkerInterval, shutdownBase),
		readinessWorker([]checker{managementPostgresChecker{repository}, current.control, provider, cipher, authorityChecker, secretBoundary, gitSecretBoundary, oidcVerifier, authorizer, gitFetcher, providerCatalog, gitCatalog, managementEffects}, current.readiness, current.metrics, current.business, current.logger, config.ReadinessInterval),
	)
	workerResult := make(chan error, 1)
	go func() { workerResult <- current.workers.Wait(shutdownBase) }()
	select {
	case <-lifecycle.Done():
		return nil
	case err := <-workerResult:
		return err
	}
}

func preparePrivateDirectory(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("management runtime directory is invalid")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return errors.New("create management runtime directory")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("management runtime directory is unsafe")
	}
	if err = os.Chmod(path, 0o700); err != nil {
		return errors.New("secure management runtime directory")
	}
	return nil
}

func loadDefinitions(ctx context.Context, directory string, service *domainservice.Service, catalog *controlclient.Catalog) error {
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) == 0 || len(entries) > 64 {
		return errors.New("integration definition directory is invalid")
	}
	parsedDefinitions := make([]entity.Definition, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || (filepath.Ext(entry.Name()) != ".yaml" && filepath.Ext(entry.Name()) != ".yml") {
			return errors.New("integration definition entry is invalid")
		}
		raw, err := readRuntimeFile(filepath.Join(directory, entry.Name()), definition.MaximumDefinitionBytes)
		if err != nil {
			return err
		}
		parsed, err := definition.Parse(raw)
		if err != nil {
			return err
		}
		parsedDefinitions = append(parsedDefinitions, parsed)
	}
	// Полный закрытый registry проверяется до первой materialization в БД.
	for _, parsed := range parsedDefinitions {
		if err := catalog.Store(parsed); err != nil {
			return err
		}
	}
	for _, parsed := range parsedDefinitions {
		if err := service.StoreDefinition(ctx, parsed); err != nil {
			return err
		}
	}
	return nil
}

func serverWorker(server *http.Server, listener net.Listener) serviceruntime.Worker {
	return func(ctx context.Context) error {
		go func() { <-ctx.Done(); _ = listener.Close() }()
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
			return nil
		}
		return err
	}
}

func grpcServerWorker(server *stdgrpc.Server, listener net.Listener) serviceruntime.Worker {
	return func(ctx context.Context) error {
		go func() {
			<-ctx.Done()
			server.Stop()
			_ = listener.Close()
		}()
		err := server.Serve(listener)
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		return err
	}
}

func executionWorker(service *domainservice.Service, metrics *internalobservability.Metrics, logger *slog.Logger, interval time.Duration, finalizationBase context.Context) serviceruntime.Worker {
	return func(ctx context.Context) error {
		for {
			didWork, invocationOutcome, err := service.ExecuteOne(ctx, finalizationBase)
			if invocationOutcome != "" {
				metrics.ObserveInvocation(metricInvocationOutcome(invocationOutcome))
			}
			if err != nil {
				metrics.ObserveWorker("execution", "failure")
				logger.Error("integration execution cycle failed", "error", err)
			} else if didWork {
				metrics.ObserveWorker("execution", "success")
			} else {
				metrics.ObserveWorker("execution", "idle")
			}
			if !wait(ctx, interval) {
				return nil
			}
		}
	}
}

func metricInvocationOutcome(status enum.InvocationStatus) string {
	switch status {
	case enum.InvocationSucceeded:
		return "success"
	case enum.InvocationFailed:
		return "failure"
	case enum.InvocationUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

func lifecycleWorker(service *domainservice.Service, metrics *internalobservability.Metrics, logger *slog.Logger, interval time.Duration) serviceruntime.Worker {
	return func(ctx context.Context) error {
		for {
			didWork, err := service.ExpireOneScope(ctx)
			if err != nil {
				metrics.ObserveWorker("lifecycle", "failure")
				logger.Error("integration lifecycle cycle failed", "error", err)
			} else if didWork {
				metrics.ObserveWorker("lifecycle", "success")
			} else {
				metrics.ObserveWorker("lifecycle", "idle")
			}
			if !wait(ctx, interval) {
				return nil
			}
		}
	}
}

func continuationWorker(service *domainservice.Service, metrics *internalobservability.Metrics, logger *slog.Logger, interval time.Duration) serviceruntime.Worker {
	return func(ctx context.Context) error {
		for {
			didWork, err := service.SyncContinuationOne(ctx)
			if err != nil {
				metrics.ObserveWorker("continuation", "failure")
				logger.Error("integration continuation cycle failed", "error", err)
			} else if didWork {
				metrics.ObserveWorker("continuation", "success")
			} else {
				metrics.ObserveWorker("continuation", "idle")
			}
			if !wait(ctx, interval) {
				return nil
			}
		}
	}
}

func managementWorker(service *managementservice.Service, metrics *internalobservability.Metrics, logger *slog.Logger, interval time.Duration, finalizationBase context.Context) serviceruntime.Worker {
	return func(ctx context.Context) error {
		for {
			didWork, err := service.ProcessOne(ctx, finalizationBase)
			if err != nil {
				metrics.ObserveWorker("management", "failure")
				logger.Error("integration management effect cycle failed", "error", err)
			} else if didWork {
				metrics.ObserveWorker("management", "success")
			} else {
				metrics.ObserveWorker("management", "idle")
			}
			if !wait(ctx, interval) {
				return nil
			}
		}
	}
}

type checker interface{ Check(context.Context) error }

type managementRepository interface {
	Check(context.Context) error
	CheckManagement(context.Context) error
}

type managementPostgresChecker struct{ repository managementRepository }

func (checker managementPostgresChecker) Check(ctx context.Context) error {
	return errors.Join(checker.repository.Check(ctx), checker.repository.CheckManagement(ctx))
}

type authorityVerifierChecker struct {
	client internalrpcauthorityv1.AuthorizationVerifierServiceClient
}

func (checker authorityVerifierChecker) Check(ctx context.Context) error {
	response, err := checker.client.CheckReadiness(ctx,
		&internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessRequest{})
	if err != nil || !response.GetReady() || !response.GetReplayStoreReady() ||
		response.GetSourceRevision() == 0 || response.GetPolicyRevision() == 0 ||
		response.GetSignerGeneration() == 0 || response.GetSnapshotDigestSha256() == "" {
		return errors.New("local authorization verifier is unavailable")
	}
	return nil
}

func readinessWorker(checks []checker, readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, business *internalobservability.Metrics, logger *slog.Logger, interval time.Duration) serviceruntime.Worker {
	return func(ctx context.Context) error {
		for {
			checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			var checkErrors []error
			for _, dependency := range checks {
				checkErrors = append(checkErrors, dependency.Check(checkCtx))
			}
			err := errors.Join(checkErrors...)
			cancel()
			if err != nil {
				readiness.Set(false, "dependency unavailable")
				metrics.SetReady(false)
				business.ObserveWorker("readiness", "failure")
				logger.Error("integration gateway readiness failed", "error", err)
			} else {
				readiness.Set(true, "ready")
				metrics.SetReady(true)
				business.ObserveWorker("readiness", "success")
			}
			if !wait(ctx, interval) {
				return nil
			}
		}
	}
}

func observeHTTP(metrics *internalobservability.Metrics, route string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: response, status: http.StatusOK}
		next.ServeHTTP(recorder, request)
		outcome := "success"
		if recorder.status >= 400 {
			outcome = "rejected"
		}
		if recorder.status >= 500 {
			outcome = "failure"
		}
		metrics.ObserveHTTP(route, outcome, started)
	})
}

func boundedHTTP(next http.Handler, maximumConcurrency int, deadline time.Duration) http.Handler {
	semaphore := make(chan struct{}, maximumConcurrency)
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		select {
		case semaphore <- struct{}{}:
			defer func() { <-semaphore }()
		default:
			response.Header().Set("Content-Type", "application/json")
			response.Header().Set("Cache-Control", "no-store")
			response.Header().Set("X-Content-Type-Options", "nosniff")
			response.WriteHeader(http.StatusTooManyRequests)
			_, _ = response.Write([]byte("{\"error\":\"QUOTA_EXCEEDED\"}\n"))
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), deadline)
		defer cancel()
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *statusRecorder) WriteHeader(status int) {
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (current *state) shutdown(background context.Context) error {
	current.readiness.Set(false, "shutting down")
	if current.metrics != nil {
		current.metrics.SetReady(false)
	}
	if current.workers != nil {
		current.workers.Stop()
	}
	var operations []serviceruntime.ShutdownOperation
	if current.workers != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{Name: "workers", Timeout: current.config.ShutdownTimeout, Run: current.workers.Wait})
	}
	if current.publicServer != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{Name: "public HTTP", Timeout: current.config.ShutdownTimeout, Run: current.publicServer.Shutdown})
	}
	if current.technicalServer != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{Name: "technical HTTP", Timeout: current.config.ShutdownTimeout, Run: current.technicalServer.Shutdown})
	}
	if current.control != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{Name: "control-plane client", Timeout: current.config.ShutdownTimeout, Run: func(context.Context) error { return current.control.Close() }})
	}
	if current.managementControl != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{Name: "control-plane management client", Timeout: current.config.ShutdownTimeout, Run: func(context.Context) error { return current.managementControl.Close() }})
	}
	if current.authority != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{Name: "authority verifier client", Timeout: current.config.ShutdownTimeout, Run: func(context.Context) error { return current.authority.Close() }})
	}
	for _, boundary := range current.secretBoundaries {
		boundary := boundary
		operations = append(operations, serviceruntime.ShutdownOperation{Name: "credential boundary", Timeout: current.config.ShutdownTimeout, Run: func(context.Context) error { boundary.Close(); return nil }})
	}
	if current.pool != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{Name: "PostgreSQL", Timeout: current.config.ShutdownTimeout, Run: func(context.Context) error { current.pool.Close(); return nil }})
	}
	if current.telemetry != nil {
		operations = append(operations,
			serviceruntime.ShutdownOperation{Name: "tracing", Timeout: current.config.ShutdownTimeout, Run: current.telemetry.ShutdownTracing},
			serviceruntime.ShutdownOperation{Name: "Sentry", Timeout: current.config.ShutdownTimeout, Run: current.telemetry.FlushSentry})
	}
	return serviceruntime.RunShutdown(background, operations...)
}
