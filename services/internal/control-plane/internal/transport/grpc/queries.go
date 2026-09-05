package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func (server *Server) GetBootstrapState(ctx context.Context, _ *controlplanev1.GetBootstrapStateRequest) (*controlplanev1.GetBootstrapStateResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetBootstrapState_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetBootstrapState(ctx, p)
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetBootstrapStateResponse{State: castBootstrap(result)}, nil
}

func (server *Server) GetPlatformEventCursor(ctx context.Context, _ *controlplanev1.GetPlatformEventCursorRequest) (*controlplanev1.GetPlatformEventCursorResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetPlatformEventCursor_FullMethodName)
	if err != nil {
		return nil, err
	}
	organizationRef, sequence, err := server.service.GetPlatformEventCursor(ctx, p)
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetPlatformEventCursorResponse{OrganizationRef: organizationRef, CurrentSequence: sequence}, nil
}

func (server *Server) GetOverview(ctx context.Context, request *controlplanev1.GetOverviewRequest) (*controlplanev1.GetOverviewResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetOverview_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetOverview(ctx, p, request.GetProjectRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetOverviewResponse{Overview: castOverview(result)}, nil
}

func (server *Server) ListPlatformCapabilities(ctx context.Context, _ *controlplanev1.ListPlatformCapabilitiesRequest) (*controlplanev1.ListPlatformCapabilitiesResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListPlatformCapabilities_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, err := server.service.ListCapabilities(ctx, p)
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListPlatformCapabilitiesResponse{}
	for _, item := range items {
		response.Capabilities = append(response.Capabilities, castCapability(item))
	}
	return response, nil
}

func (server *Server) ListRuntimeSelections(ctx context.Context, _ *controlplanev1.ListRuntimeSelectionsRequest) (*controlplanev1.ListRuntimeSelectionsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListRuntimeSelections_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, err := server.service.ListRuntimes(ctx, p)
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListRuntimeSelectionsResponse{}
	for _, item := range items {
		response.Runtimes = append(response.Runtimes, castRuntime(item))
	}
	return response, nil
}

func (server *Server) SearchPlatform(ctx context.Context, request *controlplanev1.SearchPlatformRequest) (*controlplanev1.SearchPlatformResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_SearchPlatform_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, total, next, err := server.service.Search(ctx, p, query.Filter{Query: request.GetQuery(), ProjectRef: request.GetProjectRef(), Limit: request.GetLimit(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.SearchPlatformResponse{Total: total, Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Results = append(response.Results, castSearchResult(item))
	}
	return response, nil
}

func (server *Server) ListProjects(ctx context.Context, request *controlplanev1.ListProjectsRequest) (*controlplanev1.ListProjectsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListProjects_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, actions, err := server.service.ListProjects(ctx, p, query.Filter{Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListProjectsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}, NextActions: nextActions(actions)}
	for _, item := range items {
		response.Projects = append(response.Projects, castProject(item))
	}
	return response, nil
}

func (server *Server) GetProject(ctx context.Context, request *controlplanev1.GetProjectRequest) (*controlplanev1.GetProjectResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetProject_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetProject(ctx, p, request.GetProjectRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetProjectResponse{Project: castProject(item)}, nil
}

func (server *Server) ListPlatformMemberships(ctx context.Context, request *controlplanev1.ListPlatformMembershipsRequest) (*controlplanev1.ListPlatformMembershipsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListPlatformMemberships_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListPlatformMemberships(ctx, p, query.Filter{Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListPlatformMembershipsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}, NextActions: []controlplanev1.NextAction{controlplanev1.NextAction_NEXT_ACTION_MANAGE_MEMBERS}}
	for _, item := range items {
		response.Memberships = append(response.Memberships, castMembership(item))
	}
	return response, nil
}

func (server *Server) ListPlatformMembershipCandidates(ctx context.Context, request *controlplanev1.ListPlatformMembershipCandidatesRequest) (*controlplanev1.ListPlatformMembershipCandidatesResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListPlatformMembershipCandidates_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListPlatformMembershipCandidates(ctx, p, query.Filter{Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListPlatformMembershipCandidatesResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Users = append(response.Users, castUser(item))
	}
	return response, nil
}

func (server *Server) ListProjectMemberships(ctx context.Context, request *controlplanev1.ListProjectMembershipsRequest) (*controlplanev1.ListProjectMembershipsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListProjectMemberships_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListMemberships(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListProjectMembershipsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Memberships = append(response.Memberships, castMembership(item))
	}
	return response, nil
}

func (server *Server) ListProjectMembershipCandidates(ctx context.Context, request *controlplanev1.ListProjectMembershipCandidatesRequest) (*controlplanev1.ListProjectMembershipCandidatesResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListProjectMembershipCandidates_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListMembershipCandidates(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListProjectMembershipCandidatesResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Users = append(response.Users, castUser(item))
	}
	return response, nil
}

func (server *Server) ListAgents(ctx context.Context, request *controlplanev1.ListAgentsRequest) (*controlplanev1.ListAgentsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListAgents_FullMethodName)
	if err != nil {
		return nil, err
	}
	state := ""
	if request.GetState() != controlplanev1.AgentState_AGENT_STATE_UNSPECIFIED {
		state = enumSuffix(request.GetState(), "AGENT_STATE_")
	}
	items, next, err := server.service.ListAgents(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), State: state, Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListAgentsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Agents = append(response.Agents, castAgent(item))
	}
	return response, nil
}

func (server *Server) GetAgent(ctx context.Context, request *controlplanev1.GetAgentRequest) (*controlplanev1.GetAgentResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetAgent_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetAgent(ctx, p, request.GetAgentRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetAgentResponse{Agent: castAgent(item)}, nil
}

func (server *Server) ListAgentInstructionVersions(ctx context.Context, request *controlplanev1.ListAgentInstructionVersionsRequest) (*controlplanev1.ListAgentInstructionVersionsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListAgentInstructionVersions_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListAgentInstructionVersions(ctx, p, request.GetAgentRef(), page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListAgentInstructionVersionsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for index := range items {
		response.InstructionVersions = append(response.InstructionVersions, castInstruction(&items[index]))
	}
	return response, nil
}

func (server *Server) ListWorkflows(ctx context.Context, request *controlplanev1.ListWorkflowsRequest) (*controlplanev1.ListWorkflowsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListWorkflows_FullMethodName)
	if err != nil {
		return nil, err
	}
	state := ""
	if request.GetState() != controlplanev1.WorkflowState_WORKFLOW_STATE_UNSPECIFIED {
		state = enumSuffix(request.GetState(), "WORKFLOW_STATE_")
	}
	items, next, err := server.service.ListWorkflows(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), State: state, Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListWorkflowsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Workflows = append(response.Workflows, castWorkflow(item))
	}
	return response, nil
}

func (server *Server) GetWorkflow(ctx context.Context, request *controlplanev1.GetWorkflowRequest) (*controlplanev1.GetWorkflowResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetWorkflow_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetWorkflow(ctx, p, request.GetWorkflowRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetWorkflowResponse{Workflow: castWorkflow(item)}, nil
}

func (server *Server) ListRuns(ctx context.Context, request *controlplanev1.ListRunsRequest) (*controlplanev1.ListRunsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListRuns_FullMethodName)
	if err != nil {
		return nil, err
	}
	filter := query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), Page: page(request.GetPage())}
	for _, state := range request.GetStates() {
		if state == controlplanev1.RunState_RUN_STATE_UNSPECIFIED {
			return nil, transportError(errs.ErrInvalid)
		}
		filter.States = append(filter.States, enumSuffix(state, "RUN_STATE_"))
	}
	items, total, next, err := server.service.ListRuns(ctx, p, filter)
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListRunsResponse{Total: total, Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Runs = append(response.Runs, castRun(item))
	}
	return response, nil
}

func (server *Server) GetRun(ctx context.Context, request *controlplanev1.GetRunRequest) (*controlplanev1.GetRunResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetRun_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetRun(ctx, p, request.GetRunRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetRunResponse{Run: castRun(item)}, nil
}

func (server *Server) GetRunGraph(ctx context.Context, request *controlplanev1.GetRunGraphRequest) (*controlplanev1.GetRunGraphResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetRunGraph_FullMethodName)
	if err != nil {
		return nil, err
	}
	run, graph, err := server.service.GetRunGraph(ctx, p, request.GetRunRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetRunGraphResponse{Run: castRun(run), Graph: castGraph(graph)}, nil
}

func (server *Server) ListRunEvents(ctx context.Context, request *controlplanev1.ListRunEventsRequest) (*controlplanev1.ListRunEventsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListRunEvents_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, current, complete, err := server.service.ListRunEvents(ctx, p, query.Filter{ResourceRef: request.GetRunRef(), AfterSequence: request.GetAfterSequence(), Limit: request.GetLimit()})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListRunEventsResponse{CurrentSequence: current, Complete: complete}
	for _, item := range items {
		response.Events = append(response.Events, castEvent(item))
	}
	return response, nil
}

func (server *Server) ListOwnerGates(ctx context.Context, request *controlplanev1.ListOwnerGatesRequest) (*controlplanev1.ListOwnerGatesResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListOwnerGates_FullMethodName)
	if err != nil {
		return nil, err
	}
	state := ""
	if request.GetState() != controlplanev1.OwnerGateState_OWNER_GATE_STATE_UNSPECIFIED {
		state = enumSuffix(request.GetState(), "OWNER_GATE_STATE_")
	}
	states := make([]string, 0, len(request.GetStates()))
	for _, item := range request.GetStates() {
		states = append(states, enumSuffix(item, "OWNER_GATE_STATE_"))
	}
	items, total, next, err := server.service.ListOwnerGates(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), State: state, States: states, Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListOwnerGatesResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}, Total: total}
	for _, item := range items {
		response.Gates = append(response.Gates, castGate(item))
	}
	return response, nil
}

