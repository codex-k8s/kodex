// Package team реализует generated gRPC adapter Team provider lifecycle.
package team

import (
	"context"
	"errors"

	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/authorization/teamprincipal"
	domainteam "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/service/team"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/transport/grpc/team/casters"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	listOperation       = "interaction.team.catalog.read"
	createOperation     = "interaction.team.create"
	linkOperation       = "interaction.team.link"
	getBindingOperation = "interaction.team.binding.get"
	relinkOperation     = "interaction.team.relink"
	unlinkOperation     = "interaction.team.unlink"
	getOperation        = "interaction.team.mapping-operation.get"
	readbackOperation   = "interaction.team.provider.readback"
	readinessOperation  = "interaction.team.readiness"
)

type Server struct {
	interactiongatewayv1.UnimplementedMattermostTeamServiceServer
	service *domainteam.Service
}

func New(service *domainteam.Service) (*Server, error) {
	if service == nil {
		return nil, errors.New("mattermost team gRPC service is required")
	}
	return &Server{service: service}, nil
}

func (server *Server) ListMattermostTeams(ctx context.Context,
	request *interactiongatewayv1.ListMattermostTeamsRequest,
) (*interactiongatewayv1.ListMattermostTeamsResponse, error) {
	principal, err := teamprincipal.Principal(ctx,
		interactiongatewayv1.MattermostTeamService_ListMattermostTeams_FullMethodName,
		listOperation, listOperation)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, "verified Mattermost team catalog context rejected")
	}
	pageSize, cursor, err := casters.ListRequest(request)
	if err != nil {
		return nil, invalidRequest()
	}
	teams, nextCursor, err := server.service.List(ctx, principal, pageSize, cursor)
	if err != nil {
		return nil, transportError(err)
	}
	return casters.ListResponse(teams, nextCursor), nil
}

func (server *Server) CreateMattermostTeam(ctx context.Context,
	request *interactiongatewayv1.CreateMattermostTeamRequest,
) (*interactiongatewayv1.CreateMattermostTeamResponse, error) {
	principal, err := teamprincipal.Principal(ctx,
		interactiongatewayv1.MattermostTeamService_CreateMattermostTeam_FullMethodName,
		createOperation, createOperation)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, "verified Mattermost team create context rejected")
	}
	displayName, slugIntent, idempotencyKey, err := casters.CreateRequest(request)
	if err != nil {
		return nil, invalidRequest()
	}
	operation, binding, err := server.service.CreateAndBind(ctx, principal, displayName, slugIntent, idempotencyKey)
	if err != nil {
		return nil, transportError(err)
	}
	return casters.CreateResponse(operation, binding), nil
}

func (server *Server) LinkMattermostTeam(ctx context.Context,
	request *interactiongatewayv1.LinkMattermostTeamRequest,
) (*interactiongatewayv1.LinkMattermostTeamResponse, error) {
	principal, err := teamprincipal.Principal(ctx,
		interactiongatewayv1.MattermostTeamService_LinkMattermostTeam_FullMethodName, linkOperation, linkOperation)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, "verified Mattermost team link context rejected")
	}
	selector, idempotencyKey, err := casters.LinkRequest(request)
	if err != nil {
		return nil, invalidRequest()
	}
	binding, err := server.service.Link(ctx, principal, selector, idempotencyKey)
	if err != nil {
		return nil, transportError(err)
	}
	return &interactiongatewayv1.LinkMattermostTeamResponse{Binding: casters.BindingView(binding),
		Operation: casters.MappingOperationView(binding.Operation)}, nil
}

func (server *Server) GetMattermostTeamBinding(ctx context.Context,
	_ *interactiongatewayv1.GetMattermostTeamBindingRequest,
) (*interactiongatewayv1.GetMattermostTeamBindingResponse, error) {
	principal, err := teamprincipal.Principal(ctx,
		interactiongatewayv1.MattermostTeamService_GetMattermostTeamBinding_FullMethodName,
		getBindingOperation, getBindingOperation)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, "verified Mattermost team binding context rejected")
	}
	binding, err := server.service.GetBinding(ctx, principal)
	if err != nil {
		return nil, transportError(err)
	}
	return &interactiongatewayv1.GetMattermostTeamBindingResponse{Binding: casters.BindingView(binding)}, nil
}

