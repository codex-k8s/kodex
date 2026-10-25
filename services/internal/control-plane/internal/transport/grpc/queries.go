package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// GetBootstrapState reads bootstrap state using the principal verified for this RPC and maps
// domain errors to transport status.
// GetBootstrapState читает состояние начальной настройки с principal проверенного RPC и переводит
// доменные ошибки в transport status.
func (server *Server) GetBootstrapState(ctx context.Context, _ *controlplanev1.GetBootstrapStateRequest) (*controlplanev1.GetBootstrapStateResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetBootstrapState_FullMethodName, func(p value.Principal) (*controlplanev1.GetBootstrapStateResponse, error) {
		result, err := server.service.GetBootstrapState(ctx, p)
		if err != nil {
			return nil, err
		}
		return &controlplanev1.GetBootstrapStateResponse{State: castBootstrap(result)}, nil
	})
}

// GetPlatformEventCursor returns the organization and current platform event sequence from the
// authorized domain read.
// GetPlatformEventCursor возвращает организацию и текущую последовательность событий платформы из
// авторизованного доменного чтения.
func (server *Server) GetPlatformEventCursor(ctx context.Context, _ *controlplanev1.GetPlatformEventCursorRequest) (*controlplanev1.GetPlatformEventCursorResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetPlatformEventCursor_FullMethodName, func(p value.Principal) (*controlplanev1.GetPlatformEventCursorResponse, error) {
		organizationRef, sequence, err := server.service.GetPlatformEventCursor(ctx, p)
		if err != nil {
			return nil, err
		}
		return &controlplanev1.GetPlatformEventCursorResponse{OrganizationRef: organizationRef, CurrentSequence: sequence}, nil
	})
}

// GetOverview reads the overview for the requested project under the verified transport principal.
// GetOverview читает обзор запрошенного проекта с проверенным transport principal; доступ
// определяет доменный сервис.
func (server *Server) GetOverview(ctx context.Context, request *controlplanev1.GetOverviewRequest) (*controlplanev1.GetOverviewResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetOverview_FullMethodName, func(p value.Principal) (*controlplanev1.GetOverviewResponse, error) {
		result, err := server.service.GetOverview(ctx, p, request.GetProjectRef())
		if err != nil {
			return nil, err
		}
		return &controlplanev1.GetOverviewResponse{Overview: castOverview(result)}, nil
	})
}

// ListPlatformCapabilities projects the domain capability list after resolving the principal for
// this exact method.
// ListPlatformCapabilities преобразует доменный список возможностей после получения principal для
// точного метода RPC.
func (server *Server) ListPlatformCapabilities(ctx context.Context, _ *controlplanev1.ListPlatformCapabilitiesRequest) (*controlplanev1.ListPlatformCapabilitiesResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListPlatformCapabilities_FullMethodName, func(p value.Principal) (*controlplanev1.ListPlatformCapabilitiesResponse, error) {
		items, err := server.service.ListCapabilities(ctx, p)
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListPlatformCapabilitiesResponse{}
		for _, item := range items {
			response.Capabilities = append(response.Capabilities, castCapability(item))
		}
		return response, nil
	})
}

// ListRuntimeSelections returns runtime selections from the authorized domain read without
// performing runtime probes in transport.
// ListRuntimeSelections возвращает варианты runtime из авторизованного доменного чтения без
// проверок runtime в transport.
func (server *Server) ListRuntimeSelections(ctx context.Context, _ *controlplanev1.ListRuntimeSelectionsRequest) (*controlplanev1.ListRuntimeSelectionsResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListRuntimeSelections_FullMethodName, func(p value.Principal) (*controlplanev1.ListRuntimeSelectionsResponse, error) {
		items, err := server.service.ListRuntimes(ctx, p)
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListRuntimeSelectionsResponse{}
		for _, item := range items {
			response.Runtimes = append(response.Runtimes, castRuntime(item))
		}
		return response, nil
	})
}

