package httptransport

import (
	"fmt"
	"net/http"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/boundary"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GetBootstrapState requires a verified positive session revision, reads owner bootstrap metadata and exposes that session revision as ETag before the specialized bootstrap projection; it does not derive identity from request fields.
// GetBootstrapState требует проверенную положительную ревизию сессии, читает bootstrap metadata владельца и задаёт ETag этой ревизии до специальной проекции; личность из полей запроса не выводится.
func (server *Server) GetBootstrapState(w http.ResponseWriter, r *http.Request) {
	identity, ok := boundary.IdentityFromContext(r.Context())
	if !ok || identity.SessionRevision < 1 {
		writeLocalProblem(w, http.StatusUnauthorized, "UNAUTHENTICATED", false)
		return
	}
	response, err := server.control.Query.GetBootstrapState(r.Context(), &controlplanev1.GetBootstrapStateRequest{})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", identity.SessionRevision))
	server.writeBootstrapState(w, r, response.GetState())
}

// GetOverview forwards the optional project selector under the verified request context and writes the owner overview through the common public projection; the selector grants no access itself.
// GetOverview передаёт необязательный селектор проекта в проверенном контексте и пишет обзор владельца общей публичной проекцией; сам селектор доступ не выдаёт.
func (server *Server) GetOverview(w http.ResponseWriter, r *http.Request, p generated.GetOverviewParams) {
	response, err := server.control.Query.GetOverview(r.Context(), &controlplanev1.GetOverviewRequest{ProjectRef: stringValue(p.ProjectRef)})
	writeReadRPCResult(w, response, err, "overview", "")
}

// ListPlatformCapabilities reads owner-visible capability metadata and reshapes the capabilities page without granting or executing any capability.
// ListPlatformCapabilities читает видимую metadata возможностей и перестраивает страницу capabilities без выдачи и исполнения возможностей.
func (server *Server) ListPlatformCapabilities(w http.ResponseWriter, r *http.Request) {
	response, err := server.control.Query.ListPlatformCapabilities(r.Context(), &controlplanev1.ListPlatformCapabilitiesRequest{})
	writeReadRPCResult(w, response, err, "", "capabilities")
}

// ListRuntimeSelections returns owner-visible runtime-selection metadata as a public page; listing does not select, admit or start a runtime.
// ListRuntimeSelections возвращает видимую metadata runtime selections публичной страницей; перечисление runtime не выбирает, не допускает и не запускает.
func (server *Server) ListRuntimeSelections(w http.ResponseWriter, r *http.Request) {
	response, err := server.control.Query.ListRuntimeSelections(r.Context(), &controlplanev1.ListRuntimeSelectionsRequest{})
	writeReadRPCResult(w, response, err, "", "runtimes")
}

// SearchPlatform validates catalog context, query and limit1..50, defaults to twenty results and passes pagination to the owner before specialized scoped page validation; it does not expand visibility beyond owner results.
// SearchPlatform проверяет контекст каталога, запрос и limit1..50, задаёт двадцать результатов по умолчанию и передаёт пагинацию владельцу до проверки страницы своей области; видимость не расширяется.
func (server *Server) SearchPlatform(w http.ResponseWriter, r *http.Request, p generated.SearchPlatformParams) {
	r, ok := catalogRequest(w, r, p.ProjectRef, &p.Query, nil, p.PageToken)
	if !ok {
		return
	}
	if !validSearchQuery(p.Query) || p.Limit != nil && (*p.Limit < 1 || *p.Limit > 50) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	limit := int32(20)
	if p.Limit != nil {
		limit = int32(*p.Limit)
	}
	response, err := server.control.Query.SearchPlatform(r.Context(), &controlplanev1.SearchPlatformRequest{
		Query: p.Query, Limit: limit, ProjectRef: stringValue(p.ProjectRef), Page: page(nil, p.PageToken),
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeSearchPage(w, response, stringValue(p.ProjectRef), int(limit))
}

// ListProjects forwards page and text selectors to the owner and exposes the public projects page without assigning membership from returned data.
// ListProjects передаёт селекторы страницы и текста владельцу и выдаёт публичную страницу проектов без назначения членства по возвращённым данным.
func (server *Server) ListProjects(w http.ResponseWriter, r *http.Request, p generated.ListProjectsParams) {
	response, err := server.control.Query.ListProjects(r.Context(), &controlplanev1.ListProjectsRequest{Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query)})
	writeReadRPCResult(w, response, err, "", "projects")
}

// ListTrashedProjects requires a nonnil owner page containing only nonnil TRASHED or PURGE_PENDING projects before projecting trash; it neither deletes resources nor repairs their lifecycle.
// ListTrashedProjects требует непустой ответ владельца только с ненулевыми TRASHED или PURGE_PENDING проектами до проекции корзины; ресурсы не удаляет и жизненный цикл не исправляет.
func (server *Server) ListTrashedProjects(w http.ResponseWriter, r *http.Request, p generated.ListTrashedProjectsParams) {
	response, err := server.control.Query.ListTrashedProjects(r.Context(), &controlplanev1.ListTrashedProjectsRequest{Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	for _, project := range response.Projects {
		if project == nil || project.Lifecycle != controlplanev1.EntityLifecycle_ENTITY_LIFECYCLE_TRASHED &&
			project.Lifecycle != controlplanev1.EntityLifecycle_ENTITY_LIFECYCLE_PURGE_PENDING {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
	}
	writeMessage(w, http.StatusOK, response, "", "projects")
}

// GetProject binds the exact project selector to a derived request context, then delegates eligibility and reading to the owner before public projection.
// GetProject привязывает точный селектор проекта к производному контексту, затем поручает допуск и чтение владельцу до публичной проекции.
func (server *Server) GetProject(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	response, err := server.control.Query.GetProject(r.Context(), &controlplanev1.GetProjectRequest{ProjectRef: ref})
	writeReadRPCResult(w, response, err, "project", "")
}

// ListPlatformMemberships reads the requested owner-filtered membership page and projects metadata; returned role names do not authenticate the caller.
// ListPlatformMemberships читает запрошенную отфильтрованную владельцем страницу членства и проецирует metadata; возвращённые роли вызывающего не аутентифицируют.
func (server *Server) ListPlatformMemberships(w http.ResponseWriter, r *http.Request, p generated.ListPlatformMembershipsParams) {
	response, err := server.control.Query.ListPlatformMemberships(r.Context(), &controlplanev1.ListPlatformMembershipsRequest{Page: page(p.PageSize, p.PageToken)})
	writeReadRPCResult(w, response, err, "", "memberships")
}

// ListPlatformMembershipCandidates forwards query/pagination to the owner candidate lookup and returns public user summaries without creating memberships.
// ListPlatformMembershipCandidates передаёт запрос и пагинацию поиску кандидатов владельца и выдаёт публичные сводки пользователей без создания членства.
func (server *Server) ListPlatformMembershipCandidates(w http.ResponseWriter, r *http.Request, p generated.ListPlatformMembershipCandidatesParams) {
	response, err := server.control.Query.ListPlatformMembershipCandidates(r.Context(), &controlplanev1.ListPlatformMembershipCandidatesRequest{Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query)})
	writeReadRPCResult(w, response, err, "", "users")
}

// ListProjectMemberships validates scoped catalog input and forwards exact project, query and paging to the owner, preserving public membership-page shape.
// ListProjectMemberships проверяет вход каталога своей области и передаёт точные проект, запрос и страницу владельцу, сохраняя публичную форму членства.
func (server *Server) ListProjectMemberships(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.ListProjectMembershipsParams) {
	r, ok := catalogRequest(w, r, &ref, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	response, err := server.control.Query.ListProjectMemberships(r.Context(), &controlplanev1.ListProjectMembershipsRequest{ProjectRef: ref, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken)})
	writeReadRPCResult(w, response, err, "", "memberships")
}

// ListProjectMembershipCandidates binds the route project to request context and forwards candidate query/page to the owner; it does not assign project access.
// ListProjectMembershipCandidates привязывает проект маршрута к контексту и передаёт поиск и страницу кандидатов владельцу; проектный доступ не назначается.
func (server *Server) ListProjectMembershipCandidates(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.ListProjectMembershipCandidatesParams) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	response, err := server.control.Query.ListProjectMembershipCandidates(r.Context(), &controlplanev1.ListProjectMembershipCandidatesRequest{ProjectRef: ref, Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query)})
	writeReadRPCResult(w, response, err, "", "users")
}

// ListAgents binds project context and validates the optional agent-state filter before its typed owner query and public agents-page projection; no agent is launched.
// ListAgents привязывает контекст проекта и проверяет необязательный фильтр состояния сотрудника до типизированного запроса владельцу и проекции agents; сотрудник не запускается.
func (server *Server) ListAgents(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.ListAgentsParams) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	state, stateOK := catalogStateFilter(p.State, controlplanev1.AgentState_value, "AGENT_STATE_")
	if !stateOK {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListAgents(r.Context(), &controlplanev1.ListAgentsRequest{ProjectRef: ref, Page: page(p.PageSize, p.PageToken), State: controlplanev1.AgentState(state), Query: stringValue(p.Query)})
	writeReadRPCResult(w, response, err, "", "agents")
}

// GetAgent resolves the selected agent through the owner context and writes its public projection, leaving existence and eligibility decisions to that owner.
// GetAgent разрешает выбранного сотрудника в контексте владельца и пишет публичную проекцию, оставляя владельцу решения о существовании и допуске.
func (server *Server) GetAgent(w http.ResponseWriter, r *http.Request, ref generated.AgentRef) {
	response, err := server.control.Query.GetAgent(r.Context(), &controlplanev1.GetAgentRequest{AgentRef: ref})
	writeReadRPCResult(w, response, err, "agent", "")
}

// ListAgentInstructionVersions reads the selected agent's paginated instruction-version metadata without publishing, activating or editing instructions.
// ListAgentInstructionVersions читает страницу metadata версий инструкций выбранного сотрудника без публикации, активации и изменения.
func (server *Server) ListAgentInstructionVersions(w http.ResponseWriter, r *http.Request, ref generated.AgentRef, p generated.ListAgentInstructionVersionsParams) {
	response, err := server.control.Query.ListAgentInstructionVersions(r.Context(), &controlplanev1.ListAgentInstructionVersionsRequest{AgentRef: ref, Page: page(p.PageSize, p.PageToken)})
	writeReadRPCResult(w, response, err, "", "instructionVersions")
}

// ListWorkflows binds project context, validates the optional workflow-state filter and projects the owner page without executing or publishing a workflow.
// ListWorkflows привязывает проект, проверяет необязательный фильтр состояния процесса и проецирует страницу владельца без выполнения и публикации.
func (server *Server) ListWorkflows(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.ListWorkflowsParams) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	state, stateOK := catalogStateFilter(p.State, controlplanev1.WorkflowState_value, "WORKFLOW_STATE_")
	if !stateOK {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListWorkflows(r.Context(), &controlplanev1.ListWorkflowsRequest{ProjectRef: ref, Page: page(p.PageSize, p.PageToken), State: controlplanev1.WorkflowState(state), Query: stringValue(p.Query)})
	writeReadRPCResult(w, response, err, "", "workflows")
}

// GetWorkflow reads the exact owner-authorized workflow and delegates selected revision/body shaping to the common projector without mutating draft or published state.
// GetWorkflow читает точный разрешённый владельцем процесс и передаёт общему projector выбор формы ревизии и тела без изменения чернового и опубликованного состояний.
func (server *Server) GetWorkflow(w http.ResponseWriter, r *http.Request, ref generated.WorkflowRef) {
	response, err := server.control.Query.GetWorkflow(r.Context(), &controlplanev1.GetWorkflowRequest{WorkflowRef: ref})
	writeReadRPCResult(w, response, err, "workflow", "")
}

// ListRuns validates scoped search, bounded unique state filters and the mutually constrained resumable-target mode, then checks owner count/pagination and resumable-page pins before projecting. Invalid upstream state fails closed rather than fabricating an eligible session.
// ListRuns проверяет поиск области, ограниченные уникальные состояния и совместимость resumable-target режима, затем сверяет счётчик, пагинацию и pins страницы владельца. Неверный ответ закрыто отклоняется вместо выдумывания допустимой сессии.
func (server *Server) ListRuns(w http.ResponseWriter, r *http.Request, p generated.ListRunsParams) {
	r, ok := catalogRequest(w, r, p.ProjectRef, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	if !validSearchText(stringValue(p.Query), 0, 200) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	resumable := boolValue(p.ResumableSessionsOnly)
	targetType, targetRef := stringValue(p.TargetType), stringValue(p.TargetRef)
	if resumable && p.States != nil || (p.TargetType == nil) != (p.TargetRef == nil) ||
		p.TargetType != nil && (!resumable || !p.TargetType.Valid() || !effectiveCapabilityRef(targetRef)) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	states := []controlplanev1.RunState{}
	if p.States != nil {
		if len(*p.States) == 0 || len(*p.States) > 7 {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		seen := map[controlplanev1.RunState]bool{}
		for _, state := range *p.States {
			value, known := controlplanev1.RunState_value["RUN_STATE_"+string(state)]
			if !state.Valid() || !known || value == 0 || seen[controlplanev1.RunState(value)] {
				writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
				return
			}
			seen[controlplanev1.RunState(value)] = true
			states = append(states, controlplanev1.RunState(value))
		}
	}
	response, err := server.control.Query.ListRuns(r.Context(), &controlplanev1.ListRunsRequest{ProjectRef: stringValue(p.ProjectRef), Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query), States: states, ResumableSessionsOnly: resumable, TargetType: targetType, TargetRef: targetRef})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || !validCountedCatalogPage(response.GetTotal(), len(response.GetRuns()), response.GetPage()) ||
		resumable && !validResumableRunPage(response, stringValue(p.ProjectRef), targetType, targetRef, stringValue(p.PageToken), p.PageSize) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "runs")
}

// GetRun requests one selected run under owner policy and emits its public view without treating the reference as authority or resuming execution.
// GetRun запрашивает выбранный запуск по политике владельца и выдаёт публичное представление без принятия ссылки за полномочия и без возобновления.
func (server *Server) GetRun(w http.ResponseWriter, r *http.Request, ref generated.RunRef) {
	response, err := server.control.Query.GetRun(r.Context(), &controlplanev1.GetRunRequest{RunRef: ref})
	writeReadRPCResult(w, response, err, "run", "")
}

// GetRunGraph reads the owner graph but reconstructs a response containing only Run and Graph, excluding full internal run snapshots before public projection.
// GetRunGraph читает граф владельца, но пересобирает ответ только из Run и Graph, исключая полные внутренние снимки запусков до публичной проекции.
func (server *Server) GetRunGraph(w http.ResponseWriter, r *http.Request, ref generated.RunRef) {
	response, err := server.control.Query.GetRunGraph(r.Context(), &controlplanev1.GetRunGraphRequest{RunRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, &controlplanev1.GetRunGraphResponse{Run: response.GetRun(), Graph: response.GetGraph()}, "", "")
}

// ListRunEvents forwards cursor/default limit200 to the owner, preserves completeness/current sequence and renames events to items after public projection and localization. It writes no-store JSON directly without an explicit status write and ignores the final encoder error.
// ListRunEvents передаёт курсор и limit200 по умолчанию владельцу, сохраняет complete/currentSequence и переименовывает events в items после публичной проекции и локализации. Пишет no-store JSON без явной записи статуса и игнорирует финальную ошибку encoder.
func (server *Server) ListRunEvents(w http.ResponseWriter, r *http.Request, ref generated.RunRef, p generated.ListRunEventsParams) {
	after, limit := int64(0), int32(200)
	if p.AfterSequence != nil {
		after = *p.AfterSequence
	}
	if p.Limit != nil {
		limit = int32(*p.Limit)
	}
	response, err := server.control.Query.ListRunEvents(r.Context(), &controlplanev1.ListRunEventsRequest{RunRef: ref, AfterSequence: after, Limit: limit})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	value, encodeErr := messageMap(response)
	if encodeErr != nil {
		writeLocalProblem(w, http.StatusInternalServerError, "INTERNAL", false)
		return
	}
	value["items"] = value["events"]
	delete(value, "events")
	if localizer, ok := w.(interface{ Localize(string) string }); ok {
		LocalizeSafeErrors(value, localizer.Localize)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = jsonEncoder(w).Encode(value)
}

// ListOwnerGates validates search and mutually exclusive single/list gate-state filters, rejecting duplicates or more than six states, then delegates exact scoped page verification to writeOwnerGatePage without deciding any gate.
// ListOwnerGates проверяет поиск и взаимоисключающие одиночный и списочный фильтры gate, отклоняя дубликаты и более шести состояний, затем передаёт проверку страницы writeOwnerGatePage без принятия решений.
func (server *Server) ListOwnerGates(w http.ResponseWriter, r *http.Request, p generated.ListOwnerGatesParams) {
	r, ok := catalogRequest(w, r, p.ProjectRef, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	if !validSearchText(stringValue(p.Query), 0, 200) || p.State != nil && p.States != nil {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	state := controlplanev1.OwnerGateState_OWNER_GATE_STATE_UNSPECIFIED
	if p.State != nil {
		value, known := controlplanev1.OwnerGateState_value["OWNER_GATE_STATE_"+string(*p.State)]
		if !p.State.Valid() || !known || value == 0 {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		state = controlplanev1.OwnerGateState(value)
	}
	states := []controlplanev1.OwnerGateState{}
	if p.States != nil {
		if len(*p.States) == 0 || len(*p.States) > 6 {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		seen := map[controlplanev1.OwnerGateState]bool{}
		for _, selected := range *p.States {
			value, known := controlplanev1.OwnerGateState_value["OWNER_GATE_STATE_"+string(selected)]
			if !selected.Valid() || !known || value == 0 || seen[controlplanev1.OwnerGateState(value)] {
				writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
				return
			}
			seen[controlplanev1.OwnerGateState(value)] = true
			states = append(states, controlplanev1.OwnerGateState(value))
		}
	}
	paging := page(p.PageSize, p.PageToken)
	response, err := server.control.Query.ListOwnerGates(r.Context(), &controlplanev1.ListOwnerGatesRequest{ProjectRef: stringValue(p.ProjectRef), Page: paging, State: state, States: states, Query: stringValue(p.Query)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeOwnerGatePage(w, response, stringValue(p.ProjectRef), state, states, int(paging.PageSize))
}

// GetOwnerGate requires the returned owner gate to exist and match the route reference before public projection; this read neither confirms nor executes a decision.
// GetOwnerGate требует существующий gate ответа с точной ссылкой маршрута до публичной проекции; чтение решение не подтверждает и не выполняет.
func (server *Server) GetOwnerGate(w http.ResponseWriter, r *http.Request, ref generated.GateRef) {
	response, err := server.control.Query.GetOwnerGate(r.Context(), &controlplanev1.GetOwnerGateRequest{GateRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response.GetGate() == nil || response.Gate.Ref != ref {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusOK, response, "gate", "")
}

// ListArtifacts validates project-scoped catalog and artifact filters, retaining the route project and optional run selector in the typed owner request. A nil owner page fails closed before public artifact metadata is written.
// ListArtifacts проверяет проектный каталог и фильтры файлов, сохраняя проект маршрута и необязательный run selector в типизированном запросе владельцу. Nil-страница отклоняется до выдачи публичной metadata файлов.
func (server *Server) ListArtifacts(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.ListArtifactsParams) {
	r, ok := catalogRequest(w, r, &ref, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	filters, filtersOK := artifactCatalogFilters(p.LifecycleState, p.Type, p.ScanState, p.SourceKind, p.SourceKinds)
	if !filtersOK {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	filters.ProjectRef, filters.RunRef = ref, stringValue(p.RunRef)
	filters.Page, filters.Query = page(p.PageSize, p.PageToken), stringValue(p.Query)
	response, err := server.control.Query.ListArtifacts(r.Context(), filters)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "artifacts")
}

// ListOrganizationArtifacts uses owner-derived organization scope with the same pure artifact filters, intentionally omitting project and run selectors. It requires a nonnil page and never widens scope through a caller organization field.
// ListOrganizationArtifacts использует организационную область владельца с теми же чистыми фильтрами файлов, намеренно без project и run selectors. Требует ненулевую страницу и не расширяет область входным полем организации.
func (server *Server) ListOrganizationArtifacts(w http.ResponseWriter, r *http.Request, p generated.ListOrganizationArtifactsParams) {
	r, ok := catalogRequest(w, r, nil, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	filters, filtersOK := artifactCatalogFilters(p.LifecycleState, p.Type, p.ScanState, p.SourceKind, p.SourceKinds)
	if !filtersOK {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	filters.Page, filters.Query = page(p.PageSize, p.PageToken), stringValue(p.Query)
	response, err := server.control.Query.ListArtifacts(r.Context(), filters)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "artifacts")
}

// artifactCatalogFilters applies the five existing pure filter parsers in their original order and returns a fresh typed request containing only those filters after all checks pass. Separate string-like type parameters preserve generated project/organization parameter types without a dispatcher; scope, paging and query remain caller-owned, and invalid input returns no partial request.
// artifactCatalogFilters применяет пять прежних чистых parser фильтров в исходном порядке и возвращает новый типизированный запрос только с фильтрами после успеха всех проверок. Отдельные строкоподобные параметры сохраняют generated-типы project/organization без dispatcher; область, страница и поиск остаются у вызывающего, при ошибке частичный запрос не выдаётся.
func artifactCatalogFilters[L, T, S, R, G ~string](lifecycle *L, artifactType *T, scan *S, source *R, sources *[]G) (*controlplanev1.ListArtifactsRequest, bool) {
	lifecycleState, lifecycleOK := artifactLifecycleFilter(lifecycle)
	typeValue, typeOK := artifactTypeFilter(artifactType)
	scanState, scanOK := artifactScanStateFilter(scan)
	sourceKind, sourceOK := artifactSourceFilter(source)
	sourceKinds, sourcesOK := artifactSourceGroupFilter(sources, sourceKind)
	if !lifecycleOK || !typeOK || !scanOK || !sourceOK || !sourcesOK {
		return nil, false
	}
	return &controlplanev1.ListArtifactsRequest{
		LifecycleState: lifecycleState, Type: typeValue, ScanState: scanState,
		SourceKind: sourceKind, SourceKinds: sourceKinds,
	}, true
}

// artifactLifecycleFilter returns unspecified for an absent selector and otherwise performs exact prefixed enum lookup, including an explicitly named unspecified value if present in the registry.
// artifactLifecycleFilter возвращает unspecified при отсутствии селектора, иначе точно ищет enum с префиксом, включая явно названный unspecified при его наличии в реестре.
func artifactLifecycleFilter[T ~string](value *T) (controlplanev1.ArtifactLifecycleState, bool) {
	if value == nil {
		return controlplanev1.ArtifactLifecycleState_ARTIFACT_LIFECYCLE_STATE_UNSPECIFIED, true
	}
	raw, ok := controlplanev1.ArtifactLifecycleState_value["ARTIFACT_LIFECYCLE_STATE_"+string(*value)]
	return controlplanev1.ArtifactLifecycleState(raw), ok
}

// artifactTypeFilter distinguishes an absent type from an invalid string and maps known prefixed names without inferring type from file names or content.
// artifactTypeFilter отличает отсутствующий тип от неверной строки и преобразует известные имена с префиксом без вывода типа из имени и содержимого файла.
func artifactTypeFilter[T ~string](value *T) (controlplanev1.ArtifactType, bool) {
	if value == nil {
		return controlplanev1.ArtifactType_ARTIFACT_TYPE_UNSPECIFIED, true
	}
	raw, ok := controlplanev1.ArtifactType_value["ARTIFACT_TYPE_"+string(*value)]
	return controlplanev1.ArtifactType(raw), ok
}

// artifactScanStateFilter maps only known scan-state names, defaulting absence to unspecified; selecting a state does not mark an attachment scanned or clean.
// artifactScanStateFilter преобразует только известные состояния проверки, задавая unspecified при отсутствии; выбор состояния не помечает вложение проверенным или чистым.
func artifactScanStateFilter[T ~string](value *T) (controlplanev1.ArtifactScanState, bool) {
	if value == nil {
		return controlplanev1.ArtifactScanState_ARTIFACT_SCAN_STATE_UNSPECIFIED, true
	}
	raw, ok := controlplanev1.ArtifactScanState_value["ARTIFACT_SCAN_STATE_"+string(*value)]
	return controlplanev1.ArtifactScanState(raw), ok
}

// artifactSourceFilter defaults absence to unspecified but rejects explicit unknown or zero-valued source names, preserving the source-filter contract.
// artifactSourceFilter задаёт unspecified при отсутствии, но отклоняет явно неизвестные и нулевые source, сохраняя контракт фильтра.
func artifactSourceFilter[T ~string](value *T) (controlplanev1.ArtifactSource, bool) {
	if value == nil {
		return controlplanev1.ArtifactSource_ARTIFACT_SOURCE_UNSPECIFIED, true
	}
	raw, ok := controlplanev1.ArtifactSource_value["ARTIFACT_SOURCE_"+string(*value)]
	return controlplanev1.ArtifactSource(raw), ok && raw != 0
}

// artifactSourceGroupFilter accepts absent/empty groups as no list, otherwise requires at most five unique nonzero sources and no single-source selector, retaining input order in a fresh slice.
// artifactSourceGroupFilter принимает отсутствующую и пустую группу как отсутствие списка, иначе требует не более пяти уникальных ненулевых источников без одиночного селектора, сохраняя порядок в новом slice.
func artifactSourceGroupFilter[T ~string](values *[]T, single controlplanev1.ArtifactSource) ([]controlplanev1.ArtifactSource, bool) {
	if values == nil || len(*values) == 0 {
		return nil, true
	}
	if len(*values) > 5 || single != controlplanev1.ArtifactSource_ARTIFACT_SOURCE_UNSPECIFIED {
		return nil, false
	}
	result := make([]controlplanev1.ArtifactSource, 0, len(*values))
	seen := make(map[controlplanev1.ArtifactSource]bool, len(*values))
	for _, value := range *values {
		kind, ok := artifactSourceFilter(&value)
		if !ok || seen[kind] {
			return nil, false
		}
		seen[kind] = true
		result = append(result, kind)
	}
	return result, true
}

// GetArtifact reads owner-visible artifact metadata through the common projection without downloading bytes, removing quarantine or bypassing scan policy.
// GetArtifact читает видимую metadata файла общей проекцией без загрузки байтов, снятия карантина и обхода политики проверки.
func (server *Server) GetArtifact(w http.ResponseWriter, r *http.Request, ref generated.ArtifactRef) {
	response, err := server.control.Query.GetArtifact(r.Context(), &controlplanev1.GetArtifactRequest{ArtifactRef: ref})
	writeReadRPCResult(w, response, err, "artifact", "")
}

// ListArtifactRevisions forwards the exact artifact selector and page to the owner and reshapes revision metadata into items without selecting a newer revision implicitly.
// ListArtifactRevisions передаёт точный файл и страницу владельцу и выдаёт metadata ревизий в items без неявного выбора более новой ревизии.
func (server *Server) ListArtifactRevisions(w http.ResponseWriter, r *http.Request, ref generated.ArtifactRef, p generated.ListArtifactRevisionsParams) {
	response, err := server.control.Query.ListArtifactRevisions(r.Context(), &controlplanev1.ListArtifactRevisionsRequest{ArtifactRef: ref, Page: page(p.PageSize, p.PageToken)})
	writeReadRPCResult(w, response, err, "", "items")
}

// GetArtifactRevision resolves the exact artifact/revision pair at the owner and exposes only the public revision projection, not raw storage data.
// GetArtifactRevision разрешает точную пару файла и ревизии у владельца и выдаёт только публичную проекцию ревизии, а не сырые данные хранилища.
func (server *Server) GetArtifactRevision(w http.ResponseWriter, r *http.Request, ref generated.ArtifactRef, revisionRef string) {
	response, err := server.control.Query.GetArtifactRevision(r.Context(), &controlplanev1.GetArtifactRevisionRequest{ArtifactRef: ref, RevisionRef: revisionRef})
	writeReadRPCResult(w, response, err, "revision", "")
}

// ListSchedules binds project context and forwards query/page to the owner's schedule catalog without activating timers or materializing executions.
// ListSchedules привязывает проект и передаёт поиск и страницу каталогу автоматизаций владельца без активации таймеров и создания запусков.
func (server *Server) ListSchedules(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.ListSchedulesParams) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	response, err := server.control.Query.ListSchedules(r.Context(), &controlplanev1.ListSchedulesRequest{
		ProjectRef: ref, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
	})
	writeReadRPCResult(w, response, err, "", "schedules")
}

// PreviewSchedule decodes a required preview, validates optional limit1..100 and prompt context, converts optional after time, then validates the owner preview against its request before writing JSON; it does not persist a schedule or run.
// PreviewSchedule разбирает обязательный preview, проверяет необязательный limit1..100 и prompt context, преобразует after, затем сверяет preview владельца с запросом до JSON-выдачи; автоматизация и запуск не сохраняются.
func (server *Server) PreviewSchedule(w http.ResponseWriter, r *http.Request, _ generated.PreviewScheduleParams) {
	body, ok := decodeJSON[generated.SchedulePreviewInput](w, r)
	if !ok {
		return
	}
	limit := int32(10)
	if body.Limit != nil {
		if *body.Limit < 1 || *body.Limit > 100 {
			writeLocalProblem(w, 400, "INVALID_REQUEST", false)
			return
		}
		limit = int32(*body.Limit)
	}
	materialization, valid := schedulePromptContext(body.Materialization)
	if !valid {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	request := &controlplanev1.PreviewScheduleRequest{
		Preset: string(body.Preset), CronExpression: stringValue(body.CronExpression), TimeOfDay: stringValue(body.TimeOfDay),
		DayOfWeek: stringValue(body.DayOfWeek), Timezone: body.Timezone, DstGapPolicy: string(body.DstGapPolicy),
		DstFoldPolicy: string(body.DstFoldPolicy), MisfirePolicy: string(body.MisfirePolicy),
		OverlapPolicy: string(body.OverlapPolicy), Limit: limit, Materialization: materialization,
	}
	if body.After != nil {
		request.After = timestamppb.New(*body.After)
	}
	response, err := server.control.Query.PreviewSchedule(r.Context(), request)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	output, valid := schedulePromptPreviewView(w, request, response)
	if !valid {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, http.StatusOK, output)
}

// GetSchedule reads one selected schedule through owner policy and projects it without changing schedule state or acquiring an occurrence lease.
// GetSchedule читает выбранную автоматизацию по политике владельца и проецирует её без смены состояния и получения occurrence lease.
func (server *Server) GetSchedule(w http.ResponseWriter, r *http.Request, ref generated.ScheduleRef) {
	response, err := server.control.Query.GetSchedule(r.Context(), &controlplanev1.GetScheduleRequest{ScheduleRef: ref})
	writeReadRPCResult(w, response, err, "schedule", "")
}

// ListIntegrationDefinitions forwards category, search and page to the owner and exposes public definitions without discovering remote tools or granting integration access.
// ListIntegrationDefinitions передаёт категорию, поиск и страницу владельцу и выдаёт публичные определения без поиска удалённых инструментов и выдачи доступа.
func (server *Server) ListIntegrationDefinitions(w http.ResponseWriter, r *http.Request, p generated.ListIntegrationDefinitionsParams) {
	response, err := server.control.Query.ListIntegrationDefinitions(r.Context(), &controlplanev1.ListIntegrationDefinitionsRequest{
		Category: stringValue(p.Category), Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
	})
	writeReadRPCResult(w, response, err, "", "definitions")
}

// ListIntegrationConnections validates a present definition key's text bound, then reads owner-filtered connection metadata and pagination without revealing credentials or calling a tool.
// ListIntegrationConnections проверяет текстовый предел заданного definition key, затем читает metadata подключений и пагинацию владельца без раскрытия credentials и вызова инструмента.
func (server *Server) ListIntegrationConnections(w http.ResponseWriter, r *http.Request, p generated.ListIntegrationConnectionsParams) {
	if p.DefinitionKey != nil && !validSearchText(*p.DefinitionKey, 1, 80) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListIntegrationConnections(r.Context(), &controlplanev1.ListIntegrationConnectionsRequest{
		Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query), DefinitionKey: stringValue(p.DefinitionKey),
	})
	writeReadRPCResult(w, response, err, "", "connections")
}

// GetIntegrationConnection resolves the selected connection with owner policy and emits public metadata rather than secret material or executable grants.
// GetIntegrationConnection разрешает выбранное подключение по политике владельца и выдаёт публичную metadata, а не секреты и исполняемые grants.
func (server *Server) GetIntegrationConnection(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef) {
	response, err := server.control.Query.GetIntegrationConnection(r.Context(), &controlplanev1.GetIntegrationConnectionRequest{ConnectionRef: ref})
	writeReadRPCResult(w, response, err, "connection", "")
}

// GetAdministration reads the owner's administration view and public-projects its state; this endpoint does not grant administrative privileges.
// GetAdministration читает административное представление владельца и публично проецирует состояние; endpoint административные права не выдаёт.
func (server *Server) GetAdministration(w http.ResponseWriter, r *http.Request) {
	response, err := server.control.Query.GetAdministration(r.Context(), &controlplanev1.GetAdministrationRequest{})
	writeReadRPCResult(w, response, err, "state", "")
}

// ListAuditEvents validates bounded action/outcome filters, forwards scoped search/pagination and inverts an explicitly false includeTechnical into excludeTechnical before projecting the audit page.
// ListAuditEvents проверяет пределы action/outcome, передаёт поиск и пагинацию области и превращает явно false includeTechnical в excludeTechnical до проекции аудита.
func (server *Server) ListAuditEvents(w http.ResponseWriter, r *http.Request, p generated.ListAuditEventsParams) {
	if p.Action != nil && !validSearchText(*p.Action, 1, 160) || p.Outcome != nil && !validSearchText(*p.Outcome, 1, 80) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListAuditEvents(r.Context(), &controlplanev1.ListAuditEventsRequest{ProjectRef: stringValue(p.ProjectRef), ResourceRef: stringValue(p.ResourceRef), Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query), Action: stringValue(p.Action), Outcome: stringValue(p.Outcome), ExcludeTechnical: p.IncludeTechnical != nil && !*p.IncludeTechnical})
	writeReadRPCResult(w, response, err, "", "events")
}

// writeJSON sets no-store JSON headers and the supplied status before encoding a value; an encoding/write failure is ignored after commitment, so it is not a delivery acknowledgement.
// writeJSON задаёт no-store JSON-заголовки и переданный статус до encoding значения; ошибка после записи статуса игнорируется, поэтому подтверждения доставки нет.
func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(statusCode)
	_ = jsonEncoder(w).Encode(value)
}
