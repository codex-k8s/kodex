package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	agentgrpc "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/transport/grpc"
)

const serviceName = "agent-manager"

// Run starts agent-manager process servers and shuts them down with context.
func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	agentService := agentservice.New(agentservice.Config{
		EventPublisher: agentservice.DisabledEventPublisher{},
	})
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           serviceprocess.NewHealthMux(readinessChecks(agentService), 2*time.Second),
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
	})
	if err != nil {
		return err
	}
	agentgrpc.RegisterAgentManagerService(grpcServer, agentService)

	errCh := make(chan error, 2)
	go serviceprocess.StartHTTPServer(httpServer, serviceName, logger, errCh)
	go serviceprocess.StartGRPCServer(grpcServer, serviceName, cfg.GRPCAddr, logger, errCh)

	select {
	case <-ctx.Done():
		return serviceprocess.ShutdownHTTPAndGRPC(ctx, httpServer, grpcServer, 10*time.Second)
	case err := <-errCh:
		grpcServer.Stop()
		_ = httpServer.Close()
		return err
	}
}

func readinessChecks(agentService *agentservice.Service) []serviceprocess.ReadinessCheck {
	return []serviceprocess.ReadinessCheck{
		serviceprocess.StaticReadinessCheck("agent service", agentService != nil && agentService.Ready()),
	}
}
