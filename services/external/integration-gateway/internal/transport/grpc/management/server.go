// Package management реализует internal gRPC owner boundary Issue #236.
package management

import (
	"context"
	"errors"
	"slices"
	"sort"
	"time"

	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	managementservice "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/service/management"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/enum"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	callerWorkload = "control-api-gateway"
	callerSPIFFEID = "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway"
	targetWorkload = "integration-gateway"
	targetSPIFFEID = "spiffe://mattercodex.local/ns/mattercodex-system/sa/integration-gateway"
)

var methodPermissions = map[string]string{
	integrationgatewayv1.IntegrationManagementService_ListProviders_FullMethodName:                 "integration.management.read",
	integrationgatewayv1.IntegrationManagementService_GetProvider_FullMethodName:                   "integration.management.read",
	integrationgatewayv1.IntegrationManagementService_StartProviderAuthorization_FullMethodName:    "integration.management.manage",
	integrationgatewayv1.IntegrationManagementService_GetProviderAuthorization_FullMethodName:      "integration.management.read",
	integrationgatewayv1.IntegrationManagementService_RestartProviderAuthorization_FullMethodName:  "integration.management.manage",
	integrationgatewayv1.IntegrationManagementService_CancelProviderAuthorization_FullMethodName:   "integration.management.manage",
	integrationgatewayv1.IntegrationManagementService_ListProviderConnections_FullMethodName:       "integration.management.read",
	integrationgatewayv1.IntegrationManagementService_GetProviderConnection_FullMethodName:         "integration.management.read",
	integrationgatewayv1.IntegrationManagementService_ReauthorizeProviderConnection_FullMethodName: "integration.management.manage",
	integrationgatewayv1.IntegrationManagementService_RevokeProviderConnection_FullMethodName:      "integration.management.manage",
	integrationgatewayv1.IntegrationManagementService_ManageProviderPool_FullMethodName:            "integration.management.manage",
	integrationgatewayv1.IntegrationManagementService_GetProviderPool_FullMethodName:               "integration.management.read",
	integrationgatewayv1.IntegrationManagementService_ListProviderPools_FullMethodName:             "integration.management.read",
	integrationgatewayv1.IntegrationManagementService_ListIntegrationDefinitions_FullMethodName:    "integration.management.read",
	integrationgatewayv1.IntegrationManagementService_GetIntegrationDefinition_FullMethodName:      "integration.management.read",
	integrationgatewayv1.IntegrationManagementService_ConfigureIntegration_FullMethodName:          "integration.management.manage",
	integrationgatewayv1.IntegrationManagementService_GetIntegrationConfiguration_FullMethodName:   "integration.management.read",
	integrationgatewayv1.IntegrationManagementService_ListIntegrationConfigurations_FullMethodName: "integration.management.read",
	integrationgatewayv1.IntegrationManagementService_TestIntegrationConnection_FullMethodName:     "integration.management.test",
	integrationgatewayv1.IntegrationManagementService_GetIntegrationTestReceipt_FullMethodName:     "integration.management.read",
	integrationgatewayv1.IntegrationManagementService_ListIntegrationApprovals_FullMethodName:      "integration.management.read",
	integrationgatewayv1.IntegrationManagementService_GetIntegrationApproval_FullMethodName:        "integration.management.read",
	integrationgatewayv1.IntegrationManagementService_DecideIntegrationApproval_FullMethodName:     "integration.management.approve",
	integrationgatewayv1.IntegrationManagementService_ManageGitSourceBinding_FullMethodName:        "integration.management.git",
	integrationgatewayv1.IntegrationManagementService_GetGitSourceBinding_FullMethodName:           "integration.management.read",
	integrationgatewayv1.IntegrationManagementService_ListGitSourceBindings_FullMethodName:         "integration.management.read",
	integrationgatewayv1.IntegrationManagementService_ReconcileGitSourceBinding_FullMethodName:     "integration.management.git",
	integrationgatewayv1.IntegrationManagementService_GetManagementDiagnostics_FullMethodName:      "integration.management.read",
}

type Server struct {
	integrationgatewayv1.UnimplementedIntegrationManagementServiceServer
	service *managementservice.Service
}

func New(service *managementservice.Service) (*Server, error) {
	if service == nil {
		return nil, errors.New("integration management service is required")
	}
	return &Server{service: service}, nil
}

func ownerScope(ctx context.Context, fullMethod string) (domainrepo.Scope, error) {
	verified, ok := authorityclient.VerifiedAuthorizationContext(ctx)
	expectedPermission, registered := methodPermissions[fullMethod]
	if !ok || verified.GetFullMethod() != fullMethod || verified.GetCallerWorkloadId() != callerWorkload ||
		verified.GetCallerSpiffeId() != callerSPIFFEID || verified.GetTargetWorkloadId() != targetWorkload ||
		verified.GetTargetSpiffeId() != targetSPIFFEID || !registered || verified.GetPermission() != expectedPermission || verified.GetJti() == "" {
		return domainrepo.Scope{}, status.Error(codes.PermissionDenied, "verified integration management context rejected")
	}
	authority := verified.GetAuthority()
	if authority == nil || authority.GetActor() == nil || authority.GetTenant() == nil || authority.GetProject() == nil ||
		uuid.Validate(authority.GetActor().GetId()) != nil || uuid.Validate(authority.GetTenant().GetId()) != nil || uuid.Validate(authority.GetProject().GetId()) != nil {
		return domainrepo.Scope{}, status.Error(codes.PermissionDenied, "verified owner authority is incomplete")
	}
	return domainrepo.Scope{TenantID: authority.GetTenant().GetId(), ProjectID: authority.GetProject().GetId(), ActorID: authority.GetActor().GetId()}, nil
}

