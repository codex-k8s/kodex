// Package httptransport отображает canonical OpenAPI на generated gRPC clients.
package httptransport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	texti18n "github.com/codex-k8s/matter-codex/libs/go/i18n"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/security/boundary"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const maximumJSONBody = 1 << 20

type Server struct {
	control  *controlplaneclient.Client
	boundary *boundary.Boundary
	logger   *slog.Logger
	realtime http.Handler
	texts    *texti18n.Localizer
}

func New(control *controlplaneclient.Client, security *boundary.Boundary, logger *slog.Logger, texts *texti18n.Localizer) (*Server, error) {
	if control == nil || security == nil || logger == nil || texts == nil {
		return nil, errors.New("control API HTTP server configuration is invalid")
	}
	return &Server{control: control, boundary: security, logger: logger, texts: texts}, nil
}

func (server *Server) AttachRealtime(handler http.Handler) { server.realtime = handler }

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	if server.realtime != nil {
		mux.Handle("GET /api/v1/runs/{runRef}/stream", server.realtime)
	}
	generated.HandlerWithOptions(server, generated.StdHTTPServerOptions{BaseRouter: mux, ErrorHandlerFunc: func(writer http.ResponseWriter, _ *http.Request, _ error) {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
	}})
	return server.localizationMiddleware(server.boundary.Middleware(mux))
}

type localizedResponseWriter struct {
	http.ResponseWriter
	texts  *texti18n.Localizer
	locale string
}

func (writer *localizedResponseWriter) Localize(messageID string) string {
	return writer.texts.Localize(writer.locale, messageID, nil)
}

func (writer *localizedResponseWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

func (server *Server) localizationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		locale := texti18n.ResolveAcceptLanguage(request.Header.Get("Accept-Language"))
		next.ServeHTTP(&localizedResponseWriter{ResponseWriter: writer, texts: server.texts, locale: locale}, request)
	})
}