func (server *Server) RelinkMattermostTeam(ctx context.Context,
	request *interactiongatewayv1.RelinkMattermostTeamRequest,
) (*interactiongatewayv1.RelinkMattermostTeamResponse, error) {
	principal, err := teamprincipal.Principal(ctx,
		interactiongatewayv1.MattermostTeamService_RelinkMattermostTeam_FullMethodName,
		relinkOperation, relinkOperation)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, "verified Mattermost team relink context rejected")
	}
	selector, version, generation, idempotencyKey, err := casters.RelinkRequest(request)
	if err != nil {
		return nil, invalidRequest()
	}
	binding, err := server.service.Relink(ctx, principal, selector, version, generation, idempotencyKey)
	if err != nil {
		return nil, transportError(err)
	}
	return &interactiongatewayv1.RelinkMattermostTeamResponse{Binding: casters.BindingView(binding),
		Operation: casters.MappingOperationView(binding.Operation)}, nil
}

func (server *Server) UnlinkMattermostTeam(ctx context.Context,
	request *interactiongatewayv1.UnlinkMattermostTeamRequest,
) (*interactiongatewayv1.UnlinkMattermostTeamResponse, error) {
	principal, err := teamprincipal.Principal(ctx,
		interactiongatewayv1.MattermostTeamService_UnlinkMattermostTeam_FullMethodName,
		unlinkOperation, unlinkOperation)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, "verified Mattermost team unlink context rejected")
	}
	version, generation, idempotencyKey, err := casters.UnlinkRequest(request)
	if err != nil {
		return nil, invalidRequest()
	}
	binding, err := server.service.Unlink(ctx, principal, version, generation, idempotencyKey)
	if err != nil {
		return nil, transportError(err)
	}
	return &interactiongatewayv1.UnlinkMattermostTeamResponse{Binding: casters.BindingView(binding),
		Operation: casters.MappingOperationView(binding.Operation)}, nil
}

func (server *Server) GetMattermostTeamMappingOperation(ctx context.Context,
	request *interactiongatewayv1.GetMattermostTeamMappingOperationRequest,
) (*interactiongatewayv1.GetMattermostTeamMappingOperationResponse, error) {
	principal, err := teamprincipal.Principal(ctx,
		interactiongatewayv1.MattermostTeamService_GetMattermostTeamMappingOperation_FullMethodName,
		getOperation, getOperation)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, "verified Mattermost mapping operation context rejected")
	}
	action, idempotencyKey, err := casters.MappingOperationRequest(request)
	if err != nil {
		return nil, invalidRequest()
	}
	operation, err := server.service.GetMappingOperation(ctx, principal, action, idempotencyKey)
	if err != nil {
		return nil, transportError(err)
	}
	return casters.MappingOperationResponse(operation), nil
}

func (server *Server) GetMattermostTeamProviderReadback(ctx context.Context,
	request *interactiongatewayv1.GetMattermostTeamProviderReadbackRequest,
) (*interactiongatewayv1.GetMattermostTeamProviderReadbackResponse, error) {
	principal, err := teamprincipal.Principal(ctx,
		interactiongatewayv1.MattermostTeamService_GetMattermostTeamProviderReadback_FullMethodName,
		readbackOperation, readbackOperation)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, "verified Mattermost team readback context rejected")
	}
	selector, err := casters.ProviderReadbackRequest(request)
	if err != nil {
		return nil, invalidRequest()
	}
	team, err := server.service.ReadProvider(ctx, principal, selector)
	if err != nil {
		return nil, transportError(err)
	}
	return casters.ProviderReadbackResponse(team), nil
}

func (server *Server) CheckReadiness(ctx context.Context,
	_ *interactiongatewayv1.MattermostTeamServiceCheckReadinessRequest,
) (*interactiongatewayv1.MattermostTeamServiceCheckReadinessResponse, error) {
	if _, err := teamprincipal.ReadinessPrincipal(ctx,
		interactiongatewayv1.MattermostTeamService_CheckReadiness_FullMethodName,
		readinessOperation, readinessOperation); err != nil {
		return nil, status.Error(codes.PermissionDenied, "verified Mattermost team readiness context rejected")
	}
	if err := server.service.Check(ctx); err != nil {
		return nil, status.Error(codes.Unavailable, "Mattermost team working path is unavailable")
	}
	return &interactiongatewayv1.MattermostTeamServiceCheckReadinessResponse{
		Ready: true, SchemaVersion: 2, AuthorityReady: true, PostgresReady: true, MattermostReady: true,
		ControlPlaneReady: true, MappingReady: true,
	}, nil
}
