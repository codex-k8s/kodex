package httptransport

import (
	"context"
	"log/slog"
	"net/http"
)

type Router struct {
	handler http.Handler
	openAPI *OpenAPIContract
	clients routeClients
}

type routeClients struct {
	interactionHub InteractionHubClient
	agentManager   AgentManagerClient
	governance     GovernanceManagerClient
	projectCatalog ProjectCatalogClient
}

func NewRouter(ctx context.Context, cfg Config, interactionHub InteractionHubClient, agentManager AgentManagerClient, governance GovernanceManagerClient, projectCatalog ProjectCatalogClient, logger *slog.Logger) (*Router, error) {
	if logger == nil {
		logger = slog.Default()
	}
	contract, err := LoadOpenAPIContract(ctx, cfg.OpenAPISpecPath)
	if err != nil {
		return nil, err
	}
	clients := routeClients{interactionHub: interactionHub, agentManager: agentManager, governance: governance, projectCatalog: projectCatalog}
	handlers := newHandlers(clients, contract, cfg.SelfDeployProjectRef)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/owner-inbox/items", handlers.listOwnerInboxItems)
	mux.HandleFunc("GET /v1/owner-inbox/items/{request_id}", handlers.getOwnerInboxItem)
	mux.HandleFunc("POST /v1/owner-inbox/items/{request_id}/response", handlers.respondOwnerInboxItem)
	mux.HandleFunc("GET /v1/agent-sessions", handlers.listAgentSessions)
	mux.HandleFunc("GET /v1/agent-runs", handlers.listAgentRunSummaries)
	mux.HandleFunc("GET /v1/agent-runs/{run_id}/runtime-status", handlers.getAgentRunRuntimeStatus)
	mux.HandleFunc("GET /v1/agent-runs/{run_id}/activities", handlers.listAgentRunActivities)
	mux.HandleFunc("GET /v1/projects", handlers.listProjects)
	mux.HandleFunc("GET /v1/projects/{project_id}/repositories", handlers.listProjectRepositories)
	mux.HandleFunc("GET /v1/governance/summary", handlers.getGovernanceSummary)
	mux.HandleFunc("GET /v1/self-deploy/summary", handlers.getSelfDeploySummary)
	mux.HandleFunc("POST /v1/self-deploy/gates/{gate_request_id}/decision", handlers.submitSelfDeployGateDecision)
	mux.HandleFunc("GET /openapi/staff-gateway.v1.yaml", handlers.openAPISpec)

	handler := RequestIDMiddleware(
		errorBoundary(logger,
			LoggingMiddleware(logger)(
				ActorContextMiddleware(
					TimeoutMiddleware(cfg.RequestTimeout)(
						BodyLimitMiddleware(cfg.MaxBodyBytes)(
							contract.Middleware(mux),
						),
					),
				),
			),
		),
	)
	return &Router{handler: handler, openAPI: contract, clients: clients}, nil
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.handler.ServeHTTP(w, req)
}

func (r *Router) Ready() bool {
	return r != nil && r.openAPI.Ready() && r.clients.interactionHub != nil && r.clients.agentManager != nil && r.clients.governance != nil && r.clients.projectCatalog != nil
}