// SearchPlatform passes query, project and pagination to the authorized search and preserves total
// and continuation token.
// SearchPlatform передаёт запрос, проект и пагинацию авторизованному поиску, сохраняя общее число
// и токен продолжения.
func (server *Server) SearchPlatform(ctx context.Context, request *controlplanev1.SearchPlatformRequest) (*controlplanev1.SearchPlatformResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_SearchPlatform_FullMethodName, func(p value.Principal) (*controlplanev1.SearchPlatformResponse, error) {
		items, total, next, err := server.service.Search(ctx, p, query.Filter{Query: request.GetQuery(), ProjectRef: request.GetProjectRef(), Limit: request.GetLimit(), Page: searchPage(request)})
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.SearchPlatformResponse{Total: total, Page: &controlplanev1.PageInfo{NextPageToken: next}}
		for _, item := range items {
			response.Results = append(response.Results, castSearchResult(item))
		}
		return response, nil
	})
}

// searchPage copies ordinary page fields and lets a positive legacy limit override page size.
// searchPage копирует обычные поля страницы и заменяет её размер положительным прежним limit.
func searchPage(request *controlplanev1.SearchPlatformRequest) query.Page {
	result := page(request.GetPage())
	if request.GetLimit() > 0 {
		result.Size = request.GetLimit()
	}
	return result
}

// ListProjects returns the authorized project page together with domain next actions and its
// continuation token.
// ListProjects возвращает авторизованную страницу проектов с доменными следующими действиями и
// токеном продолжения.
func (server *Server) ListProjects(ctx context.Context, request *controlplanev1.ListProjectsRequest) (*controlplanev1.ListProjectsResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListProjects_FullMethodName, func(p value.Principal) (*controlplanev1.ListProjectsResponse, error) {
		items, next, actions, err := server.service.ListProjects(ctx, p, query.Filter{Query: request.GetQuery(), Page: page(request.GetPage())})
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListProjectsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}, NextActions: nextActions(actions)}
		for _, item := range items {
			response.Projects = append(response.Projects, castProject(item))
		}
		return response, nil
	})
}

// ListTrashedProjects reads the paginated trashed-project list using the method-specific verified
// principal.
// ListTrashedProjects читает страницу удалённых проектов с principal, проверенным для конкретного
// метода.
func (server *Server) ListTrashedProjects(ctx context.Context, request *controlplanev1.ListTrashedProjectsRequest) (*controlplanev1.ListTrashedProjectsResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListTrashedProjects_FullMethodName, func(p value.Principal) (*controlplanev1.ListTrashedProjectsResponse, error) {
		items, next, err := server.service.ListTrashedProjects(ctx, p, page(request.GetPage()))
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListTrashedProjectsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
		for _, item := range items {
			response.Projects = append(response.Projects, castProject(item))
		}
		return response, nil
	})
}

// GetProject passes the project locator to the authorized domain read and casts its returned
// entity.
// GetProject передаёт ссылку проекта авторизованному доменному чтению и преобразует возвращённую
// сущность.
func (server *Server) GetProject(ctx context.Context, request *controlplanev1.GetProjectRequest) (*controlplanev1.GetProjectResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetProject_FullMethodName, func(p value.Principal) (*controlplanev1.GetProjectResponse, error) {
		item, err := server.service.GetProject(ctx, p, request.GetProjectRef())
		if err != nil {
			return nil, err
		}
		return &controlplanev1.GetProjectResponse{Project: castProject(item)}, nil
	})
}

// ListPlatformMemberships returns a page of platform memberships and the explicit manage-members
// action after domain authorization.
// ListPlatformMemberships возвращает страницу участников платформы и явное действие управления
// участниками после доменной авторизации.
func (server *Server) ListPlatformMemberships(ctx context.Context, request *controlplanev1.ListPlatformMembershipsRequest) (*controlplanev1.ListPlatformMembershipsResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListPlatformMemberships_FullMethodName, func(p value.Principal) (*controlplanev1.ListPlatformMembershipsResponse, error) {
		items, next, err := server.service.ListPlatformMemberships(ctx, p, query.Filter{Page: page(request.GetPage())})
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListPlatformMembershipsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}, NextActions: []controlplanev1.NextAction{controlplanev1.NextAction_NEXT_ACTION_MANAGE_MEMBERS}}
		for _, item := range items {
			response.Memberships = append(response.Memberships, castMembership(item))
		}
		return response, nil
	})
}

