package app

import (
	"context"
	"log/slog"
	"net/http"

	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	httptransport "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/transport/http"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

const serviceName = "matter-codex-bot-service"

func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	statusSvc := statusservice.NewStatusService(statusservice.Config{
		ServiceName:          serviceName,
		ServiceVersion:       statusservice.Version("0.1.0"),
		MattermostConfigured: cfg.MattermostSiteURL != "",
		BotTokenConfigured:   cfg.BotTokenConfigured(),
		SlashTokenConfigured: cfg.SlashTokenConfigured(),
		DefaultTeamName:      cfg.DefaultTeamName,
		DefaultChannels:      cfg.ChannelNames(),
	})

	router := httptransport.NewRouter(httptransport.RouterConfig{
		StatusService:      statusSvc,
		SlashToken:         cfg.MattermostSlashToken,
		MaxSlashFormBytes:  cfg.MaxSlashFormBytes,
		PrometheusRegistry: newPrometheusRegistry(),
		Logger:             logger,
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

func newPrometheusRegistry() *prometheus.Registry {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return registry
}
