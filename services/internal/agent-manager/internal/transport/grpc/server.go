package grpc

import (
	"context"

	"github.com/google/uuid"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
	grpccasters "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/transport/grpc/casters"
	grpcruntime "google.golang.org/grpc"
)

var _ agentsv1.AgentManagerServiceServer = (*Server)(nil)

// Server exposes AgentManagerService over gRPC.
type Server struct {
	agentsv1.UnimplementedAgentManagerServiceServer

	service agentService
}

type agentService interface {
	CreateFlow(context.Context, agentservice.CreateFlowInput) (entity.Flow, error)
	UpdateFlow(context.Context, agentservice.UpdateFlowInput) (entity.Flow, error)
	GetFlow(context.Context, uuid.UUID) (entity.Flow, error)
	ListFlows(context.Context, agentservice.FlowList) ([]entity.Flow, value.PageResult, error)
	CreateFlowVersion(context.Context, agentservice.CreateFlowVersionInput) (entity.FlowVersion, error)
	ActivateFlowVersion(context.Context, agentservice.ActivateFlowVersionInput) (entity.FlowVersion, error)
	GetFlowVersion(context.Context, uuid.UUID) (entity.FlowVersion, error)
	CreateRoleProfile(context.Context, agentservice.CreateRoleProfileInput) (entity.RoleProfile, error)
	UpdateRoleProfile(context.Context, agentservice.UpdateRoleProfileInput) (entity.RoleProfile, error)
	GetRoleProfile(context.Context, uuid.UUID) (entity.RoleProfile, error)
	ListRoleProfiles(context.Context, agentservice.RoleProfileList) ([]entity.RoleProfile, value.PageResult, error)
	GetPromptTemplate(context.Context, uuid.UUID) (entity.PromptTemplate, error)
	ListPromptTemplates(context.Context, agentservice.PromptTemplateList) ([]entity.PromptTemplate, value.PageResult, error)
	CreatePromptTemplateVersion(context.Context, agentservice.CreatePromptTemplateVersionInput) (entity.PromptTemplateVersion, error)
	ActivatePromptTemplateVersion(context.Context, agentservice.ActivatePromptTemplateVersionInput) (entity.PromptTemplateVersion, error)
	GetPromptTemplateVersion(context.Context, uuid.UUID) (entity.PromptTemplateVersion, error)
	ListPromptTemplateVersions(context.Context, agentservice.PromptTemplateVersionList) ([]entity.PromptTemplateVersion, value.PageResult, error)
	StartAgentSession(context.Context, agentservice.StartAgentSessionInput) (entity.AgentSession, error)
	GetAgentSession(context.Context, uuid.UUID) (entity.AgentSession, error)
	StartAgentRun(context.Context, agentservice.StartAgentRunInput) (entity.AgentRun, error)
	RecordRunState(context.Context, agentservice.RecordRunStateInput) (entity.AgentRun, error)
	RecordSessionStateSnapshot(context.Context, agentservice.RecordSessionStateSnapshotInput) (agentservice.SessionSnapshotResult, error)
	ListAgentRuns(context.Context, agentservice.AgentRunList) ([]entity.AgentRun, value.PageResult, error)
	GetSessionStateSnapshot(context.Context, uuid.UUID) (entity.AgentSessionStateSnapshot, error)
	RequestAcceptance(context.Context, agentservice.RequestAcceptanceInput) (entity.AcceptanceResult, error)
	RecordAcceptanceResult(context.Context, agentservice.RecordAcceptanceResultInput) (entity.AcceptanceResult, error)
	GetAcceptanceResult(context.Context, uuid.UUID) (entity.AcceptanceResult, error)
	ListAcceptanceResults(context.Context, agentservice.AcceptanceResultList) ([]entity.AcceptanceResult, value.PageResult, error)
}

// NewServer creates an agent-manager gRPC server boundary.
func NewServer(service agentService) *Server {
	if service == nil {
		panic("agent-manager domain service is required")
	}
	return &Server{service: service}
}

// RegisterAgentManagerService registers agent-manager handlers in a gRPC runtime.
func RegisterAgentManagerService(registrar grpcruntime.ServiceRegistrar, service agentService) {
	agentsv1.RegisterAgentManagerServiceServer(registrar, NewServer(service))
}

// CreateFlow creates a flow in a platform, organization, project or repository scope.
func (server *Server) CreateFlow(ctx context.Context, request *agentsv1.CreateFlowRequest) (*agentsv1.FlowResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.CreateFlowInput, server.service.CreateFlow, grpccasters.FlowEntityResponse)
}

