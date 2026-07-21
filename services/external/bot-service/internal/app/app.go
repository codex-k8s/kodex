package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	domainartifact "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/artifact"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
	githubintegration "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/integration/github"
	kubernetesintegration "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/integration/kubernetes"
	mattermostintegration "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/integration/mattermost"
	s3integration "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/integration/s3"
	adminpostgres "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	artifactpostgres "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/artifact"
	automationspostgres "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/automations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	httptransport "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/transport/http"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

const serviceName = "matter-codex-bot-service"

func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	runtimeRunner, runtimeConfigured := openRuntimeRunner(cfg, logger)
	storage, storagePool, closeStorage, err := openStorage(ctx, cfg, logger, runtimeRunner)
	if err != nil {
		return err
	}
	defer closeStorage()
	var automationStorage *automationspostgres.Repository
	if storagePool != nil {
		automationStorage = automationspostgres.NewRepository(storagePool)
	}

	localizer, err := texti18n.New(cfg.Locale)
	if err != nil {
		return fmt.Errorf("open localizer: %w", err)
	}
	statusSvc := statusservice.NewStatusService(statusservice.Config{
		Localizer:            localizer,
		ServiceName:          serviceName,
		ServiceVersion:       statusservice.Version("0.1.0"),
		MattermostConfigured: cfg.MattermostSiteURL != "",
		BotTokenConfigured:   cfg.BotTokenConfigured(),
		SlashTokenConfigured: cfg.SlashTokenConfigured(),
		DatabaseConfigured:   cfg.DatabaseConfigured(),
		StorageReady:         storage != nil,
		RuntimeConfigured:    runtimeConfigured,
		DefaultTeamName:      cfg.DefaultTeamName,
		DefaultChannels:      cfg.ChannelNames(),
	})
	var channelManager statusservice.MattermostChannelManager
	var roleBotManager statusservice.MattermostRoleBotManager
	var threadPublisher statusservice.MattermostThreadPublisher
	var dialogOpener httptransport.DialogOpener
	var controlSurface *mattermostintegration.ControlSurface
	if cfg.BotTokenConfigured() && cfg.MattermostAPIURL() != "" {
		controlSurface = mattermostintegration.NewControlSurfaceWithHTTPConfig(
			cfg.MattermostAPIURL(), cfg.MattermostBotToken, cfg.MattermostAdminToken,
			mattermostintegration.HTTPClientConfig{
				Timeout: cfg.MattermostHTTPTimeout, DialTimeout: cfg.MattermostHTTPDialTimeout,
				TLSHandshakeTimeout:   cfg.MattermostHTTPTLSHandshakeTimeout,
				ResponseHeaderTimeout: cfg.MattermostHTTPResponseHeaderTimeout,
				IdleConnTimeout:       cfg.MattermostHTTPIdleConnTimeout,
			},
		)
	}
	var artifactSvc *domainartifact.Service
	if cfg.ArtifactsEnabled {
		if !runtimeConfigured || runtimeRunner == nil || storagePool == nil || controlSurface == nil {
			return fmt.Errorf("artifact storage dependencies are not ready")
		}
		objectStore, err := s3integration.New(ctx, s3integration.Config{
			Endpoint: cfg.ArtifactS3Endpoint, Region: cfg.ArtifactS3Region, Bucket: cfg.ArtifactS3Bucket,
			AccessKeyID: cfg.ArtifactS3AccessKeyID, SecretAccessKey: cfg.ArtifactS3SecretAccessKey,
			UsePathStyle: cfg.ArtifactS3UsePathStyle,
		})
		if err != nil {
			return err
		}
		delivery, err := mattermostintegration.NewArtifactDelivery(controlSurface, runtimeRunner)
		if err != nil {
			return err
		}
		artifactSvc, err = domainartifact.NewService(domainartifact.ServiceConfig{
			Repository: artifactpostgres.NewRepository(storagePool), Source: controlSurface, ObjectStore: objectStore, Delivery: delivery,
			MaxFilesPerTurn: cfg.ArtifactMaxFilesPerTurn, MaxObjectBytes: cfg.ArtifactMaxObjectBytes,
			MaxTurnBytes: cfg.ArtifactMaxTurnBytes, Retention: cfg.ArtifactRetention,
		})
		if err != nil {
			return err
		}
	}
	interactionSecurity := statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{
		Repository: storage,
		Admission:  statusservice.NewServerSideInteractionAdmission("", controlSurface, storage),
	})
	if controlSurface != nil {
		channelManager = controlSurface
		roleBotManager = controlSurface
		threadPublisher = statusservice.NewSecuredMattermostThreadPublisher(controlSurface, interactionSecurity)
		dialogOpener = controlSurface
	}
	gitHubProvider, err := openGitHubProvider(cfg)
	if err != nil {
		return err
	}
	gitHubAccountProvider := openGitHubAccountProvider(runtimeRunner, cfg)
	gitHubAccountInspector := githubintegration.NewTokenInspector()
	chatRunSvc := statusservice.NewChatRunService(statusservice.ChatRunServiceConfig{
		Localizer:         localizer,
		Store:             storage,
		RuntimeRunner:     runtimeRunner,
		ThreadPublisher:   threadPublisher,
		BotServiceURL:     botServiceRuntimeURL(cfg),
		MenuActionURL:     agentsActionURL(cfg),
		MattermostSiteURL: cfg.MattermostSiteURL,
		StorageReady:      storage != nil,
		RuntimeReady:      runtimeConfigured,
		Artifacts:         artifactSvc,
	})
	automationSvc := statusservice.NewAutomationService(statusservice.AutomationServiceConfig{
		Repository:              automationStorage,
		Catalog:                 storage,
		Dispatcher:              chatRunSvc,
		Publisher:               threadPublisher,
		OwnerMattermostUsername: cfg.OwnerMattermostUsername,
		StorageReady:            automationStorage != nil,
		RuntimeReady:            runtimeConfigured,
	})
	chatRunSvc.SetAutomationRuntimeReconciler(automationSvc)
	slashSvc := statusservice.NewSlashCommandService(statusservice.SlashCommandServiceConfig{
		Localizer:                localizer,
		StatusService:            statusSvc,
		Store:                    storage,
		ChannelManager:           channelManager,
		RoleBotManager:           roleBotManager,
		RepositoryProvider:       gitHubProvider,
		GitHubRepositoryProvider: gitHubAccountProvider,
		GitHubAccountInspector:   gitHubAccountInspector,
		ThreadRepositorySelector: chatRunSvc,
		Automations:              automationSvc,
		RuntimeRunner:            runtimeRunner,
		DefaultTeamName:          cfg.DefaultTeamName,
		OwnerMattermostUsername:  cfg.OwnerMattermostUsername,
		CodexAuthSecretName:      cfg.CodexAuthSecretName,
		GitHubSecretName:         cfg.GitHubSecretName,
		MenuActionURL:            agentsActionURL(cfg),
		DialogSubmitURL:          agentsDialogURL(cfg),
		BotTokenConfigured:       cfg.BotTokenConfigured(),
		SlashTokenConfigured:     cfg.SlashTokenConfigured(),
		GitHubTokenConfigured:    cfg.GitHubTokenConfigured(),
		GitHubWebhookConfigured:  cfg.GitHubWebhookConfigured(),
		DatabaseConfigured:       cfg.DatabaseConfigured(),
		StorageReady:             storage != nil,
		RuntimeConfigured:        runtimeConfigured,
		MattermostConfigured:     cfg.MattermostSiteURL != "",
		ChannelManagerEnabled:    channelManager != nil,
	})
	if err := slashSvc.BootstrapSystemAgentRoles(ctx); err != nil {
		logger.Warn("system agent role bootstrap failed", "error", err)
	}
	sessionSvc := statusservice.NewAgentSessionService(statusservice.AgentSessionServiceConfig{
		Localizer:                   localizer,
		Store:                       storage,
		RuntimeRunner:               runtimeRunner,
		ThreadPublisher:             threadPublisher,
		ConversationReader:          controlSurface,
		RoleBotManager:              roleBotManager,
		TurnDispatcher:              chatRunSvc,
		AutomationCallbacks:         automationSvc,
		AutomationRuntimeReconciler: automationSvc,
		MenuActionURL:               agentsActionURL(cfg),
		MattermostSiteURL:           cfg.MattermostSiteURL,
		StorageReady:                storage != nil,
		RuntimeReady:                runtimeConfigured,
		CallbackMaxBytes:            cfg.CallbackMaxBytes,
		CallbackMaxChunks:           cfg.CallbackMaxChunks,
		CallbackMaxChunkBytes:       cfg.CallbackMaxChunkBytes,
		CallbackPublishConcurrency:  cfg.CallbackPublishConcurrency,
		CallbackPublishDeadline:     cfg.CallbackPublishDeadline,
		Artifacts:                   artifactSvc,
	})

	router := httptransport.NewRouter(httptransport.RouterConfig{
		StatusService:          statusSvc,
		SlashService:           slashSvc,
		SessionService:         sessionSvc,
		DialogOpener:           dialogOpener,
		InteractionSecurity:    interactionSecurity,
		Localizer:              localizer,
		SlashToken:             cfg.MattermostSlashToken,
		GitHubWebhookSecret:    cfg.GitHubWebhookSecret,
		MaxSlashFormBytes:      cfg.MaxSlashFormBytes,
		MaxGitHubWebhookBytes:  cfg.MaxGitHubWebhookBytes,
		MaxMCPRequestBodyBytes: cfg.MaxMCPRequestBodyBytes,
		MaxArtifactBodyBytes:   cfg.ArtifactMaxObjectBytes,
		PrometheusRegistry:     newPrometheusRegistry(),
		MattermostSiteURL:      cfg.MattermostSiteURL,
		MattermostInternalURL:  cfg.MattermostInternalURL,
		ThreadPublisher:        threadPublisher,
		Logger:                 logger,
	})
	server := newHTTPServer(cfg, router)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("bot-service listening", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	if controlSurface != nil {
		botUserID, err := controlSurface.BotUserID(ctx)
		if err != nil {
			logger.Warn("Mattermost chat listener disabled: bot user was not resolved", "error", err)
		} else if listener, err := mattermostintegration.NewChatListener(mattermostintegration.ChatListenerConfig{
			SiteURL:          cfg.MattermostAPIURL(),
			Token:            cfg.MattermostBotToken,
			BotUserID:        botUserID,
			Handler:          chatRunSvc,
			UserNameResolver: controlSurface,
			Logger:           logger,
		}); err != nil {
			logger.Warn("Mattermost chat listener disabled: configuration is invalid", "error", err)
		} else {
			go listener.Run(ctx)
		}
	}
	if runtimeConfigured && cfg.RuntimeRetentionEnabled {
		go runRuntimeRetentionLoop(ctx, runtimeRunner, cfg.RuntimeRetentionInterval, cfg.RuntimeRetentionOlderThan, logger)
	}
	if runtimeConfigured && cfg.RuntimeSessionRepairEnabled {
		go runAgentSessionRepairLoop(ctx, chatRunSvc, cfg.RuntimeSessionRepairInterval, cfg.RuntimeSessionRepairBatch, logger)
	}
	if storage != nil && cfg.InteractionCleanupEnabled {
		go runInteractionCapabilityCleanupLoop(ctx, storage, cfg.InteractionCleanupInterval, cfg.InteractionCleanupRetention, cfg.InteractionCleanupBatch, logger)
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func newHTTPServer(cfg Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}
}

func runInteractionCapabilityCleanupLoop(ctx context.Context, repository securityrepo.CapabilityCleanupRepository, interval time.Duration, retention time.Duration, batch int, logger *slog.Logger) {
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			deleted, err := cleanupInteractionCapabilities(ctx, repository, time.Now().UTC(), retention, batch)
			if err != nil {
				logger.Warn("interaction capability cleanup failed", "error", err)
			} else if deleted > 0 {
				logger.Info("interaction capability cleanup applied", "rows_deleted", deleted, "batch_limit", batch)
			}
			timer.Reset(interval)
		}
	}
}