// ListPlatformMembershipCandidates reads platform membership candidates using the supplied query
// and page under the verified principal.
// ListPlatformMembershipCandidates читает кандидатов в участники платформы по запросу и странице с
// проверенным principal.
func (server *Server) ListPlatformMembershipCandidates(ctx context.Context, request *controlplanev1.ListPlatformMembershipCandidatesRequest) (*controlplanev1.ListPlatformMembershipCandidatesResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListPlatformMembershipCandidates_FullMethodName, func(p value.Principal) (*controlplanev1.ListPlatformMembershipCandidatesResponse, error) {
		items, next, err := server.service.ListPlatformMembershipCandidates(ctx, p, query.Filter{Query: request.GetQuery(), Page: page(request.GetPage())})
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListPlatformMembershipCandidatesResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
		for _, item := range items {
			response.Users = append(response.Users, castUser(item))
		}
		return response, nil
	})
}

// ListProjectMemberships reads project memberships with the project locator, query and page; the
// domain owns access checks.
// ListProjectMemberships читает участников проекта по ссылке, запросу и странице; проверки доступа
// принадлежат домену.
func (server *Server) ListProjectMemberships(ctx context.Context, request *controlplanev1.ListProjectMembershipsRequest) (*controlplanev1.ListProjectMembershipsResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListProjectMemberships_FullMethodName, func(p value.Principal) (*controlplanev1.ListProjectMembershipsResponse, error) {
		items, next, err := server.service.ListMemberships(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), Page: page(request.GetPage())})
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListProjectMembershipsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
		for _, item := range items {
			response.Memberships = append(response.Memberships, castMembership(item))
		}
		return response, nil
	})
}

// ListProjectMembershipCandidates projects a paginated candidate-user list from the authorized
// project membership read.
// ListProjectMembershipCandidates преобразует страницу пользователей-кандидатов из авторизованного
// чтения участников проекта.
func (server *Server) ListProjectMembershipCandidates(ctx context.Context, request *controlplanev1.ListProjectMembershipCandidatesRequest) (*controlplanev1.ListProjectMembershipCandidatesResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListProjectMembershipCandidates_FullMethodName, func(p value.Principal) (*controlplanev1.ListProjectMembershipCandidatesResponse, error) {
		items, next, err := server.service.ListMembershipCandidates(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), Page: page(request.GetPage())})
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListProjectMembershipCandidatesResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
		for _, item := range items {
			response.Users = append(response.Users, castUser(item))
		}
		return response, nil
	})
}

// ListAgents converts an optional agent state to the domain filter and returns the authorized
// agent page.
// ListAgents преобразует необязательное состояние агента в доменный фильтр и возвращает
// авторизованную страницу агентов.
func (server *Server) ListAgents(ctx context.Context, request *controlplanev1.ListAgentsRequest) (*controlplanev1.ListAgentsResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListAgents_FullMethodName, func(p value.Principal) (*controlplanev1.ListAgentsResponse, error) {
		state := ""
		if request.GetState() != controlplanev1.AgentState_AGENT_STATE_UNSPECIFIED {
			state = enumSuffix(request.GetState(), "AGENT_STATE_")
		}
		items, next, err := server.service.ListAgents(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), State: state, Page: page(request.GetPage())})
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListAgentsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
		for _, item := range items {
			response.Agents = append(response.Agents, castAgent(item))
		}
		return response, nil
	})
}