func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errs.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, "authentication failed")
	case errors.Is(err, errs.ErrForbidden):
		return status.Error(codes.PermissionDenied, "access denied")
	case errors.Is(err, errs.ErrNotFound):
		return status.Error(codes.NotFound, "resource not found")
	case errors.Is(err, errs.ErrConflict):
		return status.Error(codes.Aborted, "resource version conflict")
	case errors.Is(err, errs.ErrInvalid):
		return status.Error(codes.InvalidArgument, "request is invalid")
	case errors.Is(err, errs.ErrExpired):
		return status.Error(codes.FailedPrecondition, "resource expired")
	case errors.Is(err, errs.ErrCredentialUnavailable):
		return status.Error(codes.FailedPrecondition, "credential unavailable")
	default:
		return status.Error(codes.Internal, "integration management operation failed")
	}
}

func timestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

func optionalTimestamp(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamp(*value)
}

func providerToProto(value entity.ProviderDescriptor) *integrationgatewayv1.Provider {
	capabilities := make([]*integrationgatewayv1.ProviderCapability, 0, len(value.Capabilities))
	for _, capability := range value.Capabilities {
		capabilities = append(capabilities, &integrationgatewayv1.ProviderCapability{Name: capability.Name, Risk: capability.Risk, RequiresApproval: capability.RequiresApproval})
	}
	return &integrationgatewayv1.Provider{ProviderId: value.ID, Version: value.Version, DigestSha256: value.Digest, DisplayName: value.DisplayName, AuthorizationModes: slices.Clone(value.AuthorizationModes), Capabilities: capabilities}
}

func authorizationState(value string) integrationgatewayv1.ProviderAuthorizationState {
	return map[string]integrationgatewayv1.ProviderAuthorizationState{
		"PENDING":     integrationgatewayv1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_PENDING,
		"CODE_ISSUED": integrationgatewayv1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_CODE_ISSUED,
		"AUTHORIZED":  integrationgatewayv1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_AUTHORIZED,
		"DENIED":      integrationgatewayv1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_DENIED,
		"EXPIRED":     integrationgatewayv1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_EXPIRED,
		"FAILED":      integrationgatewayv1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_FAILED,
		"CANCELLED":   integrationgatewayv1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_CANCELLED,
	}[value]
}

func authorizationToProto(value entity.ProviderAuthorization) *integrationgatewayv1.ProviderAuthorization {
	return &integrationgatewayv1.ProviderAuthorization{AuthorizationId: value.ID, ProviderId: value.ProviderID, ConnectionId: value.ConnectionID, Attempt: value.Attempt, Version: value.Version, Generation: value.Generation, State: authorizationState(value.State), VerificationUrl: value.VerificationURL, UserCode: value.UserCode, CodeExpiresAt: optionalTimestamp(value.CodeExpiresAt), ExpiresAt: timestamp(value.ExpiresAt), FailureCategory: value.FailureCategory, UpdatedAt: timestamp(value.UpdatedAt)}
}

func connectionState(value string) integrationgatewayv1.ManagedProviderConnectionState {
	return map[string]integrationgatewayv1.ManagedProviderConnectionState{
		"PENDING": integrationgatewayv1.ManagedProviderConnectionState_MANAGED_PROVIDER_CONNECTION_STATE_PENDING,
		"VALID":   integrationgatewayv1.ManagedProviderConnectionState_MANAGED_PROVIDER_CONNECTION_STATE_VALID,
		"INVALID": integrationgatewayv1.ManagedProviderConnectionState_MANAGED_PROVIDER_CONNECTION_STATE_INVALID,
		"REVOKED": integrationgatewayv1.ManagedProviderConnectionState_MANAGED_PROVIDER_CONNECTION_STATE_REVOKED,
	}[value]
}

func connectionToProto(value entity.ManagedProviderConnection) *integrationgatewayv1.ProviderConnection {
	return &integrationgatewayv1.ProviderConnection{ConnectionId: value.ID, StableKey: value.StableKey, ProviderId: value.ProviderID, DisplayName: value.DisplayName, Version: value.Version, Generation: value.Generation, State: connectionState(value.Status), MaskedLabel: value.MaskedLabel, MaskedAccount: value.MaskedAccount, Capabilities: slices.Clone(value.Capabilities), CapabilityDigestSha256: value.CapabilityDigest, CredentialBindingId: value.CredentialBindingID, CredentialBindingVersion: value.CredentialBindingVersion, CredentialBindingSha256: value.CredentialBindingDigest, ObservationDigestSha256: value.ObservationDigest, ObservedAt: optionalTimestamp(value.ObservedAt), UpdatedAt: timestamp(value.UpdatedAt), ActiveCredentialGeneration: value.ActiveCredential}
}