// UpdateFlow changes mutable flow metadata and safe lifecycle status.
func (server *Server) UpdateFlow(ctx context.Context, request *agentsv1.UpdateFlowRequest) (*agentsv1.FlowResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.UpdateFlowInput, server.service.UpdateFlow, grpccasters.FlowEntityResponse)
}

// CreateFlowVersion creates an immutable flow definition snapshot.
func (server *Server) CreateFlowVersion(ctx context.Context, request *agentsv1.CreateFlowVersionRequest) (*agentsv1.FlowVersionResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.CreateFlowVersionInput, server.service.CreateFlowVersion, grpccasters.FlowVersionResponse)
}

// ActivateFlowVersion activates a flow version for future sessions.
func (server *Server) ActivateFlowVersion(ctx context.Context, request *agentsv1.ActivateFlowVersionRequest) (*agentsv1.FlowVersionResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ActivateFlowVersionInput, server.service.ActivateFlowVersion, grpccasters.FlowVersionResponse)
}

// GetFlow returns flow metadata and active version when one exists.
func (server *Server) GetFlow(ctx context.Context, request *agentsv1.GetFlowRequest) (*agentsv1.FlowResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetFlowInput, server.getFlow, grpccasters.FlowResponse)
}

// ListFlows returns flows by scope and optional status.
func (server *Server) ListFlows(ctx context.Context, request *agentsv1.ListFlowsRequest) (*agentsv1.ListFlowsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListFlowsInput, server.listFlows, grpccasters.ListFlowsResponse)
}

// CreateRoleProfile creates an agent role profile.
func (server *Server) CreateRoleProfile(ctx context.Context, request *agentsv1.CreateRoleProfileRequest) (*agentsv1.RoleProfileResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.CreateRoleProfileInput, server.service.CreateRoleProfile, grpccasters.RoleProfileResponse)
}

// UpdateRoleProfile changes mutable role metadata and safe lifecycle status.
func (server *Server) UpdateRoleProfile(ctx context.Context, request *agentsv1.UpdateRoleProfileRequest) (*agentsv1.RoleProfileResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.UpdateRoleProfileInput, server.service.UpdateRoleProfile, grpccasters.RoleProfileResponse)
}

// GetRoleProfile returns authoritative role state.
func (server *Server) GetRoleProfile(ctx context.Context, request *agentsv1.GetRoleProfileRequest) (*agentsv1.RoleProfileResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetRoleProfileInput, server.getRoleProfile, grpccasters.RoleProfileResponse)
}

// ListRoleProfiles returns role profiles by scope, kind and status.
func (server *Server) ListRoleProfiles(ctx context.Context, request *agentsv1.ListRoleProfilesRequest) (*agentsv1.ListRoleProfilesResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListRoleProfilesInput, server.listRoleProfiles, grpccasters.ListRoleProfilesResponse)
}

// GetPromptTemplate returns prompt template metadata and active version when one exists.
func (server *Server) GetPromptTemplate(ctx context.Context, request *agentsv1.GetPromptTemplateRequest) (*agentsv1.PromptTemplateResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetPromptTemplateInput, server.getPromptTemplate, grpccasters.PromptTemplateResponse)
}

// ListPromptTemplates returns prompt templates by role and optional kind.
func (server *Server) ListPromptTemplates(ctx context.Context, request *agentsv1.ListPromptTemplatesRequest) (*agentsv1.ListPromptTemplatesResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListPromptTemplatesInput, server.listPromptTemplates, grpccasters.ListPromptTemplatesResponse)
}

// CreatePromptTemplateVersion creates an immutable prompt template version.
func (server *Server) CreatePromptTemplateVersion(ctx context.Context, request *agentsv1.CreatePromptTemplateVersionRequest) (*agentsv1.PromptTemplateVersionResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.CreatePromptTemplateVersionInput, server.service.CreatePromptTemplateVersion, grpccasters.PromptTemplateVersionResponse)
}

// ActivatePromptTemplateVersion activates a prompt template version for future runs.
func (server *Server) ActivatePromptTemplateVersion(ctx context.Context, request *agentsv1.ActivatePromptTemplateVersionRequest) (*agentsv1.PromptTemplateVersionResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ActivatePromptTemplateVersionInput, server.service.ActivatePromptTemplateVersion, grpccasters.PromptTemplateVersionResponse)
}

