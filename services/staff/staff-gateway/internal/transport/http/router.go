package httptransport

import (
	"context"
	"log/slog"
	"net/http"
)

type Router struct {
	handler http.Handler
	openAPI *OpenAPIContract
	client  InteractionHubClient
}

func NewRouter(ctx context.Context, cfg Config, interactionHub InteractionHubClient, logger *slog.Logger) (*Router, error) {
	if logger == nil {
		logger = slog.Default()
	}
	contract, err := LoadOpenAPIContract(ctx, cfg.OpenAPISpecPath)
	if err != nil {
		return nil, err
	}
	handlers := newHandlers(interactionHub, contract)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/owner-inbox/items", handlers.listOwnerInboxItems)
	mux.HandleFunc("GET /v1/owner-inbox/items/{request_id}", handlers.getOwnerInboxItem)
	mux.HandleFunc("POST /v1/owner-inbox/items/{request_id}/response", handlers.respondOwnerInboxItem)
	mux.HandleFunc("GET /openapi/staff-gateway.v1.yaml", handlers.openAPISpec)

	handler := RequestIDMiddleware(
		errorBoundary(logger,
			LoggingMiddleware(logger)(
				TimeoutMiddleware(cfg.RequestTimeout)(
					BodyLimitMiddleware(cfg.MaxBodyBytes)(mux),
				),
			),
		),
	)
	return &Router{handler: handler, openAPI: contract, client: interactionHub}, nil
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.handler.ServeHTTP(w, req)
}

func (r *Router) Ready() bool {
	return r != nil && r.openAPI.Ready() && r.client != nil
}