func (server *Server) ListProviders(ctx context.Context, _ *integrationgatewayv1.ListProvidersRequest) (*integrationgatewayv1.ListProvidersResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_ListProviders_FullMethodName)
	if err != nil {
		return nil, err
	}
	values, version, digest, err := server.service.ListProviders(scope)
	if err != nil {
		return nil, mapError(err)
	}
	result := &integrationgatewayv1.ListProvidersResponse{CatalogVersion: version, CatalogDigestSha256: digest}
	for _, value := range values {
		result.Providers = append(result.Providers, providerToProto(value))
	}
	return result, nil
}

func (server *Server) GetProvider(ctx context.Context, request *integrationgatewayv1.GetProviderRequest) (*integrationgatewayv1.GetProviderResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_GetProvider_FullMethodName)
	if err != nil {
		return nil, err
	}
	value, err := server.service.GetProvider(scope, request.GetProviderId(), request.GetExpectedVersion(), request.GetExpectedDigestSha256())
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.GetProviderResponse{Provider: providerToProto(value)}, nil
}

func (server *Server) StartProviderAuthorization(ctx context.Context, request *integrationgatewayv1.StartProviderAuthorizationRequest) (*integrationgatewayv1.StartProviderAuthorizationResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_StartProviderAuthorization_FullMethodName)
	if err != nil {
		return nil, err
	}
	value, err := server.service.StartAuthorization(ctx, managementservice.StartAuthorizationInput{Scope: scope, ProviderID: request.GetProviderId(), StableKey: request.GetConnectionStableKey(), DisplayName: request.GetDisplayName(), IdempotencyKey: request.GetIdempotencyKey()})
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.StartProviderAuthorizationResponse{Authorization: authorizationToProto(value)}, nil
}

func (server *Server) GetProviderAuthorization(ctx context.Context, request *integrationgatewayv1.GetProviderAuthorizationRequest) (*integrationgatewayv1.GetProviderAuthorizationResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_GetProviderAuthorization_FullMethodName)
	if err != nil {
		return nil, err
	}
	value, err := server.service.GetAuthorization(ctx, scope, request.GetAuthorizationId())
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.GetProviderAuthorizationResponse{Authorization: authorizationToProto(value)}, nil
}

func (server *Server) RestartProviderAuthorization(ctx context.Context, request *integrationgatewayv1.RestartProviderAuthorizationRequest) (*integrationgatewayv1.RestartProviderAuthorizationResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_RestartProviderAuthorization_FullMethodName)
	if err != nil {
		return nil, err
	}
	value, err := server.service.RestartAuthorization(ctx, managementservice.RestartAuthorizationInput{Scope: scope, AuthorizationID: request.GetAuthorizationId(), ExpectedVersion: request.GetExpectedVersion(), IdempotencyKey: request.GetIdempotencyKey()})
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.RestartProviderAuthorizationResponse{Authorization: authorizationToProto(value)}, nil
}

func (server *Server) CancelProviderAuthorization(ctx context.Context, request *integrationgatewayv1.CancelProviderAuthorizationRequest) (*integrationgatewayv1.CancelProviderAuthorizationResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_CancelProviderAuthorization_FullMethodName)
	if err != nil {
		return nil, err
	}
	value, err := server.service.CancelAuthorization(ctx, scope, request.GetAuthorizationId(), request.GetExpectedVersion(), request.GetIdempotencyKey())
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.CancelProviderAuthorizationResponse{Authorization: authorizationToProto(value)}, nil
}

func (server *Server) ListProviderConnections(ctx context.Context, request *integrationgatewayv1.ListProviderConnectionsRequest) (*integrationgatewayv1.ListProviderConnectionsResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_ListProviderConnections_FullMethodName)
	if err != nil {
		return nil, err
	}
	states := make([]string, 0, len(request.GetStates()))
	for _, value := range request.GetStates() {
		states = append(states, connectionStateName(value))
	}
	values, next, err := server.service.ListConnections(ctx, scope, states, request.GetPageSize(), request.GetPageToken())
	if err != nil {
		return nil, mapError(err)
	}
	result := &integrationgatewayv1.ListProviderConnectionsResponse{NextPageToken: next}
	for _, value := range values {
		result.Connections = append(result.Connections, connectionToProto(value))
	}
	return result, nil
}

func connectionStateName(value integrationgatewayv1.ManagedProviderConnectionState) string {
	return map[integrationgatewayv1.ManagedProviderConnectionState]string{
		integrationgatewayv1.ManagedProviderConnectionState_MANAGED_PROVIDER_CONNECTION_STATE_PENDING: "PENDING",
		integrationgatewayv1.ManagedProviderConnectionState_MANAGED_PROVIDER_CONNECTION_STATE_VALID:   "VALID",
		integrationgatewayv1.ManagedProviderConnectionState_MANAGED_PROVIDER_CONNECTION_STATE_INVALID: "INVALID",
		integrationgatewayv1.ManagedProviderConnectionState_MANAGED_PROVIDER_CONNECTION_STATE_REVOKED: "REVOKED",
	}[value]
}

