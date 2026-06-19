package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	grpcruntime "google.golang.org/grpc"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	packagesv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/packages/v1"
	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	governanceclient "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/governance"
	interactionhubclient "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/interactionhub"
	packagehubclient "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/packagehub"
	projectcatalogclient "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/projectcatalog"
	providerhubclient "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/providerhub"
	runtimeclient "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/runtime"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	agentpostgres "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/repository/postgres/agent"
	agentgrpc "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/transport/grpc"
)

const serviceName = "agent-manager"

// Run starts agent-manager process servers and shuts them down with context.
func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	dbPool, err := postgreslib.OpenPool(ctx, cfg.DatabasePoolSettings())
	if err != nil {
		return err
	}
	defer dbPool.Close()
	eventLogPool, err := serviceprocess.OpenEventLogPool(ctx, cfg.needsEventLogDatabase(), cfg.EventLogDatabasePoolSettings())
	if err != nil {
		return err
	}
	if eventLogPool != nil {
		defer eventLogPool.Close()
	}
	guidanceResolver, packageHubConn, err := newGuidanceResolver(cfg)
	if err != nil {
		return err
	}
	if packageHubConn != nil {
		defer func() { _ = packageHubConn.Close() }()
	}
	workspacePolicyResolver, selfDeploySignalReader, selfDeployBuildPlanReader, selfDeployDeployPlanReader, projectCatalogConn, err := newProjectCatalogAdapters(cfg)
	if err != nil {
		return err
	}
	if projectCatalogConn != nil {
		defer func() { _ = projectCatalogConn.Close() }()
	}
	runtimePreparer, runtimeJobCreator, runtimeJobReader, selfDeployBuildContextPreparer, selfDeployRuntimeJobReader, selfDeployBuildJobCreator, selfDeployDeployJobCreator, runtimeManagerConn, err := newRuntimePreparer(cfg)
	if err != nil {
		return err
	}
	if runtimeManagerConn != nil {
		defer func() { _ = runtimeManagerConn.Close() }()
	}
	providerFollowUpDispatcher, providerHubConn, err := newProviderFollowUpDispatcher(cfg)
	if err != nil {
		return err
	}
	if providerHubConn != nil {
		defer func() { _ = providerHubConn.Close() }()
	}
	humanGateRequester, interactionHubConn, err := newHumanGateRequester(cfg)
	if err != nil {
		return err
	}
	if interactionHubConn != nil {
		defer func() { _ = interactionHubConn.Close() }()
	}
	selfDeployGatePreparer, governanceManagerConn, err := newSelfDeployGatePreparer(cfg)
	if err != nil {
		return err
	}
	if governanceManagerConn != nil {
		defer func() { _ = governanceManagerConn.Close() }()
	}
	agentRepository := agentpostgres.NewRepository(dbPool)
	agentService := agentservice.New(agentservice.Config{
		Repository:                     agentRepository,
		Clock:                          systemClock{},
		IDGenerator:                    uuidGenerator{},
		GuidanceResolver:               guidanceResolver,
		WorkspacePolicyResolver:        workspacePolicyResolver,
		RuntimePreparer:                runtimePreparer,
		RuntimeJobCreator:              runtimeJobCreator,
		RuntimeJobReader:               runtimeJobReader,
		SelfDeployBuildPlanReader:      selfDeployBuildPlanReader,
		SelfDeployDeployPlanReader:     selfDeployDeployPlanReader,
		SelfDeployBuildContextPreparer: selfDeployBuildContextPreparer,
		SelfDeployRuntimeJobReader:     selfDeployRuntimeJobReader,
		SelfDeployBuildJobCreator:      selfDeployBuildJobCreator,
		SelfDeployDeployJobCreator:     selfDeployDeployJobCreator,
		RuntimeJobRunnerImageRef:       cfg.RuntimeJobRunnerImageRef,
		CodexSessionExecution: agentservice.CodexSessionExecutionConfig{
			ResultSchemaRef:    cfg.CodexSessionResultSchemaRef,
			ResultSchemaDigest: cfg.CodexSessionResultSchemaDigest,
			HookEndpointRef:    cfg.CodexSessionHookEndpointRef,
			TimeoutSeconds:     int32(cfg.CodexSessionTimeout.Seconds()),
		},
		ProviderFollowUpDispatcher:     providerFollowUpDispatcher,
		HumanGateRequester:             humanGateRequester,
		SelfDeployGatePreparer:         selfDeployGatePreparer,
		RuntimePreparationEnabled:      cfg.RuntimePreparationEnabled,
		RuntimeJobDispatchEnabled:      cfg.RuntimeJobDispatchEnabled,
		SelfDeployBuildDispatchEnabled: cfg.SelfDeployBuildDispatchEnabled,
		HumanGateRequestEnabled:        cfg.InteractionHubRequestEnabled,
		SelfDeployGateEnabled:          cfg.SelfDeployGovernanceGateEnabled,
		EventPublisher:                 agentservice.DisabledEventPublisher{},
	})
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           serviceprocess.NewHealthMux(readinessChecks(agentService, dbPool, eventLogPool), 2*time.Second),
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcMetrics, err := grpcserver.NewMetrics(nil, grpcserver.MetricsConfig{
		Subsystem:   "agent_manager_grpc",
		ServiceName: serviceName,
	})
	if err != nil {
		return err
	}
	grpcServer, err := grpcserver.NewServer(cfg.GRPCServerConfig(), grpcserver.Dependencies{
		Logger:        logger,
		Metrics:       grpcMetrics,
		Authenticator: grpcserver.NewSharedTokenAuthenticator(cfg.GRPCAuthToken),
		UnaryInterceptors: []grpcserver.UnaryInterceptor{
			agentgrpc.UnaryErrorInterceptor(logger),
		},
	})
	if err != nil {
		return err
	}
	agentgrpc.RegisterAgentManagerService(grpcServer, agentService)

	errCh := make(chan error, 4)
	go serviceprocess.StartHTTPServer(httpServer, serviceName, logger, errCh)
	go serviceprocess.StartGRPCServer(grpcServer, serviceName, cfg.GRPCAddr, logger, errCh)
	err = serviceprocess.StartOutboxDispatcher(
		ctx,
		serviceName,
		agentRepository,
		outboxEvent,
		serviceprocess.OutboxRuntimeConfig{
			DispatchEnabled:     cfg.OutboxDispatchEnabled,
			PublisherKind:       cfg.OutboxPublisherKind,
			AllowLossyPublisher: cfg.OutboxAllowLossy,
			EventLogSource:      cfg.OutboxEventLogSource,
			Dispatcher:          cfg.OutboxDispatcherConfig(),
		},
		serviceprocess.EventLogAppender(eventLogPool),
		logger,
		errCh,
	)
	if err != nil {
		return err
	}
	if err := startInteractionResponseConsumer(ctx, cfg, eventLogPool, agentService, logger, errCh); err != nil {
		return err
	}
	if err := startSelfDeploySignalConsumer(ctx, cfg, eventLogPool, selfDeploySignalReader, agentService, logger, errCh); err != nil {
		return err
	}
	gateDecisionConsumer := selfDeployGateDecisionConsumerStarter{cfg: cfg, eventLogPool: eventLogPool, recorder: agentService, logger: logger, errCh: errCh}
	if err := gateDecisionConsumer.start(ctx); err != nil {
		return err
	}
	if err := startSelfDeployGateReconciler(ctx, cfg, agentService, logger); err != nil {
		return err
	}
	if err := startSelfDeployRuntimeReconciler(ctx, cfg, agentService, logger); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return serviceprocess.ShutdownHTTPAndGRPC(ctx, httpServer, grpcServer, 10*time.Second)
	case err := <-errCh:
		grpcServer.Stop()
		_ = httpServer.Close()
		return err
	}
}

