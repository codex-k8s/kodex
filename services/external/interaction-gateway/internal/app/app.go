// Package app содержит единственный composition root interaction-gateway.
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	"github.com/codex-k8s/matter-codex/libs/go/httpserver"
	sharedobservability "github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	controlclient "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/clients/controlplane"
	domainerrs "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/errs"
	domainservice "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/service/gateway"
	mattermostclient "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/integration/mattermost"
	objectclient "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/integration/objectstore"
	internalobservability "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/observability"
	postgresgateway "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/repository/postgres/gateway"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/security/mattermostevent"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/security/readbackgrant"
	apihttp "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/transport/http/api"
	generatedhttp "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/transport/http/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	serviceName          = "interaction-gateway"
	logWorkerCycleFailed = "interaction worker cycle failed"
	logWebSocketFailed   = "Mattermost WebSocket connection failed"
)

type state struct {
	config     Config
	logger     *slog.Logger
	telemetry  *sharedobservability.Runtime
	metrics    *sharedobservability.Metrics
	business   *internalobservability.Metrics
	readiness  *serviceruntime.Readiness
	workers    *serviceruntime.WorkerGroup
	public     *http.Server
	publicConn net.Listener
	technical  *httpserver.Server
	pool       *pgxpool.Pool
	control    *controlplaneclient.Client
}