func (server *Server) GetProviderConnection(ctx context.Context, request *integrationgatewayv1.GetProviderConnectionRequest) (*integrationgatewayv1.GetProviderConnectionResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_GetProviderConnection_FullMethodName)
	if err != nil {
		return nil, err
	}
	value, err := server.service.GetConnection(ctx, scope, request.GetConnectionId())
	if err != nil {
		return nil, mapError(err)
	}
	if request.GetExpectedVersion() != 0 && request.GetExpectedVersion() != value.Version {
		return nil, mapError(errs.ErrConflict)
	}
	return &integrationgatewayv1.GetProviderConnectionResponse{Connection: connectionToProto(value)}, nil
}

func (server *Server) ReauthorizeProviderConnection(ctx context.Context, request *integrationgatewayv1.ReauthorizeProviderConnectionRequest) (*integrationgatewayv1.ReauthorizeProviderConnectionResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_ReauthorizeProviderConnection_FullMethodName)
	if err != nil {
		return nil, err
	}
	value, err := server.service.ReauthorizeConnection(ctx, scope, request.GetConnectionId(), request.GetExpectedVersion(), request.GetExpectedGeneration(), request.GetIdempotencyKey())
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.ReauthorizeProviderConnectionResponse{Authorization: authorizationToProto(value)}, nil
}

func (server *Server) RevokeProviderConnection(ctx context.Context, request *integrationgatewayv1.RevokeProviderConnectionRequest) (*integrationgatewayv1.RevokeProviderConnectionResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_RevokeProviderConnection_FullMethodName)
	if err != nil {
		return nil, err
	}
	value, err := server.service.RevokeConnection(ctx, scope, request.GetConnectionId(), request.GetExpectedVersion(), request.GetExpectedGeneration(), request.GetIdempotencyKey())
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.RevokeProviderConnectionResponse{Connection: connectionToProto(value)}, nil
}

func poolToProto(value entity.ManagedProviderPool) *integrationgatewayv1.ProviderPool {
	members := make([]*integrationgatewayv1.ProviderPoolMember, 0, len(value.Members))
	for _, member := range value.Members {
		members = append(members, &integrationgatewayv1.ProviderPoolMember{ConnectionId: member.ConnectionID, ConnectionVersion: member.ConnectionVersion, ConnectionGeneration: member.ConnectionGeneration, ObservationDigestSha256: member.ObservationDigest, Weight: member.Weight, Eligible: member.Eligible})
	}
	return &integrationgatewayv1.ProviderPool{ProviderPoolId: value.ID, StableKey: value.StableKey, DisplayName: value.DisplayName, Policy: value.Policy, Version: value.Version, DesiredDigestSha256: value.DesiredDigest, ObservationVersion: value.ObservationVersion, ObservationDigestSha256: value.ObservationDigest, EffectiveDigestSha256: value.EffectiveDigest, State: value.Status, Members: members, UpdatedAt: timestamp(value.UpdatedAt)}
}

func poolAction(value integrationgatewayv1.ProviderPoolAction) string {
	return map[integrationgatewayv1.ProviderPoolAction]string{integrationgatewayv1.ProviderPoolAction_PROVIDER_POOL_ACTION_CREATE: "CREATE", integrationgatewayv1.ProviderPoolAction_PROVIDER_POOL_ACTION_UPDATE: "UPDATE", integrationgatewayv1.ProviderPoolAction_PROVIDER_POOL_ACTION_ARCHIVE: "ARCHIVE", integrationgatewayv1.ProviderPoolAction_PROVIDER_POOL_ACTION_DELETE: "DELETE"}[value]
}

func (server *Server) ManageProviderPool(ctx context.Context, request *integrationgatewayv1.ManageProviderPoolRequest) (*integrationgatewayv1.ManageProviderPoolResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_ManageProviderPool_FullMethodName)
	if err != nil {
		return nil, err
	}
	members := make([]managementservice.PoolMemberInput, 0, len(request.GetMembers()))
	for _, member := range request.GetMembers() {
		members = append(members, managementservice.PoolMemberInput{ConnectionID: member.GetConnectionId(), ExpectedVersion: member.GetExpectedConnectionVersion(), ExpectedGeneration: member.GetExpectedConnectionGeneration(), Weight: member.GetWeight()})
	}
	value, err := server.service.ManagePool(ctx, managementservice.ManagePoolInput{Scope: scope, Action: poolAction(request.GetAction()), ID: request.GetProviderPoolId(), ExpectedVersion: request.GetExpectedVersion(), StableKey: request.GetStableKey(), DisplayName: request.GetDisplayName(), Policy: request.GetPolicy(), Members: members, IdempotencyKey: request.GetIdempotencyKey()})
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.ManageProviderPoolResponse{ProviderPool: poolToProto(value)}, nil
}

