package httptransport

import (
	"bytes"
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
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/projection"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/security/boundary"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	sessionPath         = "/api/v1/session"
	maximumBodyBytes    = 256 << 10
	maximumPageSize     = 100
	defaultPageSize     = 50
	incidentAction      = "record_runtime_incident"
	sessionCookieMaxAge = 3600
)

type ControlPlane interface {
	CreateProject(context.Context, *controlplanev1.CreateProjectRequest, ...grpc.CallOption) (*controlplanev1.CreateProjectResponse, error)
	ListProjects(context.Context, *controlplanev1.ListProjectsRequest, ...grpc.CallOption) (*controlplanev1.ListProjectsResponse, error)
	CreateResource(context.Context, *controlplanev1.CreateResourceRequest, ...grpc.CallOption) (*controlplanev1.CreateResourceResponse, error)
	UpdateResource(context.Context, *controlplanev1.UpdateResourceRequest, ...grpc.CallOption) (*controlplanev1.UpdateResourceResponse, error)
	TransitionResource(context.Context, *controlplanev1.TransitionResourceRequest, ...grpc.CallOption) (*controlplanev1.TransitionResourceResponse, error)
	DeleteResource(context.Context, *controlplanev1.DeleteResourceRequest, ...grpc.CallOption) (*controlplanev1.DeleteResourceResponse, error)
	ManageAccessResource(context.Context, *controlplanev1.ManageAccessResourceRequest, ...grpc.CallOption) (*controlplanev1.ManageAccessResourceResponse, error)
	GetResource(context.Context, *controlplanev1.GetResourceRequest, ...grpc.CallOption) (*controlplanev1.GetResourceResponse, error)
	ListResources(context.Context, *controlplanev1.ListResourcesRequest, ...grpc.CallOption) (*controlplanev1.ListResourcesResponse, error)
	ListAuditEvents(context.Context, *controlplanev1.ListAuditEventsRequest, ...grpc.CallOption) (*controlplanev1.ListAuditEventsResponse, error)
	GetDiagnostics(context.Context, *controlplanev1.GetDiagnosticsRequest, ...grpc.CallOption) (*controlplanev1.GetDiagnosticsResponse, error)
}

type Server struct {
	control  ControlPlane
	boundary *boundary.Boundary
	logger   *slog.Logger
	realtime http.Handler
}

func (server *Server) AttachRealtime(handler http.Handler) {
	server.realtime = handler
}

func New(control ControlPlane, security *boundary.Boundary, logger *slog.Logger) (*Server, error) {
	if control == nil || security == nil || logger == nil {
		return nil, errors.New("control API HTTP server configuration is invalid")
	}
	return &Server{control: control, boundary: security, logger: logger}, nil
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

func (server *Server) CreateOwnerSession(writer http.ResponseWriter, request *http.Request) {
	principal, bearer, err := server.boundary.VerifyAuthorization(request.Context(), request.Header.Get("Authorization"))
	if err != nil {
		writeProblem(writer, localProblem(http.StatusUnauthorized, "UNAUTHENTICATED", false))
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
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) DeleteOwnerSession(writer http.ResponseWriter, _ *http.Request, _ generated.DeleteOwnerSessionParams) {
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
	version, ok := parseETag(params.IfMatch)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
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
		Name: body.Name, Spec: spec, DetachGitManagement: body.DetachGitManagement != nil && *body.DetachGitManagement,
	})
	server.writeResourceResponse(writer, request.Context(), http.StatusOK, response.GetResource(), rpcErr, true)
}

func (server *Server) DeleteResource(writer http.ResponseWriter, request *http.Request, resourceID generated.ResourceID, params generated.DeleteResourceParams) {
	version, ok := parseETag(params.IfMatch)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.DeleteResource(request.Context(), &controlplanev1.DeleteResourceRequest{
		IdempotencyKey: params.IdempotencyKey.String(), ResourceId: resourceID.String(), ExpectedVersion: version,
	})
	server.writeResourceResponse(writer, request.Context(), http.StatusOK, response.GetResource(), err, true)
}

