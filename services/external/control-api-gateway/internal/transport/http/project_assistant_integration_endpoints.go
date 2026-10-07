package httptransport

import (
	"net/http"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (s *Server) GetProjectAssistantIntegrationGrantCandidates(w http.ResponseWriter, r *http.Request, projectRef string, p generated.GetProjectAssistantIntegrationGrantCandidatesParams) {
	response, err := s.control.Query.GetProjectAssistantIntegrationGrantCandidates(r.Context(), &cp.GetProjectAssistantIntegrationGrantCandidatesRequest{
		ProjectRef: projectRef, ConnectionRef: p.ConnectionRef, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	view, ok := projectAssistantGrantCandidatePage(response, projectRef, p.ConnectionRef, int(page(p.PageSize, p.PageToken).PageSize))
	if !ok {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", true)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func projectAssistantGrantCandidatePage(v *cp.GetProjectAssistantIntegrationGrantCandidatesResponse, projectRef, connectionRef string, limit int) (map[string]any, bool) {
	if v == nil || len(v.ProtoReflect().GetUnknown()) != 0 || v.ProjectRef != projectRef || !fileTargetRef(v.ProjectRef) || !fileTargetRef(v.AssistantProfileRef) || v.ProfileVersion < 1 || v.ProfileVersion > maximumSafeJSONInteger {
		return nil, false
	}
	view, ok := systemAssistantGrantCandidatePage(&cp.GetSystemAssistantIntegrationGrantCandidatesResponse{ScopeKind: v.ScopeKind, OrganizationRef: v.OrganizationRef,
		AssistantRef: v.AssistantRef, AssistantVersion: v.AssistantVersion, ConnectionRef: v.ConnectionRef, ConnectionVersion: v.ConnectionVersion,
		DefinitionVersion: v.DefinitionVersion, DefinitionDigest: v.DefinitionDigest, Items: v.Items, Total: v.Total, Page: v.Page}, connectionRef, limit)
	if !ok {
		return nil, false
	}
	view["projectRef"], view["assistantProfileRef"], view["profileVersion"] = v.ProjectRef, v.AssistantProfileRef, v.ProfileVersion
	return view, true
}
