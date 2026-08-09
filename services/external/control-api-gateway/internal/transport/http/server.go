package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/grpcserver"
	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/projection"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/security/boundary"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	sessionPath         = "/api/v1/session"
	maximumBodyBytes    = 256 << 10
	maximumPageSize     = 100
	defaultPageSize     = 50
	sessionCookieMaxAge = 3600
	maximumAuditScan    = 500
)

var (
	errAuditScanLimit  = errors.New("audit projection scan limit exceeded")
	errAuditPagination = errors.New("audit pagination did not advance")
)

type ControlPlane interface {
	CreateProject(context.Context, *controlplanev1.CreateProjectRequest, ...grpc.CallOption) (*controlplanev1.CreateProjectResponse, error)
	ListProjects(context.Context, *controlplanev1.ListProjectsRequest, ...grpc.CallOption) (*controlplanev1.ListProjectsResponse, error)
	UpdateProject(context.Context, *controlplanev1.UpdateProjectRequest, ...grpc.CallOption) (*controlplanev1.UpdateProjectResponse, error)
	DeleteProject(context.Context, *controlplanev1.DeleteProjectRequest, ...grpc.CallOption) (*controlplanev1.DeleteProjectResponse, error)
	CreateResource(context.Context, *controlplanev1.CreateResourceRequest, ...grpc.CallOption) (*controlplanev1.CreateResourceResponse, error)
	UpdateResource(context.Context, *controlplanev1.UpdateResourceRequest, ...grpc.CallOption) (*controlplanev1.UpdateResourceResponse, error)
	TransitionResource(context.Context, *controlplanev1.TransitionResourceRequest, ...grpc.CallOption) (*controlplanev1.TransitionResourceResponse, error)
	DeleteResource(context.Context, *controlplanev1.DeleteResourceRequest, ...grpc.CallOption) (*controlplanev1.DeleteResourceResponse, error)
	ManageAccessResource(context.Context, *controlplanev1.ManageAccessResourceRequest, ...grpc.CallOption) (*controlplanev1.ManageAccessResourceResponse, error)
	DetachAccessResource(context.Context, *controlplanev1.DetachAccessResourceRequest, ...grpc.CallOption) (*controlplanev1.DetachAccessResourceResponse, error)
	CopyAccessResource(context.Context, *controlplanev1.CopyAccessResourceRequest, ...grpc.CallOption) (*controlplanev1.CopyAccessResourceResponse, error)
	GetResource(context.Context, *controlplanev1.GetResourceRequest, ...grpc.CallOption) (*controlplanev1.GetResourceResponse, error)
	ListResources(context.Context, *controlplanev1.ListResourcesRequest, ...grpc.CallOption) (*controlplanev1.ListResourcesResponse, error)
	SearchResources(context.Context, *controlplanev1.SearchResourcesRequest, ...grpc.CallOption) (*controlplanev1.SearchResourcesResponse, error)
	ListAuditEvents(context.Context, *controlplanev1.ListAuditEventsRequest, ...grpc.CallOption) (*controlplanev1.ListAuditEventsResponse, error)
	ListRuntimeIncidents(context.Context, *controlplanev1.ListRuntimeIncidentsRequest, ...grpc.CallOption) (*controlplanev1.ListRuntimeIncidentsResponse, error)
	AdmitOwnerSession(context.Context, *controlplanev1.AdmitOwnerSessionRequest, ...grpc.CallOption) (*controlplanev1.AdmitOwnerSessionResponse, error)
	RevokeOwnerSession(context.Context, *controlplanev1.RevokeOwnerSessionRequest, ...grpc.CallOption) (*controlplanev1.RevokeOwnerSessionResponse, error)
	GetDiagnostics(context.Context, *controlplanev1.GetDiagnosticsRequest, ...grpc.CallOption) (*controlplanev1.GetDiagnosticsResponse, error)
	RunScheduleNow(context.Context, *controlplanev1.RunScheduleNowRequest, ...grpc.CallOption) (*controlplanev1.RunScheduleNowResponse, error)
	ListScheduleOccurrences(context.Context, *controlplanev1.ListScheduleOccurrencesRequest, ...grpc.CallOption) (*controlplanev1.ListScheduleOccurrencesResponse, error)
	ResolveScheduleRecovery(context.Context, *controlplanev1.ResolveScheduleRecoveryRequest, ...grpc.CallOption) (*controlplanev1.ResolveScheduleRecoveryResponse, error)
	ManageSchedule(context.Context, *controlplanev1.ManageScheduleRequest, ...grpc.CallOption) (*controlplanev1.ManageScheduleResponse, error)
	ResolveOwnerGate(context.Context, *controlplanev1.ResolveOwnerGateRequest, ...grpc.CallOption) (*controlplanev1.ResolveOwnerGateResponse, error)
	ListBackups(context.Context, *controlplanev1.ListBackupsRequest, ...grpc.CallOption) (*controlplanev1.ListBackupsResponse, error)
	GetBackup(context.Context, *controlplanev1.GetBackupRequest, ...grpc.CallOption) (*controlplanev1.GetBackupResponse, error)
	RestoreBackup(context.Context, *controlplanev1.RestoreBackupRequest, ...grpc.CallOption) (*controlplanev1.RestoreBackupResponse, error)
	GetRestoreOperation(context.Context, *controlplanev1.GetRestoreOperationRequest, ...grpc.CallOption) (*controlplanev1.GetRestoreOperationResponse, error)
	ListRestoreOperations(context.Context, *controlplanev1.ListRestoreOperationsRequest, ...grpc.CallOption) (*controlplanev1.ListRestoreOperationsResponse, error)
	ManageRoleImageRecipe(context.Context, *controlplanev1.ManageRoleImageRecipeRequest, ...grpc.CallOption) (*controlplanev1.ManageRoleImageRecipeResponse, error)
	GetRoleImageRecipe(context.Context, *controlplanev1.GetRoleImageRecipeRequest, ...grpc.CallOption) (*controlplanev1.GetRoleImageRecipeResponse, error)
	ManageImageBuild(context.Context, *controlplanev1.ManageImageBuildRequest, ...grpc.CallOption) (*controlplanev1.ManageImageBuildResponse, error)
	GetRoleImageBuild(context.Context, *controlplanev1.GetRoleImageBuildRequest, ...grpc.CallOption) (*controlplanev1.GetRoleImageBuildResponse, error)
	ManageRoleDefinition(context.Context, *controlplanev1.ManageRoleDefinitionRequest, ...grpc.CallOption) (*controlplanev1.ManageRoleDefinitionResponse, error)
	GetRoleDefinition(context.Context, *controlplanev1.GetRoleDefinitionRequest, ...grpc.CallOption) (*controlplanev1.GetRoleDefinitionResponse, error)
	ListRoleDefinitions(context.Context, *controlplanev1.ListRoleDefinitionsRequest, ...grpc.CallOption) (*controlplanev1.ListRoleDefinitionsResponse, error)
	ListRoleDefinitionHistory(context.Context, *controlplanev1.ListRoleDefinitionHistoryRequest, ...grpc.CallOption) (*controlplanev1.ListRoleDefinitionHistoryResponse, error)
	ManageAgent(context.Context, *controlplanev1.ManageAgentRequest, ...grpc.CallOption) (*controlplanev1.ManageAgentResponse, error)
	GetAgent(context.Context, *controlplanev1.GetAgentRequest, ...grpc.CallOption) (*controlplanev1.GetAgentResponse, error)
	ListAgents(context.Context, *controlplanev1.ListAgentsRequest, ...grpc.CallOption) (*controlplanev1.ListAgentsResponse, error)
	ListAgentHistory(context.Context, *controlplanev1.ListAgentHistoryRequest, ...grpc.CallOption) (*controlplanev1.ListAgentHistoryResponse, error)
	ManageAgentAssignment(context.Context, *controlplanev1.ManageAgentAssignmentRequest, ...grpc.CallOption) (*controlplanev1.ManageAgentAssignmentResponse, error)
	GetAgentAssignment(context.Context, *controlplanev1.GetAgentAssignmentRequest, ...grpc.CallOption) (*controlplanev1.GetAgentAssignmentResponse, error)
	ListAgentAssignments(context.Context, *controlplanev1.ListAgentAssignmentsRequest, ...grpc.CallOption) (*controlplanev1.ListAgentAssignmentsResponse, error)
	ListAgentAssignmentHistory(context.Context, *controlplanev1.ListAgentAssignmentHistoryRequest, ...grpc.CallOption) (*controlplanev1.ListAgentAssignmentHistoryResponse, error)
	ManageInstructionSet(context.Context, *controlplanev1.ManageInstructionSetRequest, ...grpc.CallOption) (*controlplanev1.ManageInstructionSetResponse, error)
	GetInstructionSet(context.Context, *controlplanev1.GetInstructionSetRequest, ...grpc.CallOption) (*controlplanev1.GetInstructionSetResponse, error)
	ListInstructionSets(context.Context, *controlplanev1.ListInstructionSetsRequest, ...grpc.CallOption) (*controlplanev1.ListInstructionSetsResponse, error)
	ListInstructionSetHistory(context.Context, *controlplanev1.ListInstructionSetHistoryRequest, ...grpc.CallOption) (*controlplanev1.ListInstructionSetHistoryResponse, error)
	CompareInstructionSetVersions(context.Context, *controlplanev1.CompareInstructionSetVersionsRequest, ...grpc.CallOption) (*controlplanev1.CompareInstructionSetVersionsResponse, error)
	CreateScheduleFromOwnerSelections(context.Context, *controlplanev1.CreateScheduleFromOwnerSelectionsRequest, ...grpc.CallOption) (*controlplanev1.CreateScheduleFromOwnerSelectionsResponse, error)
	BindScheduleConfiguration(context.Context, *controlplanev1.BindScheduleConfigurationRequest, ...grpc.CallOption) (*controlplanev1.BindScheduleConfigurationResponse, error)
	ManageRun(context.Context, *controlplanev1.ManageRunRequest, ...grpc.CallOption) (*controlplanev1.ManageRunResponse, error)
	GetRunDetail(context.Context, *controlplanev1.GetRunDetailRequest, ...grpc.CallOption) (*controlplanev1.GetRunDetailResponse, error)
	ListRunTimeline(context.Context, *controlplanev1.ListRunTimelineRequest, ...grpc.CallOption) (*controlplanev1.ListRunTimelineResponse, error)
	GetRunLineage(context.Context, *controlplanev1.GetRunLineageRequest, ...grpc.CallOption) (*controlplanev1.GetRunLineageResponse, error)
	ListRunArtifacts(context.Context, *controlplanev1.ListRunArtifactsRequest, ...grpc.CallOption) (*controlplanev1.ListRunArtifactsResponse, error)
	ManageRuntimeIncident(context.Context, *controlplanev1.ManageRuntimeIncidentRequest, ...grpc.CallOption) (*controlplanev1.ManageRuntimeIncidentResponse, error)
	GetRuntimeIncident(context.Context, *controlplanev1.GetRuntimeIncidentRequest, ...grpc.CallOption) (*controlplanev1.GetRuntimeIncidentResponse, error)
	ListRuntimeIncidentHistory(context.Context, *controlplanev1.ListRuntimeIncidentHistoryRequest, ...grpc.CallOption) (*controlplanev1.ListRuntimeIncidentHistoryResponse, error)
	ManageWorkspaceBackup(context.Context, *controlplanev1.ManageWorkspaceBackupRequest, ...grpc.CallOption) (*controlplanev1.ManageWorkspaceBackupResponse, error)
	GetWorkspaceBackup(context.Context, *controlplanev1.GetWorkspaceBackupRequest, ...grpc.CallOption) (*controlplanev1.GetWorkspaceBackupResponse, error)
	ListWorkspaceBackups(context.Context, *controlplanev1.ListWorkspaceBackupsRequest, ...grpc.CallOption) (*controlplanev1.ListWorkspaceBackupsResponse, error)
	ManageWorkspaceRestore(context.Context, *controlplanev1.ManageWorkspaceRestoreRequest, ...grpc.CallOption) (*controlplanev1.ManageWorkspaceRestoreResponse, error)
	GetWorkspaceRestore(context.Context, *controlplanev1.GetWorkspaceRestoreRequest, ...grpc.CallOption) (*controlplanev1.GetWorkspaceRestoreResponse, error)
	ListWorkspaceRestores(context.Context, *controlplanev1.ListWorkspaceRestoresRequest, ...grpc.CallOption) (*controlplanev1.ListWorkspaceRestoresResponse, error)
}

