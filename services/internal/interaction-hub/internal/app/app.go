package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	interactionservice "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/service"
	interactionstub "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/repository/stub/interaction"
	interactiongrpc "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/transport/grpc"
)

const serviceName = "interaction-hub"

// Run starts interaction-hub process servers and shuts them down with context.
func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	interactionRepository := interactionstub.NewRepository()
	interactionService := interactionservice.New(interactionRepository)
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           serviceprocess.NewHealthMux(readinessChecks(interactionService), 2*time.Second),
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcMetrics, err := grpcserver.NewMetrics(nil, grpcserver.MetricsConfig{
		Subsystem:   "interaction_hub_grpc",
		ServiceName: serviceName,
	})
	if err != nil {
		return err
	}
	grpcServer, err := grpcserver.NewServer(cfg.GRPCServerConfig(), grpcserver.Dependencies{
		Logger:        logger,
		Metrics:       grpcMetrics,
		Authenticator: grpcserver.NewSharedTokenAuthenticator(cfg.GRPC.AuthToken),
		UnaryInterceptors: []grpcserver.UnaryInterceptor{
			interactiongrpc.UnaryErrorInterceptor(logger),
		},
	})
	if err != nil {
		return err
	}
	interactiongrpc.RegisterInteractionHubService(grpcServer, interactionService)

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

func readinessChecks(interactionService *interactionservice.Service) []serviceprocess.ReadinessCheck {
	return []serviceprocess.ReadinessCheck{
		serviceprocess.StaticReadinessCheck("interaction service", interactionService != nil && interactionService.Ready()),
	}
}