func (server *Server) GetProviderPool(ctx context.Context, request *integrationgatewayv1.GetProviderPoolRequest) (*integrationgatewayv1.GetProviderPoolResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_GetProviderPool_FullMethodName)
	if err != nil {
		return nil, err
	}
	value, err := server.service.GetPool(ctx, scope, request.GetProviderPoolId())
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.GetProviderPoolResponse{ProviderPool: poolToProto(value)}, nil
}

func (server *Server) ListProviderPools(ctx context.Context, request *integrationgatewayv1.ListProviderPoolsRequest) (*integrationgatewayv1.ListProviderPoolsResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_ListProviderPools_FullMethodName)
	if err != nil {
		return nil, err
	}
	values, next, err := server.service.ListPools(ctx, scope, request.GetPageSize(), request.GetPageToken())
	if err != nil {
		return nil, mapError(err)
	}
	result := &integrationgatewayv1.ListProviderPoolsResponse{NextPageToken: next}
	for _, value := range values {
		result.ProviderPools = append(result.ProviderPools, poolToProto(value))
	}
	return result, nil
}

func definitionToProto(value entity.Definition) *integrationgatewayv1.IntegrationDefinitionSummary {
	seen := make(map[string]*integrationgatewayv1.ProviderCapability)
	for _, tool := range value.Tools {
		seen[tool.Capability] = &integrationgatewayv1.ProviderCapability{Name: tool.Capability, Risk: string(tool.Risk), RequiresApproval: tool.ApprovalPolicy == enum.ApprovalAlways || tool.Risk.RequiresApproval()}
	}
	capabilities := make([]*integrationgatewayv1.ProviderCapability, 0, len(seen))
	for _, capability := range seen {
		capabilities = append(capabilities, capability)
	}
	sort.Slice(capabilities, func(left, right int) bool { return capabilities[left].Name < capabilities[right].Name })
	return &integrationgatewayv1.IntegrationDefinitionSummary{DefinitionId: value.ID, Version: value.Version, DigestSha256: value.Digest, DisplayName: value.ID, State: "ACTIVE", Capabilities: capabilities}
}

func (server *Server) ListIntegrationDefinitions(ctx context.Context, _ *integrationgatewayv1.ListIntegrationDefinitionsRequest) (*integrationgatewayv1.ListIntegrationDefinitionsResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_ListIntegrationDefinitions_FullMethodName)
	if err != nil {
		return nil, err
	}
	values, err := server.service.ListDefinitions(scope)
	if err != nil {
		return nil, mapError(err)
	}
	result := &integrationgatewayv1.ListIntegrationDefinitionsResponse{}
	for _, value := range values {
		result.Definitions = append(result.Definitions, definitionToProto(value))
	}
	return result, nil
}

func (server *Server) GetIntegrationDefinition(ctx context.Context, request *integrationgatewayv1.GetIntegrationDefinitionRequest) (*integrationgatewayv1.GetIntegrationDefinitionResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_GetIntegrationDefinition_FullMethodName)
	if err != nil {
		return nil, err
	}
	value, err := server.service.GetDefinition(scope, request.GetDefinitionId(), request.GetVersion(), request.GetExpectedDigestSha256())
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.GetIntegrationDefinitionResponse{Definition: definitionToProto(value)}, nil
}

func configurationToProto(value entity.IntegrationConfiguration) *integrationgatewayv1.IntegrationConfiguration {
	return &integrationgatewayv1.IntegrationConfiguration{ConfigurationId: value.ID, StableKey: value.StableKey, Version: value.Version, DigestSha256: value.Digest, DefinitionId: value.DefinitionID, DefinitionVersion: value.DefinitionVersion, DefinitionDigestSha256: value.DefinitionDigest, ConnectionId: value.ConnectionID, ConnectionVersion: value.ConnectionVersion, ConnectionGeneration: value.ConnectionGeneration, Capabilities: slices.Clone(value.Capabilities), CapabilityDigestSha256: value.CapabilityDigest, EffectKind: value.EffectKind, State: value.Status, UpdatedAt: timestamp(value.UpdatedAt)}
}

func (server *Server) ConfigureIntegration(ctx context.Context, request *integrationgatewayv1.ConfigureIntegrationRequest) (*integrationgatewayv1.ConfigureIntegrationResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_ConfigureIntegration_FullMethodName)
	if err != nil {
		return nil, err
	}
	value, err := server.service.ConfigureIntegration(ctx, managementservice.ConfigureIntegrationInput{Scope: scope, ID: request.GetConfigurationId(), ExpectedVersion: request.GetExpectedVersion(), StableKey: request.GetStableKey(), DefinitionID: request.GetDefinitionId(), DefinitionVersion: request.GetDefinitionVersion(), DefinitionDigest: request.GetDefinitionDigestSha256(), ConnectionID: request.GetConnectionId(), ConnectionVersion: request.GetConnectionVersion(), ConnectionGeneration: request.GetConnectionGeneration(), Capabilities: request.GetCapabilities(), EffectKind: request.GetEffectKind(), IdempotencyKey: request.GetIdempotencyKey()})
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.ConfigureIntegrationResponse{Configuration: configurationToProto(value)}, nil
}