func (server *Server) RunScheduleNow(writer http.ResponseWriter, request *http.Request,
	scheduleID uuid.UUID, params generated.RunScheduleNowParams,
) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.RunScheduleNow(request.Context(), &controlplanev1.RunScheduleNowRequest{
		IdempotencyKey: params.IdempotencyKey.String(), ScheduleId: scheduleID.String(), ExpectedVersion: version,
	})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	if response.GetOccurrence() == nil || response.GetOccurrence().GetScheduleId() != scheduleID.String() {
		server.writeInternal(writer, request.Context(), errors.New("manual schedule occurrence response is invalid"))
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Location", "/api/v1/schedules/"+scheduleID.String()+"/occurrences")
	projected, err := scheduleOccurrenceProjection(response.GetOccurrence())
	if err != nil {
		server.writeInternal(writer, request.Context(), err)
		return
	}
	writeJSON(writer, http.StatusAccepted, projected)
}

func (server *Server) ResolveScheduleRecovery(writer http.ResponseWriter, request *http.Request,
	scheduleID uuid.UUID, occurrenceID uuid.UUID, params generated.ResolveScheduleRecoveryParams,
) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	var body generated.ResolveScheduleRecoveryJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	action := map[generated.ResolveScheduleRecoveryAction]controlplanev1.ScheduleRecoveryAction{
		generated.ResolveScheduleRecoveryActionREPAIR: controlplanev1.ScheduleRecoveryAction_SCHEDULE_RECOVERY_ACTION_REPAIR,
		generated.ResolveScheduleRecoveryActionCANCEL: controlplanev1.ScheduleRecoveryAction_SCHEDULE_RECOVERY_ACTION_CANCEL,
		generated.ResolveScheduleRecoveryActionSKIP:   controlplanev1.ScheduleRecoveryAction_SCHEDULE_RECOVERY_ACTION_SKIP,
	}[body.Action]
	if action == controlplanev1.ScheduleRecoveryAction_SCHEDULE_RECOVERY_ACTION_UNSPECIFIED {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.ResolveScheduleRecovery(request.Context(),
		&controlplanev1.ResolveScheduleRecoveryRequest{
			IdempotencyKey: params.IdempotencyKey.String(), ScheduleId: scheduleID.String(),
			OccurrenceId: occurrenceID.String(), ExpectedVersion: version,
			ExpectedAttempt: uint32(body.ExpectedAttempt), Action: action,
			EvidenceSha256: body.RecoveryEvidenceSha256, ReasonCode: body.ReasonCode,
		})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	if response.GetOccurrence() == nil || response.GetOccurrence().GetScheduleId() != scheduleID.String() ||
		response.GetOccurrence().GetOccurrenceId() != occurrenceID.String() {
		server.writeInternal(writer, request.Context(), errors.New("schedule recovery response is invalid"))
		return
	}
	projected, err := scheduleOccurrenceProjection(response.GetOccurrence())
	if err != nil {
		server.writeInternal(writer, request.Context(), err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf("\"%d\"", projected.Version))
	writeJSON(writer, http.StatusOK, projected)
}

func (server *Server) ListScheduleOccurrences(writer http.ResponseWriter, request *http.Request,
	scheduleID uuid.UUID, params generated.ListScheduleOccurrencesParams,
) {
	size := uint32(defaultPageSize)
	if params.PageSize != nil {
		size = uint32(*params.PageSize)
	}
	token := ""
	if params.PageToken != nil {
		token = string(*params.PageToken)
	}
	response, err := server.control.ListScheduleOccurrences(request.Context(),
		&controlplanev1.ListScheduleOccurrencesRequest{ScheduleId: scheduleID.String(), PageSize: size, PageToken: token})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	page := generated.ScheduleOccurrencePage{Occurrences: make([]generated.ScheduleOccurrence, 0, len(response.GetOccurrences()))}
	for _, occurrence := range response.GetOccurrences() {
		projected, castErr := scheduleOccurrenceProjection(occurrence)
		if castErr != nil {
			server.writeInternal(writer, request.Context(), castErr)
			return
		}
		page.Occurrences = append(page.Occurrences, projected)
	}
	if response.GetNextPageToken() != "" {
		page.NextPageToken = stringPointer(response.GetNextPageToken())
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusOK, page)
}

func scheduleOccurrenceProjection(input *controlplanev1.ScheduleOccurrence) (generated.ScheduleOccurrence, error) {
	if input == nil || input.GetScheduledFor() == nil || input.GetAvailableAt() == nil {
		return generated.ScheduleOccurrence{}, errors.New("schedule occurrence projection is incomplete")
	}
	occurrenceID, err := uuid.Parse(input.GetOccurrenceId())
	if err != nil {
		return generated.ScheduleOccurrence{}, errors.New("schedule occurrence identity is invalid")
	}
	scheduleID, err := uuid.Parse(input.GetScheduleId())
	if err != nil {
		return generated.ScheduleOccurrence{}, errors.New("schedule identity is invalid")
	}
	targetID, err := uuid.Parse(input.GetTargetResourceId())
	if err != nil {
		return generated.ScheduleOccurrence{}, errors.New("schedule target identity is invalid")
	}
	outcome := input.GetOutcome()
	result := generated.ScheduleOccurrence{OccurrenceId: occurrenceID, ScheduleId: scheduleID,
		ScheduledFor: input.GetScheduledFor().AsTime(), TargetResourceId: targetID,
		TargetKind:    generated.ResourceKind(strings.TrimPrefix(input.GetTargetKind().String(), "RESOURCE_KIND_")),
		TargetVersion: int64(input.GetTargetVersion()), EffectiveInputSha256: input.GetEffectiveInputSha256(),
		State:   generated.ScheduleOccurrenceState(strings.TrimPrefix(input.GetState().String(), "SCHEDULE_OCCURRENCE_STATE_")),
		Attempt: int(input.GetAttempt()), AuthorityGeneration: int64(input.GetAuthorityGeneration()),
		Version: int64(input.GetVersion()), AvailableAt: input.GetAvailableAt().AsTime()}
	if outcome != "" {
		result.Outcome = &outcome
	}
	if input.GetRecoveryEvidenceSha256() != "" {
		evidence := input.GetRecoveryEvidenceSha256()
		result.RecoveryEvidenceSha256 = &evidence
	}
	if result.Version < 1 {
		return generated.ScheduleOccurrence{}, errors.New("schedule occurrence version is invalid")
	}
	return result, nil
}

type Server struct {
	control     ControlPlane
	interaction interactiongatewayv1.MattermostTeamServiceClient
	integration integrationgatewayv1.IntegrationManagementServiceClient
	boundary    *boundary.Boundary
	logger      *slog.Logger
	realtime    http.Handler
}

func (server *Server) AttachRealtime(handler http.Handler) {
	server.realtime = handler
}

func New(control ControlPlane, interaction interactiongatewayv1.MattermostTeamServiceClient, integration integrationgatewayv1.IntegrationManagementServiceClient, security *boundary.Boundary, logger *slog.Logger) (*Server, error) {
	if control == nil || interaction == nil || integration == nil || security == nil || logger == nil {
		return nil, errors.New("control API HTTP server configuration is invalid")
	}
	return &Server{control: control, interaction: interaction, integration: integration, boundary: security, logger: logger}, nil
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	if server.realtime != nil {
		mux.Handle("/api/v1/realtime", server.realtime)
	}
	generated.HandlerWithOptions(server, generated.StdHTTPServerOptions{
		BaseURL: "/api/v1", BaseRouter: mux,
		ErrorHandlerFunc: func(writer http.ResponseWriter, _ *http.Request, _ error) {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		},
	})
	return server.boundary.Middleware(mux)
}

func (server *Server) CreateOwnerSession(writer http.ResponseWriter, request *http.Request, params generated.CreateOwnerSessionParams) {
	principal, bearer, ok := boundary.VerifiedAuthorizationFromContext(request.Context())
	if !ok {
		writeProblem(writer, localProblem(http.StatusUnauthorized, "UNAUTHENTICATED", false))
		return
	}
	admitted, err := server.control.AdmitOwnerSession(request.Context(), &controlplanev1.AdmitOwnerSessionRequest{IdempotencyKey: params.IdempotencyKey.String()})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	if admitted.GetSession() == nil || !admitted.GetSession().GetActive() || admitted.GetSession().GetSessionId() != principal.SessionID || admitted.GetSession().GetCurrentRevision() != principal.SessionRevision {
		server.writeInternal(writer, request.Context(), errors.New("owner session admission response is invalid"))
		return
	}
	claims, encoded, csrf, err := server.boundary.IssueSession(principal, bearer)
	if err != nil {
		switch {
		case errors.Is(err, boundary.ErrRateLimited):
			writeProblem(writer, localProblem(http.StatusTooManyRequests, "RATE_LIMITED", true))
		case errors.Is(err, boundary.ErrUnauthenticated):
			writeProblem(writer, localProblem(http.StatusUnauthorized, "UNAUTHENTICATED", false))
		default:
			server.logger.ErrorContext(request.Context(), "owner session issuance failed", "error_class", "session_contract")
			writeProblem(writer, localProblem(http.StatusInternalServerError, "INTERNAL", false))
		}
		return
	}
	maxAge := int(time.Until(time.Unix(claims.ExpiresAt, 0)).Seconds())
	if maxAge < 1 || maxAge > sessionCookieMaxAge {
		maxAge = sessionCookieMaxAge
	}
	http.SetCookie(writer, &http.Cookie{Name: boundary.SessionCookieName, Value: encoded, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
	http.SetCookie(writer, &http.Cookie{Name: boundary.CSRFCookieName, Value: csrf, Path: "/", Secure: true, HttpOnly: false, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
	writer.Header().Set("ETag", fmt.Sprintf("\"%d\"", claims.SessionRevision))
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) DeleteOwnerSession(writer http.ResponseWriter, request *http.Request, params generated.DeleteOwnerSessionParams) {
	revision, ok := requireETag(writer, params.IfMatch)
	identity, identityOK := boundary.IdentityFromContext(request.Context())
	if !ok {
		return
	}
	if !identityOK {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	if revision != identity.SessionRevision {
		writeProblem(writer, localProblem(http.StatusPreconditionFailed, "VERSION_MISMATCH", false))
		return
	}
	response, err := server.control.RevokeOwnerSession(request.Context(), &controlplanev1.RevokeOwnerSessionRequest{IdempotencyKey: params.IdempotencyKey.String(), ExpectedRevision: revision})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	if response.GetSession() == nil || response.GetSession().GetActive() || response.GetSession().GetSessionId() != identity.SessionID {
		server.writeInternal(writer, request.Context(), errors.New("owner session revocation response is invalid"))
		return
	}
	clearCookie(writer, boundary.SessionCookieName, true)
	clearCookie(writer, boundary.CSRFCookieName, false)
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) ListProjects(writer http.ResponseWriter, request *http.Request, params generated.ListProjectsParams) {
	pageLimit, ok := pageSize(params.PageSize)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.ListProjects(request.Context(), &controlplanev1.ListProjectsRequest{
		PageSize: pageLimit, PageToken: value(params.PageToken),
	})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	page, err := resourcePage(response.GetProjects(), response.GetNextPageToken())
	if err != nil {
		server.writeInternal(writer, request.Context(), err)
		return
	}
	writeJSON(writer, http.StatusOK, page)
}

func (server *Server) CreateProject(writer http.ResponseWriter, request *http.Request, params generated.CreateProjectParams) {
	var body generated.CreateProject
	if !decodeJSON(writer, request, &body) {
		return
	}
	response, err := server.control.CreateProject(request.Context(), &controlplanev1.CreateProjectRequest{
		IdempotencyKey: params.IdempotencyKey.String(), Name: body.Name,
		Spec: &controlplanev1.ProjectSpec{Slug: body.Spec.Slug, Description: value(body.Spec.Description), Locale: body.Spec.Locale, Ownership: uiOwnership()},
	})
	server.writeResourceResponse(writer, request.Context(), http.StatusCreated, response.GetProject(), err, true)
}

func (server *Server) UpdateProject(
	writer http.ResponseWriter,
	request *http.Request,
	projectID openapi_types.UUID,
	params generated.UpdateProjectParams,
) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	var body generated.UpdateProject
	if !decodeJSON(writer, request, &body) {
		return
	}
	response, err := server.control.UpdateProject(request.Context(), &controlplanev1.UpdateProjectRequest{
		IdempotencyKey: params.IdempotencyKey.String(), ProjectId: projectID.String(),
		ExpectedVersion: version, Name: body.Name,
		Spec: &controlplanev1.ProjectSpec{
			Slug: body.Spec.Slug, Description: value(body.Spec.Description),
			Locale: body.Spec.Locale, Ownership: uiOwnership(),
		},
	})
	server.writeResourceResponse(
		writer, request.Context(), http.StatusOK, response.GetProject(), err, true,
	)
}

func (server *Server) DeleteProject(
	writer http.ResponseWriter,
	request *http.Request,
	projectID openapi_types.UUID,
	params generated.DeleteProjectParams,
) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.DeleteProject(request.Context(), &controlplanev1.DeleteProjectRequest{
		IdempotencyKey: params.IdempotencyKey.String(), ProjectId: projectID.String(),
		ExpectedVersion: version,
	})
	server.writeResourceResponse(
		writer, request.Context(), http.StatusOK, response.GetProject(), err, true,
	)
}

func (server *Server) CreateSchedule(
	writer http.ResponseWriter,
	request *http.Request,
	params generated.CreateScheduleParams,
) {
	var body generated.CreateSchedule
	if !decodeJSON(writer, request, &body) {
		return
	}
	spec, ok := scheduleSpec(body.Input)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.ManageSchedule(request.Context(), &controlplanev1.ManageScheduleRequest{
		IdempotencyKey: params.IdempotencyKey.String(),
		Action:         controlplanev1.AdministrativeAction_ADMINISTRATIVE_ACTION_CREATE,
		Name:           body.Name, Spec: spec,
	})
	server.writeResourceResponse(
		writer, request.Context(), http.StatusCreated, response.GetSchedule(), err, true,
	)
}

func (server *Server) UpdateSchedule(
	writer http.ResponseWriter,
	request *http.Request,
	scheduleID openapi_types.UUID,
	params generated.UpdateScheduleParams,
) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	var body generated.UpdateSchedule
	if !decodeJSON(writer, request, &body) {
		return
	}
	spec, ok := scheduleSpec(body.Input)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.ManageSchedule(request.Context(), &controlplanev1.ManageScheduleRequest{
		IdempotencyKey: params.IdempotencyKey.String(),
		Action:         controlplanev1.AdministrativeAction_ADMINISTRATIVE_ACTION_UPDATE,
		ScheduleId:     scheduleID.String(), ExpectedVersion: version,
		Name: body.Name, Spec: spec,
	})
	server.writeResourceResponse(
		writer, request.Context(), http.StatusOK, response.GetSchedule(), err, true,
	)
}

func (server *Server) DeleteSchedule(
	writer http.ResponseWriter,
	request *http.Request,
	scheduleID openapi_types.UUID,
	params generated.DeleteScheduleParams,
) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.ManageSchedule(request.Context(), &controlplanev1.ManageScheduleRequest{
		IdempotencyKey: params.IdempotencyKey.String(),
		Action:         controlplanev1.AdministrativeAction_ADMINISTRATIVE_ACTION_DELETE,
		ScheduleId:     scheduleID.String(), ExpectedVersion: version,
	})
	server.writeResourceResponse(
		writer, request.Context(), http.StatusOK, response.GetSchedule(), err, true,
	)
}

func scheduleSpec(input generated.ScheduleInput) (*controlplanev1.ScheduleSpec, bool) {
	overlap, overlapOK := controlplanev1.ScheduleOverlapPolicy_value["SCHEDULE_OVERLAP_POLICY_"+string(input.OverlapPolicy)]
	misfire, misfireOK := controlplanev1.ScheduleMisfirePolicy_value["SCHEDULE_MISFIRE_POLICY_"+string(input.MisfirePolicy)]
	session, sessionOK := controlplanev1.ScheduleSessionPolicy_value["SCHEDULE_SESSION_POLICY_"+string(input.SessionPolicy)]
	notification, notificationOK := controlplanev1.ScheduleNotificationPolicy_value["SCHEDULE_NOTIFICATION_POLICY_"+string(input.NotificationPolicy)]
	target, targetOK := controlplanev1.ScheduleTargetType_value["SCHEDULE_TARGET_TYPE_"+string(input.TargetType)]
	if !overlapOK || !misfireOK || !sessionOK || !notificationOK || !targetOK {
		return nil, false
	}
	result := &controlplanev1.ScheduleSpec{
		TargetResourceId: input.TargetResourceId.String(), Cron: value(input.Cron),
		Timezone:      input.Timezone,
		OverlapPolicy: controlplanev1.ScheduleOverlapPolicy(overlap),
		MisfirePolicy: controlplanev1.ScheduleMisfirePolicy(misfire),
		MisfireGrace:  durationpb.New(time.Duration(input.MisfireGraceSeconds) * time.Second),
		Calendar:      string(input.Calendar), DeliveryPolicy: string(input.DeliveryPolicy),
		MaximumAttempts: uint32(input.MaximumAttempts),
		InitialBackoff:  durationpb.New(time.Duration(input.InitialBackoffSeconds) * time.Second),
		MaximumBackoff:  durationpb.New(time.Duration(input.MaximumBackoffSeconds) * time.Second),
		DeadLetterAfter: durationpb.New(time.Duration(input.DeadLetterAfterSeconds) * time.Second),
		PromptProfileId: input.PromptProfileId.String(), PromptRevision: uint64(input.PromptRevision),
		SessionPolicy: controlplanev1.ScheduleSessionPolicy(session), RoomId: uuidValue(input.RoomId),
		NotificationPolicy: controlplanev1.ScheduleNotificationPolicy(notification),
		MaximumExecutionDuration: durationpb.New(
			time.Duration(input.MaximumExecutionSeconds) * time.Second,
		),
		Coalesce: input.Coalesce, RuntimeRevisionId: input.RuntimeRevisionId.String(),
		TargetType: controlplanev1.ScheduleTargetType(target), PlaybookRef: value(input.PlaybookRef),
		PlaybookVersion:    uint64Value(input.PlaybookVersion),
		PromptArtifactId:   input.PromptArtifactId.String(),
		ExecutionSessionId: uuidValue(input.ExecutionSessionId), Ownership: uiOwnership(),
	}
	if input.IntervalSeconds != nil {
		result.Interval = durationpb.New(time.Duration(*input.IntervalSeconds) * time.Second)
	}
	return result, true
}

func (server *Server) ResolveOwnerGate(
	writer http.ResponseWriter,
	request *http.Request,
	ownerGateID openapi_types.UUID,
	params generated.ResolveOwnerGateParams,
) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	var body generated.ResolveOwnerGate
	if !decodeJSON(writer, request, &body) {
		return
	}
	decisionValue, ok := controlplanev1.OwnerGateDecision_value["OWNER_GATE_DECISION_"+string(body.Decision)]
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.ResolveOwnerGate(request.Context(), &controlplanev1.ResolveOwnerGateRequest{
		IdempotencyKey: params.IdempotencyKey.String(), OwnerGateId: ownerGateID.String(),
		ExpectedVersion: version, Decision: controlplanev1.OwnerGateDecision(decisionValue),
		Reason: body.Reason,
	})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	if response.GetOwnerGate() == nil || response.GetProcessRun() == nil ||
		response.GetOwnerGate().GetId() != ownerGateID.String() {
		server.writeInternal(writer, request.Context(), errors.New("owner gate response is invalid"))
		return
	}
	ownerGate, err := ConvertResource(response.GetOwnerGate())
	if err != nil {
		server.writeInternal(writer, request.Context(), err)
		return
	}
	processRun, err := ConvertResource(response.GetProcessRun())
	if err != nil {
		server.writeInternal(writer, request.Context(), err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf("\"%d\"", ownerGate.Version))
	writeJSON(writer, http.StatusOK, generated.ResolveOwnerGateResult{
		OwnerGate: ownerGate, ProcessRun: processRun,
	})
}

func (server *Server) ListBackups(
	writer http.ResponseWriter,
	request *http.Request,
	params generated.ListBackupsParams,
) {
	limit, ok := pageSize(params.PageSize)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.ListBackups(request.Context(), &controlplanev1.ListBackupsRequest{
		PageSize: limit, PageToken: value(params.PageToken),
	})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	page := generated.BackupPage{Backups: make([]generated.Backup, 0, len(response.GetBackups()))}
	for _, backup := range response.GetBackups() {
		projected, projectErr := backupProjection(backup)
		if projectErr != nil {
			server.writeInternal(writer, request.Context(), projectErr)
			return
		}
		page.Backups = append(page.Backups, projected)
	}
	if response.GetNextPageToken() != "" {
		page.NextPageToken = stringPointer(response.GetNextPageToken())
	}
	writeJSON(writer, http.StatusOK, page)
}

func (server *Server) GetBackup(
	writer http.ResponseWriter,
	request *http.Request,
	backupID openapi_types.UUID,
) {
	response, err := server.control.GetBackup(request.Context(), &controlplanev1.GetBackupRequest{
		BackupId: backupID.String(),
	})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	projected, err := backupProjection(response.GetBackup())
	if err != nil || projected.BackupId != backupID {
		server.writeInternal(writer, request.Context(), errors.New("backup response is invalid"))
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf("\"%d\"", projected.Version))
	writeJSON(writer, http.StatusOK, projected)
}

func (server *Server) RestoreBackup(
	writer http.ResponseWriter,
	request *http.Request,
	backupID openapi_types.UUID,
	params generated.RestoreBackupParams,
) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	var body generated.RestoreBackup
	if !decodeJSON(writer, request, &body) {
		return
	}
	response, err := server.control.RestoreBackup(request.Context(), &controlplanev1.RestoreBackupRequest{
		IdempotencyKey: params.IdempotencyKey.String(), BackupId: backupID.String(),
		ExpectedBackupVersion: version, ExpectedSourceVersion: uint64(body.SourceVersion),
		ArchiveSha256:    body.ArchiveSha256,
		ProvenanceSha256: body.ProvenanceSha256, Scope: string(body.Scope),
		ScopeId: body.ScopeId.String(),
	})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	projected, err := restoreOperationProjection(response.GetOperation())
	if err != nil || projected.BackupId != backupID || projected.SourceVersion != body.SourceVersion {
		server.writeInternal(writer, request.Context(), errors.New("restore operation response is invalid"))
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf("\"%d\"", projected.Version))
	writer.Header().Set("Location", "/api/v1/restore-operations/"+projected.RestoreOperationId.String())
	writeJSON(writer, http.StatusAccepted, projected)
}

func (server *Server) GetRestoreOperation(
	writer http.ResponseWriter,
	request *http.Request,
	operationID openapi_types.UUID,
) {
	response, err := server.control.GetRestoreOperation(
		request.Context(), &controlplanev1.GetRestoreOperationRequest{
			RestoreOperationId: operationID.String(),
		},
	)
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	projected, err := restoreOperationProjection(response.GetOperation())
	if err != nil || projected.RestoreOperationId != operationID {
		server.writeInternal(writer, request.Context(), errors.New("restore operation response is invalid"))
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf("\"%d\"", projected.Version))
	writeJSON(writer, http.StatusOK, projected)
}

func (server *Server) ListRestoreOperations(
	writer http.ResponseWriter,
	request *http.Request,
	params generated.ListRestoreOperationsParams,
) {
	limit, ok := pageSize(params.PageSize)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.ListRestoreOperations(
		request.Context(), &controlplanev1.ListRestoreOperationsRequest{
			BackupId: uuidValue(params.BackupId), PageSize: limit, PageToken: value(params.PageToken),
		},
	)
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	page := generated.RestoreOperationPage{Operations: make([]generated.RestoreOperation, 0, len(response.GetOperations()))}
	for _, operation := range response.GetOperations() {
		projected, projectErr := restoreOperationProjection(operation)
		if projectErr != nil {
			server.writeInternal(writer, request.Context(), projectErr)
			return
		}
		page.Operations = append(page.Operations, projected)
	}
	if response.GetNextPageToken() != "" {
		page.NextPageToken = stringPointer(response.GetNextPageToken())
	}
	writeJSON(writer, http.StatusOK, page)
}

func backupProjection(input *controlplanev1.Backup) (generated.Backup, error) {
	if input == nil || input.GetVersion() == 0 || input.GetSourceVersion() == 0 || input.GetCreatedAt() == nil ||
		input.GetUpdatedAt() == nil || !input.GetCreatedAt().IsValid() || !input.GetUpdatedAt().IsValid() {
		return generated.Backup{}, errors.New("backup projection is incomplete")
	}
	backupID, err := uuid.Parse(input.GetBackupId())
	if err != nil {
		return generated.Backup{}, err
	}
	scopeID, err := uuid.Parse(input.GetScopeId())
	if err != nil {
		return generated.Backup{}, err
	}
	state := generated.BackupState(strings.TrimPrefix(input.GetState().String(), "BACKUP_STATE_"))
	scope := generated.BackupScope(input.GetScope())
	if !state.Valid() || !scope.Valid() {
		return generated.Backup{}, errors.New("backup projection enum is invalid")
	}
	result := generated.Backup{
		BackupId: backupID, Version: int64(input.GetVersion()), SourceVersion: int64(input.GetSourceVersion()),
		SourceRuntimeRevisionSha256: input.GetSourceRuntimeRevisionSha256(),
		SourceImmutableInputSha256:  input.GetSourceImmutableInputSha256(),
		ArchiveSha256:               input.GetArchiveSha256(), ProvenanceSha256: input.GetProvenanceSha256(),
		State: state, Scope: scope, ScopeId: scopeID, Restorable: input.GetRestorable(),
		CreatedAt: input.GetCreatedAt().AsTime(), UpdatedAt: input.GetUpdatedAt().AsTime(),
	}
	if input.GetRestoreOperationId() != "" {
		operationID, parseErr := uuid.Parse(input.GetRestoreOperationId())
		if parseErr != nil {
			return generated.Backup{}, parseErr
		}
		result.RestoreOperationId = &operationID
	}
	if input.GetAvailableAt() != nil {
		if !input.GetAvailableAt().IsValid() {
			return generated.Backup{}, errors.New("backup available timestamp is invalid")
		}
		available := input.GetAvailableAt().AsTime()
		result.AvailableAt = &available
	}
	if input.GetRetainUntil() != nil {
		if !input.GetRetainUntil().IsValid() {
			return generated.Backup{}, errors.New("backup retention timestamp is invalid")
		}
		retainUntil := input.GetRetainUntil().AsTime()
		result.RetainUntil = &retainUntil
	}
	return result, nil
}

func restoreOperationProjection(
	input *controlplanev1.RestoreOperation,
) (generated.RestoreOperation, error) {
	if input == nil || input.GetVersion() == 0 || input.GetSourceVersion() == 0 || input.GetGeneration() == 0 ||
		input.GetTargetAttempt() == 0 || input.GetCreatedAt() == nil || input.GetUpdatedAt() == nil ||
		!input.GetCreatedAt().IsValid() || !input.GetUpdatedAt().IsValid() {
		return generated.RestoreOperation{}, errors.New("restore operation projection is incomplete")
	}
	operationID, err := uuid.Parse(input.GetRestoreOperationId())
	if err != nil {
		return generated.RestoreOperation{}, err
	}
	backupID, err := uuid.Parse(input.GetBackupId())
	if err != nil {
		return generated.RestoreOperation{}, err
	}
	scopeID, err := uuid.Parse(input.GetScopeId())
	if err != nil {
		return generated.RestoreOperation{}, err
	}
	targetTurnID, err := uuid.Parse(input.GetTargetTurnId())
	if err != nil {
		return generated.RestoreOperation{}, err
	}
	state := generated.RestoreOperationState(strings.TrimPrefix(
		input.GetState().String(), "RESTORE_OPERATION_STATE_",
	))
	scope := generated.RestoreOperationScope(input.GetScope())
	nextAction := generated.RestoreOperationNextAction(strings.TrimPrefix(
		input.GetNextAction().String(), "RESTORE_OPERATION_NEXT_ACTION_",
	))
	if !state.Valid() || !scope.Valid() || !nextAction.Valid() {
		return generated.RestoreOperation{}, errors.New("restore operation enum is invalid")
	}
	result := generated.RestoreOperation{
		RestoreOperationId: operationID, Version: int64(input.GetVersion()), State: state,
		BackupId: backupID, SourceVersion: int64(input.GetSourceVersion()),
		ArchiveSha256: input.GetArchiveSha256(), ProvenanceSha256: input.GetProvenanceSha256(),
		Scope: scope, ScopeId: scopeID, TargetTurnId: targetTurnID,
		TargetAttempt: int(input.GetTargetAttempt()), Generation: int64(input.GetGeneration()),
		Partial: input.GetPartial(), NextAction: nextAction,
		CreatedAt: input.GetCreatedAt().AsTime(), UpdatedAt: input.GetUpdatedAt().AsTime(),
	}
	if input.GetErrorCode() != "" {
		result.ErrorCode = stringPointer(input.GetErrorCode())
	}
	return result, nil
}

func (server *Server) ListResources(writer http.ResponseWriter, request *http.Request, params generated.ListResourcesParams) {
	pageLimit, pageOK := pageSize(params.PageSize)
	if !pageOK {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	kind, ok := resourceKind(params.Kind)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	states, ok := lifecycleStates(params.State)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.ListResources(request.Context(), &controlplanev1.ListResourcesRequest{
		Kind: kind, ParentId: uuidValue(params.ParentId), States: states,
		PageSize: pageLimit, PageToken: value(params.PageToken),
	})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	page, err := resourcePage(response.GetResources(), response.GetNextPageToken())
	if err != nil {
		server.writeInternal(writer, request.Context(), err)
		return
	}
	writeJSON(writer, http.StatusOK, page)
}

func (server *Server) CreateResource(writer http.ResponseWriter, request *http.Request, params generated.CreateResourceParams) {
	var body generated.CreateResource
	if !decodeJSON(writer, request, &body) {
		return
	}
	kind, spec, err := mutableSpec(body.Kind, body.Spec)
	if err != nil {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, rpcErr := server.control.CreateResource(request.Context(), &controlplanev1.CreateResourceRequest{
		IdempotencyKey: params.IdempotencyKey.String(), Kind: kind, Name: body.Name,
		ParentId: uuidValue(body.ParentId), Spec: spec,
	})
	server.writeResourceResponse(writer, request.Context(), http.StatusCreated, response.GetResource(), rpcErr, true)
}

func (server *Server) GetResource(writer http.ResponseWriter, request *http.Request, resourceID generated.ResourceID, params generated.GetResourceParams) {
	kind, ok := resourceKind(params.ExpectedKind)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.GetResource(request.Context(), &controlplanev1.GetResourceRequest{ResourceId: resourceID.String(), ExpectedKind: kind})
	server.writeResourceResponse(writer, request.Context(), http.StatusOK, response.GetResource(), err, false)
}

func (server *Server) UpdateResource(writer http.ResponseWriter, request *http.Request, resourceID generated.ResourceID, params generated.UpdateResourceParams) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	var body generated.UpdateResource
	if !decodeJSON(writer, request, &body) {
		return
	}
	_, spec, err := mutableSpec(body.Kind, body.Spec)
	if err != nil {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, rpcErr := server.control.UpdateResource(request.Context(), &controlplanev1.UpdateResourceRequest{
		IdempotencyKey: params.IdempotencyKey.String(), ResourceId: resourceID.String(), ExpectedVersion: version,
		Name: body.Name, Spec: spec,
	})
	server.writeResourceResponse(writer, request.Context(), http.StatusOK, response.GetResource(), rpcErr, true)
}

func (server *Server) SearchResources(writer http.ResponseWriter, request *http.Request, params generated.SearchResourcesParams) {
	pageLimit, pageOK := pageSize(params.PageSize)
	kind, kindOK := resourceKind(params.Kind)
	states, statesOK := lifecycleStates(params.State)
	if !pageOK || !kindOK || !statesOK || strings.TrimSpace(params.Query) != params.Query || params.Query == "" {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.SearchResources(request.Context(), &controlplanev1.SearchResourcesRequest{
		Kind: kind, Query: params.Query, States: states, PageSize: pageLimit, PageToken: value(params.PageToken),
	})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	page, err := resourcePage(response.GetResources(), response.GetNextPageToken())
	if err != nil {
		server.writeInternal(writer, request.Context(), err)
		return
	}
	writeJSON(writer, http.StatusOK, page)
}

func (server *Server) DeleteResource(writer http.ResponseWriter, request *http.Request, resourceID generated.ResourceID, params generated.DeleteResourceParams) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.DeleteResource(request.Context(), &controlplanev1.DeleteResourceRequest{
		IdempotencyKey: params.IdempotencyKey.String(), ResourceId: resourceID.String(), ExpectedVersion: version,
	})
	server.writeResourceResponse(writer, request.Context(), http.StatusOK, response.GetResource(), err, true)
}

func (server *Server) TransitionResource(writer http.ResponseWriter, request *http.Request, resourceID generated.ResourceID, params generated.TransitionResourceParams) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	var body generated.TransitionResource
	if !decodeJSON(writer, request, &body) {
		return
	}
	state, ok := lifecycleState(body.TargetState)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.TransitionResource(request.Context(), &controlplanev1.TransitionResourceRequest{
		IdempotencyKey: params.IdempotencyKey.String(), ResourceId: resourceID.String(), ExpectedVersion: version,
		TargetState: state, ReasonCode: body.ReasonCode,
	})
	server.writeResourceResponse(writer, request.Context(), http.StatusOK, response.GetResource(), err, true)
}

func (server *Server) ManageAccessResource(writer http.ResponseWriter, request *http.Request, params generated.ManageAccessResourceParams) {
	var body generated.ManageAccessResource
	if !decodeJSON(writer, request, &body) {
		return
	}
	action, actionOK := administrativeAction(body.Action)
	kind, kindOK := accessKind(body.Kind)
	if !kindOK || !actionOK {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	var name string
	var spec *controlplanev1.ResourceSpec
	if action == controlplanev1.AdministrativeAction_ADMINISTRATIVE_ACTION_CREATE || action == controlplanev1.AdministrativeAction_ADMINISTRATIVE_ACTION_UPDATE {
		if body.Name == nil || body.Spec == nil {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
			return
		}
		var specKind controlplanev1.ResourceKind
		var specOK bool
		specKind, spec, specOK = accessSpec(body.Kind, *body.Spec)
		if !specOK || specKind != kind {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
			return
		}
		name = *body.Name
	} else if body.Name != nil || body.Spec != nil {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	var resourceID string
	var version uint64
	if action != controlplanev1.AdministrativeAction_ADMINISTRATIVE_ACTION_CREATE {
		if body.ResourceId == nil || params.IfMatch == nil {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
			return
		}
		resourceID = body.ResourceId.String()
		var versionOK bool
		version, versionOK = requireETag(writer, *params.IfMatch)
		if !versionOK {
			return
		}
	} else if body.ResourceId != nil || params.IfMatch != nil {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.ManageAccessResource(request.Context(), &controlplanev1.ManageAccessResourceRequest{
		IdempotencyKey: params.IdempotencyKey.String(), Kind: kind, Action: action,
		ResourceId: resourceID, ExpectedVersion: version, Name: name, Spec: spec,
	})
	server.writeResourceResponse(writer, request.Context(), http.StatusOK, response.GetResource(), err, true)
}

func (server *Server) ManageRoleImageRecipe(writer http.ResponseWriter, request *http.Request, params generated.ManageRoleImageRecipeParams) {
	var body generated.ManageRoleImageRecipe
	if !decodeJSON(writer, request, &body) {
		return
	}
	action, ok := roleImageRecipeAction(body.Action)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	create := action == controlplanev1.RoleImageRecipeAction_ROLE_IMAGE_RECIPE_ACTION_CREATE
	update := action == controlplanev1.RoleImageRecipeAction_ROLE_IMAGE_RECIPE_ACTION_UPDATE
	var recipeID, name string
	var version uint64
	var input *controlplanev1.RoleImageRecipeInput
	if create {
		if body.RecipeId != nil || params.IfMatch != nil || body.Name == nil || body.Input == nil {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
			return
		}
		name, input = *body.Name, roleImageRecipeInput(*body.Input)
	} else {
		if body.RecipeId == nil || params.IfMatch == nil {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
			return
		}
		recipeID = body.RecipeId.String()
		parsedVersion, versionOK := requireETag(writer, generated.IfMatch(*params.IfMatch))
		if !versionOK {
			return
		}
		version = parsedVersion
		if update {
			if body.Name == nil || body.Input == nil {
				writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
				return
			}
			name, input = *body.Name, roleImageRecipeInput(*body.Input)
		} else if body.Name != nil || body.Input != nil {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
			return
		}
	}
	response, err := server.control.ManageRoleImageRecipe(request.Context(), &controlplanev1.ManageRoleImageRecipeRequest{
		IdempotencyKey: params.IdempotencyKey.String(), Action: action, RecipeId: recipeID,
		ExpectedVersion: version, Name: name, Input: input,
	})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	recipe, convertErr := ConvertResource(response.GetRecipe())
	if convertErr != nil {
		server.writeInternal(writer, request.Context(), convertErr)
		return
	}
	result := generated.RoleImageRecipeResult{Recipe: recipe, Reused: response.GetReused()}
	if response.GetImageBuild() != nil {
		converted, err := ConvertResource(response.GetImageBuild())
		if err != nil {
			server.writeInternal(writer, request.Context(), err)
			return
		}
		result.ImageBuild = &converted
	}
	if response.GetImageArtifact() != nil {
		converted, err := ConvertResource(response.GetImageArtifact())
		if err != nil {
			server.writeInternal(writer, request.Context(), err)
			return
		}
		result.ImageArtifact = &converted
	}
	writer.Header().Set("ETag", fmt.Sprintf("\"%d\"", recipe.Version))
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) GetRoleImageRecipe(
	writer http.ResponseWriter,
	request *http.Request,
	recipeID openapi_types.UUID,
	params generated.GetRoleImageRecipeParams,
) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.GetRoleImageRecipe(request.Context(), &controlplanev1.GetRoleImageRecipeRequest{
		RecipeId: recipeID.String(), ExpectedVersion: version,
	})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	resource, err := ConvertResource(response.GetRecipe())
	spec := response.GetRecipe().GetSpec().GetRoleImageRecipe()
	input, inputErr := roleImageRecipeReadbackInput(spec.GetInput())
	if err != nil || inputErr != nil || spec == nil {
		server.writeInternal(writer, request.Context(), errors.New("role image recipe readback is invalid"))
		return
	}
	result := generated.RoleImageRecipeReadback{Recipe: resource, Input: input,
		Generation: int64(spec.GetGeneration()), SpecSha256: spec.GetSpecSha256(),
		PolicyRevision: int64(spec.GetPolicyRevision()), PolicySha256: spec.GetPolicySha256(),
		RoleRuntimeContractRevision: int64(spec.GetRoleRuntimeContractRevision()),
		RoleRuntimeContractSha256:   spec.GetRoleRuntimeContractSha256()}
	writer.Header().Set("ETag", fmt.Sprintf("\"%d\"", resource.Version))
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) ManageImageBuild(writer http.ResponseWriter, request *http.Request, params generated.ManageImageBuildParams) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	var body generated.ManageImageBuild
	if !decodeJSON(writer, request, &body) {
		return
	}
	action, ok := imageBuildOwnerAction(body.Action)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.ManageImageBuild(request.Context(), &controlplanev1.ManageImageBuildRequest{
		IdempotencyKey: params.IdempotencyKey.String(), ImageBuildId: body.ImageBuildId.String(),
		ExpectedVersion: version, Action: action,
	})
	server.writeResourceResponse(writer, request.Context(), http.StatusOK, response.GetImageBuild(), err, true)
}

func (server *Server) GetRoleImageBuild(
	writer http.ResponseWriter,
	request *http.Request,
	imageBuildID openapi_types.UUID,
	params generated.GetRoleImageBuildParams,
) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.GetRoleImageBuild(request.Context(), &controlplanev1.GetRoleImageBuildRequest{
		ImageBuildId: imageBuildID.String(), ExpectedVersion: version,
	})
	server.writeResourceResponse(writer, request.Context(), http.StatusOK, response.GetImageBuild(), err, false)
}

func (server *Server) DetachAccessResource(writer http.ResponseWriter, request *http.Request, resourceID generated.ResourceID, params generated.DetachAccessResourceParams) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	var body generated.DetachAccessResource
	if !decodeJSON(writer, request, &body) {
		return
	}
	kind, ok := accessKind(body.Kind)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.DetachAccessResource(request.Context(), &controlplanev1.DetachAccessResourceRequest{
		IdempotencyKey: params.IdempotencyKey.String(), ResourceId: resourceID.String(), ExpectedVersion: version, ExpectedKind: kind,
	})
	server.writeResourceResponse(writer, request.Context(), http.StatusOK, response.GetResource(), err, true)
}

func (server *Server) CopyAccessResource(writer http.ResponseWriter, request *http.Request, resourceID generated.ResourceID, params generated.CopyAccessResourceParams) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	var body generated.CopyAccessResource
	if !decodeJSON(writer, request, &body) {
		return
	}
	kind, ok := accessKind(body.Kind)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.CopyAccessResource(request.Context(), &controlplanev1.CopyAccessResourceRequest{
		IdempotencyKey: params.IdempotencyKey.String(), SourceResourceId: resourceID.String(), ExpectedSourceVersion: version, ExpectedKind: kind, Name: body.Name,
	})
	server.writeResourceResponse(writer, request.Context(), http.StatusCreated, response.GetResource(), err, true)
}

