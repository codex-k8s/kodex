// Package botidentity реализует generated gRPC Agent bot source adapter.
package botidentity

import (
	"context"
	"errors"

	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/authorization/teamprincipal"
	domainbot "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/service/botidentity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/transport/grpc/botidentity/casters"
)

const (
	listOperation      = "interaction.agent-bot.catalog.read"
	createOperation    = "interaction.agent-bot.create-and-bind"
	bindOperation      = "interaction.agent-bot.bind"
	getOperation       = "interaction.agent-bot.get"
	rebindOperation    = "interaction.agent-bot.rebind"
	revokeOperation    = "interaction.agent-bot.revoke"
	operationRead      = "interaction.agent-bot.operation.get"
	providerReadback   = "interaction.agent-bot.provider.readback"
	readinessOperation = "interaction.agent-bot.readiness"
)

type Server struct {
	interactiongatewayv1.UnimplementedAgentMattermostBotIdentityServiceServer
	service *domainbot.Service
}

func New(service *domainbot.Service) (*Server, error) {
	if service == nil {
		return nil, errors.New("agent Mattermost bot identity gRPC service is required")
	}
	return &Server{service: service}, nil
}

func (server *Server) ListAgentMattermostBotIdentities(ctx context.Context,
	request *interactiongatewayv1.ListAgentMattermostBotIdentitiesRequest,
) (*interactiongatewayv1.ListAgentMattermostBotIdentitiesResponse, error) {
	principal, err := teamprincipal.Principal(ctx,
		interactiongatewayv1.AgentMattermostBotIdentityService_ListAgentMattermostBotIdentities_FullMethodName,
		listOperation, listOperation)
	if err != nil {
		return nil, permissionError(ctx, "verified Agent bot catalog context rejected")
	}
	pageSize, cursor, err := casters.ListRequest(request)
	if err != nil {
		return nil, invalidRequest(ctx)
	}
	identities, next, err := server.service.List(ctx, principal, pageSize, cursor)
	if err != nil {
		return nil, transportError(ctx, err)
	}
	return casters.ListResponse(identities, next), nil
}

func (server *Server) CreateAndBindAgentMattermostBotIdentity(ctx context.Context,
	request *interactiongatewayv1.CreateAndBindAgentMattermostBotIdentityRequest,
) (*interactiongatewayv1.CreateAndBindAgentMattermostBotIdentityResponse, error) {
	principal, err := teamprincipal.Principal(ctx,
		interactiongatewayv1.AgentMattermostBotIdentityService_CreateAndBindAgentMattermostBotIdentity_FullMethodName,
		createOperation, createOperation)
	if err != nil {
		return nil, permissionError(ctx, "verified Agent bot create context rejected")
	}
	agentRef, version, username, displayName, key, err := casters.CreateRequest(request)
	if err != nil {
		return nil, invalidRequest(ctx)
	}
	operation, binding, err := server.service.CreateAndBind(ctx, principal, agentRef, version, username, displayName, key)
	if err != nil {
		return nil, transportError(ctx, err)
	}
	return &interactiongatewayv1.CreateAndBindAgentMattermostBotIdentityResponse{
		Operation: casters.OperationView(operation), Binding: casters.BindingView(binding),
	}, nil
}

func (server *Server) BindAgentMattermostBotIdentity(ctx context.Context,
	request *interactiongatewayv1.BindAgentMattermostBotIdentityRequest,
) (*interactiongatewayv1.BindAgentMattermostBotIdentityResponse, error) {
	principal, err := teamprincipal.Principal(ctx,
		interactiongatewayv1.AgentMattermostBotIdentityService_BindAgentMattermostBotIdentity_FullMethodName,
		bindOperation, bindOperation)
	if err != nil {
		return nil, permissionError(ctx, "verified Agent bot bind context rejected")
	}
	agentRef, version, selector, key, err := casters.BindRequest(request)
	if err != nil {
		return nil, invalidRequest(ctx)
	}
	operation, binding, err := server.service.Bind(ctx, principal, agentRef, version, selector, key)
	if err != nil {
		return nil, transportError(ctx, err)
	}
	return &interactiongatewayv1.BindAgentMattermostBotIdentityResponse{
		Operation: casters.OperationView(operation), Binding: casters.BindingView(binding),
	}, nil
}

func (server *Server) GetAgentMattermostBotIdentity(ctx context.Context,
	request *interactiongatewayv1.GetAgentMattermostBotIdentityRequest,
) (*interactiongatewayv1.GetAgentMattermostBotIdentityResponse, error) {
	principal, err := teamprincipal.Principal(ctx,
		interactiongatewayv1.AgentMattermostBotIdentityService_GetAgentMattermostBotIdentity_FullMethodName,
		getOperation, getOperation)
	if err != nil {
		return nil, permissionError(ctx, "verified Agent bot get context rejected")
	}
	agentRef, err := casters.AgentRequest(request.GetAgentRef())
	if err != nil {
		return nil, invalidRequest(ctx)
	}
	binding, err := server.service.Get(ctx, principal, agentRef)
	if err != nil {
		return nil, transportError(ctx, err)
	}
	return &interactiongatewayv1.GetAgentMattermostBotIdentityResponse{Binding: casters.BindingView(binding)}, nil
}