func (server *Server) GetOwnerGate(ctx context.Context, request *controlplanev1.GetOwnerGateRequest) (*controlplanev1.GetOwnerGateResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetOwnerGate_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetOwnerGate(ctx, p, request.GetGateRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetOwnerGateResponse{Gate: castGate(item)}, nil
}

func (server *Server) ListArtifacts(ctx context.Context, request *controlplanev1.ListArtifactsRequest) (*controlplanev1.ListArtifactsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListArtifacts_FullMethodName)
	if err != nil {
		return nil, err
	}
	lifecycleState := ""
	if request.GetLifecycleState() != controlplanev1.ArtifactLifecycleState_ARTIFACT_LIFECYCLE_STATE_UNSPECIFIED {
		lifecycleState = enumSuffix(request.GetLifecycleState(), "ARTIFACT_LIFECYCLE_STATE_")
	}
	artifactType := ""
	if request.GetType() != controlplanev1.ArtifactType_ARTIFACT_TYPE_UNSPECIFIED {
		artifactType = enumSuffix(request.GetType(), "ARTIFACT_TYPE_")
	}
	scanState := ""
	if request.GetScanState() != controlplanev1.ArtifactScanState_ARTIFACT_SCAN_STATE_UNSPECIFIED {
		scanState = enumSuffix(request.GetScanState(), "ARTIFACT_SCAN_STATE_")
	}
	sourceKind := ""
	if request.GetSourceKind() != controlplanev1.ArtifactSource_ARTIFACT_SOURCE_UNSPECIFIED {
		sourceKind = enumSuffix(request.GetSourceKind(), "ARTIFACT_SOURCE_")
	}
	items, total, next, err := server.service.ListArtifacts(ctx, p, query.Filter{
		ProjectRef: request.GetProjectRef(), ResourceRef: request.GetRunRef(), Query: request.GetQuery(),
		State: lifecycleState, ArtifactType: artifactType, ScanState: scanState, SourceKind: sourceKind, Page: page(request.GetPage()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListArtifactsResponse{Total: total, Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Artifacts = append(response.Artifacts, castArtifact(item))
	}
	return response, nil
}

func (server *Server) GetArtifactImpact(ctx context.Context, request *controlplanev1.GetArtifactImpactRequest) (*controlplanev1.GetArtifactImpactResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetArtifactImpact_FullMethodName)
	if err != nil {
		return nil, err
	}
	action := enumSuffix(request.GetAction(), "ARTIFACT_IMPACT_ACTION_")
	impact, err := server.service.GetArtifactImpact(ctx, p, request.GetArtifactRef(), action)
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ArtifactImpact{
		ArtifactRef: impact.ArtifactRef, ArtifactVersion: impact.ArtifactVersion,
		Action: request.GetAction(), ImpactDigest: impact.Digest, BindingCount: impact.BindingCount,
		AttachmentCount: impact.AttachmentCount, ActiveRuntimeCount: impact.ActiveRuntimeCount,
		Blockers: impact.Blockers, Permitted: impact.Permitted, ActiveRunsTruncated: impact.ActiveRunsTruncated,
	}
	for _, run := range impact.ActiveRuns {
		response.ActiveRuns = append(response.ActiveRuns, &controlplanev1.ArtifactImpactRun{
			RunRef: run.RunRef, Title: run.Title,
			State: runState(run.State), ProjectRef: run.ProjectRef,
		})
	}
	return &controlplanev1.GetArtifactImpactResponse{Impact: response}, nil
}

func (server *Server) GetArtifact(ctx context.Context, request *controlplanev1.GetArtifactRequest) (*controlplanev1.GetArtifactResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetArtifact_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetArtifact(ctx, p, request.GetArtifactRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetArtifactResponse{Artifact: castArtifact(item)}, nil
}

func (server *Server) GetAttachmentSet(ctx context.Context, request *controlplanev1.GetAttachmentSetRequest) (*controlplanev1.GetAttachmentSetResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetAttachmentSet_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, next, err := server.service.GetAttachmentSet(ctx, p, request.GetAttachmentSetRef(), page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetAttachmentSetResponse{AttachmentSet: castAttachmentSet(item),
		Page: &controlplanev1.PageInfo{NextPageToken: next}}, nil
}

func (server *Server) ListSchedules(ctx context.Context, request *controlplanev1.ListSchedulesRequest) (*controlplanev1.ListSchedulesResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListSchedules_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListSchedules(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListSchedulesResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Schedules = append(response.Schedules, castSchedule(item))
	}
	return response, nil
}

func (server *Server) GetSchedule(ctx context.Context, request *controlplanev1.GetScheduleRequest) (*controlplanev1.GetScheduleResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetSchedule_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetSchedule(ctx, p, request.GetScheduleRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetScheduleResponse{Schedule: castSchedule(item)}, nil
}

func (server *Server) ListIntegrationDefinitions(ctx context.Context, request *controlplanev1.ListIntegrationDefinitionsRequest) (*controlplanev1.ListIntegrationDefinitionsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListIntegrationDefinitions_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, actions, err := server.service.ListIntegrationDefinitions(ctx, p, query.Filter{
		Category: request.GetCategory(), Query: request.GetQuery(), Page: page(request.GetPage()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListIntegrationDefinitionsResponse{
		NextActions: nextActions(actions), CoreReady: true,
		Page: &controlplanev1.PageInfo{NextPageToken: next},
	}
	for _, item := range items {
		response.Definitions = append(response.Definitions, castDefinition(item))
	}
	return response, nil
}

func (server *Server) ListIntegrationConnections(ctx context.Context, request *controlplanev1.ListIntegrationConnectionsRequest) (*controlplanev1.ListIntegrationConnectionsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListIntegrationConnections_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListIntegrationConnections(ctx, p, query.Filter{
		Category: request.GetDefinitionKey(), Query: request.GetQuery(), Page: page(request.GetPage()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListIntegrationConnectionsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Connections = append(response.Connections, castConnection(item))
	}
	return response, nil
}

func (server *Server) GetIntegrationConnection(ctx context.Context, request *controlplanev1.GetIntegrationConnectionRequest) (*controlplanev1.GetIntegrationConnectionResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetIntegrationConnection_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetIntegrationConnection(ctx, p, request.GetConnectionRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetIntegrationConnectionResponse{Connection: castConnection(item)}, nil
}

func (server *Server) GetAdministration(ctx context.Context, _ *controlplanev1.GetAdministrationRequest) (*controlplanev1.GetAdministrationResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetAdministration_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetAdministration(ctx, p)
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetAdministrationResponse{State: castAdministration(item)}, nil
}

func (server *Server) ListAuditEvents(ctx context.Context, request *controlplanev1.ListAuditEventsRequest) (*controlplanev1.ListAuditEventsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListAuditEvents_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListAuditEvents(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Action: request.GetAction(), Outcome: request.GetOutcome(), Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListAuditEventsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Events = append(response.Events, castAudit(item))
	}
	return response, nil
}