// GetPromptTemplateVersion returns one immutable prompt template version.
func (server *Server) GetPromptTemplateVersion(ctx context.Context, request *agentsv1.GetPromptTemplateVersionRequest) (*agentsv1.PromptTemplateVersionResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetPromptTemplateVersionInput, server.getPromptTemplateVersion, grpccasters.PromptTemplateVersionResponse)
}

// ListPromptTemplateVersions returns prompt versions by role, kind and status.
func (server *Server) ListPromptTemplateVersions(ctx context.Context, request *agentsv1.ListPromptTemplateVersionsRequest) (*agentsv1.ListPromptTemplateVersionsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListPromptTemplateVersionsInput, server.listPromptTemplateVersions, grpccasters.ListPromptTemplateVersionsResponse)
}

// StartAgentSession creates a logical resumable agent session.
func (server *Server) StartAgentSession(ctx context.Context, request *agentsv1.StartAgentSessionRequest) (*agentsv1.AgentSessionResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.StartAgentSessionInput, server.service.StartAgentSession, grpccasters.AgentSessionEntityResponse)
}

// GetAgentSession returns authoritative session state and latest snapshot metadata when available.
func (server *Server) GetAgentSession(ctx context.Context, request *agentsv1.GetAgentSessionRequest) (*agentsv1.AgentSessionResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetAgentSessionInput, server.getAgentSession, grpccasters.AgentSessionResponse)
}

// StartAgentRun creates a role run inside an existing session.
func (server *Server) StartAgentRun(ctx context.Context, request *agentsv1.StartAgentRunRequest) (*agentsv1.AgentRunResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.StartAgentRunInput, server.service.StartAgentRun, grpccasters.AgentRunResponse)
}

// RecordRunState records a lifecycle transition from runtime, MCP or hook ingress.
func (server *Server) RecordRunState(ctx context.Context, request *agentsv1.RecordRunStateRequest) (*agentsv1.AgentRunResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.RecordRunStateInput, server.service.RecordRunState, grpccasters.AgentRunResponse)
}

// RecordSessionStateSnapshot records metadata for a Codex session state object.
func (server *Server) RecordSessionStateSnapshot(ctx context.Context, request *agentsv1.RecordSessionStateSnapshotRequest) (*agentsv1.AgentSessionStateSnapshotResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.RecordSessionStateSnapshotInput, server.service.RecordSessionStateSnapshot, grpccasters.AgentSessionStateSnapshotResponse)
}

// ListAgentRuns returns runs by session, role, status or provider target.
func (server *Server) ListAgentRuns(ctx context.Context, request *agentsv1.ListAgentRunsRequest) (*agentsv1.ListAgentRunsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListAgentRunsInput, server.listAgentRuns, grpccasters.ListAgentRunsResponse)
}

// RequestAcceptance creates a pending machine acceptance check.
func (server *Server) RequestAcceptance(ctx context.Context, request *agentsv1.RequestAcceptanceRequest) (*agentsv1.AcceptanceResultResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.RequestAcceptanceInput, server.service.RequestAcceptance, grpccasters.AcceptanceResultResponse)
}

// RecordAcceptanceResult records safe machine acceptance result metadata.
func (server *Server) RecordAcceptanceResult(ctx context.Context, request *agentsv1.RecordAcceptanceResultRequest) (*agentsv1.AcceptanceResultResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.RecordAcceptanceResultInput, server.service.RecordAcceptanceResult, grpccasters.AcceptanceResultResponse)
}

// GetAcceptanceResult returns one acceptance result.
func (server *Server) GetAcceptanceResult(ctx context.Context, request *agentsv1.GetAcceptanceResultRequest) (*agentsv1.AcceptanceResultResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetAcceptanceResultInput, server.getAcceptanceResult, grpccasters.AcceptanceResultResponse)
}

// ListAcceptanceResults returns acceptance results by session, run, stage or status.
func (server *Server) ListAcceptanceResults(ctx context.Context, request *agentsv1.ListAcceptanceResultsRequest) (*agentsv1.ListAcceptanceResultsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListAcceptanceResultsInput, server.listAcceptanceResults, grpccasters.ListAcceptanceResultsResponse)
}

func (server *Server) getFlow(ctx context.Context, input grpccasters.IDQueryInput) (grpccasters.FlowOutput, error) {
	return entityOutputByID(ctx, input, server.service.GetFlow, server.flowOutput)
}

func (server *Server) flowOutput(ctx context.Context, flow entity.Flow) (grpccasters.FlowOutput, error) {
	return outputWithActiveVersion(ctx, flow, flow.ActiveVersionID, server.service.GetFlowVersion, grpccasters.NewFlowOutput)
}

