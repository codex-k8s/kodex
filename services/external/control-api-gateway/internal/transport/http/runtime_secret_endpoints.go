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
	page := generated.RuntimeSecretPage{NextPageToken: response.GetPage().GetNextPageToken()}
	for _, item := range response.GetSecrets() {
		page.Items = append(page.Items, castControlPlaneRuntimeSecret(item))
	}
	writeJSON(writer, http.StatusOK, page)
}

func (server *Server) GetRuntimeSecret(writer http.ResponseWriter, request *http.Request, secretRef generated.SecretRef) {
	setRuntimeSecretHeaders(writer)
	response, err := server.control.Query.GetRuntimeSecret(request.Context(), &controlplanev1.GetRuntimeSecretRequest{SecretRef: secretRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	if response.GetSecret() == nil {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(writer, http.StatusOK, castControlPlaneRuntimeSecret(response.GetSecret()))
}

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
	writeJSON(writer, http.StatusCreated, castRuntimeSecretMetadata(response.GetSecret()))
}

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
	writeJSON(writer, http.StatusOK, castRuntimeSecretMetadata(response.GetSecret()))
}

func (server *Server) RevealRuntimeSecret(writer http.ResponseWriter, request *http.Request, secretRef generated.SecretRef, parameters generated.RevealRuntimeSecretParams) {
	setRuntimeSecretHeaders(writer)
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, "")
	if !ok {
		return
	}
	projectRef, ok := boundary.ProjectReferenceFromContext(request.Context())
	if !ok {
		writeLocalProblem(writer, http.StatusForbidden, "FRESH_AUTHENTICATION_REQUIRED", false)
		return
	}
	if err := server.boundary.ConsumeRuntimeSecretReveal(request.Context(), writer, projectRef, secretRef); err != nil {
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
	writeJSON(writer, http.StatusOK, castRuntimeSecretMetadata(response.GetSecret()))
}

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

func castRuntimeSecretMetadata(value *secretbrokerv1.RuntimeSecretMetadata) generated.RuntimeSecret {
	result := generated.RuntimeSecret{
		Ref: value.GetSecretRef(), ProjectRef: value.GetProjectRef(), Name: value.GetName(), Description: value.GetDescription(),
		ValueType: generated.RuntimeSecretValueType(runtimeSecretValueTypeName(value.GetValueType())),
		State:     generated.RuntimeSecretState(runtimeSecretStatusName(value.GetStatus())), Version: value.GetVersion(), CurrentRevision: int64(value.GetRevision()),
		CreatedAt: runtimeSecretTime(value.GetCreatedAt()), UpdatedAt: runtimeSecretTime(value.GetUpdatedAt()),
	}
	if hint := value.GetDisplayHint(); hint != nil {
		result.DisplayHint = &generated.RuntimeSecretDisplayHint{Prefix: hint.GetPrefix(), Suffix: hint.GetSuffix()}
	}
	return result
}

func castControlPlaneRuntimeSecret(value *controlplanev1.RuntimeSecret) generated.RuntimeSecret {
	result := generated.RuntimeSecret{
		Ref: value.GetRef(), ProjectRef: value.GetProjectRef(), Name: value.GetName(), Description: value.GetDescription(),
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

func writeTerminalRuntimeSecretOperation(writer http.ResponseWriter, status int, operation *controlplanev1.RuntimeSecretOperationReceipt) bool {
	if operation == nil {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return true
	}
	if operation.GetOperationGrant() != "" {
		return false
	}
	if secret := operation.GetTerminalSecret(); operation.GetState() == controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_COMPLETED && secret != nil {
		setRuntimeSecretHeaders(writer)
		writeJSON(writer, status, castControlPlaneRuntimeSecret(secret))
		return true
	}
	writeLocalProblem(writer, http.StatusConflict, "RUNTIME_SECRET_OPERATION_FAILED", false)
	return true
}

func runtimeSecretSHA256(value [sha256.Size]byte) string {
	return hex.EncodeToString(value[:])
}

func runtimeSecretTime(value *timestamppb.Timestamp) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.AsTime().UTC()
}

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

func runtimeSecretValueTypeName(value interface{ String() string }) string {
	const prefix = "RUNTIME_SECRET_VALUE_TYPE_"
	text := value.String()
	if len(text) > len(prefix) && text[:len(prefix)] == prefix {
		return text[len(prefix):]
	}
	return ""
}

func runtimeSecretStatusName(value secretbrokerv1.RuntimeSecretStatus) string {
	if value == secretbrokerv1.RuntimeSecretStatus_RUNTIME_SECRET_STATUS_ACTIVE {
		return "ACTIVE"
	}
	if value == secretbrokerv1.RuntimeSecretStatus_RUNTIME_SECRET_STATUS_REVOKED {
		return "REVOKED"
	}
	return ""
}

func setRuntimeSecretHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("Expires", "0")
}

func erase(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
