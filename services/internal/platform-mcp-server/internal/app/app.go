package app

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	agentmanagerclient "github.com/codex-k8s/kodex/services/internal/platform-mcp-server/internal/clients/agentmanager"
	providerhubclient "github.com/codex-k8s/kodex/services/internal/platform-mcp-server/internal/clients/providerhub"
	mcptransport "github.com/codex-k8s/kodex/services/internal/platform-mcp-server/internal/transport/mcp"
)

const serviceName = "platform-mcp-server"

// Run starts platform-mcp-server process servers and shuts them down with context.
func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	ownerRoutes, err := cfg.OwnerRouteCatalog()
	if err != nil {
		return err
	}
	agentConn, err := agentmanagerclient.NewConnection(cfg.AgentManagerClientConfig())
	if err != nil {
		return err
	}
	defer func() {
		_ = agentConn.Close()
	}()
	providerConn, err := providerhubclient.NewConnection(cfg.ProviderHubClientConfig())
	if err != nil {
		return err
	}
	defer func() {
		_ = providerConn.Close()
	}()
	agentClient, err := agentmanagerclient.New(agentsv1.NewAgentManagerServiceClient(agentConn), cfg.AgentManagerClientConfig())
	if err != nil {
		return err
	}
	providerClient, err := providerhubclient.New(providersv1.NewProviderHubServiceClient(providerConn), cfg.ProviderHubClientConfig())
	if err != nil {
		return err
	}
	mcpServer, err := mcptransport.NewServer(cfg.MCPTransportConfig(ownerRoutes, agentClient, providerClient), logger)
	if err != nil {
		return err
	}
	mux := serviceprocess.NewHealthMux(readinessChecks(mcpServer, ownerRoutes.Ready()), 2*time.Second)
	mux.Handle(strings.TrimSpace(cfg.MCP.Path), mcpServer.HTTPHandler())
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go serviceprocess.StartHTTPServer(httpServer, serviceName, logger, errCh)

	select {
	case <-ctx.Done():
		return shutdownHTTP(ctx, httpServer, 10*time.Second)
	case err := <-errCh:
		_ = httpServer.Close()
		return err
	}
}

func readinessChecks(mcpServer *mcptransport.Server, ownerRoutesReady bool) []serviceprocess.ReadinessCheck {
	return []serviceprocess.ReadinessCheck{
		serviceprocess.StaticReadinessCheck("mcp registry", mcpServer != nil && mcpServer.Ready()),
		serviceprocess.StaticReadinessCheck("owner route catalog", ownerRoutesReady),
	}
}

func shutdownHTTP(ctx context.Context, httpServer *http.Server, timeout time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}