// GetAgent reads the requested agent through the domain using the principal bound to this RPC.
// GetAgent читает запрошенного агента через домен с principal, связанным с данным RPC.
func (server *Server) GetAgent(ctx context.Context, request *controlplanev1.GetAgentRequest) (*controlplanev1.GetAgentResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetAgent_FullMethodName, func(p value.Principal) (*controlplanev1.GetAgentResponse, error) {
		item, err := server.service.GetAgent(ctx, p, request.GetAgentRef())
		if err != nil {
			return nil, err
		}
		return &controlplanev1.GetAgentResponse{Agent: castAgent(item)}, nil
	})
}

// ListAgentInstructionVersions casts the authorized instruction-version page while preserving its
// domain continuation token.
// ListAgentInstructionVersions преобразует авторизованную страницу версий инструкции, сохраняя
// доменный токен продолжения.
func (server *Server) ListAgentInstructionVersions(ctx context.Context, request *controlplanev1.ListAgentInstructionVersionsRequest) (*controlplanev1.ListAgentInstructionVersionsResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListAgentInstructionVersions_FullMethodName, func(p value.Principal) (*controlplanev1.ListAgentInstructionVersionsResponse, error) {
		items, next, err := server.service.ListAgentInstructionVersions(ctx, p, request.GetAgentRef(), page(request.GetPage()))
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListAgentInstructionVersionsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
		for index := range items {
			response.InstructionVersions = append(response.InstructionVersions, castInstruction(&items[index]))
		}
		return response, nil
	})
}

// ListWorkflows normalizes an optional workflow state and passes project, query and page to the
// domain read.
// ListWorkflows нормализует необязательное состояние процесса и передаёт проект, запрос и страницу
// доменному чтению.
func (server *Server) ListWorkflows(ctx context.Context, request *controlplanev1.ListWorkflowsRequest) (*controlplanev1.ListWorkflowsResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListWorkflows_FullMethodName, func(p value.Principal) (*controlplanev1.ListWorkflowsResponse, error) {
		state := ""
		if request.GetState() != controlplanev1.WorkflowState_WORKFLOW_STATE_UNSPECIFIED {
			state = enumSuffix(request.GetState(), "WORKFLOW_STATE_")
		}
		items, next, err := server.service.ListWorkflows(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), State: state, Page: page(request.GetPage())})
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListWorkflowsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
		for _, item := range items {
			response.Workflows = append(response.Workflows, castWorkflow(item))
		}
		return response, nil
	})
}

// GetWorkflow returns the workflow selected by the domain after method-specific principal
// resolution.
// GetWorkflow возвращает процесс, выбранный доменом после получения principal конкретного метода.
func (server *Server) GetWorkflow(ctx context.Context, request *controlplanev1.GetWorkflowRequest) (*controlplanev1.GetWorkflowResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetWorkflow_FullMethodName, func(p value.Principal) (*controlplanev1.GetWorkflowResponse, error) {
		item, err := server.service.GetWorkflow(ctx, p, request.GetWorkflowRef())
		if err != nil {
			return nil, err
		}
		return &controlplanev1.GetWorkflowResponse{Workflow: castWorkflow(item)}, nil
	})
}

// ListRuns rejects unspecified requested states, forwards session and target filters, and
// preserves total and pagination.
// ListRuns отклоняет неопределённые запрошенные состояния, передаёт фильтры сессии и цели,
// сохраняя общее число и пагинацию.
func (server *Server) ListRuns(ctx context.Context, request *controlplanev1.ListRunsRequest) (*controlplanev1.ListRunsResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListRuns_FullMethodName, func(p value.Principal) (*controlplanev1.ListRunsResponse, error) {
		filter := query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), Page: page(request.GetPage()), ResumableSessionsOnly: request.GetResumableSessionsOnly(), TargetType: request.GetTargetType(), TargetRef: request.GetTargetRef()}
		for _, state := range request.GetStates() {
			if state == controlplanev1.RunState_RUN_STATE_UNSPECIFIED {
				return nil, errs.ErrInvalid
			}
			filter.States = append(filter.States, enumSuffix(state, "RUN_STATE_"))
		}
		items, total, next, err := server.service.ListRuns(ctx, p, filter)
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListRunsResponse{Total: total, Page: &controlplanev1.PageInfo{NextPageToken: next}}
		for _, item := range items {
			response.Runs = append(response.Runs, castRun(item))
		}
		return response, nil
	})
}

