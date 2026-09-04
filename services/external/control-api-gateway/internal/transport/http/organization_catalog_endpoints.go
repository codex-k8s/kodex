package httptransport

import (
	"net/http"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

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

func (server *Server) ListOrganizationAgents(w http.ResponseWriter, r *http.Request, p generated.ListOrganizationAgentsParams) {
	r, ok := catalogRequest(w, r, p.ProjectRef, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	response, err := server.control.Query.ListAgents(r.Context(), &controlplanev1.ListAgentsRequest{
		ProjectRef: stringValue(p.ProjectRef), Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "agents")
}

func (server *Server) ListOrganizationWorkflows(w http.ResponseWriter, r *http.Request, p generated.ListOrganizationWorkflowsParams) {
	r, ok := catalogRequest(w, r, p.ProjectRef, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	response, err := server.control.Query.ListWorkflows(r.Context(), &controlplanev1.ListWorkflowsRequest{
		ProjectRef: stringValue(p.ProjectRef), Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "workflows")
}

func (server *Server) ListOrganizationSchedules(w http.ResponseWriter, r *http.Request, p generated.ListOrganizationSchedulesParams) {
	r, ok := catalogRequest(w, r, p.ProjectRef, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	response, err := server.control.Query.ListSchedules(r.Context(), &controlplanev1.ListSchedulesRequest{
		ProjectRef: stringValue(p.ProjectRef), Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "schedules")
}

func (server *Server) ListOrganizationRuntimeEnvironmentSets(w http.ResponseWriter, r *http.Request, p generated.ListOrganizationRuntimeEnvironmentSetsParams) {
	r, ok := catalogRequest(w, r, p.ProjectRef, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	response, err := server.control.Query.ListRuntimeEnvironmentSets(r.Context(), &controlplanev1.ListRuntimeEnvironmentSetsRequest{
		ProjectRef: stringValue(p.ProjectRef), Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "environments")
}

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