func cleanupInteractionCapabilities(ctx context.Context, repository securityrepo.CapabilityCleanupRepository, now time.Time, retention time.Duration, batch int) (int64, error) {
	if repository == nil || retention <= 0 || batch <= 0 {
		return 0, fmt.Errorf("interaction capability cleanup is not configured")
	}
	return repository.CleanupInteractionCapabilities(ctx, securityrepo.CapabilityCleanupInput{
		DeleteBefore: now.UTC().Add(-retention),
		Limit:        batch,
	})
}

func runAgentSessionRepairLoop(ctx context.Context, svc *statusservice.ChatRunService, interval time.Duration, batch int, logger *slog.Logger) {
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			result, err := svc.RepairAgentSessions(ctx, batch)
			if err != nil {
				logger.Warn("agent session repair failed", "error", err)
			} else if agentSessionRepairDidWork(result) {
				logger.Info(
					"agent session repair applied",
					"queued_sessions_ensured", result.QueuedSessionsEnsured,
					"stale_sessions_reset", result.StaleSessionsReset,
					"failed", result.Failed,
				)
			}
			timer.Reset(interval)
		}
	}
}

func agentSessionRepairDidWork(result statusservice.AgentSessionRepairResult) bool {
	return result.QueuedSessionsEnsured > 0 ||
		result.StaleSessionsReset > 0 ||
		result.Failed > 0
}