// GetRun resolves the verified principal before reading and projecting the requested run.
// GetRun получает проверенный principal до чтения и преобразования запрошенного запуска.
func (server *Server) GetRun(ctx context.Context, request *controlplanev1.GetRunRequest) (*controlplanev1.GetRunResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetRun_FullMethodName, func(p value.Principal) (*controlplanev1.GetRunResponse, error) {
		item, err := server.service.GetRun(ctx, p, request.GetRunRef())
		if err != nil {
			return nil, err
		}
		return &controlplanev1.GetRunResponse{Run: castRun(item)}, nil
	})
}

// GetRunGraph selects the graph read with or without run snapshots and projects the root, graph
// and returned runs.
// GetRunGraph выбирает чтение графа со снимками запусков либо без них и преобразует корень, граф и
// возвращённые запуски.
func (server *Server) GetRunGraph(ctx context.Context, request *controlplanev1.GetRunGraphRequest) (*controlplanev1.GetRunGraphResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetRunGraph_FullMethodName, func(p value.Principal) (*controlplanev1.GetRunGraphResponse, error) {
		readGraph := server.service.GetRunGraph
		if request.GetIncludeRunSnapshots() {
			readGraph = server.service.GetRunGraphSnapshot
		}
		run, graph, err := readGraph(ctx, p, request.GetRunRef())
		if err != nil {
			return nil, err
		}
		runs := make([]*controlplanev1.Run, 0, len(graph.Runs))
		for _, item := range graph.Runs {
			runs = append(runs, castRun(item))
		}
		return &controlplanev1.GetRunGraphResponse{Run: castRun(run), Graph: castGraph(graph), Runs: runs}, nil
	})
}

// ListRunEvents reads events after the requested sequence and returns the current sequence and
// completion flag unchanged.
// ListRunEvents читает события после заданной последовательности и сохраняет текущую
// последовательность и признак завершения.
func (server *Server) ListRunEvents(ctx context.Context, request *controlplanev1.ListRunEventsRequest) (*controlplanev1.ListRunEventsResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListRunEvents_FullMethodName, func(p value.Principal) (*controlplanev1.ListRunEventsResponse, error) {
		items, current, complete, err := server.service.ListRunEvents(ctx, p, query.Filter{ResourceRef: request.GetRunRef(), AfterSequence: request.GetAfterSequence(), Limit: request.GetLimit()})
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListRunEventsResponse{CurrentSequence: current, Complete: complete}
		for _, item := range items {
			response.Events = append(response.Events, castEvent(item))
		}
		return response, nil
	})
}

// ListOwnerGates passes singular and plural state filters to the authorized gate read and returns
// its total and page.
// ListOwnerGates передаёт одиночный и множественный фильтры состояний авторизованному чтению
// шлюзов и возвращает общее число и страницу.
func (server *Server) ListOwnerGates(ctx context.Context, request *controlplanev1.ListOwnerGatesRequest) (*controlplanev1.ListOwnerGatesResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListOwnerGates_FullMethodName, func(p value.Principal) (*controlplanev1.ListOwnerGatesResponse, error) {
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
			return nil, err
		}
		response := &controlplanev1.ListOwnerGatesResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}, Total: total}
		for _, item := range items {
			response.Gates = append(response.Gates, castGate(item))
		}
		return response, nil
	})
}

