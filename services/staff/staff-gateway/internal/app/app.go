package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	interactionhubclient "github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/clients/interactionhub"
	httptransport "github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/transport/http"
)

const serviceName = "staff-gateway"

func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	interactionClient, closeInteractionClient, err := buildInteractionHubClient(cfg)
	if err != nil {
		return err
	}
	defer closeInteractionClient()

	apiHandler, err := httptransport.NewRouter(ctx, cfg.HTTPRouterConfig(), interactionClient, logger)
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

func buildInteractionHubClient(cfg Config) (httptransport.InteractionHubClient, func(), error) {
	clientCfg := cfg.InteractionHubClientConfig()
	conn, err := interactionhubclient.NewConnection(clientCfg)
	if err != nil {
		return nil, nil, err
	}
	closeFn := func() {
		_ = conn.Close()
	}
	client, err := interactionhubclient.New(interactionsv1.NewInteractionHubServiceClient(conn), clientCfg)
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
	rootMux := http.NewServeMux()
	attachHealthRoutes(rootMux, serviceprocess.NewHealthMux(readinessChecks(apiHandler), readinessTimeout))
	if apiHandler != nil {
		rootMux.Handle("/", apiHandler)
	}
	return rootMux
}

func attachHealthRoutes(rootMux *http.ServeMux, healthHandler http.Handler) {
	for _, route := range []string{"/health/", "/metrics"} {
		rootMux.Handle(route, healthHandler)
	}
}

func shutdownHTTP(ctx context.Context, httpServer *http.Server, timeout time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}