func runRuntimeRetentionLoop(ctx context.Context, runner runtimerepo.Runner, interval time.Duration, olderThan time.Duration, logger *slog.Logger) {
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			result, err := runner.CleanupExpiredRuns(ctx, runtimerepo.RetentionCleanupInput{
				OlderThan: olderThan,
				Now:       time.Now().UTC(),
				DryRun:    false,
			})
			if err != nil {
				logger.Warn("runtime retention cleanup failed", "error", err)
			} else if retentionCleanupHasReport(result) {
				logger.Info(
					"runtime retention pass completed",
					"older_than", result.OlderThan.String(),
					"namespace", result.Namespace,
					"session_data_mode", result.SessionDataMode,
					"jobs_deleted", result.JobsDeleted,
					"pvcs_deleted", result.PVCsDeleted,
					"configmaps_deleted", result.ConfigMapsDeleted,
					"session_pods_deleted", result.SessionPodsDeleted,
					"session_pvcs_inventoried", result.SessionPVCsMatched,
					"session_pvcs_deleted", result.SessionPVCsDeleted,
					"session_secrets_inventoried", result.SessionSecretsMatched,
					"session_secrets_deleted", result.SessionSecretsDeleted,
				)
			}
			timer.Reset(interval)
		}
	}
}

func retentionCleanupHasReport(result runtimerepo.RetentionCleanupResult) bool {
	return result.JobsDeleted > 0 ||
		result.PVCsDeleted > 0 ||
		result.ConfigMapsDeleted > 0 ||
		result.SessionPodsDeleted > 0 ||
		result.SessionPVCsMatched > 0 ||
		result.SessionSecretsMatched > 0
}

