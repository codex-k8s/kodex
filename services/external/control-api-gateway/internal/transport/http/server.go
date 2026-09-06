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

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	texti18n "github.com/codex-k8s/kodex/libs/go/i18n"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/boundary"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const maximumJSONBody = 1 << 20

type Server struct {
	control      *controlplaneclient.Client
	secrets      secretbrokerv1.SecretBrokerServiceClient
	secretDrafts secretbrokerv1.SecretBrokerServiceClient
	speech       speechToTextClient
	boundary     *boundary.Boundary
	logger       *slog.Logger
	realtime     http.Handler
	texts        *texti18n.Localizer
}

func New(control *controlplaneclient.Client, security *boundary.Boundary, logger *slog.Logger, texts *texti18n.Localizer) (*Server, error) {
	if control == nil || security == nil || logger == nil || texts == nil {
		return nil, errors.New("control API HTTP server configuration is invalid")
	}
	return &Server{control: control, boundary: security, logger: logger, texts: texts}, nil
}

func (server *Server) AttachSecretBroker(client secretbrokerv1.SecretBrokerServiceClient) error {
	if client == nil || server.secrets != nil {
		return errors.New("secret broker attachment is invalid")
	}
	server.secrets = client
	return nil
}

func (server *Server) AttachSecretDraftBroker(client secretbrokerv1.SecretBrokerServiceClient) error {
	if client == nil || server.secretDrafts != nil {
		return errors.New("secret draft broker attachment is invalid")
	}
	server.secretDrafts = client
	return nil
}