func Run(lifecycle context.Context, shutdownBase context.Context, version string) (resultErr error) {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	current := &state{config: config, readiness: serviceruntime.NewReadiness()}
	defer func() { resultErr = errors.Join(resultErr, current.shutdown(context.WithoutCancel(shutdownBase))) }()
	startup, cancelStartup := context.WithTimeout(lifecycle, config.Gateway.StartupTimeout)
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
	current.pool, err = openPostgres(startup, config.Gateway)
	if err != nil {
		return err
	}
	key, err := readRuntimeFile(config.Gateway.DeliveryKeyFile, 128)
	if err != nil || len(key) != 32 {
		return errors.New("delivery encryption key is unavailable")
	}
	current.control, err = controlplaneclient.Dial(startup, controlplaneclient.Config{
		Target: config.Control.Target, TLSServerName: config.Control.TLSServerName, CAFile: config.Control.CAFile,
		ClientCertificateFile: config.Control.ClientCertificateFile, ClientPrivateKeyFile: config.Control.ClientPrivateKeyFile,
		ApplicationGrantFile: config.Control.ApplicationGrantFile,
		ExpectedIssuerUID:    config.Control.ExpectedAuthorityIssuerUID, ExpectedIssuerGID: config.Control.ExpectedAuthorityIssuerGID,
		DialTimeout: config.Control.DialTimeout, Operations: controlplaneclient.InteractionGatewayOperations(),
	})
	if err != nil {
		return err
	}
	control, err := controlclient.New(current.control, config.Control.RequestTimeout)
	if err != nil {
		return err
	}
	mattermost, err := mattermostclient.New(mattermostclient.Config{
		SiteURL: config.Mattermost.SiteURL, TLSServerName: config.Mattermost.TLSServerName, CAFile: config.Mattermost.CAFile,
		ClientCertificateFile: config.Mattermost.ClientCertificateFile, ClientPrivateKeyFile: config.Mattermost.ClientPrivateKeyFile,
		MappingManifestFile: config.Mattermost.MappingManifestFile, RequestTimeout: config.Mattermost.RequestTimeout,
		MappingExpectedRevision: config.Mattermost.MappingExpectedRevision,
		MappingSHA256File:       config.Mattermost.MappingSHA256File, MappingVaultKVVersion: config.Mattermost.MappingVaultKVVersion,
		MaximumFileBytes: config.Mattermost.MaximumFileBytes, CatchUpWindow: config.Mattermost.CatchUpWindow,
	})
	if err != nil {
		return err
	}
	boundaries := mattermost.ChannelBoundaries()
	organizationID, projectSet := "", map[string]struct{}{}
	for _, boundary := range boundaries {
		if organizationID == "" {
			organizationID = boundary.OrganizationID
		}
		if boundary.OrganizationID != organizationID {
			return errors.New("Mattermost mapping spans multiple PostgreSQL tenant principals")
		}
		projectSet[boundary.ProjectID] = struct{}{}
	}
	projectIDs := make([]string, 0, len(projectSet))
	for projectID := range projectSet {
		projectIDs = append(projectIDs, projectID)
	}
	slices.Sort(projectIDs)
	repository, err := postgresgateway.New(current.pool, postgresgateway.Config{
		EncryptionKey: key, PrincipalName: config.Gateway.PostgresExpectedUser,
		PrincipalGeneration: config.Gateway.PostgresPrincipalGeneration,
		OrganizationID:      organizationID, AllowedProjectIDs: projectIDs,
		CleanupBase: shutdownBase, CleanupTimeout: config.Gateway.ShutdownTimeout,
	})
	if err != nil {
		return err
	}
	objects, err := objectclient.New(objectclient.Config{
		Endpoint:      config.Object.Endpoint,
		TLSServerName: config.Object.TLSServerName, CAFile: config.Object.CAFile,
		ClientCertificateFile: config.Object.ClientCertificateFile, ClientPrivateKeyFile: config.Object.ClientPrivateKeyFile,
		AccessKeyFile: config.Object.AccessKeyFile, SecretKeyFile: config.Object.SecretKeyFile,
		SessionTokenFile: config.Object.SessionTokenFile, Bucket: config.Object.Bucket,
		MaximumObjectBytes: config.Object.MaximumObjectBytes, Timeout: config.Object.Timeout,
	})
	if err != nil {
		return err
	}
	authority, err := mattermostevent.New(mattermostevent.Config{
		ProducerID: "control-plane.interaction-gateway", Purpose: "MATTERMOST_SIGNED_EVENT",
		Issuer: config.Gateway.EventIssuer, Audience: config.Gateway.EventAudience,
		PrivateJWKFile: config.Gateway.EventPrivateJWKFile, CallbackKeyFile: config.Gateway.CallbackKeyFile,
		Generation: config.Gateway.EventGeneration, MaximumTTL: config.Gateway.EventTTL,
	})
	if err != nil {
		return err
	}
	readbackVerifier, err := readbackgrant.New(startup, readbackgrant.Config{
		Issuer: config.Gateway.ReadbackIssuer, Audience: config.Gateway.ReadbackAudience,
		ProducerID: "control-plane.interaction-delivery-readback", Purpose: "INTERACTION_DELIVERY_READBACK_GRANT",
		Operation: "interaction.delivery.read", Permission: "interaction.delivery.read",
		PublicKeysetFile: config.Gateway.ReadbackPublicKeysetFile, Generation: config.Gateway.ReadbackGeneration,
		MaximumTTL: 15 * time.Minute,
	}, repository)
	if err != nil {
		return err
	}
	service, err := domainservice.New(repository, mattermost, objects, control, authority, current.business, domainservice.Config{
		ActionCallbackURL: config.Gateway.ActionCallbackURL, DialogCallbackURL: config.Gateway.DialogCallbackURL,
		ArtifactDownloadBaseURL: config.Gateway.ArtifactDownloadBaseURL, ArtifactDownloadTTL: config.Gateway.ArtifactDownloadTTL,
		RetentionRef: config.Gateway.RetentionRef, MaximumPromptBytes: config.Gateway.MaximumPromptBytes,
		MaximumFiles: config.Gateway.MaximumFiles, MaximumAttempts: config.Gateway.MaximumAttempts,
		MaximumMattermostFileBytes: config.Mattermost.MaximumFileBytes,
		InboundLease:               config.Gateway.InboundLease, DeliveryLease: config.Gateway.DeliveryLease,
		ScanPollInterval: config.Gateway.ScanPollInterval, RetryBase: config.Gateway.RetryBase,
	})
	if err != nil {
		return err
	}
	slashTokenRaw, err := readRuntimeFile(config.Gateway.SlashTokenFile, 16<<10)
	if err != nil {
		return errors.New("read Mattermost slash token")
	}
	handler, err := apihttp.New(service, readbackVerifier, apihttp.Config{
		SlashToken: strings.TrimSpace(string(slashTokenRaw)), MaximumBodyBytes: config.Gateway.MaximumBodyBytes,
		MattermostClientSPIFFE:  config.Gateway.MattermostClientSPIFFE,
		ReadbackClientSPIFFEIDs: config.Gateway.ReadbackClientSPIFFEIDs,
	})
	if err != nil {
		return err
	}
	readbackCheck := func(ctx context.Context) error {
		deliveryID, key := uuid.NewString(), uuid.NewString()
		credential, expiresAt, credentialErr := control.IssueInteractionDeliveryReadback(ctx, key, deliveryID, true)
		if credentialErr != nil {
			return credentialErr
		}
		claims, verifyErr := readbackVerifier.Verify(ctx, "Bearer "+credential)
		if verifyErr != nil || !claims.Readiness || claims.DeliveryID != deliveryID ||
			!expiresAt.Equal(time.Unix(claims.ExpiresAt, 0)) ||
			!slices.Contains(config.Gateway.ReadbackClientSPIFFEIDs, claims.CallerSPIFFEID) {
			return errors.New("interaction delivery readback working path is not ready")
		}
		if validateErr := service.ValidateDeliveryReadback(ctx, claims.JTI, claims.DeliveryID,
			claims.OrganizationID, claims.ProjectID, claims.CredentialSHA256, claims.Generation); validateErr != nil {
			return errors.New("interaction delivery readback working path is not ready")
		}
		_, readErr := service.GetDeliveryScoped(ctx, claims.OrganizationID, claims.ProjectID, claims.DeliveryID)
		if readErr != nil && !errors.Is(readErr, domainerrs.ErrNotFound) {
			return errors.New("interaction delivery readback working path is not ready")
		}
		return nil
	}
	for _, check := range []struct {
		name string
		run  func(context.Context) error
	}{{"PostgreSQL", repository.Check}, {"control-plane", control.Check}, {"Mattermost event", service.CheckInteraction}, {"S3", objects.Check}, {"delivery readback", readbackCheck}} {
		if err := check.run(startup); err != nil {
			return errors.New("startup barrier: " + check.name + " working path is not ready")
		}
	}
	if err := service.ReconcileLifecycle(startup); err != nil {
		return errors.New("startup barrier: Mattermost lifecycle reconciliation failed")
	}
	if err := mattermost.Check(startup); err != nil {
		return errors.New("startup barrier: Mattermost mapping readback failed")
	}
	if err := service.CatchUp(startup); err != nil {
		return errors.New("startup barrier: Mattermost catch-up failed")
	}
	publicTLSConfig, err := publicTLS(config.Gateway)
	if err != nil {
		return err
	}
	current.public = &http.Server{
		Addr: config.Gateway.HTTPListen,
		Handler: boundedHTTP(observeHTTP(current.business, generatedhttp.Handler(handler)),
			config.Gateway.MaximumConnections, config.Gateway.OperationTimeout),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: config.Gateway.OperationTimeout + time.Second,
		WriteTimeout: config.Gateway.OperationTimeout + time.Second, IdleTimeout: 30 * time.Second,
		MaxHeaderBytes: 32 << 10, TLSConfig: publicTLSConfig,
		ErrorLog: slog.NewLogLogger(current.logger.Handler(), slog.LevelError),
	}
	listener, err := net.Listen("tcp", config.Gateway.HTTPListen)
	if err != nil {
		return errors.New("listen interaction HTTPS server")
	}
	current.publicConn = tls.NewListener(listener, publicTLSConfig)
	current.technical, err = httpserver.New(httpserver.Config{
		Address: config.Gateway.TechnicalListen, ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second,
		MaximumHeaderBytes: 32 << 10, MaximumConnections: config.Gateway.MaximumConnections,
	}, current.readiness, current.metrics.PrometheusHandler())
	if err != nil {
		return err
	}
	if err := current.technical.Listen(); err != nil {
		return err
	}
	current.readiness.Set(true, "ready")
	current.metrics.SetReady(true)
	current.workers = serviceruntime.StartWorkers(lifecycle,
		publicWorker(current.public, current.publicConn, shutdownBase, config.Gateway.ShutdownTimeout),
		technicalWorker(current.technical, shutdownBase, config.Gateway.ShutdownTimeout),
		webSocketWorker(service, current.business, current.logger, config.Gateway.RetryBase),
		periodicWorker("inbound", config.Gateway.WorkerInterval, config.Gateway.OperationTimeout, current.business, current.logger,
			func(ctx context.Context) (bool, error) { return service.ProcessWaiting(ctx) }),
		periodicWorker("delivery", config.Gateway.WorkerInterval, config.Gateway.OperationTimeout, current.business, current.logger,
			func(ctx context.Context) (bool, error) {
				return service.ProcessDelivery(ctx, config.Gateway.InstanceID)
			}),
		periodicWorker("owner_gate", config.Gateway.OwnerGateInterval, config.Gateway.OperationTimeout, current.business, current.logger,
			func(ctx context.Context) (bool, error) { return service.ClaimOwnerGate(ctx) }),
		periodicWorker("owner_delivery", config.Gateway.OwnerGateInterval, config.Gateway.OperationTimeout, current.business, current.logger,
			func(ctx context.Context) (bool, error) { return service.ClaimInteractionDelivery(ctx) }),
		periodicWorker("expiry", config.Gateway.ExpiryInterval, config.Gateway.OperationTimeout, current.business, current.logger,
			func(ctx context.Context) (bool, error) { return true, service.ExpireOwnerGate(ctx) }),
		readinessWorker(repository.Check, control.Check, service.CheckInteraction, mattermost.Check, objects.Check, readbackCheck,
			current.readiness, current.metrics, current.business, current.logger,
			config.Gateway.ReadinessInterval, config.Gateway.OperationTimeout),
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

func observeHTTP(metrics *internalobservability.Metrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		started := time.Now()
		route := "unknown"
		switch request.URL.Path {
		case "/mattermost/v1/commands/codex":
			route = "slash"
		case "/mattermost/v1/actions":
			route = "action"
		case "/mattermost/v1/dialogs":
			route = "dialog"
		default:
			if strings.HasPrefix(request.URL.Path, "/internal/v1/deliveries/") {
				route = "readback"
			}
		}
		recorder := &statusRecorder{ResponseWriter: response, status: http.StatusOK}
		next.ServeHTTP(recorder, request)
		outcome := "success"
		if recorder.status >= http.StatusBadRequest {
			outcome = "failure"
		}
		metrics.ObserveHTTP(route, outcome, started)
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

func publicWorker(server *http.Server, listener net.Listener, shutdownBase context.Context, timeout time.Duration) serviceruntime.Worker {
	return func(ctx context.Context) error {
		stopped := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				shutdown, cancel := context.WithTimeout(context.WithoutCancel(shutdownBase), timeout)
				_ = server.Shutdown(shutdown)
				cancel()
			case <-stopped:
			}
		}()
		err := server.Serve(listener)
		close(stopped)
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
			return nil
		}
		return err
	}
}