func (server *Server) TransitionResource(writer http.ResponseWriter, request *http.Request, resourceID generated.ResourceID, params generated.TransitionResourceParams) {
	version, ok := parseETag(params.IfMatch)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
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
		version, versionOK = parseETag(*params.IfMatch)
		if !versionOK {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
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
	server.listAudit(writer, request, &controlplanev1.ListAuditEventsRequest{
		ResourceKind: kind, ResourceId: uuidValue(params.ResourceId), Action: value(params.Action),
		PageSize: pageLimit, PageToken: value(params.PageToken),
	}, nil)
}

func (server *Server) ListIncidents(writer http.ResponseWriter, request *http.Request, params generated.ListIncidentsParams) {
	pageLimit, ok := pageSize(params.PageSize)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	server.listAudit(writer, request, &controlplanev1.ListAuditEventsRequest{
		Action: incidentAction, PageSize: pageLimit, PageToken: value(params.PageToken),
	}, nil)
}

func (server *Server) ListConfigurationChanges(writer http.ResponseWriter, request *http.Request, params generated.ListConfigurationChangesParams) {
	pageLimit, ok := pageSize(params.PageSize)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	server.listAudit(writer, request, &controlplanev1.ListAuditEventsRequest{
		PageSize: pageLimit, PageToken: value(params.PageToken),
	}, projection.IsConfigurationAction)
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

func (server *Server) listAudit(writer http.ResponseWriter, request *http.Request, rpcRequest *controlplanev1.ListAuditEventsRequest, filter func(string) bool) {
	response, err := server.control.ListAuditEvents(request.Context(), rpcRequest)
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	events := make([]generated.AuditEvent, 0, len(response.GetEvents()))
	for _, item := range response.GetEvents() {
		if filter != nil && !filter(item.GetAction()) {
			continue
		}
		converted, convertErr := auditEvent(item)
		if convertErr != nil {
			server.writeInternal(writer, request.Context(), convertErr)
			return
		}
		events = append(events, converted)
	}
	page := generated.AuditPage{Events: events}
	if response.GetNextPageToken() != "" {
		page.NextPageToken = stringPointer(response.GetNextPageToken())
	}
	writeJSON(writer, http.StatusOK, page)
}

func (server *Server) writeResourceResponse(writer http.ResponseWriter, ctx context.Context, statusCode int, resource *controlplanev1.Resource, err error, mutation bool) {
	if err != nil {
		server.writeRPCError(writer, ctx, err, mutation)
		return
	}
	converted, convertErr := convertResource(resource)
	if convertErr != nil {
		server.writeInternal(writer, ctx, convertErr)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf("\"%d\"", converted.Version))
	writeJSON(writer, statusCode, converted)
}

func (server *Server) writeRPCError(writer http.ResponseWriter, ctx context.Context, err error, mutation bool) {
	problem, expected := mapRPCError(err, mutation)
	if !expected {
		server.logger.ErrorContext(ctx, "unexpected control-plane RPC outcome", "error_class", "rpc_contract")
	}
	writeProblem(writer, problem)
}

func (server *Server) writeInternal(writer http.ResponseWriter, ctx context.Context, _ error) {
	server.logger.ErrorContext(ctx, "invalid control-plane response", "error_class", "response_contract")
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
		room := map[generated.ChatSpecRoomType]controlplanev1.RoomType{generated.USER: controlplanev1.RoomType_ROOM_TYPE_USER, generated.COORDINATION: controlplanev1.RoomType_ROOM_TYPE_COORDINATION, generated.WORKCONTROL: controlplanev1.RoomType_ROOM_TYPE_WORK_CONTROL, generated.RUNS: controlplanev1.RoomType_ROOM_TYPE_RUNS}[input.Chat.RoomType]
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
		return controlplanev1.ResourceKind_RESOURCE_KIND_ROLE, &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_Role{Role: &controlplanev1.RoleSpec{StableKey: input.Role.StableKey, Capabilities: input.Role.Capabilities, AllowedTargetRoleIds: uuidStrings(input.Role.AllowedTargetRoleIds), PromptProfileId: uuidValue(input.Role.PromptProfileId), ProviderCredentialBindingIds: uuidStrings(input.Role.ProviderCredentialBindingIds), RepositoryWorkspaceIds: uuidStrings(input.Role.RepositoryWorkspaceIds), IntegrationIds: uuidStrings(input.Role.IntegrationIds), Ownership: uiOwnership()}}}, true
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
		generated.CREATE: controlplanev1.AdministrativeAction_ADMINISTRATIVE_ACTION_CREATE, generated.UPDATE: controlplanev1.AdministrativeAction_ADMINISTRATIVE_ACTION_UPDATE,
		generated.ACTIVATE: controlplanev1.AdministrativeAction_ADMINISTRATIVE_ACTION_ACTIVATE, generated.PAUSE: controlplanev1.AdministrativeAction_ADMINISTRATIVE_ACTION_PAUSE,
		generated.ARCHIVE: controlplanev1.AdministrativeAction_ADMINISTRATIVE_ACTION_ARCHIVE, generated.DELETE: controlplanev1.AdministrativeAction_ADMINISTRATIVE_ACTION_DELETE,
	}
	value, ok := mapping[input]
	return value, ok
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
		converted, err := convertResource(item)
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

func convertResource(input *controlplanev1.Resource) (generated.Resource, error) {
	if input == nil || input.GetVersion() == 0 || input.GetCreatedAt() == nil || input.GetUpdatedAt() == nil || input.GetSpec() == nil {
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
	raw, err := protojson.MarshalOptions{UseProtoNames: false}.Marshal(input.GetSpec())
	if err != nil {
		return generated.Resource{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var spec map[string]any
	if decoder.Decode(&spec) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return generated.Resource{}, errors.New("resource spec JSON is invalid")
	}
	result := generated.Resource{Id: id, Kind: kind, Name: input.GetName(), State: state, Version: int64(input.GetVersion()), Spec: spec, CreatedAt: input.GetCreatedAt().AsTime(), UpdatedAt: input.GetUpdatedAt().AsTime()}
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

func auditEvent(input *controlplanev1.AuditEvent) (generated.AuditEvent, error) {
	if input == nil || input.GetOccurredAt() == nil || input.GetResourceVersion() == 0 || input.GetPolicyRevision() == 0 {
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

func uuidStrings(values []uuid.UUID) []string {
	result := make([]string, len(values))
	for index := range values {
		result[index] = values[index].String()
	}
	return result
}
func stringPointer(value string) *string { return &value }

var _ generated.ServerInterface = (*Server)(nil)
