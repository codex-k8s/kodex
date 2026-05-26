package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	providerhubclient "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/clients/providerhub"
	httptransport "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/transport/http"
)

const serviceName = "integration-gateway"

// Run starts integration-gateway and shuts it down with context.
func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	providerClient, closeProviderClient, err := buildProviderHubClient(cfg)
	if err != nil {
		return err
	}
	defer closeProviderClient()

	apiHandler, err := httptransport.NewRouter(ctx, cfg.HTTPRouterConfig(), providerClient, logger)
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           newHTTPHandler(apiHandler, cfg.HTTP.ReadinessTimeout),
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
	}

	errCh := make(chan error, 1)
	go serviceprocess.StartHTTPServer(httpServer, serviceName, logger, errCh)

	select {
	case <-ctx.Done():
		return shutdownHTTP(ctx, httpServer, cfg.HTTP.ShutdownTimeout)
	case err := <-errCh:
		_ = httpServer.Close()
		return err
	}
}

func buildProviderHubClient(cfg Config) (httptransport.ProviderHubClient, func(), error) {
	if !cfg.ProviderWebhook.Enabled {
		return providerhubclient.Disabled{}, func() {}, nil
	}
	conn, err := providerhubclient.NewConnection(cfg.ProviderHubClientConfig())
	if err != nil {
		return nil, nil, err
	}
	closeFn := func() {
		_ = conn.Close()
	}
	client, err := providerhubclient.New(providersv1.NewProviderHubServiceClient(conn), cfg.ProviderHubClientConfig())
	if err != nil {
		closeFn()
		return nil, nil, err
	}
	return client, closeFn, nil
}

func readinessChecks(apiHandler *httptransport.Router) []serviceprocess.ReadinessCheck {
	return []serviceprocess.ReadinessCheck{
		serviceprocess.StaticReadinessCheck("http router", apiHandler != nil && apiHandler.Ready()),
	}
}

func newHTTPHandler(apiHandler *httptransport.Router, readinessTimeout time.Duration) http.Handler {
	healthMux := serviceprocess.NewHealthMux(readinessChecks(apiHandler), readinessTimeout)
	rootMux := http.NewServeMux()
	rootMux.Handle("/health/", healthMux)
	rootMux.Handle("/metrics", healthMux)
	if apiHandler != nil {
		rootMux.Handle("/", apiHandler)
	}
	return rootMux
}

func shutdownHTTP(ctx context.Context, httpServer *http.Server, timeout time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}