// GetOwnerGate reads the owner gate identified by the request using the verified method principal.
// GetOwnerGate читает шлюз владельца по ссылке запроса с проверенным principal метода.
func (server *Server) GetOwnerGate(ctx context.Context, request *controlplanev1.GetOwnerGateRequest) (*controlplanev1.GetOwnerGateResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetOwnerGate_FullMethodName, func(p value.Principal) (*controlplanev1.GetOwnerGateResponse, error) {
		item, err := server.service.GetOwnerGate(ctx, p, request.GetGateRef())
		if err != nil {
			return nil, err
		}
		return &controlplanev1.GetOwnerGateResponse{Gate: castGate(item)}, nil
	})
}

// ListArtifacts normalizes artifact filters, rejects more than five source kinds and returns the
// authorized artifact page.
// ListArtifacts нормализует фильтры артефактов, отклоняет более пяти видов источника и возвращает
// авторизованную страницу артефактов.
func (server *Server) ListArtifacts(ctx context.Context, request *controlplanev1.ListArtifactsRequest) (*controlplanev1.ListArtifactsResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListArtifacts_FullMethodName, func(p value.Principal) (*controlplanev1.ListArtifactsResponse, error) {
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
		if len(request.GetSourceKinds()) > 5 {
			return nil, errs.ErrInvalid
		}
		sourceKinds := make([]string, 0, len(request.GetSourceKinds()))
		for _, source := range request.GetSourceKinds() {
			sourceKinds = append(sourceKinds, enumSuffix(source, "ARTIFACT_SOURCE_"))
		}
		items, total, next, err := server.service.ListArtifacts(ctx, p, query.Filter{
			ProjectRef: request.GetProjectRef(), ResourceRef: request.GetRunRef(), Query: request.GetQuery(),
			State: lifecycleState, ArtifactType: artifactType, ScanState: scanState, SourceKind: sourceKind, SourceKinds: sourceKinds, Page: page(request.GetPage()),
		})
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListArtifactsResponse{Total: total, Page: &controlplanev1.PageInfo{NextPageToken: next}}
		for _, item := range items {
			response.Artifacts = append(response.Artifacts, castArtifact(item))
		}
		return response, nil
	})
}

// GetArtifactImpact projects the domain impact decision, digest, counts, blockers and bounded
// active-run list for the requested action.
// GetArtifactImpact преобразует доменное решение о влиянии действия, digest, счётчики, препятствия
// и ограниченный список активных запусков.
func (server *Server) GetArtifactImpact(ctx context.Context, request *controlplanev1.GetArtifactImpactRequest) (*controlplanev1.GetArtifactImpactResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetArtifactImpact_FullMethodName, func(p value.Principal) (*controlplanev1.GetArtifactImpactResponse, error) {
		action := enumSuffix(request.GetAction(), "ARTIFACT_IMPACT_ACTION_")
		impact, err := server.service.GetArtifactImpact(ctx, p, request.GetArtifactRef(), action)
		if err != nil {
			return nil, err
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
	})
}

// GetArtifact returns the artifact from the authorized domain read without reading its content in
// this handler.
// GetArtifact возвращает артефакт из авторизованного доменного чтения без чтения его содержимого в
// обработчике.
func (server *Server) GetArtifact(ctx context.Context, request *controlplanev1.GetArtifactRequest) (*controlplanev1.GetArtifactResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetArtifact_FullMethodName, func(p value.Principal) (*controlplanev1.GetArtifactResponse, error) {
		item, err := server.service.GetArtifact(ctx, p, request.GetArtifactRef())
		if err != nil {
			return nil, err
		}
		return &controlplanev1.GetArtifactResponse{Artifact: castArtifact(item)}, nil
	})
}

// ListArtifactRevisions returns revision entities, total and continuation token from the
// authorized artifact revision read.
// ListArtifactRevisions возвращает сущности ревизий, общее число и токен продолжения из
// авторизованного чтения ревизий артефакта.
func (server *Server) ListArtifactRevisions(ctx context.Context, request *controlplanev1.ListArtifactRevisionsRequest) (*controlplanev1.ListArtifactRevisionsResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListArtifactRevisions_FullMethodName, func(p value.Principal) (*controlplanev1.ListArtifactRevisionsResponse, error) {
		items, total, next, err := server.service.ListArtifactRevisions(ctx, p, request.GetArtifactRef(), query.Page{Size: request.GetPage().GetPageSize(), Token: request.GetPage().GetPageToken()})
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListArtifactRevisionsResponse{Total: total, Page: &controlplanev1.PageInfo{NextPageToken: next}}
		for _, item := range items {
			response.Items = append(response.Items, castArtifactRevision(item))
		}
		return response, nil
	})
}

