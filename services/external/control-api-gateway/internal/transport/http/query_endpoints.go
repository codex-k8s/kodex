package httptransport

import (
	"fmt"
	"net/http"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/boundary"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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
func (server *Server) ListProjectMemberships(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.ListProjectMembershipsParams) {
	r, ok := catalogRequest(w, r, &ref, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	response, err := server.control.Query.ListProjectMemberships(r.Context(), &controlplanev1.ListProjectMembershipsRequest{ProjectRef: ref, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken)})
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
	r, ok := catalogRequest(w, r, p.ProjectRef, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	if !validSearchText(stringValue(p.Query), 0, 200) {
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
	response, err := server.control.Query.ListRuns(r.Context(), &controlplanev1.ListRunsRequest{ProjectRef: stringValue(p.ProjectRef), Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query), States: states})
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
	lifecycleState, lifecycleStateOK := artifactLifecycleFilter(p.LifecycleState)
	artifactType, artifactTypeOK := artifactTypeFilter(p.Type)
	scanState, scanStateOK := artifactScanStateFilter(p.ScanState)
	sourceKind, sourceKindOK := artifactSourceFilter(p.SourceKind)
	if !lifecycleStateOK || !artifactTypeOK || !scanStateOK || !sourceKindOK {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListArtifacts(r.Context(), &controlplanev1.ListArtifactsRequest{
		ProjectRef: ref, RunRef: stringValue(p.RunRef), Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query),
		LifecycleState: lifecycleState, Type: artifactType, ScanState: scanState, SourceKind: sourceKind,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "artifacts")
}

func (server *Server) ListOrganizationArtifacts(w http.ResponseWriter, r *http.Request, p generated.ListOrganizationArtifactsParams) {
	lifecycleState, lifecycleStateOK := artifactLifecycleFilter(p.LifecycleState)
	artifactType, artifactTypeOK := artifactTypeFilter(p.Type)
	scanState, scanStateOK := artifactScanStateFilter(p.ScanState)
	sourceKind, sourceKindOK := artifactSourceFilter(p.SourceKind)
	if !lifecycleStateOK || !artifactTypeOK || !scanStateOK || !sourceKindOK {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListArtifacts(r.Context(), &controlplanev1.ListArtifactsRequest{
		Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query),
		LifecycleState: lifecycleState, Type: artifactType, ScanState: scanState, SourceKind: sourceKind,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "artifacts")
}

func artifactLifecycleFilter[T ~string](value *T) (controlplanev1.ArtifactLifecycleState, bool) {
	if value == nil {
		return controlplanev1.ArtifactLifecycleState_ARTIFACT_LIFECYCLE_STATE_UNSPECIFIED, true
	}
	raw, ok := controlplanev1.ArtifactLifecycleState_value["ARTIFACT_LIFECYCLE_STATE_"+string(*value)]
	return controlplanev1.ArtifactLifecycleState(raw), ok
}

func artifactTypeFilter[T ~string](value *T) (controlplanev1.ArtifactType, bool) {
	if value == nil {
		return controlplanev1.ArtifactType_ARTIFACT_TYPE_UNSPECIFIED, true
	}
	raw, ok := controlplanev1.ArtifactType_value["ARTIFACT_TYPE_"+string(*value)]
	return controlplanev1.ArtifactType(raw), ok
}

func artifactScanStateFilter[T ~string](value *T) (controlplanev1.ArtifactScanState, bool) {
	if value == nil {
		return controlplanev1.ArtifactScanState_ARTIFACT_SCAN_STATE_UNSPECIFIED, true
	}
	raw, ok := controlplanev1.ArtifactScanState_value["ARTIFACT_SCAN_STATE_"+string(*value)]
	return controlplanev1.ArtifactScanState(raw), ok
}

func artifactSourceFilter[T ~string](value *T) (controlplanev1.ArtifactSource, bool) {
	if value == nil {
		return controlplanev1.ArtifactSource_ARTIFACT_SOURCE_UNSPECIFIED, true
	}
	raw, ok := controlplanev1.ArtifactSource_value["ARTIFACT_SOURCE_"+string(*value)]
	return controlplanev1.ArtifactSource(raw), ok
}
func (server *Server) GetArtifact(w http.ResponseWriter, r *http.Request, ref generated.ArtifactRef) {
	response, err := server.control.Query.GetArtifact(r.Context(), &controlplanev1.GetArtifactRequest{ArtifactRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "artifact", "")
}
func (server *Server) ListSchedules(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.ListSchedulesParams) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	response, err := server.control.Query.ListSchedules(r.Context(), &controlplanev1.ListSchedulesRequest{
		ProjectRef: ref, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "schedules")
}
func (server *Server) PreviewSchedule(w http.ResponseWriter, r *http.Request, _ generated.PreviewScheduleParams) {
	body, ok := decodeJSON[generated.SchedulePreviewInput](w, r)
	if !ok {
		return
	}
	limit := int32(10)
	if body.Limit != nil {
		limit = int32(*body.Limit)
	}
	request := &controlplanev1.PreviewScheduleRequest{
		Preset: string(body.Preset), CronExpression: stringValue(body.CronExpression), TimeOfDay: stringValue(body.TimeOfDay),
		DayOfWeek: stringValue(body.DayOfWeek), Timezone: body.Timezone, DstGapPolicy: string(body.DstGapPolicy),
		DstFoldPolicy: string(body.DstFoldPolicy), MisfirePolicy: string(body.MisfirePolicy),
		OverlapPolicy: string(body.OverlapPolicy), Limit: limit,
	}
	if body.After != nil {
		request.After = timestamppb.New(*body.After)
	}
	response, err := server.control.Query.PreviewSchedule(r.Context(), request)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "")
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
	response, err := server.control.Query.ListIntegrationDefinitions(r.Context(), &controlplanev1.ListIntegrationDefinitionsRequest{
		Category: stringValue(p.Category), Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "definitions")
}
func (server *Server) ListIntegrationConnections(w http.ResponseWriter, r *http.Request, p generated.ListIntegrationConnectionsParams) {
	response, err := server.control.Query.ListIntegrationConnections(r.Context(), &controlplanev1.ListIntegrationConnectionsRequest{
		Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query),
	})
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
