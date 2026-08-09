package httptransport

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) ListProviders(writer http.ResponseWriter, request *http.Request) {
	response, err := server.integration.ListProviders(request.Context(), &integrationgatewayv1.ListProvidersRequest{})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	if response.GetCatalogVersion() == 0 || !validSHA256(response.GetCatalogDigestSha256()) {
		server.writeInternal(writer, request.Context(), errors.New("provider catalog metadata is invalid"))
		return
	}
	result := generated.ProviderCatalog{Version: int64(response.GetCatalogVersion()), DigestSha256: generated.Sha256(strings.ToLower(response.GetCatalogDigestSha256())), Providers: make([]generated.Provider, 0, len(response.GetProviders()))}
	for _, item := range response.GetProviders() {
		provider, convertErr := ConvertProvider(item)
		if convertErr != nil {
			server.writeInternal(writer, request.Context(), convertErr)
			return
		}
		result.Providers = append(result.Providers, provider)
	}
	writer.Header().Set("ETag", etag(response.GetCatalogVersion()))
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) GetProvider(writer http.ResponseWriter, request *http.Request, providerRef generated.ProviderRef, params generated.GetProviderParams) {
	response, err := server.integration.GetProvider(request.Context(), &integrationgatewayv1.GetProviderRequest{ProviderId: string(providerRef), ExpectedVersion: uint64(params.Version), ExpectedDigestSha256: string(params.DigestSha256)})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	provider, convertErr := ConvertProvider(response.GetProvider())
	if convertErr != nil || provider.ProviderRef != string(providerRef) || provider.Version != params.Version || provider.DigestSha256 != params.DigestSha256 {
		server.writeInternal(writer, request.Context(), errors.New("provider readback does not match exact request"))
		return
	}
	writer.Header().Set("ETag", etag(uint64(provider.Version)))
	writeJSON(writer, http.StatusOK, provider)
}

func (server *Server) StartProviderAuthorization(writer http.ResponseWriter, request *http.Request, params generated.StartProviderAuthorizationParams) {
	var body generated.StartProviderAuthorizationJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	response, err := server.integration.StartProviderAuthorization(request.Context(), &integrationgatewayv1.StartProviderAuthorizationRequest{
		ProviderId: body.ProviderRef, ConnectionStableKey: body.ConnectionStableKey, DisplayName: body.DisplayName, IdempotencyKey: params.IdempotencyKey.String(),
	})
	server.writeProviderAuthorization(writer, request, http.StatusAccepted, response.GetAuthorization(), err)
}

func (server *Server) GetProviderAuthorization(writer http.ResponseWriter, request *http.Request, authorizationRef generated.AuthorizationRef) {
	response, err := server.integration.GetProviderAuthorization(request.Context(), &integrationgatewayv1.GetProviderAuthorizationRequest{AuthorizationId: string(authorizationRef)})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	value, convertErr := ConvertProviderAuthorization(response.GetAuthorization())
	if convertErr != nil || value.AuthorizationRef != string(authorizationRef) {
		server.writeInternal(writer, request.Context(), errors.New("provider authorization readback is invalid"))
		return
	}
	writer.Header().Set("ETag", etag(uint64(value.Version)))
	writeJSON(writer, http.StatusOK, value)
}

func (server *Server) RestartProviderAuthorization(writer http.ResponseWriter, request *http.Request, authorizationRef generated.AuthorizationRef, params generated.RestartProviderAuthorizationParams) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	response, err := server.integration.RestartProviderAuthorization(request.Context(), &integrationgatewayv1.RestartProviderAuthorizationRequest{AuthorizationId: string(authorizationRef), ExpectedVersion: version, IdempotencyKey: params.IdempotencyKey.String()})
	server.writeProviderAuthorization(writer, request, http.StatusAccepted, response.GetAuthorization(), err)
}

func (server *Server) CancelProviderAuthorization(writer http.ResponseWriter, request *http.Request, authorizationRef generated.AuthorizationRef, params generated.CancelProviderAuthorizationParams) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	response, err := server.integration.CancelProviderAuthorization(request.Context(), &integrationgatewayv1.CancelProviderAuthorizationRequest{AuthorizationId: string(authorizationRef), ExpectedVersion: version, IdempotencyKey: params.IdempotencyKey.String()})
	server.writeProviderAuthorization(writer, request, http.StatusOK, response.GetAuthorization(), err)
}

