package httptransport

import (
	"net/http"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// catalogRequest checks UTF-8 query/token, query length up to two hundred runes, page size1..100 and token bytes up to512, optionally deriving project context. It preserves an absent selector and does not enforce catalog eligibility or infer an organization from caller input.
// catalogRequest проверяет UTF-8 запроса и token, длину запроса до двухсот рун, размер страницы1..100 и token до512 байт, при наличии выводя контекст проекта. Отсутствующий селектор сохраняется; допуск каталога и организация вызывающего здесь не назначаются.
func catalogRequest(w http.ResponseWriter, r *http.Request, projectRef, query *string, pageSize *int, pageToken *string) (*http.Request, bool) {
	if query != nil && (!utf8.ValidString(*query) || utf8.RuneCountInString(*query) > 200) ||
		pageSize != nil && (*pageSize < 1 || *pageSize > 100) ||
		pageToken != nil && (len(*pageToken) > 512 || !utf8.ValidString(*pageToken)) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	if projectRef != nil {
		return withProjectReference(w, r, *projectRef)
	}
	return r, true
}

// catalogStateFilter returns zero for an absent filter, otherwise requires a known nonzero enum after prefixing the exact input; it does not apply transitions or validate arbitrary enum registries.
// catalogStateFilter возвращает ноль при отсутствии фильтра, иначе требует известный ненулевой enum после добавления префикса к точному вводу; переходы не выполняет и произвольные реестры enum не проверяет.
func catalogStateFilter[T ~string](state *T, values map[string]int32, prefix string) (int32, bool) {
	if state == nil {
		return 0, true
	}
	value, ok := values[prefix+string(*state)]
	return value, ok && value != 0
}

// ListOrganizationProjectMemberships validates catalog selectors and reads the owner-filtered membership page, forwarding an optional project without organization fan-out or assigning membership.
// ListOrganizationProjectMemberships проверяет селекторы каталога и читает отфильтрованную владельцем страницу членства, передавая необязательный проект без перебора организаций и назначения членства.
func (server *Server) ListOrganizationProjectMemberships(w http.ResponseWriter, r *http.Request, p generated.ListOrganizationProjectMembershipsParams) {
	r, ok := catalogRequest(w, r, p.ProjectRef, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	response, err := server.control.Query.ListProjectMemberships(r.Context(), &controlplanev1.ListProjectMembershipsRequest{
		ProjectRef: stringValue(p.ProjectRef), Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
	})
	writeReadRPCResult(w, response, err, "", "memberships")
}

// ListOrganizationAgents validates catalog and agent-state selectors, then delegates cross-project eligibility to the owner and projects public metadata without launching agents.
// ListOrganizationAgents проверяет селекторы каталога и состояния сотрудника, затем поручает межпроектный допуск владельцу и проецирует metadata без запуска сотрудников.
func (server *Server) ListOrganizationAgents(w http.ResponseWriter, r *http.Request, p generated.ListOrganizationAgentsParams) {
	r, ok := catalogRequest(w, r, p.ProjectRef, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	state, stateOK := catalogStateFilter(p.State, controlplanev1.AgentState_value, "AGENT_STATE_")
	if !stateOK {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListAgents(r.Context(), &controlplanev1.ListAgentsRequest{
		ProjectRef: stringValue(p.ProjectRef), State: controlplanev1.AgentState(state), Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
	})
	writeReadRPCResult(w, response, err, "", "agents")
}

// ListOrganizationWorkflows validates catalog and nonzero workflow state, forwarding the optional project/query/page to one owner query without executing processes.
// ListOrganizationWorkflows проверяет каталог и ненулевое состояние процесса и передаёт необязательные проект, поиск и страницу одному запросу владельцу без выполнения процессов.
func (server *Server) ListOrganizationWorkflows(w http.ResponseWriter, r *http.Request, p generated.ListOrganizationWorkflowsParams) {
	r, ok := catalogRequest(w, r, p.ProjectRef, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	state, stateOK := catalogStateFilter(p.State, controlplanev1.WorkflowState_value, "WORKFLOW_STATE_")
	if !stateOK {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListWorkflows(r.Context(), &controlplanev1.ListWorkflowsRequest{
		ProjectRef: stringValue(p.ProjectRef), State: controlplanev1.WorkflowState(state), Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
	})
	writeReadRPCResult(w, response, err, "", "workflows")
}

// ListOrganizationSchedules validates scoped catalog input and exposes the owner's schedules page without creating timers, occurrence claims or executions.
// ListOrganizationSchedules проверяет вход каталога области и выдаёт страницу автоматизаций владельца без создания таймеров, occurrence claims и запусков.
func (server *Server) ListOrganizationSchedules(w http.ResponseWriter, r *http.Request, p generated.ListOrganizationSchedulesParams) {
	r, ok := catalogRequest(w, r, p.ProjectRef, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	response, err := server.control.Query.ListSchedules(r.Context(), &controlplanev1.ListSchedulesRequest{
		ProjectRef: stringValue(p.ProjectRef), Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
	})
	writeReadRPCResult(w, response, err, "", "schedules")
}

// ListOrganizationRuntimeEnvironmentSets passes validated optional project/query/page to the owner environment catalog; visible metadata is not runtime admission or a new scope grant.
// ListOrganizationRuntimeEnvironmentSets передаёт проверенные необязательные проект, поиск и страницу каталогу окружений владельца; видимая metadata не является runtime-допуском или grant области.
func (server *Server) ListOrganizationRuntimeEnvironmentSets(w http.ResponseWriter, r *http.Request, p generated.ListOrganizationRuntimeEnvironmentSetsParams) {
	r, ok := catalogRequest(w, r, p.ProjectRef, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	response, err := server.control.Query.ListRuntimeEnvironmentSets(r.Context(), &controlplanev1.ListRuntimeEnvironmentSetsRequest{
		ProjectRef: stringValue(p.ProjectRef), Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
	})
	writeReadRPCResult(w, response, err, "", "environments")
}

// ListOrganizationRuntimeSecrets reads owner-filtered secret metadata using validated catalog selectors and the specialized secret-page writer, never a reveal RPC or raw secret value.
// ListOrganizationRuntimeSecrets читает отфильтрованную владельцем metadata секретов по проверенным селекторам через специальный writer страницы, а не reveal RPC и не сырые значения.
func (server *Server) ListOrganizationRuntimeSecrets(w http.ResponseWriter, r *http.Request, p generated.ListOrganizationRuntimeSecretsParams) {
	r, ok := catalogRequest(w, r, p.ProjectRef, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	response, err := server.control.Query.ListRuntimeSecrets(r.Context(), &controlplanev1.ListRuntimeSecretsRequest{
		ProjectRef: stringValue(p.ProjectRef), Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeRuntimeSecretPage(w, response)
}