func (server *Server) RebindAgentMattermostBotIdentity(ctx context.Context,
	request *interactiongatewayv1.RebindAgentMattermostBotIdentityRequest,
) (*interactiongatewayv1.RebindAgentMattermostBotIdentityResponse, error) {
	principal, err := teamprincipal.Principal(ctx,
		interactiongatewayv1.AgentMattermostBotIdentityService_RebindAgentMattermostBotIdentity_FullMethodName,
		rebindOperation, rebindOperation)
	if err != nil {
		return nil, permissionError(ctx, "verified Agent bot rebind context rejected")
	}
	agentRef, version, generation, selector, key, err := casters.RebindRequest(request)
	if err != nil {
		return nil, invalidRequest(ctx)
	}
	operation, binding, err := server.service.Rebind(ctx, principal, agentRef, version, generation, selector, key)
	if err != nil {
		return nil, transportError(ctx, err)
	}
	return &interactiongatewayv1.RebindAgentMattermostBotIdentityResponse{
		Operation: casters.OperationView(operation), Binding: casters.BindingView(binding),
	}, nil
}

func (server *Server) RevokeAgentMattermostBotIdentity(ctx context.Context,
	request *interactiongatewayv1.RevokeAgentMattermostBotIdentityRequest,
) (*interactiongatewayv1.RevokeAgentMattermostBotIdentityResponse, error) {
	principal, err := teamprincipal.Principal(ctx,
		interactiongatewayv1.AgentMattermostBotIdentityService_RevokeAgentMattermostBotIdentity_FullMethodName,
		revokeOperation, revokeOperation)
	if err != nil {
		return nil, permissionError(ctx, "verified Agent bot revoke context rejected")
	}
	agentRef, version, generation, key, err := casters.RevokeRequest(request)
	if err != nil {
		return nil, invalidRequest(ctx)
	}
	operation, binding, err := server.service.Revoke(ctx, principal, agentRef, version, generation, key)
	if err != nil {
		return nil, transportError(ctx, err)
	}
	return &interactiongatewayv1.RevokeAgentMattermostBotIdentityResponse{
		Operation: casters.OperationView(operation), Binding: casters.BindingView(binding),
	}, nil
}

func (server *Server) GetAgentMattermostBotIdentityOperation(ctx context.Context,
	request *interactiongatewayv1.GetAgentMattermostBotIdentityOperationRequest,
) (*interactiongatewayv1.GetAgentMattermostBotIdentityOperationResponse, error) {
	principal, err := teamprincipal.Principal(ctx,
		interactiongatewayv1.AgentMattermostBotIdentityService_GetAgentMattermostBotIdentityOperation_FullMethodName,
		operationRead, operationRead)
	if err != nil {
		return nil, permissionError(ctx, "verified Agent bot operation context rejected")
	}
	agentRef, action, key, err := casters.OperationRequest(request)
	if err != nil {
		return nil, invalidRequest(ctx)
	}
	operation, err := server.service.GetOperation(ctx, principal, agentRef, action, key)
	if err != nil {
		return nil, transportError(ctx, err)
	}
	return &interactiongatewayv1.GetAgentMattermostBotIdentityOperationResponse{
		Operation: casters.OperationView(operation),
	}, nil
}

func (server *Server) GetAgentMattermostBotIdentityProviderReadback(ctx context.Context,
	request *interactiongatewayv1.GetAgentMattermostBotIdentityProviderReadbackRequest,
) (*interactiongatewayv1.GetAgentMattermostBotIdentityProviderReadbackResponse, error) {
	principal, err := teamprincipal.Principal(ctx,
		interactiongatewayv1.AgentMattermostBotIdentityService_GetAgentMattermostBotIdentityProviderReadback_FullMethodName,
		providerReadback, providerReadback)
	if err != nil {
		return nil, permissionError(ctx, "verified Agent bot provider readback context rejected")
	}
	agentRef, selector, err := casters.ReadbackRequest(request)
	if err != nil {
		return nil, invalidRequest(ctx)
	}
	identity, err := server.service.ReadProvider(ctx, principal, agentRef, selector)
	if err != nil {
		return nil, transportError(ctx, err)
	}
	return &interactiongatewayv1.GetAgentMattermostBotIdentityProviderReadbackResponse{
		Identity: casters.IdentityView(identity),
	}, nil
}

func (server *Server) CheckAgentMattermostBotIdentityReadiness(ctx context.Context,
	request *interactiongatewayv1.CheckAgentMattermostBotIdentityReadinessRequest,
) (*interactiongatewayv1.CheckAgentMattermostBotIdentityReadinessResponse, error) {
	principal, err := teamprincipal.Principal(ctx,
		interactiongatewayv1.AgentMattermostBotIdentityService_CheckAgentMattermostBotIdentityReadiness_FullMethodName,
		readinessOperation, readinessOperation)
	if err != nil {
		return nil, permissionError(ctx, "verified Agent bot readiness context rejected")
	}
	agentRef := ""
	if request != nil {
		agentRef = request.GetAgentRef()
	}
	if _, castErr := casters.AgentRequest(agentRef); castErr != nil {
		return nil, invalidRequest(ctx)
	}
	readiness := server.service.CheckAgent(ctx, principal, agentRef)
	return &interactiongatewayv1.CheckAgentMattermostBotIdentityReadinessResponse{
		Ready: readiness.Ready, SchemaVersion: 1, AuthorityReady: true,
		PostgresReady: readiness.PostgresReady, MattermostReady: readiness.MattermostReady,
		ControlPlaneReady:       readiness.ControlPlaneReady,
		IdentityGenerationReady: readiness.IdentityGenerationReady,
		FailureCode:             readiness.FailureCode,
	}, nil
}