func (server *Server) ListRuns(writer http.ResponseWriter, request *http.Request, params generated.ListRunsParams) {
	pageLimit, pageOK := pageSize(params.PageSize)
	if !pageOK {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	states, ok := lifecycleStates(params.State)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.ListResources(request.Context(), &controlplanev1.ListResourcesRequest{
		Kind: controlplanev1.ResourceKind_RESOURCE_KIND_PROCESS_RUN, States: states,
		PageSize: pageLimit, PageToken: value(params.PageToken),
	})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	page, err := resourcePage(response.GetResources(), response.GetNextPageToken())
	if err != nil {
		server.writeInternal(writer, request.Context(), err)
		return
	}
	writeJSON(writer, http.StatusOK, page)
}

func (server *Server) ListAuditEvents(writer http.ResponseWriter, request *http.Request, params generated.ListAuditEventsParams) {
	pageLimit, pageOK := pageSize(params.PageSize)
	if !pageOK {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	var kind controlplanev1.ResourceKind
	if params.ResourceKind != nil {
		var ok bool
		kind, ok = resourceKind(*params.ResourceKind)
		if !ok {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
			return
		}
	}
	selected, token, err := server.scanAudit(request.Context(), &controlplanev1.ListAuditEventsRequest{
		ResourceKind: kind, ResourceId: uuidValue(params.ResourceId), Action: value(params.Action),
		PageSize: pageLimit, PageToken: value(params.PageToken),
	}, nil)
	if err != nil {
		server.writeAuditScanError(writer, request.Context(), err)
		return
	}
	events := make([]generated.AuditEvent, 0, len(selected))
	for _, item := range selected {
		converted, convertErr := ConvertAuditEvent(item)
		if convertErr != nil {
			server.writeInternal(writer, request.Context(), convertErr)
			return
		}
		events = append(events, converted)
	}
	page := generated.AuditPage{Events: events}
	if token != "" {
		page.NextPageToken = stringPointer(token)
	}
	writeJSON(writer, http.StatusOK, page)
}

func (server *Server) ListIncidents(writer http.ResponseWriter, request *http.Request, params generated.ListIncidentsParams) {
	pageLimit, ok := pageSize(params.PageSize)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.ListRuntimeIncidents(request.Context(), &controlplanev1.ListRuntimeIncidentsRequest{PageSize: pageLimit, PageToken: value(params.PageToken)})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	incidents := make([]generated.RuntimeIncident, 0, len(response.GetIncidents()))
	for _, item := range response.GetIncidents() {
		converted, convertErr := ConvertRuntimeIncident(item)
		if convertErr != nil {
			server.writeInternal(writer, request.Context(), convertErr)
			return
		}
		incidents = append(incidents, converted)
	}
	page := generated.IncidentPage{Incidents: incidents}
	if response.GetNextPageToken() != "" {
		page.NextPageToken = stringPointer(response.GetNextPageToken())
	}
	writeJSON(writer, http.StatusOK, page)
}

func (server *Server) ListConfigurationChanges(writer http.ResponseWriter, request *http.Request, params generated.ListConfigurationChangesParams) {
	pageLimit, ok := pageSize(params.PageSize)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	selected, token, err := server.scanAudit(request.Context(), &controlplanev1.ListAuditEventsRequest{
		PageSize: pageLimit, PageToken: value(params.PageToken),
	}, projection.IsConfigurationAction)
	if err != nil {
		server.writeAuditScanError(writer, request.Context(), err)
		return
	}
	changes := make([]generated.ConfigurationChange, 0, len(selected))
	for _, item := range selected {
		converted, convertErr := ConvertConfigurationChange(item)
		if convertErr != nil {
			server.writeInternal(writer, request.Context(), convertErr)
			return
		}
		changes = append(changes, converted)
	}
	page := generated.ConfigurationChangePage{Changes: changes}
	if token != "" {
		page.NextPageToken = stringPointer(token)
	}
	writeJSON(writer, http.StatusOK, page)
}

func (server *Server) GetDiagnostics(writer http.ResponseWriter, request *http.Request) {
	response, err := server.control.GetDiagnostics(request.Context(), &controlplanev1.GetDiagnosticsRequest{})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	writeJSON(writer, http.StatusOK, generated.Diagnostics{
		SchemaVersion: int64(response.GetSchemaVersion()), PendingOutboxEvents: int64(response.GetPendingOutboxEvents()),
		TerminalOutboxEvents: int64(response.GetTerminalOutboxEvents()), OldestPendingAgeSeconds: response.GetOldestPendingAge().AsDuration().Seconds(),
		ActiveTurnLeases: int64(response.GetActiveTurnLeases()), QueuedScheduleOccurrences: int64(response.GetQueuedScheduleOccurrences()),
		RuntimePrincipalStatus: response.GetRuntimePrincipalStatus(), RuntimePrincipalGeneration: int64(response.GetRuntimePrincipalGeneration()),
	})
}

func (server *Server) scanAudit(ctx context.Context, rpcRequest *controlplanev1.ListAuditEventsRequest, filter func(string) bool) ([]*controlplanev1.AuditEvent, string, error) {
	target := int(rpcRequest.GetPageSize())
	events := make([]*controlplanev1.AuditEvent, 0, target)
	token := rpcRequest.GetPageToken()
	scanned := 0
	for len(events) < target {
		remaining := maximumAuditScan - scanned
		if remaining <= 0 {
			return nil, "", errAuditScanLimit
		}
		size := uint32(maximumPageSize)
		if remaining < int(size) {
			size = uint32(remaining)
		}
		requestedToken := token
		response, err := server.control.ListAuditEvents(ctx, &controlplanev1.ListAuditEventsRequest{ResourceKind: rpcRequest.GetResourceKind(), ResourceId: rpcRequest.GetResourceId(), Action: rpcRequest.GetAction(), PageSize: size, PageToken: token})
		if err != nil {
			return nil, "", err
		}
		next := response.GetNextPageToken()
		if next != "" && (len(response.GetEvents()) == 0 || next == requestedToken) {
			return nil, "", errAuditPagination
		}
		for index, item := range response.GetEvents() {
			scanned++
			token = item.GetId()
			if filter != nil && !filter(item.GetAction()) {
				continue
			}
			events = append(events, item)
			if len(events) == target {
				if index == len(response.GetEvents())-1 && next == "" {
					token = ""
				}
				break
			}
		}
		if len(events) == target {
			break
		}
		if next == "" {
			token = ""
			break
		}
		token = next
	}
	return events, token, nil
}

func (server *Server) writeAuditScanError(writer http.ResponseWriter, ctx context.Context, err error) {
	if errors.Is(err, errAuditScanLimit) {
		writeProblem(writer, localProblem(http.StatusServiceUnavailable, "UNAVAILABLE", true))
		return
	}
	if errors.Is(err, errAuditPagination) {
		server.writeInternal(writer, ctx, err)
		return
	}
	server.writeRPCError(writer, ctx, err, false)
}

func (server *Server) writeResourceResponse(writer http.ResponseWriter, ctx context.Context, statusCode int, resource *controlplanev1.Resource, err error, mutation bool) {
	if err != nil {
		server.writeRPCError(writer, ctx, err, mutation)
		return
	}
	converted, convertErr := ConvertResource(resource)
	if convertErr != nil {
		server.writeInternal(writer, ctx, convertErr)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf("\"%d\"", converted.Version))
	writeJSON(writer, statusCode, converted)
}

func (server *Server) writeRPCError(writer http.ResponseWriter, ctx context.Context, err error, mutation bool) {
	problem, expected := mapRPCError(err, mutation)
	grpcStatus, isGRPC := status.FromError(err)
	if !expected && (!isGRPC || !grpcserver.IsUnexpectedCode(grpcStatus.Code())) {
		server.logger.ErrorContext(ctx, "unexpected owner RPC outcome", "error_class", "rpc_contract")
	}
	writeProblem(writer, problem)
}

func (server *Server) writeInternal(writer http.ResponseWriter, ctx context.Context, _ error) {
	server.logger.ErrorContext(ctx, "invalid owner RPC response", "error_class", "response_contract")
	writeProblem(writer, localProblem(http.StatusInternalServerError, "INTERNAL", false))
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeProblem(writer, localProblem(http.StatusUnsupportedMediaType, "INVALID_REQUEST", false))
		return false
	}
	limited := http.MaxBytesReader(writer, request.Body, maximumBodyBytes)
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return false
	}
	return true
}

