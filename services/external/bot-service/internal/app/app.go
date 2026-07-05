package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
	githubintegration "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/integration/github"
	kubernetesintegration "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/integration/kubernetes"
	mattermostintegration "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/integration/mattermost"
	adminpostgres "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	httptransport "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/transport/http"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

const serviceName = "matter-codex-bot-service"

func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	storage, closeStorage, err := openStorage(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer closeStorage()

	localizer, err := texti18n.New(cfg.Locale)
	if err != nil {
		return fmt.Errorf("open localizer: %w", err)
	}
	runtimeRunner, runtimeConfigured := openRuntimeRunner(cfg, logger)

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
		controlSurface = mattermostintegration.NewControlSurface(cfg.MattermostAPIURL(), cfg.MattermostBotToken)
		channelManager = controlSurface
		roleBotManager = controlSurface
		threadPublisher = controlSurface
		dialogOpener = controlSurface
	}
	gitHubProvider, err := openGitHubProvider(cfg)
	if err != nil {
		return err
	}
	gitHubAccountProvider := openGitHubAccountProvider(runtimeRunner, cfg)
	gitHubAccountInspector := githubintegration.NewTokenInspector()
	chatRunSvc := statusservice.NewChatRunService(statusservice.ChatRunServiceConfig{
		Localizer:       localizer,
		Store:           storage,
		RuntimeRunner:   runtimeRunner,
		ThreadPublisher: threadPublisher,
		BotServiceURL:   botServiceRuntimeURL(cfg),
		MenuActionURL:   agentsActionURL(cfg),
		StorageReady:    storage != nil,
		RuntimeReady:    runtimeConfigured,
	})
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
		RuntimeRunner:            runtimeRunner,
		DefaultTeamName:          cfg.DefaultTeamName,
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
		Localizer:          localizer,
		Store:              storage,
		RuntimeRunner:      runtimeRunner,
		ThreadPublisher:    threadPublisher,
		ConversationReader: controlSurface,
		TurnDispatcher:     chatRunSvc,
		MenuActionURL:      agentsActionURL(cfg),
		StorageReady:       storage != nil,
		RuntimeReady:       runtimeConfigured,
	})

	router := httptransport.NewRouter(httptransport.RouterConfig{
		StatusService:         statusSvc,
		SlashService:          slashSvc,
		SessionService:        sessionSvc,
		DialogOpener:          dialogOpener,
		Localizer:             localizer,
		SlashToken:            cfg.MattermostSlashToken,
		GitHubWebhookSecret:   cfg.GitHubWebhookSecret,
		MaxSlashFormBytes:     cfg.MaxSlashFormBytes,
		MaxGitHubWebhookBytes: cfg.MaxGitHubWebhookBytes,
		PrometheusRegistry:    newPrometheusRegistry(),
		Logger:                logger,
	})
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}

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
			SiteURL:   cfg.MattermostAPIURL(),
			Token:     cfg.MattermostBotToken,
			BotUserID: botUserID,
			Handler:   chatRunSvc,
			Logger:    logger,
		}); err != nil {
			logger.Warn("Mattermost chat listener disabled: configuration is invalid", "error", err)
		} else {
			go listener.Run(ctx)
		}
	}
	if runtimeConfigured && cfg.RuntimeRetentionEnabled {
		go runRuntimeRetentionLoop(ctx, runtimeRunner, cfg.RuntimeRetentionInterval, cfg.RuntimeRetentionOlderThan, logger)
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
			} else if retentionCleanupDidWork(result) {
				logger.Info(
					"runtime retention cleanup applied",
					"older_than", result.OlderThan.String(),
					"namespace", result.Namespace,
					"jobs_deleted", result.JobsDeleted,
					"pvcs_deleted", result.PVCsDeleted,
					"configmaps_deleted", result.ConfigMapsDeleted,
					"session_pods_deleted", result.SessionPodsDeleted,
					"session_pvcs_deleted", result.SessionPVCsDeleted,
					"session_secrets_deleted", result.SessionSecretsDeleted,
				)
			}
			timer.Reset(interval)
		}
	}
}

func retentionCleanupDidWork(result runtimerepo.RetentionCleanupResult) bool {
	return result.JobsDeleted > 0 ||
		result.PVCsDeleted > 0 ||
		result.ConfigMapsDeleted > 0 ||
		result.SessionPodsDeleted > 0 ||
		result.SessionPVCsDeleted > 0 ||
		result.SessionSecretsDeleted > 0
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
		JobTTLSecondsAfterFinish:              cfg.RuntimeJobTTLSeconds,
		AuthCheckJobTTLSecondsAfterFinish:     cfg.AuthCheckJobTTLSeconds,
		LogTailLines:                          cfg.RuntimeLogTailLines,
		AgentRunnerServiceAccount:             cfg.AgentServiceAccount,
		AgentRunnerClusterAdminServiceAccount: cfg.AgentClusterAdminServiceAccount,
		CodexAuthSecretName:                   cfg.CodexAuthSecretName,
		GitHubSecretName:                      cfg.GitHubSecretName,
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
	if baseURL == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(cfg.BotServiceSiteURL), "/")
	}
	return baseURL
}

func agentsActionURL(cfg Config) string {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BotServiceSiteURL), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(cfg.BotServiceInternalURL), "/")
	}
	if baseURL == "" {
		return ""
	}
	return baseURL + "/mattermost/actions/agents"
}

func agentsDialogURL(cfg Config) string {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BotServiceSiteURL), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(cfg.BotServiceInternalURL), "/")
	}
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

func openStorage(ctx context.Context, cfg Config, logger *slog.Logger) (*adminpostgres.Repository, func(), error) {
	if !cfg.DatabaseConfigured() {
		logger.Warn("storage disabled: MATTERCODEX_DATABASE_DSN is not configured")
		return nil, func() {}, nil
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseDSN)
	if err != nil {
		return nil, nil, fmt.Errorf("open storage pool: %w", err)
	}
	closePool := func() {
		pool.Close()
	}
	if err := pool.Ping(ctx); err != nil {
		closePool()
		return nil, nil, fmt.Errorf("ping storage: %w", err)
	}
	if cfg.StorageMigrations {
		if err := migrations.Run(ctx, cfg.DatabaseDSN); err != nil {
			closePool()
			return nil, nil, fmt.Errorf("run storage migrations: %w", err)
		}
	}
	repo := adminpostgres.NewRepository(pool)
	seeded, err := statusservice.SeedDefaultAgentPromptTemplates(ctx, repo)
	if err != nil {
		closePool()
		return nil, nil, fmt.Errorf("seed default agent prompt templates: %w", err)
	}
	if seeded > 0 {
		logger.Info("seeded default agent prompt templates", "count", seeded)
	}
	return repo, closePool, nil
}