func openRuntimeRunner(cfg Config, logger *slog.Logger) (runtimerepo.Runner, bool) {
	if !cfg.RuntimeEnabled {
		logger.Warn("kubernetes runtime disabled: MATTERCODEX_RUNTIME_ENABLED is false")
		return nil, false
	}
	runner, err := kubernetesintegration.NewRunner(kubernetesintegration.Config{
		Namespace:                             cfg.RuntimeNamespace,
		KubeconfigPath:                        cfg.RuntimeKubeconfigPath,
		SmokeImage:                            cfg.RuntimeSmokeImage,
		AgentRunnerImage:                      cfg.AgentRunnerImage,
		CodexPackage:                          cfg.CodexPackage,
		WorkspaceStorageSize:                  cfg.RuntimeWorkspaceSize,
		SessionCPURequest:                     cfg.AgentSessionCPURequest,
		SessionMemoryRequest:                  cfg.AgentSessionMemoryRequest,
		SessionMemoryLimit:                    cfg.AgentSessionMemoryLimit,
		UtilityMemoryLimit:                    cfg.AgentUtilityMemoryLimit,
		DevShmSizeLimit:                       cfg.AgentDevShmSizeLimit,
		JobTTLSecondsAfterFinish:              cfg.RuntimeJobTTLSeconds,
		AuthCheckJobTTLSecondsAfterFinish:     cfg.AuthCheckJobTTLSeconds,
		LogTailLines:                          cfg.RuntimeLogTailLines,
		AgentRunnerServiceAccount:             cfg.AgentServiceAccount,
		AgentRunnerClusterAdminServiceAccount: cfg.AgentClusterAdminServiceAccount,
		CodexAuthSecretName:                   cfg.CodexAuthSecretName,
		GitHubSecretName:                      cfg.GitHubSecretName,
		ArtifactStorageSecretName:             cfg.ArtifactStorageSecretName,
	})
	if err != nil {
		logger.Warn("kubernetes runtime disabled: client-go runner is not configured", "error", err)
		return nil, false
	}
	return runner, true
}