func (server *Server) writeProviderAuthorization(writer http.ResponseWriter, request *http.Request, statusCode int, input *integrationgatewayv1.ProviderAuthorization, err error) {
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	value, convertErr := ConvertProviderAuthorization(input)
	if convertErr != nil {
		server.writeInternal(writer, request.Context(), convertErr)
		return
	}
	writer.Header().Set("ETag", etag(uint64(value.Version)))
	writeJSON(writer, statusCode, value)
}

func (server *Server) ListProviderConnections(writer http.ResponseWriter, request *http.Request, params generated.ListProviderConnectionsParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	states := []integrationgatewayv1.ManagedProviderConnectionState{}
	if params.State != nil {
		state, valid := providerConnectionStateInput(*params.State)
		if !valid {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
			return
		}
		states = append(states, state)
	}
	response, err := server.integration.ListProviderConnections(request.Context(), &integrationgatewayv1.ListProviderConnectionsRequest{States: states, PageSize: size, PageToken: token})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	result := generated.ProviderConnectionPage{Connections: make([]generated.ProviderConnection, 0, len(response.GetConnections()))}
	for _, item := range response.GetConnections() {
		connection, convertErr := ConvertProviderConnection(item)
		if convertErr != nil {
			server.writeInternal(writer, request.Context(), convertErr)
			return
		}
		result.Connections = append(result.Connections, connection)
	}
	if next := response.GetNextPageToken(); next != "" {
		result.NextPageToken = &next
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) GetProviderConnection(writer http.ResponseWriter, request *http.Request, connectionRef generated.ConnectionRef) {
	response, err := server.integration.GetProviderConnection(request.Context(), &integrationgatewayv1.GetProviderConnectionRequest{ConnectionId: string(connectionRef)})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	value, convertErr := ConvertProviderConnection(response.GetConnection())
	if convertErr != nil || value.ConnectionRef != string(connectionRef) {
		server.writeInternal(writer, request.Context(), errors.New("provider connection readback is invalid"))
		return
	}
	writer.Header().Set("ETag", etag(uint64(value.Version)))
	writeJSON(writer, http.StatusOK, value)
}

func (server *Server) ReauthorizeProviderConnection(writer http.ResponseWriter, request *http.Request, connectionRef generated.ConnectionRef, params generated.ReauthorizeProviderConnectionParams) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	response, err := server.integration.ReauthorizeProviderConnection(request.Context(), &integrationgatewayv1.ReauthorizeProviderConnectionRequest{ConnectionId: string(connectionRef), ExpectedVersion: version, ExpectedGeneration: uint64(params.Generation), IdempotencyKey: params.IdempotencyKey.String()})
	server.writeProviderAuthorization(writer, request, http.StatusAccepted, response.GetAuthorization(), err)
}

func (server *Server) RevokeProviderConnection(writer http.ResponseWriter, request *http.Request, connectionRef generated.ConnectionRef, params generated.RevokeProviderConnectionParams) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	response, err := server.integration.RevokeProviderConnection(request.Context(), &integrationgatewayv1.RevokeProviderConnectionRequest{ConnectionId: string(connectionRef), ExpectedVersion: version, ExpectedGeneration: uint64(params.Generation), IdempotencyKey: params.IdempotencyKey.String()})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	value, convertErr := ConvertProviderConnection(response.GetConnection())
	if convertErr != nil || value.ConnectionRef != string(connectionRef) {
		server.writeInternal(writer, request.Context(), errors.New("revoked provider connection readback is invalid"))
		return
	}
	writer.Header().Set("ETag", etag(uint64(value.Version)))
	writeJSON(writer, http.StatusOK, value)
}