func (server *Server) GetIntegrationConfiguration(ctx context.Context, request *integrationgatewayv1.GetIntegrationConfigurationRequest) (*integrationgatewayv1.GetIntegrationConfigurationResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_GetIntegrationConfiguration_FullMethodName)
	if err != nil {
		return nil, err
	}
	value, err := server.service.GetConfiguration(ctx, scope, request.GetConfigurationId())
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.GetIntegrationConfigurationResponse{Configuration: configurationToProto(value)}, nil
}

func (server *Server) ListIntegrationConfigurations(ctx context.Context, request *integrationgatewayv1.ListIntegrationConfigurationsRequest) (*integrationgatewayv1.ListIntegrationConfigurationsResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_ListIntegrationConfigurations_FullMethodName)
	if err != nil {
		return nil, err
	}
	values, next, err := server.service.ListConfigurations(ctx, scope, request.GetPageSize(), request.GetPageToken())
	if err != nil {
		return nil, mapError(err)
	}
	result := &integrationgatewayv1.ListIntegrationConfigurationsResponse{NextPageToken: next}
	for _, value := range values {
		result.Configurations = append(result.Configurations, configurationToProto(value))
	}
	return result, nil
}

func testCategory(value string) integrationgatewayv1.IntegrationTestCategory {
	return map[string]integrationgatewayv1.IntegrationTestCategory{"PENDING": integrationgatewayv1.IntegrationTestCategory_INTEGRATION_TEST_CATEGORY_PENDING, "OK": integrationgatewayv1.IntegrationTestCategory_INTEGRATION_TEST_CATEGORY_OK, "CREDENTIAL_UNAVAILABLE": integrationgatewayv1.IntegrationTestCategory_INTEGRATION_TEST_CATEGORY_CREDENTIAL_UNAVAILABLE, "UNAUTHORIZED": integrationgatewayv1.IntegrationTestCategory_INTEGRATION_TEST_CATEGORY_UNAUTHORIZED, "FORBIDDEN": integrationgatewayv1.IntegrationTestCategory_INTEGRATION_TEST_CATEGORY_FORBIDDEN, "ENDPOINT_UNAVAILABLE": integrationgatewayv1.IntegrationTestCategory_INTEGRATION_TEST_CATEGORY_ENDPOINT_UNAVAILABLE, "TIMEOUT": integrationgatewayv1.IntegrationTestCategory_INTEGRATION_TEST_CATEGORY_TIMEOUT, "PROTOCOL_ERROR": integrationgatewayv1.IntegrationTestCategory_INTEGRATION_TEST_CATEGORY_PROTOCOL_ERROR}[value]
}

func testToProto(value entity.IntegrationTestReceipt) *integrationgatewayv1.IntegrationTestReceipt {
	return &integrationgatewayv1.IntegrationTestReceipt{TestId: value.ID, ConnectionId: value.ConnectionID, ConnectionVersion: value.ConnectionVersion, ConnectionGeneration: value.ConnectionGeneration, DefinitionId: value.DefinitionID, DefinitionVersion: value.DefinitionVersion, Category: testCategory(value.Category), ReceiptSha256: value.Digest, TestedAt: optionalTimestamp(value.TestedAt), ExpiresAt: timestamp(value.ExpiresAt)}
}

func (server *Server) TestIntegrationConnection(ctx context.Context, request *integrationgatewayv1.TestIntegrationConnectionRequest) (*integrationgatewayv1.TestIntegrationConnectionResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_TestIntegrationConnection_FullMethodName)
	if err != nil {
		return nil, err
	}
	value, err := server.service.TestConnection(ctx, scope, request.GetConnectionId(), request.GetConnectionVersion(), request.GetConnectionGeneration(), request.GetDefinitionId(), request.GetDefinitionVersion(), request.GetIdempotencyKey())
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.TestIntegrationConnectionResponse{Receipt: testToProto(value)}, nil
}

func (server *Server) GetIntegrationTestReceipt(ctx context.Context, request *integrationgatewayv1.GetIntegrationTestReceiptRequest) (*integrationgatewayv1.GetIntegrationTestReceiptResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_GetIntegrationTestReceipt_FullMethodName)
	if err != nil {
		return nil, err
	}
	value, err := server.service.GetTestReceipt(ctx, scope, request.GetTestId())
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.GetIntegrationTestReceiptResponse{Receipt: testToProto(value)}, nil
}

func approvalToProto(value entity.Approval) *integrationgatewayv1.IntegrationApproval {
	return &integrationgatewayv1.IntegrationApproval{ApprovalId: value.ID, InvocationId: value.InvocationID, Version: value.Version, Status: string(value.Status), RequestHash: value.RequestHash, RedactedPreviewJson: slices.Clone(value.Preview), ExpiresAt: timestamp(value.ExpiresAt), DecidedAt: optionalTimestamp(value.DecidedAt), ReasonCode: value.DecisionReasonCode}
}