func newGuidanceResolver(cfg Config) (agentservice.GuidanceResolver, *grpcruntime.ClientConn, error) {
	if !cfg.PackageHubEnabled {
		return agentservice.DisabledGuidanceResolver{}, nil, nil
	}
	return connectPackageHubGuidance(packagehubclient.Config{
		Addr:      cfg.PackageHubGRPCAddr,
		AuthToken: cfg.PackageHubGRPCAuthToken,
		Timeout:   cfg.PackageHubReadTimeout,
	})
}

func connectPackageHubGuidance(clientConfig packagehubclient.Config) (agentservice.GuidanceResolver, *grpcruntime.ClientConn, error) {
	conn, err := packagehubclient.NewConnection(clientConfig)
	if err != nil {
		return nil, nil, err
	}
	resolver, err := packagehubclient.NewGuidanceResolver(packagesv1.NewPackageHubServiceClient(conn), clientConfig)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return resolver, conn, nil
}

func newProjectCatalogAdapters(cfg Config) (agentservice.WorkspacePolicyResolver, agentservice.SelfDeploySignalReader, agentservice.SelfDeployBuildPlanReader, agentservice.SelfDeployDeployPlanReader, *grpcruntime.ClientConn, error) {
	workspacePolicyResolver := agentservice.WorkspacePolicyResolver(agentservice.DisabledWorkspacePolicyResolver{})
	selfDeploySignalReader := agentservice.SelfDeploySignalReader(agentservice.DisabledSelfDeploySignalReader{})
	selfDeployBuildPlanReader := agentservice.SelfDeployBuildPlanReader(agentservice.DisabledSelfDeployBuildPlanReader{})
	selfDeployDeployPlanReader := agentservice.SelfDeployDeployPlanReader(agentservice.DisabledSelfDeployDeployPlanReader{})
	if !cfg.needsProjectCatalogClient() {
		return workspacePolicyResolver, selfDeploySignalReader, selfDeployBuildPlanReader, selfDeployDeployPlanReader, nil, nil
	}
	clientConfig := projectcatalogclient.Config{
		Addr:      cfg.ProjectCatalogGRPCAddr,
		AuthToken: cfg.ProjectCatalogGRPCAuthToken,
		Timeout:   cfg.ProjectCatalogReadTimeout,
	}
	conn, err := projectcatalogclient.NewConnection(clientConfig)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	client := projectsv1.NewProjectCatalogServiceClient(conn)
	if cfg.RuntimePreparationEnabled {
		resolver, err := projectcatalogclient.NewWorkspacePolicyResolver(client, clientConfig)
		if err != nil {
			_ = conn.Close()
			return nil, nil, nil, nil, nil, err
		}
		workspacePolicyResolver = resolver
	}
	if cfg.SelfDeploySignalConsumerEnabled {
		reader, err := projectcatalogclient.NewSelfDeploySignalReader(client, clientConfig)
		if err != nil {
			_ = conn.Close()
			return nil, nil, nil, nil, nil, err
		}
		selfDeploySignalReader = reader
	}
	if cfg.SelfDeployBuildDispatchEnabled {
		reader, err := projectcatalogclient.NewSelfDeployBuildPlanReader(client, clientConfig)
		if err != nil {
			_ = conn.Close()
			return nil, nil, nil, nil, nil, err
		}
		selfDeployBuildPlanReader = reader
		deployReader, err := projectcatalogclient.NewSelfDeployDeployPlanReader(client, clientConfig)
		if err != nil {
			_ = conn.Close()
			return nil, nil, nil, nil, nil, err
		}
		selfDeployDeployPlanReader = deployReader
	}
	return workspacePolicyResolver, selfDeploySignalReader, selfDeployBuildPlanReader, selfDeployDeployPlanReader, conn, nil
}