func (server *Server) ListProviderPools(writer http.ResponseWriter, request *http.Request, params generated.ListProviderPoolsParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.integration.ListProviderPools(request.Context(), &integrationgatewayv1.ListProviderPoolsRequest{PageSize: size, PageToken: token})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	result := generated.ProviderPoolPage{Pools: make([]generated.ProviderPoolView, 0, len(response.GetProviderPools()))}
	for _, item := range response.GetProviderPools() {
		pool, convertErr := ConvertProviderPool(item)
		if convertErr != nil {
			server.writeInternal(writer, request.Context(), convertErr)
			return
		}
		result.Pools = append(result.Pools, pool)
	}
	if next := response.GetNextPageToken(); next != "" {
		result.NextPageToken = &next
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) GetProviderPool(writer http.ResponseWriter, request *http.Request, poolRef generated.PoolRef) {
	response, err := server.integration.GetProviderPool(request.Context(), &integrationgatewayv1.GetProviderPoolRequest{ProviderPoolId: string(poolRef)})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	value, convertErr := ConvertProviderPool(response.GetProviderPool())
	if convertErr != nil || value.PoolRef != string(poolRef) {
		server.writeInternal(writer, request.Context(), errors.New("provider pool readback is invalid"))
		return
	}
	writer.Header().Set("ETag", etag(uint64(value.Version)))
	writeJSON(writer, http.StatusOK, value)
}

func (server *Server) ManageProviderPool(writer http.ResponseWriter, request *http.Request, params generated.ManageProviderPoolParams) {
	var body generated.ManageProviderPoolJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	action := map[string]integrationgatewayv1.ProviderPoolAction{
		"CREATE": integrationgatewayv1.ProviderPoolAction_PROVIDER_POOL_ACTION_CREATE, "UPDATE": integrationgatewayv1.ProviderPoolAction_PROVIDER_POOL_ACTION_UPDATE,
		"ARCHIVE": integrationgatewayv1.ProviderPoolAction_PROVIDER_POOL_ACTION_ARCHIVE, "DELETE": integrationgatewayv1.ProviderPoolAction_PROVIDER_POOL_ACTION_DELETE,
	}[string(body.Action)]
	version, ok := commandVersion(writer, action == integrationgatewayv1.ProviderPoolAction_PROVIDER_POOL_ACTION_CREATE, params.IfMatch)
	if !ok || action == integrationgatewayv1.ProviderPoolAction_PROVIDER_POOL_ACTION_UNSPECIFIED || !validProviderPoolCommand(body, action) {
		if ok {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		}
		return
	}
	members := make([]*integrationgatewayv1.ProviderPoolMemberIntent, 0)
	if body.Members != nil {
		for _, item := range *body.Members {
			members = append(members, &integrationgatewayv1.ProviderPoolMemberIntent{ConnectionId: item.ConnectionRef, ExpectedConnectionVersion: uint64(item.ConnectionVersion), ExpectedConnectionGeneration: uint64(item.ConnectionGeneration), Weight: uint32(item.Weight)})
		}
	}
	response, err := server.integration.ManageProviderPool(request.Context(), &integrationgatewayv1.ManageProviderPoolRequest{Action: action, ProviderPoolId: stringValue(body.PoolRef), ExpectedVersion: version,
		StableKey: stringValue(body.StableKey), DisplayName: stringValue(body.DisplayName), Policy: stringValue(body.Policy), Members: members, IdempotencyKey: params.IdempotencyKey.String()})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	value, convertErr := ConvertProviderPool(response.GetProviderPool())
	if convertErr != nil {
		server.writeInternal(writer, request.Context(), convertErr)
		return
	}
	writer.Header().Set("ETag", etag(uint64(value.Version)))
	statusCode := http.StatusOK
	if action == integrationgatewayv1.ProviderPoolAction_PROVIDER_POOL_ACTION_CREATE {
		statusCode = http.StatusCreated
	}
	writeJSON(writer, statusCode, value)
}

func validProviderPoolCommand(body generated.ProviderPoolCommand, action integrationgatewayv1.ProviderPoolAction) bool {
	if action == integrationgatewayv1.ProviderPoolAction_PROVIDER_POOL_ACTION_CREATE || action == integrationgatewayv1.ProviderPoolAction_PROVIDER_POOL_ACTION_UPDATE {
		return stringValue(body.StableKey) != "" && stringValue(body.DisplayName) != "" && body.Policy != nil && body.Members != nil && len(*body.Members) > 0
	}
	return stringValue(body.PoolRef) != "" && body.StableKey == nil && body.DisplayName == nil && body.Policy == nil && body.Members == nil
}

// ConvertProvider не пропускает внутренние provider payload или credentials.
func ConvertProvider(input *integrationgatewayv1.Provider) (generated.Provider, error) {
	if input == nil || input.GetProviderId() == "" || input.GetDisplayName() == "" || input.GetVersion() == 0 || !validSHA256(input.GetDigestSha256()) {
		return generated.Provider{}, errors.New("provider projection is invalid")
	}
	capabilities, err := convertCapabilities(input.GetCapabilities())
	if err != nil {
		return generated.Provider{}, err
	}
	return generated.Provider{ProviderRef: input.GetProviderId(), Version: int64(input.GetVersion()), DigestSha256: generated.Sha256(strings.ToLower(input.GetDigestSha256())), DisplayName: input.GetDisplayName(), AuthorizationModes: append([]string(nil), input.GetAuthorizationModes()...), Capabilities: capabilities}, nil
}

func ConvertProviderAuthorization(input *integrationgatewayv1.ProviderAuthorization) (generated.ProviderAuthorization, error) {
	if input == nil || input.GetAuthorizationId() == "" || input.GetProviderId() == "" || input.GetAttempt() == 0 || input.GetVersion() == 0 || input.GetGeneration() == 0 {
		return generated.ProviderAuthorization{}, errors.New("provider authorization projection is incomplete")
	}
	state, ok := providerAuthorizationState(input.GetState())
	expires, expiresErr := requiredTimestamp(input.GetExpiresAt())
	updated, updatedErr := requiredTimestamp(input.GetUpdatedAt())
	if !ok || expiresErr != nil || updatedErr != nil {
		return generated.ProviderAuthorization{}, errors.New("provider authorization values are invalid")
	}
	result := generated.ProviderAuthorization{AuthorizationRef: input.GetAuthorizationId(), ProviderRef: input.GetProviderId(), Attempt: int(input.GetAttempt()), Version: int64(input.GetVersion()), Generation: int64(input.GetGeneration()), State: state,
		ConnectionRef: optionalString(input.GetConnectionId()), VerificationUrl: optionalString(input.GetVerificationUrl()), UserCode: optionalString(input.GetUserCode()), FailureCategory: optionalString(input.GetFailureCategory()), ExpiresAt: expires, UpdatedAt: updated}
	if input.GetCodeExpiresAt() != nil {
		value, err := requiredTimestamp(input.GetCodeExpiresAt())
		if err != nil {
			return generated.ProviderAuthorization{}, err
		}
		result.CodeExpiresAt = &value
	}
	return result, nil
}

func ConvertProviderConnection(input *integrationgatewayv1.ProviderConnection) (generated.ProviderConnection, error) {
	if input == nil || input.GetConnectionId() == "" || input.GetStableKey() == "" || input.GetProviderId() == "" || input.GetDisplayName() == "" || input.GetVersion() == 0 || input.GetGeneration() == 0 ||
		input.GetActiveCredentialGeneration() == 0 || !validSHA256(input.GetCapabilityDigestSha256()) || !validSHA256(input.GetObservationDigestSha256()) {
		return generated.ProviderConnection{}, errors.New("provider connection projection is incomplete")
	}
	state, ok := providerConnectionState(input.GetState())
	observed, observedErr := requiredTimestamp(input.GetObservedAt())
	updated, updatedErr := requiredTimestamp(input.GetUpdatedAt())
	if !ok || observedErr != nil || updatedErr != nil {
		return generated.ProviderConnection{}, errors.New("provider connection values are invalid")
	}
	result := generated.ProviderConnection{ConnectionRef: input.GetConnectionId(), StableKey: input.GetStableKey(), ProviderRef: input.GetProviderId(), DisplayName: input.GetDisplayName(), Version: int64(input.GetVersion()), Generation: int64(input.GetGeneration()), State: state,
		MaskedLabel: input.GetMaskedLabel(), MaskedAccount: input.GetMaskedAccount(), Capabilities: append([]string(nil), input.GetCapabilities()...), CapabilityDigestSha256: generated.Sha256(strings.ToLower(input.GetCapabilityDigestSha256())),
		ObservationDigestSha256: generated.Sha256(strings.ToLower(input.GetObservationDigestSha256())), ObservedAt: observed, UpdatedAt: updated, ActiveCredentialGeneration: int64(input.GetActiveCredentialGeneration())}
	if input.GetCapacity() != nil {
		capacity, err := convertProviderCapacity(input.GetCapacity())
		if err != nil {
			return generated.ProviderConnection{}, err
		}
		result.Capacity = &capacity
	}
	return result, nil
}

func convertProviderCapacity(input *integrationgatewayv1.ProviderCapacityObservation) (generated.ProviderCapacity, error) {
	if input.GetRevision() == 0 || !validSHA256(input.GetDigestSha256()) {
		return generated.ProviderCapacity{}, errors.New("provider capacity projection is invalid")
	}
	observed, err1 := requiredTimestamp(input.GetObservedAt())
	expires, err2 := requiredTimestamp(input.GetExpiresAt())
	if err1 != nil || err2 != nil {
		return generated.ProviderCapacity{}, errors.New("provider capacity timestamps are invalid")
	}
	result := generated.ProviderCapacity{Usage: int64(input.GetUsage()), Limit: int64(input.GetLimit()), Revision: int64(input.GetRevision()), ObservedAt: observed, WindowDurationSeconds: int64(input.GetWindowDurationSeconds()), ExpiresAt: expires, DigestSha256: generated.Sha256(strings.ToLower(input.GetDigestSha256()))}
	if input.GetResetsAt() != nil {
		value, err := requiredTimestamp(input.GetResetsAt())
		if err != nil {
			return generated.ProviderCapacity{}, err
		}
		result.ResetsAt = &value
	}
	return result, nil
}

func ConvertProviderPool(input *integrationgatewayv1.ProviderPool) (generated.ProviderPoolView, error) {
	if input == nil || input.GetProviderPoolId() == "" || input.GetStableKey() == "" || input.GetDisplayName() == "" || input.GetVersion() == 0 || input.GetObservationVersion() == 0 ||
		!validSHA256(input.GetDesiredDigestSha256()) || !validSHA256(input.GetObservationDigestSha256()) || !validSHA256(input.GetEffectiveDigestSha256()) || (input.GetPolicy() != "weighted" && input.GetPolicy() != "least_used") {
		return generated.ProviderPoolView{}, errors.New("provider pool projection is invalid")
	}
	updated, err := requiredTimestamp(input.GetUpdatedAt())
	if err != nil {
		return generated.ProviderPoolView{}, err
	}
	result := generated.ProviderPoolView{PoolRef: input.GetProviderPoolId(), StableKey: input.GetStableKey(), DisplayName: input.GetDisplayName(), Policy: generated.ProviderPoolViewPolicy(input.GetPolicy()), Version: int64(input.GetVersion()),
		DesiredDigestSha256: generated.Sha256(strings.ToLower(input.GetDesiredDigestSha256())), ObservationVersion: int64(input.GetObservationVersion()), ObservationDigestSha256: generated.Sha256(strings.ToLower(input.GetObservationDigestSha256())),
		EffectiveDigestSha256: generated.Sha256(strings.ToLower(input.GetEffectiveDigestSha256())), State: input.GetState(), UpdatedAt: updated, Members: make([]generated.ProviderPoolMemberView, 0, len(input.GetMembers()))}
	for _, item := range input.GetMembers() {
		if item == nil || item.GetConnectionId() == "" || item.GetConnectionVersion() == 0 || item.GetConnectionGeneration() == 0 || item.GetWeight() == 0 || !validSHA256(item.GetObservationDigestSha256()) {
			return generated.ProviderPoolView{}, errors.New("provider pool member is invalid")
		}
		result.Members = append(result.Members, generated.ProviderPoolMemberView{ConnectionRef: item.GetConnectionId(), ConnectionVersion: int64(item.GetConnectionVersion()), ConnectionGeneration: int64(item.GetConnectionGeneration()), ObservationDigestSha256: generated.Sha256(strings.ToLower(item.GetObservationDigestSha256())), Weight: int(item.GetWeight()), Eligible: item.GetEligible()})
	}
	return result, nil
}

func convertCapabilities(input []*integrationgatewayv1.ProviderCapability) ([]generated.ProviderCapability, error) {
	result := make([]generated.ProviderCapability, 0, len(input))
	seen := map[string]struct{}{}
	for _, item := range input {
		if item == nil || item.GetName() == "" || item.GetRisk() == "" {
			return nil, errors.New("provider capability is invalid")
		}
		if _, exists := seen[item.GetName()]; exists {
			return nil, errors.New("provider capability is duplicated")
		}
		seen[item.GetName()] = struct{}{}
		result = append(result, generated.ProviderCapability{Name: item.GetName(), Risk: item.GetRisk(), RequiresApproval: item.GetRequiresApproval()})
	}
	return result, nil
}

func providerConnectionStateInput(value generated.ProviderConnectionState) (integrationgatewayv1.ManagedProviderConnectionState, bool) {
	result := map[string]integrationgatewayv1.ManagedProviderConnectionState{"PENDING": integrationgatewayv1.ManagedProviderConnectionState_MANAGED_PROVIDER_CONNECTION_STATE_PENDING, "VALID": integrationgatewayv1.ManagedProviderConnectionState_MANAGED_PROVIDER_CONNECTION_STATE_VALID, "INVALID": integrationgatewayv1.ManagedProviderConnectionState_MANAGED_PROVIDER_CONNECTION_STATE_INVALID, "REVOKED": integrationgatewayv1.ManagedProviderConnectionState_MANAGED_PROVIDER_CONNECTION_STATE_REVOKED}[string(value)]
	return result, result != integrationgatewayv1.ManagedProviderConnectionState_MANAGED_PROVIDER_CONNECTION_STATE_UNSPECIFIED
}

func providerConnectionState(value integrationgatewayv1.ManagedProviderConnectionState) (generated.ProviderConnectionState, bool) {
	result := map[integrationgatewayv1.ManagedProviderConnectionState]generated.ProviderConnectionState{integrationgatewayv1.ManagedProviderConnectionState_MANAGED_PROVIDER_CONNECTION_STATE_PENDING: "PENDING", integrationgatewayv1.ManagedProviderConnectionState_MANAGED_PROVIDER_CONNECTION_STATE_VALID: "VALID", integrationgatewayv1.ManagedProviderConnectionState_MANAGED_PROVIDER_CONNECTION_STATE_INVALID: "INVALID", integrationgatewayv1.ManagedProviderConnectionState_MANAGED_PROVIDER_CONNECTION_STATE_REVOKED: "REVOKED"}[value]
	return result, result != ""
}

func providerAuthorizationState(value integrationgatewayv1.ProviderAuthorizationState) (generated.ProviderAuthorizationState, bool) {
	states := map[integrationgatewayv1.ProviderAuthorizationState]generated.ProviderAuthorizationState{
		integrationgatewayv1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_PENDING: "PENDING", integrationgatewayv1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_CODE_ISSUED: "CODE_ISSUED",
		integrationgatewayv1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_AUTHORIZED: "AUTHORIZED", integrationgatewayv1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_DENIED: "DENIED",
		integrationgatewayv1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_EXPIRED: "EXPIRED", integrationgatewayv1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_FAILED: "FAILED", integrationgatewayv1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_CANCELLED: "CANCELLED",
	}
	result := states[value]
	return result, result != ""
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func redactedPreview(input []byte) (summary string, fields []string, err error) {
	if len(input) == 0 || len(input) > 32<<10 {
		return "", nil, errors.New("redacted preview size is invalid")
	}
	var value map[string]json.RawMessage
	decoder := json.NewDecoder(strings.NewReader(string(input)))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil || value == nil || decoder.Decode(&struct{}{}) != io.EOF {
		return "", nil, errors.New("redacted preview is invalid")
	}
	fields = make([]string, 0, len(value))
	for field := range value {
		if field == "" || len(field) > 96 {
			return "", nil, errors.New("redacted preview field is invalid")
		}
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fmt.Sprintf("Параметры запроса: %d", len(fields)), fields, nil
}