func writeJSON(writer http.ResponseWriter, statusCode int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(value)
}

func clearCookie(writer http.ResponseWriter, name string, httpOnly bool) {
	http.SetCookie(writer, &http.Cookie{Name: name, Value: "", Path: "/", Secure: true, HttpOnly: httpOnly, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}

func parseETag(raw string) (uint64, bool) {
	if len(raw) < 3 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return 0, false
	}
	parsed, err := strconv.ParseUint(raw[1:len(raw)-1], 10, 64)
	return parsed, err == nil && parsed > 0
}

func requireETag(writer http.ResponseWriter, raw string) (uint64, bool) {
	value, ok := parseETag(raw)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
	}
	return value, ok
}

func pageSize(value *int) (uint32, bool) {
	if value == nil {
		return defaultPageSize, true
	}
	if *value < 1 || *value > maximumPageSize {
		return 0, false
	}
	return uint32(*value), true
}

func value[T ~string](pointer *T) string {
	if pointer == nil {
		return ""
	}
	return string(*pointer)
}

func uuidValue(pointer *uuid.UUID) string {
	if pointer == nil {
		return ""
	}
	return pointer.String()
}

func uint64Value(pointer *int64) uint64 {
	if pointer == nil || *pointer < 0 {
		return 0
	}
	return uint64(*pointer)
}

