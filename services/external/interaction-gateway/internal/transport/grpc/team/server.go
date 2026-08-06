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
	listOperation      = "interaction.team.catalog.read"
	createOperation    = "interaction.team.create"
	readbackOperation  = "interaction.team.provider.readback"
	readinessOperation = "interaction.team.readiness"
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
	operation, err := server.service.Create(ctx, principal, displayName, slugIntent, idempotencyKey)
	if err != nil {
		return nil, transportError(err)
	}
	return casters.CreateResponse(operation), nil
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
	if _, err := teamprincipal.Principal(ctx,
		interactiongatewayv1.MattermostTeamService_CheckReadiness_FullMethodName,
		readinessOperation, readinessOperation); err != nil {
		return nil, status.Error(codes.PermissionDenied, "verified Mattermost team readiness context rejected")
	}
	if err := server.service.Check(ctx); err != nil {
		return nil, status.Error(codes.Unavailable, "Mattermost team working path is unavailable")
	}
	return &interactiongatewayv1.MattermostTeamServiceCheckReadinessResponse{
		Ready: true, SchemaVersion: 1, AuthorityReady: true, PostgresReady: true, MattermostReady: true,
	}, nil
}