// GetArtifactRevision passes artifact and revision locators to the authorized domain read before
// projecting the result.
// GetArtifactRevision передаёт ссылки артефакта и ревизии авторизованному доменному чтению до
// преобразования результата.
func (server *Server) GetArtifactRevision(ctx context.Context, request *controlplanev1.GetArtifactRevisionRequest) (*controlplanev1.GetArtifactRevisionResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetArtifactRevision_FullMethodName, func(p value.Principal) (*controlplanev1.GetArtifactRevisionResponse, error) {
		item, err := server.service.GetArtifactRevision(ctx, p, request.GetArtifactRef(), request.GetRevisionRef())
		if err != nil {
			return nil, err
		}
		return &controlplanev1.GetArtifactRevisionResponse{Revision: castArtifactRevision(item)}, nil
	})
}

// GetAttachmentSet returns the authorized attachment-set projection together with the domain
// continuation token.
// GetAttachmentSet возвращает авторизованное представление набора вложений с доменным токеном
// продолжения.
func (server *Server) GetAttachmentSet(ctx context.Context, request *controlplanev1.GetAttachmentSetRequest) (*controlplanev1.GetAttachmentSetResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetAttachmentSet_FullMethodName, func(p value.Principal) (*controlplanev1.GetAttachmentSetResponse, error) {
		item, next, err := server.service.GetAttachmentSet(ctx, p, request.GetAttachmentSetRef(), page(request.GetPage()))
		if err != nil {
			return nil, err
		}
		return &controlplanev1.GetAttachmentSetResponse{AttachmentSet: castAttachmentSet(item),
			Page: &controlplanev1.PageInfo{NextPageToken: next}}, nil
	})
}

// ListSchedules reads schedules with project, text and page filters under the principal verified
// for this RPC.
// ListSchedules читает расписания с фильтрами проекта, текста и страницы с principal, проверенным
// для данного RPC.
func (server *Server) ListSchedules(ctx context.Context, request *controlplanev1.ListSchedulesRequest) (*controlplanev1.ListSchedulesResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListSchedules_FullMethodName, func(p value.Principal) (*controlplanev1.ListSchedulesResponse, error) {
		items, next, err := server.service.ListSchedules(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), Page: page(request.GetPage())})
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListSchedulesResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
		for _, item := range items {
			response.Schedules = append(response.Schedules, castSchedule(item))
		}
		return response, nil
	})
}

// GetSchedule reads and casts one schedule after resolving the method-specific transport
// principal.
// GetSchedule читает и преобразует одно расписание после получения transport principal конкретного
// метода.
func (server *Server) GetSchedule(ctx context.Context, request *controlplanev1.GetScheduleRequest) (*controlplanev1.GetScheduleResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetSchedule_FullMethodName, func(p value.Principal) (*controlplanev1.GetScheduleResponse, error) {
		item, err := server.service.GetSchedule(ctx, p, request.GetScheduleRef())
		if err != nil {
			return nil, err
		}
		return &controlplanev1.GetScheduleResponse{Schedule: castSchedule(item)}, nil
	})
}

