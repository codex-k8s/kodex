package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
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
	runtimeRunner, runtimeConfigured := openRuntimeRunner(cfg, logger)
	storage, closeStorage, err := openStorage(ctx, cfg, logger, runtimeRunner)
	if err != nil {
		return err
	}
	defer closeStorage()
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
		ThreadPublisher:   threadPublisher,
		BotServiceURL:     botServiceRuntimeURL(cfg),
		MenuActionURL:     agentsActionURL(cfg),
		MattermostSiteURL: cfg.MattermostSiteURL,
		StorageReady:      storage != nil,
		RuntimeReady:      false,
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
		DisableLegacyRuntime:     true,
		MattermostConfigured:     cfg.MattermostSiteURL != "",
		ChannelManagerEnabled:    channelManager != nil,
	})
	if err := slashSvc.BootstrapSystemAgentRoles(ctx); err != nil {
		logger.Warn("system agent role bootstrap failed", "error", err)
	}
	sessionSvc := statusservice.NewAgentSessionService(statusservice.AgentSessionServiceConfig{
		Localizer:                  localizer,
		Store:                      storage,
		RuntimeRunner:              runtimeRunner,
		ThreadPublisher:            threadPublisher,
		ConversationReader:         controlSurface,
		RoleBotManager:             roleBotManager,
		MenuActionURL:              agentsActionURL(cfg),
		MattermostSiteURL:          cfg.MattermostSiteURL,
		StorageReady:               storage != nil,
		RuntimeReady:               runtimeConfigured,
		CallbackMaxBytes:           cfg.CallbackMaxBytes,
		CallbackMaxChunks:          cfg.CallbackMaxChunks,
		CallbackMaxChunkBytes:      cfg.CallbackMaxChunkBytes,
		CallbackPublishConcurrency: cfg.CallbackPublishConcurrency,
		CallbackPublishDeadline:    cfg.CallbackPublishDeadline,
	})

	router := httptransport.NewRouter(httptransport.RouterConfig{
		StatusService:                   statusSvc,
		SlashService:                    slashSvc,
		SessionService:                  sessionSvc,
		DialogOpener:                    dialogOpener,
		InteractionSecurity:             interactionSecurity,
		Localizer:                       localizer,
		SlashToken:                      cfg.MattermostSlashToken,
		GitHubWebhookSecret:             cfg.GitHubWebhookSecret,
		MaxSlashFormBytes:               cfg.MaxSlashFormBytes,
		MaxGitHubWebhookBytes:           cfg.MaxGitHubWebhookBytes,
		MaxMCPRequestBodyBytes:          cfg.MaxMCPRequestBodyBytes,
		PrometheusRegistry:              newPrometheusRegistry(),
		MattermostSiteURL:               cfg.MattermostSiteURL,
		MattermostInternalURL:           cfg.MattermostInternalURL,
		ThreadPublisher:                 threadPublisher,
		ControlCenterAssetsDir:          cfg.ControlCenterAssetsDir,
		Logger:                          logger,
		RuntimeMCPBindingClientSPIFFEID: cfg.RuntimeMCPBindingClientSPIFFEID,
	})
	server := newHTTPServer(cfg, router)
	var runtimeServer *http.Server
	var runtimeListener net.Listener
	if cfg.RuntimeEnabled {
		runtimeServer, err = newRuntimeTLSServer(cfg, router)
		if err != nil {
			return err
		}
		certificate, certificateErr := tls.LoadX509KeyPair(cfg.RuntimeTLSCertificateFile, cfg.RuntimeTLSPrivateKeyFile)
		if certificateErr != nil {
			return fmt.Errorf("load bot-service runtime TLS identity")
		}
		tlsConfig := runtimeServer.TLSConfig.Clone()
		tlsConfig.Certificates = []tls.Certificate{certificate}
		runtimeListener, err = tls.Listen("tcp", cfg.RuntimeTLSAddr, tlsConfig)
		if err != nil {
			return fmt.Errorf("bind bot-service runtime mTLS listener: %w", err)
		}
		defer runtimeListener.Close()
	}

	errCh := make(chan error, 2)
	go func() {
		logger.Info("bot-service listening", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	if runtimeServer != nil {
		go func() {
			logger.Info("bot-service runtime mTLS listening", "addr", cfg.RuntimeTLSAddr)
			if err := runtimeServer.Serve(runtimeListener); err != nil && err != http.ErrServerClosed {
				errCh <- err
				return
			}
			errCh <- nil
		}()
	}
	if storage != nil && cfg.InteractionCleanupEnabled {
		go runInteractionCapabilityCleanupLoop(ctx, storage, cfg.InteractionCleanupInterval, cfg.InteractionCleanupRetention, cfg.InteractionCleanupBatch, logger)
	}
	select {
	case <-ctx.Done():
		var shutdownErrors []error
		if runtimeServer != nil {
			runtimeShutdownCtx, cancelRuntime := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
			if err := runtimeServer.Shutdown(runtimeShutdownCtx); err != nil {
				shutdownErrors = append(shutdownErrors, err)
			}
			cancelRuntime()
		}
		publicShutdownCtx, cancelPublic := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
		if err := server.Shutdown(publicShutdownCtx); err != nil {
			shutdownErrors = append(shutdownErrors, err)
		}
		cancelPublic()
		return errors.Join(shutdownErrors...)
	case err := <-errCh:
		return err
	}
}

func newRuntimeTLSServer(cfg Config, handler http.Handler) (*http.Server, error) {
	caRaw, err := os.ReadFile(cfg.RuntimeTLSClientCAFile)
	if err != nil || len(caRaw) == 0 || len(caRaw) > 1<<20 {
		return nil, fmt.Errorf("read bot-service runtime client CA")
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(caRaw) {
		return nil, fmt.Errorf("parse bot-service runtime client CA")
	}
	server := newHTTPServer(cfg, handler)
	server.Addr = cfg.RuntimeTLSAddr
	server.TLSConfig = &tls.Config{
		MinVersion: tls.VersionTLS13,
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  clientCAs,
	}
	return server, nil
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

func openStorage(ctx context.Context, cfg Config, logger *slog.Logger, runtimeRunner runtimerepo.Runner) (*adminpostgres.Repository, func(), error) {
	if !cfg.DatabaseConfigured() {
		logger.Warn("storage disabled: MATTERCODEX_DATABASE_DSN is not configured")
		return nil, func() {}, nil
	}
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseDSN)
	if err != nil {
		return nil, nil, fmt.Errorf("parse storage pool config: %w", err)
	}
	if cfg.StorageMigrations {
		if err := adminpostgres.ProvisionRuntimeDatabaseRole(ctx, cfg.MigrationsDatabaseDSN, poolConfig.ConnConfig.User, poolConfig.ConnConfig.Password); err != nil {
			return nil, nil, fmt.Errorf("provision runtime database role: %w", err)
		}
		if err := migrations.RunTo(ctx, cfg.MigrationsDatabaseDSN, 24); err != nil {
			return nil, nil, fmt.Errorf("run storage migrations through integrity staging schema: %w", err)
		}
		if err := prepareClusterAdminSecretIntegrity(ctx, cfg.MigrationsDatabaseDSN, runtimeRunner); err != nil {
			return nil, nil, fmt.Errorf("prepare cluster-admin secret integrity: %w", err)
		}
		if err := migrations.RunForRuntimeRole(ctx, cfg.MigrationsDatabaseDSN, poolConfig.ConnConfig.User); err != nil {
			return nil, nil, fmt.Errorf("run storage migrations: %w", err)
		}
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
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
	if err := adminpostgres.ValidateRuntimeDatabaseRole(ctx, pool); err != nil {
		closePool()
		return nil, nil, fmt.Errorf("validate runtime database role: %w", err)
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

type secretIntegrityStagingRow struct {
	tableName  string
	id         int64
	secretName string
	secretKey  string
}

func prepareClusterAdminSecretIntegrity(ctx context.Context, dsn string, runtimeRunner runtimerepo.Runner) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open integrity staging database: %w", err)
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
where lower(trim(role.kubernetes_access)) = 'cluster-admin'`)
	if err != nil {
		return fmt.Errorf("list cluster-admin secret bindings: %w", err)
	}
	defer rows.Close()
	var bindings []secretIntegrityStagingRow
	for rows.Next() {
		var item secretIntegrityStagingRow
		var historicalScope string
		if err := rows.Scan(&item.tableName, &item.id, &item.secretName, &item.secretKey, &historicalScope); err != nil {
			return fmt.Errorf("scan cluster-admin secret binding: %w", err)
		}
		bindings = append(bindings, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read cluster-admin secret bindings: %w", err)
	}
	if len(bindings) > 0 && runtimeRunner == nil {
		return fmt.Errorf("Kubernetes runtime is required to stage frozen secret integrity")
	}
	for _, binding := range bindings {
		integrity, err := runtimeRunner.InspectSecretIntegrity(ctx, runtimerepo.SecretIntegrityInput{
			SecretName: binding.secretName,
			SecretKey:  binding.secretKey,
		})
		if err != nil {
			return fmt.Errorf("inspect frozen secret binding %s/%d: %w", binding.tableName, binding.id, err)
		}
		statement := fmt.Sprintf(`update %s set secret_content_sha256 = $1, secret_resource_uid = $2, secret_resource_version = $3 where id = $4`, binding.tableName)
		if _, err := db.ExecContext(ctx, statement, integrity.ContentSHA256, integrity.UID, integrity.ResourceVersion, binding.id); err != nil {
			return fmt.Errorf("store frozen secret metadata for %s/%d: %w", binding.tableName, binding.id, err)
		}
	}
	return nil
}