func openGitHubProvider(cfg Config) (*githubintegration.Provider, error) {
	if !cfg.GitHubTokenConfigured() {
		return nil, nil
	}
	provider, err := githubintegration.NewProvider(githubintegration.ProviderConfig{
		Token:         cfg.GitHubToken,
		WebhookURL:    gitHubWebhookURL(cfg),
		WebhookSecret: cfg.GitHubWebhookSecret,
	})
	if err != nil {
		return nil, fmt.Errorf("open github provider: %w", err)
	}
	return provider, nil
}

func openGitHubAccountProvider(runtimeRunner runtimerepo.Runner, cfg Config) *githubintegration.AccountProvider {
	tokenReader, ok := runtimeRunner.(runtimerepo.GitHubTokenSecretReader)
	if !ok {
		return nil
	}
	return githubintegration.NewAccountProvider(tokenReader, githubintegration.ProviderConfig{
		WebhookURL:    gitHubWebhookURL(cfg),
		WebhookSecret: cfg.GitHubWebhookSecret,
	})
}

func gitHubWebhookURL(cfg Config) string {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BotServiceSiteURL), "/")
	if baseURL == "" {
		return ""
	}
	return baseURL + "/github/webhook"
}

func botServiceRuntimeURL(cfg Config) string {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BotServiceInternalURL), "/")
	return baseURL
}

func agentsActionURL(cfg Config) string {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BotServiceInternalURL), "/")
	if baseURL == "" {
		return ""
	}
	return baseURL + "/mattermost/actions/agents"
}

func agentsDialogURL(cfg Config) string {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BotServiceInternalURL), "/")
	if baseURL == "" {
		return ""
	}
	return baseURL + "/mattermost/dialogs/agents"
}

func newPrometheusRegistry() *prometheus.Registry {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return registry
}

func openStorage(ctx context.Context, cfg Config, logger *slog.Logger, runtimeRunner runtimerepo.Runner) (*adminpostgres.Repository, *pgxpool.Pool, func(), error) {
	if !cfg.DatabaseConfigured() {
		logger.Warn("storage disabled: MATTERCODEX_DATABASE_DSN is not configured")
		return nil, nil, func() {}, nil
	}
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseDSN)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse storage pool config: %w", err)
	}
	if cfg.StorageMigrations {
		if err := adminpostgres.ProvisionRuntimeDatabaseRole(ctx, cfg.MigrationsDatabaseDSN, poolConfig.ConnConfig.User, poolConfig.ConnConfig.Password); err != nil {
			return nil, nil, nil, fmt.Errorf("provision runtime database role: %w", err)
		}
		if err := migrations.RunTo(ctx, cfg.MigrationsDatabaseDSN, 24); err != nil {
			return nil, nil, nil, fmt.Errorf("run storage migrations through integrity staging schema: %w", err)
		}
		blockedSessions, err := prepareClusterAdminSecretIntegrity(ctx, cfg.MigrationsDatabaseDSN, runtimeRunner)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("prepare cluster-admin secret integrity: %w", err)
		}
		if blockedSessions > 0 {
			logger.Warn("blocked cluster-admin sessions with missing runtime token secrets", "count", blockedSessions)
		}
		if err := migrations.RunForRuntimeRole(ctx, cfg.MigrationsDatabaseDSN, poolConfig.ConnConfig.User); err != nil {
			return nil, nil, nil, fmt.Errorf("run storage migrations: %w", err)
		}
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open storage pool: %w", err)
	}
	closePool := func() {
		pool.Close()
	}
	if err := pool.Ping(ctx); err != nil {
		closePool()
		return nil, nil, nil, fmt.Errorf("ping storage: %w", err)
	}
	if err := adminpostgres.ValidateRuntimeDatabaseRole(ctx, pool); err != nil {
		closePool()
		return nil, nil, nil, fmt.Errorf("validate runtime database role: %w", err)
	}
	repo := adminpostgres.NewRepository(pool)
	seeded, err := statusservice.SeedDefaultAgentPromptTemplates(ctx, repo)
	if err != nil {
		closePool()
		return nil, nil, nil, fmt.Errorf("seed default agent prompt templates: %w", err)
	}
	if seeded > 0 {
		logger.Info("seeded default agent prompt templates", "count", seeded)
	}
	return repo, pool, closePool, nil
}