func (server *Server) ListIntegrationApprovals(ctx context.Context, request *integrationgatewayv1.ListIntegrationApprovalsRequest) (*integrationgatewayv1.ListIntegrationApprovalsResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_ListIntegrationApprovals_FullMethodName)
	if err != nil {
		return nil, err
	}
	values, next, err := server.service.ListApprovals(ctx, scope, request.GetStatuses(), request.GetPageSize(), request.GetPageToken())
	if err != nil {
		return nil, mapError(err)
	}
	result := &integrationgatewayv1.ListIntegrationApprovalsResponse{NextPageToken: next}
	for _, value := range values {
		result.Approvals = append(result.Approvals, approvalToProto(value))
	}
	return result, nil
}

func (server *Server) GetIntegrationApproval(ctx context.Context, request *integrationgatewayv1.GetIntegrationApprovalRequest) (*integrationgatewayv1.GetIntegrationApprovalResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_GetIntegrationApproval_FullMethodName)
	if err != nil {
		return nil, err
	}
	value, err := server.service.GetApproval(ctx, scope, request.GetApprovalId())
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.GetIntegrationApprovalResponse{Approval: approvalToProto(value)}, nil
}

func (server *Server) DecideIntegrationApproval(ctx context.Context, request *integrationgatewayv1.DecideIntegrationApprovalRequest) (*integrationgatewayv1.DecideIntegrationApprovalResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_DecideIntegrationApproval_FullMethodName)
	if err != nil {
		return nil, err
	}
	approve := request.GetDecision() == integrationgatewayv1.ApprovalDecision_APPROVAL_DECISION_APPROVE
	if !approve && request.GetDecision() != integrationgatewayv1.ApprovalDecision_APPROVAL_DECISION_REJECT {
		return nil, mapError(errs.ErrInvalid)
	}
	value, err := server.service.DecideApproval(ctx, scope, request.GetApprovalId(), request.GetExpectedVersion(), request.GetExpectedRequestHash(), approve, request.GetReasonCode(), request.GetIdempotencyKey())
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.DecideIntegrationApprovalResponse{Approval: approvalToProto(value)}, nil
}

func gitKindName(value integrationgatewayv1.GitTargetKind) string {
	return map[integrationgatewayv1.GitTargetKind]string{integrationgatewayv1.GitTargetKind_GIT_TARGET_KIND_ROLE_DEFINITION: "ROLE_DEFINITION", integrationgatewayv1.GitTargetKind_GIT_TARGET_KIND_AGENT: "AGENT", integrationgatewayv1.GitTargetKind_GIT_TARGET_KIND_INSTRUCTION_SET: "INSTRUCTION_SET", integrationgatewayv1.GitTargetKind_GIT_TARGET_KIND_PROVIDER_POOL: "PROVIDER_POOL"}[value]
}

func gitKind(value string) integrationgatewayv1.GitTargetKind {
	return map[string]integrationgatewayv1.GitTargetKind{"ROLE_DEFINITION": integrationgatewayv1.GitTargetKind_GIT_TARGET_KIND_ROLE_DEFINITION, "AGENT": integrationgatewayv1.GitTargetKind_GIT_TARGET_KIND_AGENT, "INSTRUCTION_SET": integrationgatewayv1.GitTargetKind_GIT_TARGET_KIND_INSTRUCTION_SET, "PROVIDER_POOL": integrationgatewayv1.GitTargetKind_GIT_TARGET_KIND_PROVIDER_POOL}[value]
}

func gitAction(value integrationgatewayv1.GitSourceBindingAction) string {
	return map[integrationgatewayv1.GitSourceBindingAction]string{integrationgatewayv1.GitSourceBindingAction_GIT_SOURCE_BINDING_ACTION_CREATE: "CREATE", integrationgatewayv1.GitSourceBindingAction_GIT_SOURCE_BINDING_ACTION_UPDATE: "UPDATE", integrationgatewayv1.GitSourceBindingAction_GIT_SOURCE_BINDING_ACTION_ARCHIVE: "ARCHIVE"}[value]
}

func gitBindingToProto(value entity.GitSourceBinding) *integrationgatewayv1.GitSourceBinding {
	return &integrationgatewayv1.GitSourceBinding{BindingId: value.ID, StableKey: value.StableKey, Version: value.Version, State: value.Status, RepositoryKey: value.RepositoryKey, RefKey: value.RefKey, PathKey: value.PathKey, RepositoryConnectionId: value.RepositoryConnectionID, RepositoryConnectionVersion: value.RepositoryConnectionVersion, RepositoryConnectionSha256: value.RepositoryConnectionDigest, CredentialBindingId: value.CredentialBindingID, CredentialBindingVersion: value.CredentialBindingVersion, CredentialBindingSha256: value.CredentialBindingDigest, TargetKind: gitKind(value.TargetKind), TargetStableKey: value.TargetStableKey, FetchedCommit: value.FetchedCommit, SourceRevision: value.SourceRevision, SourceDigestSha256: value.SourceDigest, FetchedAt: optionalTimestamp(value.FetchedAt), UpdatedAt: timestamp(value.UpdatedAt)}
}