func uiOwnership() *controlplanev1.ConfigurationOwnership {
	return &controlplanev1.ConfigurationOwnership{ManagedBy: controlplanev1.ConfigurationManager_CONFIGURATION_MANAGER_UI}
}

func mutableSpec(kind generated.MutableResourceKind, input generated.ResourceSpecInput) (controlplanev1.ResourceKind, *controlplanev1.ResourceSpec, error) {
	count := 0
	for _, present := range []bool{input.Chat != nil, input.CredentialBinding != nil, input.RepositoryWorkspace != nil, input.Integration != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return 0, nil, errors.New("resource spec cardinality is invalid")
	}
	switch kind {
	case generated.CHAT:
		if input.Chat == nil {
			break
		}
		room := map[generated.ChatRoomType]controlplanev1.RoomType{generated.USER: controlplanev1.RoomType_ROOM_TYPE_USER, generated.COORDINATION: controlplanev1.RoomType_ROOM_TYPE_COORDINATION, generated.WORKCONTROL: controlplanev1.RoomType_ROOM_TYPE_WORK_CONTROL, generated.RUNS: controlplanev1.RoomType_ROOM_TYPE_RUNS}[input.Chat.RoomType]
		if room == 0 {
			break
		}
		return controlplanev1.ResourceKind_RESOURCE_KIND_CHAT, &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_Chat{Chat: &controlplanev1.ChatSpec{StableKey: input.Chat.StableKey, RoomType: room, DefaultAgentId: uuidValue(input.Chat.DefaultAgentId), ExternalChannelRef: value(input.Chat.ExternalChannelRef), WorkPolicy: input.Chat.WorkPolicy, Ownership: uiOwnership()}}}, nil
	case generated.CREDENTIALBINDING:
		if input.CredentialBinding == nil || input.CredentialBinding.Revision < 1 {
			break
		}
		return controlplanev1.ResourceKind_RESOURCE_KIND_CREDENTIAL_BINDING, &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_CredentialBinding{CredentialBinding: &controlplanev1.CredentialBindingSpec{Purpose: input.CredentialBinding.Purpose, ImmutableSecretRef: input.CredentialBinding.ImmutableSecretRef, PrincipalRef: input.CredentialBinding.PrincipalRef, Revision: uint64(input.CredentialBinding.Revision), Ownership: uiOwnership()}}}, nil
	case generated.REPOSITORYWORKSPACE:
		if input.RepositoryWorkspace == nil {
			break
		}
		return controlplanev1.ResourceKind_RESOURCE_KIND_REPOSITORY_WORKSPACE, &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_RepositoryWorkspace{RepositoryWorkspace: &controlplanev1.RepositoryWorkspaceSpec{RepositoryRef: input.RepositoryWorkspace.RepositoryRef, WorkspaceMode: input.RepositoryWorkspace.WorkspaceMode, DefaultBranch: input.RepositoryWorkspace.DefaultBranch, CredentialBindingId: uuidValue(input.RepositoryWorkspace.CredentialBindingId), Ownership: uiOwnership()}}}, nil
	case generated.INTEGRATION:
		if input.Integration == nil || input.Integration.DefinitionVersion < 1 {
			break
		}
		return controlplanev1.ResourceKind_RESOURCE_KIND_INTEGRATION, &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_Integration{Integration: &controlplanev1.IntegrationSpec{DefinitionRef: input.Integration.DefinitionRef, DefinitionVersion: uint64(input.Integration.DefinitionVersion), Capabilities: input.Integration.Capabilities, CredentialBindingIds: uuidStrings(input.Integration.CredentialBindingIds), EndpointRef: input.Integration.EndpointRef, Ownership: uiOwnership()}}}, nil
	}
	return 0, nil, errors.New("resource kind and spec mismatch")
}

