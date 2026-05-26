package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	hookservice "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/service"
	hookstub "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/repository/stub/hook"
	opsstub "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/repository/stub/ops"
	commandtransport "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/transport/command"
)

const serviceName = "codex-hook-ingress"

// Run starts codex-hook-ingress process servers and shuts them down with context.
func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	events, err := cfg.SupportedEvents()
	if err != nil {
		return err
	}
	disabledRoutes, err := cfg.DisabledRouteOwners()
	if err != nil {
		return err
	}
	preToolUseRiskClasses, err := cfg.PreToolUseRiskClasses()
	if err != nil {
		return err
	}
	repository := hookstub.NewRepository()
	domainService := hookservice.New(
		repository,
		hookservice.Config{
			SchemaVersion:                   cfg.SchemaVersion,
			MaxEnvelopeBytes:                cfg.MaxEnvelopeBytes,
			MaxTextPreviewBytes:             cfg.MaxTextPreviewBytes,
			MaxBoundedErrorBytes:            cfg.MaxBoundedErrorBytes,
			SupportedEvents:                 events,
			DisabledRoutes:                  disabledRoutes,
			RouteFailurePolicy:              cfg.RouteFailureMode(),
			OpsFeedRetention:                cfg.OpsFeedRetention,
			RateLimitWindow:                 cfg.RateLimitWindow,
			RateLimitBurst:                  cfg.RateLimitBurst,
			DecisionBridgeTimeout:           cfg.DecisionBridgeTimeout,
			PreToolUseDecisionRiskClasses:   preToolUseRiskClasses,
			PermissionDecisionFailurePolicy: cfg.PermissionDecisionFailureMode(),
			PreToolUseDecisionFailurePolicy: cfg.PreToolUseDecisionFailureMode(),
		},
		hookservice.Dependencies{
			Clock:          systemClock{},
			Validator:      hookservice.DefaultEnvelopeValidator{},
			SourceVerifier: hookservice.StaticSourceVerifier{},
			Sanitizer:      hookservice.DefaultSanitizer{},
			RouteRegistry:  hookservice.NewDefaultRouteRegistry(),
			DecisionBridge: hookservice.NewOwnerDecisionBridge(hookservice.NewDefaultDecisionOwnerPorts()),
			OpsFeed: opsstub.NewRepository(opsstub.Config{
				Capacity:  cfg.OpsFeedCapacity,
				Retention: cfg.OpsFeedRetention,
			}),
			RateLimiter: hookservice.NewFixedWindowRateLimiter(hookservice.RateLimitConfig{
				Window: cfg.RateLimitWindow,
				Burst:  cfg.RateLimitBurst,
			}),
		},
	)
	logicalHandler := commandtransport.NewHandler(domainService)
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           serviceprocess.NewHealthMux(readinessChecks(domainService, logicalHandler), cfg.ReadinessTimeout),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go serviceprocess.StartHTTPServer(httpServer, serviceName, logger, errCh)

	select {
	case <-ctx.Done():
		return shutdownHTTP(ctx, httpServer, cfg.ShutdownTimeout)
	case err := <-errCh:
		_ = httpServer.Close()
		return err
	}
}

func readinessChecks(domainService *hookservice.Service, logicalHandler *commandtransport.Handler) []serviceprocess.ReadinessCheck {
	return []serviceprocess.ReadinessCheck{
		serviceprocess.StaticReadinessCheck("hook domain service", domainService != nil && domainService.Ready()),
		serviceprocess.StaticReadinessCheck("logical SubmitHookEvent handler", logicalHandler != nil && logicalHandler.Ready()),
	}
}

func shutdownHTTP(ctx context.Context, httpServer *http.Server, timeout time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}