func (server *Server) listFlows(ctx context.Context, input agentservice.FlowList) (grpccasters.FlowListOutput, error) {
	return listWithPage(ctx, input, server.service.ListFlows)
}

func (server *Server) getRoleProfile(ctx context.Context, input grpccasters.IDQueryInput) (entity.RoleProfile, error) {
	return server.service.GetRoleProfile(ctx, input.ID)
}

func (server *Server) listRoleProfiles(ctx context.Context, input agentservice.RoleProfileList) (grpccasters.RoleProfileListOutput, error) {
	return listWithPage(ctx, input, server.service.ListRoleProfiles)
}

func (server *Server) getPromptTemplate(ctx context.Context, input grpccasters.IDQueryInput) (grpccasters.PromptTemplateOutput, error) {
	return entityOutputByID(ctx, input, server.service.GetPromptTemplate, server.promptTemplateOutput)
}

func (server *Server) promptTemplateOutput(ctx context.Context, template entity.PromptTemplate) (grpccasters.PromptTemplateOutput, error) {
	return outputWithActiveVersion(ctx, template, template.ActiveVersionID, server.service.GetPromptTemplateVersion, grpccasters.NewPromptTemplateOutput)
}

func (server *Server) listPromptTemplates(ctx context.Context, input agentservice.PromptTemplateList) (grpccasters.PromptTemplateListOutput, error) {
	return listWithPage(ctx, input, server.service.ListPromptTemplates)
}

func (server *Server) getPromptTemplateVersion(ctx context.Context, input grpccasters.IDQueryInput) (entity.PromptTemplateVersion, error) {
	return server.service.GetPromptTemplateVersion(ctx, input.ID)
}

func (server *Server) listPromptTemplateVersions(ctx context.Context, input agentservice.PromptTemplateVersionList) (grpccasters.PromptTemplateVersionListOutput, error) {
	return listWithPage(ctx, input, server.service.ListPromptTemplateVersions)
}

func (server *Server) getAgentSession(ctx context.Context, input grpccasters.IDQueryInput) (grpccasters.AgentSessionOutput, error) {
	return entityOutputByID(ctx, input, server.service.GetAgentSession, server.agentSessionOutput)
}

func (server *Server) agentSessionOutput(ctx context.Context, session entity.AgentSession) (grpccasters.AgentSessionOutput, error) {
	return outputWithActiveVersion(ctx, session, session.LatestStateSnapshotID, server.service.GetSessionStateSnapshot, grpccasters.NewAgentSessionOutput)
}

func (server *Server) listAgentRuns(ctx context.Context, input agentservice.AgentRunList) (grpccasters.AgentRunListOutput, error) {
	return listWithPage(ctx, input, server.service.ListAgentRuns)
}

func (server *Server) getAcceptanceResult(ctx context.Context, input grpccasters.IDQueryInput) (entity.AcceptanceResult, error) {
	return server.service.GetAcceptanceResult(ctx, input.ID)
}

func (server *Server) listAcceptanceResults(ctx context.Context, input agentservice.AcceptanceResultList) (grpccasters.AcceptanceResultListOutput, error) {
	return listWithPage(ctx, input, server.service.ListAcceptanceResults)
}

func listWithPage[Input any, Item any](
	ctx context.Context,
	input Input,
	call func(context.Context, Input) ([]Item, value.PageResult, error),
) (grpccasters.PageOutput[Item], error) {
	items, page, err := call(ctx, input)
	return grpccasters.PageOutput[Item]{Items: items, Page: page}, err
}

func entityOutputByID[Entity any, Output any](
	ctx context.Context,
	input grpccasters.IDQueryInput,
	get func(context.Context, uuid.UUID) (Entity, error),
	respond func(context.Context, Entity) (Output, error),
) (Output, error) {
	entity, err := get(ctx, input.ID)
	if err != nil {
		var zero Output
		return zero, err
	}
	return respond(ctx, entity)
}

func outputWithActiveVersion[Entity any, Version any, Output any](
	ctx context.Context,
	entity Entity,
	activeID *uuid.UUID,
	get func(context.Context, uuid.UUID) (Version, error),
	build func(Entity, *Version) Output,
) (Output, error) {
	if activeID == nil {
		return build(entity, nil), nil
	}
	version, err := get(ctx, *activeID)
	if err != nil {
		var zero Output
		return zero, err
	}
	return build(entity, &version), nil
}
