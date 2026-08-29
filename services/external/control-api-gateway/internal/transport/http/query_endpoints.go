package httptransport

import (
	"net/http"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) GetBootstrapState(w http.ResponseWriter, r *http.Request) {
	response, err := server.control.Query.GetBootstrapState(r.Context(), &controlplanev1.GetBootstrapStateRequest{})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "state", "")
}
func (server *Server) GetOverview(w http.ResponseWriter, r *http.Request, p generated.GetOverviewParams) {
	response, err := server.control.Query.GetOverview(r.Context(), &controlplanev1.GetOverviewRequest{ProjectRef: stringValue(p.ProjectRef)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "overview", "")
}
func (server *Server) ListPlatformCapabilities(w http.ResponseWriter, r *http.Request) {
	response, err := server.control.Query.ListPlatformCapabilities(r.Context(), &controlplanev1.ListPlatformCapabilitiesRequest{})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "capabilities")
}
func (server *Server) ListRuntimeSelections(w http.ResponseWriter, r *http.Request) {
	response, err := server.control.Query.ListRuntimeSelections(r.Context(), &controlplanev1.ListRuntimeSelectionsRequest{})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "runtimes")
}
func (server *Server) SearchPlatform(w http.ResponseWriter, r *http.Request, p generated.SearchPlatformParams) {
	limit := int32(20)
	if p.Limit != nil {
		limit = int32(*p.Limit)
	}
	response, err := server.control.Query.SearchPlatform(r.Context(), &controlplanev1.SearchPlatformRequest{Query: p.Query, Limit: limit})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "results")
}
func (server *Server) ListProjects(w http.ResponseWriter, r *http.Request, p generated.ListProjectsParams) {
	response, err := server.control.Query.ListProjects(r.Context(), &controlplanev1.ListProjectsRequest{Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "projects")
}
func (server *Server) GetProject(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	response, err := server.control.Query.GetProject(r.Context(), &controlplanev1.GetProjectRequest{ProjectRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "project", "")
}
func (server *Server) ListPlatformMemberships(w http.ResponseWriter, r *http.Request, p generated.ListPlatformMembershipsParams) {
	response, err := server.control.Query.ListPlatformMemberships(r.Context(), &controlplanev1.ListPlatformMembershipsRequest{Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "memberships")
}
func (server *Server) ListPlatformMembershipCandidates(w http.ResponseWriter, r *http.Request, p generated.ListPlatformMembershipCandidatesParams) {
	response, err := server.control.Query.ListPlatformMembershipCandidates(r.Context(), &controlplanev1.ListPlatformMembershipCandidatesRequest{Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "users")
}
func (server *Server) ListProjectMemberships(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	response, err := server.control.Query.ListProjectMemberships(r.Context(), &controlplanev1.ListProjectMembershipsRequest{ProjectRef: ref, Page: page(nil, nil)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "memberships")
}
func (server *Server) ListProjectMembershipCandidates(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.ListProjectMembershipCandidatesParams) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	response, err := server.control.Query.ListProjectMembershipCandidates(r.Context(), &controlplanev1.ListProjectMembershipCandidatesRequest{ProjectRef: ref, Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "users")
}
func (server *Server) ListAgents(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.ListAgentsParams) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	response, err := server.control.Query.ListAgents(r.Context(), &controlplanev1.ListAgentsRequest{ProjectRef: ref, Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "agents")
}
func (server *Server) GetAgent(w http.ResponseWriter, r *http.Request, ref generated.AgentRef) {
	response, err := server.control.Query.GetAgent(r.Context(), &controlplanev1.GetAgentRequest{AgentRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "agent", "")
}
func (server *Server) ListAgentInstructionVersions(w http.ResponseWriter, r *http.Request, ref generated.AgentRef, p generated.ListAgentInstructionVersionsParams) {
	response, err := server.control.Query.ListAgentInstructionVersions(r.Context(), &controlplanev1.ListAgentInstructionVersionsRequest{AgentRef: ref, Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "instructionVersions")
}
func (server *Server) ListWorkflows(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.ListWorkflowsParams) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	response, err := server.control.Query.ListWorkflows(r.Context(), &controlplanev1.ListWorkflowsRequest{ProjectRef: ref, Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "workflows")
}
func (server *Server) GetWorkflow(w http.ResponseWriter, r *http.Request, ref generated.WorkflowRef) {
	response, err := server.control.Query.GetWorkflow(r.Context(), &controlplanev1.GetWorkflowRequest{WorkflowRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "workflow", "")
}
func (server *Server) ListRuns(w http.ResponseWriter, r *http.Request, p generated.ListRunsParams) {
	response, err := server.control.Query.ListRuns(r.Context(), &controlplanev1.ListRunsRequest{ProjectRef: stringValue(p.ProjectRef), Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "runs")
}
func (server *Server) GetRun(w http.ResponseWriter, r *http.Request, ref generated.RunRef) {
	response, err := server.control.Query.GetRun(r.Context(), &controlplanev1.GetRunRequest{RunRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "run", "")
}
func (server *Server) GetRunGraph(w http.ResponseWriter, r *http.Request, ref generated.RunRef) {
	response, err := server.control.Query.GetRunGraph(r.Context(), &controlplanev1.GetRunGraphRequest{RunRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "")
}
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
func (server *Server) ListOwnerGates(w http.ResponseWriter, r *http.Request, p generated.ListOwnerGatesParams) {
	response, err := server.control.Query.ListOwnerGates(r.Context(), &controlplanev1.ListOwnerGatesRequest{ProjectRef: stringValue(p.ProjectRef), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "gates")
}
func (server *Server) GetOwnerGate(w http.ResponseWriter, r *http.Request, ref generated.GateRef) {
	response, err := server.control.Query.GetOwnerGate(r.Context(), &controlplanev1.GetOwnerGateRequest{GateRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "gate", "")
}
func (server *Server) ListArtifacts(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.ListArtifactsParams) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	lifecycleState := controlplanev1.ArtifactLifecycleState_ARTIFACT_LIFECYCLE_STATE_UNSPECIFIED
	if p.LifecycleState != nil {
		lifecycleState = controlplanev1.ArtifactLifecycleState(controlplanev1.ArtifactLifecycleState_value["ARTIFACT_LIFECYCLE_STATE_"+string(*p.LifecycleState)])
	}
	response, err := server.control.Query.ListArtifacts(r.Context(), &controlplanev1.ListArtifactsRequest{ProjectRef: ref, RunRef: stringValue(p.RunRef), LifecycleState: lifecycleState, Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "artifacts")
}
func (server *Server) GetArtifact(w http.ResponseWriter, r *http.Request, ref generated.ArtifactRef) {
	response, err := server.control.Query.GetArtifact(r.Context(), &controlplanev1.GetArtifactRequest{ArtifactRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "artifact", "")
}
func (server *Server) ListSchedules(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	response, err := server.control.Query.ListSchedules(r.Context(), &controlplanev1.ListSchedulesRequest{ProjectRef: ref, Page: page(nil, nil)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "schedules")
}
func (server *Server) GetSchedule(w http.ResponseWriter, r *http.Request, ref generated.ScheduleRef) {
	response, err := server.control.Query.GetSchedule(r.Context(), &controlplanev1.GetScheduleRequest{ScheduleRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "schedule", "")
}
func (server *Server) ListIntegrationDefinitions(w http.ResponseWriter, r *http.Request, p generated.ListIntegrationDefinitionsParams) {
	response, err := server.control.Query.ListIntegrationDefinitions(r.Context(), &controlplanev1.ListIntegrationDefinitionsRequest{Category: stringValue(p.Category)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "definitions")
}
func (server *Server) ListIntegrationConnections(w http.ResponseWriter, r *http.Request) {
	response, err := server.control.Query.ListIntegrationConnections(r.Context(), &controlplanev1.ListIntegrationConnectionsRequest{Page: page(nil, nil)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "connections")
}
func (server *Server) GetIntegrationConnection(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef) {
	response, err := server.control.Query.GetIntegrationConnection(r.Context(), &controlplanev1.GetIntegrationConnectionRequest{ConnectionRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "connection", "")
}
func (server *Server) GetAdministration(w http.ResponseWriter, r *http.Request) {
	response, err := server.control.Query.GetAdministration(r.Context(), &controlplanev1.GetAdministrationRequest{})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "state", "")
}
func (server *Server) ListAuditEvents(w http.ResponseWriter, r *http.Request, p generated.ListAuditEventsParams) {
	response, err := server.control.Query.ListAuditEvents(r.Context(), &controlplanev1.ListAuditEventsRequest{ProjectRef: stringValue(p.ProjectRef), Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "events")
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(statusCode)
	_ = jsonEncoder(w).Encode(value)
}
