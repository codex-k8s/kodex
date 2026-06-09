package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	githubintegration "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/integration/github"
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

	statusSvc := statusservice.NewStatusService(statusservice.Config{
		ServiceName:          serviceName,
		ServiceVersion:       statusservice.Version("0.1.0"),
		MattermostConfigured: cfg.MattermostSiteURL != "",
		BotTokenConfigured:   cfg.BotTokenConfigured(),
		SlashTokenConfigured: cfg.SlashTokenConfigured(),
		DatabaseConfigured:   cfg.DatabaseConfigured(),
		StorageReady:         storage != nil,
		DefaultTeamName:      cfg.DefaultTeamName,
		DefaultChannels:      cfg.ChannelNames(),
	})
	var channelManager statusservice.MattermostChannelManager
	if cfg.BotTokenConfigured() && cfg.MattermostSiteURL != "" {
		channelManager = mattermostintegration.NewControlSurface(cfg.MattermostSiteURL, cfg.MattermostBotToken)
	}
	gitHubProvider, err := openGitHubProvider(cfg)
	if err != nil {
		return err
	}
	slashSvc := statusservice.NewSlashCommandService(statusservice.SlashCommandServiceConfig{
		StatusService:         statusSvc,
		Store:                 storage,
		ChannelManager:        channelManager,
		RepositoryProvider:    gitHubProvider,
		DefaultTeamName:       cfg.DefaultTeamName,
		BotTokenConfigured:    cfg.BotTokenConfigured(),
		SlashTokenConfigured:  cfg.SlashTokenConfigured(),
		GitHubTokenConfigured: cfg.GitHubTokenConfigured(),
		DatabaseConfigured:    cfg.DatabaseConfigured(),
		StorageReady:          storage != nil,
		MattermostConfigured:  cfg.MattermostSiteURL != "",
		ChannelManagerEnabled: channelManager != nil,
	})

	router := httptransport.NewRouter(httptransport.RouterConfig{
		StatusService:         statusSvc,
		SlashService:          slashSvc,
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

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func openGitHubProvider(cfg Config) (*githubintegration.Provider, error) {
	if !cfg.GitHubTokenConfigured() {
		return nil, nil
	}
	provider, err := githubintegration.NewProvider(cfg.GitHubToken)
	if err != nil {
		return nil, fmt.Errorf("open github provider: %w", err)
	}
	return provider, nil
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
	return adminpostgres.NewRepository(pool), closePool, nil
}
