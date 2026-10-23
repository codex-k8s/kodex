package httptransport

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/boundary"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ListRuntimeSecrets binds project context, reads owner metadata and rejects nil or foreign-scope entries before the shared no-store page projection. It never fetches secret values; search and page arguments are forwarded rather than locally revalidated here.
// ListRuntimeSecrets привязывает проект, читает metadata владельца и отклоняет nil и элементы чужой области до общей проекции страницы с no-store. Значения секретов не получает; поиск и page здесь передаются, а не проверяются заново.
func (server *Server) ListRuntimeSecrets(writer http.ResponseWriter, request *http.Request, projectRef generated.ProjectRef, parameters generated.ListRuntimeSecretsParams) {
	request, ok := withProjectReference(writer, request, projectRef)
	if !ok {
		return
	}
	setRuntimeSecretHeaders(writer)
	response, err := server.control.Query.ListRuntimeSecrets(request.Context(), &controlplanev1.ListRuntimeSecretsRequest{
		ProjectRef: projectRef, Query: stringValue(parameters.Query), Page: page(parameters.PageSize, parameters.PageToken),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	for _, secret := range response.GetSecrets() {
		if secret == nil || secret.GetScopeKind() != controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT || secret.GetProjectRef() != projectRef {
			invalidSecretDraft(writer)
			return
		}
	}
	writeRuntimeSecretPage(writer, response)
}

// writeRuntimeSecretPage projects at most 100 metadata entries after validating each scope tuple and one shared organization; nil replies fail closed and empty items are allocated. The continuation token is forwarded unchanged, not checked for bounds or advancement here.
// writeRuntimeSecretPage проецирует не более 100 metadata-элементов после проверки каждого scope tuple и единой организации; nil-ответ отклоняется, пустой items выделяется явно. Токен продолжения передаётся без проверки длины или продвижения.
func writeRuntimeSecretPage(writer http.ResponseWriter, response *controlplanev1.ListRuntimeSecretsResponse) {
	setRuntimeSecretHeaders(writer)
	if response == nil || len(response.GetSecrets()) > 100 {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	page := generated.RuntimeSecretPage{Items: make([]generated.RuntimeSecret, 0, len(response.GetSecrets())), NextPageToken: response.GetPage().GetNextPageToken()}
	organizationRef := ""
	for _, item := range response.GetSecrets() {
		if item == nil || !validRuntimeResourceScope(runtimeResourceScopeKind(item.GetScopeKind().String()), item.GetOrganizationRef(), item.GetProjectRef()) || organizationRef != "" && organizationRef != item.GetOrganizationRef() {
			writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		organizationRef = item.GetOrganizationRef()
		page.Items = append(page.Items, castControlPlaneRuntimeSecret(item))
	}
	writeJSON(writer, http.StatusOK, page)
}

// GetRuntimeSecret reads metadata by exact reference, validates the returned reference and resource scope, and writes a no-store view. Eligibility is owned by the typed owner query; this endpoint does not reveal or consume elevation for the value.
// GetRuntimeSecret читает metadata по точной ссылке, сверяет ссылку ответа и область ресурса, выдаёт no-store view. Eligibility принадлежит типизированному запросу владельца; значение не раскрывается и elevation для него не расходуется.
func (server *Server) GetRuntimeSecret(writer http.ResponseWriter, request *http.Request, secretRef generated.SecretRef) {
	setRuntimeSecretHeaders(writer)
	response, err := server.control.Query.GetRuntimeSecret(request.Context(), &controlplanev1.GetRuntimeSecretRequest{SecretRef: secretRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	if response.GetSecret() == nil || response.GetSecret().GetRef() != secretRef || !validRuntimeResourceScope(runtimeResourceScopeKind(response.GetSecret().GetScopeKind().String()), response.GetSecret().GetOrganizationRef(), response.GetSecret().GetProjectRef()) {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(writer, http.StatusOK, castControlPlaneRuntimeSecret(response.GetSecret()))
}

// CreateRuntimeSecret decodes bounded bytes, pins their SHA-256 in the project-scoped prepare command and either returns its terminal receipt or passes the grant to the broker. Returned metadata must retain the project scope; the temporary byte slice is erased on exit, but immutable request strings are not wiped.
// CreateRuntimeSecret декодирует ограниченные байты, закрепляет SHA-256 в проектном prepare и возвращает terminal receipt либо передаёт grant брокеру. Metadata ответа должны сохранить проектную область; временный срез затирается при выходе, неизменяемые строки запроса не затираются.
func (server *Server) CreateRuntimeSecret(writer http.ResponseWriter, request *http.Request, projectRef generated.ProjectRef, parameters generated.CreateRuntimeSecretParams) {
	request, ok := withProjectReference(writer, request, projectRef)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.RuntimeSecretCreateInput](writer, request)
	if !ok {
		return
	}
	valueType := runtimeSecretValueType(string(body.ValueType))
	value, ok := decodeRuntimeSecretValue(writer, valueType, body.Value)
	if !ok {
		return
	}
	defer erase(value)
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, "")
	if !ok {
		return
	}
	contentHash := sha256.Sum256(value)
	prepared, err := server.control.Command.PrepareCreateRuntimeSecret(request.Context(), &controlplanev1.PrepareCreateRuntimeSecretRequest{
		Mutation: mutation, ProjectRef: projectRef, Name: body.Name, Description: body.Description, ValueType: valueType,
		ExpectedContentSha256: runtimeSecretSHA256(contentHash),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	operation := prepared.GetOperation()
	if terminal := operation.GetTerminalSecret(); terminal != nil && (terminal.GetScopeKind() != controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT || terminal.GetProjectRef() != projectRef) {
		invalidSecretDraft(writer)
		return
	}
	if writeTerminalRuntimeSecretOperation(writer, http.StatusCreated, operation) {
		return
	}
	if server.secrets == nil {
		writeLocalProblem(writer, http.StatusServiceUnavailable, "UNAVAILABLE", true)
		return
	}
	response, err := server.secrets.CreateSecret(request.Context(), &secretbrokerv1.CreateSecretRequest{OperationGrant: operation.GetOperationGrant(), Value: value})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	setRuntimeSecretHeaders(writer)
	metadata := castRuntimeSecretMetadata(response.GetSecret())
	if metadata.ScopeKind != generated.RuntimeResourceScopeKindPROJECT || metadata.ProjectRef != projectRef || !validRuntimeResourceScope(metadata.ScopeKind, metadata.OrganizationRef, metadata.ProjectRef) {
		invalidSecretDraft(writer)
		return
	}
	writeJSON(writer, http.StatusCreated, metadata)
}

// RotateRuntimeSecret requires idempotency and expected version, decodes and hashes replacement bytes, then follows the owner-prepared rotation grant or terminal receipt. The broker result is checked against the requested secret before output; temporary value bytes are erased, not every string copy.
// RotateRuntimeSecret требует идемпотентность и ожидаемую версию, декодирует и хеширует замену, затем использует owner-prepared grant ротации либо terminal receipt. Ответ брокера сверяется с запрошенным секретом; затираются временные байты, а не все строковые копии.
func (server *Server) RotateRuntimeSecret(writer http.ResponseWriter, request *http.Request, secretRef generated.SecretRef, parameters generated.RotateRuntimeSecretParams) {
	body, ok := decodeJSON[generated.RuntimeSecretRotateInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	valueType := runtimeSecretValueType(string(body.ValueType))
	value, ok := decodeRuntimeSecretValue(writer, valueType, body.Value)
	if !ok {
		return
	}
	defer erase(value)
	contentHash := sha256.Sum256(value)
	prepared, err := server.control.Command.PrepareRotateRuntimeSecret(request.Context(), &controlplanev1.PrepareRotateRuntimeSecretRequest{
		Mutation: mutation, SecretRef: secretRef, ValueType: valueType, ExpectedContentSha256: runtimeSecretSHA256(contentHash),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	operation := prepared.GetOperation()
	if writeTerminalRuntimeSecretOperation(writer, http.StatusOK, operation) {
		return
	}
	if server.secrets == nil {
		writeLocalProblem(writer, http.StatusServiceUnavailable, "UNAVAILABLE", true)
		return
	}
	response, err := server.secrets.RotateSecret(request.Context(), &secretbrokerv1.RotateSecretRequest{OperationGrant: operation.GetOperationGrant(), Value: value})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	setRuntimeSecretHeaders(writer)
	writeRuntimeSecretMetadata(writer, response.GetSecret(), secretRef)
}

// RevealRuntimeSecret reads and verifies the exact metadata scope, consumes fresh scope-bound authentication, then obtains a reveal grant and calls the broker once. Errors never serialize the value; successful binary values become Base64 and the broker byte slice is erased after the no-store response, not the resulting strings.
// RevealRuntimeSecret читает и сверяет точную область metadata, расходует свежую привязанную аутентификацию, затем получает reveal grant и один раз вызывает брокер. Ошибки значение не сериализуют; binary кодируется Base64, срез брокера затирается после no-store ответа, полученные строки не затираются.
func (server *Server) RevealRuntimeSecret(writer http.ResponseWriter, request *http.Request, secretRef generated.SecretRef, parameters generated.RevealRuntimeSecretParams) {
	setRuntimeSecretHeaders(writer)
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, "")
	if !ok {
		return
	}
	metadata, err := server.control.Query.GetRuntimeSecret(request.Context(), &controlplanev1.GetRuntimeSecretRequest{SecretRef: secretRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	secret := metadata.GetSecret()
	scopeKind := runtimeResourceScopeKind(secret.GetScopeKind().String())
	projectRef, projectScoped := boundary.ProjectReferenceFromContext(request.Context())
	if secret.GetRef() != secretRef || !validRuntimeResourceScope(scopeKind, secret.GetOrganizationRef(), secret.GetProjectRef()) || scopeKind == generated.RuntimeResourceScopeKindORGANIZATION && projectScoped || scopeKind == generated.RuntimeResourceScopeKindPROJECT && (!projectScoped || projectRef != secret.GetProjectRef()) {
		writeLocalProblem(writer, http.StatusForbidden, "FRESH_AUTHENTICATION_REQUIRED", false)
		return
	}
	if err := server.boundary.ConsumeRuntimeSecretReveal(request.Context(), writer, string(scopeKind), secret.GetOrganizationRef(), secret.GetProjectRef(), secretRef); err != nil {
		if errors.Is(err, boundary.ErrElevationUnavailable) {
			writeLocalProblem(writer, http.StatusServiceUnavailable, "UNAVAILABLE", false)
		} else {
			writeLocalProblem(writer, http.StatusForbidden, "FRESH_AUTHENTICATION_REQUIRED", false)
		}
		return
	}
	prepared, err := server.control.Command.PrepareRevealRuntimeSecret(request.Context(), &controlplanev1.PrepareRevealRuntimeSecretRequest{
		Mutation: mutation, SecretRef: secretRef,
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	if server.secrets == nil {
		writeLocalProblem(writer, http.StatusServiceUnavailable, "UNAVAILABLE", true)
		return
	}
	operation := prepared.GetOperation()
	if operation == nil || operation.GetOperationGrant() == "" {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	response, err := server.secrets.RevealSecret(request.Context(), &secretbrokerv1.RevealSecretRequest{OperationGrant: operation.GetOperationGrant()})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	value := response.GetValue()
	defer erase(value)
	encoded := string(value)
	if response.GetValueType() == secretbrokerv1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_BINARY {
		encoded = base64.StdEncoding.EncodeToString(value)
	}
	writeJSON(writer, http.StatusOK, generated.RuntimeSecretReveal{Value: encoded, ValueType: generated.RuntimeSecretValueType(runtimeSecretValueTypeName(response.GetValueType()))})
}

// RevokeRuntimeSecret requires a versioned mutation and uses the dedicated owner prepare/revoke path. A terminal receipt skips the broker; otherwise returned metadata is checked against the requested secret. It does not delete historical revisions itself.
// RevokeRuntimeSecret требует versioned mutation и отдельный owner prepare/revoke: terminal receipt возвращается без нового вызова брокера, иначе metadata брокера сверяются с запрошенным секретом. Исторические ревизии самостоятельно не удаляет.
func (server *Server) RevokeRuntimeSecret(writer http.ResponseWriter, request *http.Request, secretRef generated.SecretRef, parameters generated.RevokeRuntimeSecretParams) {
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	prepared, err := server.control.Command.PrepareRevokeRuntimeSecret(request.Context(), &controlplanev1.PrepareRevokeRuntimeSecretRequest{
		Mutation: mutation, SecretRef: secretRef,
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	operation := prepared.GetOperation()
	if writeTerminalRuntimeSecretOperation(writer, http.StatusOK, operation) {
		return
	}
	if server.secrets == nil {
		writeLocalProblem(writer, http.StatusServiceUnavailable, "UNAVAILABLE", true)
		return
	}
	response, err := server.secrets.RevokeSecret(request.Context(), &secretbrokerv1.RevokeSecretRequest{OperationGrant: operation.GetOperationGrant()})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	setRuntimeSecretHeaders(writer)
	writeRuntimeSecretMetadata(writer, response.GetSecret(), secretRef)
}

// writeRuntimeSecretMetadata maps broker metadata and checks its exact secret reference and valid scope before HTTP 200; a mismatch returns the shared invalid-upstream problem. Cache headers are set by the calling mutation path, not this mapper.
// writeRuntimeSecretMetadata преобразует metadata брокера и сверяет точную ссылку секрета и область до HTTP 200; mismatch возвращает общий invalid-upstream problem. Cache headers задаёт вызывающая mutation-ветвь, а не этот mapper.
func writeRuntimeSecretMetadata(writer http.ResponseWriter, value *secretbrokerv1.RuntimeSecretMetadata, secretRef string) {
	metadata := castRuntimeSecretMetadata(value)
	if metadata.Ref != secretRef || !validRuntimeResourceScope(metadata.ScopeKind, metadata.OrganizationRef, metadata.ProjectRef) {
		invalidSecretDraft(writer)
		return
	}
	writeJSON(writer, http.StatusOK, metadata)
}

// decodeRuntimeSecretValue rejects empty/oversized input, strictly decodes standard Base64 for binary, and caps decoded values at 512 KiB. JSON additionally requires json.Valid and an unspecified type fails; rejected allocated bytes are erased. This is representation validation, not secret-content policy.
// decodeRuntimeSecretValue отклоняет пустой и слишком большой вход, строго декодирует standard Base64 для binary, ограничивает результат 512 КиБ. JSON требует json.Valid, unspecified type отклоняется; выделенные байты отказа затираются. Это проверка представления, не политика содержимого секрета.
func decodeRuntimeSecretValue(writer http.ResponseWriter, valueType controlplanev1.RuntimeSecretValueType, encoded string) ([]byte, bool) {
	if encoded == "" || len(encoded) > maximumJSONBody {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	if valueType == controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_BINARY {
		value, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || len(value) == 0 || len(value) > 512<<10 {
			erase(value)
			writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
			return nil, false
		}
		return value, true
	}
	value := []byte(encoded)
	if len(value) > 512<<10 || valueType == controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_JSON && !json.Valid(value) ||
		valueType == controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_UNSPECIFIED {
		erase(value)
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return value, true
}

// castRuntimeSecretMetadata builds a value-free public view from broker metadata, mapping enum names, times and an optional display hint; next actions are explicitly empty. It does not validate ownership, version bounds or the timestamp before conversion.
// castRuntimeSecretMetadata создаёт публичный вид без значения из metadata брокера, переносит enum names, время и необязательный display hint; next actions явно пусты. Владение, пределы версии и timestamp до преобразования не проверяет.
func castRuntimeSecretMetadata(value *secretbrokerv1.RuntimeSecretMetadata) generated.RuntimeSecret {
	result := generated.RuntimeSecret{
		Ref: value.GetSecretRef(), ScopeKind: runtimeResourceScopeKind(value.GetScopeKind().String()), OrganizationRef: value.GetOrganizationRef(), ProjectRef: value.GetProjectRef(), Name: value.GetName(), Description: value.GetDescription(),
		ValueType: generated.RuntimeSecretValueType(runtimeSecretValueTypeName(value.GetValueType())),
		State:     generated.RuntimeSecretState(runtimeSecretStatusName(value.GetStatus())), Version: value.GetVersion(), CurrentRevision: int64(value.GetRevision()),
		CreatedAt: runtimeSecretTime(value.GetCreatedAt()), UpdatedAt: runtimeSecretTime(value.GetUpdatedAt()),
		NextActions: make([]generated.NextAction, 0),
	}
	if hint := value.GetDisplayHint(); hint != nil {
		result.DisplayHint = &generated.RuntimeSecretDisplayHint{Prefix: hint.GetPrefix(), Suffix: hint.GetSuffix()}
	}
	return result
}

// castControlPlaneRuntimeSecret maps owner metadata, optional display hint and ordered next-action enum names without secret bytes. Scope, safe versions and timestamps require validation by the caller; the mapper itself does not establish eligibility.
// castControlPlaneRuntimeSecret переносит metadata владельца, необязательный display hint и порядок next-action enum names без байтов секрета. Область, safe versions и timestamps должен проверять вызывающий код; mapper сам eligibility не устанавливает.
func castControlPlaneRuntimeSecret(value *controlplanev1.RuntimeSecret) generated.RuntimeSecret {
	result := generated.RuntimeSecret{
		Ref: value.GetRef(), ScopeKind: runtimeResourceScopeKind(value.GetScopeKind().String()), OrganizationRef: value.GetOrganizationRef(), ProjectRef: value.GetProjectRef(), Name: value.GetName(), Description: value.GetDescription(),
		ValueType: generated.RuntimeSecretValueType(runtimeSecretValueTypeName(value.GetValueType())), State: generated.RuntimeSecretState(value.GetState()),
		Version: value.GetVersion(), CurrentRevision: value.GetCurrentRevision(), CreatedAt: runtimeSecretTime(value.GetCreatedAt()), UpdatedAt: runtimeSecretTime(value.GetUpdatedAt()),
		NextActions: make([]generated.NextAction, 0, len(value.GetNextActions())),
	}
	for _, action := range value.GetNextActions() {
		result.NextActions = append(result.NextActions, generated.NextAction(normalizeEnum(action.String())))
	}
	if hint := value.GetDisplayHint(); hint != nil {
		result.DisplayHint = &generated.RuntimeSecretDisplayHint{Prefix: hint.GetPrefix(), Suffix: hint.GetSuffix()}
	}
	return result
}

// writeTerminalRuntimeSecretOperation returns false only when a nonempty operation grant requires broker continuation. Nil receipts produce 502; grantless completed receipts require a terminal secret with valid scope, otherwise the operation returns conflict. This helper does not independently bind the terminal secret to the caller's requested ref.
// writeTerminalRuntimeSecretOperation возвращает false только при непустом operation grant для продолжения через брокер. Nil receipt даёт 502; completed без grant требует terminal secret с допустимой областью, иначе conflict. Сам helper отдельно не связывает terminal secret с запрошенной вызывающим кодом ссылкой.
func writeTerminalRuntimeSecretOperation(writer http.ResponseWriter, status int, operation *controlplanev1.RuntimeSecretOperationReceipt) bool {
	if operation == nil {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return true
	}
	if operation.GetOperationGrant() != "" {
		return false
	}
	if secret := operation.GetTerminalSecret(); operation.GetState() == controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_COMPLETED && secret != nil {
		if !validRuntimeResourceScope(runtimeResourceScopeKind(secret.GetScopeKind().String()), secret.GetOrganizationRef(), secret.GetProjectRef()) {
			invalidSecretDraft(writer)
			return true
		}
		setRuntimeSecretHeaders(writer)
		writeJSON(writer, status, castControlPlaneRuntimeSecret(secret))
		return true
	}
	writeLocalProblem(writer, http.StatusConflict, "RUNTIME_SECRET_OPERATION_FAILED", false)
	return true
}

// runtimeSecretSHA256 encodes an already computed fixed-size digest as lowercase hex for owner/broker content pins; it neither hashes input nor logs secret bytes.
// runtimeSecretSHA256 кодирует уже вычисленный digest фиксированной длины в lowercase hex для owner/broker content pins; вход не хеширует и байты секрета не логирует.
func runtimeSecretSHA256(value [sha256.Size]byte) string {
	return hex.EncodeToString(value[:])
}

// runtimeSecretTime converts a non-nil protobuf timestamp to UTC and maps nil to zero time. It deliberately performs no CheckValid or chronology check; callers retain those obligations.
// runtimeSecretTime преобразует ненулевой protobuf timestamp в UTC, nil — в нулевое время. CheckValid и хронологию намеренно не проверяет; эти обязанности остаются у вызывающего кода.
func runtimeSecretTime(value *timestamppb.Timestamp) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.AsTime().UTC()
}

// runtimeSecretValueType maps the three public value-type names to their control-plane enum and treats every other string as unspecified, which the decoder rejects.
// runtimeSecretValueType преобразует три публичных имени типа значения в enum control plane, остальные строки — в unspecified, отклоняемый decoder.
func runtimeSecretValueType(value string) controlplanev1.RuntimeSecretValueType {
	switch value {
	case "STRING":
		return controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING
	case "BINARY":
		return controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_BINARY
	case "JSON":
		return controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_JSON
	default:
		return controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_UNSPECIFIED
	}
}

// runtimeSecretValueTypeName removes the exact protobuf value-type prefix when a suffix exists and otherwise returns an empty name. It accepts either owner's enum String method but does not validate that the suffix is a supported public type.
// runtimeSecretValueTypeName убирает точный protobuf prefix типа значения при наличии суффикса, иначе возвращает пустое имя. Принимает String enum обоих владельцев, но не проверяет допустимость суффикса как публичного типа.
func runtimeSecretValueTypeName(value interface{ String() string }) string {
	const prefix = "RUNTIME_SECRET_VALUE_TYPE_"
	text := value.String()
	if len(text) > len(prefix) && text[:len(prefix)] == prefix {
		return text[len(prefix):]
	}
	return ""
}

// runtimeSecretStatusName maps only broker ACTIVE and REVOKED statuses to public names; unknown or unspecified statuses yield an empty string rather than an invented state.
// runtimeSecretStatusName преобразует только broker ACTIVE и REVOKED в публичные имена; неизвестный и unspecified дают пустую строку, а не выдуманный state.
func runtimeSecretStatusName(value secretbrokerv1.RuntimeSecretStatus) string {
	if value == secretbrokerv1.RuntimeSecretStatus_RUNTIME_SECRET_STATUS_ACTIVE {
		return "ACTIVE"
	}
	if value == secretbrokerv1.RuntimeSecretStatus_RUNTIME_SECRET_STATUS_REVOKED {
		return "REVOKED"
	}
	return ""
}

// setRuntimeSecretHeaders sets the three existing no-store/no-cache response headers for secret values and metadata; it does not erase previously written bodies or override other security headers.
// setRuntimeSecretHeaders задаёт три прежних no-store/no-cache заголовка для значений и metadata секретов; уже выданное тело не стирает и остальные security headers не подменяет.
func setRuntimeSecretHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("Expires", "0")
}

// erase overwrites the supplied mutable byte slice in place, including shared aliases; nil is harmless. It is best-effort cleanup of that buffer, not guaranteed removal of immutable strings, transport buffers or runtime copies.
// erase перезаписывает переданный изменяемый срез на месте, включая общие aliases; nil безопасен. Это best-effort очистка данного буфера, не гарантия удаления строк, transport buffers и копий runtime.
func erase(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