func newRuntimePreparer(cfg Config) (agentservice.RuntimePreparer, agentservice.RuntimeJobCreator, agentservice.RuntimeJobReader, agentservice.SelfDeployBuildContextPreparer, agentservice.SelfDeployRuntimeJobReader, agentservice.SelfDeployBuildJobCreator, agentservice.SelfDeployDeployJobCreator, *grpcruntime.ClientConn, error) {
	if !cfg.RuntimePreparationEnabled && !cfg.SelfDeployBuildDispatchEnabled {
		disabled := agentservice.DisabledRuntimePreparer{}
		disabledJob := agentservice.DisabledRuntimeJobCreator{}
		disabledReader := agentservice.DisabledRuntimeJobReader{}
		disabledBuildContext := agentservice.DisabledSelfDeployBuildContextPreparer{}
		disabledSelfDeployJobReader := agentservice.DisabledSelfDeployRuntimeJobReader{}
		disabledBuild := agentservice.DisabledSelfDeployBuildJobCreator{}
		disabledDeploy := agentservice.DisabledSelfDeployDeployJobCreator{}
		return disabled, disabledJob, disabledReader, disabledBuildContext, disabledSelfDeployJobReader, disabledBuild, disabledDeploy, nil, nil
	}
	clientConfig := runtimeclient.Config{
		Addr:      cfg.RuntimeManagerGRPCAddr,
		AuthToken: cfg.RuntimeManagerGRPCAuthToken,
		Timeout:   cfg.RuntimeManagerPrepareTimeout,
	}
	preparer, conn, err := connectOwnerService[*runtimeclient.Preparer](clientConfig, runtimeclient.NewConnection, func(conn *grpcruntime.ClientConn, clientConfig runtimeclient.Config) (*runtimeclient.Preparer, error) {
		return runtimeclient.NewPreparer(runtimev1.NewRuntimeManagerServiceClient(conn), clientConfig)
	})
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	selfDeployBuildContextPreparer := agentservice.SelfDeployBuildContextPreparer(agentservice.DisabledSelfDeployBuildContextPreparer{})
	selfDeployRuntimeJobReader := agentservice.SelfDeployRuntimeJobReader(agentservice.DisabledSelfDeployRuntimeJobReader{})
	selfDeployBuildJobCreator := agentservice.SelfDeployBuildJobCreator(agentservice.DisabledSelfDeployBuildJobCreator{})
	selfDeployDeployJobCreator := agentservice.SelfDeployDeployJobCreator(agentservice.DisabledSelfDeployDeployJobCreator{})
	if cfg.SelfDeployBuildDispatchEnabled {
		selfDeployBuildContextPreparer = preparer
		selfDeployRuntimeJobReader = preparer
		selfDeployBuildJobCreator = preparer
		selfDeployDeployJobCreator = preparer
	}
	if !cfg.RuntimePreparationEnabled {
		return agentservice.DisabledRuntimePreparer{}, agentservice.DisabledRuntimeJobCreator{}, agentservice.DisabledRuntimeJobReader{}, selfDeployBuildContextPreparer, selfDeployRuntimeJobReader, selfDeployBuildJobCreator, selfDeployDeployJobCreator, conn, nil
	}
	if !cfg.RuntimeJobDispatchEnabled {
		return preparer, agentservice.DisabledRuntimeJobCreator{}, preparer, selfDeployBuildContextPreparer, selfDeployRuntimeJobReader, selfDeployBuildJobCreator, selfDeployDeployJobCreator, conn, nil
	}
	return preparer, preparer, preparer, selfDeployBuildContextPreparer, selfDeployRuntimeJobReader, selfDeployBuildJobCreator, selfDeployDeployJobCreator, conn, nil
}