func (server *Server) AttachRealtime(session http.Handler) {
	server.realtime = session
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	if server.realtime != nil {
		mux.Handle("GET /api/v1/session/stream", server.realtime)
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

func (writer *localizedResponseWriter) LocalizeFor(locale, messageID string) string {
	return writer.texts.Localize(locale, messageID, nil)
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
	body, ok := decodeOptionalJSON[generated.OwnerSessionCreateInput](writer, request)
	if !ok {
		return
	}
	var purpose *boundary.SessionPurpose
	if body != nil && body.Purpose != nil {
		secretRef := ""
		if body.Purpose.SecretRef != nil {
			secretRef = *body.Purpose.SecretRef
		}
		purpose = &boundary.SessionPurpose{
			Kind: string(body.Purpose.Kind), ProjectRef: stringValue(body.Purpose.ProjectRef), SecretRef: secretRef,
			ReceiptRef: stringValue(body.Purpose.ReceiptRef), ReceiptDigest: stringValue(body.Purpose.ReceiptDigest),
		}
		if body.Purpose.ReceiptVersion != nil {
			purpose.ReceiptVersion = *body.Purpose.ReceiptVersion
		}
	}
	claims, encoded, csrf, err := server.boundary.IssueSession(principal, bearer, purpose)
	if err != nil {
		switch {
		case errors.Is(err, boundary.ErrRateLimited):
			writeLocalProblem(writer, http.StatusTooManyRequests, "RATE_LIMITED", true)
		case errors.Is(err, boundary.ErrSessionPurposeInvalid):
			writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		case errors.Is(err, boundary.ErrFreshAuthenticationRequired):
			writeLocalProblem(writer, http.StatusForbidden, "FRESH_AUTHENTICATION_REQUIRED", false)
		default:
			writeLocalProblem(writer, http.StatusUnauthorized, "UNAUTHENTICATED", false)
		}
		return
	}
	boundary.SetOwnerSessionCookies(writer, claims, encoded, csrf)
	writer.Header().Set("ETag", fmt.Sprintf("\"%d\"", claims.SessionRevision))
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) RenewOwnerSession(writer http.ResponseWriter, request *http.Request, _ generated.RenewOwnerSessionParams) {
	server.GetOwnerSession(writer, request)
}

func (server *Server) DeleteOwnerSession(writer http.ResponseWriter, request *http.Request, parameters generated.DeleteOwnerSessionParams) {
	identity, ok := boundary.IdentityFromContext(request.Context())
	if !ok || string(parameters.IfMatch) != fmt.Sprintf("\"%d\"", identity.SessionRevision) {
		writeLocalProblem(writer, http.StatusPreconditionFailed, "STALE_VERSION", false)
		return
	}
	if err := server.boundary.RevokeSession(request.Context(), identity); err != nil {
		writeLocalProblem(writer, http.StatusInternalServerError, "INTERNAL", false)
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

func decodeOptionalJSON[T any](writer http.ResponseWriter, request *http.Request) (*T, bool) {
	decoder := json.NewDecoder(io.LimitReader(request.Body, maximumJSONBody+1))
	decoder.DisallowUnknownFields()
	var result T
	if err := decoder.Decode(&result); errors.Is(err, io.EOF) {
		return nil, true
	} else if err != nil {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return &result, true
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

func withProjectReference(writer http.ResponseWriter, request *http.Request, reference string) (*http.Request, bool) {
	ctx, err := controlplaneclient.WithProjectReference(request.Context(), reference)
	if err != nil {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return request.WithContext(ctx), true
}

func writeMessage(writer http.ResponseWriter, statusCode int, message proto.Message, field string, pageField string) {
	value, err := messageMap(message)
	if err != nil {
		if errors.Is(err, errPublicSecretDescriptor) || errors.Is(err, errPublicProviderStatusReason) || errors.Is(err, errPublicIntegrationShape) || errors.Is(err, errRuntimeCatalogView) || errors.Is(err, errOwnerGateShape) {
			writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		writeLocalProblem(writer, http.StatusInternalServerError, "INTERNAL", false)
		return
	}
	if field != "" {
		value, _ = value[field].(map[string]any)
	}
	if pageField != "" {
		items := []any{}
		if present, ok := value[pageField].([]any); ok {
			items = present
		}
		output := map[string]any{"items": items}
		if pageValue, ok := value["page"].(map[string]any); ok {
			if next, ok := pageValue["nextPageToken"].(string); ok && next != "" {
				output["nextPageToken"] = next
			}
		}
		if nextActions, ok := value["nextActions"].([]any); ok {
			output["nextActions"] = nextActions
		}
		if coreReady, ok := value["coreReady"].(bool); ok {
			output["coreReady"] = coreReady
		}
		if total, ok := value["total"].(float64); ok {
			output["total"] = total
		}
		for _, key := range []string{"subject", "evaluatedAt", "role"} {
			if preserved, ok := value[key]; ok {
				output[key] = preserved
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
	} else if version, ok := value["agentVersion"].(float64); ok && version >= 1 {
		writer.Header().Set("ETag", fmt.Sprintf("\"%.0f\"", version))
	}
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(value)
}

func messageMap(message proto.Message) (map[string]any, error) {
	if err := validateRuntimeCatalogMessage(message.ProtoReflect(), 0); err != nil {
		return nil, err
	}
	raw, err := (protojson.MarshalOptions{UseProtoNames: false}).Marshal(message)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if err := normalizeProtoJSONShape(value, message.ProtoReflect().Descriptor()); err != nil {
		return nil, err
	}
	normalize(value)
	return value, nil
}

const maximumSafeJSONInteger = int64(1<<53 - 1)

var errPublicSecretDescriptor = errors.New("public Secret descriptor revision is invalid")
var errPublicProviderStatusReason = errors.New("public provider status reason is invalid")

func normalizeProtoJSONShape(value map[string]any, descriptor protoreflect.MessageDescriptor) error {
	if descriptor.FullName() == "controlplane.v1.WorkflowVersion" {
		ref, ok := value["ref"].(string)
		if !ok || !fileTargetRef(ref) {
			return errors.New("workflow revision reference is invalid")
		}
		if _, ok := controlplanev1.WorkflowState_value[fmt.Sprint(value["state"])]; !ok || value["state"] == "WORKFLOW_STATE_UNSPECIFIED" {
			return errors.New("workflow revision state is invalid")
		}
	}
	if descriptor.FullName() == "controlplane.v1.ProviderAccount" {
		if reason, exists := value["safeStatusReason"]; exists {
			switch reason {
			case "AUTHORIZED", "ACCOUNT_DISABLED", "ACCOUNT_REVOKED", "REAUTHORIZATION_REQUIRED", "DEVICE_AUTHORIZATION_PENDING", "CREDENTIAL_CONFIGURATION_REQUIRED", "ACCOUNT_STATE_UNKNOWN", "DEVICE_AUTHORIZATION_EXPIRED", "DEVICE_AUTHORIZATION_FAILED", "CREDENTIAL_MATERIALIZATION_FAILED", "ACCOUNT_DELETING", "ACCOUNT_DELETED":
			default:
				return errPublicProviderStatusReason
			}
		}
	}
	if descriptor.FullName() == "controlplane.v1.RuntimeSecretDescriptor" {
		revision, ok := value["revision"].(string)
		pin, err := strconv.ParseInt(revision, 10, 64)
		if !ok || err != nil || !validManagedVersion(pin) {
			return errPublicSecretDescriptor
		}
		delete(value, "namespace")
	}
	fields := descriptor.Fields()
	for index := 0; index < fields.Len(); index++ {
		field := fields.Get(index)
		current, exists := value[field.JSONName()]
		if !exists {
			if field.IsList() {
				value[field.JSONName()] = []any{}
			} else if field.IsMap() {
				value[field.JSONName()] = map[string]any{}
			} else if defaultValue, required := requiredProtoScalarDefault(descriptor, field); required {
				value[field.JSONName()] = defaultValue
			}
			continue
		}
		if field.IsList() {
			items, ok := current.([]any)
			if !ok {
				return errors.New("public protobuf list shape is invalid")
			}
			for itemIndex := range items {
				normalized, err := normalizeProtoField(items[itemIndex], field)
				if err != nil {
					return err
				}
				items[itemIndex] = normalized
			}
			continue
		}
		normalized, err := normalizeProtoField(current, field)
		if err != nil {
			return err
		}
		value[field.JSONName()] = normalized
	}
	if descriptor.FullName() == "controlplane.v1.WorkflowVersion" {
		for _, key := range []string{"version", "revision"} {
			number, ok := value[key].(float64)
			if !ok || number < 1 || number > float64(maximumSafeJSONInteger) || number != float64(int64(number)) {
				return errors.New("workflow revision number is invalid")
			}
		}
	}
	if descriptor.FullName() == "controlplane.v1.AgentInstructionsBinding" {
		ref, refOK := value["ref"].(string)
		revision, revisionOK := value["revisionRef"].(string)
		version, versionOK := value["version"].(float64)
		if !refOK || !revisionOK || !versionOK || !fileTargetRef(ref) || !fileTargetRef(revision) || version < 1 || version > float64(maximumSafeJSONInteger) || version != float64(int64(version)) {
			return errors.New("agent instructions binding shape is invalid")
		}
	}
	return normalizeIntegrationShape(value, descriptor)
}

func requiredProtoScalarDefault(descriptor protoreflect.MessageDescriptor, field protoreflect.FieldDescriptor) (any, bool) {
	if descriptor.FullName() == "controlplane.v1.WorkflowCardSummary" {
		if field.Kind() == protoreflect.BoolKind {
			return false, field.JSONName() == "hasHumanGate"
		}
		if field.Kind() == protoreflect.Int32Kind {
			switch field.JSONName() {
			case "stageCount", "uniqueAgentCount", "parallelGroupCount", "activeRunCount", "pendingGateCount":
				return float64(0), true
			}
		}
	}
	if descriptor.FullName() == "controlplane.v1.Project" && field.Kind() == protoreflect.Int32Kind {
		switch field.JSONName() {
		case "agentCount", "workflowCount", "activeRunCount", "pendingGateCount":
			return float64(0), true
		}
	}
	if field.Kind() == protoreflect.Int64Kind {
		switch descriptor.FullName() {
		case "controlplane.v1.ProviderAccountBlockerCount":
			return float64(0), field.JSONName() == "total"
		case "controlplane.v1.ProviderAccountDeletion":
			return float64(0), field.JSONName() == "pendingCleanup"
		case "controlplane.v1.ListProviderAccountBlockersResponse":
			return float64(0), field.JSONName() == "total" || field.JSONName() == "hiddenCount" || field.JSONName() == "deletionIntentVersion"
		}
	}
	if descriptor.FullName() == "controlplane.v1.ProviderAccountBlocker" {
		if field.Kind() == protoreflect.BoolKind && field.JSONName() == "canCancel" {
			return false, true
		}
		if field.Kind() == protoreflect.StringKind && field.JSONName() == "name" {
			return "", true
		}
	}
	if descriptor.FullName() == "controlplane.v1.AgentInstructionsBinding" && field.Kind() == protoreflect.BoolKind && field.JSONName() == "effective" {
		return false, true
	}
	// Некоторым proto3 zero values соответствует обязательное поле OpenAPI.
	// Список явный: другие пустые scalar могут означать отсутствующую ссылку.
	if field.JSONName() == "total" && field.Kind() == protoreflect.Int64Kind {
		switch descriptor.FullName() {
		case "controlplane.v1.ListRunsResponse", "controlplane.v1.ListArtifactsResponse", "controlplane.v1.ListConfigOverlayRevisionsResponse":
			return float64(0), true
		}
	}
	if descriptor.FullName() == "controlplane.v1.TokenUsage" && isProto64BitInteger(field.Kind()) {
		return float64(0), true
	}
	if field.Kind() == protoreflect.StringKind {
		switch descriptor.FullName() {
		case "controlplane.v1.OwnerGateDecisionConsequence":
			return "", field.JSONName() == "safeSummary"
		case "controlplane.v1.IntegrationIntent":
			return "", field.JSONName() == "connectionName"
		case "controlplane.v1.ProviderAccountCandidate":
			return "", field.JSONName() == "defaultReasoningEffort"
		case "controlplane.v1.ConfigOverlayField":
			return "", field.JSONName() == "defaultValue"
		case "controlplane.v1.ConfigOverlayDiagnostic":
			return "", field.JSONName() == "key"
		case "controlplane.v1.ConfigOverlayVersion":
			return "", field.JSONName() == "content"
		case "controlplane.v1.AgentRuntimeConfigurationView":
			return "", field.JSONName() == "safeEffectiveConfig"
		case "controlplane.v1.ProviderAccount":
			return "", field.JSONName() == "externalAccountMasked"
		case "controlplane.v1.ModelCapability":
			return "", field.JSONName() == "defaultReasoningEffort"
		case "controlplane.v1.ProviderDefinition":
			return "", field.JSONName() == "description" || field.JSONName() == "defaultModelId"
		}
	}
	if descriptor.FullName() == "controlplane.v1.ConfigOverlayDiagnostic" && field.Kind() == protoreflect.Int32Kind {
		return float64(0), field.JSONName() == "line" || field.JSONName() == "column"
	}
	if descriptor.FullName() == "controlplane.v1.ProviderAccount" && field.Kind() == protoreflect.BoolKind {
		return false, field.JSONName() == "enabled" || field.JSONName() == "ready"
	}
	if descriptor.FullName() == "controlplane.v1.ProviderAccountUsage" {
		if field.Kind() == protoreflect.BoolKind {
			return false, field.JSONName() == "allowedToSubmit" || field.JSONName() == "eligibleForSelection"
		}
		if isProto64BitInteger(field.Kind()) {
			return float64(0), field.JSONName() == "agentVersion" || field.JSONName() == "activeExecutions"
		}
	}
	if descriptor.FullName() == "controlplane.v1.WorkflowLaunchReadiness" && field.Kind() == protoreflect.BoolKind {
		return false, field.JSONName() == "allowedToSubmit"
	}
	if field.Kind() == protoreflect.BoolKind {
		switch descriptor.FullName() {
		case "controlplane.v1.OwnerGateDecisionConsequence":
			return false, field.JSONName() == "executesExternalEffect" || field.JSONName() == "terminalForRun"
		case "controlplane.v1.IntegrationCapability":
			return false, field.JSONName() == "approvalRequired"
		case "controlplane.v1.IntegrationConfigurationField":
			return false, field.JSONName() == "required"
		case "controlplane.v1.IntegrationGrant":
			return false, field.JSONName() == "enabled"
		case "controlplane.v1.IntegrationDefinition":
			return false, field.JSONName() == "builtIn" || field.JSONName() == "available"
		case "controlplane.v1.IntegrationConnection":
			return false, field.JSONName() == "credentialsConfigured"
		}
	}
	if (descriptor.FullName() == "controlplane.v1.ModelCapability" || descriptor.FullName() == "controlplane.v1.ProviderDefinition") && field.Kind() == protoreflect.BoolKind {
		return false, field.JSONName() == "available" || field.JSONName() == "ready"
	}
	return nil, false
}

func normalizeProtoField(value any, field protoreflect.FieldDescriptor) (any, error) {
	if field.IsMap() {
		items, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("public protobuf map shape is invalid")
		}
		for key, item := range items {
			normalized, err := normalizeProtoField(item, field.MapValue())
			if err != nil {
				return nil, err
			}
			items[key] = normalized
		}
		return items, nil
	}
	if field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind {
		if field.Message().FullName() == "google.protobuf.Struct" ||
			field.Message().FullName() == "google.protobuf.Value" ||
			field.Message().FullName() == "google.protobuf.ListValue" {
			return value, nil
		}
		item, ok := value.(map[string]any)
		if !ok {
			return value, nil
		}
		return item, normalizeProtoJSONShape(item, field.Message())
	}
	switch field.Kind() {
	case protoreflect.EnumKind:
		return normalizeIntegrationEnum(value, field.Enum())
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		text, ok := value.(string)
		if !ok {
			return nil, errors.New("public protobuf int64 shape is invalid")
		}
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil || parsed < -maximumSafeJSONInteger || parsed > maximumSafeJSONInteger {
			return nil, errors.New("public protobuf int64 exceeds JSON safe range")
		}
		return float64(parsed), nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		text, ok := value.(string)
		if !ok {
			return nil, errors.New("public protobuf uint64 shape is invalid")
		}
		parsed, err := strconv.ParseUint(text, 10, 64)
		if err != nil || parsed > uint64(maximumSafeJSONInteger) {
			return nil, errors.New("public protobuf uint64 exceeds JSON safe range")
		}
		return float64(parsed), nil
	default:
		return value, nil
	}
}

func isProto64BitInteger(kind protoreflect.Kind) bool {
	switch kind {
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return true
	default:
		return false
	}
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
		for key, item := range current {
			if key == "integrationIntent" {
				continue
			}
			LocalizeSafeErrors(item, localize)
			if text, ok := item.(string); ok && strings.HasPrefix(text, "i18n:") {
				current[key] = localize(strings.TrimPrefix(text, "i18n:"))
			}
		}
		code, _ := current["safeErrorCode"].(string)
		if code != "" {
			current["safeErrorMessage"] = localize(strings.ToUpper(code))
		}
	}
}

var enumPrefixes = []string{
	"PLATFORM_ROLE_", "PROJECT_PERMISSION_", "NEXT_ACTION_", "ENTITY_LIFECYCLE_",
	"AGENT_STATE_", "INSTRUCTION_STATE_", "WORKFLOW_STATE_", "RUN_STATE_", "RUN_SOURCE_",
	"RUN_NODE_TYPE_", "RUN_NODE_STATE_", "RUN_EDGE_TYPE_", "RUN_EVENT_TYPE_",
	"RUN_EVENT_ACTOR_KIND_", "RUN_EVENT_MESSAGE_KIND_", "RUN_TOOL_CALL_STATE_",
	"OWNER_GATE_STATE_", "OWNER_GATE_DECISION_", "ARTIFACT_SCAN_STATE_", "ARTIFACT_SOURCE_", "ARTIFACT_LIFECYCLE_STATE_",
	"ATTACHMENT_SET_STATE_", "ATTACHMENT_SET_PURPOSE_",
	"SCHEDULE_STATE_", "CONNECTION_STATE_", "ASSISTANT_RUNTIME_STATE_", "ASSISTANT_PLAN_STATE_", "ASSISTANT_CONVERSATION_STATE_",
	"PROVIDER_ACCOUNT_STATE_", "PROVIDER_AUTHORIZATION_METHOD_", "PROVIDER_AUTHORIZATION_STATE_",
	"PROVIDER_ACCOUNT_DELETION_STATE_", "PROVIDER_ACCOUNT_VERIFICATION_STATE_", "PROVIDER_ACCOUNT_VERIFICATION_SCOPE_", "PROVIDER_ACCOUNT_BLOCKER_KIND_", "PROVIDER_ACCOUNT_QUEUED_WORK_OUTCOME_",
	"PROVIDER_ACCOUNT_USAGE_PURPOSE_", "PROVIDER_USAGE_STATE_", "PROVIDER_USAGE_REASON_", "PROVIDER_USAGE_REMEDIATION_", "PROVIDER_HEALTH_SCOPE_",
	"RUNTIME_SECRET_VALUE_TYPE_",
	"SEARCH_RESULT_KIND_",
	"PERMISSION_RISK_", "ACCESS_SUBJECT_KIND_", "ACCESS_SCOPE_KIND_", "ACCESS_RESOURCE_KIND_",
	"ACCESS_ROLE_KIND_", "ACCESS_ROLE_STATE_", "ACCESS_BINDING_STATE_", "OIDC_GROUP_STATE_",
	"ACCESS_DECISION_", "ACTION_", "TYPE_",
}

func normalize(value any) {
	switch current := value.(type) {
	case []any:
		for index, item := range current {
			normalize(item)
			if text, ok := item.(string); ok {
				current[index] = normalizeEnum(text)
			}
		}
	case map[string]any:
		for key, item := range current {
			// Ссылка — literal owner data, даже если совпала с префиксом enum.
			if key == "currentRunRef" {
				continue
			}
			// Typed intent уже проверен по Proto; его owner data не являются enum.
			if key == "integrationIntent" || key == "decisionConsequences" {
				continue
			}
			// Имена и ссылки контекста — literal owner data, не enum.
			_, contextRoute := current["route"]
			_, contextOperations := current["allowedOperations"]
			if contextRoute && contextOperations && (key == "route" || key == "entityKind" || key == "entityRef" || key == "entityName") {
				continue
			}
			_, blockerCancellation := current["canCancel"]
			if blockerCancellation && (key == "name" || key == "ref" || key == "projectRef") {
				continue
			}
			normalize(item)
			if text, ok := item.(string); ok {
				current[key] = normalizeEnum(text)
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
			current["runtimeRef"] = runtimeValue["ref"]
			current["runtimeName"] = runtimeValue["name"]
			current["runtimeRevision"] = runtimeValue["revision"]
			current["runtimeProvider"] = runtimeValue["provider"]
			current["runtimeModel"] = runtimeValue["model"]
			runtimeReady, _ := runtimeValue["ready"].(bool)
			current["runtimeReady"] = runtimeReady
			delete(current, "runtime")
		}
		if assistantState, ok := current["corePromptRevision"]; ok && assistantState != nil {
			current["system"] = true
			current["removable"] = false
		}
		normalizeAssistantShape(current)
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
		ensureRequiredCollections(current)
	}
}

func normalizeAssistantShape(value map[string]any) {
	if _, hasRoute := value["route"]; hasRoute {
		if _, hasOperations := value["allowedOperations"]; hasOperations {
			for _, key := range []string{"entityKind", "entityRef", "entityName"} {
				if _, exists := value[key]; !exists {
					value[key] = ""
				}
			}
		}
	}
	if _, isPlan := value["auditSummary"]; isPlan {
		if _, exists := value["applied"]; !exists {
			value["applied"] = false
		}
		for _, key := range []string{"operations", "validationProblems", "nextActions"} {
			if _, exists := value[key]; !exists {
				value[key] = []any{}
			}
		}
	}
	if _, isReceipt := value["planRevision"]; isReceipt {
		if operations, exists := value["operations"]; exists {
			value["operationReceipts"] = operations
			delete(value, "operations")
		}
		for _, key := range []string{"operationReceipts", "conflicts", "auditRefs", "createdResourceRefs"} {
			if _, exists := value[key]; !exists {
				value[key] = []any{}
			}
		}
	}
	if _, hasType := value["type"]; !hasType {
		return
	}
	if _, hasAction := value["action"]; !hasAction {
		return
	}
	targetKind, hasTargetKind := value["targetKind"]
	if !hasTargetKind {
		return
	}
	target := map[string]any{"kind": targetKind, "name": ""}
	if targetName, exists := value["targetName"]; exists {
		target["name"] = targetName
	}
	if targetRef, exists := value["targetRef"]; exists && targetRef != "" {
		target["ref"] = targetRef
	}
	value["target"] = target
	delete(value, "targetKind")
	delete(value, "targetRef")
	delete(value, "targetName")
	for _, key := range []string{"parameters", "before", "after"} {
		if _, exists := value[key]; !exists {
			value[key] = map[string]any{}
		}
	}
	for _, key := range []string{"selected", "permitted"} {
		if _, exists := value[key]; !exists {
			value[key] = false
		}
	}
	if _, exists := value["validationProblems"]; !exists {
		value["validationProblems"] = []any{}
	}
}

func normalizeEnum(value string) string {
	for _, prefix := range enumPrefixes {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimPrefix(value, prefix)
		}
	}
	return value
}

func ensureRequiredCollections(value map[string]any) {
	for _, key := range requiredCollectionKeys(value) {
		if _, exists := value[key]; !exists {
			value[key] = []any{}
		}
	}
	if _, isStep := value["position"]; isStep {
		if _, exists := value["parallel"]; !exists {
			value["parallel"] = false
		}
		if _, exists := value["parallelGroup"]; !exists {
			value["parallelGroup"] = float64(0)
		}
		if _, exists := value["expectedResult"]; !exists {
			value["expectedResult"] = ""
		}
		if _, exists := value["humanGate"]; !exists {
			value["humanGate"] = false
		}
	}
}

func requiredCollectionKeys(value map[string]any) []string {
	keys := []string{}
	if _, isStep := value["position"]; isStep {
		keys = append(keys, "gateDecisions", "requiredCapabilityKeys")
	}
	if _, isWorkflowInput := value["valueType"]; isWorkflowInput {
		keys = append(keys, "options")
	}
	if _, isAgent := value["roleDescription"]; isAgent {
		keys = append(keys, "capabilities", "integrations", "knowledgeArtifactRefs", "nextActions")
	}
	if _, isMembership := value["platformRole"]; isMembership {
		keys = append(keys, "permissions", "nextActions")
	}
	if _, isGraph := value["sequence"]; isGraph {
		if _, hasRevision := value["revision"]; hasRevision {
			keys = append(keys, "nodes", "edges")
		}
	}
	return keys
}

func target(value map[string]any) (string, any, bool) {
	// RunTarget является закрытым oneof с двумя собственными scalar-полями.
	// RunNode, WorkflowStep и grant тоже имеют agentRef, поэтому неизвестный
	// ключ закрыто исключает такую map из target-нормализации.
	for key := range value {
		switch key {
		case "agentRef", "workflowRef", "displayName", "targetVersion":
		default:
			return "", nil, false
		}
	}
	agentRef, hasAgent := value["agentRef"]
	workflowRef, hasWorkflow := value["workflowRef"]
	if hasAgent == hasWorkflow {
		return "", nil, false
	}
	if hasAgent {
		ref := agentRef
		return "AGENT", ref, true
	}
	return "WORKFLOW", workflowRef, true
}

func flattenWorkflow(value map[string]any) {
	for source, field := range map[string]string{"publishedVersion": "publishedRevisionRef", "draftVersion": "draftRevisionRef"} {
		if snapshot, ok := value[source].(map[string]any); ok {
			value[field] = snapshot["ref"]
		}
	}
	if draft, ok := value["draftVersion"].(map[string]any); ok {
		snapshot := map[string]any{}
		for _, key := range []string{"ref", "version", "revision", "state", "coordinatorAgentRef", "inputFields", "steps", "maxConcurrency", "timeoutSeconds", "completionCriteria", "validationMessages"} {
			if item, exists := draft[key]; exists {
				snapshot[key] = item
			}
		}
		value["draft"] = snapshot
	}
	version, _ := value["publishedVersion"].(map[string]any)
	if version == nil {
		version, _ = value["draftVersion"].(map[string]any)
	}
	if version != nil {
		value["revisionRef"] = version["ref"]
	}
	for _, key := range []string{"revision", "coordinatorAgentRef", "inputFields", "steps", "maxConcurrency", "timeoutSeconds", "completionCriteria", "validationMessages"} {
		if item, exists := version[key]; exists {
			value[key] = item
		}
	}
	for _, key := range []string{"inputFields", "steps", "validationMessages", "nextActions"} {
		if _, exists := value[key]; !exists {
			value[key] = []any{}
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

func permissions[T ~string](values []T) []controlplanev1.ProjectPermission {
	result := make([]controlplanev1.ProjectPermission, 0, len(values))
	for _, item := range values {
		if value, ok := controlplanev1.ProjectPermission_value["PROJECT_PERMISSION_"+string(item)]; ok {
			result = append(result, controlplanev1.ProjectPermission(value))
		}
	}
	return result
}

func platformRole(value string) controlplanev1.PlatformRole {
	return controlplanev1.PlatformRole(controlplanev1.PlatformRole_value["PLATFORM_ROLE_"+value])
}

func gateDecision(value string) controlplanev1.OwnerGateDecision {
	return controlplanev1.OwnerGateDecision(controlplanev1.OwnerGateDecision_value["OWNER_GATE_DECISION_"+value])
}

func jsonEncoder(writer io.Writer) *json.Encoder { return json.NewEncoder(writer) }
