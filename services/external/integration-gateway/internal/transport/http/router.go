package httptransport

import (
	"context"
	"log/slog"
	stdhttp "net/http"
	"os"
	"strings"

	"github.com/labstack/echo/v5"
)

// Router is the integration-gateway HTTP router and readiness surface.
type Router struct {
	handler       stdhttp.Handler
	openAPI       *OpenAPIValidator
	routeRegistry routeRegistry
}

// NewRouter creates the Echo router and wraps it with edge middleware.
func NewRouter(ctx context.Context, cfg Config, providerHub ProviderHubClient, logger *slog.Logger) (*Router, error) {
	return NewRouterWithVerifier(ctx, cfg, providerHub, rejectingProviderWebhookVerifier{}, logger)
}

// NewRouterWithVerifier creates the Echo router with an explicit provider webhook verifier.
func NewRouterWithVerifier(ctx context.Context, cfg Config, providerHub ProviderHubClient, verifier ProviderWebhookVerifier, logger *slog.Logger) (*Router, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if verifier == nil {
		verifier = rejectingProviderWebhookVerifier{}
	}
	validator, err := NewOpenAPIValidator(ctx, cfg.OpenAPISpecPath)
	if err != nil {
		return nil, err
	}
	registry := newRouteRegistry(cfg.ProviderWebhookEnabled, cfg.AllowedProviderSlugs)
	handlers := newHandlers(registry, providerHub, verifier, cfg.OpenAPISpecPath)
	e := echo.New()
	e.HTTPErrorHandler = ErrorHandler(logger)
	e.POST("/v1/provider-webhooks/:provider_slug", handlers.providerWebhook)
	e.POST("/v1/external-callbacks/:callback_source", handlers.externalCallback)
	e.GET("/openapi/integration-gateway.v1.yaml", handlers.openAPISpec)

	handler := RequestIDMiddleware(
		LoggingMiddleware(logger)(
			TimeoutMiddleware(cfg.RequestTimeout)(
				BodyCaptureMiddleware(cfg.MaxBodyBytes)(
					validator.Middleware(e),
				),
			),
		),
	)
	return &Router{handler: handler, openAPI: validator, routeRegistry: registry}, nil
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	r.handler.ServeHTTP(w, req)
}

// Ready reports whether runtime routing dependencies were composed.
func (r *Router) Ready() bool {
	return r != nil && r.openAPI != nil && r.routeRegistry.ready()
}

func readSpec(path string) ([]byte, error) {
	return os.ReadFile(strings.TrimSpace(path))
}
