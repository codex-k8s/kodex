package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	agentmanagerclient "github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/clients/agentmanager"
	governanceclient "github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/clients/governance"
	interactionhubclient "github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/clients/interactionhub"
	httptransport "github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/transport/http"
	"google.golang.org/grpc"
)

const serviceName = "staff-gateway"

func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	interactionClient, closeInteractionClient, err := buildDownstreamClient(cfg.InteractionHubClientConfig(), interactionhubclient.NewConnection, func(conn *grpc.ClientConn, cfg interactionhubclient.Config) (httptransport.InteractionHubClient, error) {
		return interactionhubclient.New(interactionsv1.NewInteractionHubServiceClient(conn), cfg)
	})
	if err != nil {
		return err
	}
	defer closeInteractionClient()
	agentClient, closeAgentClient, err := buildDownstreamClient(cfg.AgentManagerClientConfig(), agentmanagerclient.NewConnection, func(conn *grpc.ClientConn, cfg agentmanagerclient.Config) (httptransport.AgentManagerClient, error) {
		return agentmanagerclient.New(agentsv1.NewAgentManagerServiceClient(conn), cfg)
	})
	if err != nil {
		return err
	}
	defer closeAgentClient()
	governanceClient, closeGovernanceClient, err := buildDownstreamClient(cfg.GovernanceClientConfig(), governanceclient.NewConnection, func(conn *grpc.ClientConn, cfg governanceclient.Config) (httptransport.GovernanceManagerClient, error) {
		return governanceclient.New(governancev1.NewGovernanceManagerServiceClient(conn), cfg)
	})
	if err != nil {
		return err
	}
	defer closeGovernanceClient()

	apiHandler, err := httptransport.NewRouter(ctx, cfg.HTTPRouterConfig(), interactionClient, agentClient, governanceClient, logger)
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

func buildDownstreamClient[C any, T any](
	cfg C,
	connect func(C) (*grpc.ClientConn, error),
	build func(*grpc.ClientConn, C) (T, error),
) (T, func(), error) {
	var zero T
	conn, err := connect(cfg)
	if err != nil {
		return zero, nil, err
	}
	closeFn := func() {
		_ = conn.Close()
	}
	client, err := build(conn, cfg)
	if err != nil {
		closeFn()
		return zero, nil, err
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