func newProviderFollowUpDispatcher(cfg Config) (agentservice.ProviderFollowUpDispatcher, *grpcruntime.ClientConn, error) {
	if !cfg.ProviderHubWriteEnabled {
		return agentservice.DisabledProviderFollowUpDispatcher{}, nil, nil
	}
	clientConfig := providerhubclient.Config{
		Addr:      cfg.ProviderHubGRPCAddr,
		AuthToken: cfg.ProviderHubGRPCAuthToken,
		Timeout:   cfg.ProviderHubWriteTimeout,
	}
	return connectGeneratedOwnerService[agentservice.ProviderFollowUpDispatcher](clientConfig, providerhubclient.NewConnection, providersv1.NewProviderHubServiceClient, func(client providersv1.ProviderHubServiceClient, clientConfig providerhubclient.Config) (agentservice.ProviderFollowUpDispatcher, error) {
		return providerhubclient.NewFollowUpDispatcher(client, clientConfig)
	})
}

func newHumanGateRequester(cfg Config) (agentservice.HumanGateInteractionRequester, *grpcruntime.ClientConn, error) {
	if !cfg.InteractionHubRequestEnabled {
		return agentservice.DisabledHumanGateInteractionRequester{}, nil, nil
	}
	clientConfig := interactionhubclient.Config{Addr: cfg.InteractionHubGRPCAddr, AuthToken: cfg.InteractionHubGRPCAuthToken, Timeout: cfg.InteractionHubRequestTimeout}
	conn, err := interactionhubclient.NewConnection(clientConfig)
	if err != nil {
		return nil, nil, err
	}
	client := interactionsv1.NewInteractionHubServiceClient(conn)
	requester, err := interactionhubclient.NewHumanGateRequester(client, clientConfig)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return requester, conn, nil
}