func accessSpec(kind generated.AccessResourceKind, input generated.AccessSpecInput) (controlplanev1.ResourceKind, *controlplanev1.ResourceSpec, bool) {
	count := 0
	for _, present := range []bool{input.Team != nil, input.Role != nil, input.PromptProfile != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return 0, nil, false
	}
	switch kind {
	case generated.AccessResourceKindTEAM:
		if input.Team == nil {
			break
		}
		return controlplanev1.ResourceKind_RESOURCE_KIND_TEAM, &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_Team{Team: &controlplanev1.TeamSpec{StableKey: input.Team.StableKey, ExternalTeamRef: value(input.Team.ExternalTeamRef), MemberActorIds: uuidStrings(input.Team.MemberActorIds), RoleIds: uuidStrings(input.Team.RoleIds), Ownership: uiOwnership()}}}, true
	case generated.AccessResourceKindROLE:
		if input.Role == nil {
			break
		}
		return controlplanev1.ResourceKind_RESOURCE_KIND_ROLE, &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_Role{Role: &controlplanev1.RoleSpec{StableKey: input.Role.StableKey, Capabilities: input.Role.Capabilities, AllowedTargetRoleIds: uuidStrings(input.Role.AllowedTargetRoleIds), PromptProfileId: uuidValue(input.Role.PromptProfileId), RoleImageRecipeId: input.Role.RoleImageRecipeId.String(), ProviderCredentialBindingIds: uuidStrings(input.Role.ProviderCredentialBindingIds), RepositoryWorkspaceIds: uuidStrings(input.Role.RepositoryWorkspaceIds), IntegrationIds: uuidStrings(input.Role.IntegrationIds), Ownership: uiOwnership()}}}, true
	case generated.AccessResourceKindPROMPTPROFILE:
		if input.PromptProfile == nil || input.PromptProfile.Revision < 1 {
			break
		}
		return controlplanev1.ResourceKind_RESOURCE_KIND_PROMPT_PROFILE, &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_PromptProfile{PromptProfile: &controlplanev1.PromptProfileSpec{Revision: uint64(input.PromptProfile.Revision), ContentSha256: input.PromptProfile.ContentSha256, SourceRef: input.PromptProfile.SourceRef, Locale: input.PromptProfile.Locale, Ownership: uiOwnership()}}}, true
	}
	return 0, nil, false
}