func technicalWorker(server *httpserver.Server, shutdownBase context.Context, timeout time.Duration) serviceruntime.Worker {
	return func(ctx context.Context) error {
		stopped := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				shutdown, cancel := context.WithTimeout(context.WithoutCancel(shutdownBase), timeout)
				_ = server.Shutdown(shutdown)
				cancel()
			case <-stopped:
			}
		}()
		err := server.Serve()
		close(stopped)
		return err
	}
}

func webSocketWorker(service *domainservice.Service, metrics *internalobservability.Metrics,
	logger *slog.Logger, retry time.Duration) serviceruntime.Worker {
	return func(ctx context.Context) error {
		for ctx.Err() == nil {
			catchUpContext, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := service.CatchUp(catchUpContext)
			cancel()
			if err == nil {
				err = service.Listen(ctx)
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			metrics.ObserveWorker("websocket", "failure")
			logger.ErrorContext(ctx, logWebSocketFailed, "error", err)
			if !wait(ctx, retry) {
				return ctx.Err()
			}
		}
		return ctx.Err()
	}
}

func periodicWorker(name string, interval, timeout time.Duration, metrics *internalobservability.Metrics,
	logger *slog.Logger, cycle func(context.Context) (bool, error)) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				cycleContext, cancel := context.WithTimeout(ctx, timeout)
				worked, err := cycle(cycleContext)
				cancel()
				outcome := "idle"
				if worked {
					outcome = "success"
				}
				if err != nil {
					outcome = "failure"
					logger.ErrorContext(ctx, logWorkerCycleFailed, "worker", name, "error", err)
				}
				metrics.ObserveWorker(name, outcome)
			}
		}
	}
}