type secretIntegrityStagingRow struct {
	tableName  string
	id         int64
	secretName string
	secretKey  string
	sessionKey string
}

func prepareClusterAdminSecretIntegrity(ctx context.Context, dsn string, runtimeRunner runtimerepo.Runner) (int, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return 0, fmt.Errorf("open integrity staging database: %w", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
select 'matter_codex_credentials', credential.id, credential.secret_ref,
	case when account_kind = 'openai' then 'auth.json' else 'github-token' end, ''
from matter_codex_credentials credential
join (
	select distinct account.credential_id, 'openai' as account_kind
	from matter_codex_openai_accounts account
	where exists (
		select 1 from matter_codex_agent_profiles profile
		where lower(trim(profile.kubernetes_access)) = 'cluster-admin'
			and profile.openai_account_name = account.name
	) or exists (
		select 1 from matter_codex_agent_roles role
		where lower(trim(role.kubernetes_access)) = 'cluster-admin'
			and role.openai_account_name = account.name
	)
	union
	select distinct account.credential_id, 'github'
	from matter_codex_github_accounts account
	where exists (
		select 1 from matter_codex_agent_profiles profile
		where lower(trim(profile.kubernetes_access)) = 'cluster-admin'
			and profile.github_account_name = account.name
	) or exists (
		select 1
		from matter_codex_agent_roles role
		join matter_codex_projects project on project.id = role.project_id
		where lower(trim(role.kubernetes_access)) = 'cluster-admin'
			and account.name in (role.github_account_name, project.github_account_name)
	) or exists (
		select 1
		from matter_codex_agent_roles role
		join matter_codex_project_repositories project_repository
			on project_repository.project_id = role.project_id
		join matter_codex_repositories repository
			on repository.id = project_repository.repository_id
		where lower(trim(role.kubernetes_access)) = 'cluster-admin'
			and account.name = repository.github_account_name
	) or exists (
		select 1
		from matter_codex_agent_roles role
		join matter_codex_chat_participants participant
			on participant.role_id = role.id and participant.enabled
		join matter_codex_chat_repositories chat_repository
			on chat_repository.chat_id = participant.chat_id
		join matter_codex_repositories repository
			on repository.id = chat_repository.repository_id
		where lower(trim(role.kubernetes_access)) = 'cluster-admin'
			and account.name = repository.github_account_name
	)
) referenced on referenced.credential_id = credential.id
where trim(credential.secret_ref) <> ''
union all
select 'matter_codex_project_runtime_variables', variable.id, variable.secret_ref, variable.secret_key, ''
from matter_codex_project_runtime_variables variable
join matter_codex_agent_role_runtime_variables binding on binding.variable_id = variable.id
join matter_codex_agent_roles role on role.id = binding.role_id
where lower(trim(role.kubernetes_access)) = 'cluster-admin'
union all
select 'matter_codex_mattermost_bot_identities', binding.id, binding.token_secret_ref, 'token', ''
from matter_codex_mattermost_bot_identities binding
join matter_codex_agent_roles role on role.id = binding.role_id
where lower(trim(role.kubernetes_access)) = 'cluster-admin'
union all
select 'matter_codex_agent_sessions', session.id, session.token_secret_ref, 'token', session.session_key
from matter_codex_agent_sessions session
join matter_codex_agent_roles role on role.id = session.role_id
where lower(trim(role.kubernetes_access)) = 'cluster-admin'
	and trim(session.token_secret_ref) <> ''
	and session.status not in ('blocked', 'closed')`)
	if err != nil {
		return 0, fmt.Errorf("list cluster-admin secret bindings: %w", err)
	}
	defer rows.Close()
	var bindings []secretIntegrityStagingRow
	for rows.Next() {
		var item secretIntegrityStagingRow
		if err := rows.Scan(&item.tableName, &item.id, &item.secretName, &item.secretKey, &item.sessionKey); err != nil {
			return 0, fmt.Errorf("scan cluster-admin secret binding: %w", err)
		}
		bindings = append(bindings, item)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read cluster-admin secret bindings: %w", err)
	}
	if len(bindings) > 0 && runtimeRunner == nil {
		return 0, fmt.Errorf("Kubernetes runtime is required to stage frozen secret integrity")
	}
	blockedSessions := 0
	for _, binding := range bindings {
		integrity, err := runtimeRunner.InspectSecretIntegrity(ctx, runtimerepo.SecretIntegrityInput{
			SecretName: binding.secretName,
			SecretKey:  binding.secretKey,
		})
		if err != nil {
			if binding.tableName == "matter_codex_agent_sessions" && errors.Is(err, runtimerepo.ErrSecretNotFound) {
				if err := isolateClusterAdminSessionWithMissingToken(
					ctx,
					binding.id,
					binding.sessionKey,
					func(ctx context.Context, sessionID int64, isolate func(context.Context) error) error {
						return blockClusterAdminSessionWithMissingToken(ctx, db, sessionID, isolate)
					},
					func(ctx context.Context, sessionKey string) error {
						_, cleanupErr := runtimeRunner.CleanupAgentSession(ctx, sessionKey)
						return cleanupErr
					},
				); err != nil {
					return blockedSessions, err
				}
				blockedSessions++
				continue
			}
			return blockedSessions, fmt.Errorf("inspect frozen secret binding %s/%d: %w", binding.tableName, binding.id, err)
		}
		statement := fmt.Sprintf(`update %s set secret_content_sha256 = $1, secret_resource_uid = $2, secret_resource_version = $3 where id = $4`, binding.tableName)
		if _, err := db.ExecContext(ctx, statement, integrity.ContentSHA256, integrity.UID, integrity.ResourceVersion, binding.id); err != nil {
			return blockedSessions, fmt.Errorf("store frozen secret metadata for %s/%d: %w", binding.tableName, binding.id, err)
		}
	}
	return blockedSessions, nil
}

func isolateClusterAdminSessionWithMissingToken(
	ctx context.Context,
	sessionID int64,
	sessionKey string,
	block func(context.Context, int64, func(context.Context) error) error,
	cleanup func(context.Context, string) error,
) error {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return fmt.Errorf("cluster-admin session %d with missing token has no session key", sessionID)
	}
	return block(ctx, sessionID, func(ctx context.Context) error {
		if err := cleanup(ctx, sessionKey); err != nil {
			return fmt.Errorf("delete cluster-admin session %d pod: %w", sessionID, err)
		}
		return nil
	})
}

func blockClusterAdminSessionWithMissingToken(
	ctx context.Context,
	db *sql.DB,
	sessionID int64,
	isolate func(context.Context) error,
) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin missing cluster-admin session token recovery: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
update matter_codex_agent_sessions
set status = 'blocked', active_turn_id = null, active_run_id = '', updated_at = now()
where id = $1 and status not in ('blocked', 'closed')`, sessionID); err != nil {
		return fmt.Errorf("block cluster-admin session %d with missing token: %w", sessionID, err)
	}
	if _, err := tx.ExecContext(ctx, `
update matter_codex_agent_session_turns
set status = 'failed',
	error_message = case when trim(error_message) = '' then 'cluster-admin session token secret is missing' else error_message end,
	finished_at = coalesce(finished_at, now()), updated_at = now()
where session_id = $1 and status in ('queued', 'running')`, sessionID); err != nil {
		return fmt.Errorf("fail unfinished turns for cluster-admin session %d with missing token: %w", sessionID, err)
	}
	if err := isolate(ctx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit missing cluster-admin session token recovery: %w", err)
	}
	return nil
}