func accessKind(kind generated.AccessResourceKind) (controlplanev1.ResourceKind, bool) {
	switch kind {
	case generated.AccessResourceKindTEAM:
		return controlplanev1.ResourceKind_RESOURCE_KIND_TEAM, true
	case generated.AccessResourceKindROLE:
		return controlplanev1.ResourceKind_RESOURCE_KIND_ROLE, true
	case generated.AccessResourceKindPROMPTPROFILE:
		return controlplanev1.ResourceKind_RESOURCE_KIND_PROMPT_PROFILE, true
	default:
		return 0, false
	}
}

func administrativeAction(input generated.ManageAccessResourceAction) (controlplanev1.AdministrativeAction, bool) {
	mapping := map[generated.ManageAccessResourceAction]controlplanev1.AdministrativeAction{
		generated.ManageAccessResourceActionCREATE: controlplanev1.AdministrativeAction_ADMINISTRATIVE_ACTION_CREATE, generated.ManageAccessResourceActionUPDATE: controlplanev1.AdministrativeAction_ADMINISTRATIVE_ACTION_UPDATE,
		generated.ManageAccessResourceActionACTIVATE: controlplanev1.AdministrativeAction_ADMINISTRATIVE_ACTION_ACTIVATE, generated.ManageAccessResourceActionPAUSE: controlplanev1.AdministrativeAction_ADMINISTRATIVE_ACTION_PAUSE,
		generated.ManageAccessResourceActionARCHIVE: controlplanev1.AdministrativeAction_ADMINISTRATIVE_ACTION_ARCHIVE, generated.ManageAccessResourceActionDELETE: controlplanev1.AdministrativeAction_ADMINISTRATIVE_ACTION_DELETE,
	}
	value, ok := mapping[input]
	return value, ok
}

