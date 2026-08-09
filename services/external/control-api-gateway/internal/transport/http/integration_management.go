package httptransport

import (
	"errors"
	"net/http"
	"strings"

	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) ListIntegrationDefinitions(writer http.ResponseWriter, request *http.Request) {
	response, err := server.integration.ListIntegrationDefinitions(request.Context(), &integrationgatewayv1.ListIntegrationDefinitionsRequest{})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	result := generated.IntegrationDefinitionPage{Definitions: make([]generated.IntegrationDefinition, 0, len(response.GetDefinitions()))}
	for _, item := range response.GetDefinitions() {
		definition, convertErr := ConvertIntegrationDefinition(item)
		if convertErr != nil {
			server.writeInternal(writer, request.Context(), convertErr)
			return
		}
		result.Definitions = append(result.Definitions, definition)
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) GetIntegrationDefinition(writer http.ResponseWriter, request *http.Request, definitionRef generated.DefinitionRef, params generated.GetIntegrationDefinitionParams) {
	response, err := server.integration.GetIntegrationDefinition(request.Context(), &integrationgatewayv1.GetIntegrationDefinitionRequest{DefinitionId: string(definitionRef), Version: uint64(params.Version), ExpectedDigestSha256: string(params.DigestSha256)})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	value, convertErr := ConvertIntegrationDefinition(response.GetDefinition())
	if convertErr != nil || value.DefinitionRef != string(definitionRef) || value.Version != params.Version || value.DigestSha256 != params.DigestSha256 {
		server.writeInternal(writer, request.Context(), errors.New("integration definition readback does not match request"))
		return
	}
	writer.Header().Set("ETag", etag(uint64(value.Version)))
	writeJSON(writer, http.StatusOK, value)
}

func (server *Server) ListIntegrationConfigurations(writer http.ResponseWriter, request *http.Request, params generated.ListIntegrationConfigurationsParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.integration.ListIntegrationConfigurations(request.Context(), &integrationgatewayv1.ListIntegrationConfigurationsRequest{PageSize: size, PageToken: token})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	result := generated.IntegrationConfigurationPage{Configurations: make([]generated.IntegrationConfiguration, 0, len(response.GetConfigurations()))}
	for _, item := range response.GetConfigurations() {
		configuration, convertErr := ConvertIntegrationConfiguration(item)
		if convertErr != nil {
			server.writeInternal(writer, request.Context(), convertErr)
			return
		}
		result.Configurations = append(result.Configurations, configuration)
	}
	if next := response.GetNextPageToken(); next != "" {
		result.NextPageToken = &next
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) GetIntegrationConfiguration(writer http.ResponseWriter, request *http.Request, configurationRef generated.ConfigurationRef) {
	response, err := server.integration.GetIntegrationConfiguration(request.Context(), &integrationgatewayv1.GetIntegrationConfigurationRequest{ConfigurationId: string(configurationRef)})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	value, convertErr := ConvertIntegrationConfiguration(response.GetConfiguration())
	if convertErr != nil || value.ConfigurationRef != string(configurationRef) {
		server.writeInternal(writer, request.Context(), errors.New("integration configuration readback is invalid"))
		return
	}
	writer.Header().Set("ETag", etag(uint64(value.Version)))
	writeJSON(writer, http.StatusOK, value)
}

func (server *Server) ConfigureIntegration(writer http.ResponseWriter, request *http.Request, params generated.ConfigureIntegrationParams) {
	var body generated.ConfigureIntegrationJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	create := body.ConfigurationRef == nil
	version, ok := commandVersion(writer, create, params.IfMatch)
	if !ok {
		return
	}
	if body.StableKey == "" || body.DefinitionRef == "" || body.DefinitionVersion < 1 || !validSHA256(string(body.DefinitionDigestSha256)) || body.ConnectionRef == "" || body.ConnectionVersion < 1 || body.ConnectionGeneration < 1 || len(body.Capabilities) == 0 || body.EffectKind == "" {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.integration.ConfigureIntegration(request.Context(), &integrationgatewayv1.ConfigureIntegrationRequest{ConfigurationId: stringValue(body.ConfigurationRef), ExpectedVersion: version, StableKey: body.StableKey,
		DefinitionId: body.DefinitionRef, DefinitionVersion: uint64(body.DefinitionVersion), DefinitionDigestSha256: string(body.DefinitionDigestSha256), ConnectionId: body.ConnectionRef, ConnectionVersion: uint64(body.ConnectionVersion),
		ConnectionGeneration: uint64(body.ConnectionGeneration), Capabilities: append([]string(nil), body.Capabilities...), EffectKind: body.EffectKind, IdempotencyKey: params.IdempotencyKey.String()})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	value, convertErr := ConvertIntegrationConfiguration(response.GetConfiguration())
	if convertErr != nil {
		server.writeInternal(writer, request.Context(), convertErr)
		return
	}
	writer.Header().Set("ETag", etag(uint64(value.Version)))
	statusCode := http.StatusOK
	if create {
		statusCode = http.StatusCreated
	}
	writeJSON(writer, statusCode, value)
}

func (server *Server) TestIntegrationConnection(writer http.ResponseWriter, request *http.Request, params generated.TestIntegrationConnectionParams) {
	var body generated.TestIntegrationConnectionJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	response, err := server.integration.TestIntegrationConnection(request.Context(), &integrationgatewayv1.TestIntegrationConnectionRequest{ConnectionId: body.ConnectionRef, ConnectionVersion: uint64(body.ConnectionVersion), ConnectionGeneration: uint64(body.ConnectionGeneration),
		DefinitionId: body.DefinitionRef, DefinitionVersion: uint64(body.DefinitionVersion), DefinitionDigestSha256: string(body.DefinitionDigestSha256), ConfigurationId: stringValue(body.ConfigurationRef),
		ConfigurationVersion: uint64Value(body.ConfigurationVersion), ConfigurationDigestSha256: shaValue(body.ConfigurationDigestSha256), IdempotencyKey: params.IdempotencyKey.String()})
	server.writeIntegrationTest(writer, request, http.StatusAccepted, response.GetReceipt(), err)
}

func (server *Server) GetIntegrationTestReceipt(writer http.ResponseWriter, request *http.Request, testRef generated.TestRef) {
	response, err := server.integration.GetIntegrationTestReceipt(request.Context(), &integrationgatewayv1.GetIntegrationTestReceiptRequest{TestId: string(testRef)})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	value, convertErr := ConvertIntegrationTestReceipt(response.GetReceipt())
	if convertErr != nil || value.TestRef != string(testRef) {
		server.writeInternal(writer, request.Context(), errors.New("integration test receipt readback is invalid"))
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (server *Server) writeIntegrationTest(writer http.ResponseWriter, request *http.Request, statusCode int, input *integrationgatewayv1.IntegrationTestReceipt, err error) {
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	value, convertErr := ConvertIntegrationTestReceipt(input)
	if convertErr != nil {
		server.writeInternal(writer, request.Context(), convertErr)
		return
	}
	writeJSON(writer, statusCode, value)
}

func (server *Server) ListIntegrationApprovals(writer http.ResponseWriter, request *http.Request, params generated.ListIntegrationApprovalsParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	statuses := []string{}
	if params.Status != nil {
		if !approvalStatusValid(*params.Status) {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
			return
		}
		statuses = append(statuses, *params.Status)
	}
	response, err := server.integration.ListIntegrationApprovals(request.Context(), &integrationgatewayv1.ListIntegrationApprovalsRequest{Statuses: statuses, PageSize: size, PageToken: token})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	result := generated.IntegrationApprovalPage{Approvals: make([]generated.IntegrationApproval, 0, len(response.GetApprovals()))}
	for _, item := range response.GetApprovals() {
		value, convertErr := ConvertIntegrationApproval(item)
		if convertErr != nil {
			server.writeInternal(writer, request.Context(), convertErr)
			return
		}
		result.Approvals = append(result.Approvals, value)
	}
	if next := response.GetNextPageToken(); next != "" {
		result.NextPageToken = &next
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) GetIntegrationApproval(writer http.ResponseWriter, request *http.Request, approvalRef generated.ApprovalRef) {
	response, err := server.integration.GetIntegrationApproval(request.Context(), &integrationgatewayv1.GetIntegrationApprovalRequest{ApprovalId: string(approvalRef)})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	value, convertErr := ConvertIntegrationApproval(response.GetApproval())
	if convertErr != nil || value.ApprovalRef != string(approvalRef) {
		server.writeInternal(writer, request.Context(), errors.New("integration approval readback is invalid"))
		return
	}
	writer.Header().Set("ETag", etag(uint64(value.Version)))
	writeJSON(writer, http.StatusOK, value)
}

func (server *Server) DecideIntegrationApproval(writer http.ResponseWriter, request *http.Request, approvalRef generated.ApprovalRef, params generated.DecideIntegrationApprovalParams) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	var body generated.DecideIntegrationApprovalJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	decision := map[string]integrationgatewayv1.ApprovalDecision{"APPROVE": integrationgatewayv1.ApprovalDecision_APPROVAL_DECISION_APPROVE, "REJECT": integrationgatewayv1.ApprovalDecision_APPROVAL_DECISION_REJECT}[string(body.Decision)]
	if decision == integrationgatewayv1.ApprovalDecision_APPROVAL_DECISION_UNSPECIFIED || !validSHA256(body.ExpectedRequestHash) {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.integration.DecideIntegrationApproval(request.Context(), &integrationgatewayv1.DecideIntegrationApprovalRequest{ApprovalId: string(approvalRef), ExpectedVersion: version, ExpectedRequestHash: body.ExpectedRequestHash,
		Decision: decision, ReasonCode: body.ReasonCode, IdempotencyKey: params.IdempotencyKey.String()})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	value, convertErr := ConvertIntegrationApproval(response.GetApproval())
	if convertErr != nil || value.ApprovalRef != string(approvalRef) {
		server.writeInternal(writer, request.Context(), errors.New("integration approval decision readback is invalid"))
		return
	}
	writer.Header().Set("ETag", etag(uint64(value.Version)))
	writeJSON(writer, http.StatusOK, value)
}

func ConvertIntegrationDefinition(input *integrationgatewayv1.IntegrationDefinitionSummary) (generated.IntegrationDefinition, error) {
	if input == nil || input.GetDefinitionId() == "" || input.GetVersion() == 0 || !validSHA256(input.GetDigestSha256()) || input.GetDisplayName() == "" || input.GetState() == "" {
		return generated.IntegrationDefinition{}, errors.New("integration definition projection is invalid")
	}
	capabilities, err := convertCapabilities(input.GetCapabilities())
	if err != nil {
		return generated.IntegrationDefinition{}, err
	}
	return generated.IntegrationDefinition{DefinitionRef: input.GetDefinitionId(), Version: int64(input.GetVersion()), DigestSha256: generated.Sha256(strings.ToLower(input.GetDigestSha256())), DisplayName: input.GetDisplayName(), State: input.GetState(), Capabilities: capabilities}, nil
}

func ConvertIntegrationConfiguration(input *integrationgatewayv1.IntegrationConfiguration) (generated.IntegrationConfiguration, error) {
	if input == nil || input.GetConfigurationId() == "" || input.GetStableKey() == "" || input.GetVersion() == 0 || !validSHA256(input.GetDigestSha256()) || input.GetDefinitionId() == "" || input.GetDefinitionVersion() == 0 ||
		!validSHA256(input.GetDefinitionDigestSha256()) || input.GetConnectionId() == "" || input.GetConnectionVersion() == 0 || input.GetConnectionGeneration() == 0 || !validSHA256(input.GetCapabilityDigestSha256()) || input.GetEffectKind() == "" || input.GetState() == "" {
		return generated.IntegrationConfiguration{}, errors.New("integration configuration projection is invalid")
	}
	updated, err := requiredTimestamp(input.GetUpdatedAt())
	if err != nil {
		return generated.IntegrationConfiguration{}, err
	}
	return generated.IntegrationConfiguration{ConfigurationRef: input.GetConfigurationId(), StableKey: input.GetStableKey(), Version: int64(input.GetVersion()), DigestSha256: generated.Sha256(strings.ToLower(input.GetDigestSha256())),
		DefinitionRef: input.GetDefinitionId(), DefinitionVersion: int64(input.GetDefinitionVersion()), DefinitionDigestSha256: generated.Sha256(strings.ToLower(input.GetDefinitionDigestSha256())), ConnectionRef: input.GetConnectionId(), ConnectionVersion: int64(input.GetConnectionVersion()),
		ConnectionGeneration: int64(input.GetConnectionGeneration()), Capabilities: append([]string(nil), input.GetCapabilities()...), CapabilityDigestSha256: generated.Sha256(strings.ToLower(input.GetCapabilityDigestSha256())), EffectKind: input.GetEffectKind(), State: input.GetState(), UpdatedAt: updated}, nil
}

func ConvertIntegrationTestReceipt(input *integrationgatewayv1.IntegrationTestReceipt) (generated.IntegrationTestReceipt, error) {
	if input == nil || input.GetTestId() == "" || input.GetConnectionId() == "" || input.GetConnectionVersion() == 0 || input.GetConnectionGeneration() == 0 || input.GetDefinitionId() == "" || input.GetDefinitionVersion() == 0 ||
		!validSHA256(input.GetDefinitionDigestSha256()) || !validSHA256(input.GetReceiptSha256()) {
		return generated.IntegrationTestReceipt{}, errors.New("integration test receipt is incomplete")
	}
	category, ok := integrationTestCategory(input.GetCategory())
	tested, testedErr := requiredTimestamp(input.GetTestedAt())
	expires, expiresErr := requiredTimestamp(input.GetExpiresAt())
	if !ok || testedErr != nil || expiresErr != nil {
		return generated.IntegrationTestReceipt{}, errors.New("integration test receipt values are invalid")
	}
	result := generated.IntegrationTestReceipt{TestRef: input.GetTestId(), ConnectionRef: input.GetConnectionId(), ConnectionVersion: int64(input.GetConnectionVersion()), ConnectionGeneration: int64(input.GetConnectionGeneration()), DefinitionRef: input.GetDefinitionId(),
		DefinitionVersion: int64(input.GetDefinitionVersion()), DefinitionDigestSha256: generated.Sha256(strings.ToLower(input.GetDefinitionDigestSha256())), Category: category, ReceiptSha256: generated.Sha256(strings.ToLower(input.GetReceiptSha256())), TestedAt: tested, ExpiresAt: expires}
	if input.GetConfigurationId() != "" {
		if input.GetConfigurationVersion() == 0 || !validSHA256(input.GetConfigurationDigestSha256()) {
			return generated.IntegrationTestReceipt{}, errors.New("integration configuration receipt metadata is invalid")
		}
		ref, version, digest := input.GetConfigurationId(), int64(input.GetConfigurationVersion()), generated.Sha256(strings.ToLower(input.GetConfigurationDigestSha256()))
		result.ConfigurationRef, result.ConfigurationVersion, result.ConfigurationDigestSha256 = &ref, &version, &digest
	}
	return result, nil
}

func ConvertIntegrationApproval(input *integrationgatewayv1.IntegrationApproval) (generated.IntegrationApproval, error) {
	if input == nil || input.GetApprovalId() == "" || input.GetInvocationId() == "" || input.GetVersion() == 0 || !approvalStatusValid(input.GetStatus()) || !validSHA256(input.GetRequestHash()) {
		return generated.IntegrationApproval{}, errors.New("integration approval projection is incomplete")
	}
	expires, err := requiredTimestamp(input.GetExpiresAt())
	if err != nil {
		return generated.IntegrationApproval{}, err
	}
	summary, fields, previewErr := redactedPreview(input.GetRedactedPreviewJson())
	if previewErr != nil {
		return generated.IntegrationApproval{}, previewErr
	}
	result := generated.IntegrationApproval{ApprovalRef: input.GetApprovalId(), InvocationRef: input.GetInvocationId(), Version: int64(input.GetVersion()), Status: generated.IntegrationApprovalStatus(input.GetStatus()), RequestHash: strings.ToLower(input.GetRequestHash()), ExpiresAt: expires, ReasonCode: optionalString(input.GetReasonCode())}
	result.RedactedPreview.Summary, result.RedactedPreview.Fields = summary, fields
	if input.GetDecidedAt() != nil {
		value, timestampErr := requiredTimestamp(input.GetDecidedAt())
		if timestampErr != nil {
			return generated.IntegrationApproval{}, timestampErr
		}
		result.DecidedAt = &value
	}
	return result, nil
}

func integrationTestCategory(value integrationgatewayv1.IntegrationTestCategory) (generated.IntegrationTestCategory, bool) {
	categories := map[integrationgatewayv1.IntegrationTestCategory]generated.IntegrationTestCategory{
		integrationgatewayv1.IntegrationTestCategory_INTEGRATION_TEST_CATEGORY_PENDING: "PENDING", integrationgatewayv1.IntegrationTestCategory_INTEGRATION_TEST_CATEGORY_OK: "OK",
		integrationgatewayv1.IntegrationTestCategory_INTEGRATION_TEST_CATEGORY_CREDENTIAL_UNAVAILABLE: "CREDENTIAL_UNAVAILABLE", integrationgatewayv1.IntegrationTestCategory_INTEGRATION_TEST_CATEGORY_UNAUTHORIZED: "UNAUTHORIZED",
		integrationgatewayv1.IntegrationTestCategory_INTEGRATION_TEST_CATEGORY_FORBIDDEN: "FORBIDDEN", integrationgatewayv1.IntegrationTestCategory_INTEGRATION_TEST_CATEGORY_ENDPOINT_UNAVAILABLE: "ENDPOINT_UNAVAILABLE",
		integrationgatewayv1.IntegrationTestCategory_INTEGRATION_TEST_CATEGORY_TIMEOUT: "TIMEOUT", integrationgatewayv1.IntegrationTestCategory_INTEGRATION_TEST_CATEGORY_PROTOCOL_ERROR: "PROTOCOL_ERROR",
	}
	result := categories[value]
	return result, result != ""
}

func approvalStatusValid(value string) bool {
	switch value {
	case "PENDING", "APPROVED", "REJECTED", "CANCELLED", "EXPIRED":
		return true
	default:
		return false
	}
}