// ListIntegrationDefinitions returns definition projections, domain next actions and page
// information with the core-ready transport flag.
// ListIntegrationDefinitions возвращает представления определений, доменные следующие действия и
// страницу с transport-признаком готовности ядра.
func (server *Server) ListIntegrationDefinitions(ctx context.Context, request *controlplanev1.ListIntegrationDefinitionsRequest) (*controlplanev1.ListIntegrationDefinitionsResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListIntegrationDefinitions_FullMethodName, func(p value.Principal) (*controlplanev1.ListIntegrationDefinitionsResponse, error) {
		items, next, actions, err := server.service.ListIntegrationDefinitions(ctx, p, query.Filter{
			Category: request.GetCategory(), Query: request.GetQuery(), Page: page(request.GetPage()),
		})
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListIntegrationDefinitionsResponse{
			NextActions: nextActions(actions), CoreReady: true,
			Page: &controlplanev1.PageInfo{NextPageToken: next},
		}
		for _, item := range items {
			response.Definitions = append(response.Definitions, castDefinition(item))
		}
		return response, nil
	})
}

// ListIntegrationConnections maps the definition key to the domain category filter and returns the
// authorized connection page.
// ListIntegrationConnections переносит ключ определения в доменный фильтр категории и возвращает
// авторизованную страницу подключений.
func (server *Server) ListIntegrationConnections(ctx context.Context, request *controlplanev1.ListIntegrationConnectionsRequest) (*controlplanev1.ListIntegrationConnectionsResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListIntegrationConnections_FullMethodName, func(p value.Principal) (*controlplanev1.ListIntegrationConnectionsResponse, error) {
		items, next, err := server.service.ListIntegrationConnections(ctx, p, query.Filter{
			Category: request.GetDefinitionKey(), Query: request.GetQuery(), Page: page(request.GetPage()),
		})
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListIntegrationConnectionsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
		for _, item := range items {
			response.Connections = append(response.Connections, castConnection(item))
		}
		return response, nil
	})
}

// GetIntegrationConnection reads the requested connection under the verified principal and maps
// domain errors before casting.
// GetIntegrationConnection читает запрошенное подключение с проверенным principal и переводит
// доменные ошибки до преобразования.
func (server *Server) GetIntegrationConnection(ctx context.Context, request *controlplanev1.GetIntegrationConnectionRequest) (*controlplanev1.GetIntegrationConnectionResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetIntegrationConnection_FullMethodName, func(p value.Principal) (*controlplanev1.GetIntegrationConnectionResponse, error) {
		item, err := server.service.GetIntegrationConnection(ctx, p, request.GetConnectionRef())
		if err != nil {
			return nil, err
		}
		return &controlplanev1.GetIntegrationConnectionResponse{Connection: castConnection(item)}, nil
	})
}

// GetAdministration returns administration state obtained from the domain with the verified
// principal for this exact method.
// GetAdministration возвращает административное состояние, полученное из домена с principal,
// проверенным для точного метода.
func (server *Server) GetAdministration(ctx context.Context, _ *controlplanev1.GetAdministrationRequest) (*controlplanev1.GetAdministrationResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetAdministration_FullMethodName, func(p value.Principal) (*controlplanev1.GetAdministrationResponse, error) {
		item, err := server.service.GetAdministration(ctx, p)
		if err != nil {
			return nil, err
		}
		return &controlplanev1.GetAdministrationResponse{State: castAdministration(item)}, nil
	})
}

// ListAuditEvents forwards resource, action, outcome, text and technical-event filters and
// preserves the audit page token.
// ListAuditEvents передаёт фильтры ресурса, действия, исхода, текста и технических событий,
// сохраняя токен страницы аудита.
func (server *Server) ListAuditEvents(ctx context.Context, request *controlplanev1.ListAuditEventsRequest) (*controlplanev1.ListAuditEventsResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListAuditEvents_FullMethodName, func(p value.Principal) (*controlplanev1.ListAuditEventsResponse, error) {
		items, next, err := server.service.ListAuditEvents(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), ResourceRef: request.GetResourceRef(), Action: request.GetAction(), Outcome: request.GetOutcome(), Query: request.GetQuery(), ExcludeTechnical: request.GetExcludeTechnical(), Page: page(request.GetPage())})
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListAuditEventsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
		for _, item := range items {
			response.Events = append(response.Events, castAudit(item))
		}
		return response, nil
	})
}