func roleImageRecipeAction(input generated.ManageRoleImageRecipeAction) (controlplanev1.RoleImageRecipeAction, bool) {
	mapping := map[generated.ManageRoleImageRecipeAction]controlplanev1.RoleImageRecipeAction{
		generated.ManageRoleImageRecipeActionCREATE:       controlplanev1.RoleImageRecipeAction_ROLE_IMAGE_RECIPE_ACTION_CREATE,
		generated.ManageRoleImageRecipeActionUPDATE:       controlplanev1.RoleImageRecipeAction_ROLE_IMAGE_RECIPE_ACTION_UPDATE,
		generated.ManageRoleImageRecipeActionARCHIVE:      controlplanev1.RoleImageRecipeAction_ROLE_IMAGE_RECIPE_ACTION_ARCHIVE,
		generated.ManageRoleImageRecipeActionRESTORE:      controlplanev1.RoleImageRecipeAction_ROLE_IMAGE_RECIPE_ACTION_RESTORE,
		generated.ManageRoleImageRecipeActionDELETE:       controlplanev1.RoleImageRecipeAction_ROLE_IMAGE_RECIPE_ACTION_DELETE,
		generated.ManageRoleImageRecipeActionREQUESTBUILD: controlplanev1.RoleImageRecipeAction_ROLE_IMAGE_RECIPE_ACTION_REQUEST_BUILD,
	}
	value, ok := mapping[input]
	return value, ok
}

func imageBuildOwnerAction(input generated.ManageImageBuildAction) (controlplanev1.ImageBuildOwnerAction, bool) {
	mapping := map[generated.ManageImageBuildAction]controlplanev1.ImageBuildOwnerAction{
		generated.ManageImageBuildActionCANCEL:     controlplanev1.ImageBuildOwnerAction_IMAGE_BUILD_OWNER_ACTION_CANCEL,
		generated.ManageImageBuildActionRETRY:      controlplanev1.ImageBuildOwnerAction_IMAGE_BUILD_OWNER_ACTION_RETRY,
		generated.ManageImageBuildActionEXPIRE:     controlplanev1.ImageBuildOwnerAction_IMAGE_BUILD_OWNER_ACTION_EXPIRE,
		generated.ManageImageBuildActionDEADLETTER: controlplanev1.ImageBuildOwnerAction_IMAGE_BUILD_OWNER_ACTION_DEAD_LETTER,
	}
	value, ok := mapping[input]
	return value, ok
}

func roleImageRecipeInput(input generated.RoleImageRecipeInput) *controlplanev1.RoleImageRecipeInput {
	result := &controlplanev1.RoleImageRecipeInput{
		BaseImageReference: input.BaseImageReference, BaseImageDigest: input.BaseImageDigest,
		SourceRef: input.SourceRef, SourceRevision: input.SourceRevision, SourceSha256: input.SourceSha256,
		ContextRef: input.ContextRef, ContextSha256: input.ContextSha256,
		BuilderSha256: input.BuilderSha256, FrontendSha256: input.FrontendSha256,
		InstallationBlock: input.InstallationBlock,
		ToolchainSha256:   input.ToolchainSha256,
	}
	for _, platform := range input.Platforms {
		result.Platforms = append(result.Platforms, &controlplanev1.RoleImagePlatform{
			Os: string(platform.Os), Architecture: string(platform.Architecture), Variant: value(platform.Variant),
		})
	}
	for _, item := range input.Packages {
		result.Packages = append(result.Packages, &controlplanev1.RoleImagePackage{
			Manager: string(item.Manager), Name: item.Name, Version: item.Version, Digest: item.Digest, SourceRef: item.SourceRef,
		})
	}
	for _, item := range input.Tools {
		result.Tools = append(result.Tools, &controlplanev1.RoleImageTool{
			Name: item.Name, Version: item.Version, SourceRef: item.SourceRef, Sha256: item.Sha256,
		})
	}
	return result
}

func roleImageRecipeReadbackInput(input *controlplanev1.RoleImageRecipeInput) (generated.RoleImageRecipeInput, error) {
	status, err := roleImageRecipeProjectionInput(input)
	if err != nil {
		return generated.RoleImageRecipeInput{}, err
	}
	return generated.RoleImageRecipeInput{
		BaseImageReference: status.BaseImageReference, BaseImageDigest: status.BaseImageDigest,
		SourceRef: status.SourceRef, SourceRevision: status.SourceRevision, SourceSha256: status.SourceSha256,
		ContextRef: status.ContextRef, ContextSha256: status.ContextSha256, BuilderSha256: status.BuilderSha256,
		FrontendSha256: status.FrontendSha256, Platforms: status.Platforms, Packages: status.Packages,
		Tools: status.Tools, InstallationBlock: input.GetInstallationBlock(), ToolchainSha256: status.ToolchainSha256,
	}, nil
}

func resourceKind(input generated.ResourceKind) (controlplanev1.ResourceKind, bool) {
	value := controlplanev1.ResourceKind(controlplanev1.ResourceKind_value["RESOURCE_KIND_"+string(input)])
	return value, value != controlplanev1.ResourceKind_RESOURCE_KIND_UNSPECIFIED
}

func lifecycleState(input generated.LifecycleState) (controlplanev1.LifecycleState, bool) {
	value := controlplanev1.LifecycleState(controlplanev1.LifecycleState_value["LIFECYCLE_STATE_"+string(input)])
	return value, value != controlplanev1.LifecycleState_LIFECYCLE_STATE_UNSPECIFIED
}

func lifecycleStates(input *[]generated.LifecycleState) ([]controlplanev1.LifecycleState, bool) {
	if input == nil {
		return nil, true
	}
	if len(*input) > 8 {
		return nil, false
	}
	result := make([]controlplanev1.LifecycleState, 0, len(*input))
	for _, state := range *input {
		converted, ok := lifecycleState(state)
		if !ok {
			return nil, false
		}
		result = append(result, converted)
	}
	return result, true
}

func resourcePage(resources []*controlplanev1.Resource, next string) (generated.ResourcePage, error) {
	result := generated.ResourcePage{Resources: make([]generated.Resource, 0, len(resources))}
	for _, item := range resources {
		converted, err := ConvertResource(item)
		if err != nil {
			return generated.ResourcePage{}, err
		}
		result.Resources = append(result.Resources, converted)
	}
	if next != "" {
		result.NextPageToken = stringPointer(next)
	}
	return result, nil
}

func ConvertResource(input *controlplanev1.Resource) (generated.Resource, error) {
	if input == nil || input.GetVersion() == 0 || !validSHA256(input.GetProjectionSha256()) || input.GetCreatedAt() == nil || input.GetCreatedAt().CheckValid() != nil ||
		input.GetUpdatedAt() == nil || input.GetUpdatedAt().CheckValid() != nil || input.GetSpec() == nil {
		return generated.Resource{}, errors.New("resource response is incomplete")
	}
	id, err := uuid.Parse(input.GetId())
	if err != nil {
		return generated.Resource{}, err
	}
	kindName := strings.TrimPrefix(input.GetKind().String(), "RESOURCE_KIND_")
	stateName := strings.TrimPrefix(input.GetState().String(), "LIFECYCLE_STATE_")
	kind := generated.ResourceKind(kindName)
	state := generated.LifecycleState(stateName)
	if !kind.Valid() || !state.Valid() {
		return generated.Resource{}, errors.New("resource enum is invalid")
	}
	spec, err := resourceProjection(input)
	if err != nil {
		return generated.Resource{}, err
	}
	result := generated.Resource{Id: id, Kind: kind, Name: input.GetName(), State: state, Version: int64(input.GetVersion()), ProjectionSha256: generated.Sha256(strings.ToLower(input.GetProjectionSha256())), Spec: spec, CreatedAt: input.GetCreatedAt().AsTime(), UpdatedAt: input.GetUpdatedAt().AsTime()}
	if input.GetProjectId() != "" {
		parsed, parseErr := uuid.Parse(input.GetProjectId())
		if parseErr != nil {
			return generated.Resource{}, parseErr
		}
		result.ProjectId = &parsed
	}
	if input.GetParentId() != "" {
		parsed, parseErr := uuid.Parse(input.GetParentId())
		if parseErr != nil {
			return generated.Resource{}, parseErr
		}
		result.ParentId = &parsed
	}
	return result, nil
}

func ConvertAuditEvent(input *controlplanev1.AuditEvent) (generated.AuditEvent, error) {
	if input == nil || input.GetOccurredAt() == nil || input.GetOccurredAt().CheckValid() != nil || input.GetResourceVersion() == 0 || input.GetPolicyRevision() == 0 {
		return generated.AuditEvent{}, errors.New("audit response is incomplete")
	}
	id, err := uuid.Parse(input.GetId())
	if err != nil {
		return generated.AuditEvent{}, err
	}
	resourceID, err := uuid.Parse(input.GetResourceId())
	if err != nil {
		return generated.AuditEvent{}, err
	}
	actorID, err := uuid.Parse(input.GetActorId())
	if err != nil {
		return generated.AuditEvent{}, err
	}
	correlationID, err := uuid.Parse(input.GetCorrelationId())
	if err != nil {
		return generated.AuditEvent{}, err
	}
	kind := generated.ResourceKind(strings.TrimPrefix(input.GetResourceKind().String(), "RESOURCE_KIND_"))
	if !kind.Valid() {
		return generated.AuditEvent{}, errors.New("audit kind is invalid")
	}
	return generated.AuditEvent{Id: id, Action: input.GetAction(), ResourceId: resourceID, ResourceKind: kind, ResourceVersion: int64(input.GetResourceVersion()), Outcome: input.GetOutcome(), ActorId: actorID, CorrelationId: correlationID, PolicyRevision: int64(input.GetPolicyRevision()), OccurredAt: input.GetOccurredAt().AsTime()}, nil
}

func ConvertConfigurationChange(input *controlplanev1.AuditEvent) (generated.ConfigurationChange, error) {
	audit, err := ConvertAuditEvent(input)
	if err != nil {
		return generated.ConfigurationChange{}, err
	}
	action := generated.ConfigurationChangeAction(audit.Action)
	outcome := generated.ConfigurationChangeOutcome(audit.Outcome)
	if !action.Valid() || !outcome.Valid() {
		return generated.ConfigurationChange{}, errors.New("configuration change enum is invalid")
	}
	return generated.ConfigurationChange{
		Id: audit.Id, Action: action, ResourceId: audit.ResourceId,
		ResourceKind: audit.ResourceKind, ResourceVersion: audit.ResourceVersion,
		Outcome: outcome, ActorId: audit.ActorId, CorrelationId: audit.CorrelationId,
		PolicyRevision: audit.PolicyRevision, OccurredAt: audit.OccurredAt,
	}, nil
}

func uuidStrings(values []uuid.UUID) []string {
	result := make([]string, len(values))
	for index := range values {
		result[index] = values[index].String()
	}
	return result
}
func stringPointer(value string) *string { return &value }

var _ generated.ServerInterface = (*Server)(nil)