func (server *Server) ManageGitSourceBinding(ctx context.Context, request *integrationgatewayv1.ManageGitSourceBindingRequest) (*integrationgatewayv1.ManageGitSourceBindingResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_ManageGitSourceBinding_FullMethodName)
	if err != nil {
		return nil, err
	}
	value, err := server.service.ManageGitBinding(ctx, managementservice.ManageGitBindingInput{Scope: scope, Action: gitAction(request.GetAction()), ID: request.GetBindingId(), ExpectedVersion: request.GetExpectedVersion(), StableKey: request.GetStableKey(), RepositoryKey: request.GetRepositoryKey(), RefKey: request.GetRefKey(), PathKey: request.GetPathKey(), TargetKind: gitKindName(request.GetTargetKind()), TargetStableKey: request.GetTargetStableKey(), IdempotencyKey: request.GetIdempotencyKey()})
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.ManageGitSourceBindingResponse{Binding: gitBindingToProto(value)}, nil
}

func (server *Server) GetGitSourceBinding(ctx context.Context, request *integrationgatewayv1.GetGitSourceBindingRequest) (*integrationgatewayv1.GetGitSourceBindingResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_GetGitSourceBinding_FullMethodName)
	if err != nil {
		return nil, err
	}
	value, err := server.service.GetGitBinding(ctx, scope, request.GetBindingId())
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.GetGitSourceBindingResponse{Binding: gitBindingToProto(value)}, nil
}

func (server *Server) ListGitSourceBindings(ctx context.Context, request *integrationgatewayv1.ListGitSourceBindingsRequest) (*integrationgatewayv1.ListGitSourceBindingsResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_ListGitSourceBindings_FullMethodName)
	if err != nil {
		return nil, err
	}
	values, next, err := server.service.ListGitBindings(ctx, scope, request.GetPageSize(), request.GetPageToken())
	if err != nil {
		return nil, mapError(err)
	}
	result := &integrationgatewayv1.ListGitSourceBindingsResponse{NextPageToken: next}
	for _, value := range values {
		result.Bindings = append(result.Bindings, gitBindingToProto(value))
	}
	return result, nil
}

func gitReconciliationToProto(value entity.GitReconciliation) *integrationgatewayv1.GitReconciliation {
	state := map[string]integrationgatewayv1.GitReconciliationState{"PENDING": integrationgatewayv1.GitReconciliationState_GIT_RECONCILIATION_STATE_PENDING, "FETCHED": integrationgatewayv1.GitReconciliationState_GIT_RECONCILIATION_STATE_FETCHED, "APPLIED": integrationgatewayv1.GitReconciliationState_GIT_RECONCILIATION_STATE_APPLIED, "FAILED": integrationgatewayv1.GitReconciliationState_GIT_RECONCILIATION_STATE_FAILED, "CANCELLED": integrationgatewayv1.GitReconciliationState_GIT_RECONCILIATION_STATE_CANCELLED}[value.State]
	return &integrationgatewayv1.GitReconciliation{ReconciliationId: value.ID, BindingId: value.BindingID, BindingVersion: value.BindingVersion, State: state, FetchedCommit: value.FetchedCommit, SourceRevision: value.SourceRevision, SourceDigestSha256: value.SourceDigest, TargetResourceId: value.TargetResourceID, TargetVersion: value.TargetVersion, TargetDigestSha256: value.TargetDigest, ReceiptId: value.ReceiptID, ReceiptSha256: value.ReceiptDigest, FailureCategory: value.FailureCategory, UpdatedAt: timestamp(value.UpdatedAt)}
}

func (server *Server) ReconcileGitSourceBinding(ctx context.Context, request *integrationgatewayv1.ReconcileGitSourceBindingRequest) (*integrationgatewayv1.ReconcileGitSourceBindingResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_ReconcileGitSourceBinding_FullMethodName)
	if err != nil {
		return nil, err
	}
	value, err := server.service.ReconcileGitBinding(ctx, scope, request.GetBindingId(), request.GetExpectedVersion(), request.GetExpectedSourceRevision(), request.GetIdempotencyKey())
	if err != nil {
		return nil, mapError(err)
	}
	return &integrationgatewayv1.ReconcileGitSourceBindingResponse{Reconciliation: gitReconciliationToProto(value)}, nil
}

func (server *Server) GetManagementDiagnostics(ctx context.Context, _ *integrationgatewayv1.GetManagementDiagnosticsRequest) (*integrationgatewayv1.GetManagementDiagnosticsResponse, error) {
	scope, err := ownerScope(ctx, integrationgatewayv1.IntegrationManagementService_GetManagementDiagnostics_FullMethodName)
	if err != nil {
		return nil, err
	}
	values, err := server.service.Diagnostics(ctx, scope)
	result := &integrationgatewayv1.GetManagementDiagnosticsResponse{Status: "READY"}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	now := time.Now().UTC()
	for _, key := range keys {
		result.Dependencies = append(result.Dependencies, &integrationgatewayv1.ManagementDependencyStatus{Dependency: key, Status: values[key], Version: 1, CheckedAt: timestamp(now)})
		if values[key] != "READY" {
			result.Status = "UNAVAILABLE"
		}
	}
	if err != nil {
		result.Status = "UNAVAILABLE"
		return result, mapError(err)
	}
	return result, nil
}