func readinessWorker(
	repositoryCheck func(context.Context) error,
	controlCheck func(context.Context) error,
	interactionCheck func(context.Context) error,
	mattermostCheck func(context.Context) error,
	objectCheck func(context.Context) error,
	readbackCheck func(context.Context) error,
	readiness *serviceruntime.Readiness,
	metrics *sharedobservability.Metrics,
	business *internalobservability.Metrics,
	logger *slog.Logger,
	interval time.Duration,
	timeout time.Duration,
) serviceruntime.Worker {
	checkFunctions := []func(context.Context) error{repositoryCheck, controlCheck, interactionCheck, mattermostCheck, objectCheck, readbackCheck}
	return periodicWorker("readiness", interval, timeout, business, logger, func(ctx context.Context) (bool, error) {
		for _, check := range checkFunctions {
			if err := check(ctx); err != nil {
				readiness.Set(false, "dependency unavailable")
				metrics.SetReady(false)
				return true, err
			}
		}
		readiness.Set(true, "ready")
		metrics.SetReady(true)
		return true, nil
	})
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

func (current *state) shutdown(base context.Context) error {
	if current.readiness != nil {
		current.readiness.Set(false, "shutting down")
	}
	if current.metrics != nil {
		current.metrics.SetReady(false)
	}
	if current.workers != nil {
		current.workers.Stop()
	}
	operations := []serviceruntime.ShutdownOperation{}
	if current.workers != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{Name: "join workers", Timeout: current.config.Gateway.ShutdownTimeout, Run: current.workers.Wait})
	}
	if current.public != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{Name: "shutdown public server", Timeout: current.config.Gateway.ShutdownTimeout, Run: current.public.Shutdown})
	}
	if current.publicConn != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{Name: "close public listener", Timeout: current.config.Gateway.ShutdownTimeout, Run: func(context.Context) error { _ = current.publicConn.Close(); return nil }})
	}
	if current.technical != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{Name: "shutdown technical server", Timeout: current.config.Gateway.ShutdownTimeout, Run: current.technical.Shutdown})
	}
	if current.control != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{Name: "close control-plane client", Timeout: current.config.Gateway.ShutdownTimeout, Run: func(context.Context) error { return current.control.Close() }})
	}
	if current.pool != nil {
		operations = append(operations, serviceruntime.ShutdownOperation{Name: "close PostgreSQL", Timeout: current.config.Gateway.ShutdownTimeout, Run: func(context.Context) error { current.pool.Close(); return nil }})
	}
	if current.telemetry != nil {
		operations = append(operations,
			serviceruntime.ShutdownOperation{Name: "shutdown tracing", Timeout: current.config.Gateway.ShutdownTimeout, Run: current.telemetry.ShutdownTracing},
			serviceruntime.ShutdownOperation{Name: "flush Sentry", Timeout: current.config.Gateway.ShutdownTimeout, Run: current.telemetry.FlushSentry},
		)
	}
	return serviceruntime.RunShutdown(base, operations...)
}