func newSelfDeployGatePreparer(cfg Config) (agentservice.SelfDeployGatePreparer, *grpcruntime.ClientConn, error) {
	clientConfig := governanceclient.Config{
		Addr:      cfg.GovernanceManagerGRPCAddr,
		AuthToken: cfg.GovernanceManagerGRPCAuthToken,
		Timeout:   cfg.GovernanceManagerRequestTimeout,
	}
	return optionalGeneratedOwnerService[agentservice.SelfDeployGatePreparer](
		cfg.SelfDeployGovernanceGateEnabled,
		agentservice.DisabledSelfDeployGatePreparer{},
		clientConfig,
		governanceclient.NewConnection,
		governancev1.NewGovernanceManagerServiceClient,
		func(client governancev1.GovernanceManagerServiceClient, clientConfig governanceclient.Config) (agentservice.SelfDeployGatePreparer, error) {
			return governanceclient.NewSelfDeployGatePreparer(client, clientConfig)
		},
	)
}

func optionalGeneratedOwnerService[T any, C any, P any](
	enabled bool,
	disabled T,
	clientConfig C,
	newConnection func(C) (*grpcruntime.ClientConn, error),
	newGeneratedClient func(grpcruntime.ClientConnInterface) P,
	newAdapter func(P, C) (T, error),
) (T, *grpcruntime.ClientConn, error) {
	if !enabled {
		return disabled, nil, nil
	}
	return connectGeneratedOwnerService(clientConfig, newConnection, newGeneratedClient, newAdapter)
}

func connectGeneratedOwnerService[T any, C any, P any](
	clientConfig C,
	newConnection func(C) (*grpcruntime.ClientConn, error),
	newGeneratedClient func(grpcruntime.ClientConnInterface) P,
	newAdapter func(P, C) (T, error),
) (T, *grpcruntime.ClientConn, error) {
	return connectOwnerService[T](clientConfig, newConnection, func(conn *grpcruntime.ClientConn, clientConfig C) (T, error) {
		return newAdapter(newGeneratedClient(conn), clientConfig)
	})
}

func connectOwnerService[T any, C any](
	clientConfig C,
	newConnection func(C) (*grpcruntime.ClientConn, error),
	newAdapter func(*grpcruntime.ClientConn, C) (T, error),
) (T, *grpcruntime.ClientConn, error) {
	var zero T
	conn, err := newConnection(clientConfig)
	if err != nil {
		return zero, nil, err
	}
	adapter, err := newAdapter(conn, clientConfig)
	if err != nil {
		_ = conn.Close()
		return zero, nil, err
	}
	return adapter, conn, nil
}

func readinessChecks(agentService *agentservice.Service, agentDB serviceprocess.PingStore, eventLogDB serviceprocess.PingStore) []serviceprocess.ReadinessCheck {
	return serviceprocess.ServiceDatabaseReadinessChecks(
		"agent service",
		agentService != nil && agentService.Ready(),
		"agent database",
		agentDB,
		eventLogDB,
	)
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID {
	return uuid.New()
}