func (server *Server) CreateOwnerSession(writer http.ResponseWriter, request *http.Request, _ generated.CreateOwnerSessionParams) {
	principal, bearer, ok := boundary.VerifiedAuthorizationFromContext(request.Context())
	if !ok {
		writeLocalProblem(writer, http.StatusUnauthorized, "UNAUTHENTICATED", false)
		return
	}
	claims, encoded, csrf, err := server.boundary.IssueSession(principal, bearer)
	if err != nil {
		if errors.Is(err, boundary.ErrRateLimited) {
			writeLocalProblem(writer, http.StatusTooManyRequests, "RATE_LIMITED", true)
		} else {
			writeLocalProblem(writer, http.StatusUnauthorized, "UNAUTHENTICATED", false)
		}
		return
	}
	maxAge := int(time.Until(time.Unix(claims.ExpiresAt, 0)).Seconds())
	if maxAge < 1 || maxAge > 3600 {
		maxAge = 3600
	}
	http.SetCookie(writer, &http.Cookie{Name: boundary.SessionCookieName, Value: encoded, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
	http.SetCookie(writer, &http.Cookie{Name: boundary.CSRFCookieName, Value: csrf, Path: "/", Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
	writer.Header().Set("ETag", fmt.Sprintf("\"%d\"", claims.SessionRevision))
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) DeleteOwnerSession(writer http.ResponseWriter, request *http.Request, parameters generated.DeleteOwnerSessionParams) {
	identity, ok := boundary.IdentityFromContext(request.Context())
	if !ok || string(parameters.IfMatch) != fmt.Sprintf("\"%d\"", identity.SessionRevision) {
		writeLocalProblem(writer, http.StatusPreconditionFailed, "STALE_VERSION", false)
		return
	}
	for _, item := range []struct {
		name     string
		httpOnly bool
	}{{boundary.SessionCookieName, true}, {boundary.CSRFCookieName, false}} {
		http.SetCookie(writer, &http.Cookie{Name: item.name, Path: "/", Secure: true, HttpOnly: item.httpOnly, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func decodeJSON[T any](writer http.ResponseWriter, request *http.Request) (T, bool) {
	var result T
	decoder := json.NewDecoder(io.LimitReader(request.Body, maximumJSONBody+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return result, false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return result, false
	}
	return result, true
}

func mutation(idempotency, etag string) (*controlplanev1.MutationContext, bool) {
	result := &controlplanev1.MutationContext{IdempotencyKey: idempotency}
	if etag == "" {
		return result, true
	}
	if len(etag) < 3 || etag[0] != '"' || etag[len(etag)-1] != '"' {
		return nil, false
	}
	version, err := strconv.ParseInt(etag[1:len(etag)-1], 10, 64)
	if err != nil || version < 1 {
		return nil, false
	}
	result.ExpectedVersion = &version
	return result, true
}

func requireMutation(writer http.ResponseWriter, idempotency, etag string) (*controlplanev1.MutationContext, bool) {
	result, ok := mutation(idempotency, etag)
	if !ok {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
	}
	return result, ok
}

func page(size *int, token *string) *controlplanev1.PageRequest {
	result := &controlplanev1.PageRequest{PageSize: 50}
	if size != nil {
		result.PageSize = int32(*size)
	}
	if token != nil {
		result.PageToken = *token
	}
	return result
}

func stringValue[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func writeMessage(writer http.ResponseWriter, statusCode int, message proto.Message, field string, pageField string) {
	value, err := messageMap(message)
	if err != nil {
		writeLocalProblem(writer, http.StatusInternalServerError, "INTERNAL", false)
		return
	}
	if field != "" {
		value, _ = value[field].(map[string]any)
	}
	if pageField != "" {
		items, _ := value[pageField].([]any)
		output := map[string]any{"items": items}
		if pageValue, ok := value["page"].(map[string]any); ok {
			if next, ok := pageValue["nextPageToken"].(string); ok && next != "" {
				output["nextPageToken"] = next
			}
		}
		value = output
	}
	if localizer, ok := writer.(interface{ Localize(string) string }); ok {
		LocalizeSafeErrors(value, localizer.Localize)
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	if version, ok := value["version"].(float64); ok && version >= 1 {
		writer.Header().Set("ETag", fmt.Sprintf("\"%.0f\"", version))
	}
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(value)
}

func messageMap(message proto.Message) (map[string]any, error) {
	raw, err := (protojson.MarshalOptions{UseProtoNames: false}).Marshal(message)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	normalize(value)
	return value, nil
}

// ProtoMap используется realtime transport для того же публичного enum и
// redaction mapping, что и HTTP readback.
func ProtoMap(message proto.Message) (map[string]any, error) { return messageMap(message) }

// LocalizeSafeErrors заменяет только безопасное отображаемое сообщение по
// стабильному коду. Raw provider/runtime payload никогда не используется как
// пользовательский текст.
func LocalizeSafeErrors(value any, localize func(string) string) {
	if localize == nil {
		return
	}
	switch current := value.(type) {
	case []any:
		for _, item := range current {
			LocalizeSafeErrors(item, localize)
		}
	case map[string]any:
		for _, item := range current {
			LocalizeSafeErrors(item, localize)
		}
		code, _ := current["safeErrorCode"].(string)
		if code != "" {
			current["safeErrorMessage"] = localize(strings.ToUpper(code))
		}
	}
}

var enumPrefixes = []string{"PLATFORM_ROLE_", "PROJECT_PERMISSION_", "NEXT_ACTION_", "ENTITY_LIFECYCLE_", "AGENT_STATE_", "INSTRUCTION_STATE_", "WORKFLOW_STATE_", "RUN_STATE_", "RUN_SOURCE_", "RUN_NODE_TYPE_", "RUN_NODE_STATE_", "RUN_EDGE_TYPE_", "RUN_EVENT_TYPE_", "OWNER_GATE_STATE_", "OWNER_GATE_DECISION_", "ARTIFACT_SCAN_STATE_", "SCHEDULE_STATE_", "CONNECTION_STATE_", "ASSISTANT_RUNTIME_STATE_", "TYPE_"}

func normalize(value any) {
	switch current := value.(type) {
	case []any:
		for _, item := range current {
			normalize(item)
		}
	case map[string]any:
		for key, item := range current {
			normalize(item)
			if text, ok := item.(string); ok {
				for _, prefix := range enumPrefixes {
					if strings.HasPrefix(text, prefix) {
						current[key] = strings.TrimPrefix(text, prefix)
						break
					}
				}
			}
		}
		if permissions, ok := current["projectPermissions"]; ok {
			current["permissions"] = permissions
			delete(current, "projectPermissions")
		}
		if grants, ok := current["integrationGrantRefs"]; ok {
			current["integrations"] = grants
			delete(current, "integrationGrantRefs")
		}
		if bindings, ok := current["agentBindingRefs"]; ok {
			current["agentBindings"] = bindings
			delete(current, "agentBindingRefs")
		}
		if runtimeValue, ok := current["runtime"].(map[string]any); ok {
			current["runtimeName"] = runtimeValue["name"]
			delete(current, "runtime")
		}
		if assistantState, ok := current["corePromptRevision"]; ok && assistantState != nil {
			current["system"] = true
			current["removable"] = false
		}
		if targetType, targetRef, ok := target(current); ok {
			current["type"] = targetType
			current["ref"] = targetRef
			delete(current, "agentRef")
			delete(current, "workflowRef")
		}
		if _, isWorkflow := current["publishedVersion"]; isWorkflow {
			flattenWorkflow(current)
		} else if _, hasDraft := current["draftVersion"]; hasDraft {
			flattenWorkflow(current)
		}
	}
}

func target(value map[string]any) (string, any, bool) {
	if ref, ok := value["agentRef"]; ok {
		return "AGENT", ref, true
	}
	if ref, ok := value["workflowRef"]; ok {
		return "WORKFLOW", ref, true
	}
	return "", nil, false
}

func flattenWorkflow(value map[string]any) {
	version, _ := value["publishedVersion"].(map[string]any)
	if version == nil {
		version, _ = value["draftVersion"].(map[string]any)
	}
	for _, key := range []string{"revision", "coordinatorAgentRef", "steps", "maxConcurrency", "timeoutSeconds", "completionCriteria", "validationMessages"} {
		if item, exists := version[key]; exists {
			value[key] = item
		}
	}
	delete(value, "publishedVersion")
	delete(value, "draftVersion")
}

func targetProto(kind, reference string) *controlplanev1.RunTarget {
	result := &controlplanev1.RunTarget{}
	if kind == "AGENT" {
		result.Target = &controlplanev1.RunTarget_AgentRef{AgentRef: reference}
	} else {
		result.Target = &controlplanev1.RunTarget_WorkflowRef{WorkflowRef: reference}
	}
	return result
}

func permissions(values []generated.MembershipInputPermissions) []controlplanev1.ProjectPermission {
	result := make([]controlplanev1.ProjectPermission, 0, len(values))
	for _, item := range values {
		if value, ok := controlplanev1.ProjectPermission_value["PROJECT_PERMISSION_"+string(item)]; ok {
			result = append(result, controlplanev1.ProjectPermission(value))
		}
	}
	return result
}

func platformRole(value generated.MembershipInputPlatformRole) controlplanev1.PlatformRole {
	return controlplanev1.PlatformRole(controlplanev1.PlatformRole_value["PLATFORM_ROLE_"+string(value)])
}

func gateDecision(value string) controlplanev1.OwnerGateDecision {
	return controlplanev1.OwnerGateDecision(controlplanev1.OwnerGateDecision_value["OWNER_GATE_DECISION_"+value])
}

func jsonEncoder(writer io.Writer) *json.Encoder { return json.NewEncoder(writer) }
